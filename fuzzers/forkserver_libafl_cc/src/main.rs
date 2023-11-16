// use core::time::Duration;
// use std::path::PathBuf;
//
// use clap::{self, Parser};
// use libafl::{
//     bolts::{
//         current_nanos,
//         rands::StdRand,
//         shmem::{ShMem, ShMemProvider, UnixShMemProvider},
//         tuples::{tuple_list, MatchName, Merge},
//         AsMutSlice,
//     },
//     corpus::{Corpus, InMemoryCorpus, OnDiskCorpus},
//     events::SimpleEventManager,
//     executors::{
//         forkserver::{ForkserverExecutor, TimeoutForkserverExecutor},
//         HasObservers,
//     },
//     feedback_and_fast, feedback_or,
//     feedbacks::{CrashFeedback, MaxMapFeedback, TimeFeedback},
//     fuzzer::{Fuzzer, StdFuzzer},
//     inputs::BytesInput,
//     monitors::SimpleMonitor,
//     mutators::{scheduled::havoc_mutations, tokens_mutations, StdScheduledMutator, Tokens},
//     observers::{HitcountsMapObserver, MapObserver, StdMapObserver, TimeObserver},
//     schedulers::{IndexesLenTimeMinimizerScheduler, QueueScheduler},
//     stages::mutational::StdMutationalStage,
//     state::{HasCorpus, HasMetadata, StdState},
// };
// use nix::sys::signal::Signal;
//
// /// The commandline args this fuzzer accepts
// #[derive(Debug, Parser)]
// #[command(
//     name = "forkserver_libafl_cc",
//     about = "This is a simple example fuzzer to fuzz a executable instrumented by libafl_cc.",
//     author = "ergrelet <ergrelet@users.noreply.github.com>"
// )]
// struct Opt {
//     #[arg(
//         help = "The instrumented binary we want to fuzz",
//         name = "EXEC",
//         required = true
//     )]
//     executable: String,
//
//     #[arg(
//         help = "The directory to read initial inputs from ('seeds')",
//         name = "INPUT_DIR",
//         required = true
//     )]
//     in_dir: PathBuf,
//
//     #[arg(
//         help = "Timeout for each individual execution, in milliseconds",
//         short = 't',
//         long = "timeout",
//         default_value = "1200"
//     )]
//     timeout: u64,
//
//     #[arg(
//         help = "If not set, the child's stdout and stderror will be redirected to /dev/null",
//         short = 'd',
//         long = "debug-child",
//         default_value = "false"
//     )]
//     debug_child: bool,
//
//     #[arg(
//         help = "Arguments passed to the target",
//         name = "arguments",
//         num_args(1..),
//         allow_hyphen_values = true,
//     )]
//     arguments: Vec<String>,
//
//     #[arg(
//         help = "Signal used to stop child",
//         short = 's',
//         long = "signal",
//         value_parser = str::parse::<Signal>,
//         default_value = "SIGKILL"
//     )]
//     signal: Signal,
// }
//
// #[allow(clippy::similar_names)]
// pub fn main() {
//     const MAP_SIZE: usize = 65536;
//
//     let opt = Opt::parse();
//
//     let corpus_dirs: Vec<PathBuf> = [opt.in_dir].to_vec();
//
//     // The unix shmem provider supported by LibAFL for shared memory
//     let mut shmem_provider = UnixShMemProvider::new().unwrap();
//
//     // The coverage map shared between observer and executor
//     let mut shmem = shmem_provider.new_shmem(MAP_SIZE).unwrap();
//     // let the forkserver know the shmid
//     shmem.write_to_env("__AFL_SHM_ID").unwrap();
//     let shmem_buf = shmem.as_mut_slice();
//
//     // Create an observation channel using the signals map
//     let edges_observer =
//         unsafe { HitcountsMapObserver::new(StdMapObserver::new("shared_mem", shmem_buf)) };
//
//     // Create an observation channel to keep track of the execution time
//     let time_observer = TimeObserver::new("time");
//
//     // Feedback to rate the interestingness of an input
//     // This one is composed by two Feedbacks in OR
//     let mut feedback = feedback_or!(
//         // New maximization map feedback linked to the edges observer and the feedback state
//         MaxMapFeedback::new_tracking(&edges_observer, true, false),
//         // Time feedback, this one does not need a feedback state
//         TimeFeedback::with_observer(&time_observer)
//     );
//
//     // A feedback to choose if an input is a solution or not
//     // We want to do the same crash deduplication that AFL does
//     let mut objective = feedback_and_fast!(
//         // Must be a crash
//         CrashFeedback::new(),
//         // Take it only if trigger new coverage over crashes
//         // Uses `with_name` to create a different history from the `MaxMapFeedback` in `feedback` above
//         MaxMapFeedback::with_name("mapfeedback_metadata_objective", &edges_observer)
//     );
//
//     // create a State from scratch
//     let mut state = StdState::new(
//         // RNG
//         StdRand::with_seed(current_nanos()),
//         // Corpus that will be evolved, we keep it in memory for performance
//         InMemoryCorpus::<BytesInput>::new(),
//         // Corpus in which we store solutions (crashes in this example),
//         // on disk so the user can get them after stopping the fuzzer
//         OnDiskCorpus::new(PathBuf::from("./crashes")).unwrap(),
//         // States of the feedbacks.
//         // The feedbacks can report the data that should persist in the State.
//         &mut feedback,
//         // Same for objective feedbacks
//         &mut objective,
//     )
//     .unwrap();
//
//     // The Monitor trait define how the fuzzer stats are reported to the user
//     let monitor = SimpleMonitor::new(|s| println!("{s}"));
//
//     // The event manager handle the various events generated during the fuzzing loop
//     // such as the notification of the addition of a new item to the corpus
//     let mut mgr = SimpleEventManager::new(monitor);
//
//     // A minimization+queue policy to get testcasess from the corpus
//     let scheduler = IndexesLenTimeMinimizerScheduler::new(QueueScheduler::new());
//
//     // A fuzzer with feedbacks and a corpus scheduler
//     let mut fuzzer = StdFuzzer::new(scheduler, feedback, objective);
//
//     // If we should debug the child
//     let debug_child = opt.debug_child;
//
//     // Create the executor for the forkserver
//     let args = opt.arguments;
//
//     let mut tokens = Tokens::new();
//     let mut forkserver = ForkserverExecutor::builder()
//         .program(opt.executable)
//         .debug_child(debug_child)
//         .shmem_provider(&mut shmem_provider)
//         .autotokens(&mut tokens)
//         .parse_afl_cmdline(args)
//         .coverage_map_size(MAP_SIZE)
//         .build(tuple_list!(time_observer, edges_observer))
//         .unwrap();
//
//     if let Some(dynamic_map_size) = forkserver.coverage_map_size() {
//         forkserver
//             .observers_mut()
//             .match_name_mut::<HitcountsMapObserver<StdMapObserver<'_, u8, false>>>("shared_mem")
//             .unwrap()
//             .downsize_map(dynamic_map_size);
//     }
//
//     let mut executor = TimeoutForkserverExecutor::with_signal(
//         forkserver,
//         Duration::from_millis(opt.timeout),
//         opt.signal,
//     )
//     .expect("Failed to create the executor.");
//
//     // In case the corpus is empty (on first run), reset
//     if state.must_load_initial_inputs() {
//         state
//             .load_initial_inputs(&mut fuzzer, &mut executor, &mut mgr, &corpus_dirs)
//             .unwrap_or_else(|err| {
//                 panic!(
//                     "Failed to load initial corpus at {:?}: {:?}",
//                     &corpus_dirs, err
//                 )
//             });
//         println!("We imported {} inputs from disk.", state.corpus().count());
//     }
//
//     state.add_metadata(tokens);
//
//     // Setup a mutational stage with a basic bytes mutator
//     let mutator =
//         StdScheduledMutator::with_max_stack_pow(havoc_mutations().merge(tokens_mutations()), 6);
//     let mut stages = tuple_list!(StdMutationalStage::new(mutator));
//
//     fuzzer
//         .fuzz_loop(&mut stages, &mut executor, &mut state, &mut mgr)
//         .expect("Error in the fuzzing loop");
// }

use core::{time::Duration};
use std::{
    env,
    fs::{self},
    path::PathBuf,
};


use clap::{Arg, ArgAction, Command};

use libafl::{bolts::{
    current_nanos,
    rands::StdRand,
    shmem::{ShMem, ShMemProvider, UnixShMemProvider},
    tuples::{tuple_list},
    AsMutSlice,
}, corpus::{InMemoryCorpus}, executors::forkserver::{ForkserverExecutor, TimeoutForkserverExecutor}, feedback_or, feedbacks::{CrashFeedback, MaxMapFeedback, TimeFeedback}, fuzzer::{StdFuzzer}, inputs::BytesInput, observers::{HitcountsMapObserver, StdMapObserver, TimeObserver}, state::{StdState}, Error, feedback_or_fast};
use libafl::prelude::NopEventManager;
use libafl::schedulers::StdScheduler;
use nix::sys::signal::Signal;

pub fn main() {
    let res = match Command::new(env!("CARGO_PKG_NAME"))
        .version(env!("CARGO_PKG_VERSION"))
        .author("AFLplusplus team")
        .about("Lib                                            bench")
        .arg(
            Arg::new("timeout")
                .short('t')
                .long("timeout")
                .help("Timeout for each individual execution, in milliseconds")
                .default_value("1200"),
        )
        .arg(
            Arg::new("exec")
                .help("The instrumented binary we want to fuzz")
                .required(true),
        )
        .arg(
            Arg::new("debug-child")
                .short('d')
                .long("debug-child")
                .help("If not set, the child's stdout and stderror will be redirected to /dev/null")
                .action(ArgAction::SetTrue),
        )
        .arg(
            Arg::new("signal")
                .short('s')
                .long("signal")
                .help("Signal used to stop child")
                .default_value("SIGKILL"),
        )
        .arg(
            Arg::new("cmplog")
                .short('c')
                .long("cmplog")
                .help("The instrumented binary with cmplog"),
        )
        .arg(Arg::new("arguments"))
        .try_get_matches()
    {
        Ok(res) => res,
        Err(err) => {
            println!(
                "Syntax: {}, [-x dictionary] -o corpus_dir -i seed_dir\n{:?}",
                env::current_exe()
                    .unwrap_or_else(|_| "fuzzer".into())
                    .to_string_lossy(),
                err,
            );
            return;
        }
    };

    println!(
        "Workdir: {:?}",
        env::current_dir().unwrap().to_string_lossy().to_string()
    );

    // For fuzzbench, crashes and finds are inside the same `corpus` directory, in the "queue" and "crashes" subdir.
    let mut out_dir = PathBuf::from(
        res.get_one::<String>("out")
            .expect("The --output parameter is missing")
            .to_string(),
    );
    if fs::create_dir(&out_dir).is_err() {
        println!("Out dir at {:?} already exists.", &out_dir);
        if !out_dir.is_dir() {
            println!("Out dir at {:?} is not a valid directory!", &out_dir);
            return;
        }
    }
    let mut crashes = out_dir.clone();
    crashes.push("crashes");
    out_dir.push("queue");

    let in_dir = PathBuf::from(
        res.get_one::<String>("in")
            .expect("The --input parameter is missing")
            .to_string(),
    );
    if !in_dir.is_dir() {
        println!("In dir at {:?} is not a valid directory!", &in_dir);
        return;
    }

    let timeout = Duration::from_millis(
        res.get_one::<String>("timeout")
            .unwrap()
            .to_string()
            .parse()
            .expect("Could not parse timeout in milliseconds"),
    );

    let executable = res
        .get_one::<String>("exec")
        .expect("The executable is missing")
        .to_string();

    let debug_child = res.get_flag("debug-child");

    let signal = str::parse::<Signal>(
        &res.get_one::<String>("signal")
            .expect("The --signal parameter is missing")
            .to_string(),
    )
    .unwrap();

    let arguments = res
        .get_many::<String>("arguments")
        .map(|v| v.map(std::string::ToString::to_string).collect::<Vec<_>>())
        .unwrap_or_default();

    let input_file: String = res
        .get_one::<String>("input")
        .unwrap()
        .to_string()
        .parse()
        .expect("Could not parse input file");

    let input_bytes = fs::read(input_file).expect("failed to read input file");

    collect_coverage(
        BytesInput::new(input_bytes),
        timeout,
        executable,
        debug_child,
        signal,
        &arguments,
    )
    .expect("An error occurred while fuzzing");
}

/// The actual fuzzer
fn collect_coverage(
    input: BytesInput,
    timeout: Duration,
    executable: String,
    debug_child: bool,
    signal: Signal,
    arguments: &[String],
) -> Result<(), Error> {
    // a large initial map size that should be enough
    // to house all potential coverage maps for our targets
    // (we will eventually reduce the used size according to the actual map)
    const MAP_SIZE: usize = 2_621_440;

    // The unix shmem provider for shared memory, to match AFL++'s shared memory at the target side
    let mut shmem_provider = UnixShMemProvider::new().unwrap();

    // The coverage map shared between observer and executor
    let mut shmem = shmem_provider.new_shmem(MAP_SIZE).unwrap();
    // let the forkserver know the shmid
    shmem.write_to_env("__AFL_SHM_ID").unwrap();
    let shmem_buf = shmem.as_mut_slice();
    // To let know the AFL++ binary that we have a big map
    env::set_var("AFL_MAP_SIZE", format!("{}", MAP_SIZE));

    // Create an observation channel using the hitcounts map of AFL++
    let edges_observer =
        unsafe { HitcountsMapObserver::new(StdMapObserver::new("shared_mem", shmem_buf)) };

    // Create an observation channel to keep track of the execution time
    let time_observer = TimeObserver::new("time");

    // помойка
    let mut feedback = feedback_or!(
            MaxMapFeedback::new(&edges_observer),
            TimeFeedback::with_observer(&time_observer)
        );
    let mut objective = feedback_or_fast!(CrashFeedback::new());
    let sch = StdScheduler::new();

    let forkserver = ForkserverExecutor::builder()
        .program(executable)
        .debug_child(debug_child)
        .shmem_provider(&mut shmem_provider)
        .parse_afl_cmdline(arguments)
        .coverage_map_size(MAP_SIZE)
        .build_dynamic_map(edges_observer, tuple_list!(time_observer))
        .unwrap();

    // create a State from scratch
    let mut state = StdState::new(
        // RNG
        StdRand::with_seed(current_nanos()),
        // Corpus that will be evolved, we keep it in memory for performance
        InMemoryCorpus::<BytesInput>::new(),
        // Corpus in which we store solutions (crashes in this example),
        // on disk so the user can get them after stopping the fuzzer
        InMemoryCorpus::<BytesInput>::new(),
        // States of the feedbacks.
        // The feedbacks can report the data that should persist in the State.
        &mut feedback,
        // Same for objective feedbacks
        &mut objective,
    )
        .unwrap();


    let mut fuzzer = StdFuzzer::new(sch, feedback, objective);

    let mut executor = TimeoutForkserverExecutor::with_signal(forkserver, timeout, signal)
        .expect("Failed to create the executor.");

    let inp = &BytesInput::from(input);

    let res = fuzzer.execute_input(&mut state, &mut executor, &mut NopEventManager::new(),&inp);

    dbg!(res.unwrap());

    // Never reached
    Ok(())
}