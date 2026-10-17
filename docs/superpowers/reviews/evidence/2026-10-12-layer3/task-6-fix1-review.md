- **P1 — Emit a Windows command within the native 1,024-character limit** — ADDRESSED. `internal/render/answer.go:193` emits compact local `-File` invocations; `:200` owns concrete UTF-8-BOM firstboot/cleanup bytes, and `:226` defines their filenames. The runtime contract explicitly assigns their exact-byte pre-reboot OEM staging to Task8. All four changed unattend goldens at line 24 independently measure 304 XML-decoded UTF-16 characters. `internal/render/answer_test.go:430` asserts the decoded native limit for all four cases; `:450` checks an eight-slot single chain. The two new PS goldens pin the actual payloads.

### New Breakage in the Fix Diff

- **Important P1 — Direct cmd invocation uses an invalid forward-slash command path.** `internal/render/answer.go:89` replaces the previous PowerShell `Join-Path`/native invocation with `cmd /d /c %SystemDrive%/ProgramData/lober/script-{Seq}.cmd`. Native cmd does not normalize that executable command path as PowerShell did. A focused native reproduction, `cmd /d /c %SystemDrive%/Windows/System32/cmd.exe /d /c exit 71`, reports `The syntax of the command is incorrect.` and exits 1; the otherwise identical backslash path executes the nested command and exits 71. Consequently the firstboot chain can emit booted/init-start, then fail before executing its slot wrapper, and its success-only cleanup never runs. All four changed goldens line 24 contain this path; `internal/render/answer_test.go:362` now pins the broken spelling rather than exercising native cmd execution. Use a cmd-compatible invocation that also honors the renderer's safe one-line contract, and verify the actual native wrapper invocation and success/failure chain before regenerating fixtures.

### Out-of-Scope Observations

- **Non-blocking inherited Windows pre-path risk:** `internal/render/answer.go:93` already emits `cmd /d /c X:/lober/script-{Seq}.cmd`; the same native cmd path-parsing issue applies there. This line is unchanged by the fix, so it does not extend this review loop. Task8/9 integration should resolve and execute-test the WinPE pre invocation alongside the corrected firstboot invocation.

### Checks

- Reviewed the supplied exact two-tree fix package `5ff3df847d4678cec5f3b0c5d674269be1e1018f` → `129edaf8612d018c7c35f042a64a565b60bda8ed`: 13 changed files, 210 insertions and 137 deletions. Production/test hunks read once; generated evidence read in bounded passes, recovering truncated legacy-only output with normalized unique lines. No unchanged Task6 surface was re-reviewed and no Git commands were run.
- Checked the appended fix report against changed code and supplied red/green/final-green/race logs. The red run fails all four native-limit cases at 3,916 characters. Focused final tests report render 2.384s, catalog 0.390s and archtest 1.122s PASS; race logs report 3.588s/1.270s/2.471s PASS. New tests cover compact XML chain, decoded length, concrete PS byte goldens, BOM/non-BMP/base64 roundtrip and URLs from staged scripts. These tests do not execute the newly emitted cmd wrapper path, so their passing results do not answer the new defect.
- Independently decoded the four changed golden CommandLine values from supplied diff bytes: 304 characters each. Ran only the focused native cmd parsing checks described above, with a backslash control; no Go suite or broad tests were rerun. A proposed temporary batch-file probe was rejected by automatic execution policy before running; no temporary file or checkout source was changed by it.
- Staged firstboot/cleanup retain independent local finalize-token reading, concrete signal phases, nonfatal telemetry failure and success-only cleanup. Removed UTF16 production encoder/import/guard permission corresponds to the new UTF8-BOM representation; no additional fix-introduced problem found in those changes.
- Modified only this review report; source, index, HEAD and branch state remain untouched.

### Verdict

- **Fix round: Findings remain open** — original native-length finding is addressed, but the fix introduces Important P1 native cmd firstboot-path failure at `internal/render/answer.go:89`.
