# SDD ledger — plan: docs/superpowers/plans/2026-10-17-enterprise-testing.md

Batch 2 resumes already approved Tasks 3/4/6. Baseline test 9a4d43d91d551fad24ac49eff04c836a723e3a69; product dev 88bce232fc633ff5dc06c0285748d17f4f47b184.
Earlier Tasks 1/2 and dependency work have committed reports. Do not repeat them or touch dirty primary checkout.
Pre-flight: Tasks 3/4/6 share fixture, candidate identity and run.py/gate.py results. Three added suites now consume same schema isolation and identity checks. Extended gate observed RED (4 tests, 8 subtest failures) then GREEN (29 tests).
Ruling: keep established standard-library enterprise runner rather than migrate outer legacy pytest — avoids collecting old fixtures/contracts; cost if wrong: migrate suite entry points later.
Ruling: test.transport store probe qualifies SSE transport only, leaves task pending — machine event API is unimplemented; cost if wrong: rerun real machine signals after layer 7 delivery.
Ruling: explicit source Retry resets terminal failed work; crash fetching resumes its checkpoint — verified iso.go/remote_test.go contracts; cost if wrong: revise recovery expectations with product contract change.
Ruling: control-complete means listed control profile, not every enterprise plan scenario — exact publication/deletion crashes and other release requirements remain unqualified; cost if wrong: additional process fault checkpoints and runs.

Task 3 supplement: 3 original SSE tests GREEN; added browser true TCP disconnect/replay/dedup GREEN, total events 4.
Task 4 supplement: full four actual form flows GREEN; delayed real resource reply GREEN; role revocation and account switch GREEN; empty menu restore enhancement running.
Task 6 supplement: DB proxy disconnect, bounded 8MiB tmpfs ENOSPC, TCP body cut + manual Retry, SIGKILL + Range resume GREEN. HTTP timeout response fixture removed; genuine kernel TCP timeout qualification running.
Source changes belong only to test branch. No product bug identified in this batch. Product reuse requires matching entire cmd/internal/web/go.mod/go.sum/Makefile file map and binary digest.
Remaining: final frozen gate/control/flows/events/recovery, fresh backend/frontend checks, one whole-branch review, evidence/report.

Implementation committed: 078117f. WIP gate29/control88/flows3 (8 UI scenarios)/events4/recovery5 GREEN, real kernel SYN timeout completed138.775s; no mocked HTTP 408 retained.
Final review: one fresh-context reviewer on 9a4d43d..078117f. Critical0; Important2 (Bearer diagnostic leak, nondeterministic browser duplicate coverage); Minor1 cleanup order regraded Important because a failed run must not leave a mount. Both declined-to-judge groups remain explicit report limitations.
Final single fix pass: redaction RED→GREEN; old dedup scenario failed new actual-overlap assertion, then real REST/SSE seq5 overlap GREEN; cleanup old registration RED (unmount/proxy before service)→new ordering GREEN; gate30 GREEN. Fix committed fc6d827; no second independent review claimed.
Final candidate: product88bce232, testsfc6d827, run vm-control-20261018-9400840715ba9728. Final suite runs in /home/dev/lober-control-20261018-reviewed. Backend/frontend fresh commands from first batch2 freeze e39778df3fd64c64; exact1643 product input map unchanged, evidence origin retained; no repeat of same unchanged commands after Python/JS test-only fixes.
Final Python/Node suites GREEN: gate30 (0.021s), control88 (37.849s), flows4 (138.684s; 8 actual UI scenarios plus redaction), events4 (58.120s; real REST/SSE same-seq overlap), recovery5 (143.839s). All zero failed/skipped. Cleanup evidence: zero dedicated timeout rules and zero enterprise tmpfs mounts remain. Fresh frontend check:console exit0; fresh backend make check/race then uncached still running (historical console race403s, current process busy rather than stalled).

Final: backend make check + uncached exit0; frontend check:console exit0. control-smoke/control-complete PASS exit0; release FAIL exit1. All final enterprise suites131 cases, failed0/skipped0. Evidence validated including red/green logs; public archive transport SHA256 f426708dc32d98a5fd9695b4de2910b2f29dc57ff1ccbfe3c99f78b2d2f236ed. Report records every ruling and qualification gap; no deferred minor remains. No product changes, remote push or main merge.
