### Spec Compliance

- ✅ Spec compliant for the Task 8 gate, with two nonblocking Minor findings below. Reviewed base c284faf5222b8c4b4dd1a1a370ba8033a5fea1d8 to head 555afc167f5129b2526a13f42f9221cb79b81f2f against task-8-brief.md, constraints.md, integration-notes.md and the Task 8 rulings in progress.md. The Linux-only user override and recorded target-runtime scope control this assessment.
- ✅ Every requested production/test surface is represented: internal/core/catalog/delivery.go:59 registers the sole Agama answer name and delivery_test.go:93 removes its pending cells; internal/render/answer.go:31 enables Agama/ESXi expansion; menu.go:74 adds the remaining menus; aux.go:212 implements Windows startup; script.go:14 implements FileScript; remaining_test.go:18 and task8_golden_test.go:55 pin the new menu/answer/aux/runtime goldens.
- ✅ Runtime requirements are implemented in internal/render/script.go:29: byte-preserving EnvName/EnvValue launchers, stable parameter ordering, digest checks, timeout handling, required failure propagation, optional continuation, per-hook telemetry and 4096-byte tails. internal/render/script_test.go:171 exercises hostile values, mixed interpreters, order and log tails; script_test.go:198 exercises required/optional/digest/timeout cases.
- ✅ Firstboot remains offline and uses the independent credential: internal/render/script.go:61 reads the local token; answer.go:294 embeds immutable ESXi payloads under the approved exception; aux.go:212 stages Windows tools, wrappers, raw hooks and owned PowerShell helpers before Setup. runtime_lifecycle_test.go:75 and runtime_lifecycle_test.go:186 exercise Linux/ESXi ordering, failure suppression of cleanup and credential lifetime.
- ✅ Existing actual-firmware checks are retained in internal/render/menu.go:16. Existing immutable original ESXi boot.cfg behavior is not altered by the Task 8 aux dispatch addition. No production filesystem/network/clock/database access or third-party dependencies are introduced by the reviewed changes.
- ⚠️ Cannot verify from diff: the complete pre-existing validation and purity architecture, private finalize-token capability registration, unchanged original boot.cfg implementation and all prior Task 6/7 scaffold guarantees. The controller should retain their prior gates and the later whole-branch review; this task does not re-certify them.
- ⚠️ Cannot verify from diff: Task 9 complete native profiles, actual Agama product IDs/schema, firmware-specific partitions, required installed tools, Windows login prerequisite and the single FirstLogonCommand chain, plus builtin version/fingerprint replay. These remain Task 9 work, as recorded in internal/render/testdata/runtime-contract.md:19.
- ⚠️ Cannot verify from diff: layer 4 artifact existence/architecture/ABI attestation and layer 7 real installation, credential issuance/TTL/authorization, telemetry validation/deduplication, Windows native UTF8/full-width status/process handling and OEM copying, ESXi native $0 ownership and Secure Boot exclusion. internal/render/testdata/runtime-contract.md:25 explicitly limits the current UTF8-manifest helper on Server 2016/2019; static outputs do not establish support. Windows SSH key support beyond 2025 remains unverified in docs/superpowers/specs/2026-09-30-lober-rewrite-design.md:414.

### Strengths

- internal/render/menu.go:50 and remaining_test.go:174 make required artifact roles explicit and test missing roles rather than assuming a stock WinPE HTTP/PowerShell environment.
- internal/render/script.go:162 centralizes sorted environment export using model.EnvName and EnvValue; immutable hook content is kept outside parameter expansion. script_test.go:171 checks actual hostile bytes and list newlines through generated POSIX execution.
- internal/render/answer.go:335 checks native ESXi file ownership before credential-bearing file deletion; runtime_lifecycle_test.go:279 and runtime_lifecycle_test.go:332 test unrelated marker, symlink and relative-path retention.
- internal/render/remaining_test.go:225 verifies UTF8 BOM-copy bytes while preserving the digest cache. The report and runtime-contract.md:23 correctly distinguish this evidence from native Windows execution.
- internal/render/script_test.go:242 reproduces the watchdog cancellation interleaving deterministically, instead of accepting an occasional passing retry.
- internal/render/testdata/runtime-contract.md:35 supplies primary-source provenance and explicit machine-validation limits. The implementation remains within the approved target-output scope.

### Issues

#### Critical (Must Fix)

- None found.

#### Important (Should Fix)

- None found.

#### Minor (Nice to Have)

- internal/render/testdata/esxi-answer.golden:2 and internal/render/task8_golden_test.go:50 — The fixture pins literal rootpw --iscrypted {{param:root-password}} even though its Snapshot supplies fixture-hash. Consequently this golden does not exercise the intended password substitution and its output is not a usable directive; runtime-contract.md:50 describes the fixture value as if substituted. Use a supported parameter identifier consistently in template and snapshot, regenerate this fixture, and assert the expected substituted line. This is a test/provenance defect, not a demonstrated failure for valid production snapshots.
- internal/render/script.go:29 (posixRuntime, ESXi watchdog cancellation branch) — ESXi cancels only the watchdog shell with kill -KILL "$watchdog"; its sleep child survives until the original hook timeout expires. Hook timeout enforcement still uses the dedicated hook process group, so this does not overturn required/optional results, but repeated fast hooks unnecessarily accumulate orphan sleepers. Launch/cancel the watchdog in its own group or explicitly stop its sleep child, retaining the cancellation-status guard. Add a focused cancellation test if this is addressed.

### Assessment

**Task quality:** Approved, with Minor items deferred to the ledger.

**Reasoning:** The changes cover the task's format and runtime interfaces with deterministic generation and meaningful POSIX execution evidence. No new Critical or Important defect was established; actual target installations and subsequent builtin integration remain explicit downstream obligations.

### Review checks and boundaries

- Read the supplied review package rather than deriving a new git diff; no git commands, source edits, native Windows execution or suite reruns were performed.
- Initial combined output truncated the beginning of the package. Re-read package lines 1–275 to recover that omitted portion, then read the remaining package in bounded sections. No changed source file was read separately.
- Named outside-diff risk: the ESXi golden retains a parameter placeholder despite a supplied value. Focused inspection of internal/core/scan/scan.go's placeholder contract and a targeted symbol lookup in scan.go/model/param.go did not justify broadening the search; the finding rests on the exact changed golden and fixture bytes.
- Read task-8-fullcheck.log: Go 1.27.2 Linux, gofmt/vet, ordinary tests, race tests and Linux amd64 build all passed; changed renderer ran in both test modes. Read the focused-log header and searched task-8-green.log for warnings/failures; the only matching panic text was the name of passing expected-panic tests. No test warning was found. WSL emitted a host proxy warning in review command transport, not in the submitted test logs.
- No focused test was necessary: the two Minor observations follow directly from the generated text, and the supplied evidence answers the execution paths used for the task gate.

