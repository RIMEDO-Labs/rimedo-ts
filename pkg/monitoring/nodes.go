package monitoring

import (
	"context"

	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	e2sm_v2_ies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-v2-ies"
	e2smrcies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_rc/v1/e2sm-rc-ies"
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
