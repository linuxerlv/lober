# Layer 6 code review — 2026-10-15

Reviewed the actual staged implementation and current working files against baseline `467314f`, the Layer 6 plan, overall design §4.6/§5.1–5.3, and Layer 5 console design. This is a source review, not a claim that browser acceptance or backend tests passed. No full test suite was duplicated. Findings below remain actionable at the reviewed snapshot.

## Findings

### P2 — Parameter edits made during validation are silently replaced by the request snapshot

- Location: `web/apps/web-ele/src/views/tasks/new.vue:102–107`, with editable controls at lines 169–171.
- Trigger: On the parameter step, enter a valid value, click “校验并确认”, then change that value or use “填写整列” before `/wizard/params` finishes. The text/select controls and batch-fill button stay enabled during `busy`.
- Impact: `evaluate` captures `entered` before the request, and the completion callback rebuilds `state.entered` from that captured object and advances to confirmation. The latest visible edit disappears. A subsequent task submission uses the earlier value; this can affect target disk, addresses, or other installation settings.
- Fix: Disable all parameter and batch-fill controls while evaluating, or track input revisions and reject/re-evaluate responses whose input revision changed. Do not overwrite newer input or advance to confirmation for an older input revision. Add a deferred-response interaction regression that edits a field after validation starts.

### P2 — Late authoring mutations can overwrite a different editor session

- Location: `web/apps/web-ele/src/views/templates/index.vue:265–277` and `web/apps/web-ele/src/views/hooks/index.vue:176–190`. The enabled close controls are at template line 790 and hook line 468.
- Trigger: Save draft A with a delayed response, close its dialog while the save is pending, then open/create draft B. `open` and `create` change `editorToken`, but save/new-version/derive/lifecycle mutation completions do not check it.
- Impact: A's completed request calls `assign` into the shared `selected` ref, replacing B's editor content and potentially losing B's input. Follow-up detail loads and success messages also run against the replacement state. The token checks on GET requests do not protect these mutation paths.
- Fix: Capture the editor token and resource key before each mutation; apply results and follow-up editor updates only if both still match. Alternatively block closing/changing the editor for the entire mutation. Cover close A → open/create B → resolve A in a deferred-response regression for both editors.

### P2 — Template preview lacks the required server-driven parameter workflow

- Location: `web/apps/web-ele/src/views/templates/index.vue:412–429` and `:918–929`.
- Trigger: Preview a draft with a list parameter, an ISO-offer enum, or an editable value supplied from a host attribute. `previewFields` derives only key/label/locked locally from declarations and bindings; every input is a single-line `ElInput`. No server parameter-form evaluation is requested after choosing the host/ISO.
- Impact: A list cannot be entered with the wizard's newline semantics, ISO options and evaluated prefills are unavailable, and preview cannot be operated with the same parameter form as actual task creation. This is a real workflow gap against overall design §4.6 (“参数表单都由服务端给出”) and §5.2 step 4 (“参数按向导的方式填”). The preview dialog also exposes no `series` choice, so without a real ISO it can only preview the backend's first selected series rather than another series covered by the template (§5.2 step 4).
- Fix: Expose/reuse server-side parameter evaluation for the selected draft plus real/sample ISO and real/sample host, then render its returned type/list/options/locked/prefill fields as the wizard does. Add a template-series selector when ISO is omitted. Preserve the existing complete-file rendering endpoint.

## Inspected scope

Inspected generated schema/type generator and API adapters; authentication/session/password/permission routes; host editing, CSV bytes and cursor handling; ISO source registration, chunk resume identity and progress lifecycle; hook/template editing, bindings, replacement ordering, approval controls and candidate/source-diff handlers; wizard selection/parameter/submit state; task list/detail/event/SSE lifecycle; user/audit surfaces; SPA fallback; exporter dependency guard. No additional actionable issue was established in those inspected areas. Existing evidence logs were not treated as substitutes for source inspection or fresh acceptance evidence.

## Resolution after review

The implementer addressed all three findings and retained this original report as the reviewed snapshot. `views/tasks/validation.test.ts` covers validation input freezing and failed-preview recovery. Mounted regressions in `views/hooks/selection.test.ts` and `views/templates/authoring.test.ts` cover late editor mutations. `internal/console/preview_params_test.go` and the template mounted tests cover server-evaluated preview fields. Final checks and real browser evidence are recorded in [Layer 6 acceptance](2026-10-15-layer6-acceptance.md). This resolution note is not a second independent review.

## Independent targeted re-review — 2026-10-15

Re-reviewed only the three P2 findings above and their fixes, as requested. No full-suite rerun or expanded repository review was performed. **All three original findings are resolved; no remaining blocker was found within this targeted scope.**

- Parameter validation: every editable parameter input/select and both batch-fill inputs now honor `busy`; the batch-fill button is disabled and `fillColumn` also guards `busy`. The original input-during-validation trigger is prevented.
- Authoring mutations: hook/template save, lifecycle, new-version, derive and delete paths capture the editor token and check resource identity before applying a result. Closing invalidates the token synchronously; stale request errors, completion messages and busy-state changes are also gated. Follow-up submit/validate/preview/candidate actions stop when a save belongs to an obsolete editor.
- Preview: `preview-params` uses core `params.Evaluate`, and shares real/sample host, ISO, series and firmware resolution with final rendering through `previewTemplateFacts`. The UI consumes returned fields; `PreviewFields.vue` renders multiline lists, ISO enum options, locked prefills and canonical editable values. The sample-series selector is present. Context revisions clear the old form and reject late parameter/render responses. Inspected `preview_params_test.go` covers real facts, locked lists, ISO options, core diagnostic equivalence and invalid/missing facts; those Go tests were not independently rerun in this targeted review.

Fresh targeted verification from `web` at 22:09 local time:

```text
pnpm exec vitest run --config apps/web-ele/vitest.config.ts apps/web-ele/src/views/tasks/validation.test.ts apps/web-ele/src/views/hooks/selection.test.ts
Test Files  2 passed (2)
Tests       11 passed (11)
```

This conclusion closes the original three review findings. It does not independently certify the separate full acceptance report or other browser/backend checks.
