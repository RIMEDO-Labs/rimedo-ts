// SPDX-FileCopyrightText: 2022-present Intel Corporation
// SPDX-FileCopyrightText: 2019-present Open Networking Foundation <info@opennetworking.org>
// SPDX-FileCopyrightText: 2019-present Rimedo Labs
//
// SPDX-License-Identifier: Apache-2.0
// Created by RIMEDO-Labs team
// Based on work of Open Networking Foundation and Intel Corporation teams

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
				log.Debugf(" Received topo event does not have RC RAN function for RIMEDO TS xApp - %v ", topoEvent)
				continue
			}

			go func() {
				log.Debugf(" start creating subscription %v ", topoEvent)
				err := m.createSubscription(ctx, e2NodeID)
				if err != nil {
					log.Warn(err)
				}
			}()

			// go m.watchMHOChanges(ctx, e2NodeID)
		} else if topoEvent.Type == topoapi.EventType_REMOVED {
			relation := topoEvent.Object.Obj.(*topoapi.Object_Relation)
			e2NodeID := relation.Relation.TgtEntityID
			if !m.rnibClient.HasRcRANFunction(ctx, e2NodeID, oid) {
				log.Debugf(" Received topo event does not have RC RAN function for MHO - %v ", topoEvent)
				continue
			}
			cellIDs, err := m.rnibClient.GetCells(ctx, e2NodeID)
			if err != nil {
				return err
			}
			for _, cellID := range cellIDs {
				log.Debugf(" cell removed, e2NodeID:%v, cellID:%v ", e2NodeID, cellID.CellGlobalID.GetValue())
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
				id := fmt.Sprintf("%d", rawUEID.GetGNbUeid().AmfUeNgapId.Value)
				ueKey := id
				for len(id) < 16 {
					id = "0" + id
				}
				ue := m.nodeManager.GetUe(ctx, id)
				if ue == nil {
					log.Error(" There's no such UE ")
				}
				// log.Debugf("send control message for key: %v, value: %v", key, nv)
				log.Debug("")
				log.Infof(" CONTROL MESSAGE: UE [ID:%v, 5QI:%v] switched to new CELL [CGI:%v] ", ueKey, ue.FiveQi, ue.CGI)
				log.Debug("")
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
	eventTriggerData, err := subscription.CreateEventTriggerDefinition()
	if err != nil {
		return err
	}

	actions, err := subscription.CreateSubscriptionAction()
	if err != nil {
		log.Warn(err)
	}

	aspects, err := m.rnibClient.GetE2NodeAspects(ctx, e2nodeID)
	if err != nil {
		return err
	}

	_, err = m.getRanFunction(aspects.ServiceModels)
	if err != nil {
		return err
	}

	ch := make(chan e2api.Indication)
	node := m.e2client.Node(e2client.NodeID(e2nodeID))
	subName := "rimedo-ts-subscription"
	subSpec := e2api.SubscriptionSpec{
		Actions: actions,
		EventTrigger: e2api.EventTrigger{
			Payload: eventTriggerData,
		},
	}

	channelID, err := node.Subscribe(ctx, subName, subSpec, ch)
	if err != nil {
		log.Warn(err)
		return err
	}

	streamReader, err := m.streams.OpenReader(ctx, node, subName, channelID, subSpec)
	if err != nil {
		return err
	}
	go m.sendIndicationOnStream(streamReader.StreamID(), ch)

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
