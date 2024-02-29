// SPDX-FileCopyrightText: 2022-present Intel Corporation
// SPDX-FileCopyrightText: 2019-present Open Networking Foundation <info@opennetworking.org>
// SPDX-FileCopyrightText: 2019-present Rimedo Labs
//
// SPDX-License-Identifier: Apache-2.0
// Created by RIMEDO-Labs team
// Based on work of Open Networking Foundation team

package manager

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/northbound/a1"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/sdran"
	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	topoAPI "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/logging"
)

var log = logging.GetLogger("rimedo-ts", "ts-manager")
var logLength = 150
var nodesLogLen = 0
var policiesLogLen = 0

func NewManager(sdranConfig sdran.Config, a1Config a1.Config, flag bool) *Manager {

	sdranManager := sdran.NewManager(sdranConfig, flag)

	a1PolicyTypes := make([]*topoAPI.A1PolicyType, 0)
	a1Policy := &topoAPI.A1PolicyType{
		Name:        topoAPI.PolicyTypeName(a1Config.PolicyName),
		Version:     topoAPI.PolicyTypeVersion(a1Config.PolicyVersion),
		ID:          topoAPI.PolicyTypeID(a1Config.PolicyID),
		Description: topoAPI.PolicyTypeDescription(a1Config.PolicyDescription),
	}
	a1PolicyTypes = append(a1PolicyTypes, a1Policy)

	a1Manager, err := a1.NewManager("", "", "", a1Config.A1tPort, sdranConfig.AppID, a1PolicyTypes)
	if err != nil {
		log.Warn(err)
	}

	manager := &Manager{
		sdranManager:   sdranManager,
		a1Manager:      *a1Manager,
		topoIDsEnabled: flag,
		mutex:          sync.RWMutex{},
	}
	return manager
}

type Manager struct {
	sdranManager   *sdran.Manager
	a1Manager      a1.Manager
	topoIDsEnabled bool
	mutex          sync.RWMutex
}

func (m *Manager) Run() {

	if err := m.start(); err != nil {
		log.Fatal("Unable to run Manager", err)
	}

}

func (m *Manager) Close() {
	m.a1Manager.Close(context.Background())
}

func (m *Manager) start() error {

	ctx := context.Background()

	policyMap := make(map[string][]byte)

	policyChange := make(chan bool)

	restApiManager := m.sdranManager.GetRestApiManager()

	m.sdranManager.AddService(a1.NewA1EIService())
	m.sdranManager.AddService(a1.NewA1PService(&policyMap, policyChange))

	m.sdranManager.Run(ctx)

	m.a1Manager.Start()

	go func() {
		for range policyChange {
			log.Debug("")
			drawWithLine("POLICY STORE CHANGED!", logLength)
			log.Debug("")
			if err := m.updatePolicies(ctx, policyMap); err != nil {
				log.Warn("Some problems occured when updating Policy store!")
			}
			log.Debug("")
			m.checkPolicies(ctx, true, true, true)
		}

	}()
	flag := true
	show := false
	prepare := false
	counter := 0
	delay := 3
	time.Sleep(5 * time.Second)
	log.Info("\n\n\n\n\n\n\n\n\n\n")
	go func() {
		for {
			time.Sleep(1 * time.Second)
			counter++
			if counter == delay {
				compareLengths()
				counter = 0
				show = true
			} else if counter == delay-1 {
				prepare = true
			} else {
				show = false
				prepare = false
			}
			m.checkPolicies(ctx, flag, show, prepare)
			err := restApiManager.PrintUes(ctx, show)
			if err != nil {
				log.Error("Something went wrong with printing UEs")
			}
			err = restApiManager.PrintCells(ctx, show)
			if err != nil {
				log.Error("Something went wrong with printing UEs")
			}
			flag = false
		}
	}()

	return nil
}

func (m *Manager) updatePolicies(ctx context.Context, policyMap map[string][]byte) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	policies := m.sdranManager.GetPolicies(ctx)
	for k := range policies {
		if _, ok := policyMap[k]; !ok {
			m.sdranManager.DeletePolicy(ctx, k)
			log.Infof("POLICY MESSAGE: Policy [ID:%v] deleted\n", k)
		}
	}
	for i := range policyMap {
		r, err := policyAPI.UnmarshalAPI(policyMap[i])
		if err == nil {
			policyObject := m.sdranManager.CreatePolicy(ctx, i, &r)
			info := fmt.Sprintf("POLICY MESSAGE: Policy [ID:%v] applied -> ", policyObject.Key)
			previous := false
			if policyObject.API.Scope.SliceID != nil {
				info = info + fmt.Sprintf("Slice [SD:%v, SST:%v, PLMN:(MCC:%v, MNC:%v)]", *policyObject.API.Scope.SliceID.SD, policyObject.API.Scope.SliceID.Sst, policyObject.API.Scope.SliceID.PlmnID.Mcc, policyObject.API.Scope.SliceID.PlmnID.Mnc)
				previous = true
			}
			if policyObject.API.Scope.UeID != nil {
				if previous {
					info = info + ", "
				}
				ue := *policyObject.API.Scope.UeID
				new_ue := ue
				for i := 0; i < len(ue); i++ {
					if ue[i:i+1] == "0" {
						new_ue = ue[i+1:]
					} else {
						break
					}
				}
				info = info + fmt.Sprintf("UE [ID:%v]", new_ue)
				previous = true
			}
			if policyObject.API.Scope.QosID != nil {
				if previous {
					info = info + ", "
				}
				if policyObject.API.Scope.QosID.QcI != nil {
					info = info + fmt.Sprintf("QoS [QCI:%v]", *policyObject.API.Scope.QosID.QcI)
				}
				if policyObject.API.Scope.QosID.The5QI != nil {
					info = info + fmt.Sprintf("QoS [5QI:%v]", *policyObject.API.Scope.QosID.The5QI)
				}
			}
			if policyObject.API.Scope.CellID != nil {
				if previous {
					info = info + ", "
				}
				info = info + "CELL ["
				if policyObject.API.Scope.CellID.CID.NcI != nil {

					info = info + fmt.Sprintf("NCI:%v, ", *policyObject.API.Scope.CellID.CID.NcI)
				}
				if policyObject.API.Scope.CellID.CID.EcI != nil {

					info = info + fmt.Sprintf("ECI:%v, ", *policyObject.API.Scope.CellID.CID.EcI)
				}
				info = info + fmt.Sprintf("PLMN:(MCC:%v, MNC:%v)]", policyObject.API.Scope.CellID.PlmnID.Mcc, policyObject.API.Scope.CellID.PlmnID.Mnc)
			}
			for i := range policyObject.API.TSPResources {
				info = info + fmt.Sprintf(" - (%v) -", policyObject.API.TSPResources[i].Preference)
				for j := range policyObject.API.TSPResources[i].CellIDList {
					nci := *policyObject.API.TSPResources[i].CellIDList[j].CID.NcI
					plmnId, _ := monitoring.GetPlmnIdFromMccMnc(policyObject.API.TSPResources[i].CellIDList[j].PlmnID.Mcc, policyObject.API.TSPResources[i].CellIDList[j].PlmnID.Mnc, false)
					cgi := m.PlmnIDNciToTopoCGI(plmnId, uint64(nci))
					info = info + fmt.Sprintf(" CELL [CGI:%v],", cgi)
				}
				info = info[0 : len(info)-1]

			}
			info = info + "\n"
			log.Info(info)
		} else {
			log.Warn("Can't unmarshal the JSON file!")
			return err
		}
	}
	return nil
}

func (m *Manager) deployPolicies(ctx context.Context) {
	policyManager := m.sdranManager.GetPolicyManager()
	restApiManager := m.sdranManager.GetRestApiManager()
	ues := m.sdranManager.GetUes()
	keys := make([]string, 0, len(ues))
	for k := range ues {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i := range keys {
		var cellIDs []policyAPI.CellID
		var rsrps []int
		fiveQi, err := strconv.ParseInt(ues[keys[i]].FiveQi, 10, 64)
		if err != nil {
			log.Error("Something went wrong!")
		}
		sst, err := strconv.ParseInt(restApiManager.GetSstSlice(ues[keys[i]].Slice, false), 10, 64)
		if err != nil {
			log.Error("Something went wrong!")
		}
		sd := "456DEF"
		scopeUe := policyAPI.Scope{

			SliceID: &policyAPI.SliceID{
				SD:  &sd,
				Sst: sst,
				PlmnID: policyAPI.PlmnID{
					Mcc: "138", // "314",
					Mnc: "426", // "628",
				},
			},
			UeID: &keys[i],
			QosID: &policyAPI.QosID{
				The5QI: &fiveQi,
			},
		}

		cgiKeys := make([]string, 0, len(ues[keys[i]].RsrpTab))
		for cgi := range ues[keys[i]].RsrpTab {
			cgiKeys = append(cgiKeys, cgi)
		}
		inside := false
		for j := range cgiKeys {

			inside = true
			cgi := cgiKeys[j]
			// nci, plmnId := monitoring.PlmnIDNciFromCGI(cgi)
			tab := make([]string, 0)
			var temp string
			for s := 0; s < len(cgi); s++ {
				// log.Debug(character)
				if cgi[s:s+1] != "/" {
					temp = temp + cgi[s:s+1]
				} else {
					tab = append(tab, temp)
					temp = ""
				}
			}
			tab = append(tab, temp)
			nci, err := strconv.ParseInt(restApiManager.TranslateUtfAscii(tab[1]), 10, 64)
			if err != nil {
				log.Error("Something went wrong!")
			}
			mcc := restApiManager.TranslateUtfAscii(tab[2])
			mnc := restApiManager.TranslateUtfAscii(tab[0])
			// mcc, mnc := monitoring.GetMccMncFromPlmnID(plmnId, false)
			cellID := policyAPI.CellID{
				CID: policyAPI.CID{
					NcI: &nci,
				},
				PlmnID: policyAPI.PlmnID{
					Mcc: mcc,
					Mnc: mnc,
				},
			}

			cellIDs = append(cellIDs, cellID)
			rsrp, err := strconv.ParseFloat(ues[keys[i]].RsrpTab[cgiKeys[j]], 64)
			if err != nil {
				log.Error("Something went wrong!")
			}
			rsrps = append(rsrps, int(rsrp))

		}

		if inside {

			tsResult := policyManager.GetTsResultForUEV2(scopeUe, rsrps, cellIDs)
			// plmnId, err := monitoring.GetPlmnIdFromMccMnc(tsResult.PlmnID.Mcc, tsResult.PlmnID.Mnc, false)

			// if err != nil {
			// 	log.Warnf("Cannot get PLMN ID from these MCC and MNC parameters:%v,%v.", tsResult.PlmnID.Mcc, tsResult.PlmnID.Mnc)
			// } else {
			// 	targetCellCGI := m.PlmnIDNciToTopoCGI(plmnId, uint64(*tsResult.CID.NcI))
			// 	err = m.sdranManager.SwitchUeBetweenCells(ctx, keys[i], targetCellCGI)
			// 	if err != nil {
			// 		log.Error(err)
			// 	}
			// }
			ascii := tsResult.PlmnID.Mnc + "47" + tsResult.PlmnID.Mnc + "47" + fmt.Sprint(tsResult.CID.NcI)
			if len(ascii) > 16 {
				ascii = ascii[16-len(ascii):]
			} else {
				for i := 0; i < 16-len(ascii); i++ {
					ascii = "0" + ascii
				}
			}
			targetCellCGI := restApiManager.GetUtfAscii(ascii, false, true)
			err := restApiManager.HandoverControl(ctx, keys[i], targetCellCGI)
			if err != nil {
				log.Error("Something went wrong with TS HO!")
			}
		}

		cellIDs = nil
		rsrps = nil

	}

}

func (m *Manager) checkPolicies(ctx context.Context, defaultFlag bool, showFlag bool, prepareFlag bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	policyLen := 0
	policies := m.sdranManager.GetPolicies(ctx)
	keys := make([]string, 0, len(policies))
	for k := range policies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if defaultFlag && (len(policies) == 0) {
		log.Infof("POLICY MESSAGE: Default policy applied\n")
	}
	if prepareFlag && len(policies) != 0 {
		if showFlag {
			log.Debug("")
			drawWithLine("POLICIES", logLength)
		}
		for _, key := range keys {
			policyObject := policies[key]
			info := fmt.Sprintf("ID:%v POLICY: {", policyObject.Key)
			previous := false
			if policyObject.API.Scope.SliceID != nil {
				info = info + fmt.Sprintf("Slice [SD:%v, SST:%v, PLMN:(MCC:%v, MNC:%v)]", *policyObject.API.Scope.SliceID.SD, policyObject.API.Scope.SliceID.Sst, policyObject.API.Scope.SliceID.PlmnID.Mcc, policyObject.API.Scope.SliceID.PlmnID.Mnc)
				previous = true
			}
			if policyObject.API.Scope.UeID != nil {
				if previous {
					info = info + ", "
				}
				ue := *policyObject.API.Scope.UeID
				new_ue := ue
				for i := 0; i < len(ue); i++ {
					if ue[i:i+1] == "0" {
						new_ue = ue[i+1:]
					} else {
						break
					}
				}
				info = info + fmt.Sprintf("UE [ID:%v]", new_ue)
				previous = true
			}
			if policyObject.API.Scope.QosID != nil {
				if previous {
					info = info + ", "
				}
				if policyObject.API.Scope.QosID.QcI != nil {
					info = info + fmt.Sprintf("QoS [QCI:%v]", *policyObject.API.Scope.QosID.QcI)
				}
				if policyObject.API.Scope.QosID.The5QI != nil {
					info = info + fmt.Sprintf("QoS [5QI:%v]", *policyObject.API.Scope.QosID.The5QI)
				}
			}
			if policyObject.API.Scope.CellID != nil {
				if previous {
					info = info + ", "
				}
				info = info + "CELL ["
				if policyObject.API.Scope.CellID.CID.NcI != nil {

					info = info + fmt.Sprintf("NCI:%v, ", *policyObject.API.Scope.CellID.CID.NcI)
				}
				if policyObject.API.Scope.CellID.CID.EcI != nil {

					info = info + fmt.Sprintf("ECI:%v, ", *policyObject.API.Scope.CellID.CID.EcI)
				}
				info = info + fmt.Sprintf("PLMN:(MCC:%v, MNC:%v)]", policyObject.API.Scope.CellID.PlmnID.Mcc, policyObject.API.Scope.CellID.PlmnID.Mnc)
			}
			for i := range policyObject.API.TSPResources {
				info = info + fmt.Sprintf(" - (%v) -", policyObject.API.TSPResources[i].Preference)
				for j := range policyObject.API.TSPResources[i].CellIDList {
					nci := *policyObject.API.TSPResources[i].CellIDList[j].CID.NcI
					plmnId, _ := monitoring.GetPlmnIdFromMccMnc(policyObject.API.TSPResources[i].CellIDList[j].PlmnID.Mcc, policyObject.API.TSPResources[i].CellIDList[j].PlmnID.Mnc, false)
					cgi := m.PlmnIDNciToTopoCGI(plmnId, uint64(nci))
					info = info + fmt.Sprintf(" CELL [CGI:%v],", cgi)
				}
				info = info[0 : len(info)-1]

			}
			info = info + "} STATUS: "
			if policyObject.IsEnforced {
				info = info + "ENFORCED"
			} else {
				info = info + "NOT ENFORCED"
			}
			if policyLen < len(info) {
				policyLen = len(info)
			}
			if showFlag {
				log.Debug(info)
			}
		}
		if showFlag {
			log.Debug("")
		}
	}
	m.deployPolicies(ctx)
}

func (m *Manager) changeCellsTypes(ctx context.Context) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	cellTypes := make(map[int]string)
	cellTypes[0] = "Macro"
	cellTypes[1] = "SmallCell"
	for {
		time.Sleep(10 * time.Second)
		cells := m.sdranManager.GetCellTypes(ctx)
		type_id := rand.Intn(len(cellTypes))
		for key, val := range cells {
			_ = val
			err := m.sdranManager.SetCellType(ctx, key, cellTypes[type_id])
			if err != nil {
				log.Warn(err)
			}
			break
		}

	}
}

func (m *Manager) PlmnIDNciToTopoCGI(plmnID uint64, nci uint64) string {
	cgi := strconv.FormatInt(int64(plmnID<<36|(nci&0xfffffffff)), 16)
	if m.topoIDsEnabled {
		cgi = cgi[0:6] + cgi[14:15] + cgi[12:14] + cgi[10:12] + cgi[8:10] + cgi[6:8]
	}
	return cgi
}

func (m *Manager) CgiFromTopoToIndicationFormat(cgi string) string {
	if !m.topoIDsEnabled {
		cgi = cgi[0:6] + cgi[13:15] + cgi[11:13] + cgi[9:11] + cgi[7:9] + cgi[6:7]
	}
	return cgi
}

func drawWithLine(word string, length int) {
	wordLength := len(word)
	diff := length - wordLength
	info := ""
	if diff == length {
		for i := 0; i < diff; i++ {
			info = info + "-"
		}
	} else {
		info = " " + word + " "
		diff -= 2
		for i := 0; i < diff/2; i++ {
			info = "-" + info + "-"
		}
		if diff%2 != 0 {
			info = info + "-"
		}
	}
	log.Debug(info)
}

func compareLengths() {
	temp := nodesLogLen
	if nodesLogLen < policiesLogLen {
		temp = policiesLogLen
	}
	logLength = temp
}

func separateCgi(cgi string) (string, string, string) {

	var mnc string
	var mcc string
	var nci string

	counter := 0
	for i := 0; i < len(cgi); i++ {
		character := cgi[i : i+1]
		if !(character == "/") {
			switch counter {
			case 0:
				mnc = mnc + character
				break
			case 1:
				mcc = mcc + character
				break
			case 2:
				nci = nci + character
				break
			default:
				break
			}
		} else {
			counter++
		}
	}

	return mnc, mcc, nci

}

func separateCgiV2(cgi string) []string {

	re := regexp.MustCompile("[0-9]+")
	return re.FindAllString(cgi, -1)

}
