use std::{thread, time::Duration};
use libafl::executors;
use num_cpus;
use tokio::{net::TcpStream, sync::{oneshot, mpsc}};
use serde::{Deserialize, Serialize};
use rmp_serde;
use clap::{self, Parser};
use libafl::{bolts::{
    compress::GzipCompressor,
    current_nanos,
    rands::StdRand,
    shmem::{ShMem, ShMemProvider, UnixShMemProvider},
    tuples::tuple_list,
    AsMutSlice,
}, corpus::InMemoryCorpus, executors::{
    forkserver::ForkserverExecutor,
    HasObservers,
}, feedback_and_fast, feedback_or, feedbacks::{MaxMapFeedback, TimeFeedback}, fuzzer::StdFuzzer, inputs::BytesInput, observers::{HitcountsMapObserver, MapObserver, StdMapObserver, TimeObserver}, schedulers::{IndexesLenTimeMinimizerScheduler, QueueScheduler}, state::StdState};
use libafl::events::NopEventManager;
use nix::sys::signal::Signal;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

/// The commandline args this fuzzer accepts
#[derive(Debug, Parser)]
#[command(
    name = "forkserver_simple",
    about = "This is a simple example fuzzer to fuzz a executable instrumented by afl-cc.",
    author = "tokatoka <tokazerkje@outlook.com>"
)]
struct Opt {
    #[arg(
        help = "The instrumented binary we want to fuzz",
        name = "EXEC",
        required = true
    )]
    executable: String,

    #[arg(
        help = "Timeout for each individual execution, in milliseconds",
        short = 't',
        long = "timeout",
        default_value = "1200"
    )]
    timeout: u64,

    #[arg(
        help = "If not set, the child's stdout and stderror will be redirected to /dev/null",
        short = 'd',
        long = "debug-child",
        default_value = "false"
    )]
    debug_child: bool,

    #[arg(
        help = "Arguments passed to the target",
        name = "arguments",
        num_args(1..),
        allow_hyphen_values = true,
    )]
    arguments: Vec<String>,

    #[arg(
        help = "Signal used to stop child",
        short = 's',
        long = "signal",
        value_parser = str::parse::<Signal>,
        default_value = "SIGKILL"
    )]
    signal: Signal,
}

struct EvalTask {
    inp : Vec<BytesInput>,
    response: tokio::sync::oneshot::Sender<Vec<EvalutionData>>,
}


#[allow(clippy::similar_names)]
#[tokio::main]
async fn main() {
    let mut socket = TcpStream::connect("127.0.0.1:9090").await.unwrap();
    let compressor =  GzipCompressor::new(1024);
    let cpus = num_cpus::get_physical();
    send_tcp_msg(&mut socket, &Cores{cores: cpus}, &compressor).await.expect("failed to send cpus count to master");
    println!("connected to {:#?}", socket.peer_addr());

    // сюда лучше сделать что-то типо avg conn or max os threads
    let (producer,mut consumer) = tokio::sync::mpsc::channel::<EvalTask>(10);

    let _= thread::spawn(move ||{
        const MAP_SIZE: usize = 65536;

        let opt = Opt::parse();

        // The unix shmem provider supported by AFL++ for shared memory
        let mut shmem_provider = UnixShMemProvider::new().unwrap();

        // The coverage map shared between observer and executor
        let mut shmem = shmem_provider.new_shmem(MAP_SIZE).unwrap();
        // let the forkserver know the shmid
        shmem.write_to_env("__AFL_SHM_ID").unwrap();
        let shmem_buf = shmem.as_mut_slice();

        // Create an observation channel using the signals map
        let edges_observer =
            unsafe { HitcountsMapObserver::new(StdMapObserver::new("shared_mem", shmem_buf)) };

        // Create an observation channel to keep track of the execution time
        let time_observer = TimeObserver::new("time");

        // Feedback to rate the interestingness of an input
        // This one is composed by two Feedbacks in OR
        let mut feedback = feedback_or!(
        // New maximization map feedback linked to the edges observer and the feedback state
        MaxMapFeedback::new_tracking(&edges_observer, true, false),
        // Time feedback, this one does not need a feedback state
        TimeFeedback::with_observer(&time_observer)
    );

        // A feedback to choose if an input is a solution or not
        // We want to do the same crash deduplication that AFL does
        let mut objective = feedback_and_fast!(
        // Take it only if trigger new coverage over crashes
        // Uses `with_name` to create a different history from the `MaxMapFeedback` in `feedback` above
        MaxMapFeedback::with_name("mapfeedback_metadata_objective", &edges_observer)
    );

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

        let mut mgr = NopEventManager::new();
        // A minimization+queue policy to get testcasess from the corpus
        let scheduler = IndexesLenTimeMinimizerScheduler::new(QueueScheduler::new());
        // A fuzzer with feedbacks and a corpus scheduler
        // TODO: delete fuzzer and use only executor, its math more transparently
        let mut fuzzer = StdFuzzer::new(scheduler, feedback, objective);

        // Create the executor for the forkserver
        let args = opt.arguments;
        let mut executor = ForkserverExecutor::builder()
            .program(opt.executable)
            .shmem_provider(&mut shmem_provider)
            .parse_afl_cmdline(args)
            .coverage_map_size(MAP_SIZE)
            .build(tuple_list!(time_observer, edges_observer))
            .unwrap();

        while let Some(tasks) = consumer.blocking_recv() {
            println!("got eval task");
            let mut res_msg: Vec<EvalutionData> = Vec::new();
            res_msg.reserve(tasks.inp.len());
            
            for eval_task in tasks.inp {
                let exit_code = fuzzer.execute_input(&mut state, &mut executor, &mut mgr, &eval_task).unwrap();
                let exec_info = match exit_code{
                    executors::ExitKind::Ok => 1,
                    executors::ExitKind::Crash => 2,
                    executors::ExitKind::Oom => 4,
                    executors::ExitKind::Timeout => 8,
                    _ => unreachable!(),
                };
                let cov = executor.observers_mut().1.to_owned().0.to_vec();
                res_msg.push(EvalutionData{
                    coverage: cov,
                    exec_info: ExecutionInfo(exec_info),
                });
            }
            tasks.response.send(res_msg).unwrap();
        }
    });

    process(socket, producer, compressor).await;

    tokio::time::sleep(Duration::from_secs(20)).await;
}

async fn process(mut socket: TcpStream, msg_producer: tokio::sync::mpsc::Sender<EvalTask>, compressor: GzipCompressor) {
    loop{
        let testcases_bytes_compressed = match recv_tcp_msg(&mut socket).await {
            Ok(testcases_bytes_compressed) => testcases_bytes_compressed,
            Err(e) => {
                println!("failed to read message, close tcp stream: {}", e);
                let _= socket.shutdown().await;
                return;
            }
        };
        
        let testcases_bytes = compressor.decompress(&testcases_bytes_compressed).expect("failed to decompress testcases");

        let mut testcases: Input = match rmp_serde::from_slice(&testcases_bytes){
            Ok(testcases) => testcases,
            Err(e) => {
                println!("failed to unmarshal testcases: {}", e);
                continue;
            }
        };
        let (resp_tx, resp_rx)= tokio::sync::oneshot::channel();
        
        let inputs = testcases.testcases.iter_mut().map(|x| BytesInput::from(x.as_slice())).collect();
        let msg = EvalTask{
            inp: inputs,
            response: resp_tx,
        };
        msg_producer.send(msg).await.unwrap();

        let res= resp_rx.await.unwrap();

        send_tcp_msg(
            &mut socket, 
            &Output{
                eval_data: res,
            },
            &compressor,
        ).await.expect("failed to send results over tcp");
    }
}


async fn recv_tcp_msg(stream: &mut TcpStream) -> Result<Vec<u8>, tokio::io::Error> {
    // Always receive one be u32 of size, then the command.

    let mut size_bytes = [0_u8; 4];
    stream.read_exact(&mut size_bytes).await?;
    let size = u32::from_be_bytes(size_bytes);
    let mut bytes = vec![];
    bytes.resize(size as usize, 0_u8);

    stream.read_exact(&mut bytes).await?;

    Ok(bytes)
}

async fn send_tcp_msg<T>(stream: &mut TcpStream, msg: &T, compressor: &GzipCompressor) -> Result<(), tokio::io::Error>
where
    T: Serialize,
{
    let msg = rmp_serde::to_vec(msg).expect("failed to serialize message");
    let msg_compressed = match compressor.compress(&msg).expect("failed to compress message") {
        Some(msg_compressed) => msg_compressed,
        None => msg,
    };

    let size_bytes = (msg_compressed.len() as u32).to_be_bytes();
    stream.write_all(&size_bytes).await?;
    stream.write_all(&msg_compressed).await?;
    Ok(())
}

struct ElementStream<T> {
    stream: TcpStream,
    recv: mpsc::Receiver<T>,
    send: mpsc::Sender<T>,
} 

/// information about execution on evaler such as exit code, is asan, is msan etc.
#[repr(transparent)]
#[derive(
    Debug, Default, Copy, Clone, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize,
)]
pub struct ExecutionInfo(pub u64);

 /// Send code coverage from evaler to master
 #[derive(Serialize, Deserialize, Debug, Clone)]
 struct EvalutionData {
    /// execution information such as exit code or is
    exec_info: ExecutionInfo,
    /// code coverage collected from evaler
    coverage: Vec<u8>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct Testcase {
    input: Vec<u8>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct Input {
    testcases :Vec<Vec<u8>>
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct Output {
    eval_data: Vec<EvalutionData>
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct Cores{
    pub cores: usize
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct MasterMessage {
    pub on_node_id: u16,
    pub payload: Vec<u8>,
}