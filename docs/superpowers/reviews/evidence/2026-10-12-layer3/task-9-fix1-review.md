**Q1 — duplicated exact 16-line Linux SSH algorithm** — **ADDRESSED**. internal/builtin/sshkey.go:10 embeds shared/linux-sshkey.sh once; internal/builtin/sshkey.go:22 calls linuxSSHKey separately for Preseed and Agama. The supplied fix diff deletes preseed/sshkey.sh and records a 100% identical rename from agama/sshkey.sh to shared/linux-sshkey.sh, preserving the original executable bytes. The new helper at sshkey.go:13-15 computes series/family per format and retains ImplScript, interpreter sh and firstboot timing, equivalent to the old script helper. The two distinct Impl records remain in their original order.

### New Breakage in the Fix Diff

- **None.** No new Critical, Important or Minor finding established in the supplied old/new diff. The separate string embed is initialized before package hook construction; it does not require changing the existing format-directory embed.FS. The documentation change at internal/builtin/testdata/native-content.md:31 explicitly states that Debian source retrieval was unpinned master and does not establish acceptance ISO compatibility; :43-45 retain layer4/7 qualification obligations.

### Out-of-Scope Observations

- **None.** Native installation and exact-media qualification remain the existing later-layer obligations, not new findings from this fix.

### Checks

- **Check:** Read the supplied amended-commit diff once for old 5075f45dcea0cf217cf736dc44d2202628383553 and new a4cc3b0d0e6982b330cf9d082119f5a14ff701de. Used that explicit old/new diff because the amended commit is not a descendant. Did not derive another git diff or mutate the checkout.
- **Check:** Compared the new helper with the existing fragment/script construction in internal/builtin/builtin.go:43-54 to verify metadata equivalence and embedding initialization compatibility.
- **Check:** Current shared/linux-sshkey.sh SHA256 is dc216187b511eddd459a1e296f573c0be9c81d8aa584ab70428a914ff2062571, matching the fix report; the supplied 100% rename supplies old-body identity evidence.
- **Check:** internal/builtin/native_behavior_test.go:156 retains TestLinuxSSHHookConsumesEnvironmentAndWritesProtectedKey and iterates both Preseed and Agama. It selects each actual Impl, executes its Body with only NSS replaced, and checks exact authorized_keys bytes plus 0700/0600 permissions. No behavior assertions were removed by the fix.
- **Check:** The appended fix report identifies go test ./internal/builtin ./internal/render -count=1 and its covering activation, SSH behavior, snapshot/render golden, fingerprint and PostgreSQL replay tests. evidence/task9/q1-focused.log contains successful package results: builtin 0.881s and render 4.480s. Existing TestShippedContentActivates, TestAllBuiltinAnswersFromSnapshots, TestFingerprintsPinEveryCurrentAndHistoricalVersion and TestReleasedVersionsLoadAndUpgradeIdempotently remain present. These recorded results were inspected, not rerun; the aggregate log is not an independent per-test execution trace.
- **Check:** No answer golden, release or fingerprint file appears in the fix diff, agreeing with the report of unchanged bytes. Read-only inspection raised no specific unresolved doubt requiring another test run. All review commands used WSL and sourced the specified Ubuntu environment; no Windows-native test or global change was made.

### Verdict

**Fix round:** **All findings addressed, no new Critical/Important breakage.** Q1 is closed.
