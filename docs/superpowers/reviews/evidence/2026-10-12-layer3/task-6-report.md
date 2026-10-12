# Task 6 implementation report

Status: DONE (commit recorded below). Work clone: `E:/src/lober/.tmp/wf/r6`, base `09de4da`. Task brief read first, then constraints/integration notes, approved spec, inherited protocol and all sixteen legacy outputs. No legacy implementation source was read or copied. No subagents dispatched.

## Implemented

`internal/render/answer.go` assembles kickstart, preseed, autoinstall, autoyast and unattend through the existing shared expansion engine. It prepares only referenced system parts/execslots, consumes frozen flattened snapshot instances, derives artifact values through existing catalog expansion and returns no partial bytes on content problems. Unknown not-yet-implemented formats keep explicit errNotImplemented, not fake output. No production file/database/clock/network IO.

Execslot/system replacements remain one safe line without the forbidden quotes, <>&, backslash, #, or colon-space. Real multiline scaffolds are base64 POSIX-shell payloads or native UTF16LE PowerShell encoded commands; testdata/decoded-scaffolds.md exposes their exact decoded bytes. Nothing is replaced by a hash-only claim.

Linux pre/post-installer slots download and execute wrapper with installer sh; post-target downloads outside target and explicitly chroots for execution. Firstboot slot stages wrapper and every digest hook BEFORE reboot and executes none. Firstboot part stages protected independent finalize.token, native systemd unit/runner, enabling symlink and pending marker. Native runner uses the local finalize credential, emits booted/init-start, executes all firstboot slots in frozen order, and only after success calls staged cleanup. Cleanup emits init-done/ready and removes credentials, staged payloads and cached answers. Transport failure is nonfatal. Required staging download fails; optional download may fail and runtime reports missing local hook (Task8).

Windows uses one native oobeSystem source command chain (`cmd ... firstboot && slot && cleanup`, XML escaped). Firstboot/cleanup encoded PowerShell runs only in target Windows, gets token from local protected file and emits the same protocol phases. Slot command resolves SystemDrive and invokes its locally staged cmd wrapper. It independently reads token in Task8, with no cross-process environment assumption. Windows pre command refers to a real injected wrapper at X:/lober/script-{Seq}.cmd, which Task8 FileAux stages; no assumed stock WinPE PowerShell or HTTP client. No synthetic X helper and no duplicate SetupComplete/FirstLogon execution path.

Added private catalog vocabulary `finalize-token`, NOT author capability exposure. Linux firstboot needs the caller to provide it, with no boot-token fallback. Added canonical catalog.SignalURL for inherited POST route and segment roundtrip test. Root approved adding pure unicode/utf16 to stdAllowed; corresponding guard fixture updated. No net package added. Request docs and existing dispatcher sentinel test updated narrowly for real FileAnswer output.

## Tests and TDD evidence

Environment for every Go/make command:

```powershell
. 'E:/src/lober/.tmp/l3-env.ps1'
```

RED command, run before answer implementation and before testdata goldens existed:

```powershell
go test ./internal/render/ -count=1
```

Exact relevant output (full log `task-6-red.log`):

```text
--- FAIL: TestAnswerGolden (0.00s)
    --- FAIL: TestAnswerGolden/kickstart-full-bios (0.00s)
        answer_test.go:66: open testdata\kickstart-full-bios.golden: The system cannot find the path specified.
...
    --- FAIL: TestAnswerGolden/unattend-minimal-uefi (0.00s)
        answer_test.go:66: open testdata\unattend-minimal-uefi.golden: The system cannot find the path specified.
FAIL
FAIL github.com/lober/lober/internal/render 0.341s
FAIL
```

Exit 1; all twenty matrix cases failed on absent goldens as specified, not compilation. Tests read missing golden first intentionally. Initial implementation then generated fixtures using explicit `LOBER_UPDATE_GOLDEN=1` only in authoring command; ordinary tests never write fixtures. Initial additional focused tests exposed expected stale sentinel/private-catalog contracts plus test extractor assumptions; these were corrected, no feature contract weakened. Guard then explicitly rejected net and unicode/utf16: net removed; root ruled pure UTF16 encoding allowed and whitelist test updated.

GREEN commands:

```powershell
$env:LOBER_UPDATE_GOLDEN='1'
go test ./internal/render -run TestAnswerGolden -count=1
Remove-Item Env:LOBER_UPDATE_GOLDEN
go test ./internal/render ./internal/core/catalog ./internal/archtest -count=1
```

Focused log `task-6-green.log`, exact output:

```text
ok github.com/lober/lober/internal/render 3.882s
ok github.com/lober/lober/internal/core/catalog 0.842s
ok github.com/lober/lober/internal/archtest 2.235s
```

Final fixture include improvement regenerated actual firmware partition dataflow instead of touching empty include file; subsequent `go test ./internal/render/ -count=1` output `ok github.com/lober/lober/internal/render 2.364s`, exit 0, and final full make check below ran after this change.

Coverage: 5 formats × full/minimal × BIOS/UEFI = 20 byte goldens; mandatory catalog parts present; deterministic repeat render; each generated command safe one-line; decoded canonical answer/signal/artifact URLs parsed and segment-unescaped; ASCII Seq route; missing independent finalize credential no bytes; finalize value never author-capability allowed. Four named Appendix A regressions assert explicit KS install source, cmdline, include-after-pre AND real include partition dataflow, BIOS GPT biosboot vs UEFI ESP. Preseed multiline concatenation regression covered by one-line payload and real POSIX syntax.

All decoded Linux staging scripts checked using BOTH actual busybox sh -n and MSYS bash -n. A real shell executes staging with local deterministic download transport writing `exit 71`: firstboot wrapper and hook are saved without execution; real unit/runner/cleanup/token are staged. Native runner executing failed wrapper exits nonzero, preserving token/pending rather than cleanup. Telemetry transport failure is simulated and does not stop before wrapper execution. Native Windows XML parsed; exactly one oobeSystem SynchronousCommand verified; no target firstboot/PowerShell in windowsPE; UTF16 roundtrip includes non-BMP `机器 🐧`. All three emitted Windows native PowerShell payloads parsed through System.Management.Automation.Language.Parser: `Parsed all 3 native PowerShell firstboot/slot/cleanup payloads: no syntax errors`.

Final full-check command:

```powershell
D:/msys/usr/bin/make.exe SHELL=D:/msys/usr/bin/bash.exe check
```

Full final output in `task-6-make-check-green.log`: gofmt, go vet ./..., go test ./..., go test -race ./..., GOOS=linux GOARCH=amd64 go build ./..., all thirteen packages passed, exit 0. Real PostgreSQL test suite passed, not skipped. DSN was inherited from ignored root `.pgtest.dsn`; no credentials printed or committed. Earlier uncached full check also passed (pgstore ordinary 40.697s, race 63.856s), final rerun used valid Go caches plus reran amended render/guard tests.

## Legacy evidence

Read all sixteen legacy files. Ten answer files are scoped reference; six menu/aux/ESXi outputs unchanged and owned by Tasks7/8. `testdata/legacy-line-diff.md` lists EVERY inserted/deleted line with one-based old/new numbers and per-range reasons, comparing each BIOS/UEFI fixture against its respective old unsplit full/minimal reference. Final comparator output: 20 comparisons, 3,850 changed lines (counts include insertions/deletions), zero unclassified changes. Category S1–S5: explicit snapshot/source/fixture ownership changes; B1–B4: explained lifecycle/syntax/regression fixes. `legacy-differences.md` records concise causal explanation and links; `decoded-scaffolds.md` exposes every emitted script. Old global host env/SSH defaults move to per-instance params and Task9 builtin hooks. Handwritten fixtures are intentionally independent of builtin defaults; missing defaults in these fixtures is not approval to omit them from builtin profiles.

## Decisions, sources and cross-task obligations

Full concrete route/path/token/placement contract: `E:/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/task-6-runtime-contract.md`, supplied to root for Tasks8/9/10. Root rulings: private finalize-token; stage Linux before reboot and run in native target; Windows one native source chain due concurrent separate FirstLogonCommands; SystemDrive (no C assumption); pure UTF16 whitelist. No remaining design blockers.

Primary docs checked 2026-10-11:

- https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/system_design_guide/kickstart-script-file-format-reference_system-design-guide : %post --nochroot `/mnt/sysroot`, erroronfail, cmdline.
- https://www.debian.org/releases/trixie/amd64/apbs05.en.html : early/late timing, mounted `/target`, in-target/chroot.
- https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html : early/late commands, `/target`, nonzero fails installation.
- https://documentation.suse.com/sles/15-SP7/single-html/SLES-autoyast/book-autoyast.html : pre/chroot timing and chrooted=false `/mnt`.
- https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-shell-setup-firstlogoncommands-synchronouscommand-order : separate FirstLogonCommands concurrent, so one chain required.
- https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/windows-setup-command-line-options?view=windows-11 : root supplied official /m OEM staging route for Task8 to verify.

Task8: complete wrapper runtime body for all interpreters, digest/timeout/required/optional/log tail/EnvName, resolve firstboot local files/token; Windows startnet injects/stages actual wrappers/hooks/token via injected HTTP client and /m OEM tree before setup reboot, validates copied volume path and exact-once pre execution. No boot credential after reboot.

Task9: actual builtin bodies/defaults/firmware partition hooks, target curl/systemd/package prerequisites, correct outside-chroot late source placement, error propagation, Windows single oobeSystem native chain and login/AutoLogon prerequisite, preserve historic v1 fixtures/fingerprints and bump changed content versions. Task6 does not alter builtin v1.

Task7/layer7: real installer/machine verification for documented contexts, OEM tree copy/Windows login path and Server-specific scope; token issuer independence/90min boot vs24h finalize permissions. Snapshot.Host.Firmware remains optional; renderer introduces no BIOS fallback. Task10 carries these obligations and pure renderer limitations forward.

## Files and self-review

Created answer.go/answer_test.go, catalog signal_test.go, 5 handwritten template fixtures, 10 firmware partition fragment fixtures, 20 answer goldens, legacy summary/detailed line diff/decoded evidence. Narrow modifications: dispatcher stub/docs/test; catalog timing vocabulary/test/address helper; architecture pure UTF16 whitelist/test. No unrelated files or builtin changes.

Self-review caught and corrected: preseed outer semicolons would hide required failures (changed to &&); enclosing Kickstart/AutoYaST scripts now set -eu in fixtures; native signal URL substitution uses LAST /TOKEN/ segment so a gateway containing TOKEN cannot be accidentally split; root-pre context and Windows concurrency are explicit; dummy include replaced by actual heredoc partition write; no source reads legacy implementation; no synthetic artifacts/hooks addresses or lober binary. `git diff --check` clean. Decoder evidence inspected directly, including native runner order/token and canonical gateway hook route.

Scope limitations are planned downstream work, not silent placeholders: FileScript/FileAux remain explicit errNotImplemented pending Task8; builtin real defaults are Task9; machine acceptance is layer7. Task6 output assembly and staging/scaffolds are implemented and tested.

Commit: `5ff3df847d4678cec5f3b0c5d674269be1e1018f` — `feat(render): 五种有旧版可对照的应答文件`. Final clone clean after commit.

## Review fix round 1 — native Windows CommandLine limit

Read complete task-6-review.md and applied receiving-code-review/TDD workflows. Verified Microsoft's primary [CommandLine documentation](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-shell-setup-firstlogoncommands-synchronouscommand-commandline): field permits at most 1,024 characters. Original four Windows commands measured 3,916 UTF-16 characters. Earlier XML tokenization and PS syntax checks did not establish this native schema property. This finding was a real Task6 output defect and has been corrected, not deferred.

Changed Windows replacements to compact local invocations: firstboot/cleanup use `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/firstboot.ps1|cleanup.ps1`; each firstboot slot uses `cmd /d /c %SystemDrive%/ProgramData/lober/script-{Seq}.cmd`. Source still supplies ONE native oobeSystem cmd chain with XML-escaped &&; any slot failure prevents cleanup. There is no duplicate slot dispatch and no WinPE PS invocation. All four one-slot matrix fixtures now measure 304 XML-decoded UTF-16 characters; eight-slot chain also remains under 1,024. Author text/slot counts must obey native field length; XML parsing alone is not Windows schema validation.

The actual two PS files are concrete Task6-owned bytes, not unowned assumed files: reusable private `windowsPartScript(expansion{request:r}, part, subject)` returns immutable UTF-8-BOM bytes with gateway/task missing-value Problems; `windowsPartFile(part)` defines canonical filename. Two new standalone PS byte goldens pin firstboot.ps1 and cleanup.ps1. Task8 FileAux MUST stage exactly these bytes at its local OEM `$1/ProgramData/lober` tree before setup /m copying. Updated task-6-runtime-contract.md pins this responsibility and target paths. PS uses independent local finalize.token, firstboot emits booted/init-start, cleanup emits init-done/ready after success, transport failure nonfatal. Cleanup also deletes the two staged PS files. Actual wrapper runtime/token reading stays with Task8 as originally scoped.

Removed now-unused encodedPowerShell helper/fmt/unicode/utf16 production imports and reverted the no-longer-needed guard whitelist addition/fixture. Original pure UTF16 permission was not a requirement to retain dead code. Tests use test-only UTF16 to count native characters; staged file encoding is BOM-prefixed UTF8, with non-BMP gateway text preserved and exact base64 staging transport roundtrip verified.

RED command (environment script sourced as above):

```powershell
go test ./internal/render -run TestAnswerWindowsNativeCommandLineLimit -count=1
```

Exact output in task-6-fix1-red.log:

```text
--- FAIL: TestAnswerWindowsNativeCommandLineLimit (0.00s)
    --- FAIL: TestAnswerWindowsNativeCommandLineLimit/bios-minimal (0.00s)
        answer_test.go:418: XML-decoded CommandLine 3916 characters exceeds native maximum 1024
    --- FAIL: TestAnswerWindowsNativeCommandLineLimit/bios-full (0.00s)
        answer_test.go:418: XML-decoded CommandLine 3916 characters exceeds native maximum 1024
    --- FAIL: TestAnswerWindowsNativeCommandLineLimit/uefi-minimal (0.00s)
        answer_test.go:418: XML-decoded CommandLine 3916 characters exceeds native maximum 1024
    --- FAIL: TestAnswerWindowsNativeCommandLineLimit/uefi-full (0.00s)
        answer_test.go:418: XML-decoded CommandLine 3916 characters exceeds native maximum 1024
FAIL
FAIL github.com/lober/lober/internal/render 0.390s
FAIL
```

Exit 1, expected native-limit regression before production changes.

GREEN regeneration commands:

```powershell
$env:LOBER_UPDATE_GOLDEN='1'
go test ./internal/render -run 'TestAnswerGolden|TestAnswerWindowsStagedPartsGolden' -count=1
Remove-Item Env:LOBER_UPDATE_GOLDEN
go test ./internal/render -run 'TestAnswerWindows|TestPowerShell' -count=1
```

Output: `ok github.com/lober/lober/internal/render 0.399s`, then `ok github.com/lober/lober/internal/render 0.391s`, exit 0. New regression tests: four matrix decoded-limit cases; exact single source chain/native placement; eight-slot chain order/length; two full staged PS byte goldens; BOM/non-BMP and base64 byte roundtrip; URLs parsed from actual staged native scripts.

Focused final GREEN command:

```powershell
go test ./internal/render ./internal/core/catalog ./internal/archtest -count=1
```

task-6-fix1-green.log exact output:

```text
ok github.com/lober/lober/internal/render 2.573s
ok github.com/lober/lober/internal/core/catalog 0.386s
ok github.com/lober/lober/internal/archtest 1.288s
```

All four Windows native commands measured 304; direct System.Management.Automation.Language.Parser parsing both staged PS files produced `Both staged PowerShell files parse without errors`.

Additional appropriate checks: `gofmt -l internal/render internal/core/catalog internal/archtest` (no output); `go vet ./internal/render ./internal/core/catalog ./internal/archtest` (no output); `go test -race ./internal/render ./internal/core/catalog ./internal/archtest -count=1` produced render3.588s/catalog1.270s/archtest2.471s PASS (task-6-fix1-race-green.log); `GOOS=linux GOARCH=amd64 go build ./...` exit0. Full PostgreSQL suite not rerun per controller instruction: no storage changes; original complete makecheck evidence remains above.

Regenerated all golden/decoded/legacy explanation evidence. New decoded-scaffolds.md shows the concrete staged PS file bodies rather than obsolete inline payloads; two PS goldens include actual BOM bytes. Changed-line comparison remains 20-vs10, 3,850 inserted/deleted lines, zero unexplained; reason B4 now explicitly records compact native limit fix and actual staged-file ownership. Generated Markdown retains its existing CRLF to avoid unrelated churn; `git -c core.whitespace=cr-at-eol diff --check` clean. Self-review confirmed no forgotten production EncodedCommand uses/imports/whitelist, all native path references have Task8 staging ownership, one chain/failure semantics preserved, no installer PS added.

Final post-self-review focused command repeated after strengthening XML extractor error handling: `go test ./internal/render ./internal/core/catalog ./internal/archtest -count=1` => render2.384s/catalog0.390s/archtest1.122s PASS, exit0 (task-6-fix1-final-green.log). Amended Task6 commit: `129edaf8612d018c7c35f042a64a565b60bda8ed` — one Task6 commit preserved. Clone clean. Previous reviewed head `5ff3df8` retained by root for scoped fix diff.

## Review round 2 — exact native CMD evidence, Ubuntu development

Read complete task-6-fix1-review.md. Original length P1 was marked addressed. New path P1 was based on a forward-slash `.exe` analogue rather than the generated `.cmd` wrapper. Controller explicitly ruled that no failure should be forced when the exact existing command succeeds, and tests-only counterevidence is sufficient for reviewer reassessment. No production command, profile, golden or decoded/legacy output was changed in this round; current direct .cmd path contract remains as in 129edaf.

Development/checks moved to Ubuntu mgr (uid1000), sourced `/mnt/e/src/lober/.tmp/l3-env-ubuntu.sh`; SDK `go1.27.1 linux/amd64`, Linux busybox/bash and verified module dependencies. No native Windows Go or MSYS development/check command used. Native CMD/PowerShell execution below is specific target-runtime QA through WSL interop, not the development environment. Controller owns baseline/full PostgreSQL checks; unchanged storage suite not repeated here.

Initial investigative probe auto-quoted Go command args and replaced telemetry with relative batch stubs; those relative stubs failed from the installer's unrelated cwd. This did not reproduce a failure of the actual staged files/actual decoded native command and was discarded as invalid RED evidence. Its log is retained as `task-6-fix2-rejected-probe.log`, not a claimed product regression. The improved exact before-change harness passed (`task-6-fix2-native-before.log`), so no TDD production fix was justified and no fabricated RED is reported.

Native harness uses `syscall.SysProcAttr.CmdLine` so CreateProcess receives actual XML-decoded firstLogon bytes without Go argument quoting. It stages actual generated UTF8-BOM firstboot.ps1 and cleanup.ps1, actual local finalize.token, and a controlled `.cmd` wrapper returning71 or0. Both SystemDrive and WINDIR are redirected to a private temporary tree before cleanup; actual Windows files cannot be targeted. SystemDrive is forward-slash normalized, proving the expanded executable path has no hidden backslash control. Gateway is loopback127.0.0.1:1; transport refusal is nonfatal. The real native wrapper marker proves execution; token retention/deletion proves actual cleanup sequencing. WinPE's real XML-decoded Path is also executed, with only unavailable X:/lober mapped to a private forward-slash root.

Permanent Windows-only package test initially triggered the architecture invariant forbidding all build-excluded Go files. Controller chose to preserve this invariant: harness source is committed as `testdata/native-cmd-qa.go.txt`, compiled temporarily against actual package sources and removed before Linux checks. `testdata/native-cmd-qa.md` documents exact reproducible commands. No guard exception, skipped mandatory test or Windows dependency was added to Linux make check. QA fixture is real test code against current renderer, not a copied/fake implementation.

Exact native QA commands (inside Ubuntu):

```bash
source /mnt/e/src/lober/.tmp/l3-env-ubuntu.sh
cd /mnt/e/src/lober/.tmp/wf/r6
test ! -e internal/render/answer_cmd_windows_test.go
cp internal/render/testdata/native-cmd-qa.go.txt internal/render/answer_cmd_windows_test.go
trap 'rm -f /mnt/e/src/lober/.tmp/wf/r6/internal/render/answer_cmd_windows_test.go' EXIT
GOOS=windows GOARCH=amd64 go test -c -o /mnt/e/src/lober/.tmp/task6-native-cmd.test.exe ./internal/render
cd internal/render
/mnt/e/src/lober/.tmp/task6-native-cmd.test.exe -test.run TestAnswerWindowsNativeCMD -test.v
```

`task-6-fix2-native-green.log` exact substantive output:

```text
=== RUN   TestAnswerWindowsNativeCMDFirstboot
=== RUN   TestAnswerWindowsNativeCMDFirstboot/wrapper-exit-71
raw decoded chain executed wrapper exit=71; token retained=true; staging root forward slashes=C:/Users/mgr/AppData/Local/Temp/.../001
=== RUN   TestAnswerWindowsNativeCMDFirstboot/wrapper-exit-0
raw decoded chain executed wrapper exit=0; token retained=false; staging root forward slashes=C:/Users/mgr/AppData/Local/Temp/.../001
--- PASS: TestAnswerWindowsNativeCMDFirstboot (13.81s)
    --- PASS: TestAnswerWindowsNativeCMDFirstboot/wrapper-exit-71 (5.01s)
    --- PASS: TestAnswerWindowsNativeCMDFirstboot/wrapper-exit-0 (8.80s)
=== RUN   TestAnswerWindowsNativeCMDPre
raw decoded WinPE Path with mapped forward-slash staging root executed wrapper exit=71
--- PASS: TestAnswerWindowsNativeCMDPre (0.03s)
PASS
```

Full log retains exact temporary directory names and file/line diagnostics. Thus actual generated .cmd paths execute in this native environment; a failing .exe analogue alone does not establish the review's claimed .cmd failure. Runtime contract now links this evidence and keeps layer7 actual WinPE/image/copy obligations explicit. Reviewer adjudication remains controller-owned.

Final focused Linux commands (temporary test source removed):

```bash
go test ./internal/render ./internal/core/catalog ./internal/archtest -count=1
go vet ./internal/render ./internal/core/catalog ./internal/archtest
go test -race ./internal/render ./internal/core/catalog ./internal/archtest -count=1
```

`task-6-fix2-linux-green.log`: render0.432s/catalog0.011s/archtest1.442s PASS. Vet no output. `task-6-fix2-linux-race-green.log`: render1.493s/catalog1.027s/archtest2.291s PASS. Native CMD not part of Linux test dependency. Existing 20 golden/4 limit/eight-slot/PS-byte/shell/URL tests passed unchanged. No full database suite rerun.

Changes only: concrete separate native QA source text fixture and reproduction instructions, report/runtime contract evidence. No unneeded cwd redesign, production edits, reblessed golden or architecture exception. Before amending, clone-local Linux Git identity is copied from the existing commit's committer (global identity untouched, author preserved).

Round2 amended Task6 SHA: `b5ca3ce09c13576276fa2aca4d403ed754713680` — one Task6 commit preserved. Linux clone clean. Exact fix diff against controller-retained `129edaf` contains only the two native QA source/documentation artifacts; production, existing tests and goldens unchanged.
