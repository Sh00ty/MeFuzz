use std::{thread::{self}, time::Duration, ops::{BitOr, BitAnd}, path::PathBuf, str::FromStr, borrow::Borrow};
use libafl::{executors, bolts::{llmp::{Flags, LLMP_FLAG_COMPRESSED, LLMP_FLAG_B2M, LLMP_FLAG_EVALUATION, self}, os::{fork, self}, compress}, FuzzerConfiguration, events::master::MasterEventManager, monitors::{NopMonitor, SimpleMonitor}};
use num_cpus;
use tokio::{net::TcpStream, sync::{oneshot, mpsc}, time::sleep};
use serde::{Deserialize, Serialize};
use rmp_serde;
use clap::{self, Parser};
use libafl::{bolts::{
    ClientId,
    compress::GzipCompressor,
    current_nanos,
    rands::StdRand,
    shmem::{ShMem, ShMemProvider, UnixShMemProvider},
    tuples::{tuple_list, MatchName, Merge},
    AsMutSlice,
}, corpus::{Corpus, InMemoryCorpus, OnDiskCorpus}, executors::{
    forkserver::{TimeoutForkserverExecutor, ForkserverExecutor},
    HasObservers,
},feedback_and_fast, feedback_or,stages::mutational::StdMutationalStage, feedbacks::{CrashFeedback, MaxMapFeedback, TimeFeedback}, fuzzer::{StdFuzzer, Fuzzer}, inputs::BytesInput, observers::{HitcountsMapObserver, MapObserver, StdMapObserver, TimeObserver}, schedulers::{IndexesLenTimeMinimizerScheduler, QueueScheduler}, state::{HasCorpus, HasMetadata, StdState}, mutators::{scheduled::havoc_mutations, tokens_mutations, StdScheduledMutator, Tokens}};
use libafl::events::NopEventManager;
use nix::{sys::signal::Signal, libc::time};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use std::collections::HashMap;
use libfuzzer_libpng;

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
        help = "The directory to read initial inputs from ('seeds')",
        name = "INPUT_DIR",
        required = true
    )]
    in_dir: PathBuf,

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

    #[arg(
        help = "Manual role selection(fuzz/eval)",
        short = 'r',
        long = "role",
        default_value = ""
    )]
    manual_role: String,
}

struct EvalTask {
    inp : Vec<BytesInput>,
    response: tokio::sync::oneshot::Sender<Vec<EvalutionData>>,
}

const GZIP_THRESHOLD:usize= 1024;
const FUZZER: i64 = 2;
const EVALER: i64 = 3;

#[allow(clippy::similar_names)]
#[tokio::main]
async fn main() {
    let opt = Opt::parse();

    let mut stream = TcpStream::connect("127.0.0.1:9990").await.unwrap();
    
    let init_msg = InitMsg{
        cores:num_cpus::get_physical(),
        manual_role: opt.manual_role,
    };
    let init_msg_bytes = rmp_serde::to_vec(&init_msg).expect("failed to serialize cpus");
    send_tcp_msg(&mut stream, init_msg_bytes).await.expect("failed to send cpus count to master");

    let conf_msg_bytes = recv_tcp_msg(&mut stream).await.expect("failed to recv configuration");
    println!("connected to {:#?}", stream.peer_addr());
    
    let conf:NodeConfiguration = rmp_serde::from_slice(&conf_msg_bytes).expect("failed to deserialize node configuration");
    println!("got configurations {:#?}", conf);

    let mut multeplex_tx_map: HashMap<ClientId, mpsc::Sender<Vec<u8>>> = HashMap::new();
    let mut multeplex_rx_map: HashMap<ClientId, mpsc::Receiver<Vec<u8>>> = HashMap::new();
    for i in 0..conf.elements.len() {
        let (tx, rx) = mpsc::channel(100);
        multeplex_tx_map.insert(ClientId(conf.elements[i].on_node_id), tx);
        multeplex_rx_map.insert(ClientId(conf.elements[i].on_node_id), rx);
    }

    let (rstream,mut  wstream) = stream.into_split();
    let multiplexer = Multiplexer::new(rstream, multeplex_tx_map);
    
    tokio::spawn(async move {
        multiplexer.handle_connection(GzipCompressor::new(GZIP_THRESHOLD)).await;
    });

    let (sendtx, mut sendrx) = mpsc::channel::<llmp::TcpMasterMessage>(100);

    tokio::spawn(async move {
        loop {
            let compressor = GzipCompressor::new(GZIP_THRESHOLD);
            let msg = sendrx.recv().await.unwrap();
            
            match Multiplexer::send_msg(&mut wstream, &compressor, msg.payload, msg.client_id.0, msg.flags).await {
                Ok(_) => {},
                Err(e) => {
                    println!("failed to send multiplexed message {}", e);
                }
            };
        }
    });

    for el in conf.elements.into_iter(){
        let rx = multeplex_rx_map.remove(&ClientId(el.on_node_id)).unwrap();
        let on_node_id = el.on_node_id;
        let es = ElementStream{
            recv: rx,
            stream: sendtx.clone(),
        };
        
        match el.kind {
            FUZZER => {
                // TODO: возможно тут нужен чистый поток
                thread::spawn(move|| {
                    start_fuzzer(es, on_node_id, el.fuzzer_configuration);
                });
            }
            EVALER => {
                tokio::spawn(start_evaler(es, on_node_id));
            },
            _ => {},
        }
    }
    sleep(Duration::from_secs(1200)).await;
}

fn start_fuzzer(stream: ElementStream, client_id: u32, _conf: Option<FuzzerConfiguration>) {
    
    // if !conf.is_none() {
    //     let _ = libfuzzer_libpng::fuzz(
    //         &[PathBuf::from("../../libfuzzer_libpng/corpus/")],
    //         PathBuf::from("../../libfuzzer_libpng/corpus/crashes"),
    //         vec![String::from("abc"), String::from("../../libfuzzer_libpng/fuzzer_libpng")],
    //         ClientId(client_id), 
    //         stream.stream,
    //     );
    //     return;
    // }

    const MAP_SIZE: usize = 65536;

    let opt = Opt::parse();
    let corpus_dirs: Vec<PathBuf> = [opt.in_dir].to_vec();

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
        // Must be a crash
        CrashFeedback::new(),
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

    // The Monitor trait define how the fuzzer stats are reported to the user
    let monitor = SimpleMonitor::new(|s| println!("{s}"));

    // The event manager handle the various events generated during the fuzzing loop
    // such as the notification of the addition of a new item to the corpus
    let mut mgr = MasterEventManager::new(
        monitor, 
        compress::GzipCompressor::new(GZIP_THRESHOLD), 
        ClientId(client_id), 
        stream.stream, 
    );

    // A minimization+queue policy to get testcasess from the corpus
    let scheduler = IndexesLenTimeMinimizerScheduler::new(QueueScheduler::new());

    // A fuzzer with feedbacks and a corpus scheduler
    let mut fuzzer = StdFuzzer::new(scheduler, feedback, objective);
    
    // If we should debug the child
    let debug_child = opt.debug_child;

    // Create the executor for the forkserver
    let args = opt.arguments;

    let mut tokens = Tokens::new();
    let mut forkserver = ForkserverExecutor::builder()
        .program(opt.executable)
        .debug_child(debug_child)
        .shmem_provider(&mut shmem_provider)
        .autotokens(&mut tokens)
        .parse_afl_cmdline(args)
        .coverage_map_size(MAP_SIZE)
        .build(tuple_list!(time_observer, edges_observer))
        .unwrap();

    let mut executor = TimeoutForkserverExecutor::with_signal(
        forkserver,
        Duration::from_millis(opt.timeout),
        opt.signal,
    )
    .expect("Failed to create the executor.");
    
    state.load_initial_inputs(&mut fuzzer,&mut executor, &mut mgr, &corpus_dirs).expect("Failed to load initial inputs");

    state.add_metadata(tokens);

    // Setup a mutational stage with a basic bytes mutator
    let mutator = StdScheduledMutator::with_max_stack_pow(havoc_mutations().merge(tokens_mutations()), 6);
    let mut stages = tuple_list!(StdMutationalStage::new(mutator));
    
    fuzzer
        .fuzz_loop(&mut stages, &mut executor, &mut state, &mut mgr)
        .expect("Error in the fuzzing loop");
}

async fn start_evaler(mut stream: ElementStream, client_id: u32) {
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

    loop{
        let testcases_bytes = match stream.recv.recv().await{
            Some(msg) => msg,
            None => continue,
        };

        let mut testcases: Input = match rmp_serde::from_slice(&testcases_bytes){
            Ok(testcases) => testcases,
            Err(e) => {
                println!("failed to unmarshal testcases: {}", e);
                continue;
            }
        };
        let (resp_tx, resp_rx)= oneshot::channel();
        
        let inputs = testcases.testcases.iter_mut().map(|x| BytesInput::from(x.as_slice())).collect();
        let msg = EvalTask{
            inp: inputs,
            response: resp_tx,
        };
        producer.send(msg).await.unwrap();

        let res= resp_rx.await.unwrap();
        let res_bytes = rmp_serde::to_vec(&EvaluationOutput{eval_data: res}).unwrap();
        
        stream.stream.send(llmp::TcpMasterMessage{
            payload: res_bytes,
            flags: LLMP_FLAG_EVALUATION,
            client_id: ClientId(client_id),
        }).await.expect("failed to send message to channel in evaler");
        println!("send testcases coverage");
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

async fn send_tcp_msg(stream: &mut TcpStream, msg: Vec<u8>) -> Result<(), tokio::io::Error> {
    let size_bytes = (msg.len() as u32).to_be_bytes();
    stream.write_all(&size_bytes).await?;
    stream.write_all(&msg).await?;
    Ok(())
}


struct ElementStream {
    stream: mpsc::Sender<llmp::TcpMasterMessage>,
    recv: mpsc::Receiver<Vec<u8>>,
}

struct Multiplexer {
    conn: tokio::net::tcp::OwnedReadHalf,
    streams: HashMap<ClientId, mpsc::Sender<Vec<u8>>>,
}

impl Multiplexer {  
    pub fn new(conn: tokio::net::tcp::OwnedReadHalf, streams: HashMap<ClientId, mpsc::Sender<Vec<u8>>>) -> Multiplexer {
        return Multiplexer{
            conn,
            streams,
        }
    }

    async fn recv_tcp_msg(stream: &mut tokio::net::tcp::OwnedReadHalf) -> Result<Vec<u8>, tokio::io::Error> {
        // Always receive one be u32 of size, then the command.
    
        let mut size_bytes = [0_u8; 4];
        stream.read_exact(&mut size_bytes).await?;
        let size = u32::from_be_bytes(size_bytes);
        let mut bytes = vec![];
        bytes.resize(size as usize, 0_u8);
    
        stream.read_exact(&mut bytes).await?;
    
        Ok(bytes)
    }

    async fn send_tcp_msg(stream: &mut tokio::net::tcp::OwnedWriteHalf, msg: Vec<u8>) -> Result<(), tokio::io::Error> {
        let size_bytes = (msg.len() as u32).to_be_bytes();
        stream.write_all(&size_bytes).await?;
        stream.write_all(&msg).await?;
        Ok(())
    }

    pub async fn send_msg(stream: &mut tokio::net::tcp::OwnedWriteHalf, compressor: &GzipCompressor, msg: Vec<u8>, on_node_id: u32, flags:Flags) -> Result<(), tokio::io::Error>{

        let mut flags = flags.bitor(LLMP_FLAG_B2M);
        let mut compressed_msg = msg;
        if flags & LLMP_FLAG_COMPRESSED != LLMP_FLAG_COMPRESSED {
            compressed_msg = match compressor.compress(&compressed_msg) {
                Ok(compressed) => {
                    match compressed {
                        Some(compressed) => {
                            flags = flags.bitor(LLMP_FLAG_COMPRESSED);
                            compressed
                        }
                        None => compressed_msg,
                    }
                }
                Err(e) => {
                    println!("failed to compress message: {}", e);
                    compressed_msg
                }
            };
        }

        let master_msg = llmp::TcpMasterMessage{
            client_id: ClientId(on_node_id),
            flags,
            payload: compressed_msg,
        };
        let msg = rmp_serde::to_vec(&master_msg).expect("failed to serialize message");
        
        return Multiplexer::send_tcp_msg(stream, msg).await;
    }   

    pub async fn handle_connection(mut self, compressor: GzipCompressor) {
        loop {
            let msg = match Multiplexer::recv_tcp_msg(&mut self.conn).await{
                Ok(msg) => msg,
                Err(e) => {
                    println!("failed to read message, close tcp stream: {}", e);
                    return;
                }
            };
            let mut master_msg: llmp::TcpMasterMessage = match rmp_serde::from_slice(&msg){
                Ok(testcases) => testcases,
                Err(e) => {
                    println!("failed to unmarshal tcp master message: {}", e);
                    continue;
                }
            };

            if master_msg.flags.bitand(LLMP_FLAG_COMPRESSED) == LLMP_FLAG_COMPRESSED {
                match compressor.decompress(&master_msg.payload) {
                    Ok(decompressed) => {
                        master_msg.payload = decompressed
                    }
                    Err(e) => {
                        print!("failed to decompress message: {}", e);
                        continue;
                    }
                }
            }
            let ch = match self.streams.get(&master_msg.client_id){
                Some(ch) => ch,
                None => {
                    println!("not found client with client_id {:#?}", master_msg.client_id);
                    continue;
                }
            };
            if ch.is_closed() {
                println!("chan with client_id {:#?} is closed", master_msg.client_id);
                continue;
            }
            match ch.send(master_msg.payload).await {
                Err(e) => {
                    println!("failed to send message to channel: {}", e);
                }
                _ => {println!("send message to dst {:#?}", master_msg.client_id)}
            }
        }
    }
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
struct EvaluationOutput {
    eval_data: Vec<EvalutionData>
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct InitMsg{
    pub cores: usize,
    pub manual_role: String,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct NodeConfiguration {
    pub elements: Vec<Element>
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct Element {
    pub on_node_id: u32,
    pub kind: i64,
    #[serde(skip_serializing_if="Option::is_none")]
    pub fuzzer_configuration: Option<FuzzerConfiguration>,
}