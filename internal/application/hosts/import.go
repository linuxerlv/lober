package hosts

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
)

func (s *registry) Import(ctx context.Context, a ports.Actor, r ImportRequest) (ImportResult, error) {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return ImportResult{}, err
	}
	p, hs, err := parseHostCSV(r.CSV)
	if err != nil {
		return ImportResult{}, err
	}
	sns := make([]string, len(hs))
	for i, h := range hs {
		sns[i] = h.SN
	}
	known, err := s.deps.Hosts.GetHosts(ctx, sns)
	if err != nil {
		return ImportResult{}, err
	}
	rejected := false
	message := model.ConsoleRejected
	for i := range p.Rows {
		row := &p.Rows[i]
		if row.Action == "error" {
			if !rejected {
				message = row.Problems[0].Message
			}
			rejected = true
			continue
		}
		if _, exists := known[row.SN]; exists {
			row.Action = "update"
		}
	}
	if r.DryRun {
		return ImportResult{Plan: p}, nil
	}
	if rejected {
		return ImportResult{}, &ImportRejectedError{Plan: p, Message: message}
	}
	imported, err := s.deps.Hosts.ImportHosts(ctx, hs, s.audit(a, "host.import"))
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Hosts: imported}, nil
}
