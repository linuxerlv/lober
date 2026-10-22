package console

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

type csvRow struct {
	Row      int           `json:"row"`
	SN       string        `json:"sn"`
	Action   string        `json:"action"`
	Problems []problemJSON `json:"problems"`
}
type csvPlan struct {
	Rows  []csvRow `json:"rows"`
	Total int      `json:"total"`
}

// parseHostCSV never writes. Invalid records remain in the preview; hosts holds
// only valid, unique records. Callers must reject the entire plan if any row errs.
func parseHostCSV(data []byte, known func(string) bool) (csvPlan, []model.Host, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	header, err := r.Read()
	if err != nil {
		return csvPlan{}, nil, fmt.Errorf(model.ConsoleCSVRead, err)
	}
	snCol := -1
	seenHeaders := map[string]bool{}
	for i, name := range header {
		name = strings.TrimSpace(name)
		header[i] = name
		if seenHeaders[name] || (!slices.Contains(model.HostReservedAttrs(), name) && !model.ValidAttrKey(name)) {
			return csvPlan{}, nil, fmt.Errorf(model.ConsoleCSVHeader, name)
		}
		seenHeaders[name] = true
		if name == "sn" {
			snCol = i
		}
	}
	if snCol < 0 {
		return csvPlan{}, nil, errors.New(model.ConsoleCSVNoSN)
	}
	p := csvPlan{Rows: []csvRow{}}
	records := []model.Host{}
	indices := map[string][]int{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return csvPlan{}, nil, fmt.Errorf(model.ConsoleCSVRead, err)
		}
		line, _ := r.FieldPos(0)
		h := model.Host{SN: rec[snCol], Attrs: map[string]string{}}
		var ps []model.Problem
		sn, err := model.NormalizeSN(h.SN)
		if err != nil {
			var problem model.Problem
			if !errors.As(err, &problem) {
				return csvPlan{}, nil, err
			}
			ps = append(ps, problem)
		} else {
			h.SN = sn
			indices[sn] = append(indices[sn], len(p.Rows))
		}
		for i, name := range header {
			switch name {
			case "sn":
			case "arch":
				h.Arch = catalog.Arch(rec[i])
			case "firmware":
				h.Firmware = catalog.Firmware(rec[i])
			default:
				h.Attrs[name] = rec[i]
			}
		}
		ps = append(ps, h.Validate()...)
		action := "create"
		if len(ps) > 0 {
			action = "error"
		} else if known != nil && known(h.SN) {
			action = "update"
		}
		p.Rows = append(p.Rows, csvRow{Row: line, SN: h.SN, Action: action, Problems: jsonProblems(ps)})
		records = append(records, h)
	}
	if len(p.Rows) == 0 {
		return csvPlan{}, nil, errors.New(model.ConsoleCSVEmpty)
	}
	for sn, positions := range indices {
		if len(positions) < 2 {
			continue
		}
		duplicate := jsonProblems([]model.Problem{model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: model.KindHost, ID: sn})})[0]
		for _, i := range positions {
			p.Rows[i].Action = "error"
			p.Rows[i].Problems = append([]problemJSON{duplicate}, p.Rows[i].Problems...)
		}
	}
	hosts := make([]model.Host, 0, len(records))
	for i, h := range records {
		if p.Rows[i].Action != "error" {
			hosts = append(hosts, h)
		}
	}
	p.Total = len(p.Rows)
	return p, hosts, nil
}
