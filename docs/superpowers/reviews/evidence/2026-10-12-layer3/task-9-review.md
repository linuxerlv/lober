### Spec Compliance

- ✅ Spec compliant for the Task 9 scope at 5075f45dcea0cf217cf736dc44d2202628383553. All six requested format directories and all five hook declaration files are present; internal/builtin/builtin.go:15 and internal/builtin/builtin.go:68 assemble seven templates with all five hooks. internal/builtin/content_test.go:9 checks the complete manifest. There is no schema/model or Task 8 production change.
- ✅ Activation and snapshot/render coverage: unchanged internal/builtin/builtin_test.go:15 iterates shipped objects through activation.Hook and activation.Template; internal/builtin/render_test.go:25 builds real snapshots and the answer matrix exercises the same Render entry, deterministic output, execution wrappers, and slot/assembly agreement. Fourteen answer fixtures and twelve current object fingerprints are added.
- ✅ Native firmware is selected by installer probes/proposals, not a registered-host fallback: internal/builtin/kickstart/partition.cfg:6, preseed/partition.cfg:1, autoinstall/partition.yaml:1, autoyast/partition.xml:1, agama/partition.jsonnet:1, and unattend/partition.cmd:3. Windows explicitly agrees on disk0/OS partition3 (unattend/answer.xml:16). The shell branch tests execute Kickstart output; the proposal and separate DiskPart assertions establish configuration, not native installer execution. ESXi BIOS refusal is exercised by the delivery tests.
- ✅ Credentials have no public defaults (account.go:7, sshkey.go:7); Agama products and Windows editions are media offers without static defaults (builtin.go:104). AutoYaST keys are nested under the created user (autoyast/answer.xml:11). Ubuntu memory requirements are body comments (autoinstall/answer.yaml:2), as ruled.
- ✅ Historical v1 files are absent from the change list; self-review.log records their expected raw hashes. Changed kickstart/packages identities advance to version2. The full current release and hashes are archived, and fingerprint_test.go:163 / :176 add preservation of historical revisions to real PostgreSQL upgrade replay.

### Strengths

- internal/builtin/native_behavior_test.go:1 contains substantive behavior checks: actual emitted Kickstart BIOS/UEFI partition programs run under bash and BusyBox, SSH scripts perform real file/permission changes with only NSS replaced, and Debian's emitted early command is executed with a debconf shim. These strengthen the golden evidence.
- internal/builtin/agama/answer.jsonnet:15 and preseed/answer.cfg:15 stage firstboot in installer target roots; Agama has one systemd path. unattend/answer.xml:34 preserves one bounded sequential FirstLogonCommand and account.xml:7 supplies AutoLogon.
- internal/builtin/testdata/native-content.md:20 separates native source/configuration evidence from exact-media qualification and actual installation. It honestly preserves Windows2016/2019 helper limitations, Server2025-only SSH guarantee, and ESXi Secure Boot/applets/persistence boundaries.
- internal/builtin/testdata/native_validate.py:12 pins four upstream schema hashes and resolves them locally; the saved native-final.log confirms Agama Jsonnet/schema and Ubuntu YAML checks.
- evidence/task9/fullcheck.log:1 records all five Ubuntu checks and EXIT_STATUS=0; pg-replay.log:1 records a real non-skipped PostgreSQL pass. Neither output has test warnings. red.log:1 records the intended initial missing-manifest failure. No suite, race run, high-count loop, or native Windows command was rerun for this review.

### Issues

#### Critical (Must Fix)

- None.

#### Important (Should Fix)

- Q1 — internal/builtin/agama/sshkey.sh:1 duplicates all 16 executable lines of internal/builtin/preseed/sshkey.sh:1 verbatim, including NSS resolution, path validation, ownership, permissions, and SELinux handling. These are the same algorithm and already produce the same digest; keeping independent copies makes a future correctness/security fix easy to apply to only one installer. Store one shared embedded Linux SSH body and reference it from the two format-specific Impl records in sshkey.go:9. Preserve the exact body bytes, distinct series/timing metadata, and both-format behavior assertions. This is Important under the supplied task-reviewer rubric's explicit treatment of verbatim duplicated logic; it is not a demand to unify different native installer syntax.

#### Minor (Nice to Have)

- None.

### Cannot verify from this diff

- ⚠️ Exact installer/media qualification and real BIOS/UEFI installation remain layers4/7. Unit tests do not certify AutoYaST/Subiquity/Agama/partman execution, package availability, firewall/service behavior, native Windows Setup/ACL/AutoLogon, Windows2016/2019 helper support, or ESXi8/9 applets and owned-$0 persistence. internal/builtin/testdata/native-content.md:37 retains these obligations.
- ⚠️ The source-based proposal distinction is sound for the sources inspected: [Subiquity storage](https://canonical-subiquity.readthedocs-hosted.com/en/latest/howto/configure-storage.html) documents firmware-specific automatic boot partitions; [AutoYaST expert partitioning](https://documentation.suse.com/sles/15-SP7/html/SLES-all/cha-configuration-installation-options.html#CreateProfile.Partitioning) documents adding missing boot partitions; [Agama storage](https://agama-project.github.io/docs/user/reference/profile/storage#partitions-needed-for-booting) documents boot.configure and device aliases. The Debian Salsa recipe bodies were inaccessible to this review's web reader; native-content.md:24 cites the relevant upstream repository, but independent inspection of those two recipe bodies is unverified here. This is a source-access limitation, not evidence that the recipes are absent.
- ⚠️ Inherited renderer semantics are covered by the prior gates and tracked runtime contracts, not re-audited wholesale here. The two already deferred Task8 minors remain outside this diff.

### Review bounds and checks

- Read the supplied package once in bounded sections, without deriving another git diff or reopening changed production files. Long generated base64 golden lines and the one-line release JSON were display-bounded; their exact identity is supported by existing fullcheck/fingerprint evidence rather than a new manual byte audit.
- Focused outside-diff checks: runtime/menu contracts for staging/input compatibility; unchanged activation calls for manifest-wide activation coverage; the unchanged first half of fingerprint_test.go for fixture coverage and real replay semantics. The changed revision assertions were already visible in the package.
- Source inspection was limited to the named native-proposal risk; no broad code crawl or native execution was performed. Existing verification evidence was read in place.

### Assessment

**Task quality: Needs fixes.**

**Reasoning:** The functional/spec coverage is strong, with meaningful shell behavior checks and honest native validation boundaries. Resolve the one duplicated executable body before approving the task quality gate; no other Critical or Important finding was established.
