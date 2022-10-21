// Created by RIMEDO-Labs team
// based on onosproject/onos-mho/pkg/southbound/e2/manager.go

package e2

import (
	"context"
	"fmt"
	"strings"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/rnib"
	prototypes "github.com/gogo/protobuf/types"
	e2api "github.com/onosproject/onos-api/go/onos/e2t/e2/v1beta1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/pdubuilder"
	e2sm_mho "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-mho-go"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-mho/pkg/broker"
	"github.com/onosproject/onos-mho/pkg/store"
	"github.com/onosproject/onos-mho/pkg/utils/control"
	"github.com/onosproject/onos-mho/pkg/utils/subscription"
	e2client "github.com/onosproject/onos-ric-sdk-go/pkg/e2/v1beta1"
	"google.golang.org/protobuf/proto"
)

var log = logging.GetLogger("rimedo-ts", "e2", "manager")

const (
	oidRc  = "1.3.6.1.4.1.53148.1.1.2.3"
	oidMho = "1.3.6.1.4.1.53148.1.2.2.101"
)

type Options struct {
	AppID        string
	E2tAddress   string
	E2tPort      int
	TopoAddress  string
	TopoPort     int
	SmRcName     string
	SmRcVersion  string
	SmMhoName    string
	SmMhoVersion string
}

func NewManager(options Options, metricStore store.Store, nodeManager *monitoring.NodeManager) (Manager, error) {

	smRcName := e2client.ServiceModelName(options.SmRcName)
	smRcVer := e2client.ServiceModelVersion(options.SmRcVersion)
	smMhoName := e2client.ServiceModelName(options.SmMhoName)
	smMhoVer := e2client.ServiceModelVersion(options.SmMhoVersion)
	appID := e2client.AppID(options.AppID)
	e2ClientRc := e2client.NewClient(
		e2client.WithAppID(appID),
		e2client.WithServiceModel(smRcName, smRcVer),
		e2client.WithE2TAddress(options.E2tAddress, options.E2tPort),
	)
	e2ClientMho := e2client.NewClient(
		e2client.WithAppID(appID),
		e2client.WithServiceModel(smMhoName, smMhoVer),
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
		e2ClientRc:  e2ClientRc,
		e2ClientMho: e2ClientMho,
		rnibClient:  rnibClient,
		nodeManager: nodeManager,
		streams:     broker.NewBroker(),
		// monitor:    nil,
		// indCh:       indCh,
		// ctrlReqChs:  ctrlReqChs,
		smRcModelName:  smRcName,
		smMhoModelName: smMhoName,
		// ueStore:         ueStore,
		// cellStore:       cellStore,
		// onosPolicyStore: onosPolicyStore,
		metricStore: metricStore,
		// policies:        policies,
	}, nil
}

type Manager struct {
	e2ClientRc  e2client.Client
	e2ClientMho e2client.Client
	rnibClient  rnib.Client
	nodeManager *monitoring.NodeManager
	streams     broker.Broker
	// monitor    *monitoring.Monitor
	// indCh       chan *mho.E2NodeIndication
	// ctrlReqChs  map[string]chan *e2api.ControlMessage
	smRcModelName  e2client.ServiceModelName
	smMhoModelName e2client.ServiceModelName
	// ueStore         store.Store
	// cellStore       store.Store
	// onosPolicyStore store.Store
	metricStore store.Store
	// policies        map[string]*monitoring.PolicyData
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
			smRcFlag := false
			triggers := make(map[e2sm_mho.MhoTriggerType]bool)
			if m.rnibClient.HasRANFunction(ctx, e2NodeID, oidRc) {
				smRcFlag = true
				triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_UPON_RCV_MEAS_REPORT] = false
				triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_UPON_CHANGE_RRC_STATUS] = false
			} else { //if m.rnibClient.HasRANFunction(ctx, e2NodeID, oidMho) {
				// log.Debug("There is RAN FUNC for MHO")
				triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_UPON_RCV_MEAS_REPORT] = true
				triggers[e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_UPON_CHANGE_RRC_STATUS] = true
				// smFlag = false
			} // } else {
			// 	log.Debugf("Received topo event does not have RC or MHO function for RIMEDO TS xApp - %v", topoEvent)
			// 	continue
			// }

			for triggerType, enabled := range triggers {
				if enabled {
					go func(triggerType e2sm_mho.MhoTriggerType) {
						log.Debugf("start creating subscription %v", topoEvent)
						err := m.createSubscription(ctx, e2NodeID, &triggerType)
						if err != nil {
							log.Warn(err)
						}
					}(triggerType)
				} else if smRcFlag {
					smRcFlag = false
					go func() {
						log.Debugf("start creating subscription %v", topoEvent)
						err := m.createSubscription(ctx, e2NodeID, nil)
						if err != nil {
							log.Warn(err)
						}
					}()
				}
			}
			// go func() {
			// 	log.Debugf("start creating subscription %v", topoEvent)
			// 	err := m.createSubscription(ctx, e2NodeID)
			// 	if err != nil {
			// 		log.Warn(err)
			// 	}
			// }()

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
			if !m.rnibClient.HasRANFunction(ctx, e2NodeID, oidRc) {
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
		if e.Type == store.Updated {
			key := e.Key.(string)
			v, err := m.metricStore.Get(ctx, key)
			if err != nil {
				log.Error(err)
			}
			nv := v.Value.(*store.MetricValue)
			if e.EventMHOState.(store.MHOState) == store.Approved && nv.E2NodeID == e2nodeID {
				rawUEID := nv.RawUEID
				tgtCellID := nv.TgtCellID
				log.Debugf("UE ID CM: ", rawUEID)
				log.Debugf("TARGET CELL CM: ", tgtCellID)
				header, err := control.CreateRcControlHeader(rawUEID)
				if err != nil {
					log.Error(err)
				}
				log.Debugf("send control message for key: %v, value: %v", key, nv)
				payload, err := control.CreateRcControlMessage(tgtCellID)
				if err != nil {
					log.Error(err)
				}
				node := m.e2ClientRc.Node(e2client.NodeID(e2nodeID))
				outcome, err := node.Control(ctx, &e2api.ControlMessage{
					Header:  header,
					Payload: payload,
				}, nv.CallProcessID)
				if err != nil {
					log.Warn(err)
				}
				log.Debugf("Outcome: %v", outcome)
				log.Debugf("State changed for %v from %v to %v", key, nv.State.String(), store.Done)
				nv.State = store.Done
				_, err = m.metricStore.Put(ctx, key, nv, store.Done)
				if err != nil {
					log.Error(err)
				}
			}
		}
	}
}

func (m *Manager) createSubscription(ctx context.Context, e2nodeID topoapi.ID, triggerType *e2sm_mho.MhoTriggerType) error {
	log.Debug("I'm creating subscription")
	var err error
	var eventTriggerData []byte
	var actions []e2api.Action
	var smRcFlag bool
	var node e2client.Node
	if triggerType == nil {
		smRcFlag = true
		eventTriggerData, err = subscription.CreateEventTriggerDefinition()
		if err != nil {
			return err
		}
		log.Debug("I'm creating subscription action")
		actions, err = subscription.CreateSubscriptionAction()
		if err != nil {
			log.Warn(err)
		}
		node = m.e2ClientRc.Node(e2client.NodeID(e2nodeID))
	} else {
		smRcFlag = false
		eventTriggerData, err = m.CreateEventTriggerDefinition(*triggerType)
		if err != nil {
			return err
		}
		actions = m.CreateSubscriptionAction()
		node = m.e2ClientMho.Node(e2client.NodeID(e2nodeID))
	}

	log.Debug("I'm getting E2 Node aspects")
	aspects, err := m.rnibClient.GetE2NodeAspects(ctx, e2nodeID)
	if err != nil {
		return err
	}

	log.Debug("I'm getting RAN function")
	_, _, err = m.GetRanFunction(aspects.ServiceModels)
	if err != nil {
		return err
	}

	log.Debug("I'm creating subscription specs")
	ch := make(chan e2api.Indication)

	if triggerType == nil {
		triggerType = new(e2sm_mho.MhoTriggerType)
		*triggerType = 3
	}
	subName := fmt.Sprintf("rimedo-ts-subscription-%s", *triggerType)
	subSpec := e2api.SubscriptionSpec{
		Actions: actions,
		EventTrigger: e2api.EventTrigger{
			Payload: eventTriggerData,
		},
	}

	log.Debug("I'm subscribing")
	channelID, err := node.Subscribe(ctx, subName, subSpec, ch)
	if err != nil {
		log.Debug("There is an error")
		log.Warn(err)
		return err
	}

	log.Debug("I'm opening reader")
	streamReader, err := m.streams.OpenReader(ctx, node, subName, channelID, subSpec)
	if err != nil {
		return err
	}
	go m.sendIndicationOnStream(streamReader.StreamID(), ch)

	log.Debug("I'm here, I mean in E2 Manager befor creating Monitor")
	monitor := monitoring.NewMonitor(streamReader, e2nodeID, m.metricStore, m.nodeManager, triggerType, smRcFlag)

	err = monitor.Start(ctx)
	if err != nil {
		log.Warn(err)
	}

	return nil
}

func (m *Manager) CreateEventTriggerDefinition(triggerType e2sm_mho.MhoTriggerType) ([]byte, error) {
	var reportPeriodMs int32
	reportingPeriod := 1000
	if triggerType == e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_PERIODIC {
		reportPeriodMs = int32(reportingPeriod)
	} else {
		reportPeriodMs = 0
	}
	e2smRcEventTriggerDefinition, err := pdubuilder.CreateE2SmMhoEventTriggerDefinition(triggerType)
	if err != nil {
		return []byte{}, err
	}
	e2smRcEventTriggerDefinition.GetEventDefinitionFormats().GetEventDefinitionFormat1().SetReportingPeriodInMs(reportPeriodMs)

	err = e2smRcEventTriggerDefinition.Validate()
	if err != nil {
		return []byte{}, err
	}

	protoBytes, err := proto.Marshal(e2smRcEventTriggerDefinition)
	if err != nil {
		return []byte{}, err
	}

	return protoBytes, err
}

func (m *Manager) CreateSubscriptionAction() []e2api.Action {
	actions := make([]e2api.Action, 0)
	action := &e2api.Action{
		ID:   int32(0),
		Type: e2api.ActionType_ACTION_TYPE_REPORT,
		SubsequentAction: &e2api.SubsequentAction{
			Type:       e2api.SubsequentActionType_SUBSEQUENT_ACTION_TYPE_CONTINUE,
			TimeToWait: e2api.TimeToWait_TIME_TO_WAIT_ZERO,
		},
	}
	actions = append(actions, *action)
	return actions
}

func (m *Manager) GetRanFunction(serviceModelsInfo map[string]*topoapi.ServiceModelInfo) (*topoapi.RCRanFunction, *topoapi.MHORanFunction, error) {
	for _, sm := range serviceModelsInfo {
		smName := strings.ToLower(sm.Name)
		if smName == string(m.smRcModelName) && sm.OID == oidRc {
			log.Debug("It works")
			rcRanFunction := &topoapi.RCRanFunction{}
			for _, ranFunction := range sm.RanFunctions {
				if ranFunction.TypeUrl == ranFunction.GetTypeUrl() {
					err := prototypes.UnmarshalAny(ranFunction, rcRanFunction)
					if err != nil {
						return nil, nil, err
					}
					return rcRanFunction, nil, nil
				}
			}
		} else if smName == string(m.smMhoModelName) && sm.OID == oidMho {
			mhoRanFunction := &topoapi.MHORanFunction{}
			for _, ranFunction := range sm.RanFunctions {
				if ranFunction.TypeUrl == ranFunction.GetTypeUrl() {
					err := prototypes.UnmarshalAny(ranFunction, mhoRanFunction)
					if err != nil {
						return nil, nil, err
					}
					return nil, mhoRanFunction, nil
				}
			}
		}
	}
	return nil, nil, errors.New(errors.NotFound, "cannot retrieve ran functions")

}

// func (m *Manager) createEventTrigger(triggerType e2sm_mho.MhoTriggerType) ([]byte, error) {
// 	var reportPeriodMs int32
// 	reportingPeriod := 1000
// 	if triggerType == e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_PERIODIC {
// 		reportPeriodMs = int32(reportingPeriod)
// 	} else {
// 		reportPeriodMs = 0
// 	}
// 	e2smRcEventTriggerDefinition, err := pdubuilder.CreateE2SmMhoEventTriggerDefinition(triggerType)
// 	if err != nil {
// 		return []byte{}, err
// 	}
// 	e2smRcEventTriggerDefinition.GetEventDefinitionFormats().GetEventDefinitionFormat1().SetReportingPeriodInMs(reportPeriodMs)

// 	err = e2smRcEventTriggerDefinition.Validate()
// 	if err != nil {
// 		return []byte{}, err
// 	}

// 	protoBytes, err := proto.Marshal(e2smRcEventTriggerDefinition)
// 	if err != nil {
// 		return []byte{}, err
// 	}

// 	return protoBytes, err
// }

// func (m *Manager) createSubscriptionActions() []e2api.Action {
// 	actions := make([]e2api.Action, 0)
// 	action := &e2api.Action{
// 		ID:   int32(0),
// 		Type: e2api.ActionType_ACTION_TYPE_REPORT,
// 		SubsequentAction: &e2api.SubsequentAction{
// 			Type:       e2api.SubsequentActionType_SUBSEQUENT_ACTION_TYPE_CONTINUE,
// 			TimeToWait: e2api.TimeToWait_TIME_TO_WAIT_ZERO,
// 		},
// 	}
// 	actions = append(actions, *action)
// 	return actions
// }

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
