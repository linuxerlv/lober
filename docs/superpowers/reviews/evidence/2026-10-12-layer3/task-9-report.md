# Task9 implementer report

Status: DONE. Implementation and Ubuntu verification complete; controller review is next. No native installation success is claimed.

Base: 555afc167f5129b2526a13f42f9221cb79b81f2f. Clone: /mnt/e/src/lober/.tmp/wf/r9. Scope: internal/builtin only. No Task8 product edits, core/schema migration, global tool/config installation, native Windows execution, subagents or reviewer dispatch.

## Changes and decisions

Seven compiled embed templates assemble all five native hooks across Kickstart RHEL/Rocky/Alma/openEuler/OpenCloudOS, Debian, Ubuntu, AutoYaST SLES15/Leap15, Agama SLES16/Leap16, Windows Server and ESXi. Native fragments are used where available; executable hooks consume environment parameters. Production builtin has no render dependency, network/database access or external filesystem reads. Existing deep-clone API behavior is preserved.

Kickstart/packages become version2; new identities start at1. Fourteen answer goldens, the complete v2 release fixture and twelve current fingerprints are frozen. Historical v1 bytes remain identical. PostgreSQL replay checks both content and stored revisions of older versions. Production credentials have no defaults; Agama product and Windows edition come from actual media offer sets.

Firmware selection is native: Kickstart probes /sys/firmware/efi; Debian atomic, Ubuntu direct GPT, AutoYaST boot proposal and Agama default volumes use installer firmware decisions. Windows probes PEFirmwareType and writes distinct DiskPart programs: UEFI ESP260/MSR16/OS3; BIOS recovery750/type27, active system550/OS3. Disk0 is locked in both hook binding and InstallTo. The approved BIOS layout reserves recovery space without proving WinRE setup or later growth. ESXi BIOS menu delivery rejects firmware_mismatch.

Ubuntu RAM requirements live in a body comment, and its hostname declaration serves identity plus NoCloud metadata. AutoYaST explicitly locks root and nests native authorized_keys under the configured user, avoiding an installer-stage race. Agama has one post/systemd firstboot path. Windows uses one short ordered FirstLogonCommand and supplied AutoLogon credentials. ESXi packages mean explicitly provided qualified individual VIB URLs.

## Commands and evidence

All checks ran inside WSL Ubuntu after `source /mnt/e/src/lober/.tmp/l3-env-ubuntu.sh` and `cd /mnt/e/src/lober/.tmp/wf/r9`. Version: `go version go1.27.2 linux/amd64`. The private PostgreSQL DSN was never printed or committed.

All evidence paths below are absolute and live in the SDD evidence directory for Task10 archival.

| Check | Exact command | Result / evidence |
|---|---|---|
| Functional RED | `go test ./internal/builtin/ ./internal/render/ -count=1` | FAIL: manifest1/1 instead of7/5, missing installers/hooks; render PASS. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/red.log |
| Native RED | `go test ./internal/builtin -run 'TestNativeInstallerContracts\|TestAgamaProductAndWindowsEditionComeFromMedia\|TestLinuxFirstbootIsStaged' -count=1` | FAIL before implementation for absent formats/contracts. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/native-red.log |
| Lifecycle RED/GREEN | `go test ./internal/builtin -run TestBuiltinDeliveryLifecycle -count=1` | RED Ubuntu hostname param_required; GREEN after builtin declaration fix. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/lifecycle-red.log and /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/lifecycle-green.log |
| AutoYaST RED/GREEN | `go test ./internal/builtin -run TestAutoYaSTNativeUserAndKey -count=1` | RED expected locked root plus user, got1; GREEN after native user/key fix. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/autoyast-red.log and /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/autoyast-green.log |
| Golden/release RED | `go test ./internal/builtin/ ./internal/render/ -count=1` | FAIL for absent current fixtures/fingerprints/goldens after behavior checks. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/golden-release-red.log |
| One-time recording | `LOBER_RECORD_BUILTIN=1 LOBER_RECORD_RELEASE=1 go test ./internal/builtin -run 'TestAllBuiltinAnswersFromSnapshots\|TestRecordCurrentRelease' -count=1` | PASS; refuses existing file overwrite. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/record.log |
| Final GREEN | `go test ./internal/builtin ./internal/render -count=1` | PASS builtin0.529s, render3.825s. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/green.log |
| Real PG replay | `go test ./internal/builtin -run TestReleasedVersionsLoadAndUpgradeIdempotently -count=1 -v` | PASS0.32s, package0.327s, no skip. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/pg-replay.log |
| Full check | `make check` | EXIT_STATUS=0: gofmt/vet/test/race/Linux amd64 build. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/fullcheck.log |
| Self-review | `git diff --check`; `git diff --cached --check`; historical `git diff --exit-code` and `sha256sum`; staged scope assertion | PASS only80 builtin paths,2002 insertions/33 deletions; historical bytes unchanged. /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/self-review.log |

The behavioral suite uses real snapshot.Build and the same Render entry for answers, menus, auxiliaries and every execution slot. Activation covers all shipped templates/hooks; all thirteen series build. Windows2016/2019/2022/2025 static edition selection and ESXi8/9 static firstboot profiles are checked, with absent media choices/credentials rejected.

Kickstart executes the emitted partition programs under Ubuntu bash and BusyBox sh, replacing only the firmware probe and comparing complete BIOS biosboot/UEFI ESP output plus nvme selection. Windows inspects exact separate branch programs and disk0/partition3 agreement without native CMD simulation. Other proposal tests check the configured disk and native proposal. Debian's actual early mirror command runs with a debconf shim. Linux SSH scripts perform real key file writes/chown/chmod with only NSS replaced. Both shell parsers check POSIX hook syntax. Evidence also exists at /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/behavior-first.log.

## Native validation and sources

Commands:

```sh
GOPROXY=https://goproxy.cn go run github.com/google/go-jsonnet/cmd/jsonnet@v0.21.0 internal/builtin/testdata/answers/agama-uefi.golden > .tmp/task9/agama-final.json
python3 internal/builtin/testdata/native_validate.py .tmp/task9/agama-final.json .tmp/task9/sources
```

Output: `Agama Jsonnet/v17 pinned native schema PASS` and `Ubuntu native YAML structure/values PASS`. Evidence: /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/native-final.log.

Agama source: v17 commit6d218f662791a49c7ded902da619558448ef08b8, rust/agama-lib/share. Four schemas have pinned SHA256 values and local-only reference resolution in tracked native_validate.py. The v17 release discusses SLES16/Leap16, but this does not certify the exact build inside each acceptance ISO; importer qualification remains required.

Default Go proxy initially timed out; a command-scoped GOPROXY resolved it. An optional local Python Jsonnet build lacked Python3.14 headers and was abandoned. An initial deprecated schema resolver tried an upstream $id URL; the final helper uses a local registry and correct schema draft. These development tool failures did not become native compatibility claims. No global configuration was changed.

Durable primary references and format/runtime boundaries are in /mnt/e/src/lober/.tmp/wf/r9/internal/builtin/testdata/native-content.md: official Red Hat/pykickstart/openEuler, Debian installer/partman, Canonical Subiquity, SUSE15-SP7 AutoYaST, Agama profile/storage/scripts plus pinned v17 source, Microsoft firmware/DiskPart/Unattend/AutoLogon/OpenSSH, VMware/Broadcom ESXi/VIB documentation. Each native decision has its associated source and qualification boundary there; this tracked document survives SDD cleanup.

## Self-review and later-layer limits

Reviewed all production bodies/scripts, manifest/parameter/assembly declarations, native/lifecycle tests and release fingerprint logic. Confirmed every hook is referenced, slots match assemblies, credentials have no usable public default, native/env parameter handling is appropriate, staging roots agree with Task8, Windows disk loci agree, AutoYaST key belongs to the native user and Agama has a single firstboot path. Complete staged diff is clean. Only builtin files changed; go.mod, core, render and store are unchanged. No post-fullcheck product changes were made. No identified Task9 product blocker remains.

Historical raw file hashes match the base:

- v1.json: 266d3b6a7bce5488a6720ad17242beb0fc6b832a7ad8f3518c2f1eb798a382d3
- hook-builtin-packages-v1.sha256: 1b204804abe59133121329cd6b5e564f8360e0a92dd591c5ce13d60fa95587fa
- template-builtin-kickstart-v1.sha256: 8f8691e9f246b1c1aeaf3adde308ffc943ccb3f0515eb27966e170f7bff24991

Layer4 must provide real immutable media roles/offers, repositories/packages and qualified helper ABIs/native installer versions. Layer5 must invoke EnsureBuiltin hooks before templates, filter current builtin identities and collect/activate snapshot parameters. Layer7 must verify actual firmware/Secure Boot, serve digest-frozen payloads with independent finalize credentials, and run BIOS/UEFI installation checks for native partition creation, stages, network/firewall, accounts, SSH, services and persistence.

Ubuntu RAM sizing depends on the actual ISO. Windows2016/2019 static edition coverage does not qualify the current UTF8 firstboot route; SSH is guaranteed by this content contract only for Server2025. No native Windows ACL/API/Setup execution occurred. ESXi8/9 require disabled Secure Boot and real applet/owned-$0 ABI/persistence qualification. BIOS Windows recovery space is reserved only. Agama schema validation does not establish exact ISO compatibility. These explicit later-layer duties are not represented as passing installation tests.

## Changed files and commit

The complete file list and commit are appended from Git below.

Commit: 5075f45dcea0cf217cf736dc44d2202628383553

Working tree clean; exactly one commit above base.

- internal/builtin/account.go
- internal/builtin/agama/account.jsonnet
- internal/builtin/agama/answer.jsonnet
- internal/builtin/agama/network.jsonnet
- internal/builtin/agama/packages.jsonnet
- internal/builtin/agama/partition.jsonnet
- internal/builtin/agama/sshkey.sh
- internal/builtin/autoinstall/account.yaml
- internal/builtin/autoinstall/answer.yaml
- internal/builtin/autoinstall/network.yaml
- internal/builtin/autoinstall/packages.yaml
- internal/builtin/autoinstall/partition.yaml
- internal/builtin/autoinstall/sshkey.yaml
- internal/builtin/autoyast/account.xml
- internal/builtin/autoyast/answer.xml
- internal/builtin/autoyast/network.xml
- internal/builtin/autoyast/packages.xml
- internal/builtin/autoyast/partition.xml
- internal/builtin/autoyast/sshkey.xml
- internal/builtin/builtin.go
- internal/builtin/content_test.go
- internal/builtin/esxi/account.cfg
- internal/builtin/esxi/answer.cfg
- internal/builtin/esxi/network.cfg
- internal/builtin/esxi/packages.sh
- internal/builtin/esxi/partition.cfg
- internal/builtin/esxi/sshkey.sh
- internal/builtin/fingerprint_test.go
- internal/builtin/kickstart/account.cfg
- internal/builtin/kickstart/answer.cfg
- internal/builtin/kickstart/network.cfg
- internal/builtin/kickstart/partition.cfg
- internal/builtin/kickstart/sshkey.cfg
- internal/builtin/native_behavior_test.go
- internal/builtin/network.go
- internal/builtin/packages.go
- internal/builtin/partition.go
- internal/builtin/preseed/account.cfg
- internal/builtin/preseed/answer.cfg
- internal/builtin/preseed/network.cfg
- internal/builtin/preseed/packages.cfg
- internal/builtin/preseed/partition.cfg
- internal/builtin/preseed/sshkey.sh
- internal/builtin/render_test.go
- internal/builtin/sshkey.go
- internal/builtin/testdata/answers/agama-bios.golden
- internal/builtin/testdata/answers/agama-uefi.golden
- internal/builtin/testdata/answers/autoinstall-bios.golden
- internal/builtin/testdata/answers/autoinstall-uefi.golden
- internal/builtin/testdata/answers/autoyast-bios.golden
- internal/builtin/testdata/answers/autoyast-uefi.golden
- internal/builtin/testdata/answers/esxi-ks-bios.golden
- internal/builtin/testdata/answers/esxi-ks-uefi.golden
- internal/builtin/testdata/answers/kickstart-bios.golden
- internal/builtin/testdata/answers/kickstart-uefi.golden
- internal/builtin/testdata/answers/preseed-bios.golden
- internal/builtin/testdata/answers/preseed-uefi.golden
- internal/builtin/testdata/answers/unattend-bios.golden
- internal/builtin/testdata/answers/unattend-uefi.golden
- internal/builtin/testdata/fingerprints/hook-builtin-account-v1.sha256
- internal/builtin/testdata/fingerprints/hook-builtin-network-v1.sha256
- internal/builtin/testdata/fingerprints/hook-builtin-packages-v2.sha256
- internal/builtin/testdata/fingerprints/hook-builtin-partition-v1.sha256
- internal/builtin/testdata/fingerprints/hook-builtin-sshkey-v1.sha256
- internal/builtin/testdata/fingerprints/template-builtin-agama-v1.sha256
- internal/builtin/testdata/fingerprints/template-builtin-autoinstall-v1.sha256
- internal/builtin/testdata/fingerprints/template-builtin-autoyast-v1.sha256
- internal/builtin/testdata/fingerprints/template-builtin-esxi-v1.sha256
- internal/builtin/testdata/fingerprints/template-builtin-kickstart-v2.sha256
- internal/builtin/testdata/fingerprints/template-builtin-preseed-v1.sha256
- internal/builtin/testdata/fingerprints/template-builtin-unattend-v1.sha256
- internal/builtin/testdata/native-content.md
- internal/builtin/testdata/native_validate.py
- internal/builtin/testdata/releases/v2.json
- internal/builtin/unattend/account.xml
- internal/builtin/unattend/answer.xml
- internal/builtin/unattend/network.xml
- internal/builtin/unattend/packages.ps1
- internal/builtin/unattend/partition.cmd
- internal/builtin/unattend/sshkey.ps1

## Review fix round1 — Q1

Resolved the sole Important quality finding. The duplicated Preseed/Agama SSH algorithm now lives in `internal/builtin/shared/linux-sshkey.sh`, embedded once by sshkey.go. Separate format-specific Impl records retain their original series, family, timing, interpreter and body bytes. Old/new shared body SHA256 is dc216187b511eddd459a1e296f573c0be9c81d8aa584ab70428a914ff2062571. Removed both duplicated files and updated the tracked source-path note. Existing behavior tests continue to run both formats; no implementation-mirroring test was added.

Exact verification after the behavior-preserving refactor, in Ubuntu Go1.27.2 with the same sourced environment:

`go test ./internal/builtin ./internal/render -count=1`

Output: builtin PASS0.881s; render PASS4.480s. Evidence: /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/q1-focused.log. This covers existing activation, both-format real SSH key/permission assertions, snapshot/Render goldens, fingerprints and PostgreSQL historical replay. `git diff --cached --check` passed. `git diff --cached --exit-code 5075f45dcea0cf217cf736dc44d2202628383553 -- internal/builtin/testdata/answers internal/builtin/testdata/releases internal/builtin/testdata/fingerprints` returned0: no golden/release/fingerprint bytes changed. Original fullcheck evidence is retained; no broader rerun was required or performed.

Debian source-access clarification: original cached recipe bytes are preserved at /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/debian-atomic-bios.source and /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/debian-atomic-uefi.source. The recorded retrieval script /mnt/e/src/lober/.tmp/r9-fetch.sh names the two exact Salsa raw master URLs. Their contents contain GPT biosgrub and native EFI methods respectively. SHA256 and URLs are in /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/debian-source-provenance.txt and tracked native-content.md. The original fetch did not pin an upstream commit; it does not certify exact ISO recipe compatibility. No Debian product change was made.

Amended single Task9 commit: a4cc3b0d0e6982b330cf9d082119f5a14ff701de. Working tree clean, exactly one commit above original base. Commit evidence: /mnt/e/src/lober/.superpowers/sdd/2026-10-09-rewrite-layer3-render/evidence/task9/q1-commit.log. Prior commit5075f45 is superseded. Final file list differs by replacing both format-local SSH scripts with shared/linux-sshkey.sh; all other product scope remains builtin-only.
