# Task 4 execution report

Status: DONE; finalized `make check` GREEN. Commit: `268def9d881e2b7ba75c0d9977e5f095d811ca10` (`feat(render): 渲染骨架与占位符展开`). Clone is clean after commit.

## Scope and isolation

Read `task-4-brief.md` first, then `constraints.md`, spec §2.5/§2.6, and the scan, snapshot, value, catalog address and Problem contracts. Used a fresh shared local clone (not a worktree):

```powershell
git clone --shared --single-branch --branch feat/render E:/src/lober E:/src/lober/.tmp/wf/r4
```

Clone base: `c3c16bd5b2cb80a7fe0a719086a959643a82537b`.

Implementation files: `internal/render/{render.go,expand.go,doc.go,expand_test.go}`. Minimal supporting changes: `internal/core/scan/{lines.go,lines_test.go}`, six reason codes and their `extraCodes` alignment in core/model, and the existing architecture guard's required new-package thresholds (`layerRender: 1`, `minDirs: 10`). No concrete menu/answer/aux/script assembler was implemented; all four still return `errNotImplemented` after preconditions. No hook lookup or implementation selection is performed.

## Architecture decisions

- Root explicitly approved one provenance for `repo` and `iso`: caller provides `Values["artifacts"]`, rendering uses `catalog.ArtifactURL(base, Snapshot.ISO.Artifacts["repo"])` or the `"iso"` role. These exact role names are the contract for tasks 7/8/10. Caller-supplied `repo` or `iso` is rejected as `render_system_value_reserved`, even when equal to the derived address; there is no fallback. `Request.Values` documents ownership. `lbrace` is always literal `{{`.
- The other caller-owned values are gateway, task, token, sn, smb, and syslog-addr, as needed by a concrete output. Answer preflight scans template and fragment references; future assemblers must check their own additional requirements through the shared resolver. Hook script bodies are never expanded or scanned for render prerequisites.
- Root approved testing the shared internal expander directly with prepared one-line execution-slot/system-part replacements while concrete assemblers are deferred. The expander handles insertion and enforces forbidden characters; tasks 6–8 supply the actual commands.
- `scan.Result.Lines()` owns the newline view of existing Segments. It splits only raw segments, preserves placeholder metadata and rune positions, and represents LF/CRLF separately in `Line.End`. No placeholder lexer is copied into render. Empty input has no lines; a terminal newline does not manufacture a final extra blank line. `Line.Raw()` rebuilds line text without its ending.
- Lists repeat the whole source line once per item; repeated appearances or multiple subvalues of the same list use the same item index. `|join` remains inline with one space. Newlines between repetitions of an unterminated line are LF; terminated lines retain their LF/CRLF ending. Inputs and maps are read-only; output ordering follows body/instance order rather than map enumeration.
- A fragment replaces its whole standalone line: leading indentation is applied to every emitted line, source trailing padding is discarded, and an empty slot removes the source line. Instances preserve order and use their own parameter scope. Internal fragment endings are retained, missing boundaries between instances get LF, and the final emitted line uses the slot's source ending. Inserted braces are never recursively expanded.
- Diagnostic Subjects retain line/rune column and template `ID@version`; fragment instance Paths use `<slot>/<n>` via `model.InstancePath`.
- After self-review, root required explicit leaf mode for fragment instances. Only raw/sys/param segments are expanded there. Structural slot/part segments return the existing `fragment_forbidden_placeholder` Problem without partial bytes, so a hand-built self-referencing fragment terminates rather than re-entering slots. Hook activation remains the authoritative valid-input check; this is a traversal boundary, not another hook validator.

## TDD evidence

All commands ran from `E:/src/lober/.tmp/wf/r4`, with `GOTOOLCHAIN=local` and PATH beginning with the recovered Go SDK, native busybox helper directory, MSYS GCC, and MSYS binaries. SDK: `E:/src/lober/.tmp/go127-complete/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64`. DSN was inherited from root `.pgtest.dsn` by pgtest; credentials were never printed.

Initial tests preceded implementation. The first absent-API probe:

```powershell
go test ./internal/render/ ./internal/core/scan/ -count=1
```

Exit 1: undefined Request, expansion, Render and Result.Lines, confirming the APIs did not exist. Added only compile stubs, reran the same command, and observed assertion RED (exit 1), including:

```text
FAIL TestExpandValuesAndPreserveRaw: got "", wanted system/parameter substitutions and preserved raw text
FAIL TestExpandListLines/{default,unterminated,repeat-once,join,join-sub,several-subs,join-and-repeat,crlf}
FAIL TestExpandFragmentIndentAndLocalParams
FAIL TestExpandOneLineSlotAndPart
FAIL TestExpandProblemsDiscardPartialOutput/{missing-system,missing-base,missing-role,caller-repo,missing-param,missing-sub,inline-fragment}
FAIL TestRenderDispatchAndPreconditions: missing value returned errNotImplemented instead of Problems
FAIL TestLinesPreserveBytesAndSegmentLocations: got "", wanted "plain"
```

Implemented the shared engine, line view and validation. First focused GREEN attempt exposed a test fixture mistake: CIDR subvalue is `addr`, not `ip` (confirmed in model's single accessor table). Corrected those test references without changing the model or implementing a second accessor. Then:

```powershell
go test ./internal/render/ ./internal/core/scan/ ./internal/core/model/ ./internal/archtest/ -count=1
```

Exit 0; all four packages passed (0.563s, 0.548s, 0.716s, 1.460s). Additional coverage verified inserted brace preservation, changed map insertion order, stable reserved-key ordering, UTF-8 problem positions, fragment preflight, and script-body non-expansion. A further focused run passed render and scan (0.553s, 0.554s).

Self-review found missing template-version provenance in Subjects. Added `TestRenderProblemsIdentifyTemplateVersion` before fixing it:

```powershell
go test ./internal/render/ -run TestRenderProblemsIdentifyTemplateVersion -count=1
```

Exit 1, RED: Subject had `template-1` instead of `template-1@3`. Fixed template, slot, and fragment-instance Subjects. Focused run of render/scan/model/archtest passed (0.689s, 0.683s, 0.877s, 1.594s). Corrected the hand fixture's Family to the catalog's `linux`; fresh render test passed (0.706s).

## Authorized PostgreSQL compatibility correction

Root's stable PostgreSQL 18.6 baseline exposed exactly two pre-existing failures and explicitly extended ownership to `internal/store/pgstore/errors.go` and focused translation tests. Initial `make check` failed only on these same existing contracts:

```text
Test契约任务/引用：ISO_被任务引用之后删除报_ErrReferenced，废弃了、任务结束了也一样
Test契约任务/引用：主机被任务引用之后删除报_ErrReferenced，任务结束了也一样
ERROR ... violates RESTRICT ... fk_tasks_iso / fk_tasks_host (SQLSTATE 23001)
FAIL internal/store/pgstore 62.640s
make: *** [Makefile:22: test] Error 1
```

Full initial output: `task-4-make-check.log`. Root's prior independent baseline evidence: `.tmp/l3-db-baseline-retry.log`.

Added five table cases to existing `Test数据库错误的翻译` before the fix: known host/ISO foreign keys on deletion must map to ErrReferenced; non-deletion, unknown foreign key and CHECK-named restrictions must remain the original error.

```powershell
go test ./internal/store/pgstore/ -run Test数据库错误 -count=1
```

Exit 1, RED: both new known RESTRICT deletion cases returned the original wrapped PgError, SQLSTATE 23001. Added `codeRestrict = "23001"` and a separate translation case that maps only `del && knownConstraint(name, true)` to ErrReferenced. No schema or existing contract test was weakened. Rerun exit 0, pgstore focused translation GREEN (1.092s).

[PostgreSQL 18 Appendix A](https://www.postgresql.org/docs/18/errcodes-appendix.html) was consulted directly: class 23 distinguishes `23001 restrict_violation` from `23503 foreign_key_violation`.

## Fragment leaf boundary amendment

Root explicitly requested a regression and rejected silently preserving structural placeholders in hand-built fragments. Before amendment:

```powershell
go test ./internal/render/ -run 'TestExpandReject(SelfReferencingFragment|PartInFragment)' -count=1
```

Exit 1, RED:

```text
FAIL TestExpandRejectSelfReferencingFragment (1.00s): self-referencing fragment expansion did not terminate
FAIL TestExpandRejectPartInFragment: got "ok\nrm -f /run/token", no Problems
```

Added an explicit `fragmentOnly` mode to the common engine, checked structural scan Segments before expansion, and set that mode only when expanding an instance. No lexer or new reason code was introduced. Fresh focused GREEN:

```powershell
go test ./internal/render/ ./internal/core/scan/ ./internal/core/model/ ./internal/archtest/ -count=1
```

Exit 0 (0.793s, 0.795s, 1.030s, 2.400s). The self-reference test has a test-only one-second deadline to make termination an explicit assertion.

## Final verification and self-review

Intermediate `make check` log: `task-4-make-check-final.log`; its ordinary suite passed including pgstore (54.383s) and pgtest (3.653s). Its race suite was already running when the leaf boundary amendment was requested. A fresh complete check on the finalized code is required before commit; final log/result will be appended here. `make check` executes gofmt verification, vet, all-package tests, race tests, and Linux/amd64 cross-build.

`git diff --check` passed. Self-review confirmed:

- All four concrete assembler functions remain explicit stubs.
- Core imports only; no IO, clock, database or network in production render.
- Placeholder parsing always comes from scan; malformed owned placeholders and Jinja stay Raw.
- No mutation of Values, Params, Items, Artifacts or snapshot structures.
- System/parameter/fragment problems discard all partial bytes; unknown Kind uses error.
- Address provenance is single-source, role names and caller ownership documented.
- Five snapshot invariants are documented; no traversal through hooks/ImplFor.
- PostgreSQL compatibility mapping remains limited to recognized foreign-key constraints on deletion.

Expected remaining work belongs to tasks 6–8: concrete file assemblers, execution command generation, system part content and script parameter export using `model.EnvName`/`SnapshotValue.EnvValue`. Task 4 does not claim these implementations.

## Finalized verification and commit

After the fragment boundary amendment, ran a new complete check:

```powershell
make check
```

Exit 0. Complete output is in `task-4-make-check-finalized.log`:

```text
gofmt -l $(go list -f {{.Dir}} ./...)
go vet ./...
go test ./...
ok internal/archtest 1.938s
ok internal/render 0.625s
all other module packages ok (cached)
go test -race ./...
ok internal/archtest 3.992s
ok internal/render 1.631s
all other module packages ok (cached)
GOOS=linux GOARCH=amd64 go build ./...
```

The cached storage results came from the preceding successful full run after the 23001 compatibility fix: normal pgstore 54.383s, race pgstore 86.913s, pgtest normal 3.653s and race 4.707s. That intermediate run also exited 0, but its render result preceded the leaf boundary amendment; the finalized run above rechecked the amended render/guard packages.

Fresh staged `git diff --check` passed. Commit contains 11 files, 792 insertions and 8 deletions. `git status --short` after commit produced no output. No production changes followed finalized verification.

Concerns for integration: root may need to reconcile `extraCodes` total (this clone increases 71 to 77) and guard minimum thresholds with other concurrent task commits. No outstanding task-4 defect is known. All four assemblers intentionally remain stubs per this task's boundary.
