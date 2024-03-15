package e2

import (
  "fmt"
  "github.com/tidwall/gjson"
)

type ViaviUE struct {
	Results []struct {
		StatementID int `json:"statement_id"`
		Series      []struct {
			Name string `json:"name"`
			Tags struct {
				ViaviUEName string `json:"Viavi.UE.Name"`
			} `json:"tags"`
			Columns []string `json:"columns"`
			Values  [][]any  `json:"values"`
		} `json:"series"`
	} `json:"results"`
}

type ViaviCell struct {
	Results []struct {
		StatementID int `json:"statement_id"`
		Series      []struct {
			Name string `json:"name"`
			Tags struct {
				ViaviCellName string `json:"Viavi.Cell.Name"`
			} `json:"tags"`
			Columns []string `json:"columns"`
			Values  [][]any  `json:"values"`
		} `json:"series"`
	} `json:"results"`
}

type UeData struct {
	Id       string
	Cgi      string
	RrcState string
	FiveQi   string
	Slice    string
	RsrpTab  map[string]string
}

type CellData struct {
	Cgi   string
	UeTab map[string]string
}

func (o *ViaviCell) GetViaviCgiCellMap(bytes []byte) map[uint64]string {

  cellMap := make(map[uint64]string)

  cell_names := gjson.Get(string(bytes), "results.#.series.#.tags.Viavi\\.Cell\\.Name")
  if len(cell_names.Array()) == 0 {
    return cellMap
  }
  cell_names = cell_names.Array()[0]

  cell_nrcgis := gjson.Get(string(bytes), "results.#.series.#.values.#.[33]")
  if len(cell_nrcgis.Array()) == 0 {
    return cellMap
  }
  cell_nrcgis = cell_nrcgis.Array()[0]

  if len(cell_nrcgis.Array()) != len(cell_names.Array()) {
    return cellMap
  }

  for idx, cell_nrcgi := range cell_nrcgis.Array() {
    if len(cell_nrcgi.Array()) == 0 {
      continue
    }
    inner_nrcgi := cell_nrcgi.Array()[0]
    if len(inner_nrcgi.Array()) == 0 {
      continue
    }
    inner_nrcgi = inner_nrcgi.Array()[0]
    cellMap[uint64(inner_nrcgi.Int())] = cell_names.Array()[idx].String()
  }

  return cellMap

}

func (o *ViaviCell) ListCellIDs() map[int]string {

	list := make(map[int]string)

	counter := 0
	for _, resultsValue := range o.Results {
		for _, seriesValue := range resultsValue.Series {
			list[counter] = seriesValue.Tags.ViaviCellName
			counter++
		}
	}

	return list

}

func (o *ViaviUE) ListUeIDs() map[int]string {

	list := make(map[int]string)

	counter := 0
	for _, resultsValue := range o.Results {
		for _, seriesValue := range resultsValue.Series {
			list[counter] = seriesValue.Tags.ViaviUEName
			counter++
		}
	}

	return list

}

func (o *ViaviUE) GetUeParameter(id string, param string) string {

	for _, resultsValue := range o.Results {
		for _, seriesValue := range resultsValue.Series {
			ueId := seriesValue.Tags.ViaviUEName
			if ueId == id {
				for key, value := range seriesValue.Columns {
					if value == param {
						return fmt.Sprint(seriesValue.Values[0][key])
					}
				}
			}
		}
	}

	return " ERROR: wrong input data! "

}

func (o *ViaviCell) GetCellParameter(id string, param string) string {

	for _, resultsValue := range o.Results {
		for _, seriesValue := range resultsValue.Series {
			cellId := seriesValue.Tags.ViaviCellName
			if cellId == id {
				for key, value := range seriesValue.Columns {
					if value == param {
						return fmt.Sprint(seriesValue.Values[0][key])
					}
				}
			}
		}
	}

	return " ERROR: wrong input data! "

}
