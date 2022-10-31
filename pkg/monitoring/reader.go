// Copy from onosproject/onos-mho/pkg/monitoring/monitor.go
// modified by RIMEDO-Labs team
package monitoring

import (
	"fmt"
	"strconv"
	"strings"

	e2sm_mho "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-mho-go"
	e2sm_v2_ies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_mho_go/v2/e2sm-v2-ies"
)

func PlmnIDBytesToInt(b []byte) uint64 {
	return uint64(b[2])<<16 | uint64(b[1])<<8 | uint64(b[0])
}

func PlmnIDNciToCGI(plmnID uint64, nci uint64) string {
	cgi := strconv.FormatInt(int64(plmnID<<36|(nci&0xfffffffff)), 16)
	return cgi
}

func PlmnIDNciFromCGI(cgi string) (uint64, uint64) {
	cgiString, err := strconv.ParseUint(cgi, 16, 64)
	if err != nil {
		log.Error(err)
	}
	nci := cgiString & 0xfffffffff
	plmnId := (cgiString & 0xfffffff000000000) >> 36
	return nci, plmnId
}

func GetNciFromCellGlobalID(cellGlobalID *e2sm_v2_ies.Cgi) uint64 {
	return BitStringToUint64(cellGlobalID.GetNRCgi().GetNRcellIdentity().GetValue().GetValue(), int(cellGlobalID.GetNRCgi().GetNRcellIdentity().GetValue().GetLen()))
}

func GetPlmnIDBytesFromCellGlobalID(cellGlobalID *e2sm_v2_ies.Cgi) []byte {
	return cellGlobalID.GetNRCgi().GetPLmnidentity().GetValue()
}

func GetMccMncFromPlmnID(plmnId uint64, flag bool) (string, string) {
	plmnIdString := strconv.FormatUint(plmnId, 16)
	// log.Debug()
	// log.Debug()
	// log.Debug("PLMN ID: ", plmnId)
	// log.Debug("PLMN String: ", plmnIdString)
	// log.Debug()
	// log.Debug()
	var mcc string
	var mnc string

	if flag {
		middle := ReverseString(plmnIdString[2:4])
		mcc = ReverseString(plmnIdString[0:2]) + middle[0:1]
		mnc = ReverseString(plmnIdString[4:6]) + middle[1:2]
		mcc = strings.ReplaceAll(mcc, "f", "")
		mcc = strings.ReplaceAll(mcc, "F", "")
		mnc = strings.ReplaceAll(mnc, "f", "")
		mnc = strings.ReplaceAll(mnc, "F", "")
	} else {
		mcc = plmnIdString[0:3]
		mnc = plmnIdString[3:6]
	}

	return mcc, mnc
}

func GetPlmnIdFromMccMnc(mcc string, mnc string, flag bool) (uint64, error) {
	var plmnIdString string
	combined := mcc + mnc
	if flag {
		first := ReverseString(combined[0:2])
		var middle string
		var last string
		if len(combined) > 5 {

			middle = ReverseString(combined[2:4])
			last = ReverseString(combined[4:6])

		} else {

			middle = "f" + combined[2:3]
			last = ReverseString(combined[3:5])

		}
		plmnIdString = first + middle + last
	} else {
		plmnIdString = combined
	}
	plmnId, err := strconv.ParseUint(plmnIdString, 16, 64)
	if err != nil {
		log.Warn("Cannot convert PLMN ID string into uint64 type!")
	}
	return plmnId, err
}

func ReverseString(str string) string {
	byte_str := []rune(str)
	for i, j := 0, len(byte_str)-1; i < j; i, j = i+1, j-1 {
		byte_str[i], byte_str[j] = byte_str[j], byte_str[i]
	}
	return string(byte_str)
}

func GetCGIFromIndicationHeader(header *e2sm_mho.E2SmMhoIndicationHeaderFormat1) string {
	nci := GetNciFromCellGlobalID(header.GetCgi())
	plmnIDBytes := GetPlmnIDBytesFromCellGlobalID(header.GetCgi())
	plmnID := PlmnIDBytesToInt(plmnIDBytes)
	return PlmnIDNciToCGI(plmnID, nci)
}

func GetCGIFromMeasReportItem(measReport *e2sm_mho.E2SmMhoMeasurementReportItem) string {
	nci := GetNciFromCellGlobalID(measReport.GetCgi())
	plmnIDBytes := GetPlmnIDBytesFromCellGlobalID(measReport.GetCgi())
	plmnID := PlmnIDBytesToInt(plmnIDBytes)
	return PlmnIDNciToCGI(plmnID, nci)
}

func Uint64ToBitString(value uint64, bitCount int) []byte {
	result := make([]byte, bitCount/8+1)
	if bitCount%8 > 0 {
		value = value << (8 - bitCount%8)
	}

	for i := 0; i <= (bitCount / 8); i++ {
		result[i] = byte(value >> (((bitCount / 8) - i) * 8) & 0xFF)
	}

	return result
}

func BitStringToUint64(bitString []byte, bitCount int) uint64 {
	var result uint64
	for i, b := range bitString {
		result += uint64(b) << ((len(bitString) - i - 1) * 8)
	}
	if bitCount%8 != 0 {
		return result >> (8 - bitCount%8)
	}
	return result
}

func GetUeID(ueID *e2sm_v2_ies.Ueid) (int64, error) {

	switch ue := ueID.Ueid.(type) {
	case *e2sm_v2_ies.Ueid_GNbUeid:
		return ue.GNbUeid.GetAmfUeNgapId().GetValue(), nil
	case *e2sm_v2_ies.Ueid_ENbUeid:
		return ue.ENbUeid.GetMMeUeS1ApId().GetValue(), nil
	case *e2sm_v2_ies.Ueid_EnGNbUeid:
		return int64(ue.EnGNbUeid.GetMENbUeX2ApId().GetValue()), nil
	case *e2sm_v2_ies.Ueid_NgENbUeid:
		return ue.NgENbUeid.GetAmfUeNgapId().GetValue(), nil
	default:
		return -1, fmt.Errorf("GetUeID() couldn't extract UeID - obtained unexpected type %v", ue)
	}
}
