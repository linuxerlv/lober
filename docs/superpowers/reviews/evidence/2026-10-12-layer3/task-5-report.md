# Task 5: builtin content skeleton and loading

Implementation clone: `E:/src/lober/.tmp/wf/r5`, branch `feat/render`, base `268def9`.

Scope: one minimal embedded kickstart template (`builtin-kickstart@1`, `lober:kickstart`) and its packages fragment hook (`builtin-packages@1`, `lober:packages`). Root explicitly approved this skeleton scope. Remaining six formats and four hook identities belong to Task 9. No renderer/core/store production behavior was changed.

## Decisions and coverage

- `Templates() []model.Template`, `Hooks() []model.Hook`, and `Fingerprint(any) string` implement the contracted entry points.
- Getters JSON-round-trip the typed manifest. This recursively copies every nested collection and pointer, preserving nil versus empty collections; model fields are all JSON-compatible. Synthetic coverage exercises template provenance pointers, parameter option slices/min/max pointers, assembly maps/instance slices/binding maps/timeout and required pointers, hook implementation series/timing slices, and empty/nil representations. Returned manifest objects are also mutated recursively before checking fresh calls.
- Fingerprints are lowercase SHA256 over deterministic `encoding/json` serialization. An unsupported JSON input panics rather than silently producing an invalid fingerprint; this is documented and tested. Map ordering, content sensitivity, and the independently known JSON-null digest are covered.
- Every currently shipped template/hook must pass core activation, valid fixed IDs, approved builtin metadata, name prefix, duplicate identity/name checks, and builtin-only hook references.
- Each shipped identity/version has an immutable `.sha256` golden. `testdata/releases/v1.json` archives complete original template/hook data. Tests require the current manifest to have matching full historical fixtures and check fingerprints for both current and historical versions.
- Real PostgreSQL tests use `pgtest.Open(t)`, without skips. Fixtures are replayed into one schema before current content, with every release loaded twice, revision stability checked, and all preceding versions re-read after each upgrade. Changed body under an existing version and renamed identity at a higher version both produce `ErrBuiltinMismatch`.
- `internal/archtest/deps_test.go` now requires at least one builtin package in `minChecked`. Production builtin imports remain within its existing standard-library/core allowance. Database imports occur only in tests, with rationale in `doc.go`.

## TDD evidence

Command: `. E:/src/lober/.tmp/l3-env.ps1; go -C E:/src/lober/.tmp/wf/r5 test ./internal/builtin/ -count=1`.

First RED: exit 1, missing `Templates`, `Hooks`, `Fingerprint`, and clone helper (package did not exist).

Behavioral RED after adding temporary zero/identity stubs: exit 1:

```text
--- FAIL: TestShippedContentActivates
    builtin manifest is empty
--- FAIL: TestClonePreservesAllNestedFields
    clone aliases a nested collection or pointer
--- FAIL: TestFingerprintsPinEveryCurrentAndHistoricalVersion
    no historical builtin release fixtures
--- FAIL: TestFingerprintUsesDeterministicJSON
    JSON null SHA256:
--- FAIL: TestFingerprintRejectsUnserializableInput
    unsupported input silently fingerprinted
--- FAIL: TestReleasedVersionsLoadAndUpgradeIdempotently
    no historical builtin release fixtures
FAIL github.com/lober/lober/internal/builtin 1.747s
```

GREEN: `go test ./internal/builtin/ -count=1 -v`, exit 0:

```text
PASS TestShippedContentActivates
PASS TestManifestReturnsIndependentCopies/templates
PASS TestManifestReturnsIndependentCopies/hooks
PASS TestClonePreservesAllNestedFields
PASS TestFingerprintsPinEveryCurrentAndHistoricalVersion
PASS TestFingerprintUsesDeterministicJSON
PASS TestFingerprintRejectsUnserializableInput
PASS TestReleasedVersionsLoadAndUpgradeIdempotently
ok github.com/lober/lober/internal/builtin 1.994s
```

After fixture completeness and revision-stability assertions, focused check: `go test ./internal/builtin ./internal/archtest -count=1`, exit 0 (`builtin 2.109s`, `archtest 1.623s`). After the environment interruption/restart, repeated focused check also exited 0 (`builtin 5.034s`, `archtest 10.363s`). Root separately confirmed the PostgreSQL builtin test passed after restoring its WSL keepalive.

## Full check and interruption

Exact command in the clone: `. E:/src/lober/.tmp/l3-env.ps1; D:/msys/usr/bin/make.exe SHELL=D:/msys/usr/bin/bash.exe check`.

An initial full-check process handle was lost when the agent was interrupted. The first captured rerun exited 2 at `go test ./...`: WSL/PostgreSQL was unavailable and localhost:5432 refused connections. `TestReleasedVersionsLoadAndUpgradeIdempotently`, PostgreSQL store contract/schema tests, and pgtest fixture tests failed for this same connectivity condition. Format, vet, and all non-database packages passed. The complete failure names/output are preserved in `task-5-make-check-db-down.log` alongside this report.

Read-only `pg_lsclusters` started WSL and showed PostgreSQL 18 main online. Focused tests then passed; root restored a persistent foreground WSL keepalive. No source was changed to suppress environment failures and no tests were skipped. A fresh full check was started; format, vet, and all regular tests passed (builtin 1.809s, pgstore 55.309s, pgtest 2.634s). Final result and commit are recorded below after completion.

The second captured full run exited 2 in the race stage. All packages including builtin race (4.996s) passed except `Test契约模板/ListTemplates_按系过滤，看的是给出的那一项`, which failed opening its PostgreSQL schema with the Windows socket error `Only one usage of each socket address (protocol/network address/port) is normally permitted`. The complete output is preserved in `task-5-make-check-socket-error.log`. The focused command `go test -race ./internal/store/pgstore -run 'Test契约模板/ListTemplates_按系过滤，看的是给出的那一项' -count=1` then passed, exit 0, 2.976s. A Windows TCP snapshot showed 1095 TIME_WAIT connections; this observation does not establish the underlying socket failure's cause. No source change was made for it, and the full check was rerun for fresh completion evidence.

## Self-review and remaining scope

Reviewed the full new source, tests, archived serialized objects, and guard diff. `git diff --check` passed. No unrelated source edits were present. The package holds one identity of each kind, with one fingerprint per ID/version; the final seven-template/five-hook inventory is deliberately Task 9.

The v1 JSON archive and hashes must remain immutable. Task 9 must bump existing identity versions when replacing this skeleton or adding implementations to the existing packages hook, and add complete new release fixtures/hashes. Identity names stay unchanged. Only one historical release exists today; the replay skeleton becomes a true old-to-new regression as subsequent releases are added. Final render/golden output and installation verification are outside Task 5.

## Final result

DONE. Final captured `make check` exited **0**, all five stages passed:

```text
gofmt -l $(go list -f {{.Dir}} ./...)
go vet ./...
go test ./...
    all 13 module packages PASS (cached regular suite after prior successful fresh run)
go test -race ./...
    all 13 module packages PASS; pgstore freshly ran in 60.887s
GOOS=linux GOARCH=amd64 go build ./...
MAKE_CHECK_EXIT=0
```

Complete successful output: `task-5-make-check-green.log` alongside this report. Source did not change between the earlier fresh builtin/guard and builtin-race passes and the final complete green run.

Commit: `09de4da579c4f80a7b1d64401e0c5f627faa89a3` — `feat(builtin): 内置内容的骨架、指纹 golden 与装载用例`. Exactly one Task 5 commit, 10 scoped files. Clone working tree clean after commit; ignored scratch generation/check logs are retained locally. Root's report/log artifacts are outside the clone commit as requested.

Environment cleanup: a redundant hidden WSL one-hour keepalive process (11280) was created while diagnosing readiness; root restored its own persistent exec keepalive afterward. This redundant helper is not part of source or tests and can be stopped by root.

