// use libafl::monitors::tui::TuiMonitor;
use libafl::monitors::SimpleMonitor;
use std::{path::PathBuf};
use env_logger;
use libafl::{
    
    bolts::{
        current_nanos,
        rands::StdRand,
        tuples::tuple_list,
        AsSlice,
        launcher::Launcher,
        shmem::{ShMemProvider, StdShMemProvider},
    },
    events::{EventConfig,SimpleEventManager},
    corpus::{InMemoryCorpus, OnDiskCorpus},
    executors::{inprocess::InProcessExecutor, ExitKind, TimeoutExecutor},
    feedback_or, feedback_or_fast,
    feedbacks::{CrashFeedback, MaxMapFeedback, TimeFeedback, TimeoutFeedback},
    fuzzer::{Fuzzer, StdFuzzer},
    generators::RandPrintablesGenerator,
    inputs::{BytesInput, HasTargetBytes},
    mutators::scheduled::{havoc_mutations, StdScheduledMutator},
    observers::{StdMapObserver,TimeObserver},
    schedulers::QueueScheduler,
    stages::mutational::StdMutationalStage,
    state::StdState, prelude::Cores,
    Error,
};

/// Coverage map with explicit assignments due to the lack of instrumentation
static mut SIGNALS: [u8; 16] = [0; 16];

/// Assign a signal to the signals map
fn signals_set(idx: usize) {
    unsafe { SIGNALS[idx] = 1 };
}


#[allow(clippy::similar_names, clippy::manual_assert)]
pub fn main() {
    env_logger::init();

    log::info!("start gogo");

    let cores = Cores::from_cmdline(&"1".to_string()).expect("cores error");


    let shmem_provider = StdShMemProvider::new().expect("can't init shmem");
    

    let mon = SimpleMonitor::new(|s| println!("{s}"));
    // let mon = TuiMonitor::new(String::from("Baby Fuzzer"), false);

    // The event manager handle the various events generated during the fuzzing loop
    // such as the notification of the addition of a new item to the corpus
    let mut mgr = SimpleEventManager::new(mon.clone());



    let run_client = |state: Option<_>, mut restarting_mgr, _cores| {

        // The closure that we want to fuzz
        let mut harness = |input: &BytesInput| {
            std::thread::sleep(core::time::Duration::from_millis(100));
            let target = input.target_bytes();
            let buf = target.as_slice();
            signals_set(0);
            if !buf.is_empty() && buf[0] == b'a' {
                signals_set(1);
                if buf.len() > 1 && buf[1] == b'b' {
                    signals_set(2);
                    if buf.len() > 2 && buf[2] == b'c' {
                        println!("c");
                        signals_set(3);
                        if buf.len() > 3 && buf[3] == b'a' {
                            signals_set(4);
                            if buf.len() > 4 && buf[4] == b'a' {
                                #[cfg(unix)]
                                panic!("Artificial bug triggered =)");
                            }        
                        }
                    }
                }
            }
            ExitKind::Ok
        };


        // Create an observation channel using the signals map
        let observer =
            unsafe { StdMapObserver::from_mut_ptr("signals", SIGNALS.as_mut_ptr(), SIGNALS.len()) };

        let time_observer = TimeObserver::new("time");

        // Feedback to rate the interestingness of an input
        let mut feedback = feedback_or!(
            MaxMapFeedback::new(&observer),
            TimeFeedback::with_observer(&time_observer)
        );

        // A feedback to choose if an input is a solution or not
        let mut objective = feedback_or_fast!(CrashFeedback::new(), TimeoutFeedback::new());

        // create a State from scratch
        let mut state = state.unwrap_or_else(|| {
        StdState::new(
            // RNG
            StdRand::with_seed(current_nanos()),
            // Corpus that will be evolved, we keep it in memory for performance
            InMemoryCorpus::new(),
            // Corpus in which we store solutions (crashes in this example),
            // on disk so the user can get them after stopping the fuzzer
            OnDiskCorpus::new(PathBuf::from("./crashes")).unwrap(),
            // States of the feedbacks.
            // The feedbacks can report the data that should persist in the State.
            &mut feedback,
            // Same for objective feedbacks
            &mut objective,
        ).unwrap()
        });

        // A queue policy to get testcasess from the corpus
        let scheduler = QueueScheduler::new();

        // A fuzzer with feedbacks and a corpus scheduler
        let mut fuzzer = StdFuzzer::new(scheduler, feedback, objective);

        // Create the executor for an in-process function with just one observer
        let mut executor = TimeoutExecutor::new(
            InProcessExecutor::new(
                &mut harness,
                tuple_list!(observer, time_observer),
                &mut fuzzer,
                &mut state,
                &mut restarting_mgr,
        )?,
        std::time::Duration::from_secs(10),
        );

        let result = fuzzer.execute_input(&mut state,&mut executor, &mut mrg, &BytesInput::new(vec![1, 2, 3, 4, 5]));
        dbg!(result.unwrap());
    };

    match Launcher::builder()
    .shmem_provider(shmem_provider)
    .broker_port(1337)
    .configuration(EventConfig::from_name("default"))
    .monitor(mon.clone())
    .run_client(run_client)
    .cores(&cores)
    .spawn_broker(true)
    .build()
    .launch()
    {
            Ok(()) => (),
            Err(Error::ShuttingDown) => println!("Fuzzing stopped by user. Good bye."),
            Err(err) => panic!("Failed to run launcher: {err:?}"),
    }
}
