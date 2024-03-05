// Created by RIMEDO-Labs team
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/RIMEDO-Labs/rimedo-ts/pkg/manager"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/northbound/a1"
	"github.com/RIMEDO-Labs/rimedo-ts/pkg/sdran"
	"github.com/onosproject/onos-lib-go/pkg/certs"
	"github.com/onosproject/onos-lib-go/pkg/logging"
)

var log = logging.GetLogger("rimedo-ts")

func main() {

	log.SetLevel(logging.DebugLevel)
	log.Info(" Starting RIMEDO Labs Traffic Steering xAPP - LOCAL ")

	sdranConfig := sdran.Config{
		AppID:         "rimedo-ts",
		E2tAddress:    "onos-e2t",
		E2tPort:       5150,
		TopoAddress:   "onos-topo",
		TopoPort:      5150,
		SMName:        "oran-e2sm-rc",
		SMVersion:     "v1",
		RansimAddress: "ran-simulator",
		RansimPort:    5150,
	}

	a1Config := a1.Config{
		PolicyName:        "ORAN_TrafficSteeringPreference",
		PolicyVersion:     "2.0.0",
		PolicyID:          "ORAN_TrafficSteeringPreference_2.0.0",
		PolicyDescription: "O-RAN traffic steering",
		A1tPort:           5150,
	}

	_, err := certs.HandleCertPaths("", "", "", true)
	if err != nil {
		log.Fatal(err)
	}

	mgr := manager.NewManager(sdranConfig, a1Config, false)
	mgr.Run()

	killSignal := make(chan os.Signal, 1)
	signal.Notify(killSignal, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	log.Debug(" Application: received a shutdown signal: ", <-killSignal)
	mgr.Close()
}
