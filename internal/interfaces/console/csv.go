package console

import "github.com/lober/lober/internal/application/hosts"

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

func csvPlanOf(p hosts.ImportPlan) csvPlan {
	rows := make([]csvRow, len(p.Rows))
	for i, r := range p.Rows {
		rows[i] = csvRow{Row: r.Row, SN: r.SN, Action: r.Action, Problems: jsonProblems(r.Problems)}
	}
	return csvPlan{Rows: rows, Total: p.Total}
}
