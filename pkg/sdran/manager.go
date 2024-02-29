// SPDX-FileCopyrightText: 2022-present Intel Corporation
// SPDX-FileCopyrightText: 2019-present Open Networking Foundation <info@opennetworking.org>
// SPDX-FileCopyrightText: 2019-present Rimedo Labs
//
// SPDX-License-Identifier: Apache-2.0
// Created by RIMEDO-Labs team
// Based on work of Open Networking Foundation team

package sdran

import (
	"context"
	"sync"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/controller"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/policy"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/rnib"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/southbound/e2"
	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/logging/service"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	nbi "github.com/onosproject/onos-mho/pkg/northbound"
	"github.com/onosproject/onos-mho/pkg/store"
)

var log = logging.GetLogger("rimedo-ts", "sdran", "manager")

type Config struct {
	AppID         string
	E2tAddress    string
	E2tPort       int
	TopoAddress   string
	TopoPort      int
	SMName        string
	SMVersion     string
	RansimAddress string
	RansimPort    int
}

func NewManager(config Config, flag bool) *Manager {

	ueStore := store.NewStore()
	cellStore := store.NewStore()
	metricStore := store.NewStore()
	onosPolicyStore := store.NewStore()

	policyMap := make(map[string]*monitoring.PolicyData)

	nodeManager := monitoring.NewNodeManager(ueStore, cellStore, onosPolicyStore, policyMap)

	// ransimApiHandler, _ := ransimapi.NewHandler(config.RansimAddress+":"+strconv.Itoa(config.RansimPort), nodeManager)

	options := e2.Options{
		AppID:       config.AppID,
		E2tAddress:  config.E2tAddress,
		E2tPort:     config.E2tPort,
		TopoAddress: config.TopoAddress,
		TopoPort:    config.TopoPort,
		SMName:      config.SMName,
		SMVersion:   config.SMVersion,
	}

	e2Manager, err := e2.NewManager(options, metricStore, nodeManager, flag)
	if err != nil {
		log.Warn(err)
	}

	restManager := e2.NewRestManager(ueStore, cellStore)

	manager := &Manager{
		e2Manager:      e2Manager,
		restApiManager: restManager,
		monitor:        nil,
		mhoCtrl:        controller.NewMHOController(metricStore),
		policyManager:  policy.NewPolicyManager(&policyMap),
		nodeManager:    nodeManager,
		metricStore:    metricStore,
		services:       []service.Service{},
		mutex:          sync.RWMutex{},
		topoIDsEnabled: flag,
		// ransimApiHandler: ransimApiHandler,
	}
	return manager
}

type Manager struct {
	e2Manager      e2.Manager
	restApiManager *e2.RestManager
	monitor        *monitoring.Monitor
	mhoCtrl        *controller.MHOController
	policyManager  *policy.PolicyManager
	nodeManager    *monitoring.NodeManager
	metricStore    store.Store
	services       []service.Service
	mutex          sync.RWMutex
	topoIDsEnabled bool
	// ransimApiHandler ransimapi.Handler
}

func (m *Manager) Run(ctx context.Context) {
	if err := m.start(ctx); err != nil {
		log.Fatal("Unable to run Manager", err)
	}
}

func (m *Manager) start(ctx context.Context) error {
	m.startNorthboundServer()
	err := m.e2Manager.Start()
	if err != nil {
		log.Warn(err)
		return err
	}
	go m.mhoCtrl.Run(ctx)
	go func() {
		for {
			err = m.restApiManager.Run(ctx)
			if err != nil {
				break
			}
		}
	}()
	if err != nil {
		return err
	}
	// time.Sleep(30 * time.Second)
	// go func() {
	// 	for {
	// 		time.Sleep(1 * time.Second)
	// 		_ = m.ransimApiHandler.GetUesParameters(ctx)
	// 	}
	// }()

	return nil
}

func (m *Manager) startNorthboundServer() error {

	s := northbound.NewServer(northbound.NewServerCfg(
		"",
		"",
		"",
		int16(5150),
		true,
		northbound.SecurityConfig{}))

	for i := range m.services {
		s.AddService(m.services[i])
	}
	s.AddService(nbi.NewService(m.metricStore))

	doneCh := make(chan error)
	go func() {
		err := s.Serve(func(started string) {
			close(doneCh)
		})
		if err != nil {
			doneCh <- err
		}
	}()
	return <-doneCh
}

func (m *Manager) AddService(service service.Service) {

	m.services = append(m.services, service)

}

func (m *Manager) GetUes() map[string]*e2.UeData {

	return m.restApiManager.GetUes()

}

func (m *Manager) GetCells() map[string]*e2.CellData {

	return m.restApiManager.GetCells()

}

func (m *Manager) GetPolicies(ctx context.Context) map[string]monitoring.PolicyData {

	return m.nodeManager.GetPolicies(ctx)

}

func (m *Manager) GetCellTypes(ctx context.Context) map[string]rnib.Cell {

	return m.e2Manager.GetCellTypes(ctx)

}

func (m *Manager) SetCellType(ctx context.Context, cellID string, cellType string) error {

	return m.e2Manager.SetCellType(ctx, cellID, cellType)

}

func (m *Manager) GetCell(ctx context.Context, cgi string) (*e2.CellData, error) {

	return m.restApiManager.GetCell(ctx, cgi)

}

func (m *Manager) SetCell(ctx context.Context, cell *e2.CellData) error {

	return m.restApiManager.SetCell(ctx, cell)

}

func (m *Manager) AttachUe(ctx context.Context, ue *e2.UeData, cgi string) error {

	return m.restApiManager.AttachUe(ctx, ue, cgi)

}

func (m *Manager) GetUe(ctx context.Context, ueID string) (*e2.UeData, error) {

	return m.restApiManager.GetUe(ctx, ueID)

}

func (m *Manager) SetUe(ctx context.Context, ueData *e2.UeData) error {

	return m.restApiManager.SetUe(ctx, ueData)

}

func (m *Manager) CreatePolicy(ctx context.Context, key string, policy *policyAPI.API) *monitoring.PolicyData {

	return m.nodeManager.CreatePolicy(ctx, key, policy)

}

func (m *Manager) GetPolicy(ctx context.Context, key string) *monitoring.PolicyData {

	return m.nodeManager.GetPolicy(ctx, key)

}

func (m *Manager) SetPolicy(ctx context.Context, key string, policy *monitoring.PolicyData) {

	m.nodeManager.SetPolicy(ctx, key, policy)

}

func (m *Manager) DeletePolicy(ctx context.Context, key string) {

	m.nodeManager.DeletePolicy(ctx, key)

}

func (m *Manager) GetPolicyStore() *store.Store {

	return m.nodeManager.GetPolicyStore()

}

func (m *Manager) GetRestApiManager() *e2.RestManager {

	return m.restApiManager

}

func (m *Manager) GetPolicyManager() *policy.PolicyManager {

	return m.policyManager

}

// func (m *Manager) SwitchUeBetweenCells(ctx context.Context, ueID string, targetCellCGI string) error {

// 	m.mutex.Lock()
// 	defer m.mutex.Unlock()

// 	chosenUe := m.GetUe(ctx, ueID)

// 	if shouldBeSwitched(chosenUe, targetCellCGI) {

// 		key := idutil.GenerateGnbUeIDString(chosenUe.UeID.GetGNbUeid())

// 		if m.metricStore.HasEntry(ctx, key) {

// 			targetCell := m.GetCell(ctx, targetCellCGI)
// 			servingCell := m.GetCell(ctx, chosenUe.CGI)

// 			if targetCell == nil || servingCell == nil {
// 				return fmt.Errorf("Target or Serving Cell is not registered in the store yet!")
// 			}

// 			targetCell.CumulativeHandoversOut++
// 			servingCell.CumulativeHandoversIn++

// 			m.SetCell(ctx, targetCell)
// 			m.SetCell(ctx, servingCell)

// 			v, err := m.metricStore.Get(ctx, key)
// 			if err != nil {
// 				return err
// 			}
// 			nv := v.Value.(*store.MetricValue)

// 			// log.Debugf("State changed for %v from %v to %v", key, nv.State.String(), store.StateCreated)
// 			metricValue := &store.MetricValue{
// 				RawUEID:       chosenUe.UeID,
// 				TgtCellID:     targetCell.CGI,
// 				State:         store.StateCreated,
// 				CallProcessID: nv.CallProcessID,
// 				E2NodeID:      targetCell.E2NodeID,
// 			}
// 			_, err = m.metricStore.Put(ctx, key, metricValue, store.StateCreated)
// 			if err != nil {
// 				return err
// 			}
// 			tgtCellID := m.ConvertCgiToTheRightForm(targetCell.CGI)
// 			m.nodeManager.AttachUe(ctx, chosenUe, tgtCellID, targetCell.E2NodeID)

// 		}

// 	}
// 	return nil
// }

// func shouldBeSwitched(ue *monitoring.UeData, cgi string) bool {

// 	servingCgi := ue.CGI
// 	if servingCgi == cgi {
// 		return false
// 	}
// 	return true

// }

// func (m *Manager) ConvertCgiToTheRightForm(cgi string) string {
// 	if m.topoIDsEnabled {
// 		return cgi[0:6] + cgi[14:15] + cgi[12:14] + cgi[10:12] + cgi[8:10] + cgi[6:8]
// 	}
// 	return cgi
// }
