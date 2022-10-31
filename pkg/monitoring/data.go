// Copy from onosproject/onos-mho/pkg/monitoring/monitor.go
// modified by RIMEDO-Labs team
package monitoring

import (
	policyAPI "github.com/onosproject/onos-a1-dm/go/policy_schemas/traffic_steering_preference/v2"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	e2smcommonies "github.com/onosproject/onos-e2-sm/servicemodels/e2sm_rc/v1/e2sm-common-ies"
)

type UeData struct {
	UeKey       string
	UeID        *e2smcommonies.Ueid
	CGI         string
	RrcState    string
	FiveQi      int64
	RsrpServing int32
	RsrpTable   map[string]int32
}

type CellData struct {
	CGI                    string
	E2NodeID               topoapi.ID
	CumulativeHandoversIn  int
	CumulativeHandoversOut int
	Ues                    map[string]*UeData
}

type PolicyData struct {
	Key        string
	API        *policyAPI.API
	IsEnforced bool
}
