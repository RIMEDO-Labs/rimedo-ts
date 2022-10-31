package monitoring

import (
	"context"
	"fmt"

	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	e2smcommonies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_rc/v1/e2sm-common-ies"
	"github.com/onosproject/onos-mho/pkg/store"
)

func NewNodeManager(ueStore store.Store, cellStore store.Store, onosPolicyStore store.Store, policies map[string]*PolicyData) *NodeManager {
	return &NodeManager{
		ueStore:         ueStore,
		cellStore:       cellStore,
		onosPolicyStore: onosPolicyStore,
		cells:           make(map[string]*CellData),
		policies:        policies,
	}
}

type NodeManager struct {
	ueStore         store.Store
	cellStore       store.Store
	onosPolicyStore store.Store
	cells           map[string]*CellData
	policies        map[string]*PolicyData
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
		output[ueData.UeKey] = ueData
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
		output[cellData.CGI] = cellData
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

func (c *NodeManager) CreateUe(ctx context.Context, ueID *e2smcommonies.Ueid) *UeData {
	key := fmt.Sprint(ueID.GetGNbUeid().AmfUeNgapId.Value)
	for len(key) < 16 {
		key = "0" + key
	}
	// log.Debug("Orignal: ", ueID.GetGNbUeid().AmfUeNgapId.Value)
	// log.Debug("String: ", key)
	if len(key) == 0 {
		panic("bad data")
	}

	ueData := &UeData{
		UeKey:       key,
		UeID:        ueID,
		CGI:         "",
		RrcState:    "CONNECTED",
		FiveQi:      -1,
		RsrpServing: -1,
		RsrpTable:   make(map[string]int32),
	}
	_, err := c.ueStore.Put(ctx, key, *ueData, store.Done)
	if err != nil {
		log.Warn(err)
	}

	return ueData
}

func (c *NodeManager) GetUe(ctx context.Context, ueKey string) *UeData {
	var ueData *UeData
	u, err := c.ueStore.Get(ctx, ueKey)
	if err != nil || u == nil {
		return nil
	}
	t := u.Value.(UeData)
	ueData = &t
	if ueData.UeKey != ueKey {
		panic("bad data")
	}

	return ueData
}

func (c *NodeManager) SetUe(ctx context.Context, ueData *UeData) {
	_, err := c.ueStore.Put(ctx, ueData.UeKey, *ueData, store.Done)
	if err != nil {
		panic("bad data")
	}
}

func (c *NodeManager) AttachUe(ctx context.Context, ueData *UeData, cgi string, e2NodeID topoapi.ID) {

	c.DetachUe(ctx, ueData)

	ueData.CGI = cgi
	c.SetUe(ctx, ueData)
	cell := c.GetCell(ctx, cgi)
	if cell == nil {
		cell = c.CreateCell(ctx, cgi, e2NodeID)
	}
	cell.Ues[ueData.UeKey] = ueData
	c.SetCell(ctx, cell)
}

func (c *NodeManager) DetachUe(ctx context.Context, ueData *UeData) {
	for _, cell := range c.cells {
		// log.Debug()
		// log.Debug()
		// log.Debug("CELL: ", cell)
		// log.Debug("UE: ", ueData)
		// log.Debug()
		// log.Debug()
		delete(cell.Ues, ueData.UeKey)
	}
}

func (c *NodeManager) CreateCell(ctx context.Context, cgi string, e2NodeID topoapi.ID) *CellData {
	if len(cgi) == 0 {
		panic("bad data")
	}
	cellData := &CellData{
		CGI:                    cgi,
		E2NodeID:               e2NodeID,
		CumulativeHandoversIn:  0,
		CumulativeHandoversOut: 0,
		Ues:                    make(map[string]*UeData),
	}
	_, err := c.cellStore.Put(ctx, cgi, *cellData, store.Done)
	if err != nil {
		panic("bad data")
	}
	c.cells[cellData.CGI] = cellData
	return cellData
}

func (c *NodeManager) GetCell(ctx context.Context, cgi string) *CellData {
	var cellData *CellData
	cell, err := c.cellStore.Get(ctx, cgi)
	if err != nil || cell == nil {
		return nil
	}
	t := cell.Value.(CellData)
	if t.CGI != cgi {
		panic("bad data")
	}
	cellData = &t
	return cellData
}

func (c *NodeManager) SetCell(ctx context.Context, cellData *CellData) {
	if len(cellData.CGI) == 0 {
		panic("bad data")
	}
	_, err := c.cellStore.Put(ctx, cellData.CGI, *cellData, store.Done)
	if err != nil {
		panic("bad data")
	}
	c.cells[cellData.CGI] = cellData
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

	// log.Debugf("Key: ", key)
	// log.Debugf("PolicyData: ", policyData)

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
	c.policies[policy.Key] = policy
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

// func ConvertCgiToTheRightForm(cgi string) string {
// 	return cgi[0:8] + cgi[13:14] + cgi[10:12] + cgi[8:10] + cgi[14:15] + cgi[12:13]
// }
