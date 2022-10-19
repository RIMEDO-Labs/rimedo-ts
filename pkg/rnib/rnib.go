// Created by RIMEDO-Labs team
// based on onosproject/onos-mho/pkg/rnib/rnib.go
package rnib

import (
	"context"
	"fmt"

	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	measurmentStore "github.com/onosproject/onos-kpimon/pkg/store/measurements"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	toposdk "github.com/onosproject/onos-ric-sdk-go/pkg/topo"
)

var log = logging.GetLogger("rimedo-ts", "rnib")

type TopoClient interface {
	WatchE2Connections(ctx context.Context, ch chan topoapi.Event) error
	GetCells(ctx context.Context, nodeID topoapi.ID) ([]*topoapi.E2Cell, error)
	GetE2NodeAspects(ctx context.Context, nodeID topoapi.ID) (*topoapi.E2Node, error)
	E2NodeIDs(ctx context.Context) ([]topoapi.ID, error)
}

type Options struct {
	TopoAddress string
	TopoPort    int
}

type Cell struct {
	CGI      string
	CellType string
}

func NewClient(options Options) (Client, error) {
	sdkClient, err := toposdk.NewClient(
		toposdk.WithTopoAddress(
			options.TopoAddress,
			options.TopoPort,
		),
	)
	if err != nil {
		return Client{}, err
	}
	return Client{
		client: sdkClient,
	}, nil
}

type Client struct {
	client toposdk.Client
}

func (c *Client) HasRANFunction(ctx context.Context, nodeID topoapi.ID, oid string) bool {
	e2Node, err := c.GetE2NodeAspects(ctx, nodeID)
	if err != nil {
		return false
	}

	for _, sm := range e2Node.GetServiceModels() {
		if sm.OID == oid {
			return true
		}
	}
	return false
}

func (c *Client) UpdateCellAspects(ctx context.Context, cellID topoapi.ID, measItems []measurmentStore.MeasurementItem) error {
	object, err := c.client.Get(ctx, cellID)
	if err != nil {
		return err
	}

	if object != nil && object.GetEntity().GetKindID() == topoapi.E2CELL {
		cellObject := &topoapi.E2Cell{}
		err := object.GetAspect(cellObject)
		if err != nil {
			return err
		}
		cellObject.KpiReports = make(map[string]uint32)

		tmpTs := uint64(0)
		for _, measItem := range measItems {
			for _, record := range measItem.MeasurementRecords {
				if tmpTs <= record.Timestamp {
					tmpTs = record.Timestamp
					switch record.MeasurementValue.(type) {
					case int32:
						cellObject.KpiReports[record.MeasurementName] = uint32(record.MeasurementValue.(int32))

					case int64:
						cellObject.KpiReports[record.MeasurementName] = uint32(record.MeasurementValue.(int64))

					default:
						cellObject.KpiReports[record.MeasurementName] = uint32(0)
					}
				}
			}
		}

		err = object.SetAspect(cellObject)
		if err != nil {
			return err
		}
		err = c.client.Update(ctx, object)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) GetCellTopoID(ctx context.Context, coi string, nodeID topoapi.ID) (topoapi.ID, error) {
	cells, err := c.GetCells(ctx, nodeID)
	if err != nil {
		return "", err
	}

	for _, cell := range cells {
		if coi == cell.CellObjectID {
			return topoapi.ID(fmt.Sprintf("%s/%s", string(nodeID), cell.CellGlobalID.Value)), nil
		}
	}
	return "", errors.NewNotFound("E2Cell not found with CellObjectID")
}

// E2NodeIDs lists all of connected E2 nodes
func (c *Client) E2NodeIDs(ctx context.Context, oid string) ([]topoapi.ID, error) {
	objects, err := c.client.List(ctx, toposdk.WithListFilters(getControlRelationFilter()))
	if err != nil {
		return nil, err
	}

	e2NodeIDs := make([]topoapi.ID, len(objects))
	for _, object := range objects {
		relation := object.Obj.(*topoapi.Object_Relation)
		e2NodeID := relation.Relation.TgtEntityID
		if c.HasRANFunction(ctx, e2NodeID, oid) {
			e2NodeIDs = append(e2NodeIDs, e2NodeID)
		}
	}

	return e2NodeIDs, nil
}

func (c *Client) GetCells(ctx context.Context, nodeID topoapi.ID) ([]*topoapi.E2Cell, error) {
	filter := &topoapi.Filters{
		RelationFilter: &topoapi.RelationFilter{SrcId: string(nodeID),
			RelationKind: topoapi.CONTAINS,
			TargetKind:   ""}}
	objects, err := c.client.List(ctx, toposdk.WithListFilters(filter))
	if err != nil {
		return nil, err
	}
	var cells []*topoapi.E2Cell
	for _, obj := range objects {
		targetEntity := obj.GetEntity()
		if targetEntity.GetKindID() == topoapi.E2CELL {
			cellObject := &topoapi.E2Cell{}
			err = obj.GetAspect(cellObject)
			if err == nil {
				cells = append(cells, cellObject)
			}
		}
	}
	if len(cells) == 0 {
		return nil, errors.New(errors.NotFound, "there is no cell to subscribe for e2 node %s", nodeID)
	}
	return cells, nil
}

func getControlRelationFilter() *topoapi.Filters {
	controlRelationFilter := &topoapi.Filters{
		KindFilter: &topoapi.Filter{
			Filter: &topoapi.Filter_Equal_{
				Equal_: &topoapi.EqualFilter{
					Value: topoapi.CONTROLS,
				},
			},
		},
	}
	return controlRelationFilter
}

func (c *Client) WatchE2Connections(ctx context.Context, ch chan topoapi.Event) error {
	err := c.client.Watch(ctx, ch, toposdk.WithWatchFilters(getControlRelationFilter()))
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) GetE2CellFilter() *topoapi.Filters {
	cellEntityFilter := &topoapi.Filters{
		KindFilter: &topoapi.Filter{
			Filter: &topoapi.Filter_In{
				In: &topoapi.InFilter{
					Values: []string{topoapi.E2CELL},
				},
			},
		},
	}
	return cellEntityFilter
}

func (c *Client) GetCellTypes(ctx context.Context) (map[string]Cell, error) {
	output := make(map[string]Cell)

	cells, err := c.client.List(ctx, toposdk.WithListFilters(c.GetE2CellFilter()))
	if err != nil {
		log.Warn(err)
		return output, err
	}

	for _, cell := range cells {

		cellObject := &topoapi.E2Cell{}
		err = cell.GetAspect(cellObject)
		if err != nil {
			log.Warn(err)
		}
		output[string(cell.ID)] = Cell{
			CGI:      cellObject.CellObjectID,
			CellType: cellObject.CellType,
		}
	}
	return output, nil
}

func (c *Client) SetCellType(ctx context.Context, id string, cellType string) error {
	cell, err := c.client.Get(ctx, topoapi.ID(id))
	if err != nil {
		log.Warn(err)
		return err
	}

	cellObject := &topoapi.E2Cell{}
	err = cell.GetAspect(cellObject)
	if err != nil {
		log.Warn(err)
		return err
	}

	cellObject.CellType = cellType

	err = cell.SetAspect(cellObject)
	if err != nil {
		log.Warn(err)
		return err
	}
	err = c.client.Update(ctx, cell)
	if err != nil {
		log.Warn(err)
		return err
	}

	return nil
}

func (c *Client) GetE2NodeAspects(ctx context.Context, nodeID topoapi.ID) (*topoapi.E2Node, error) {
	object, err := c.client.Get(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	e2Node := &topoapi.E2Node{}
	err = object.GetAspect(e2Node)

	return e2Node, err

}
