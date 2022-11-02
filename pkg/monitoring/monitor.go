// SPDX-FileCopyrightText: 2022-present Intel Corporation
// SPDX-FileCopyrightText: 2019-present Open Networking Foundation <info@opennetworking.org>
// SPDX-FileCopyrightText: 2019-present Rimedo Labs
//
// SPDX-License-Identifier: Apache-2.0
// Created by Intel Corporation team
// Modified by RIMEDO-Labs team

package monitoring

import (
	"context"

	e2api "github.com/onosproject/onos-api/go/onos/e2t/e2/v1beta1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	e2smrcies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_rc/v1/e2sm-rc-ies"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-mho/pkg/broker"
	"github.com/onosproject/onos-mho/pkg/store"
	idutil "github.com/onosproject/onos-mho/pkg/utils/id"
	"google.golang.org/protobuf/proto"
)

var log = logging.GetLogger("rimedo-ts", "monitoring")

func NewMonitor(streamReader broker.StreamReader, nodeID topoapi.ID, metricStore store.Store, nodeManager *NodeManager, flag bool) *Monitor {
	return &Monitor{
		streamReader:   streamReader,
		nodeID:         nodeID,
		metricStore:    metricStore,
		nodeManager:    nodeManager,
		topoIDsEnabled: flag,
	}
}

type Monitor struct {
	streamReader   broker.StreamReader
	nodeID         topoapi.ID
	metricStore    store.Store
	nodeManager    *NodeManager
	topoIDsEnabled bool
}

func (m *Monitor) Start(ctx context.Context) error {
	errCh := make(chan error)
	go func() {
		for {
			indMsg, err := m.streamReader.Recv(ctx)
			if err != nil {
				log.Errorf("Error reading indication stream, chanID:%v, streamID:%v, err:%v", m.streamReader.ChannelID(), m.streamReader.StreamID(), err)
				errCh <- err
			}
			err = m.processIndication(ctx, indMsg, m.nodeID)
			if err != nil {
				log.Errorf("Error processing indication, err:%v", err)
				errCh <- err
			}
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Monitor) processIndication(ctx context.Context, indication e2api.Indication, nodeID topoapi.ID) error {
	// log.Debugf("processIndication, nodeID: %v, indication: %v ", nodeID, indication)

	header := e2smrcies.E2SmRcIndicationHeader{}
	err := proto.Unmarshal(indication.Header, &header)
	if err != nil {
		return err
	}

	message := e2smrcies.E2SmRcIndicationMessage{}
	err = proto.Unmarshal(indication.Payload, &message)
	if err != nil {
		return err
	}

	headerFormat2 := header.GetRicIndicationHeaderFormats().GetIndicationHeaderFormat2()
	messageFormat5 := message.GetRicIndicationMessageFormats().GetIndicationMessageFormat5()

	callProcessID := indication.GetCallProcessId()

	ueID := headerFormat2.GetUeId()
	var tgtCellID string
	if ueID.GetGNbUeid() != nil {
		key := idutil.GenerateGnbUeIDString(ueID.GetGNbUeid())
		tgtCellID = messageFormat5.GetRanPRequestedList()[0].GetRanParameterValueType().GetRanPChoiceStructure().GetRanParameterStructure().GetSequenceOfRanParameters()[0].
			GetRanParameterValueType().GetRanPChoiceStructure().GetRanParameterStructure().GetSequenceOfRanParameters()[0].
			GetRanParameterValueType().GetRanPChoiceStructure().GetRanParameterStructure().GetSequenceOfRanParameters()[0].
			GetRanParameterValueType().GetRanPChoiceElementFalse().GetRanParameterValue().GetValuePrintableString()

		if m.metricStore.HasEntry(ctx, key) {
			v, err := m.metricStore.Get(ctx, key)
			if err != nil {
				return err
			}
			nv := v.Value.(*store.MetricValue)
			cell := m.nodeManager.GetCell(ctx, tgtCellID)
			if nv.TgtCellID != tgtCellID && cell == nil {
				_ = m.nodeManager.CreateCell(ctx, tgtCellID, nodeID)
			}

			if nv.State == store.Denied {
				// update with the same value to trigger control
				// log.Debugf("State changed for %v from % to %v", key, nv.State.String(), store.Denied)
				_, err := m.metricStore.Put(ctx, key, nv, store.Denied)
				if err != nil {
					return err
				}
			} else {
				// log.Debugf("Current state for %v is %v", key, nv.State.String())
			}
		} else {
			// log.Debugf("State created for %v", key)
			metricValue := &store.MetricValue{
				RawUEID:       ueID,
				TgtCellID:     tgtCellID,
				State:         store.StateCreated,
				CallProcessID: callProcessID,
				E2NodeID:      nodeID,
			}
			_, err := m.metricStore.Put(ctx, key, metricValue, store.StateCreated)
			if err != nil {
				return err
			}
			ueData := m.nodeManager.CreateUe(ctx, ueID)
			tgtCellID = m.ConvertCgiToTheRightForm(tgtCellID)
			m.nodeManager.AttachUe(ctx, ueData, tgtCellID, nodeID)
		}
	} else {
		return errors.NewNotSupported("supported type GnbUeid only; received %v", ueID)
	}
	return nil
}

func (m *Monitor) ConvertCgiToTheRightForm(cgi string) string {
	if m.topoIDsEnabled {
		return cgi[0:6] + cgi[14:15] + cgi[12:14] + cgi[10:12] + cgi[8:10] + cgi[6:8]
	}
	return cgi
}
