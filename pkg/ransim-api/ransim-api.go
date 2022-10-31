package ransimapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/monitoring"
	modelAPI "github.com/onosproject/onos-api/go/onos/ransim/model"
	"github.com/onosproject/onos-lib-go/pkg/certs"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var log = logging.GetLogger("rimedo-ts", "ransim-api")

func NewHandler(endpoint string, nodeManager *monitoring.NodeManager) (Handler, error) {

	// log.SetLevel(logging.DebugLevel)

	cert, err := tls.X509KeyPair([]byte(certs.DefaultClientCrt), []byte(certs.DefaultClientKey))
	if err != nil {
		return nil, err
	}

	dialOpts := []grpc.DialOption{}
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	})))

	conn, err := grpc.Dial(endpoint, dialOpts...)
	if err != nil {
		return nil, err
	}

	log.Info("Dialed gRPC to ransim endpoint")

	return &handler{
		ueClient:    modelAPI.NewUEModelClient(conn),
		nodeManager: nodeManager,
	}, nil

}

type Handler interface {
	GetUesParameters(ctx context.Context) error
}

type handler struct {
	ueClient    modelAPI.UEModelClient
	nodeManager *monitoring.NodeManager
}

func (h *handler) GetUesParameters(ctx context.Context) error {

	stream, err := h.ueClient.ListUEs(ctx, &modelAPI.ListUEsRequest{})
	if err != nil {
		log.Warn("Something's gone wrong when getting the UEs info list [GetUEs()].", err)
	}

	// results := make([]UE, 0)
	for {
		receiver, err := stream.Recv()
		if err != nil {
			break
		}

		ue := receiver.Ue
		// log.Debug(ue)

		id := fmt.Sprintf("%d", ue.Ueid.AmfUeNgapId)
		for len(id) < 16 {
			id = "0" + id
		}
		// log.Debug("ID: ", id)
		ueData := h.nodeManager.GetUe(ctx, id)
		if ueData == nil {
			return fmt.Errorf("There's no such UE")
		}
		// ueData := ues[id]
		var fiveQi int64
		if int64(ue.FiveQi) > 127 {
			fiveQi = 2
		} else {
			fiveQi = 1
		}
		ueData.FiveQi = fiveQi
		// for cgi := range ueData.RsrpTable {
		// ueData.CGI = strconv.FormatUint(uint64(ue.ServingTower), 16)
		if ue.ServingTower != 0 {
			ueData.RsrpServing = int32(ue.ServingTowerStrength)
			ueData.RsrpTable[ueData.CGI] = int32(ue.ServingTowerStrength)
		}
		if ue.Tower1 != 0 {
			ueData.RsrpTable[strconv.FormatUint(uint64(ue.Tower1), 16)] = int32(ue.Tower1Strength)
		}
		if ue.Tower2 != 0 {
			ueData.RsrpTable[strconv.FormatUint(uint64(ue.Tower2), 16)] = int32(ue.Tower2Strength)
		}
		if ue.Tower3 != 0 {
			ueData.RsrpTable[strconv.FormatUint(uint64(ue.Tower3), 16)] = int32(ue.Tower3Strength)
		}
		// log.Debug()
		// log.Debug()
		// log.Debug("CGI_T1: ", strconv.FormatUint(uint64(ue.Tower1), 16))
		// log.Debug("CGI_T2: ", strconv.FormatUint(uint64(ue.Tower2), 16))
		// log.Debug("CGI_T3: ", strconv.FormatUint(uint64(ue.Tower3), 16))
		// log.Debug("CGI_ST: ", strconv.FormatUint(uint64(ue.ServingTower), 16))
		// log.Debug()
		// log.Debug()

		// log.Debug()
		// log.Debug()
		// log.Debug("T1: ", ue.Tower1Strength)
		// log.Debug("T2: ", ue.Tower2Strength)
		// log.Debug("T3: ", ue.Tower3Strength)
		// log.Debug("ST: ", ue.ServingTowerStrength)
		// log.Debug()
		// log.Debug()

		// }
		h.nodeManager.SetUe(ctx, ueData)
	}

	return nil

}
