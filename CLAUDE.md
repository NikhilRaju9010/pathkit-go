# CLAUDE.md — PathKit for Go

**Before doing anything else in this repo, read [PLAN.md](./PLAN.md) and [LIMITATIONS.md](./LIMITATIONS.md) in full, then summarize the current status (which milestone is done, which is next, any open limitation) before starting new work.**

## Project purpose

PathKit for Go answers one question about Temporal **Go** workflows:

> "Which execution paths exist, and which ones do my tests actually run?"

Think of a workflow like a subway map. Every `if`, every "did the activity fail?", every "did the signal arrive before the timer?" is a junction. A **path** is one complete trip from the start station to an end station. `analyze` draws the map. `coverage` and `report` color the trips your tests actually took.

It is a Go rewrite of the TypeScript tool PathKit (github.com/NikhilRaju9010/pathkit). Its **user-facing behavior** follows the TypeScript `PATHKIT-SETUP-GUIDE.md`:

- the same three commands: `analyze`, `coverage`, `report`
- the same flag names (`--summary`, `--limit`, `--mermaid`, `--out`, `--html`, `--json`, `--traces`, `--function`, `--allow-stale`, `--clean`, `--no-color`, `--include`, `--exclude`)
- the same output format (`Workflow: ...`, `Total paths: N`, `  1. Start -> ... -> End`, `1/3 paths · 33.3%`, the project total line)
- the same config file name, `.pathkitrc.json`
- the same error style: `pathkit <command>: <message>`, exit code 1

The TypeScript **implementation** (ts-morph, Jest, npm, instrumented file copies next to your source, a Temporal Query to fetch traces) is **redesigned for Go**, not copied. The reasons are below.

## Working rules (non-negotiable)

1. **Always use Plan Mode.** For every milestone: I write a plan, you review it, you approve it, then I execute. One milestone at a time.
2. **Explain everything in plain, simple English with everyday examples.** The project owner is a QA engineer with limited Go experience. No unexplained jargon. When a Go term is unavoidable (e.g. "package", "interface"), explain it the first time.
3. **Definition of done for every milestone:**
   - `go test ./...` passes
   - `go vet ./...` passes
   - `CLAUDE.md` Decisions Log updated (if anything was decided or changed)
   - `PLAN.md` checkbox ticked
   - `LIMITATIONS.md` updated (if anything new is known)
   - `SETUP-GUIDE.md` updated (if user-visible behavior changed)
   - then give the owner **step-by-step commands to verify it themselves** before they commit.
4. **Never start the next milestone without the owner's go-ahead.**
5. Never delete files in the live project tree to "clean up" (the TypeScript project lost real verification evidence this way twice). Use a temp directory for experiments. Verification evidence goes where the owner can see it (`.pathkit/verification/`, gitignored) and is left in place.
6. Do not guess on design questions. If something is a real trade-off, write it down and ask.

## Architecture decisions

All decisions below were **APPROVED on 2026-09-28**, with the owner's four amendments written into D3, D4, D7 and D9 (see the Decisions Log). Any later change to a decision needs the owner's approval and a new Decisions Log entry.

### D1 — Code parsing: `golang.org/x/tools/go/packages` + `go/types` + `golang.org/x/tools/go/cfg` — APPROVED (2026-09-28)

| Option | What it is (plain English) | Pros | Cons |
| --- | --- | --- | --- |
| `go/ast` only | Reads the text of one file into a tree. Knows *shapes*, not *meanings*. | Fast. Works on a single file, even if the project doesn't compile. No extra dependencies. | Can't be sure `wf.ExecuteActivity` is Temporal's function (import aliases, dot-imports, wrapper types). Can't tell what type a variable is. This is the same "guess from the text" weakness the TS version had with activity detection. |
| `go/types` | Adds meaning: "this `x` is a `workflow.Future`", "this call is `go.temporal.io/sdk/workflow.ExecuteActivity`". | Exact identification of Temporal calls, the `workflow.Context` parameter, `error` variables. No guessing. | Needs *all* files of the package and its imports; the code must compile. You need a loader to feed it. |
| `go/packages` | The standard loader. Runs `go list` under the hood and hands you files + type info for a package, the same way `go build` sees it. | Handles modules, build tags, vendoring, and **overlays** (see D4). The same loader `gopls` and `go vet` tools use. | The user's project must compile and its modules must be downloaded. A Go toolchain must be installed where PathKit runs. Slower than raw parsing (a few seconds on a big package). |
| `go/cfg` (extra) | Builds a "control-flow graph" of one function: which block of code can run after which. | Already correctly handles `break`, `continue`, labels, `goto`, `switch`, `select`, `return` — the exact things the TS version got wrong one bug at a time (unlabeled `break`, labeled `break`, `continue`, `throw`-into-`catch`). | Low-level (plain blocks, no idea what Temporal is). We still have to shrink it down to "only the junctions that matter". Does not model `panic` or `&&`/`||` short-circuits. |

**Recommendation:** load one package with `go/packages` (type info on), identify Temporal constructs with `go/types`, and build our junction graph on top of `go/cfg`'s block graph instead of writing our own statement walker. Everyday analogy: `go/ast` is reading a street map by the shapes of the roads; `go/types` adds the street names; `go/cfg` adds the one-way signs. We want all three.

Cost we accept: PathKit only works on code that compiles. That is fine — the user is running `go test` on it anyway.

### D2 — Error branches without a path explosion — APPROVED (2026-09-28)

Go workflows are full of `if err != nil { return err }`. If every one counted as a junction, a workflow with 12 of them would have 4,096 paths, and most of those junctions are boring (a JSON decode that can't really fail).

**Recommendation — a "Temporal error check" rule:** an `if err != nil` (or `if err == nil`, or `if errors.As/Is(err, ...)`) counts as a junction **only when that `err` was last assigned by a Temporal call**:
`ExecuteActivity(...).Get`, `ExecuteLocalActivity(...).Get`, `ExecuteChildWorkflow(...).Get`, `SignalExternalWorkflow(...).Get`, `RequestCancelExternalWorkflow(...).Get`, `workflow.Sleep`, `workflow.Await*`, a timer future's `.Get`, and `Selector`-less `Future.Get` on any of those futures. `go/types` lets us follow the `err` variable back to where it was set (inside the same function).

Every other `if err != nil` is **transparent**: PathKit assumes the "no error" side and does not create a junction. (This matches how the TS version treated a `try/catch` that didn't wrap an activity.)

Its outcomes are labelled `failure` / `success`, like the TS version's try/catch, so the output reads the same.

Two escape hatches:
- `//pathkit:ignore` comment on an `if` → never a junction (e.g. a defensive check you can't test).
- `//pathkit:branch` comment on an `if` → always a junction (e.g. a non-Temporal error you do want tracked).

Plain `if` statements that are not error checks (`if order.Amount <= 0`) are always junctions, same as TS.

Second defence against explosion: `report`/`coverage` also print **branch coverage** (how many junction *exits* were taken at least once) next to path coverage. Branch coverage grows linearly, so it stays meaningful even when a workflow has thousands of paths. `maxPaths` stays at 2000, same as TS.

### D3 — Which Temporal Go constructs become junctions — APPROVED (2026-09-28)

| Construct | v1 decision | Outcome labels |
| --- | --- | --- |
| `if` / `else`, `else if` | Junction (except transparent error checks, D2) | `true` / `false` |
| `switch`, type switch | **Junction** (the TS version skipped `switch`; Go workflows use it a lot, e.g. on a signal's type). A missing `default` adds an implicit `default` exit. `fallthrough` handled by `go/cfg`. | `case <expr>` / `default` |
| `workflow.Selector` + `Select(ctx)` | Junction at `Select`. Each `AddReceive`/`AddFuture`/`AddDefault` registered **on the same selector variable, in the same function, with an inline func literal** is one exit. The callback's body becomes that exit's road. | `signal "<name>"` (receive on a signal channel), `timeout` (future from `workflow.NewTimer`), `activity <name>` / `child <name>` (activity/child future), `default` |
| `workflow.NewTimer` in a Selector | Covered by the Selector rule → `timeout` label. The Go equivalent of TS `Promise.race` + `sleep`. | |
| `GetSignalChannel(...).Receive(ctx, &v)` | **Not** a junction — it just waits, only one thing can happen. | |
| `ReceiveAsync` / `ReceiveWithTimeout` (if present in the SDK version) used in an `if` | That `if` is a junction; labelled `received` / `not received`. | |
| `workflow.Await(ctx, cond)` | Not a junction (one outcome). | |
| `workflow.AwaitWithTimeout(ctx, d, cond)` | The `if ok` that follows is a junction, labelled `signaled` / `timeout` (the Go equivalent of TS `condition(fn, timeout)`). | |
| `for` loop / `for range` containing a Temporal call | Loop junction with `iterate` / `exit`; the back-edge is `retry`. A path takes the loop "at least once" or "not at all" (same as TS). A `for {}` with no condition only leaves through `break`/`return`. | `iterate`, `exit`, `retry` |
| Loop with no Temporal call | Transparent (walked once), same as TS. | |
| `ExecuteActivity` error handling | Via D2. | `success` / `failure` |
| `ExecuteChildWorkflow` | Error check via D2. The child's own code is **not** followed (it's another workflow with its own map). Shown as a labelled step. | |
| `defer` compensation (saga) | **Recognized and labelled, not a junction in v1.** See reasoning below. | |
| `workflow.NewContinueAsNewError` | Not a junction, but a **different end station**. | End kinds: `End (completed)`, `End (failed)`, `End (continued-as-new)` |
| `workflow.Go` (coroutines), futures awaited together | Not junctions in v1 (TS limit for `Promise.all` carries over). | |
| Query / Update / Signal handlers set with `SetQueryHandler` / `SetUpdateHandler` / signal callbacks | Not followed in v1 (separate entry points). | |

**Why saga `defer` is not a junction in v1:** a saga usually looks like
`defer func() { if err != nil { compensate() } }()`. Whether that inner `if` fires depends on *how the function ended*: it fires on failure exits and never on success exits. If PathKit multiplied every exit by "compensated / not compensated", it would invent paths that can never happen ("order succeeded **and** compensation ran"). Those would show up as "missed" forever, and coverage would be wrong — which is exactly the complaint about the TS version. v1 shows a `compensation (defer)` note on the paths instead; modelling it properly is a candidate for v1.1.

**Why different end stations matter:** the TS version funnelled "returned normally" and "threw an error" into one `End`. In Go we can see `return nil`, `return err`, and `return workflow.NewContinueAsNewError(...)` directly, so the output can say which kind of end each path reaches.

**How a workflow function is recognized (lesson from workflowcheck):** a top-level function or method that meets **all three** rules: its **first parameter's type is `go.temporal.io/sdk/workflow.Context`** (checked with `go/types`, not by name), its **last result is `error`**, and it is **exported** (name starts with a capital letter). Unexported helpers that take `workflow.Context` are not workflows. v1 does **not** look at `RegisterWorkflow` calls (owner's decision, 2026-09-28); the scope config (D8) handles anything this rule gets wrong.

### D4 — Trace recording: instrument in memory, swap in with `go test -overlay` — APPROVED (2026-09-28)

The problem: when a test runs a workflow, something must write down which exits it took at each junction, in order.

| Option | How it works | Pros | Cons |
| --- | --- | --- | --- |
| **(a) Instrumented copy + `go test -overlay`** | PathKit writes an instrumented version of each workflow file into `.pathkit/overlay/` and an `overlay.json` that says "when compiling `orders.go`, use this file instead". `go test -overlay=...` compiles the copy. Your real files are never touched. | Sees every junction, including plain `if`s. No copies left next to your source (TS left `*.pathkit-instrumented.ts` files). **Your existing tests work unchanged** — no `prepareCoverageRun`/`recordCoverageTrace` boilerplate. Every workflow in scope is instrumented at once (TS: one function per call). Standard Go feature (since Go 1.16), also understood by `go/packages`. | Tests must be run through `pathkit test` (or with the `-overlay` flag added by hand). Line numbers in stack traces would shift — fixed with `//line` directives so errors still point at your real file and line. |
| (b) Query handler | Instrumented code keeps the trace in memory; the test calls `env.QueryWorkflow("__pathkit_trace")`. | Familiar from TS. `QueryWorkflow` works synchronously in the Go test env (no TS-style hang). | Still needs (a)'s instrumentation to *collect* the trace — the query is only a way to fetch it. Adds boilerplate to every test. Adds nothing over (a). |
| (c) Read the event history | After the test, look at which activities/timers/signals happened. | No instrumentation at all. | The Go test environment does not expose the history. And history only shows Temporal commands, not plain `if`s — `if order.Amount <= 0` leaves no trace in history. Can't tell paths apart. |
| (d1) Go's built-in coverage (`go test -coverprofile`) | Go already counts which code blocks ran. | Zero work from us. | Counts are added up across the whole test run: you learn "this block ran at some point", never "*this* run took *this* path". Gives branch coverage, not path coverage. |
| (d2) Interceptors | Temporal lets you wrap activity/timer/child calls. | Official extension point. | Only sees Temporal calls, not plain junctions. |
| (d3) Replay real histories | Run histories exported from staging/production through `worker.WorkflowReplayer` with the instrumented code. | Answers "which paths does *real traffic* take?" | A different feature. Great v2 idea, not v1. |

**Recommendation: (a).** How it fits together (everyday version: we hand the compiler a marked-up photocopy and it never knows the difference):

1. `pathkit test ./...` loads the packages, finds workflows in scope, and builds the junction graph (D5).
2. For each workflow file, it writes an instrumented copy into `.pathkit/overlay/`. It also **adds one extra file** to the workflow's package through the overlay (`zz_pathkit_recorder.go`) that contains the tiny recorder. Because the recorder is generated into your package, **your `go.mod` never needs a PathKit dependency**. The recorder only uses the Go standard library and the Temporal `workflow` package you already have.
3. Each instrumented workflow function starts with `pk := pathkitStart(ctx, "<workflow ID>")` and `defer pk.flush()`. At each junction exit it calls `pk.hit("<edge ID>")`. The recorder is a **local variable** of that one function call — not a global map — so parallel tests and repeated runs can't mix their traces. (The Go test env gives every execution the same default run ID, so keying a global map by run ID would collide.)
4. When the workflow function returns, `flush()` writes one JSON trace file to the trace folder. If the function exits abnormally (panic, or the SDK stopping it), the trace is marked `incomplete` and ignored by reports.
5. `pathkit test` runs `go test -overlay=.pathkit/overlay/overlay.json <your packages> <your flags>`, and passes the **absolute** trace folder in an environment variable. (Fixes the TS rule 6 confusion: `go test` runs each package in its own folder, so a relative path would scatter traces.)
6. If you prefer to run `go test` yourself: `pathkit prepare ./...` writes the overlay and prints the flag to add.

**Coverage needs `pathkit test`, not plain `go test` (owner's requirement, must be stated clearly to users).** A plain `go test` compiles your original, unmarked workflow files, so it records **no traces**, and `coverage`/`report` will show 0% for runs made that way. Your tests still pass or fail normally, so the missing traces are easy to miss. Only `pathkit test` (or `go test` with the `-overlay` flag printed by `pathkit prepare`) records coverage. This must appear in `SETUP-GUIDE.md` (at the top of the coverage section, and in the errors table), in `pathkit test --help`, and in `LIMITATIONS.md`. When `coverage`/`report` find no trace files at all, the message says so directly: `no trace files found in <dir>; coverage is recorded only by "pathkit test" (plain "go test" records nothing)`.

No `workflow.IsReplaying` guard: each run of the function records the whole trip it made, so a replay just produces another complete, correct trace.

### D5 — One single source of truth for paths and edge IDs — APPROVED (2026-09-28) (hard requirement)

The TS bug: `analyze` listed a path's steps in one order, the recorded trace came out in another order, and the matcher compared them as lists, so real runs showed as "missed". The root cause was **two separate things each deciding the order**.

The Go design removes that possibility:

1. **One package builds the graph (`internal/model`).** It assigns every junction a stable ID (`J1`, `J2`, … in source order within the function) and every exit an edge ID (`J3.true`, `J5.case:"approved"`, `J7.timeout`). `analyze`, the instrumenter, and the matcher all get IDs from this one package. The instrumenter never works out IDs on its own; it asks the model "which edge ID belongs to this exit of this AST node?". (This is the TS `outcomeEdgeIndex` idea, but as the only way in, not a later add-on.)
2. **The graph follows execution order.** Built on `go/cfg`, a junction appears in the graph exactly where it runs. The recorder calls `hit()` **at the top of each exit's road**, the moment the decision is made. Graph order and trace order are therefore the same thing by construction. (Go has no `try/catch`, so the TS bug's exact trigger, "try-success recorded at the end of the try block", doesn't exist here.)
3. **Matching walks the graph; it does not compare lists.** The matcher starts at `Start`, reads the trace one edge at a time, and follows that edge in the graph. If a step doesn't exist in the graph, the trace is reported as unmatched, with the exact step where it went off the map. The loop rule (keep only the last trip round a loop, same as TS) lives in this one walker.
4. **Built-in consistency test.** For every test fixture, an automated check proves that every edge ID the instrumenter writes exists in the graph, and every graph exit has exactly one `hit()` call. Plus end-to-end tests that run fixtures in the Temporal test env and check each trace lands on the expected path.
5. **Path IDs** are a short hash of the path's edge-ID list, so "path 3" in `analyze` and "path 3" in `report` are the same trip.

**Staleness is per workflow, not per file:** the trace stores a hash of that one function's code with comments and formatting removed (gofmt-normalized). Editing a comment, reformatting, or changing *another* workflow in the same file no longer makes traces "stale" (the TS version flagged any byte change to the file).

### D6 — Test environment: `go.temporal.io/sdk/testsuite` — APPROVED (2026-09-28)

Nothing special is needed in the user's tests. A normal test works:

```go
func Test_OrderRejected(t *testing.T) {
    var s testsuite.WorkflowTestSuite
    env := s.NewTestWorkflowEnvironment()
    env.OnActivity(ChargeCard, mock.Anything, mock.Anything).Return(errors.New("declined"))
    env.ExecuteWorkflow(OrderWorkflow, input)
    require.Error(t, env.GetWorkflowError())
}
```

Under `pathkit test`, the instrumented `OrderWorkflow` is compiled in instead, and a trace file appears when `ExecuteWorkflow` finishes. Facts that matter (checked against the SDK source):

- `ExecuteWorkflow` runs the workflow to the end synchronously, with a fake clock, so timers skip instantly (no TS-style "long timer hangs").
- `OnActivity(...).Return(...)` makes it easy to force the `failure` exit of any activity.
- `RegisterDelayedCallback` + `SignalWorkflow` drive signal and timeout exits.
- `OnWorkflow` mocks a child workflow: the child's code doesn't run, so it records nothing (correct — it's a separate map).
- Activity retries still apply in the test env: to test a failure path quickly, return `temporal.NewNonRetryableApplicationError(...)` from the mock or set `MaximumAttempts: 1`. (Same trap as TS rule 2, different fix.)
- The test env does not replay workflow code and does not expose event history (another reason for D4 option a).

### D7 — CLI, Go version, module, install — APPROVED (2026-09-28)

- **CLI library: `cobra` (with `pflag`).** The standard `flag` package stops reading flags at the first non-flag word, so `pathkit analyze orders.go --summary` would silently ignore `--summary` — the same "flag swallowed" bug class the TS version fixed three times. `cobra`/`pflag` reads flags in any position, supports `--flag` and `--flag=value`, and gives `--help` and shell completion for free. Cost: two small, very widely used dependencies. One behavior difference: an optional value is written `--html=report.html` (with `=`); `--html` alone still means "default path". `--html report.html` (with a space) would read `report.html` as the positional argument. The alternative is a hand-written parser (zero dependencies, exact TS behavior) — I recommend cobra.
- **Minimum Go version: set in M0 from the real `go.mod` files.** Research (2026-09-28) found that `golang.org/x/tools` and the Temporal Go SDK (v1.49.0) both declare `go 1.26.0`, which suggests 1.26. Owner's decision: this is **verified in M0** by reading the actual `go.mod` of the exact `x/tools` and `go.temporal.io/sdk` versions we pin (the SDK only matters for the test fixtures, but users need it too). PathKit's minimum is the highest `go` line among them, recorded in the Decisions Log.
- **Module path:** `github.com/NikhilRaju9010/pathkit-go`. Binary entry point: `cmd/pathkit`.
- **Install:** `go install github.com/NikhilRaju9010/pathkit-go/cmd/pathkit@latest`, plus prebuilt binaries for Linux/macOS/Windows (amd64/arm64) on GitHub Releases, built by GoReleaser from a git tag in GitHub Actions. `pathkit --version` reads the version stamped at build time, falling back to Go's build info for `go install`.
- **PathKit's own `go.mod` does not depend on the Temporal SDK.** Only the test fixtures need it, and they live in a separate module under `testdata/` (Go ignores `testdata/`). This is the Go version of the TS "Temporal packages are devDependencies only" decision.
- **Exit codes:** `0` success; `1` real error (`pathkit <command>: <message>`); `2` coverage below `--fail-under`. Using 2 lets CI tell "PathKit broke" apart from "coverage too low".

### D8 — Workflow-level scope config (applies to `analyze` AND `report`) — APPROVED (2026-09-28)

The TS config only matched whole file names and only in `report`, so untestable workflows dragged coverage down and the number didn't mean much. Go v1:

```json
{
  "packages": ["./internal/workflows/..."],
  "traces": ".pathkit/traces",
  "workflows": {
    "include": ["OrderWorkflow", "PaymentWorkflow", "shipping.ShippingWorkflow"],
    "exclude": [
      { "name": "LegacyBillingWorkflow", "reason": "needs real bank sandbox" }
    ]
  },
  "failUnder": 80,
  "html": true
}
```

- Entries name **workflows**, not files: `FuncName`, or `pkg.FuncName` / `pkg.(*Type).Method` when a name is ambiguous.
- `include` can also name a function the automatic rule (D3) missed, e.g. an unexported workflow or one returning no `error`, as long as its first parameter is `workflow.Context`. This is how the scope config "covers the rest" now that `RegisterWorkflow` detection is out of v1. Two rules (approved 2026-09-28):
  - If an `include` name matches no function, or matches a function whose first parameter is not `workflow.Context`, that is an **error** (exit 1), the same as a misspelled workflow name. Example: `pathkit report: .pathkitrc.json: include "sendEmail" is not a workflow: its first parameter is not workflow.Context`.
  - Workflows that are in scope only because `include` added them are labelled **`added by config`** in `report` (text, `--json` and HTML) and in `analyze`, so it's always visible which workflows the automatic rule found and which a person added by hand.
- The same scope is applied by `analyze`, `coverage`, `report`, and `pathkit test` (only in-scope workflows are instrumented).
- The coverage % is computed **only over in-scope workflows**. `report` always prints a line like `2 workflows excluded by .pathkitrc.json (see "reason")`, so nobody can quietly raise coverage by excluding things.
- `analyze` shows a note `N workflows hidden by scope; pass --all to show them`.
- An `include`/`exclude` name that matches no workflow is an **error**, not a warning (TS warned). A misspelled include would otherwise silently shrink the scope and inflate the %.
- The config file is looked up in the current folder, then parent folders up to the folder containing `go.mod`. (TS only checked the current folder.)
- The `--include`/`--exclude` flags stay for one-off runs and now take workflow names.

### D9 — `--fail-under` and trace cleanup — APPROVED (2026-09-28)

- `--fail-under <percent>` on `coverage` and `report` (config key `failUnder`). Below the threshold: normal report printed, then `pathkit report: coverage 62.5% is below --fail-under 80%` on stderr, exit code 2. In v1 it checks **path coverage only** (owner's decision, 2026-09-28). Branch coverage is shown but not used for pass/fail.
- **Cleanup:** `pathkit test` starts each run by clearing old traces (pass `--keep-traces` to add to them instead). New `pathkit clean` command deletes `.pathkit/traces` (or `--older-than 7d`). `--clean` on `coverage`/`report` stays, for parity. Traces from a stale workflow version are skipped and counted in a warning.

### D10 — Output parity — APPROVED (2026-09-28)

Text output keeps the TS format line for line, including `Start -> <junction> --<label>--> ... -> End`. Junction descriptions use Go source text (`if input.AmountCents <= 0`, `switch status`, `select (Selector)`). The only intentional differences are: the end station shows its kind when known (`End (failed)`), and `report` adds the branch-coverage figure and the "excluded by scope" line. `--json` shapes are documented in `SETUP-GUIDE.md` and tested with golden files.

## Non-goals for v1

- **One package at a time for analysis.** `analyze` takes a file (analyzes the workflows in it, but type-checks its whole package, since Go needs that) or a package; `report` takes package patterns like `./...` and analyzes each package separately.
- **No following calls into other packages or into child workflows.** A helper function in another package, or a child workflow started with `ExecuteChildWorkflow`, is shown as a single step. (Recommendation: keep this for v1. Following same-package helpers that take `workflow.Context` is a strong v1.1 candidate; ask before doing it.)
- No saga/`defer` junctions (see D3), no `workflow.Go` concurrency modelling, no Update/Query handler paths.
- No replay of production histories (candidate for v2, see D4 d3).
- No non-determinism checking — that's `workflowcheck`'s job; users should run it too.
- No IDE plugin, no server, no database. The HTML report is one self-contained file.

## What we learned from Temporal's `workflowcheck`

- It finds workflows **by type** (first parameter is `workflow.Context`, or registered with `RegisterWorkflow`), not by name. → D3 uses the type check too, but in v1 only together with "last result is `error`" and "exported"; it does not follow `RegisterWorkflow` calls.
- It is built on Go's standard analysis tooling (`go/analysis`), so it plugs into `go vet`. → We use the same loading/type-checking stack (`go/packages`, `go/types`). A `go vet`-style analyzer that warns about un-instrumentable shapes is a possible later add-on.
- It follows calls across packages and prints the chain (`X -> Y -> Z`). → Powerful but a big scope jump; we deliberately don't in v1.
- It supports `//workflowcheck:ignore` comments (no space after `//`) and a config with overrides. → `//pathkit:ignore` and `//pathkit:branch` (D2).
- It says openly that it can't catch everything. → Same honesty rule as `LIMITATIONS.md`.

## Decisions Log

<!-- Append dated entries whenever a decision is approved or changed. Format: `## YYYY-MM-DD — <short title>` then what changed and why. -->

## 2026-09-28 — Research phase: initial proposals written

Researched the Go-specific design questions and wrote proposals D1–D10 above, marked PROPOSED at the time (approved later the same day, see next entry). Facts checked against published sources on 2026-09-28: `golang.org/x/tools/go/cfg` exposes `Block.Kind` and `Block.Stmt` and handles `break`/`continue`/labels/`switch`/`select` but not `panic` or `&&`/`||`; Temporal Go SDK v1.49.0 has `AwaitWithTimeout(ctx, timeout, cond) (ok bool, err error)`, `IsReplaying`, `NewContinueAsNewError`, `SetUpdateHandler`; `testsuite.TestWorkflowEnvironment` has `OnActivity`, `OnWorkflow`, `SignalWorkflow`, `RegisterDelayedCallback`, `QueryWorkflow`, `SetWorkerOptions` and timer/activity listeners, but no history or replay methods; both `golang.org/x/tools` and `go.temporal.io/sdk` declare `go 1.26.0`. **Not confirmed:** whether `ReceiveChannel.ReceiveWithTimeout` exists in the SDK (sources disagree) — check in M2 against the real SDK source before relying on it. No Go toolchain is installed on the development machine yet; M0 installs it. No code written.

## 2026-09-28 — D1–D10 and the M0–M9 order approved, with four amendments

The owner approved every recommendation in D1–D10 and the M0–M9 milestone order in `PLAN.md`, with four amendments now written into the decisions:

1. **Workflow detection (D3):** first parameter `workflow.Context`, last result `error`, and exported. The proposed "or passed to `RegisterWorkflow`" rule is dropped for v1: it would mean following registration calls that often live in another package (`main.go`, a worker package). The scope config (D8) covers the cases the simple rule gets wrong.
2. **`--fail-under` (D9):** checks path coverage only in v1. Branch coverage is displayed but not used for pass/fail.
3. **Minimum Go version (D7):** not taken on trust from research. In M0, read the real `go.mod` files of the exact `golang.org/x/tools` and `go.temporal.io/sdk` versions we pin, and set PathKit's minimum from them.
4. **`pathkit test` vs `go test` (D4):** it must be clearly documented that coverage is recorded only by `pathkit test` (or `go test` with the overlay flag from `pathkit prepare`). A plain `go test` records nothing, while tests still pass, so it is an easy mistake. It goes in `SETUP-GUIDE.md`, `pathkit test --help`, `LIMITATIONS.md`, and a direct "no trace files found" message from `coverage`/`report`.

Everything else in D1–D10 is approved as written. M0 has not started.

## 2026-09-28 — D8: `include` may add workflows the automatic rule missed, with two conditions

The owner approved letting `workflows.include` in `.pathkitrc.json` add a function that D3's automatic detection missed (for example an unexported workflow), since `RegisterWorkflow` detection is out of v1. Two conditions, now written into D8 and M5:

1. An `include` name that matches no function, or matches a function whose first parameter is not `workflow.Context`, is an error (exit 1), handled the same way as a misspelled workflow name. Reason: a wrong entry must never silently change the scope, or the coverage % stops meaning anything.
2. Workflows added this way are labelled `added by config` in `analyze` and `report` output, including `--json` and the HTML report. Reason: a reader can always tell which workflows were found automatically and which a person added.

