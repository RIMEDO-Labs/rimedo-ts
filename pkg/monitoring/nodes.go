package monitoring

import (
	"context"
	"strconv"
	"sync"

	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	e2sm_mho "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-mho-go"
	e2sm_v2_ies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-v2-ies"
	e2smrcies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_rc/v1/e2sm-rc-ies"
	"github.com/onosproject/onos-mho/pkg/store"

	// "github.com/onosproject/onos-ric-sdk-go/pkg/e2/indication"
	"google.golang.org/protobuf/proto"
)

type E2NodeIndication struct {
	NodeID      string
	TriggerType e2sm_mho.MhoTriggerType
	IndMsg      Indication
}

func NewNodeManager(infoChan chan *E2NodeIndication, ueStore store.Store, cellStore store.Store, onosPolicyStore store.Store, policies map[string]*PolicyData) *NodeManager {
	return &NodeManager{
		informationChannel: infoChan,
		ueStore:            ueStore,
		cellStore:          cellStore,
		onosPolicyStore:    onosPolicyStore,
		cells:              make(map[string]*CellData),
		policies:           policies,
		mutex:              sync.RWMutex{},
	}
}

type NodeManager struct {
	informationChannel chan *E2NodeIndication
	ueStore            store.Store
	cellStore          store.Store
	onosPolicyStore    store.Store
	cells              map[string]*CellData
	policies           map[string]*PolicyData
	mutex              sync.RWMutex
}

func (m *NodeManager) Start(ctx context.Context) {
	go m.listenInformationChannel(ctx)
}

func (m *NodeManager) listenInformationChannel(ctx context.Context) {
	var err error
	for information := range m.informationChannel {

		headerByte := information.IndMsg.Payload.Header
		messageByte := information.IndMsg.Payload.Message
		e2NodeID := information.NodeID

		header := e2sm_mho.E2SmMhoIndicationHeader{}
		if err = proto.Unmarshal(headerByte, &header); err == nil {
			message := e2sm_mho.E2SmMhoIndicationMessage{}
			if err = proto.Unmarshal(messageByte, &message); err == nil {
				switch x := message.E2SmMhoIndicationMessage.(type) {
				case *e2sm_mho.E2SmMhoIndicationMessage_IndicationMessageFormat1:
					if information.TriggerType == e2sm_mho.MhoTriggerType_MHO_TRIGGER_TYPE_PERIODIC {
						go m.handlePeriodicRaportAboutNodes(ctx, header.GetIndicationHeaderFormat1(), message.GetIndicationMessageFormat1(), e2NodeID)
					}
				case *e2sm_mho.E2SmMhoIndicationMessage_IndicationMessageFormat2:
					go m.handleRaportAboutRrcState(ctx, header.GetIndicationHeaderFormat1(), message.GetIndicationMessageFormat2(), e2NodeID)
				default:
					log.Warnf("Unknown MHO indication message format, indication message: %v", x)
				}
			}
		}
		if err != nil {
			log.Error(err)
		}
	}
}

func (c *NodeManager) handlePeriodicRaportAboutNodes(ctx context.Context, header *e2sm_mho.E2SmMhoIndicationHeaderFormat1, message *e2sm_mho.E2SmMhoIndicationMessageFormat1, e2NodeID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	ueID, err := GetUeID(message.GetUeId())
	if err != nil {
		log.Errorf("handlePeriodicReport() couldn't extract UeID: %v", err)
	}
	cgi := GetCGIFromIndicationHeader(header)
	cgi = ConvertCgiToTheRightForm(cgi)
	cgiObject := header.GetCgi()

	ueIdString := strconv.Itoa(int(ueID))
	n := (16 - len(ueIdString))
	for i := 0; i < n; i++ {
		ueIdString = "0" + ueIdString
	}
	var ueData *UeData
	// newUe := false
	ueData = c.GetUe(ctx, ueIdString)
	if ueData == nil {
		ueData = c.CreateUe(ctx, ueIdString)
		c.AttachUe(ctx, ueData, cgi, cgiObject)
		// newUe = true
	} else if ueData.CGIString != cgi {
		return
	}

	ueData.CGI = cgiObject
	ueData.E2NodeID = e2NodeID

	ueData.RsrpServing, ueData.RsrpNeighbors, ueData.RsrpTable, ueData.CgiTable = c.GetRsrpFromReport(ctx, GetNciFromCellGlobalID(header.GetCgi()), message.MeasReport)

	old5qi := ueData.FiveQi
	ueData.FiveQi = c.GetFiveQiFromReport(ctx, GetNciFromCellGlobalID(header.GetCgi()), message.MeasReport)

	if old5qi != ueData.FiveQi {
		log.Infof("\nQUALITY MESSAGE: 5QI for UE [ID:%v] changed [5QI:%v]\n", ueData.UeID, ueData.FiveQi)
	}

	// if !newUe && rsrpServing == ueData.RsrpServing && reflect.DeepEqual(rsrpNeighbors, ueData.RsrpNeighbors) {
	// 	return
	// }

	// ueData.RsrpServing, ueData.RsrpNeighbors, ueData.RsrpTable, ueData.CgiTable = rsrpServing, rsrpNeighbors, rsrpTable, cgiTable
	c.SetUe(ctx, ueData)

}

func (c *NodeManager) handleRaportAboutRrcState(ctx context.Context, header *e2sm_mho.E2SmMhoIndicationHeaderFormat1, message *e2sm_mho.E2SmMhoIndicationMessageFormat2, e2NodeID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	ueID, err := GetUeID(message.GetUeId())
	if err != nil {
		log.Errorf("handleRrcState() couldn't extract UeID: %v", err)
	}
	cgi := GetCGIFromIndicationHeader(header)
	cgi = ConvertCgiToTheRightForm(cgi)
	cgiObject := header.GetCgi()

	ueIdString := strconv.Itoa(int(ueID))
	n := (16 - len(ueIdString))
	for i := 0; i < n; i++ {
		ueIdString = "0" + ueIdString
	}
	var ueData *UeData
	ueData = c.GetUe(ctx, ueIdString)
	if ueData == nil {
		ueData = c.CreateUe(ctx, ueIdString)
		c.AttachUe(ctx, ueData, cgi, cgiObject)
	} else if ueData.CGIString != cgi {
		return
	}

	ueData.CGI = cgiObject
	ueData.E2NodeID = e2NodeID

	newRrcState := message.GetRrcStatus().String()
	c.SetUeRrcState(ctx, ueData, newRrcState, cgi, cgiObject)

	c.SetUe(ctx, ueData)

}

func (c *NodeManager) GetRsrpFromReport(ctx context.Context, servingNci uint64, measReport []*e2sm_mho.E2SmMhoMeasurementReportItem) (int32, map[string]int32, map[string]int32, map[string]*e2sm_v2_ies.Cgi) {
	var rsrpServing int32
	rsrpNeighbors := make(map[string]int32)
	rsrpTable := make(map[string]int32)
	cgiTable := make(map[string]*e2sm_v2_ies.Cgi)

	for _, measReportItem := range measReport {

		if GetNciFromCellGlobalID(measReportItem.GetCgi()) == servingNci {
			CGIString := GetCGIFromMeasReportItem(measReportItem)
			CGIString = ConvertCgiToTheRightForm(CGIString)
			rsrpServing = measReportItem.GetRsrp().GetValue()
			rsrpTable[CGIString] = measReportItem.GetRsrp().GetValue()
			cgiTable[CGIString] = measReportItem.GetCgi()
		} else {
			CGIString := GetCGIFromMeasReportItem(measReportItem)
			CGIString = ConvertCgiToTheRightForm(CGIString)
			rsrpNeighbors[CGIString] = measReportItem.GetRsrp().GetValue()
			rsrpTable[CGIString] = measReportItem.GetRsrp().GetValue()
			cgiTable[CGIString] = measReportItem.GetCgi()
			cell := c.GetCell(ctx, CGIString)
			if cell == nil {
				cell = c.CreateCell(ctx, CGIString, measReportItem.GetCgi())
				c.SetCell(ctx, cell)
			}
		}
	}

	return rsrpServing, rsrpNeighbors, rsrpTable, cgiTable
}

func (c *NodeManager) GetFiveQiFromReport(ctx context.Context, servingNci uint64, measReport []*e2sm_mho.E2SmMhoMeasurementReportItem) int64 {
	var fiveQiServing int64

	for _, measReportItem := range measReport {

		if GetNciFromCellGlobalID(measReportItem.GetCgi()) == servingNci {
			fiveQi := measReportItem.GetFiveQi()
			if fiveQi != nil {
				fiveQiServing = int64(fiveQi.GetValue())
				if fiveQiServing > 127 {
					fiveQiServing = 2
				} else {
					fiveQiServing = 1
				}
			} else {
				fiveQiServing = -1
			}
		}
	}

	return fiveQiServing
}

func (c *NodeManager) SetUeRrcState(ctx context.Context, ueData *UeData, newRrcState string, cgi string, cgiObject *e2sm_v2_ies.Cgi) {
	oldRrcState := ueData.RrcState

	if oldRrcState == e2sm_mho.Rrcstatus_name[int32(e2sm_mho.Rrcstatus_RRCSTATUS_CONNECTED)] &&
		newRrcState == e2sm_mho.Rrcstatus_name[int32(e2sm_mho.Rrcstatus_RRCSTATUS_IDLE)] {
		c.DetachUe(ctx, ueData)
	} else if oldRrcState == e2sm_mho.Rrcstatus_name[int32(e2sm_mho.Rrcstatus_RRCSTATUS_IDLE)] &&
		newRrcState == e2sm_mho.Rrcstatus_name[int32(e2sm_mho.Rrcstatus_RRCSTATUS_CONNECTED)] {
		c.AttachUe(ctx, ueData, cgi, cgiObject)
	}
	ueData.RrcState = newRrcState
}

func (m *NodeManager) SendThroughInformationChannel(message *E2NodeIndication) {
	m.informationChannel <- message
}

func (m *NodeManager) GetUEs(ctx context.Context) map[string]UeData {
	output := make(map[string]UeData)
	chEntries := make(chan *store.Entry, 1024)
	err := m.ueStore.Entries(ctx, chEntries)
	if err != nil {
		log.Warn(err)
		return output
	}
	for entry := range chEntries {
		ueData := entry.Value.(UeData)
		output[ueData.UeID] = ueData
	}
	return output
}

func (m *NodeManager) GetCells(ctx context.Context) map[string]CellData {
	output := make(map[string]CellData)
	chEntries := make(chan *store.Entry, 1024)
	err := m.cellStore.Entries(ctx, chEntries)
	if err != nil {
		log.Warn(err)
		return output
	}
	for entry := range chEntries {
		cellData := entry.Value.(CellData)
		output[cellData.CGIString] = cellData
	}
	return output
}

func (m *NodeManager) GetPolicies(ctx context.Context) map[string]PolicyData {
	output := make(map[string]PolicyData)
	chEntries := make(chan *store.Entry, 1024)
	err := m.onosPolicyStore.Entries(ctx, chEntries)
	if err != nil {
		log.Warn(err)
		return output
	}
	for entry := range chEntries {
		policyData := entry.Value.(PolicyData)
		output[policyData.Key] = policyData
	}
	return output
}

func (c *NodeManager) CreateUe(ctx context.Context, ueID string) *UeData {
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

func (c *NodeManager) GetUe(ctx context.Context, ueID string) *UeData {
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

func (c *NodeManager) SetUe(ctx context.Context, ueData *UeData) {
	_, err := c.ueStore.Put(ctx, ueData.UeID, *ueData, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *NodeManager) AttachUe(ctx context.Context, ueData *UeData, cgi string, cgiObject *e2sm_v2_ies.Cgi) {

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

func (c *NodeManager) DetachUe(ctx context.Context, ueData *UeData) {
	for _, cell := range c.cells {
		delete(cell.Ues, ueData.UeID)
	}
}

func (c *NodeManager) CreateCell(ctx context.Context, cgi string, cgiObject *e2sm_v2_ies.Cgi) *CellData {
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

func (c *NodeManager) GetCell(ctx context.Context, cgi string) *CellData {
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

func (c *NodeManager) SetCell(ctx context.Context, cellData *CellData) {
	if len(cellData.CGIString) == 0 {
		panic("bad data")
	}
	_, err := c.cellStore.Put(ctx, cellData.CGIString, *cellData, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *NodeManager) CreatePolicy(ctx context.Context, key string, policy *policyAPI.API) *PolicyData {
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

func (c *NodeManager) GetPolicy(ctx context.Context, key string) *PolicyData {
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

func (c *NodeManager) SetPolicy(ctx context.Context, key string, policy *PolicyData) {
	_, err := c.onosPolicyStore.Put(ctx, key, *policy, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *NodeManager) DeletePolicy(ctx context.Context, key string) {
	if err := c.onosPolicyStore.Delete(ctx, key); err != nil {
		panic("bad data")
	} else {
		delete(c.policies, key)
	}
}

func (c *NodeManager) GetPolicyStore() *store.Store {
	return &c.onosPolicyStore
}

func ConvertCgiToTheRightForm(cgi string) string {
	return cgi[0:8] + cgi[13:14] + cgi[10:12] + cgi[8:10] + cgi[14:15] + cgi[12:13]
}
