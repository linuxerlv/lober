### Spec Compliance

- ❌ Issues found: Windows answer output violates the native FirstLogonCommands CommandLine limit. All four unattend goldens contain a 3,916-character command where Windows permits at most 1,024; `internal/render/testdata/unattend.tmpl:24`, `internal/render/testdata/unattend-full-bios.golden:24`, and `internal/render/answer.go:213`. This is an invalid Task6 answer/scaffold, not a deferred FileScript implementation obligation.
- ✅ Remaining scoped requirements verified: five-format dispatch and shared expansion (`internal/render/answer.go:20`, `:29`); full/minimal × BIOS/UEFI matrix and deterministic bytes (`internal/render/answer_test.go:59`); required parts and safe one-line replacements (`:164`); decoded answer/artifact/signal URL parsing (`:404`); all four named Kickstart regressions including real include contents (`:311`, `internal/render/testdata/kickstart.tmpl:17`); line-by-line legacy explanations (`internal/render/testdata/legacy-line-diff.md:3`, `internal/render/testdata/legacy-differences.md:5`). The approved private finalize-token and pure UTF16 whitelist changes remain within their authorized scope.
- ⚠️ Cannot verify from this diff: Task8 FileScript interpreter/digest/timeout/required/optional/EnvName/reporting bodies and Windows FileAux pre-reboot OEM staging. Task6 explicitly leaves those dispatchers unimplemented (`internal/render/render.go:88`); its concrete caller-side paths are `internal/render/answer.go:69`, `:77`, `:88`. Controller must preserve the runtime contract when implementing them.
- ⚠️ Cannot verify from this diff: Task9 actual builtin profiles/defaults, target curl/systemd prerequisites, Windows login/AutoLogon prerequisites and native placement. These handwritten fixtures are independent renderer inputs (`internal/render/answer_test.go:25`, `internal/render/testdata/legacy-differences.md:3`, `:13`); they do not establish real-machine installation success. Layer7 must verify actual OEM copying, login, supported installers and independently issued finalize-token permissions/TTL.

### Strengths

- Linux firstboot replacement stages wrappers and digest hook payloads before reboot without executing them; required fetches propagate failure, and post-target execution explicitly chroots into the format-specific target (`internal/render/answer.go:88`, `:113`, `:229`). Target roots are `/mnt/sysroot`, `/target`, and `/mnt`, with matching outside-chroot fixture placement (`internal/render/testdata/kickstart.tmpl:22`, `preseed.tmpl:18`, `autoinstall.tmpl:24`, `autoyast.tmpl:18`).
- Native systemd runner executes frozen firstboot slots in snapshot order and invokes cleanup only after success; telemetry failure is nonfatal and no boot-token download appears in the post-reboot runner (`internal/render/answer.go:150`, `:165`, `:168`, `:249`). The real-shell test verifies staging and preservation of the credential after a failing wrapper (`internal/render/answer_test.go:221`).
- Windows uses one source chain, resolves SystemDrive, keeps WinPE pre execution separate, and independently reads the staged finalize credential rather than assuming shared subprocess environment (`internal/render/answer.go:88`, `:190`, `internal/render/testdata/unattend.tmpl:10`, `:24`). UTF16LE encoding supports non-BMP characters (`internal/render/answer.go:275`, `internal/render/answer_test.go:277`).
- Payloads are concrete and inspectable rather than fictitious helper binaries. Independent extraction from the review diff found 108 encoded commands across 20 goldens; all 108 decoded byte strings exactly matched their corresponding `decoded-scaffolds.md` blocks. All 448 inserted lines in `legacy-line-diff.md` exactly matched the stated golden file/line; 3,402 deleted lines plus 448 inserted lines equals the reported 3,850 changes.

### Issues

#### Critical (Must Fix)

- None found.

#### Important (Should Fix)

- **P1 — Emit a Windows command within the native 1,024-character limit.** `internal/render/answer.go:213` returns whole encoded PowerShell firstboot/cleanup programs, which `internal/render/testdata/unattend.tmpl:24` concatenates with the slot command. The resulting XML-decoded CommandLine is **3,916 characters in each of the four unattend goldens**, exceeding the native field's **1,024-character maximum** documented by [Microsoft CommandLine](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-shell-setup-firstlogoncommands-synchronouscommand-commandline). This prevents treating the rendered answer as a valid Windows setup configuration, even with a correct downstream wrapper. The XML tokenizer and PowerShell parser checks validate different properties and miss this native constraint (`internal/render/answer_test.go:340`). Preserve the approved single sequential source chain, but stage real firstboot/cleanup payload files before reboot and emit compact local invocations; coordinate their concrete filenames and bytes with Task8. Add a focused assertion on the XML-decoded CommandLine length and regenerate goldens/decoded/legacy evidence. Do not split it into concurrently executed FirstLogonCommands.

#### Minor (Nice to Have)

- None raised.

### Assessment

**Task quality:** Needs fixes.

**Reasoning:** The Linux assembly and lifecycle scaffolds are concrete, pure and supported by meaningful decoding and shell execution tests. Windows output has a directly measured native schema violation, so Task6 cannot pass its gate until the command representation is corrected.

### Checks and Review Boundaries

- Reviewed base `09de4da` to head `5ff3df847d4678cec5f3b0c5d674269be1e1018f` through the supplied 48-file review package. Production and test hunks were read in full once. Generated fixtures/docs were reviewed in bounded passes, with exact repeated decoded blocks deduplicated; truncated legacy-only tool output was recovered through bounded normalized unique-line passes. No production hunk was lost to truncation, no changed source file was reread, and no Git command was run.
- Named risk checked externally: base64/UTF16 expansion can exceed the native Windows CommandLine field size. A read-only extraction measured the XML-decoded field in all four committed goldens (3,916 each), and Microsoft's primary documentation confirms the 1,024 limit. No broader source crawl or external check was performed.
- Read the supplied `task-6-green.log` and `task-6-make-check-green.log`: focused tests, gofmt, vet, ordinary tests, race tests and Linux build show passing output without warnings. Did not rerun any suite or test. The additional checks only parsed supplied diff bytes and compared evidence; they did not execute product code or alter the checkout.
- Modified only this report. Source, index, HEAD and branch state remained read-only.
