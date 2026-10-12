### Spec Compliance

- ✅ Spec compliant for Task 4: the requested Request/Render interface and four dispatch branches are present; concrete assemblers remain explicit errNotImplemented stubs as required (internal/render/render.go:14, internal/render/render.go:34, internal/render/render.go:85).
- ✅ The common engine uses scan Segments and the new scan-owned line view, implements system/parameter substitution, standalone indented fragment insertion, safe inline execution-slot/system-part replacements, line repetition and join/subvalue behavior, and preserves raw malformed/Jinja text (internal/render/expand.go:23, internal/render/expand.go:53, internal/render/expand.go:144; internal/core/scan/lines.go:20; internal/render/expand_test.go:31, internal/render/expand_test.go:41, internal/render/expand_test.go:62, internal/render/expand_test.go:83).
- ✅ Address ownership follows the root-approved single-source decision: repo/iso caller overrides are rejected and addresses derive exclusively from artifacts plus ISO.Artifacts[repo/iso]; lbrace remains literal (internal/render/render.go:17, internal/render/render.go:90; internal/render/expand.go:178).
- ✅ Content Problems discard all partial bytes; unknown Kind returns error; FileScript checks an existing execution slot (internal/render/expand.go:23, internal/render/expand.go:46; internal/render/render.go:34; internal/render/expand_test.go:103, internal/render/expand_test.go:131).
- ✅ Fragment expansion has an explicit leaf boundary and rejects structural slots/parts before traversal, including the self-reference case (internal/render/expand.go:28, internal/render/expand.go:151; internal/render/expand_test.go:269, internal/render/expand_test.go:296).
- ✅ The authorized PostgreSQL compatibility amendment only translates SQLSTATE 23001 for deletion of a recognized foreign-key constraint; other restrictions retain the original error, with five focused translation cases (internal/store/pgstore/errors.go:63; internal/store/pgstore/schema_test.go:459).
- ⚠️ Cannot verify from this diff: upstream snapshot.Build enforcement of the five documented invariants. The controller should retain Task 1's accepted evidence; this task only documents/consumes them (internal/render/doc.go:6).
- ⚠️ Cannot verify from this diff: complete-file output determinism, assembler-specific system-value prerequisites, execution command/system-part generation, and script parameter exports through model.EnvName/SnapshotValue.EnvValue. These are intentionally deferred to Tasks 6–8; those tasks must use the shared engine/resolver and complete the public Render behavior (internal/render/render.go:47, internal/render/render.go:85; internal/render/doc.go:14).
- ⚠️ Cannot verify from this diff: artifact producers and future assemblers consistently use the exact repo/iso roles, and concurrent task integration reconciles code-table totals/architecture thresholds. The controller should check these at integration (internal/render/render.go:20; internal/core/model/problem_test.go:89; internal/archtest/deps_test.go:74).

### Strengths

- Input maps and snapshot slices are read through lookups; output order follows source segments and instance order, with explicit repeated-call, map insertion-order, and non-mutation assertions (internal/render/expand.go:68, internal/render/expand.go:153; internal/render/expand_test.go:167, internal/render/expand_test.go:196).
- The lexer remains centralized. Lines() preserves original LF/CRLF endings and rune positions without mutating Result.Segments; tests reconstruct the source bytes (internal/core/scan/lines.go:15, internal/core/scan/lines.go:20; internal/core/scan/lines_test.go:8).
- Diagnostics retain segment positions and template-version/instance provenance; the public entry point has explicit version-identity tests (internal/render/expand.go:202; internal/render/render.go:100; internal/render/expand_test.go:202, internal/render/expand_test.go:245).
- Responsibility boundaries remain clear: render.go owns interface/preflight/dispatch, expand.go owns the text engine, scan owns line segmentation, and doc.go names the invariants. Production render imports only core and allowed standard-library packages (internal/render/render.go:3; internal/render/expand.go:3; internal/render/doc.go:1).

### Issues

#### Critical (Must Fix)

- None found in the reviewed task diff.

#### Important (Should Fix)

- None found in the reviewed task diff.

#### Minor (Nice to Have)

- None found in the reviewed task diff.

### Assessment

**Task quality:** Approved.

**Reasoning:** The implementation meets the scoped skeleton/expansion contract and the two root-authorized amendments. The public assemblers remain deliberately deferred, and the common engine has focused behavioral coverage for the important expansion and failure boundaries.

**Evidence checked:** Read review-c3c16bd..268def9.diff once and task-4-make-check-finalized.log. The tool output truncated expand.go inside expandLine and the beginning of expand_test.go, so only those omitted ranges were read directly from the r4 clone. The finalized log shows clean gofmt verification, vet, normal tests, race tests, and Linux/amd64 build; no reported suites were rerun.

**Focused unchanged-code risk checks:** Checked internal/core/model/task.go:156–175 because repeated subvalue references index each list row; SnapshotValue.Sub preserves List and delegates to the shared accessor, while Join/EnvValue retain their established separators. Checked internal/core/scan/scan.go:139–157 and internal/core/scan/scan.go:171 because Lines() only splits raw text and assumes placeholders cannot span lines; Scan explicitly scans placeholders within each source line. Neither check exposed a defect.

