# Task 7 review

## Spec Compliance

- ✅ Spec compliant. All four requested implementation/test files are present. Menus use explicit actual firmware, reject invalid firmware and incompatible series/format/family, and restrict ESXi to UEFI (internal/render/menu.go:16, internal/render/menu.go:20, internal/render/menu.go:23, internal/render/menu_test.go:142). No Host.Firmware fallback is introduced.
- ✅ Legacy menu and auxiliary behavior is preserved with justified fixes: executable shebang/boot; independent RHEL stage2/repo; noninteractive mode, notmux, serial/syslog; Ubuntu original ISO URL and trailing-slash NoCloud directory; Debian preseed; ESXi canonical loader/config and ks.cfg (internal/render/menu.go:63, internal/render/menu.go:70, internal/render/menu_test.go:58, internal/render/aux_test.go:36).
- ✅ Immutable original ESXi input drives configuration. Kernel/module paths, arguments and order survive rebasing; unknown lines/comments and CRLF remain; optional prefix/kernelopt are appended; exact cdromBoot is removed; compatible absolute HTTP references are retained and incompatible ones produce Problems (internal/render/aux.go:38, internal/render/aux.go:93, internal/render/aux.go:135, internal/render/aux.go:173; internal/render/aux_test.go:69, internal/render/aux_test.go:115, internal/render/aux_test.go:126, internal/render/aux_test.go:163, internal/render/aux_test.go:174).
- ✅ All requested BIOS/UEFI combinations have goldens, with explicit ESXi BIOS rejection. Linux menu bytes legitimately match because partition selection does not occur here (internal/render/menu_test.go:29, internal/render/menu_test.go:149, internal/render/testdata/menu-aux-contract.md:5).
- ✅ Artifact/caller handoff is documented, including original ISO and distinct installation/package trees, and Task 8 responsibilities remain scoped out (internal/render/testdata/menu-aux-contract.md:13, internal/render/testdata/menu-aux-contract.md:29, internal/render/testdata/menu-aux-contract.md:37).
- ⚠️ Cannot verify from this diff: layer 4 path validity, complete immutable extraction and original-config sourcing; layer 5/7 request preparation and serving user-data/meta-data siblings; architecture/loader compatibility and actual installation. Controller should retain these obligations in later-layer acceptance (internal/render/testdata/menu-aux-contract.md:9, internal/render/testdata/menu-aux-contract.md:25, internal/render/testdata/menu-aux-contract.md:33).
- ⚠️ Runtime timeout, hook telemetry and early-command assembly regressions (686afcd/a59a0bb/a570ad5), other menu formats and execution scripts remain Task 6/8 gates (internal/render/testdata/menu-aux-contract.md:37).

## Strengths

- Input failures consistently return nil bytes with registered Problems, while unsupported pending assemblers remain explicit. The three added reason codes are mirrored in the reason registry and fixed total (internal/render/menu.go:16, internal/render/menu.go:40, internal/render/menu.go:58; internal/render/aux.go:39; internal/core/model/codes.go:424; internal/core/model/problem_test.go:70, internal/core/model/problem_test.go:102).
- The production change has focused responsibilities and only pure standard-library processing plus core dependencies. Ordered scans make output deterministic; maps are lookup-only. Assembly shares canonical catalog URL/name helpers and existing reserved system-value provenance (internal/render/menu.go:47, internal/render/menu.go:63, internal/render/menu.go:91, internal/render/menu.go:105; internal/render/aux.go:59, internal/render/aux.go:64).
- Tests exercise real Render output, repeat-call determinism, missing input/no-output behavior, actual-versus-registered firmware, source immutability and ESXi preservation edge cases (internal/render/menu_test.go:29, internal/render/menu_test.go:93, internal/render/menu_test.go:142, internal/render/menu_test.go:164; internal/render/aux_test.go:15, internal/render/aux_test.go:69, internal/render/aux_test.go:91).

## Issues

### Critical (Must Fix)

None.

### Important (Should Fix)

None.

### Minor (Nice to Have)

None.

## Checks and review bounds

- Reviewed the supplied b5ca3ce..c284faf package once. Tool output cut off the middle of aux_test.go through the opening menu tests; recovered only that cutoff range from the package. No changed source file was independently reread, and no Git state/command was used.
- Named outside-diff risk: new menu assembly might bypass reserved repo/iso derivation or report missing values differently. One focused check of existing expansion.systemValue confirmed canonical artifact derivation and missing-value Problems (internal/render/expand.go:178). No alternate provenance or map-order behavior was introduced.
- Named outside-diff risk: the claimed legacy audit could hide fixture drift. One focused read of the five original legacy-golden files confirmed the old text against the complete hand-explained audit. Every new delta is represented, and metadata is byte-identical (task-7-legacy-diff.md:5; legacy-golden/menu_rocky.golden:1; legacy-golden/menu_debian.golden:1; legacy-golden/menu_ubuntu.golden:1; legacy-golden/esxi_bootcfg.golden:1; legacy-golden/meta_data.golden:1).
- Inspected existing initial RED, optional/absolute-path RED/GREEN, HTTP-rebase RED/GREEN, registry failure/fix and complete make-check evidence. Initial RED exercises the real unimplemented dispatch; final evidence contains clean formatting/vet, all 13 packages, race, and Linux build. No suite, race detector or build was rerun (task-7-red.log:1; task-7-green.log:1; task-7-iteration-red.log:1; task-7-iteration-green.log:1; task-7-rebase-red.log:1; task-7-rebase-green.log:1; task-7-make-check-failed.log:1; task-7-focused-registry-red.log:1; task-7-focused-final.log:1; task-7-make-check.log:1).
- All checks used WSL Ubuntu. Startup localhost proxy warning was host bridge noise outside the saved test evidence; no global WSL configuration was changed. Only this review report was written.

## Assessment

**Task quality:** Approved

**Reasoning:** The Task 7 implementation satisfies its settled interfaces and requested outputs, with substantive regression and original-media preservation tests plus complete saved verification. Remaining integration obligations belong to the explicitly identified later layers and do not block this task gate.

