// Created by Rimedo Labs Team. Copyright © 2019-2024.

package e2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	resty "github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
	"github.com/onosproject/onos-mho/pkg/store"
)

func NewRestManager(ueStore store.Store, cellStore store.Store) *RestManager {

	urlString := ""
	if val := os.Getenv("SUT_IP"); val == "" {
		val = "172.20.14.129"
		urlString = "http://" + val
	}

	const (
		gep = "/sba/influx/query"
		pep = "/sba/commands"
		tep = "/sba/tests"
	)

	sstMap := map[string]string{
		"1": "eMBB",
		"2": "URLLC",
		"3": "MIoT",
		"4": "V2X",
		"5": "HMTC",
		"6": "user",
	}

	sliceMap := map[string]string{
		"eMBB":  "1",
		"URLLC": "2",
		"MIoT":  "3",
		"V2X":   "4",
		"HMTC":  "5",
		"user":  "6",
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary

	client := resty.New().
		SetJSONMarshaler(json.Marshal).
		SetJSONUnmarshaler(json.Unmarshal)

	return &RestManager{
		url:           urlString,
		getEndpoint:   gep,
		postEndpoint:  pep,
		testEndpoint:  tep,
		lastTimestamp: "",
		lastTest:      "",
		sstMap:        sstMap,
		sliceMap:      sliceMap,
		ueAsciiUtf:    make(map[string]string),
		ueUtfAscii:    make(map[string]string),
		cellAsciiUtf:  make(map[string]string),
		cellUtfAscii:  make(map[string]string),
		cellList:      make(map[int]string),
		ueList:        make(map[int]string),
		cellObjects:   make(map[string]*CellData),
		ueObjects:     make(map[string]*UeData),
		cellStore:     cellStore,
		ueStore:       ueStore,
		cellLen:       0,
		ueLen:         0,
		hoActionId:    1,
		client:        client,
	}
}

type RestManager struct {
	url           string
	getEndpoint   string
	postEndpoint  string
	testEndpoint  string
	lastTimestamp string
	lastTest      string
	sstMap        map[string]string
	sliceMap      map[string]string
	ueAsciiUtf    map[string]string
	ueUtfAscii    map[string]string
	cellAsciiUtf  map[string]string
	cellUtfAscii  map[string]string
	cellList      map[int]string
	ueList        map[int]string
	cellObjects   map[string]*CellData
	ueObjects     map[string]*UeData
	cellStore     store.Store
	ueStore       store.Store
	cellLen       int
	ueLen         int
	hoActionId    int
	client        *resty.Client
}

func (m *RestManager) DashMarks(s string, cell bool) string {
	length := 20
	if s != "" {
		if cell {
			if m.cellLen != 0 {
				length = int(math.Abs(float64(m.cellLen) - float64(len(s)) - 2.0))
			}
		} else {
			if m.ueLen != 0 {
				length = int(math.Abs(float64(m.ueLen) - float64(len(s)) - 2.0))
			}
		}

	} else {
		if cell {
			if m.cellLen != 0 {
				length = int(math.Abs(float64(m.cellLen)))
			}
		} else {
			if m.ueLen != 0 {
				length = int(math.Abs(float64(m.ueLen)))
			}
		}
	}
	half := int(math.Ceil(float64(length) / 2.0))
	output := s
	for i := 0; i < half; i++ {
		if i == 0 && s != "" {
			output = " " + output + " "
		}
		output = "-" + output + "-"
	}

	var printLen int
	if cell {
		printLen = m.cellLen
	} else {
		printLen = m.ueLen
	}

	counter := 1
	for printLen != len("|"+output+"|") {
		if printLen > len("|"+output+"|") {
			if counter%2 == 0 {
				output = output + "-"
			} else {
				output = "-" + output
			}
		} else {
			if counter%2 == 0 {
				output = output[:len(output)-1]
			} else {
				output = output[1:]
			}
		}
		counter++
	}

	output = "|" + output + "-|"

	return output
}

func (m *RestManager) TranslateUtfAscii(id string) string {

	tab := make([]byte, utf8.RuneCountInString(id))
	counter := 0
	for _, character := range id {
		tab[counter] = byte(character)
		counter++
	}

	var ascii string
	for _, character := range tab {
		ascii = ascii + fmt.Sprint(character)
	}
	if len(ascii) > 16 {
		ascii = ascii[16-len(ascii):]
	} else {
		for i := 0; i < 16-len(ascii); i++ {
			ascii = "0" + ascii
		}
	}

	return ascii

}

func (m *RestManager) SaveUtfAscii(utf string, ascii string, cell bool) {

	if cell {
		m.cellUtfAscii[utf] = ascii
		m.cellAsciiUtf[ascii] = utf
	} else {
		m.ueUtfAscii[utf] = ascii
		m.ueAsciiUtf[ascii] = utf
	}

}

func (m *RestManager) GetUtfAscii(id string, ascii bool, cell bool) string {

	var output string
	if cell && ascii {
		output = m.cellUtfAscii[id]
	} else if cell && !ascii {
		output = m.cellAsciiUtf[id]
	} else if !cell && ascii {
		output = m.ueUtfAscii[id]
	} else {
		output = m.ueAsciiUtf[id]
	}

	return output

}

func (m *RestManager) GetSstSlice(id string, sst bool) string {

	var output string
	if sst {
		output = m.sstMap[id]
	} else {
		output = m.sliceMap[id]
	}

	return output

}

func (m *RestManager) RequestData(test bool, json interface{}, params string) (*resty.Response, error) {

	var response *resty.Response
	var err error
	if json == nil && !test {
		response, err = m.client.R().
			SetHeader("Content-Type", "application/json").
			SetHeader("Accept", "application/json").
			Get(m.url + m.getEndpoint + params)
	} else if test {
		response, err = m.client.R().
			SetHeader("Content-Type", "application/json").
			SetHeader("Accept", "application/json").
			Get(m.url + m.testEndpoint + params)
	} else {
		response, err = m.client.R().
			SetBody(json).
			SetHeader("Content-Type", "application/json").
			SetHeader("Accept", "application/json").
			Post(m.url + m.postEndpoint + params)
	}
	if err != nil {
		err = errors.New("ERROR: " + fmt.Sprint(err))
		return nil, err
	}
	if response.StatusCode() != 200 {
		err = errors.New("ERROR: " + fmt.Sprint(response.Status()))
		return nil, err
	}

	return response, nil

}

func (m *RestManager) GetLastTest() (string, error) {

	testList, err := m.RequestData(true, nil, "")
	if err != nil {
		return "", err
	}

	var reply map[string]string
	if err := m.client.JSONUnmarshal(testList.Body(), &reply); err != nil {
		_ = errors.New("ERROR: " + fmt.Sprint(err))
	}

	maxVal := -1
	for key := range reply {
		if !strings.Contains(key, "e2-load") {
			val, err := strconv.Atoi(strings.Replace(key, "-simulation", "", -1))
			if err != nil {
				err = errors.New("ERROR: " + fmt.Sprint(err))
				return "", err
			}
			if maxVal < val {
				maxVal = val
			}
		}
	}

	done := false
	for !done {
		params := strings.Replace(reply[fmt.Sprint(maxVal)+"-simulation"], "http:/sba/tests", "", -1)
		data, err := m.RequestData(true, nil, params)
		if err != nil {
			return "", err
		}
		var dataSet map[string]string
		if err := m.client.JSONUnmarshal(data.Body(), &dataSet); err != nil {
			_ = errors.New("ERROR: " + fmt.Sprint(err))
		}
		for rKey, rValue := range dataSet {
			if strings.Contains(rKey, "status") {
				if !strings.Contains(rValue, "STARTING") {
					done = true
				} else {
					maxVal--
				}
			}
		}
	}
	lastTest := fmt.Sprintf("%d-simulation", maxVal)

	return lastTest, nil
}

func (m *RestManager) UpdateData() error {

	var err error
	m.lastTest, err = m.GetLastTest()
	if err != nil {
		return err
	}

	cellParams := fmt.Sprintf("?db=" + m.lastTest + "&q=SELECT+*+FROM+CellReports+GROUP+BY+\"Viavi.Cell.Name\"+ORDER+BY+\"time\"+DESC+LIMIT+1")
	cellResponse, err := m.RequestData(false, nil, cellParams)
	if err != nil {
		return err
	}
	var cellData *ViaviCell
	if err := m.client.JSONUnmarshal(cellResponse.Body(), &cellData); err != nil {
		err = errors.New("ERROR: " + fmt.Sprint(err))
		return err
	}

	ueParams := fmt.Sprintf("?db=" + m.lastTest + "&q=SELECT+*+FROM+UEReports+WHERE+\"report\"+=+'serving'+GROUP+BY+\"Viavi.UE.Name\"+ORDER+BY+\"time\"+DESC+LIMIT+1")
	ueResponse, err := m.RequestData(false, nil, ueParams)
	if err != nil {
		return err
	}
	var ueData *ViaviUE
	if err := m.client.JSONUnmarshal(ueResponse.Body(), &ueData); err != nil {
		err = errors.New("ERROR: " + fmt.Sprint(err))
		return err
	}

	m.lastTimestamp = fmt.Sprint(ueData.Results[0].Series[0].Values[0][0])

	m.cellList = cellData.ListCellIDs()
	m.ueList = ueData.ListUeIDs()

	return nil

}

func (m *RestManager) PrintUes(ctx context.Context, print bool) error {

	if print {
		log.Debug(m.DashMarks("UEs", false))
	}

	values := make([]string, 0, len(m.ueList))
	for _, value := range m.ueList {
		values = append(values, value)
	}
	sort.Strings(values)
	keys := make([]int, 0, len(m.ueList))
	for _, v := range values {
		for key, value := range m.ueList {
			if v == value {
				keys = append(keys, key)
				break
			}
		}
	}

	for _, k := range keys {
		if ueData, err := m.GetUe(ctx, m.ueList[k]); ueData != nil {
			output := fmt.Sprintf(" ID: %s, CGI: %s, RRC: %s, SLICE: %s, 5QI: %s, RSRP:[", ueData.Id, ueData.Cgi, ueData.RrcState, ueData.Slice, ueData.FiveQi)
			flag := false
			var suboutput string
			keyTab := make([]string, 0, len(ueData.RsrpTab))
			for k := range ueData.RsrpTab {
				keyTab = append(keyTab, k)
			}
			sort.Strings(keyTab)
			for _, v := range keyTab {
				if flag {
					suboutput = suboutput + ", "
				}
				suboutput = suboutput + fmt.Sprintf("%s (%s)", v, ueData.RsrpTab[v])
				flag = true
			}
			output = output + suboutput + "]"
			if print {
				log.Debug(output)
			}

			if m.ueLen < len(output) {
				m.ueLen = len(output)
			}
		} else if err != nil {
			return err
		}
	}

	if print {
		log.Debug(m.DashMarks("", false))
		log.Debug("")
	}

	return nil

}

func (m *RestManager) CreateUe(ctx context.Context, id string, cgi string, rrcState string, fiveQi string, slice string, rsrpTab map[string]string) (*UeData, error) {

	asciiId := m.TranslateUtfAscii(id)
	m.SaveUtfAscii(id, asciiId, false)

	ueData := &UeData{
		Id:       id,
		Cgi:      cgi,
		RrcState: rrcState,
		FiveQi:   fiveQi,
		Slice:    slice,
		RsrpTab:  rsrpTab,
	}
	_, err := m.ueStore.Put(ctx, id, *ueData, store.Done)
	if err != nil {
		err = errors.New("ERROR: " + fmt.Sprint(err))
		return nil, err
	}
	m.ueObjects[ueData.Id] = ueData

	return ueData, nil

}

func (m *RestManager) GetUe(ctx context.Context, id string) (*UeData, error) {

	var ueData *UeData
	ue, err := m.ueStore.Get(ctx, id)
	if err != nil || ue == nil {
		return nil, nil
	}
	t := ue.Value.(UeData)
	if t.Id != id {
		err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: wrong input data%s", "!")))
		return nil, err
	}
	ueData = &t

	return ueData, nil

}

func (m *RestManager) SetUe(ctx context.Context, ueData *UeData) error {

	var err error

	if len(ueData.Id) == 0 {
		err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: wrong input data%s", "!")))
		return err
	}

	_, err = m.ueStore.Put(ctx, ueData.Id, *ueData, store.Done)
	if err != nil {
		err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: wrong input data%s", "!")))
		return err
	}

	return nil

}

func (m *RestManager) AttachUe(ctx context.Context, ueData *UeData, cgi string) error {

	m.DetachUe(ctx, ueData)

	ueData.Cgi = cgi
	err := m.SetUe(ctx, ueData)
	if err != nil {
		return err
	}

	cellData, err := m.GetCell(ctx, cgi)
	if err != nil {
		return err
	} else if cellData == nil {
		cellData, err = m.CreateCell(ctx, cgi)
		if err != nil {
			return err
		}
	}
	cellData.UeTab[ueData.Id] = ueData.Id
	err = m.SetCell(ctx, cellData)
	if err != nil {
		return err
	}

	return nil

}

func (m *RestManager) DetachUe(ctx context.Context, ueData *UeData) {

	for _, cellData := range m.cellObjects {
		delete(cellData.UeTab, ueData.Id)
	}

}

func (m *RestManager) MakeUeIdle(ctx context.Context, ueData *UeData) error {

	tab := make(map[string]string)
	tab["-"] = "-"
	ueData.Cgi = ""
	ueData.RrcState = "Idle"
	ueData.FiveQi = ""
	ueData.Slice = ""
	ueData.RsrpTab = tab
	err := m.SetUe(ctx, ueData)
	if err != nil {
		return err
	}

	return nil

}

func (m *RestManager) GetUes() map[string]*UeData {
	return m.ueObjects
}

func (m *RestManager) PrintCells(ctx context.Context, print bool) error {

	if print {
		log.Debug(m.DashMarks("Cells", true))
	}

	values := make([]string, 0, len(m.cellList))
	for _, value := range m.cellList {
		values = append(values, value)
	}
	sort.Strings(values)
	keys := make([]int, 0, len(m.cellList))
	for _, v := range values {
		for key, value := range m.cellList {
			if v == value {
				keys = append(keys, key)
				break
			}
		}
	}

	for _, k := range keys {
		if cellData, err := m.GetCell(ctx, m.cellList[k]); cellData != nil {
			output := fmt.Sprintf(" CGI: %s, UEs:[", cellData.Cgi)
			flag := false
			var suboutput string
			keys := make([]string, 0, len(cellData.UeTab))
			for k := range cellData.UeTab {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, v := range keys {
				if flag {
					suboutput = suboutput + ", "
				}
				suboutput = suboutput + fmt.Sprint(cellData.UeTab[v])
				flag = true
			}
			output = output + suboutput + "]"
			if print {
				log.Debug(output)
			}

			if m.cellLen < len(output) {
				m.cellLen = len(output)
			}
		} else if err != nil {
			return err
		}
	}

	if print {
		log.Debug(m.DashMarks("", true))
		log.Debug("")
	}

	return nil

}

func (m *RestManager) CreateCell(ctx context.Context, cgi string) (*CellData, error) {

	asciiCgi := m.TranslateUtfAscii(cgi)
	m.SaveUtfAscii(cgi, asciiCgi, true)

	cellData := &CellData{
		Cgi:   cgi,
		UeTab: make(map[string]string),
	}

	_, err := m.cellStore.Put(ctx, cgi, *cellData, store.Done)
	if err != nil {
		err = errors.New("ERROR: " + fmt.Sprint(err))
		return nil, err
	}
	m.cellObjects[cellData.Cgi] = cellData

	return cellData, nil

}

func (m *RestManager) GetCell(ctx context.Context, cgi string) (*CellData, error) {

	var cellData *CellData

	cell, err := m.cellStore.Get(ctx, cgi)
	if err != nil || cell == nil {
		return nil, nil
	}
	t := cell.Value.(CellData)
	if t.Cgi != cgi {
		err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: wrong input data%s", "!")))
		return nil, err
	}
	cellData = &t

	return cellData, nil

}

func (m *RestManager) SetCell(ctx context.Context, cellData *CellData) error {

	var err error

	if len(cellData.Cgi) == 0 {
		err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: wrong input data%s", "!")))
		return err
	}

	_, err = m.cellStore.Put(ctx, cellData.Cgi, *cellData, store.Done)
	if err != nil {
		err = errors.New("ERROR: " + fmt.Sprint(err))
		return err
	}

	return nil

}

func (m *RestManager) GetCells() map[string]*CellData {
	return m.cellObjects
}

func (m *RestManager) CreateCellObjects(ctx context.Context) error {

	for _, value := range m.cellList {
		_, err := m.CreateCell(ctx, value)
		return err
	}

	return nil

}

func (m *RestManager) GetUeInfo(ctx context.Context) error {

	for _, ueId := range m.ueList {
		params := fmt.Sprintf("?db=" + m.lastTest + "&q=SELECT+*+FROM+UEReports+WHERE+\"Viavi.UE.Name\"+=+'" + ueId + "'+AND+\"time\"+=+'" + m.lastTimestamp + "'+AND+\"report\"+=+'serving'+GROUP+BY+\"Viavi.UE.Name\"+LIMIT+1")
		response, err := m.RequestData(false, nil, params)
		if err != nil {
			return err
		}
		var data *ViaviUE
		if err = m.client.JSONUnmarshal(response.Body(), &data); err != nil {
			err = errors.New("ERROR: " + fmt.Sprint(err))
			return err
		}

		var rrcState string
		var fiveQi string
		var slice string
		var servingCell string
		var servingRsrp string
		rsrpTab := make(map[string]string)

		rrcState = "Connected"
		fiveQi = fmt.Sprint(data.GetUeParameter(ueId, "Viavi.QoS.5qi"))
		if strings.Contains(fiveQi, "Error") {
			err = errors.New("ERROR: " + fmt.Sprint(fiveQi))
			return err
		}
		slice = fmt.Sprint(data.GetUeParameter(ueId, "Viavi.UE.Slice"))
		if strings.Contains(slice, "Error") {
			err = errors.New("ERROR: " + fmt.Sprint(slice))
			return err
		}
		servingCell = fmt.Sprint(data.GetUeParameter(ueId, "Viavi.Cell.Name"))
		if strings.Contains(servingCell, "Error") {
			err = errors.New("ERROR: " + fmt.Sprint(servingCell))
			return err
		}
		servingRsrp = fmt.Sprint(data.GetUeParameter(ueId, "Viavi.UE.Rsrp"))
		if strings.Contains(servingRsrp, "Error") {
			err = errors.New("ERROR: " + fmt.Sprint(servingRsrp))
			return err
		}
		rsrpTab[servingCell] = servingRsrp

		idleTab := make(map[string]string)
		idleTab["-"] = "-"

		idle := strings.Contains(servingCell, "idle")
		if idle {
			rrcState = "Idle"
			fiveQi = "-"
			slice = "-"
			servingCell = "-"
			servingRsrp = "-"
			rsrpTab = idleTab
		}

		if !idle {
			for _, cellId := range m.cellList {
				if cellId != servingCell {
					params := fmt.Sprintf("?db=" + m.lastTest + "&q=SELECT+*+FROM+UEReports+WHERE+\"Viavi.UE.Name\"+=+'" + ueId + "'+AND+\"Viavi.Cell.Name\"+=+'" + cellId + "'+AND+\"time\"+=+'" + m.lastTimestamp + "'+AND+\"report\"+=+'neighbour'+GROUP+BY+\"Viavi.UE.Name\"+LIMIT+1")
					response, err := m.RequestData(false, nil, params)
					if err != nil {
						return err
					}
					var data *ViaviUE
					if err := m.client.JSONUnmarshal(response.Body(), &data); err != nil {
						err = errors.New("ERROR: " + fmt.Sprint(err))
						return err
					}
					if data != nil {
						neighbourCell := fmt.Sprint(data.GetUeParameter(ueId, "Viavi.Cell.Name"))
						if strings.Contains(neighbourCell, "Error") {
							err = errors.New("ERROR: " + fmt.Sprint(neighbourCell))
							return err
						}
						neighbourRsrp := fmt.Sprint(data.GetUeParameter(ueId, "Viavi.UE.Rsrp"))
						if strings.Contains(neighbourRsrp, "Error") {
							err = errors.New("ERROR: " + fmt.Sprint(neighbourRsrp))
							return err
						}
						rsrpTab[neighbourCell] = neighbourRsrp
					}
				}
			}
		}

		var currentCell string
		ueData, err := m.GetUe(ctx, ueId)
		if err != nil {
			return err
		}
		if ueData == nil {
			ueData, err = m.CreateUe(ctx, ueId, servingCell, rrcState, fiveQi, slice, rsrpTab)
			if err != nil {
				return err
			}
		} else {
			currentCell = ueData.Cgi
			ueData.RrcState = rrcState
			ueData.FiveQi = fiveQi
			ueData.Slice = slice
			ueData.RsrpTab = rsrpTab
			err = m.SetUe(ctx, ueData)
			if err != nil {
				return err
			}
		}
		if !idle && servingCell != "" && servingCell != currentCell {
			err = m.AttachUe(ctx, ueData, servingCell)
			if err != nil {
				return err
			}
		} else if (idle || servingCell == "") && servingCell != currentCell {
			m.DetachUe(ctx, ueData)
		}

	}

	return nil

}

func (m *RestManager) HandoverControl(ctx context.Context, ueId string, cgi string) error {

	var err error

	ueData, err := m.GetUe(ctx, ueId)
	if err != nil {
		return err
	}

	destCellData, err := m.GetCell(ctx, cgi)
	if err != nil {
		return err
	}

	if ueData != nil && destCellData != nil {
		serCellData, err := m.GetCell(ctx, ueData.Cgi)
		if err != nil {
			return err
		}
		if (serCellData != nil && serCellData.Cgi != cgi) || (serCellData == nil && ueData.Cgi == "") {
			jsonCommand := fmt.Sprintf("{\"actionId\":%d,\"fromCell\":\"%s\",\"toCell\":\"%s\",\"type\":\"Control 3: Handover Control\",\"ue\":\"%s\",\"reason\":\"E2\",\"seqNo\":5}", m.hoActionId, ueData.Cgi, cgi, ueId)
			byteReader := bytes.NewReader([]byte(jsonCommand))
			_, err = m.RequestData(false, byteReader, "")
			if err == nil {
				m.hoActionId++
				log.Info("E2 Control Message: UE (ID: %s) has been switched to another Cell (CGI_1: %s -> CGI_2: %s)", ueId, ueData.Cgi, cgi)
				log.Info("")
				log.Info("")
			} else {
				return err
			}
		} else if serCellData == nil && ueData.Cgi != "" {
			m.MakeUeIdle(ctx, ueData)
			err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: UE assigned to not-existing Cell%s", "!")))
			return err
		} else if serCellData != nil && serCellData.Cgi == cgi {
			err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: trying connect UE with assigned Cell%s", "!")))
			return err
		}
	} else {
		err = errors.New(fmt.Sprint(fmt.Errorf("ERROR: wrong input data%s", "!")))
		return err
	}

	return nil

}

func (m *RestManager) Run(ctx context.Context) {

	for {
		m.UpdateData()
		m.GetUeInfo(ctx)
		m.PrintUes(ctx, m.ueLen != 0)
		m.PrintCells(ctx, m.cellLen != 0)

		time.Sleep(1 * time.Second)
	}

}
