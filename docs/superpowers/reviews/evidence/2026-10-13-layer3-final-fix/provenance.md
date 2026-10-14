# Final single fix wave evidence

Base: 818fb667f9d746cdce30e7b0cb5cfafb35b522aa. Checkout: `/mnt/e/src/lober/.tmp/wf/r10`. All development/execution: WSL Ubuntu, `source /mnt/e/src/lober/.tmp/l3-env-ubuntu.sh`, native Go1.27.2. No Windows native tests, global tool/service changes or builtin immutable release/fingerprint edits. Controller owns post-commit full make check and one scoped rereview. This archive does not claim those passed.

## RED / GREEN

- `red-i1.log`: `go test ./internal/builtin -run '^TestNoCloudCustomHostnameBuildAndRender$' -count=1 -v` against unchanged production. Literal/renamed author cases activate and Build, render native hostname, then fail aux with param_required. Builtin case shows now-unwanted metadata duplication. An initial test assertion omitted YAML quotes; that test construction error was corrected before this recorded behavioral RED and before implementation.
- `red-runtime.log`: `go test ./internal/render -run 'TestDebianInstallerLifecycleHTTP|TestESXiWatchdogReapsSleepOnQuickCompletion|TestESXiAnswerPasswordFixtureExpanded' -count=1 -v`. Real HTTP pre/post-installer receives zero signals; target/firstboot already pass. Three actual ESXi sleeps survive success/required/optional completion. Password placeholder remains literal. All expected failures exit1.
- `red-i3.log`: `go test ./internal/archtest -run '^TestIncrementalGateRejectsPackageInExistingRoot$' -count=1 -v`. Isolated source copy, existing tools directory, two normal Go passes establish cached arch PASS; adding tools/reviewprobe importing net/http is wrongly accepted by actual make test. Initial RED race was not warmed and already rejected correctly. The committed test additionally warms race twice; GREEN proves both real make paths reject with the expected unclassified-package rule. Other test bodies are filtered through GOFLAGS, avoiding database/full-suite work or recursion in this focused regression.
- `i2-busybox-truncation.log`: first attempted wget selection exposed host standalone-ash builtin selection and NUL truncation (4091 instead of4096 bytes). This is retained as intermediate failure evidence, not GREEN. The controller approved qualified absolute GNU wget path selection.
- `green-runtime.log`: `LOBER_DEBIAN_WGET=/mnt/e/src/lober/.tmp/l3-wget-source/wget-udeb/usr/bin/wget go test ./internal/render ./internal/builtin -run 'TestDebianInstallerLifecycleHTTP|TestESXiWatchdog|TestESXiAnswerPasswordFixtureExpanded|TestNoCloudCustomHostnameBuildAndRender|TestESXiProcessGroupTimeout|TestRuntimeWatchdogCancellation' -count=1 -v`. Actual downloaded udeb client, BusyBox shell/applets, no curl in installer PATH, real receiver and frozen hook. Empty body, binary4096 tail, method/path/content-type/query and four-event ordering verified. Only fixed client/storage paths relocate in test. Target cases use real curl; firstboot uses independent credential without hook download.
- `green-i3.log`: `go test ./internal/archtest -run '^TestIncrementalGateRejectsPackageInExistingRoot$' -count=1 -v`. Expected nested gate failures make the regression PASS. Both ordinary/race caches warmed first.
- `green-m2.log`: focused watchdog startup/cancellation/actual timeout tests, initial successful focused pass. Expanded covering package pass also verifies reaped child is absent (not merely zombie) and required/optional sequential-hook policy for both quick failure and real timeout.
- `covering-before-goldens.log`: stale golden and old fake-transport harness failures found by full covering-package check, retained honestly. Fixed only path relocation in the old failure-policy harness and the four directly affected rendered goldens.
- `covering-green.log`: `LOBER_DEBIAN_WGET=/mnt/e/src/lober/.tmp/l3-wget-source/wget-udeb/usr/bin/wget go test ./internal/render ./internal/builtin ./internal/archtest -count=1`. Complete covering packages, not entire repository. Controller performs full make check after commit.

## Debian primary source/tool qualification

Downloaded official Debian trixie source/package files:

- https://deb.debian.org/debian/pool/main/w/wget/wget_1.25.0-2.debian.tar.xz
- https://deb.debian.org/debian/pool/main/w/wget/wget_1.25.0.orig.tar.gz
- https://deb.debian.org/debian/pool/main/w/wget/wget-udeb_1.25.0-2_amd64.udeb

`wget-source-qualification.log` records SHA256 of all three downloaded archives and extracted executable, GNU version, actual dpkg-deb package path `/usr/bin/wget`, and line-numbered original source excerpts. `wget-debian-rules.txt` and `wget-debian-control.txt` preserve exact packaging source bytes. Rules build udeb from the same source with OpenSSL, IPv6, no libpsl/debug/NLS/IRI; HTTP POST/header flags remain available. src/main.c declares --post-file/--header/-T/-O and translates post-file to method POST and body-file; src/http.c sizes/opens the body and transmits file bytes. The actual udeb execution proves the exact empty and NUL-containing body behavior; source reading alone is not treated as runtime proof. Fetch initially timed out on upstream.tar.gz and resumed successfully; only final complete archive hashes are recorded.

Additional primary references: [Debian package](https://packages.debian.org/trixie/wget-udeb), [GNU binary POST documentation](https://www.gnu.org/software/wget/manual/html_node/HTTP-Options.html), [NoCloud required metadata](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html#meta-data). Native actual ISO/installer ABI remains layer4/7 qualification. Standalone Ubuntu ash's builtin wget is not the qualified Debian POST client.

## Intentional fixture changes and archive boundaries

- meta-data.golden: only task-derived instance-id; builtin hostname remains in native autoinstall identity.hostname.
- esxi-answer.golden: valid `root_password` word parameter with full SHA-512-crypt fixture (`openssl passwd -6 -salt loberfixture fixture-only-password`) and corrected watchdog embedded bytes.
- builtin esxi-ks-{bios,uefi}.golden: only renderer-owned embedded watchdog bytes. Raw builtin author models, immutable historical/current release archives, fingerprints and prior evidence remain untouched.

The source copy in architecture regression contains only git-tracked module/internal files and Makefile, with stable file mtimes; source discovery itself uses Go's eligible tree unchanged. No blanket docs/tools/examples exclusion was added. Direct cached `go test` is not the promised whole-module discovery gate; use make archcheck/test/race/check.

Final self-review tightened the sleeper test to require complete process absence and fail if ps cannot run (exit1 is its normal absent-PID result), and grouped test imports. `self-review-green.log`: `go test ./internal/render ./internal/builtin -run 'TestESXiWatchdog|TestNoCloudCustomHostnameBuildAndRender' -count=1 -v` PASS after those test-only edits. `self-review.log` records diff whitespace, changed Go formatting and unchanged old archive checks. No subsequent production changes followed covering-green.

Raw command logs intentionally retain original trailing whitespace from wget version output and existing runtime status logging. A staged whole-diff whitespace check reported those raw-output bytes; production/tests/docs outside the raw evidence archive pass diff --check. No output was normalized or silently edited.
