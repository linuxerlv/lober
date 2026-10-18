# Linux installation qualification implementation plan

> **For agentic workers:** Use executing-plans for the execution-chain work. Independent bounded web dependency and ISO compatibility repairs use dispatching-parallel-agents with disjoint ownership. Test execution is VM-only.

**Goal:** Deliver the missing Linux machine execution chain and prove actual Debian, Ubuntu, SUSE, AlmaLinux and Rocky Linux installations on disposable VM disks; report Linux results before proceeding to ESXi7/8 or WindowsServer2022.

**Architecture:** Keep existing core/store/render contracts. Persist execution metadata with task CAS updates, events and user audits in the same PostgreSQL transaction. A state-machine service owns transitions, attempt-bound credentials and recovery scanning; the HTTP gateway depends on declared interfaces, and assembly connects the renderer, store, notifier and services. Reuse canonical catalog URLs and immutable task snapshots.

**Tech Stack:** Go1.27.2, PostgreSQL16, existing Vue/Node24 workspace, PVE8-style API and disposable Linux guests on isolated vmbr1; all product tests run on192.168.137.21.

**Spec:** Existing rewrite design §§3 C27–C35,4.7,5.4,6.2 and7; enterprise testing plan Tasks5–10. User's2026-10-18 staged scope supersedes older Windows/ESXi version matrix only for the later, unapproved stage.

## Global constraints

- Do not modify dirty primary checkout, existing QA service, existing guest disks or unrelated PVE VM100. Only newly created qualification disks/VMs may be installed.
- Source baseline dev88bce232; independent worktree codex/linux-install-qualification. Product tests/fixes flow to dev only after verification; black-box tests/report stay on test.
- SN remains the sole matching identity. Verify actual arch/firmware at boot; no task means local boot without persistent records.
- Tokens are task/attempt/scope-bound; install lifetime90minutes, separate finalize lifetime24hours. Persistent signing key0600. No credential path/request body logging.
- State and runtime metadata use one expected state/revision CAS transaction with events/audit. Required hook failures stop the task. Once rebooting has been reached, retry is forbidden even after failure/cancellation/restart.
- Scripts and files use canonical catalog paths and the existing renderer. Frozen hook digest allowlist only. No simulated done/ready evidence.
- Missing media/functional requirements remain explicit blockers. New dependency forks/patches require source/provenance and functional regression evidence; no audit suppressions.

## Review focus

1. Replayed/late/changed signals cannot rewind state, revive terminal tasks or bypass required hooks; test exact duplicate and conflicting duplicate.
2. Concurrent boot/cancel/retry/timeout must preserve a single active task and transactional audit/event consistency.
3. Old tokens fail after retry, cross-task use fails, finalize credentials cannot download installation files or send installer-phase signals.
4. Reboot/initialization failure cannot cause automatic reinstallation; test persistent irreversible marker after service restart and failed user retry.
5. Official Debian netboot and SUSE ISO metadata/layout must be validated without weakening version/product/repository completeness.

## Task1 — isolated environment and media inventory

- Record available resources, official media checksums and supported exact versions; use official netboot auxiliary for Debian.
- Create only owned PVE target VM and separate data disk; verify serial, size, blank filesystem and mount status before formatting.
- Preserve original Kea/TFTP/nginx configuration; use per-target boot assignment or a private isolated service/network where needed.
- Verify storage budget for RHEL DVD plus extracted repo; use sequential real installs, preserve sanitized evidence.

## Task2 — independent dependency and ISO repairs

Web owner: web/** only; remove dormant Nitro mock, fixed smol-toml minor override, substantive reviewed braces remediation with risk record; VM RED/GREEN compatibility, build and audit.
ISO owner: internal/iso/** only; official Debian exact update metadata and Leap15/16 directory/product recognition; VM RED/GREEN positive/negative fixtures. Notify parent before catalog changes.
Parent integrates only reviewed files; no shared-file or Git-index ownership by workers.

## Task3 — durable task execution transaction

Files: internal/core/model/execution.go; store/store.go; pgstore/rows_tasks.go,tasks.go; new pgstore execution contract tests.
Consumes existing TaskExpect, NewEvent, Audit. Produces UpdateTaskExecution(ctx,id,expect,to,runtime,events,audit) with a persisted execution JSON column and atomic CAS/state/events/audit.
- Write real PostgreSQL persistence, stale CAS, event/audit rollback and restart-read regressions; observe RED.
- Add nullable runtime metadata preserving legacy task JSON and snapshots; migrate through existing model metadata without index/constraint relaxation.
- GREEN focused store tests; commit parent-owned files separately from workers.

## Task4 — execution service, credentials and timeout recovery

Files: internal/execution/{service,token,signal,control}.go and focused tests.
Consumes store interfaces, explicit clock and notifier. Produces Boot/Authorize/Answer/Signal/Cancel/Retry/Sweep methods consumed through gateway/console interfaces.
- RED: illegal boot, boot cap, scope/expiry/cross-task/old-attempt token, exact/conflicting duplicate signals, required/optional failure, out-of-order rank and irreversible post-reboot retry.
- Implement bounded CAS retries, durable progress/deadlines/attempt epoch, signing and constant-time verification; only source snapshot decides slot/digest validity.
- Fresh/restarted sweeper reads pending work from DB; no reliance on in-memory notification.
- GREEN real-store tests with deterministic explicit time; no live disk writes in these tests.

## Task5 — canonical machine gateway and event/log transport

Files: internal/machine/{http,files,signals}.go and HTTP tests; architecture guard registration.
Consumes declared engine/renderer interfaces; produces plain text machine endpoints /v1/boot,/v1/answer,/v1/signal and static boot script.
- RED: every URL rendered by the existing formats resolves, unsupported files/digests/scopes reject, body-size limits and secret-free error handling.
- Serve only allowed frozen files; hook bytes must match declared digest. Actual first answer advances installing once. No console envelope or authentication on machine token routes.
- Collect bounded binary-safe hook logs as events; add scoped syslog collection without cross-task attribution guesses.
- GREEN integration and architecture dependency tests; gateway cannot import execution/render/pgstore siblings directly.

## Task6 — assembly, configuration and service lifecycle

Files: cmd/lober config,providers,taskcontrol,main,wire injector/generated output and assembly tests.
- Explicit machine.enabled activation preserves existing console-only deployments. Enabled mode requires stable protected signing key and configured public endpoints; disabled mode retains explicit unavailable task-control errors.
- Wire real execution adapters, HTTP routing, shutdown and recovery scanning; no raw token/access logging.
- RED enabled-service HTTP lifecycle test then GREEN; regenerate Wire on VM and check drift. Run make check, uncached Go tests, frontend checks; freeze verified candidate.

## Task7 — real media import and Linux target installation

- Freeze source/binary/media identities. Register/import real ISO through current API, use built-in/approved templates and real host registration/batch task creation.
- Boot dedicated PVE target by actual PXE/iPXE into the machine gateway. Capture serial, task events, service logs and guest inspection.
- Verify Debian, Ubuntu, SUSE15/16 where available, AlmaLinux and Rocky Linux separately. Source unavailable or unsupported is explicit FAIL/BLOCKED, never a substitute distro PASS.
- Each success requires server done, real phase/hook evidence, actual installed OS/version, bootable target disk, networking/SSH and credential cleanup. Repeat local boot must not reinstall. Required-hook failure and post-reboot retry protection have representative real scenarios.
- Tests stop before ESXi7/8 and Windows2022, which require user confirmation after this report.

## Task8 — Linux-stage qualification report and branch integration

- One whole-branch independent review; fix significant findings with recorded regressions, then rerun affected checks on frozen inputs.
- Integrate verified product commits to dev then test according to existing rule. Publish sanitized Linux matrix, evidence digests and defects. No remote push/main merge.
- Capacity/24-hour soak and supported-upgrade qualification remain separate enterprise work; do not call Linux success full release approval.
