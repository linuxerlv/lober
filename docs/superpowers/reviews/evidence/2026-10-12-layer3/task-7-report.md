# Task 7 implementation report

Isolated clone: /mnt/e/src/lober/.tmp/wf/r7. Base: b5ca3ce09c13576276fa2aca4d403ed754713680. All development/checks used WSL Ubuntu and /mnt/e/src/lober/.tmp/l3-env-ubuntu.sh (official isolated Go1.27.1, mandatory Ubuntu busybox/bash, existing module proxy and local PostgreSQL18). No root-checkout edits, subagents, global configuration changes or Windows-native verification. Private database credentials were never printed.

## Implemented interfaces

- Request.Firmware catalog.Firmware is actual boot firmware, mandatory valid bios/uefi for FileMenu. No registration-metadata or BIOS fallback. Series/format/family and catalog Series.Firmwares are checked before assembly; ESXi BIOS returns Problems and no output.
- Request.ESXiBootConfig string is original immutable ISO boot.cfg supplied by caller for ESXi FileAux. Render performs no artifact reads. Original kernel/module order, nested full paths, options, unknown fields, comments and newline style are preserved during necessary HTTP rebasing.
- Existing Render dispatch implements kickstart/preseed/autoinstall/ESXi menus and Ubuntu metadata/ESXi bootcfg. Other format assemblers/runtime remain Task8 work.
- Menus begin shebang and end boot. RHEL has distinct full installation stage2 tree and repository URLs, noninteractive inst.cmdline, inst.notmux, consoles and inst.syslog. Ubuntu adds original complete ISO url, DHCP and NoCloud trailing-slash directory. Debian retains canonical preseed URL and adds serial output. ESXi chains actual UEFI media loader with canonical boot.cfg.
- Canonical machine URLs/filenames use catalog helpers. Repo/iso remain exclusively the existing reserved derived system values; no alternate provenance added.
- Missing values/roles and malformed ESXi source return Problems and nil bytes. Invalid original content includes blank, malformed lines, duplicate known fields, absent kernel/modules, traversal/empty paths and absolute URLs outside the immutable published tree. Missing optional prefix/kernelopt are appended. Absolute HTTP/HTTPS references already beneath the same publishing tree remain unchanged.
- Added reason codes at codeTable/extraCodes tails: render_firmware_invalid (Invalid), render_boot_snapshot_invalid (Incompatible), render_esxi_bootcfg_invalid (Invalid). Registry total80 includes all new codes.

## RED/GREEN and verification evidence

Commands ran in Ubuntu with the environment sourced. Evidence paths are relative to this SDD report directory.

1. RED: go test ./internal/render/ -count=1, exit1. task-7-red.log records golden, menu arguments, required-input and ESXi-original tests failing on real Render's assembler-not-implemented sentinel. Hand-checked expected fixtures were written before production assemblers. Agreed interface fields were defined before RED to compile the contract. No golden update mode generated expected output.
2. GREEN: same command exit0; task-7-green.log reports internal/render PASS after assembly implementation and focused iterations.
3. Optional-fields/absolute-path iteration: go test ./internal/render -run 'TestESXiOptional|TestESXiAbsolute' -count=1. task-7-iteration-red.log captures rejected valid optional media fields and wrongly accepted doubled leading slash. task-7-iteration-green.log PASS after preserving optional/newline behavior and removing only one slash.
4. HTTP iteration: go test ./internal/render -run 'TestESXiHTTP|TestESXiRebase' -count=1. task-7-rebase-red.log captures cdromBoot wrongly retained and original spacing lost; task-7-rebase-green.log PASS after exact-token removal and spacing preservation.
5. First full make check exit1: task-7-make-check-failed.log retains TestCode表与附录总表逐项一致 failure because new reasons were absent from extraCodes. All normal-suite packages ran; only core/model failed. PostgreSQL store tests passed. This check stopped before race/Linux build.
6. Updating extraCodes exposed its literal fixed-total77: task-7-focused-registry-red.log retains that failure. Changed the expected total to80 and accounting comment.
7. Final focused go test ./internal/core/model ./internal/render -count=1 exit0: task-7-focused-final.log reports both PASS.
8. Final full make check result appended after actual completion. Reproducer .tmp/task-7-check.sh sources Ubuntu environment and runs make check; task-7-make-check.log contains final complete output.

Before the real RED, test-authoring compile errors were corrected: catalog.DeliveryFileName returns (string,bool), and model.Code needs string conversion for a string comparison. Actual RED was recorded only after these compile errors were resolved; tool transcript retains those earlier compiler outputs.

## Behavioral coverage

Seven menu goldens: Rocky/Debian/Ubuntu actual BIOS and UEFI plus ESXi UEFI. Linux firmware bytes intentionally match: menu kernel/initrd loading does not choose partitions, yet actual firmware remains mandatory. Negative tests reject missing/invalid firmware and ESXi BIOS, even when registered Host.Firmware says otherwise. Valid UEFI ESXi succeeds when registration says BIOS.

Consumer-visible command lines pin 99b6ca7/4e32ed9 (shebang/execute), 75bdc20/7eb805e (extracted stage2 versus package repo), 7d38246 (serial console), 23442b1 (RHEL installer-startup syslog), 2ab744d (no tmux) and related9a252db (noninteractive mode). Only RHEL gets its documented inst.syslog boot parameter; no invented other-installer parameter.

Aux coverage pins sibling user-data/meta-data route names and trailing-slash seed, task instance-id/hostname, no unnecessary metadata download credentials, ks.cfg instead of boot.cfg, source immutability, nested ordered modules/options, unknown lines/comments/CRLF/tabs/spaces, optional fields, malformed originals, required inputs and compatible immutable absolute URLs. Menu URL escaping/trailing-base-slash and reserved iso rejection are covered.

686afcd concerns hook-runtime timeout availability, a59a0bb execution-slot telemetry and a570ad5 early-command shell assembly. These are explicitly Task6/8 runtime responsibilities, not falsely reported as Task7 menu tests. No Task8 bodies/scripts implemented.

## Complete legacy line audit

task-7-legacy-diff.md contains complete unified diffs and an explicit rationale for every old/new changed line; audit generation refused any change without a hand-written reason. Old URL identifiers remain. Meta-data is byte-identical. Linux UEFI golden bytes deliberately match BIOS. ESXi menu has no legacy golden and follows inherited-spec section4.5. Original-media-only root-slash/cdromBoot/optional-field changes have separate documented rationale and behavioral RED/GREEN evidence. No unexamined golden drift remains.

## Layer4/caller handoff

Committed internal/render/testdata/menu-aux-contract.md defines roles with relative path examples: kernel, initrd, stage2, repo, iso, mboot, esxi-tree. Stage2 is full extracted RHEL installation tree; repo independent package repository; iso original full Ubuntu ISO; esxi-tree entire immutable original extraction. Importer owns path validation, real completeness and case-sensitive layout. Module lists are never fabricated from Artifacts.

Layer4 preserves original immutable boot.cfg. Layer5/7 obtains its bytes outside render and supplies Request.ESXiBootConfig. Layer7 supplies mandatory actual Firmware, performs actual architecture and real-file checks, and handles refusal state/token/attempt policy. It must serve user-data/meta-data in the exact NoCloud task/token seed directory. Ubuntu templates declare the required hostname parameter. Caller bases/task/token follow established configured-address/safe-ID contracts. Repo/iso maintain one derived source rather than alternate provenance.

## Self-review and limitations

Reviewed staged production/test/fixture/contract diff, control flow, nil-output-on-Problems, ordered role/value scans, original-config ordering, and reason registry. git diff --cached --check passed. task-7-self-review.diff contains the reviewed complete changes. New production rendering has no filesystem, database, clock or network IO; url package only parses immutable input. Map iteration does not affect output.

Real hardware integration remains layer4/7 work: ESXi loader/version/network/module availability, DHCP/iPXE transport, Ubuntu ISO memory requirements, correct stage2/repo packaging and actual install validation. Unit/golden/Linux verification does not certify hardware installation. Task8 retains other menus/body/runtime/Windows target staging. No unresolved architectural decision remains within Task7.

Sources: [RHEL8 boot options](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/automatically_installing_rhel/custom-boot-options_rhel-installer), [Canonical netboot tests](https://github.com/canonical/subiquity/blob/main/NETBOOT_TESTS.md), [Canonical NoCloud](https://cloudinit.readthedocs.io/en/latest/reference/datasources/nocloud.html), [VMware ESXi Installation and Setup vSphere8](https://d3bql97l1ytoxn.cloudfront.net/app_resources/447311/documentation/1439997_1721785831666_en-US.pdf), especially p83-84 config syntax and p105-106 iPXE HTTP procedure. Root confirmed artifact roles, necessary leading-slash rebasing, compatible absolute URL handling and exact cdromBoot removal.

## Final complete verification

Final make check exited0, confirmed by session completion and complete task-7-make-check.log. It ran gofmt, go vet ./..., go test ./..., go test -race ./..., and GOOS=linux GOARCH=amd64 go build ./.... All13 packages passed normal and race runs, including PostgreSQL store tests. The original full failure and focused fixed-total failure remain preserved separately. No code changes followed this final complete verification; only report completion and commit.

## All changed files

- internal/core/model/codes.go
- internal/core/model/problem_test.go
- internal/render/aux.go
- internal/render/aux_test.go
- internal/render/expand_test.go
- internal/render/menu.go
- internal/render/menu_test.go
- internal/render/render.go
- internal/render/testdata/esxi-bootcfg.golden
- internal/render/testdata/menu-aux-contract.md
- internal/render/testdata/menu-debian-bios.golden
- internal/render/testdata/menu-debian-uefi.golden
- internal/render/testdata/menu-esxi-uefi.golden
- internal/render/testdata/menu-rocky-bios.golden
- internal/render/testdata/menu-rocky-uefi.golden
- internal/render/testdata/menu-ubuntu-bios.golden
- internal/render/testdata/menu-ubuntu-uefi.golden
- internal/render/testdata/meta-data.golden

## Commit and final state

Commit: c284faf5222b8c4b4dd1a1a370ba8033a5fea1d8

Subject: feat(render): 三种菜单与 ESXi、Ubuntu 的附属文件

Parent: b5ca3ce09c13576276fa2aca4d403ed754713680. git status --short after commit was empty. One task commit, 18 files, 804 insertions/5 deletions.
