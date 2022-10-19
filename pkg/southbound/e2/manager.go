// Created by RIMEDO-Labs team
// based on onosproject/onos-mho/pkg/southbound/e2/manager.go

package e2

import (
	"context"
	"sort"
	"strings"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/rnib"
	prototypes "github.com/gogo/protobuf/types"
	e2api "github.com/onosproject/onos-api/go/onos/e2t/e2/v1beta1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-e2-sm/servicemodels/e2sm_kpm_v2_go/pdubuilder"
	e2smkpmv2 "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_kpm_v2_go/v2/e2sm-kpm-v2-go"
	"github.com/onosproject/onos-kpimon/pkg/store/actions"
	actionsstore "github.com/onosproject/onos-kpimon/pkg/store/actions"
	"github.com/onosproject/onos-kpimon/pkg/store/measurements"
	subutils "github.com/onosproject/onos-kpimon/pkg/utils/subscription"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-mho/pkg/broker"
	"github.com/onosproject/onos-mho/pkg/store"
	"github.com/onosproject/onos-mho/pkg/utils/control"
	rcSub "github.com/onosproject/onos-mho/pkg/utils/subscription"
	e2client "github.com/onosproject/onos-ric-sdk-go/pkg/e2/v1beta1"
	"google.golang.org/protobuf/proto"
)

var log = logging.GetLogger("rimedo-ts", "e2", "manager")

const (
	oidRc  = "1.3.6.1.4.1.53148.1.1.2.3"
	oidKpm = "1.3.6.1.4.1.53148.1.2.2.2"
)

type Options struct {
	AppID        string
	E2tAddress   string
	E2tPort      int
	TopoAddress  string
	TopoPort     int
	SmRcName     string
	SmRcVersion  string
	SmKpmName    string
	SmKpmVersion string
}

func NewManager(options Options, metricStore store.Store, actionStore actions.Store, measurementStore measurements.Store, nodeManager *monitoring.NodeManager) (Manager, error) {

	smRcName := e2client.ServiceModelName(options.SmRcName)
	smRcVer := e2client.ServiceModelVersion(options.SmRcVersion)
	smKpmName := e2client.ServiceModelName(options.SmKpmName)
	smKpmVer := e2client.ServiceModelVersion(options.SmKpmVersion)
	appID := e2client.AppID(options.AppID)
	e2RcClient := e2client.NewClient(
		e2client.WithAppID(appID),
		e2client.WithServiceModel(smRcName, smRcVer),
		e2client.WithE2TAddress(options.E2tAddress, options.E2tPort),
	)
	e2KpmClient := e2client.NewClient(
		e2client.WithAppID(appID),
		e2client.WithServiceModel(smKpmName, smKpmVer),
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
		e2RcClient:  e2RcClient,
		e2KpmClient: e2KpmClient,
		rnibClient:  rnibClient,
		streams:     broker.NewBroker(),
		// monitor:     nil,
		// indCh:       indCh,
		// ctrlReqChs:  ctrlReqChs,
		smRcModelName:  smRcName,
		smKpmModelName: smKpmName,
		// ueStore:          ueStore,
		// cellStore:        cellStore,
		// onosPolicyStore:  onosPolicyStore,
		metricStore:      metricStore,
		actionStore:      actionStore,
		measurementStore: measurementStore,
		// policies:         policies,
		nodeManager: nodeManager,
	}, nil
}

type Manager struct {
	e2RcClient  e2client.Client
	e2KpmClient e2client.Client
	rnibClient  rnib.Client
	streams     broker.Broker
	// monitorRc     *monitoring.Monitor
	// indCh       chan *mho.E2NodeIndication
	// ctrlReqChs  map[string]chan *e2api.ControlMessage
	smRcModelName  e2client.ServiceModelName
	smKpmModelName e2client.ServiceModelName
	// ueStore          store.Store
	// cellStore        store.Store
	// onosPolicyStore  store.Store
	metricStore      store.Store
	actionStore      actions.Store
	measurementStore measurements.Store
	// policies         map[string]*monitoring.PolicyData
	nodeManager *monitoring.NodeManager
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

	// go func() {
	// 	ctx, cancel := context.WithCancel(context.Background())
	// 	defer cancel()
	// 	err := m.watchConfigChanges(ctx)
	// 	if err != nil {
	// 		return
	// 	}
	// }()

	return nil
}

// func (m *Manager) watchConfigChanges(ctx context.Context) error {
// 	ch := make(chan event.Event)
// 	err := m.appConfig.Watch(ctx, ch)
// 	if err != nil {
// 		return err
// 	}

// 	// Deletes all of subscriptions
// 	for configEvent := range ch {
// 		log.Debugf("Config event is received: %v", configEvent)
// 		if configEvent.Key == utils.ReportPeriodConfigPath {
// 			channelIDs := m.streams.ChannelIDs()
// 			for _, channelID := range channelIDs {
// 				_, err := m.streams.CloseStream(ctx, channelID)
// 				if err != nil {
// 					log.Warn(err)
// 					return err
// 				}

// 			}
// 		}

// 	}
// 	// Gets all of connected E2 nodes and creates new subscriptions based on new report interval
// 	e2NodeIDs, err := m.rnibClient.E2NodeIDs(ctx, oidKpm)
// 	if err != nil {
// 		log.Warn(err)
// 		return err
// 	}

// 	for _, e2NodeID := range e2NodeIDs {
// 		if !m.rnibClient.HasRANFunction(ctx, e2NodeID, oidKpm) {
// 			continue
// 		}
// 		go func(e2NodeID topoapi.ID) {
// 			err := m.createSubscription(ctx, e2NodeID, false)
// 			if err != nil {
// 				log.Warn(err)
// 			}
// 		}(e2NodeID)
// 	}

// 	return nil

// }

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
			var smRc bool
			if m.rnibClient.HasRANFunction(ctx, e2NodeID, oidRc) {
				smRc = true
			} else if m.rnibClient.HasRANFunction(ctx, e2NodeID, oidKpm) {
				smRc = false
			} else {
				log.Debugf("Received topo event does not have RC or KPM RAN function for RIMEDO TS xApp - %v", topoEvent)
				continue
			}

			go func() {
				log.Debugf("start creating subscription %v", topoEvent)
				err := m.createSubscription(ctx, e2NodeID, smRc)
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
			cellIDs, err := m.rnibClient.GetCells(ctx, e2NodeID)
			if err != nil {
				return err
			}
			if m.rnibClient.HasRANFunction(ctx, e2NodeID, oidRc) {
				for _, cellID := range cellIDs {
					log.Debugf("cell removed, e2NodeID:%v, cellID:%v", e2NodeID, cellID.CellGlobalID.GetValue())
				}
			} else if m.rnibClient.HasRANFunction(ctx, e2NodeID, oidKpm) {
				for _, coi := range cellIDs {
					key := measurements.Key{
						NodeID: string(e2NodeID),
						CellIdentity: measurements.CellIdentity{
							CellID: coi.CellObjectID,
						},
					}
					err = m.measurementStore.Delete(ctx, key)
					if err != nil {
						log.Warn(err)
					}
				}
			} else {
				log.Debugf("Received topo event does not have RC RAN function for MHO - %v", topoEvent)
				continue
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
				header, err := control.CreateRcControlHeader(rawUEID)
				if err != nil {
					log.Error(err)
				}
				log.Debugf("send control message for key: %v, value: %v", key, nv)
				payload, err := control.CreateRcControlMessage(tgtCellID)
				if err != nil {
					log.Error(err)
				}
				node := m.e2RcClient.Node(e2client.NodeID(e2nodeID))
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

func (m *Manager) createSubscription(ctx context.Context, e2nodeID topoapi.ID, rcSm bool) error {

	log.Debug("I'm getting E2 Node aspects")
	aspects, err := m.rnibClient.GetE2NodeAspects(ctx, e2nodeID)
	if err != nil {
		return err
	}

	if rcSm {
		log.Debug("I'm creating subscription")
		eventTriggerData, err := rcSub.CreateEventTriggerDefinition()
		if err != nil {
			return err
		}

		log.Debug("I'm creating subscription action")
		actions, err := rcSub.CreateSubscriptionAction()
		if err != nil {
			log.Warn(err)
		}

		log.Debug("I'm getting RAN function")
		_, err = m.getRcRanFunction(aspects.ServiceModels)
		if err != nil {
			return err
		}

		log.Debug("I'm creating subscription specs")
		ch := make(chan e2api.Indication)
		node := m.e2RcClient.Node(e2client.NodeID(e2nodeID))
		subName := "rimedo-ts-rc-subscription"
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
		monitor := monitoring.NewMonitorRC(streamReader, e2nodeID, m.metricStore, m.nodeManager)

		err = monitor.Start(ctx)
		if err != nil {
			log.Warn(err)
		}

	} else {
		reportStyles, err := m.getKpmReportStyles(aspects.ServiceModels)
		if err != nil {
			log.Warn(err)
			return err
		}

		cells, err := m.rnibClient.GetCells(ctx, e2nodeID)
		if err != nil {
			log.Warn(err)
			return err
		}

		reportPeriod := 1000

		eventTriggerData, err := subutils.CreateEventTriggerData(int64(reportPeriod))
		if err != nil {
			log.Warn(err)
			return err
		}

		granularityPeriod := 1000

		for _, reportStyle := range reportStyles {
			actions, err := m.createSubscriptionActions(ctx, reportStyle, cells, int64(granularityPeriod))
			if err != nil {
				log.Warn(err)
				return err
			}
			measurements := reportStyle.Measurements

			ch := make(chan e2api.Indication)
			node := m.e2KpmClient.Node(e2client.NodeID(e2nodeID))
			subName := "onos-kpimon-subscription"

			subSpec := e2api.SubscriptionSpec{
				Actions: actions,
				EventTrigger: e2api.EventTrigger{
					Payload: eventTriggerData,
				},
			}

			channelID, err := node.Subscribe(ctx, subName, subSpec, ch)
			if err != nil {
				return err
			}

			log.Debugf("Channel ID:%s", channelID)
			streamReader, err := m.streams.OpenReader(ctx, node, subName, channelID, subSpec)
			if err != nil {
				return err
			}

			go m.sendIndicationOnStream(streamReader.StreamID(), ch)
			monitor := monitoring.NewMonitorKPM(streamReader, m.measurementStore, m.actionStore, measurements, e2nodeID, m.rnibClient, m.nodeManager)
			err = monitor.Start(ctx)
			if err != nil {
				log.Warn(err)
			}

		}

	}
	return nil
}

func (m *Manager) createSubscriptionActions(ctx context.Context, reportStyle *topoapi.KPMReportStyle, cells []*topoapi.E2Cell, granularity int64) ([]e2api.Action, error) {
	actions := make([]e2api.Action, 0)

	sort.Slice(cells, func(i, j int) bool {
		return cells[i].CellObjectID < cells[j].CellObjectID
	})

	for index, cell := range cells {
		measInfoList := &e2smkpmv2.MeasurementInfoList{
			Value: make([]*e2smkpmv2.MeasurementInfoItem, 0),
		}

		for _, measurement := range reportStyle.Measurements {
			measTypeMeasName, err := pdubuilder.CreateMeasurementTypeMeasName(measurement.GetName())
			if err != nil {
				return nil, err
			}

			meanInfoItem, err := pdubuilder.CreateMeasurementInfoItem(measTypeMeasName)
			if err != nil {
				return nil, err
			}
			measInfoList.Value = append(measInfoList.Value, meanInfoItem)
		}
		subID := int64(index + 1)
		actionDefinition, err := pdubuilder.CreateActionDefinitionFormat1(cell.GetCellObjectID(), measInfoList, granularity, subID)
		if err != nil {
			return nil, err
		}

		key := actionsstore.NewKey(actionsstore.SubscriptionID{
			SubID: subID,
		})
		// TODO clean up this store if we delete subscriptions
		_, err = m.actionStore.Put(ctx, key, actionDefinition)
		if err != nil {
			log.Warn(err)
			return nil, err
		}

		e2smKpmActionDefinition, err := pdubuilder.CreateE2SmKpmActionDefinitionFormat1(reportStyle.Type, actionDefinition)
		if err != nil {
			return nil, err
		}

		e2smKpmActionDefinitionProto, err := proto.Marshal(e2smKpmActionDefinition)
		if err != nil {
			return nil, err
		}

		action := &e2api.Action{
			ID:   int32(index),
			Type: e2api.ActionType_ACTION_TYPE_REPORT,
			SubsequentAction: &e2api.SubsequentAction{
				Type:       e2api.SubsequentActionType_SUBSEQUENT_ACTION_TYPE_CONTINUE,
				TimeToWait: e2api.TimeToWait_TIME_TO_WAIT_ZERO,
			},
			Payload: e2smKpmActionDefinitionProto,
		}

		actions = append(actions, *action)

	}
	return actions, nil
}

func (m *Manager) getRcRanFunction(serviceModelsInfo map[string]*topoapi.ServiceModelInfo) (*topoapi.RCRanFunction, error) {
	for _, sm := range serviceModelsInfo {
		smName := strings.ToLower(sm.Name)
		if smName == string(m.smRcModelName) && sm.OID == oidRc {
			log.Debug("It works")
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

func (m *Manager) getKpmReportStyles(serviceModelsInfo map[string]*topoapi.ServiceModelInfo) ([]*topoapi.KPMReportStyle, error) {
	for _, sm := range serviceModelsInfo {
		smName := strings.ToLower(sm.Name)
		if smName == string(m.smKpmModelName) && sm.OID == oidKpm {
			kpmRanFunction := &topoapi.KPMRanFunction{}
			for _, ranFunction := range sm.RanFunctions {
				if ranFunction.TypeUrl == ranFunction.GetTypeUrl() {
					err := prototypes.UnmarshalAny(ranFunction, kpmRanFunction)
					if err != nil {
						return nil, err
					}
					return kpmRanFunction.ReportStyles, nil
				}
			}
		}
	}
	return nil, errors.New(errors.NotFound, "cannot retrieve report styles")
}

// func (m *Manager) GetMonitor() *monitoring.Monitor {
// 	return m.monitor
// }

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
