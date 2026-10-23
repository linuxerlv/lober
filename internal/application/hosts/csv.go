package hosts

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

// 只解析文件；坏行保留在计划中，有效且唯一的主机用于后续查询。
func parseHostCSV(data []byte) (ImportPlan, []model.Host, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	header, err := r.Read()
	if err != nil {
		return ImportPlan{}, nil, &CSVFileError{Message: fmt.Sprintf(model.ConsoleCSVRead, err)}
	}
	snCol := -1
	seenHeaders := map[string]bool{}
	for i, name := range header {
		name = strings.TrimSpace(name)
		header[i] = name
		if seenHeaders[name] || (!slices.Contains(model.HostReservedAttrs(), name) && !model.ValidAttrKey(name)) {
			return ImportPlan{}, nil, &CSVFileError{Message: fmt.Sprintf(model.ConsoleCSVHeader, name)}
		}
		seenHeaders[name] = true
		if name == "sn" {
			snCol = i
		}
	}
	if snCol < 0 {
		return ImportPlan{}, nil, &CSVFileError{Message: model.ConsoleCSVNoSN}
	}
	p := ImportPlan{Rows: []ImportRow{}}
	records := []model.Host{}
	indices := map[string][]int{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ImportPlan{}, nil, &CSVFileError{Message: fmt.Sprintf(model.ConsoleCSVRead, err)}
		}
		line, _ := r.FieldPos(0)
		h := model.Host{SN: rec[snCol], Attrs: map[string]string{}}
		var ps []model.Problem
		sn, err := model.NormalizeSN(h.SN)
		if err != nil {
			var problem model.Problem
			if !errors.As(err, &problem) {
				return ImportPlan{}, nil, err
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
		}
		p.Rows = append(p.Rows, ImportRow{Row: line, SN: h.SN, Action: action, Problems: ps})
		records = append(records, h)
	}
	if len(p.Rows) == 0 {
		return ImportPlan{}, nil, &CSVFileError{Message: model.ConsoleCSVEmpty}
	}
	for sn, positions := range indices {
		if len(positions) < 2 {
			continue
		}
		duplicate := model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: model.KindHost, ID: sn})
		for _, i := range positions {
			p.Rows[i].Action = "error"
			p.Rows[i].Problems = append([]model.Problem{duplicate}, p.Rows[i].Problems...)
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
