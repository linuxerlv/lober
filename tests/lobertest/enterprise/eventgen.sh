#!/usr/bin/env bash
set -euo pipefail
# A store-contract producer for transport tests. Never changes task state or
# emits machine/install signals; restricted to a disposable enterprise schema.
root=$(cd "$(dirname "$0")/../../.." && pwd)
mkdir -p "$root/.tmp"
scratch=$(mktemp -d "$root/.tmp/event-probe-XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT
cat > "$scratch/main.go" <<'GO'
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "net/url"
    "os"
    "regexp"
    "strconv"
    "strings"
    "time"
    "github.com/lober/lober/internal/store"
    "github.com/lober/lober/internal/store/pgstore"
)

func fail() { fmt.Fprintln(os.Stderr, "Restricted event probe failed (database details suppressed)"); os.Exit(1) }
func main() {
    if len(os.Args) != 6 { fail() }
    info, err := os.Stat(os.Args[1]); if err != nil || info.Mode().Perm() & 0077 != 0 { fail() }
    raw, err := os.ReadFile(os.Args[1]); if err != nil { fail() }
    var cfg struct { PG struct { DSN string `json:"dsn"` } `json:"pg"` }
    if json.Unmarshal(raw, &cfg) != nil { fail() }
    u, err := url.Parse(cfg.PG.DSN); if err != nil { fail() }
    schema := u.Query().Get("search_path")
    if schema == "" {
        m := regexp.MustCompile(`(?:^|\s)search_path=(enterprise_[a-f0-9]{16})(?:\s|$)`).FindStringSubmatch(cfg.PG.DSN)
        if len(m) == 2 { schema = m[1] }
    }
    if !regexp.MustCompile(`^enterprise_[a-f0-9]{16}$`).MatchString(schema) { fail() }
    start, e1 := strconv.Atoi(os.Args[3]); count, e2 := strconv.Atoi(os.Args[4])
    if e1 != nil || e2 != nil || start < 1 || count < 1 || count > 1000 || !regexp.MustCompile(`^[a-z0-9-]{1,40}$`).MatchString(os.Args[5]) { fail() }
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second); defer cancel()
    st, err := pgstore.Open(ctx, cfg.PG.DSN, pgstore.WithMaxConns(2)); if err != nil { fail() }; defer st.Close()
    task, err := st.GetTask(ctx, os.Args[2])
    if err != nil || string(task.State) != "pending" || !strings.HasPrefix(task.HostSN, "ENTERPRISE-") { fail() }
    rows := make([]store.NewEvent, count)
    for i := range rows {
        n := strconv.Itoa(start+i)
        rows[i] = store.NewEvent{At: time.Now().Unix(), Kind: "test.transport", Key: os.Args[5]+"-"+n,
            Fields: map[string]string{"probe": n, "qualification": "transport-only"}}
    }
    result, err := st.AppendEvents(ctx, task.ID, rows); if err != nil { fail() }
    if err := json.NewEncoder(os.Stdout).Encode(result); err != nil { fail() }
}
GO
cd "$root"
go build -o "$1" "$scratch/main.go"
