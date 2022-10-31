// Created by RIMEDO-Labs team
// based on onosproject/onos-mho/pkg/manager/manager.go
package sdran

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/controller"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/policy"
	ransimapi "github.com/RIMEDO-Labs/rimedo-ts/pkg/ransim-api"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/rnib"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/southbound/e2"
	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/logging/service"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	nbi "github.com/onosproject/onos-mho/pkg/northbound"
	"github.com/onosproject/onos-mho/pkg/store"
	idutil "github.com/onosproject/onos-mho/pkg/utils/id"
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

	ransimApiHandler, _ := ransimapi.NewHandler(config.RansimAddress+":"+strconv.Itoa(config.RansimPort), nodeManager)

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

	manager := &Manager{
		e2Manager:        e2Manager,
		monitor:          nil,
		mhoCtrl:          controller.NewMHOController(metricStore),
		policyManager:    policy.NewPolicyManager(&policyMap),
		nodeManager:      nodeManager,
		metricStore:      metricStore,
		services:         []service.Service{},
		mutex:            sync.RWMutex{},
		topoIDsEnabled:   flag,
		ransimApiHandler: ransimApiHandler,
	}
	return manager
}

type Manager struct {
	e2Manager        e2.Manager
	monitor          *monitoring.Monitor
	mhoCtrl          *controller.MHOController
	policyManager    *policy.PolicyManager
	nodeManager      *monitoring.NodeManager
	metricStore      store.Store
	services         []service.Service
	mutex            sync.RWMutex
	topoIDsEnabled   bool
	ransimApiHandler ransimapi.Handler
}

func (m *Manager) Run(ctx context.Context) {
	if err := m.start(ctx); err != nil {
		log.Fatal("Unable to run Manager", err)
	}
}

func (m *Manager) start(ctx context.Context) error {
	// ctx := context.Background()
	m.startNorthboundServer()
	err := m.e2Manager.Start()
	if err != nil {
		log.Warn(err)
		return err
	}
	go m.mhoCtrl.Run(ctx)

	time.Sleep(30 * time.Second)
	go func() {
		for {
			time.Sleep(1 * time.Second)
			// log.Debug()
			// log.Debug()
			// log.Debug("CELL STORE: ", m.GetCells(ctx))
			// log.Debug()
			// log.Debug()
			_ = m.ransimApiHandler.GetUesParameters(ctx)
			// log.Warnf("Warning: " + fmt.Sprint(err))
		}
	}()

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

func (m *Manager) GetUEs(ctx context.Context) map[string]monitoring.UeData {
	return m.nodeManager.GetUEs(ctx)
}

func (m *Manager) GetCells(ctx context.Context) map[string]monitoring.CellData {
	return m.nodeManager.GetCells(ctx)
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

func (m *Manager) GetCell(ctx context.Context, CGI string) *monitoring.CellData {

	return m.nodeManager.GetCell(ctx, CGI)

}

func (m *Manager) SetCell(ctx context.Context, cell *monitoring.CellData) {

	m.nodeManager.SetCell(ctx, cell)

}

func (m *Manager) AttachUe(ctx context.Context, ue *monitoring.UeData, CGI string, e2NodeID topoapi.ID) {

	m.nodeManager.AttachUe(ctx, ue, CGI, e2NodeID)

}

func (m *Manager) GetUe(ctx context.Context, ueID string) *monitoring.UeData {

	return m.nodeManager.GetUe(ctx, ueID)

}

func (m *Manager) SetUe(ctx context.Context, ueData *monitoring.UeData) {

	m.nodeManager.SetUe(ctx, ueData)

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

// func (m *Manager) GetControlChannelsMap(ctx context.Context) map[string]chan *e2api.ControlMessage {
// 	return m.ctrlReqChs
// }

func (m *Manager) GetPolicyManager() *policy.PolicyManager {
	return m.policyManager
}

func (m *Manager) SwitchUeBetweenCells(ctx context.Context, ueID string, targetCellCGI string) error {

	m.mutex.Lock()
	defer m.mutex.Unlock()

	chosenUe := m.GetUe(ctx, ueID)
	// chosenUe := availableUes[ueID]

	if shouldBeSwitched(chosenUe, targetCellCGI) {

		key := idutil.GenerateGnbUeIDString(chosenUe.UeID.GetGNbUeid())

		if m.metricStore.HasEntry(ctx, key) {

			// log.Debug()
			// log.Debug()
			// log.Debug("TARGET: ", targetCellCGI)
			// log.Debug("CURRENT: ", chosenUe.CGI)
			// log.Debug()
			// log.Debug()

			// cells := m.GetCells(ctx)

			// log.Debug("CELL STORE: ", cells)

			targetCell := m.GetCell(ctx, targetCellCGI)
			servingCell := m.GetCell(ctx, chosenUe.CGI)

			if targetCell == nil || servingCell == nil {
				return fmt.Errorf("Target or Serving Cell is not registered in the store yet!")
			}

			targetCell.CumulativeHandoversOut++
			servingCell.CumulativeHandoversIn++

			// m.AttachUe(ctx, chosenUe, targetCellCGI, targetCell.E2NodeID)

			m.SetCell(ctx, targetCell)
			m.SetCell(ctx, servingCell)

			v, err := m.metricStore.Get(ctx, key)
			if err != nil {
				return err
			}
			nv := v.Value.(*store.MetricValue)

			log.Debugf("State changed for %v from %v to %v", key, nv.State.String(), store.StateCreated)
			metricValue := &store.MetricValue{
				RawUEID:       chosenUe.UeID,
				TgtCellID:     targetCell.CGI,
				State:         store.StateCreated,
				CallProcessID: nv.CallProcessID,
				E2NodeID:      targetCell.E2NodeID,
			}
			_, err = m.metricStore.Put(ctx, key, metricValue, store.StateCreated)
			if err != nil {
				return err
			}
			// ueData := m.nodeManager.GetUe(ctx, chosenUe.UeID.GetGNbUeid().AmfUeNgapId.String())
			tgtCellID := m.ConvertCgiToTheRightForm(targetCell.CGI)
			m.nodeManager.AttachUe(ctx, chosenUe, tgtCellID, targetCell.E2NodeID)

		}
		// controlChannel := m.ctrlReqChs[chosenUe.E2NodeID]

		// controlHandler := &control.E2SmMhoControlHandler{
		// 	NodeID:            chosenUe.E2NodeID,
		// 	ControlAckRequest: e2tAPI.ControlAckRequest_NO_ACK,
		// }

		// ueIDnum, err := strconv.Atoi(chosenUe.UeID)
		// if err != nil {
		// 	log.Errorf("SendHORequest() failed to convert string %v to decimal number - assumption is not satisfied (UEID is a decimal number): %v", chosenUe.UeID, err)
		// }

		// ueIdentity, err := pdubuilder.CreateUeIDGNb(int64(ueIDnum), []byte{0xAA, 0xBB, 0xCC}, []byte{0xDD}, []byte{0xCC, 0xC0}, []byte{0xFC})
		// if err != nil {
		// 	log.Errorf("SendHORequest() Failed to create UEID: %v", err)
		// }

		// servingPlmnIDBytes := servingCell.CGI.GetNRCgi().GetPLmnidentity().GetValue()
		// servingNCI := servingCell.CGI.GetNRCgi().GetNRcellIdentity().GetValue().GetValue()
		// servingNCILen := servingCell.CGI.GetNRCgi().GetNRcellIdentity().GetValue().GetLen()

		// go func() {
		// 	if controlHandler.ControlHeader, err = controlHandler.CreateMhoControlHeader(servingNCI, servingNCILen, 1, servingPlmnIDBytes); err == nil {

		// 		if controlHandler.ControlMessage, err = controlHandler.CreateMhoControlMessage(servingCell.CGI, ueIdentity, targetCell.CGI); err == nil {

		// 			if controlRequest, err := controlHandler.CreateMhoControlRequest(); err == nil {

		// 				controlChannel <- controlRequest
		// 				log.Infof("\nCONTROL MESSAGE: UE [ID:%v, 5QI:%v] switched between CELLs [CGI:%v -> CGI:%v]\n", chosenUe.UeID, chosenUe.FiveQi, servingCell.CGIString, targetCell.CGIString)

		// 			} else {
		// 				log.Warn("Control request problem :(", err)
		// 			}
		// 		} else {
		// 			log.Warn("Control message problem :(", err)
		// 		}
		// 	} else {
		// 		log.Warn("Control header problem :(", err)
		// 	}
		// }()

	}
	return nil
}

func shouldBeSwitched(ue *monitoring.UeData, cgi string) bool {

	servingCgi := ue.CGI
	if servingCgi == cgi {
		return false
	}
	return true

}

func (m *Manager) ConvertCgiToTheRightForm(cgi string) string {
	if m.topoIDsEnabled {
		return cgi[0:6] + cgi[14:15] + cgi[12:14] + cgi[10:12] + cgi[8:10] + cgi[6:8]
	}
	return cgi
}
