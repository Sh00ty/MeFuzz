//! A very simple event manager, that just supports log outputs, but no multiprocessing

use alloc::{
    boxed::Box,
    string::{String, ToString},
    vec::Vec,
};

use core::{marker::PhantomData, fmt::Debug};
use tokio;
use rmp_serde;
use super::{CustomBufEventResult, CustomBufHandlerFn, HasCustomBufHandlers, ProgressReporter};
use crate::bolts::compress::GzipCompressor;
#[cfg(all(feature = "std", any(windows, not(feature = "fork"))))]
use crate::bolts::os::startable_self;
use crate::{
    bolts::ClientId,
    events::{
        BrokerEventResult, Event, EventFirer, EventManager, EventManagerId, EventProcessor,
        EventRestarter, HasEventManagerId,
    },
    inputs::UsesInput,
    monitors::Monitor,
    state::{HasClientPerfMonitor, HasExecutions, HasMetadata, UsesState},
    Error,
};
use crate::bolts::llmp;

/// The llmp connection from the actual fuzzer to the process supervising it
const _ENV_FUZZER_SENDER: &str = "_AFL_ENV_FUZZER_SENDER";
const _ENV_FUZZER_RECEIVER: &str = "_AFL_ENV_FUZZER_RECEIVER";
/// The llmp (2 way) connection from a fuzzer to the broker (broadcasting all other fuzzer messages)
const _ENV_FUZZER_BROKER_CLIENT_INITIAL: &str = "_AFL_ENV_FUZZER_BROKER_CLIENT";

/// A simple, single-threaded event manager that just logs
pub struct MasterEventManager<MT, S>
where
    S: UsesInput,
{
    /// compressor for messages
    compressor: GzipCompressor,
    /// client_id
    client_id: ClientId,
    /// recv получить, сообщения от мастера
    //recv: tokio::sync::mpsc::Receiver<Vec<u8>>,
    /// sender отправить события мастеру
    sender: tokio::sync::mpsc::Sender<llmp::TcpMasterMessage>,
    /// The monitor
    monitor: MT,
    /// The events that happened since the last handle_in_broker
    events: Vec<Event<S::Input>>,
    /// The custom buf handler
    custom_buf_handlers: Vec<Box<CustomBufHandlerFn<S>>>,
    phantom: PhantomData<S>,
}

impl<MT, S> Debug for MasterEventManager<MT, S>
where
    MT: Debug,
    S: UsesInput,
{
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        f.debug_struct("MasterEventManager")
            //.field("custom_buf_handlers", self.custom_buf_handlers)
            .field("monitor", &self.monitor)
            .field("events", &self.events)
            .field("client_id", &self.client_id)
            .finish_non_exhaustive()
    }
}

impl<MT, S> UsesState for MasterEventManager<MT, S>
where
    S: UsesInput,
{
    type State = S;
}

impl<MT, S> EventFirer for MasterEventManager<MT, S>
where
    MT: Monitor,
    S: UsesInput,
{
    fn fire(
        &mut self,
        _state: &mut Self::State,
        event: Event<<Self::State as UsesInput>::Input>,
    ) -> Result<(), Error> {
        match Self::handle_in_broker(&mut self.monitor, &event)? {
            BrokerEventResult::Forward => self.events.push(event),
            BrokerEventResult::ForwardToMaster =>  {
                let event_bytes = rmp_serde::to_vec(&event)?;
                let compressed = self.compressor.compress(&event_bytes)?;
                let mut msg = llmp::TcpMasterMessage{
                    client_id: self.client_id,
                    flags: llmp::LLMP_FLAG_B2M | llmp::LLMP_FLAG_NEW_TEST_CASE,
                    payload: event_bytes,
                };
                match compressed{
                    Some(c) => {
                        msg.flags = msg.flags | llmp::LLMP_FLAG_COMPRESSED;
                        msg.payload = c;
                    },
                    _ =>{}
                };
                match self.sender.blocking_send(msg){
                    Ok(()) => {},
                    Err(e) => {
                        log::error!("B2M: can't send message to master {}", e);
                    },
                };
            },
            BrokerEventResult::ForwardBoth => self.events.push(event),
            BrokerEventResult::Handled => (),
        };
        Ok(())
    }
}

impl<MT, S> EventRestarter for MasterEventManager<MT, S>
where
    MT: Monitor,
    S: UsesInput,
{
}

impl<E, MT, S, Z> EventProcessor<E, Z> for MasterEventManager<MT, S>
where
    MT: Monitor,
    S: UsesInput,
{
    fn process(
        &mut self,
        _fuzzer: &mut Z,
        state: &mut S,
        _executor: &mut E,
    ) -> Result<usize, Error> {
        let count = self.events.len();
        while !self.events.is_empty() {
            let event = self.events.pop().unwrap();
            self.handle_in_client(state, event)?;
        }
        Ok(count)
    }
}

impl<E, MT, S, Z> EventManager<E, Z> for MasterEventManager<MT, S>
where
    MT: Monitor,
    S: UsesInput + HasClientPerfMonitor + HasExecutions + HasMetadata,
{
}

impl<MT, S> HasCustomBufHandlers for MasterEventManager<MT, S>
where
    MT: Monitor, //CE: CustomEvent<I, OT>,
    S: UsesInput,
{
    /// Adds a custom buffer handler that will run for each incoming `CustomBuf` event.
    fn add_custom_buf_handler(
        &mut self,
        handler: Box<
            dyn FnMut(&mut Self::State, &String, &[u8]) -> Result<CustomBufEventResult, Error>,
        >,
    ) {
        self.custom_buf_handlers.push(handler);
    }
}

impl<MT, S> ProgressReporter for MasterEventManager<MT, S>
where
    MT: Monitor,
    S: UsesInput + HasExecutions + HasClientPerfMonitor + HasMetadata,
{
}

impl<MT, S> HasEventManagerId for MasterEventManager<MT, S>
where
    MT: Monitor,
    S: UsesInput,
{
    fn mgr_id(&self) -> EventManagerId {
        EventManagerId(0)
    }
}

impl<MT, S> MasterEventManager<MT, S>
where
    MT: Monitor, //TODO CE: CustomEvent,
    S: UsesInput,
{
    /// Creates a new [`MasterEventManager`].
    pub fn new(
        monitor: MT,
        compressor: GzipCompressor,
        client_id: ClientId,
        //recv: tokio::sync::mpsc::Receiver<Vec<u8>>,
        sender: tokio::sync::mpsc::Sender<llmp::TcpMasterMessage>,
    ) -> Self {
        Self {
            sender,
            //recv,
            client_id,
            compressor,
            monitor,
            events: vec![],
            custom_buf_handlers: vec![],
            phantom: PhantomData,
        }
    }

    /// Handle arriving events in the broker
    #[allow(clippy::unnecessary_wraps)]
    fn handle_in_broker(
        monitor: &mut MT,
        event: &Event<S::Input>,
    ) -> Result<BrokerEventResult, Error> {
        match event {
            Event::NewTestcase {
                input: _,
                client_config: _,
                exit_kind: _,
                corpus_size,
                observers_buf: _,
                time,
                executions,
            } => {
                monitor
                    .client_stats_mut_for(ClientId(0))
                    .update_corpus_size(*corpus_size as u64);
                monitor
                    .client_stats_mut_for(ClientId(0))
                    .update_executions(*executions as u64, *time);
                monitor.display(event.name().to_string(), ClientId(0));
                Ok(BrokerEventResult::ForwardToMaster)
            }
            Event::UpdateExecStats {
                time,
                executions,
                phantom: _,
            } => {
                // TODO: The monitor buffer should be added on client add.
                let client = monitor.client_stats_mut_for(ClientId(0));

                client.update_executions(*executions as u64, *time);

                monitor.display(event.name().to_string(), ClientId(0));
                Ok(BrokerEventResult::Handled)
            }
            Event::UpdateUserStats {
                name,
                value,
                phantom: _,
            } => {
                monitor
                    .client_stats_mut_for(ClientId(0))
                    .update_user_stats(name.clone(), value.clone());
                monitor.display(event.name().to_string(), ClientId(0));
                Ok(BrokerEventResult::Handled)
            }
            #[cfg(feature = "introspection")]
            Event::UpdatePerfMonitor {
                time,
                executions,
                introspection_monitor,
                phantom: _,
            } => {
                // TODO: The monitor buffer should be added on client add.
                let client = monitor.client_stats_mut_for(ClientId(0));
                client.update_executions(*executions as u64, *time);
                client.update_introspection_monitor((**introspection_monitor).clone());
                monitor.display(event.name().to_string(), ClientId(0));
                Ok(BrokerEventResult::Handled)
            }
            Event::Objective { objective_size } => {
                monitor
                    .client_stats_mut_for(ClientId(0))
                    .update_objective_size(*objective_size as u64);
                monitor.display(event.name().to_string(), ClientId(0));
                Ok(BrokerEventResult::Handled)
            }
            Event::Log {
                severity_level,
                message,
                phantom: _,
            } => {
                let (_, _) = (message, severity_level);
                log::log!((*severity_level).into(), "{message}");
                Ok(BrokerEventResult::Handled)
            }
            Event::CustomBuf { .. } => Ok(BrokerEventResult::Forward),
            //_ => Ok(BrokerEventResult::Forward),
        }
    }

    // Handle arriving events in the client
    #[allow(clippy::needless_pass_by_value, clippy::unused_self)]
    fn handle_in_client(&mut self, state: &mut S, event: Event<S::Input>) -> Result<(), Error> {
        if let Event::CustomBuf { tag, buf } = &event {
            for handler in &mut self.custom_buf_handlers {
                handler(state, tag, buf)?;
            }
            Ok(())
        } else {
            Err(Error::unknown(format!(
                "Received illegal message that message should not have arrived: {event:?}."
            )))
        }
    }
}
