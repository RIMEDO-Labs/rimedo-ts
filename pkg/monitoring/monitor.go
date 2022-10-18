// Copy from onosproject/onos-mho/pkg/monitoring/monitor.go
// modified by RIMEDO-Labs team

package monitoring

import (
	"context"

	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	e2api "github.com/onosproject/onos-api/go/onos/e2t/e2/v1beta1"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	e2sm_v2_ies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-v2-ies"
	e2smrcies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_rc/v1/e2sm-rc-ies"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-mho/pkg/broker"
	"github.com/onosproject/onos-mho/pkg/store"
	idutil "github.com/onosproject/onos-mho/pkg/utils/id"
	"google.golang.org/protobuf/proto"
)

var log = logging.GetLogger("monitoring")

func NewMonitor(streamReader broker.StreamReader, nodeID topoapi.ID, ueStore store.Store, cellStore store.Store, onosPolicyStore store.Store, metricStore store.Store, policies map[string]*PolicyData) *Monitor {
	return &Monitor{
		streamReader:    streamReader,
		nodeID:          nodeID,
		ueStore:         ueStore,
		cellStore:       cellStore,
		onosPolicyStore: onosPolicyStore,
		metricStore:     metricStore,
		cells:           make(map[string]*CellData),
		policies:        policies,
		// indChan:      indChan,
		// triggerType:  triggerType,
	}
}

type Monitor struct {
	streamReader    broker.StreamReader
	nodeID          topoapi.ID
	ueStore         store.Store
	cellStore       store.Store
	onosPolicyStore store.Store
	metricStore     store.Store
	cells           map[string]*CellData
	policies        map[string]*PolicyData
	// indChan      chan *mho.E2NodeIndication
	// triggerType  e2sm_mho.MhoTriggerType
}

func (m *Monitor) Start(ctx context.Context) error {
	log.Debugf("I'm starting MONITOR")
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
	log.Debugf("processIndication, nodeID: %v, indication: %v ", nodeID, indication)

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

	log.Debugf("Indication Header: %v", headerFormat2)
	log.Debugf("Indication Message: %v", messageFormat5)

	callProcessID := indication.GetCallProcessId()

	ueID := headerFormat2.GetUeId()
	var tgtCellID string
	if ueID.GetGNbUeid() != nil {
		key := idutil.GenerateGnbUeIDString(ueID.GetGNbUeid())
		// TODO make it better - implement function to recursively retrieve appropriate RAN parameter
		tgtCellID = messageFormat5.GetRanPRequestedList()[0].GetRanParameterValueType().GetRanPChoiceStructure().GetRanParameterStructure().GetSequenceOfRanParameters()[0].
			GetRanParameterValueType().GetRanPChoiceStructure().GetRanParameterStructure().GetSequenceOfRanParameters()[0].
			GetRanParameterValueType().GetRanPChoiceStructure().GetRanParameterStructure().GetSequenceOfRanParameters()[0].
			GetRanParameterValueType().GetRanPChoiceElementFalse().GetRanParameterValue().GetValuePrintableString()

		//for _, m := range messageFormat5.GetRanPRequestedList() {
		//	if m.GetRanParameterId().Value == definition.NRCGIRANParameterID {
		//		tgtCellID = m.GetRanParameterValueType().GetRanPChoiceElementFalse().GetRanParameterValue().GetValuePrintableString()
		//	} else {
		//		v, err := ranparam.GetRanParameterValue(m.GetRanParameterValueType(), definition.NRCGIRANParameterID)
		//		if err != nil {
		//			return err
		//		}
		//		tgtCellID = v.GetValuePrintableString()
		//	}
		//}
		log.Debugf("Received UEID: %v", ueID)
		log.Debugf("Received TargetCellID: %v", tgtCellID)
		log.Debugf("Received CallProcessID: %v", callProcessID)
		log.Debugf("Received Store Key: %v", key)

		if m.metricStore.HasEntry(ctx, key) {
			v, err := m.metricStore.Get(ctx, key)
			if err != nil {
				return err
			}
			nv := v.Value.(*store.MetricValue)
			if nv.TgtCellID != tgtCellID {
				log.Debugf("State changed for %v from %v to %v", key, nv.State.String(), store.StateCreated)
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
			} else if nv.State == store.Denied {
				// update with the same value to trigger control
				log.Debugf("State changed for %v from % to %v", key, nv.State.String(), store.Denied)
				_, err := m.metricStore.Put(ctx, key, nv, store.Denied)
				if err != nil {
					return err
				}
			} else {
				log.Debugf("Current state for %v is %v", key, nv.State.String())
			}
		} else {
			log.Debugf("State created for %v", key)
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
		}
	} else {
		return errors.NewNotSupported("supported type GnbUeid only; received %v", ueID)
	}
	return nil
}

func (c *Monitor) CreateUe(ctx context.Context, ueID string) *UeData {
	if len(ueID) == 0 {
		panic("bad data")
	}
	ueData := &UeData{
		UeID:          ueID,
		CGIString:     "",
		RrcState:      e2smrcies.RrcState_name[int32(e2smrcies.RrcState_RRC_STATE_RRC_CONNECTED)],
		RsrpNeighbors: make(map[string]int32),
	}
	_, err := c.ueStore.Put(ctx, ueID, *ueData, store.Done)
	if err != nil {
		log.Warn(err)
	}

	return ueData
}

func (c *Monitor) GetUe(ctx context.Context, ueID string) *UeData {
	var ueData *UeData
	u, err := c.ueStore.Get(ctx, ueID)
	if err != nil || u == nil {
		return nil
	}
	t := u.Value.(UeData)
	ueData = &t
	if ueData.UeID != ueID {
		panic("bad data")
	}

	return ueData
}

func (c *Monitor) SetUe(ctx context.Context, ueData *UeData) {
	_, err := c.ueStore.Put(ctx, ueData.UeID, *ueData, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *Monitor) AttachUe(ctx context.Context, ueData *UeData, cgi string, cgiObject *e2sm_v2_ies.Cgi) {

	c.DetachUe(ctx, ueData)

	ueData.CGIString = cgi
	c.SetUe(ctx, ueData)
	cell := c.GetCell(ctx, cgi)
	if cell == nil {
		cell = c.CreateCell(ctx, cgi, cgiObject)
	}
	cell.Ues[ueData.UeID] = ueData
	c.SetCell(ctx, cell)
}

func (c *Monitor) DetachUe(ctx context.Context, ueData *UeData) {
	for _, cell := range c.cells {
		delete(cell.Ues, ueData.UeID)
	}
}

func (c *Monitor) CreateCell(ctx context.Context, cgi string, cgiObject *e2sm_v2_ies.Cgi) *CellData {
	if len(cgi) == 0 {
		panic("bad data")
	}
	cellData := &CellData{
		CGI:       cgiObject,
		CGIString: cgi,
		Ues:       make(map[string]*UeData),
	}
	_, err := c.cellStore.Put(ctx, cgi, *cellData, store.Done)
	if err != nil {
		panic("bad data")
	}
	c.cells[cellData.CGIString] = cellData
	return cellData
}

func (c *Monitor) GetCell(ctx context.Context, cgi string) *CellData {
	var cellData *CellData
	cell, err := c.cellStore.Get(ctx, cgi)
	if err != nil || cell == nil {
		return nil
	}
	t := cell.Value.(CellData)
	if t.CGIString != cgi {
		panic("bad data")
	}
	cellData = &t
	return cellData
}

func (c *Monitor) SetCell(ctx context.Context, cellData *CellData) {
	if len(cellData.CGIString) == 0 {
		panic("bad data")
	}
	_, err := c.cellStore.Put(ctx, cellData.CGIString, *cellData, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *Monitor) CreatePolicy(ctx context.Context, key string, policy *policyAPI.API) *PolicyData {
	if len(key) == 0 {
		panic("bad data")
	}
	policyData := &PolicyData{
		Key:        key,
		API:        policy,
		IsEnforced: true,
	}

	log.Debugf("Key: ", key)
	log.Debugf("PolicyData: ", policyData)

	_, err := c.onosPolicyStore.Put(ctx, key, *policyData, store.Done)
	if err != nil {
		log.Panic("bad data")
	}
	c.policies[policyData.Key] = policyData
	return policyData
}

func (c *Monitor) GetPolicy(ctx context.Context, key string) *PolicyData {
	var policy *PolicyData
	p, err := c.onosPolicyStore.Get(ctx, key)
	if err != nil || p == nil {
		return nil
	}
	t := p.Value.(PolicyData)
	if t.Key != key {
		panic("bad data")
	}
	policy = &t

	return policy
}

func (c *Monitor) SetPolicy(ctx context.Context, key string, policy *PolicyData) {
	_, err := c.onosPolicyStore.Put(ctx, key, *policy, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *Monitor) DeletePolicy(ctx context.Context, key string) {
	if err := c.onosPolicyStore.Delete(ctx, key); err != nil {
		panic("bad data")
	} else {
		delete(c.policies, key)
	}
}

func (c *Monitor) GetPolicyStore() *store.Store {
	return &c.onosPolicyStore
}

func ConvertCgiToTheRightForm(cgi string) string {
	return cgi[0:8] + cgi[13:14] + cgi[10:12] + cgi[8:10] + cgi[14:15] + cgi[12:13]
}

// m.indChan <- &mho.E2NodeIndication{
// 	NodeID:      string(nodeID),
// 	TriggerType: m.triggerType,
// 	IndMsg: e2ind.Indication{
// 		Payload: e2ind.Payload{
// 			Header:  indication.Header,
// 			Message: indication.Payload,
// 		},
// 	},
// }
