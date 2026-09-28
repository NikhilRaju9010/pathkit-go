# PathKit for Go — Build Plan

Tick each milestone only when its Definition of Done (see `CLAUDE.md`) is met: `go test ./...` and `go vet ./...` pass, the Decisions Log, `LIMITATIONS.md` and `SETUP-GUIDE.md` are updated where needed, and the owner has been given step-by-step commands to verify it themselves. Each milestone starts in Plan Mode and only after the owner's go-ahead.

**Status (2026-09-28):** the design decisions D1–D10 in `CLAUDE.md` and this M0–M9 order are **approved**, with four amendments (see the `CLAUDE.md` Decisions Log). M0 has not started; it waits for the owner's go-ahead.

## Why this order differs from the first draft

The first draft was: M0 skeleton, M1 sample project, M2 analyze, M3 scope config, M4 trace recording, M5 coverage, M6 report, M7 HTML, M8 pilot + release. Two changes:

1. **Trace recording moves up, right after the first `analyze` slice (M3).** It's the riskiest part: will `go test -overlay` plus a generated recorder file really work inside the Temporal test environment, without touching the user's `go.mod`? If not, the edge-ID design has to change, and it's far cheaper to find out before building eight constructs on top. Everyday analogy: before you tile the whole bathroom, you check that one tile sticks.
2. **Each construct is built "vertically": analysis and recording together (M4).** The TS version built all the analysis first (M2–M7) and bolted recording on later (G3–G7). That's how the two drifted apart (the `outcomeEdgeIndex` fix, the try-order bug). In Go, each junction type (switch, Selector, loop, …) is added to the model, the instrumenter and the matcher in the same milestone, with a test proving they agree.

Scope config (M5) comes after the constructs, because it filters a list of discovered workflows that must already exist, and before coverage (M6), because coverage % must only ever be calculated on the in-scope set.

## Milestones

- [x] **M0 — Skeleton.** Install Go on the dev machine. **Verify the minimum Go version:** read the real `go.mod` of the exact `golang.org/x/tools` and `go.temporal.io/sdk` versions we pin, set PathKit's `go` line to the highest of them (research says 1.26), and record the result in the Decisions Log. `go.mod` (`github.com/NikhilRaju9010/pathkit-go`, `go` line from that check), `cmd/pathkit` with cobra, `pathkit --version`, the `pathkit <command>: <message>` error helper with exit codes 0/1/2, `go vet` + `staticcheck` + `gofmt` check in GitHub Actions CI, `.gitignore` (`.pathkit/`), `SETUP-GUIDE.md` stub.
  *Visible result:* `go run ./cmd/pathkit --version` prints a version; `go run ./cmd/pathkit analyze` prints `pathkit analyze: missing <file> argument` and exits 1.

- [ ] **M1 — Go sample project for piloting.** A separate Go module under `testdata/pilot/` (its own `go.mod`, depends on `go.temporal.io/sdk`) with ~5 small but realistic workflows: order (if + activity error), approval (signal + `AwaitWithTimeout`), polling (retry loop), shipment (Selector: signal vs timer, plus `switch`), saga (`defer` compensation, child workflow, ContinueAsNew). Each has a normal `testsuite` test file with a few cases. No PathKit code involved: `cd testdata/pilot && go test ./...` passes.
  *Why here:* every later milestone is checked against the same real-looking code, and it proves the Temporal test env behaves the way `CLAUDE.md` D6 says (fake clock, mocks, retries).

- [ ] **M2 — Graph model + `analyze`, first slice.** `internal/load` (`go/packages` loader), workflow discovery (D3: first parameter `workflow.Context`, last result `error`, exported; no `RegisterWorkflow` detection in v1), `internal/model` built on `go/cfg` with stable junction/edge IDs (D5), path enumeration with the 2000 cap and loop rule, `if`/`else` junctions and the Temporal-error-check rule (D2) including `//pathkit:ignore` / `//pathkit:branch`, end kinds (completed / failed / continued-as-new). `pathkit analyze <file|package>` with `--summary`, `--limit`, `--mermaid`, `--out`. Output format matches the TS setup guide. Also: check whether `ReceiveWithTimeout` really exists in the SDK version we pin (open fact from research).
  *Visible result:* `pathkit analyze testdata/pilot/order/order.go` prints a numbered path list.

- [ ] **M3 — Trace recording spike (end to end, `if` + error checks only).** Instrumenter that asks `internal/model` for every edge ID (never computes its own), generated recorder file added through the overlay, `//line` directives, `pathkit test` running `go test -overlay`, trace JSON files with per-workflow hash, the graph-walking matcher (D5), and the automated consistency check (every `hit()` ID exists in the graph; every graph exit has exactly one `hit()`). Proven on the pilot `order` workflow with its unchanged tests. `SETUP-GUIDE.md` and `pathkit test --help` state clearly that coverage needs `pathkit test` and that a plain `go test` records nothing.
  *Visible result:* `pathkit test ./order/...` in the pilot project writes trace files; a debug command prints which path each trace matched. **Stop point:** if the overlay approach fails here, go back to the owner with options before continuing.

- [ ] **M4 — Remaining constructs, analysis + recording together.** One sub-step per construct, each with analyze output, instrumentation, matcher support and a live `testsuite` test: (a) `switch`/type switch, (b) `for`/`range` retry loops with `break`/`continue`/labels (via `go/cfg`), (c) `workflow.Selector` with `AddReceive`/`AddFuture`/`AddDefault` and `NewTimer` timeouts, (d) `AwaitWithTimeout` / `ReceiveAsync` (and `ReceiveWithTimeout` if it exists), (e) child workflow calls as labelled steps + error checks, (f) `defer` compensation recognized and noted on paths (not a junction), (g) ContinueAsNew end kind. Anything that can be analyzed but not safely instrumented fails loudly with a clear message (TS rule: never silently wrong).
  *Visible result:* every pilot workflow analyzes and records correctly.

- [ ] **M5 — Workflow-level scope config.** `.pathkitrc.json` (search from the current folder up to `go.mod`), `workflows.include` / `workflows.exclude` with reasons, unknown names are errors, `packages`, `traces`, `failUnder`, `html`, `out`, `json`, `noColor`, `allowStale`. `include` can also add a workflow the automatic detection missed (e.g. unexported); an `include` name that matches no function, or a function whose first parameter isn't `workflow.Context`, is an error; such workflows are labelled `added by config` in `analyze` and `report` output (text and `--json`; M8 carries the label into HTML). Applied to `analyze` (with `--all` to override and a "hidden by scope" note) and to `pathkit test` (only in-scope workflows are instrumented). `--include`/`--exclude` flags take workflow names.
  *Visible result:* in the pilot project, excluding the saga workflow hides it from `analyze` and stops it being instrumented.

- [ ] **M6 — `coverage` command, `--fail-under`, trace cleanup.** `pathkit coverage <file|package> --traces <dir> [--function <name>] [--json] [--out] [--allow-stale] [--clean] [--fail-under <pct>]`. Path coverage plus branch coverage (branch coverage shown only; `--fail-under` checks path coverage). When no trace files exist, the message says coverage is recorded only by `pathkit test`, not plain `go test`. Unmatched traces are stderr warnings that say *where* the trace left the map. `pathkit clean [--older-than <duration>]`; `pathkit test` clears old traces unless `--keep-traces`. Exit code 2 below the threshold.
  *Visible result:* `Covered: 3/5 (60.0%)` for a pilot workflow; `--fail-under 80` exits 2.

- [ ] **M7 — `report` command.** Scans package patterns (`./...`), applies scope, per-workflow rows plus a project total computed only over in-scope workflows (summing raw counts, never averaging percentages), the "N workflows excluded by .pathkitrc.json" line, green/red color on a real terminal only (`--no-color`, `NO_COLOR`), `--json`, `--out` (always plain text), `--allow-stale`, `--fail-under`. Also adds `report --summary` (deferred in TS).
  *Visible result:* `pathkit report ./...` in the pilot project prints the full project report.

- [ ] **M8 — HTML report.** `--html[=path]` on `analyze` and `report`, one self-contained file (Go `html/template` + `embed`), Analysis and Coverage tabs, same path numbers in both tabs, coverage trend of the last 5 runs, excluded workflows listed with their reasons.
  *Visible result:* `.pathkit/report.html` opens in a browser with no server and no internet.

- [ ] **M9 — Pilot + release.** Run the whole tool on the pilot project and on at least one real Temporal Go codebase (e.g. `temporalio/samples-go`), record findings in `LIMITATIONS.md`, finish `SETUP-GUIDE.md`, GoReleaser + GitHub Actions release on tag `v0.1.0`, verify `go install ...@v0.1.0` and a downloaded release binary both work on a clean machine.
  *Visible result:* a tagged GitHub release with binaries.
