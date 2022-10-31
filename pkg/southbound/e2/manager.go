// Created by RIMEDO-Labs team
// based on onosproject/onos-mho/pkg/southbound/e2/manager.go

package e2

import (
	"context"
	"strings"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/rnib"
	prototypes "github.com/gogo/protobuf/types"
	e2api "github.com/onosproject/onos-api/go/onos/e2t/e2/v1beta1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-mho/pkg/broker"
	"github.com/onosproject/onos-mho/pkg/store"
	"github.com/onosproject/onos-mho/pkg/utils/control"
	"github.com/onosproject/onos-mho/pkg/utils/subscription"
	e2client "github.com/onosproject/onos-ric-sdk-go/pkg/e2/v1beta1"
)

var log = logging.GetLogger("rimedo-ts", "e2", "manager")

const (
	oid = "1.3.6.1.4.1.53148.1.1.2.3"
)

type Options struct {
	AppID       string
	E2tAddress  string
	E2tPort     int
	TopoAddress string
	TopoPort    int
	SMName      string
	SMVersion   string
}

func NewManager(options Options, metricStore store.Store, nodeManager *monitoring.NodeManager, flag bool) (Manager, error) {

	smName := e2client.ServiceModelName(options.SMName)
	smVer := e2client.ServiceModelVersion(options.SMVersion)
	appID := e2client.AppID(options.AppID)
	e2Client := e2client.NewClient(
		e2client.WithAppID(appID),
		e2client.WithServiceModel(smName, smVer),
		e2client.WithE2TAddress(options.E2tAddress, options.E2tPort),
	)

	rnibOptions := rnib.Options{
		TopoAddress: options.TopoAddress,
		TopoPort:    options.TopoPort,
	}

	rnibClient, err := rnib.NewClient(rnibOptions)
	if err != nil {
		return Manager{}, err
	}

	return Manager{
		e2client:       e2Client,
		rnibClient:     rnibClient,
		streams:        broker.NewBroker(),
		smModelName:    smName,
		metricStore:    metricStore,
		nodeManager:    nodeManager,
		topoIDsEnabled: flag,
	}, nil
}

type Manager struct {
	e2client       e2client.Client
	rnibClient     rnib.Client
	streams        broker.Broker
	smModelName    e2client.ServiceModelName
	metricStore    store.Store
	nodeManager    *monitoring.NodeManager
	topoIDsEnabled bool
}

func (m *Manager) Start() error {
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := m.watchE2Connections(ctx)
		if err != nil {
			return
		}
	}()

	return nil
}

func (m *Manager) watchE2Connections(ctx context.Context) error {
	ch := make(chan topoapi.Event)
	err := m.rnibClient.WatchE2Connections(ctx, ch)
	if err != nil {
		log.Warn(err)
		return err
	}

	for topoEvent := range ch {
		if topoEvent.Type == topoapi.EventType_ADDED || topoEvent.Type == topoapi.EventType_NONE {
			relation := topoEvent.Object.Obj.(*topoapi.Object_Relation)
			e2NodeID := relation.Relation.TgtEntityID

			if !m.rnibClient.HasRcRANFunction(ctx, e2NodeID, oid) {
				log.Debugf("Received topo event does not have RC RAN function for RIMEDO TS xApp - %v", topoEvent)
				continue
			}

			go func() {
				log.Debugf("start creating subscription %v", topoEvent)
				err := m.createSubscription(ctx, e2NodeID)
				if err != nil {
					log.Warn(err)
				}
			}()

			// m.ctrlReqChs[string(e2NodeID)] = make(chan *e2api.ControlMessage)

			// triggers := make(map[e2sm_mho.MhoTriggerType]bool)
			// triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_PERIODIC] = true
			// triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_UPON_RCV_MEAS_REPORT] = true
			// triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_UPON_CHANGE_RRC_STATUS] = true

			// for triggerType, enabled := range triggers {
			// 	if enabled {
			// 		go func(triggerType e2sm_mho.MhoTriggerType) {
			// 			_ = m.createSubscription(ctx, e2NodeID, triggerType)
			// 		}(triggerType)
			// 	}
			// }
			go m.watchMHOChanges(ctx, e2NodeID)
		} else if topoEvent.Type == topoapi.EventType_REMOVED {
			// TODO - Handle E2 node disconnect
			relation := topoEvent.Object.Obj.(*topoapi.Object_Relation)
			e2NodeID := relation.Relation.TgtEntityID
			if !m.rnibClient.HasRcRANFunction(ctx, e2NodeID, oid) {
				log.Debugf("Received topo event does not have RC RAN function for MHO - %v", topoEvent)
				continue
			}
			cellIDs, err := m.rnibClient.GetCells(ctx, e2NodeID)
			if err != nil {
				return err
			}
			for _, cellID := range cellIDs {
				log.Debugf("cell removed, e2NodeID:%v, cellID:%v", e2NodeID, cellID.CellGlobalID.GetValue())
			}
		}
	}

	return nil
}

func (m *Manager) watchMHOChanges(ctx context.Context, e2nodeID topoapi.ID) {
	ch := make(chan store.Event)
	err := m.metricStore.Watch(ctx, ch)
	if err != nil {
		log.Error(err)
		return
	}

	for e := range ch {
		// keyx := e.Key.(string)
		// vx, errx := m.metricStore.Get(ctx, keyx)
		// if errx != nil {
		// 	log.Error(errx)
		// }
		// nvx := vx.Value.(*store.MetricValue)
		// rawUEIDx := nvx.RawUEID
		// if rawUEIDx != nil && rawUEIDx.GetEnGNbUeid() != nil && rawUEIDx.GetGNbUeid() != nil {
		// 	log.Debug("")
		// 	log.Debug("Ueid: ", rawUEIDx.GetEnGNbUeid().RanUeid)
		// 	log.Debug("GlobalGnbId: ", rawUEIDx.GetGNbUeid().GlobalGnbId)
		// 	log.Debug("")
		// }

		if e.Type == store.Updated {
			key := e.Key.(string)
			v, err := m.metricStore.Get(ctx, key)
			if err != nil {
				log.Error(err)
			}
			nv := v.Value.(*store.MetricValue)
			if e.EventMHOState.(store.MHOState) == store.Approved && nv.E2NodeID == e2nodeID {
				rawUEID := nv.RawUEID
				// log.Debug("")
				// log.Debug("Ueid: ", rawUEID.GetEnGNbUeid().RanUeid)
				// log.Debug("GlobalGnbId: ", rawUEID.GetGNbUeid().GlobalGnbId)
				// log.Debug("")

				// if rawUEID.GetENbUeid() != nil {
				// 	log.Debug("GetENbUeid:")
				// 	log.Debug("GUmmei: ", rawUEID.GetENbUeid().GUmmei)
				// 	log.Debug("GlobalEnbId: ", rawUEID.GetENbUeid().GlobalEnbId)
				// 	log.Debug("MENbUeX2ApId: ", rawUEID.GetENbUeid().MENbUeX2ApId)
				// 	log.Debug("MENbUeX2ApIdExtension: ", rawUEID.GetENbUeid().MENbUeX2ApIdExtension)
				// 	log.Debug("MMeUeS1ApId: ", rawUEID.GetENbUeid().MMeUeS1ApId)
				// } else {
				// 	log.Debug("GetENbUeid: nil")
				// }

				// if rawUEID.GetEnGNbUeid() != nil {
				// 	log.Debug("GetEnGNbUeid:")
				// 	log.Debug("GNbCuCpUeE1ApIdList: ", rawUEID.GetEnGNbUeid().GNbCuCpUeE1ApIdList)
				// 	log.Debug("GNbCuUeF1ApId: ", rawUEID.GetEnGNbUeid().GNbCuUeF1ApId)
				// 	log.Debug("GlobalEnbId: ", rawUEID.GetEnGNbUeid().GlobalEnbId)
				// 	log.Debug("MENbUeX2ApId: ", rawUEID.GetEnGNbUeid().MENbUeX2ApId)
				// 	log.Debug("MENbUeX2ApIdExtension: ", rawUEID.GetEnGNbUeid().MENbUeX2ApIdExtension)
				// 	log.Debug("RanUeid: ", rawUEID.GetEnGNbUeid().RanUeid)
				// } else {
				// 	log.Debug("GetEnGNbUeid: nil")
				// }

				// if rawUEID.GetGNbCuUpUeid() != nil {
				// 	log.Debug("GetGNbCuUpUeid:")
				// 	log.Debug("GNbCuCpUeE1ApId: ", rawUEID.GetGNbCuUpUeid().GNbCuCpUeE1ApId)
				// 	log.Debug("RanUeid: ", rawUEID.GetGNbCuUpUeid().RanUeid)
				// } else {
				// 	log.Debug("GetGNbCuUpUeid: nil")
				// }

				// if rawUEID.GetGNbDuUeid() != nil {
				// 	log.Debug("GetGNbDuUeid:")
				// 	log.Debug("GNbCuUeF1ApId: ", rawUEID.GetGNbDuUeid().GNbCuUeF1ApId)
				// 	log.Debug("RanUeid: ", rawUEID.GetGNbDuUeid().RanUeid)
				// } else {
				// 	log.Debug("GetGNbDuUeid: nil")
				// }

				// if rawUEID.GetGNbUeid() != nil {
				// 	log.Debug("GetGNbUeid:")
				// 	log.Debug("AmfUeNgapId: ", rawUEID.GetGNbUeid().AmfUeNgapId)
				// 	log.Debug("GNbCuCpUeE1ApIdList: ", rawUEID.GetGNbUeid().GNbCuCpUeE1ApIdList)
				// 	log.Debug("GNbCuUeF1ApIdList: ", rawUEID.GetGNbUeid().GNbCuUeF1ApIdList)
				// 	log.Debug("GlobalGnbId: ", rawUEID.GetGNbUeid().GlobalGnbId)
				// 	log.Debug("GlobalNgRannodeId: ", rawUEID.GetGNbUeid().GlobalNgRannodeId)
				// 	log.Debug("Guami: ", rawUEID.GetGNbUeid().Guami)
				// 	log.Debug("MNgRanUeXnApId: ", rawUEID.GetGNbUeid().MNgRanUeXnApId)
				// 	log.Debug("RanUeid: ", rawUEID.GetGNbUeid().RanUeid)
				// } else {
				// 	log.Debug("GetGNbUeid: nil")
				// }

				// if rawUEID.GetNgENbDuUeid() != nil {
				// 	log.Debug("GetNgENbDuUeid:")
				// 	log.Debug("NgENbCuUeW1ApId: ", rawUEID.GetNgENbDuUeid().NgENbCuUeW1ApId)
				// } else {
				// 	log.Debug("GetNgENbDuUeid: nil")
				// }

				// if rawUEID.GetNgENbUeid() != nil {
				// 	log.Debug("GetNgENbUeid:")
				// 	log.Debug("AmfUeNgapId: ", rawUEID.GetNgENbUeid().AmfUeNgapId)
				// 	log.Debug("GlobalNgEnbId: ", rawUEID.GetNgENbUeid().GlobalNgEnbId)
				// 	log.Debug("GlobalNgRannodeId: ", rawUEID.GetNgENbUeid().GlobalNgRannodeId)
				// 	log.Debug("Guami: ", rawUEID.GetNgENbUeid().Guami)
				// 	log.Debug("MNgRanUeXnApId: ", rawUEID.GetNgENbUeid().MNgRanUeXnApId)
				// 	log.Debug("NgENbCuUeW1ApId: ", rawUEID.GetNgENbUeid().NgENbCuUeW1ApId)
				// } else {
				// 	log.Debug("GetNgENbUeid: nil")
				// }

				// log.Debug("Other:")
				// log.Debug("GetUeid: ", rawUEID.GetUeid())
				// log.Debug("ProtoReflect: ", rawUEID.ProtoReflect())

				tgtCellID := nv.TgtCellID
				// log.Debug("Target: ", tgtCellID)
				header, err := control.CreateRcControlHeader(rawUEID)
				if err != nil {
					log.Error(err)
				}
				// log.Debugf("send control message for key: %v, value: %v", key, nv)
				payload, err := control.CreateRcControlMessage(tgtCellID)
				if err != nil {
					log.Error(err)
				}
				node := m.e2client.Node(e2client.NodeID(e2nodeID))
				_, err = node.Control(ctx, &e2api.ControlMessage{
					Header:  header,
					Payload: payload,
				}, nv.CallProcessID)
				if err != nil {
					log.Warn(err)
				}
				// log.Debugf("Outcome: %v", outcome)
				// log.Debugf("State changed for %v from %v to %v", key, nv.State.String(), store.Done)
				nv.State = store.Done
				_, err = m.metricStore.Put(ctx, key, nv, store.Done)
				if err != nil {
					log.Error(err)
				}
			}
		}
	}
}

func (m *Manager) createSubscription(ctx context.Context, e2nodeID topoapi.ID) error {
	// log.Debug("I'm creating subscription")
	eventTriggerData, err := subscription.CreateEventTriggerDefinition()
	if err != nil {
		return err
	}

	// log.Debug("I'm creating subscription action")
	actions, err := subscription.CreateSubscriptionAction()
	if err != nil {
		log.Warn(err)
	}

	// log.Debug("I'm getting E2 Node aspects")
	aspects, err := m.rnibClient.GetE2NodeAspects(ctx, e2nodeID)
	if err != nil {
		return err
	}

	// log.Debug("I'm getting RAN function")
	_, err = m.getRanFunction(aspects.ServiceModels)
	if err != nil {
		return err
	}

	// log.Debug("I'm creating subscription specs")
	ch := make(chan e2api.Indication)
	node := m.e2client.Node(e2client.NodeID(e2nodeID))
	subName := "rimedo-ts-subscription"
	subSpec := e2api.SubscriptionSpec{
		Actions: actions,
		EventTrigger: e2api.EventTrigger{
			Payload: eventTriggerData,
		},
	}

	// log.Debug("I'm subscribing")
	channelID, err := node.Subscribe(ctx, subName, subSpec, ch)
	if err != nil {
		// log.Debug("There is an error")
		log.Warn(err)
		return err
	}

	// log.Debug("I'm opening reader")
	streamReader, err := m.streams.OpenReader(ctx, node, subName, channelID, subSpec)
	if err != nil {
		return err
	}
	go m.sendIndicationOnStream(streamReader.StreamID(), ch)

	// log.Debug("I'm here, I mean in E2 Manager befor creating Monitor")
	monitor := monitoring.NewMonitor(streamReader, e2nodeID, m.metricStore, m.nodeManager, m.topoIDsEnabled)

	err = monitor.Start(ctx)
	if err != nil {
		log.Warn(err)
	}

	return nil
}

func (m *Manager) getRanFunction(serviceModelsInfo map[string]*topoapi.ServiceModelInfo) (*topoapi.RCRanFunction, error) {
	for _, sm := range serviceModelsInfo {
		smName := strings.ToLower(sm.Name)
		if smName == string(m.smModelName) && sm.OID == oid {
			// log.Debug("It works")
			rcRanFunction := &topoapi.RCRanFunction{}
			for _, ranFunction := range sm.RanFunctions {
				if ranFunction.TypeUrl == ranFunction.GetTypeUrl() {
					err := prototypes.UnmarshalAny(ranFunction, rcRanFunction)
					if err != nil {
						return nil, err
					}
					return rcRanFunction, nil
				}
			}
		}
	}
	return nil, errors.New(errors.NotFound, "cannot retrieve ran functions")

}

func (m *Manager) sendIndicationOnStream(streamID broker.StreamID, ch chan e2api.Indication) {
	streamWriter, err := m.streams.GetWriter(streamID)
	if err != nil {
		log.Error(err)
		return
	}

	for msg := range ch {
		err := streamWriter.Send(msg)
		if err != nil {
			log.Warn(err)
			return
		}
	}
}

func (m *Manager) GetCellTypes(ctx context.Context) map[string]rnib.Cell {
	cellTypes, err := m.rnibClient.GetCellTypes(ctx)
	if err != nil {
		log.Warn(err)
	}
	return cellTypes
}

func (m *Manager) SetCellType(ctx context.Context, cellID string, cellType string) error {
	err := m.rnibClient.SetCellType(ctx, cellID, cellType)
	if err != nil {
		log.Warn(err)
		return err
	}
	return nil
}
