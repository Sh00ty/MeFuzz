package fuzzer

import (
	"context"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/conn/tcp"
	"orchestration/infra/utils/hashing"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"os"
	"time"

	"github.com/pkg/errors"
)

type Fuzzer struct {
	perfChan chan master.Event
}

func (f Fuzzer) OnConnect(ctx context.Context, conn tcp.Connection, recvMsgChan chan entities.Testcase) error {
	// получаем приветсвие от брокера (он всем его отправляет)
	brokerGreetBytes, err := conn.RecvMessage()
	if err != nil {
		return errors.WithMessage(err, "can't read broker hello")
	}
	brokerGreetMsg := brokerConnectHello{}
	if err = msgpack.UnmarshalEnum(brokerGreetBytes, &brokerGreetMsg); err != nil {
		return errors.WithMessage(err, "can't unmarshal broker hello")
	}
	logger.Infof("M2B greeting from %s", brokerGreetMsg.Hostname)

	masterHost, err := os.Hostname()
	if err != nil {
		logger.Debug("error while getting master hostname, pasted 'external_host' insted")
		masterHost = "external_host"
	}

	// отправляем брокеру конфигурацию
	masterHello := masterNodeHello{
		MasterHostname: masterHost,
		// для брокера наоборот будет
		// мы отправляем по sendlimit а он принимает по recv limit
		RecvLimit: tcp.SendLimit,
		SendLimit: tcp.RecvLimit,
		BrokerID:  uint32(conn.NodeID),
	}
	// создаем конвертер из наших структур в msgpack
	converter := msgpack.New()
	mhBytes, err := converter.MarshalEnum(masterHello)
	if err != nil {
		return errors.WithMessage(err, "failed to marshal master node hello message")
	}
	if err = conn.SendMessage(mhBytes); err != nil {
		return errors.WithMessage(err, "failed to send master node hello message")
	}

	// смотрим принял ли брокер наши настройки (он в ответ отправляем id который мы ему дали)
	masterAcceptedBytes, err := conn.RecvMessage()
	if err != nil {
		return errors.WithMessage(err, "failed to read master accepted message")
	}
	ma := masterAccepted{}
	if err = msgpack.UnmarshalEnum(masterAcceptedBytes, &ma); err != nil {
		return errors.WithMessage(err, "failed to unmarshal master accepted message")
	}
	if entities.NodeID(ma.BrokerID) != conn.NodeID {
		return errors.Errorf("got trash message: [ma.BrokerID != srv.conns] (%d!=%d)", ma.BrokerID, conn.NodeID)
	}

	for i, conf := range ma.FuzzerConfigurations {
		f.perfChan <- master.Event{
			Event: master.NewElement,
			Element: entities.ElementID{
				// TODO: это вообще верный факт???
				NodeID:   conn.NodeID,
				OnNodeID: entities.OnNodeID(i + 1),
			},
			NodeType: entities.Broker,
			Payload: entities.FuzzerConf{
				MutatorID:  entities.MutatorID(conf.MutatorID),
				ScheduleID: entities.ScheduleID(conf.SchedulerID),
			},
		}
	}

	return nil
}
func (f Fuzzer) Handler(ctx context.Context, recvMsgChan chan entities.Testcase, msg tcp.HandlerMessage) error {

	switch {
	case msg.Flag.Has(entities.NewTestCase):
		tcTmp := newTestcase{}
		if err := msgpack.UnmarshalEnum(msg.Payload, &tcTmp); err != nil {
			return errors.Wrapf(err, "failed to umnmarshal newTestCase; msg=%v", msg)
		}
		inputData := msgpack.CovertTo[int, byte](tcTmp.Input.Input)
		tc := entities.Testcase{
			ID: hashing.MakeHash(inputData),
			FuzzerID: entities.ElementID{
				NodeID:   msg.NodeID,
				OnNodeID: msg.ClientID,
			},
			InputData:  inputData,
			Execs:      tcTmp.Executions,
			CorpusSize: tcTmp.CorpusSize,
			CreatedAt:  time.Unix(int64(tcTmp.Timestamp.Secs), int64(tcTmp.Timestamp.Nsec)).In(time.Local),
		}

		select {
		case recvMsgChan <- tc:
		default:
			f.perfChan <- master.Event{
				Event:    master.NeedMoreEvalers,
				NodeType: entities.Broker,
			}
			select {
			case recvMsgChan <- tc:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}
