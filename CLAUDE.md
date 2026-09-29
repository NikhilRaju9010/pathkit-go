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
| `switch`, type switch | **Junction** (the TS version skipped `switch`; Go workflows use it a lot, e.g. on a signal's type). A missing `default` adds an implicit `default` exit. `fallthrough` handled by `go/cfg`: falling into the next case is not a new decision, so it adds no step (built in M4a). A `switch` with only a `default` has one outcome and is not a junction. | `case <expr>` / `default` |
| `workflow.Selector` + `Select(ctx)` | Junction at `Select`. Each `AddReceive`/`AddFuture`/`AddDefault` registered **on the same selector variable, in the same function, with an inline func literal** is one exit. The callback's body becomes that exit's road. | `signal "<name>"` (receive on a signal channel), `timeout` (future from `workflow.NewTimer`), `activity <name>` / `child <name>` (activity/child future), `default` |
| `workflow.NewTimer` in a Selector | Covered by the Selector rule → `timeout` label. The Go equivalent of TS `Promise.race` + `sleep`. | |
| `GetSignalChannel(...).Receive(ctx, &v)` | **Not** a junction — it just waits, only one thing can happen. | |
| `ReceiveAsync` / `ReceiveWithTimeout` used in an `if` (`ReceiveWithTimeout` confirmed in SDK v1.49.0, M2) | That `if` is a junction; labelled `received` / `not received`. | |
| `workflow.Await(ctx, cond)` | Not a junction (one outcome). | |
| `workflow.AwaitWithTimeout(ctx, d, cond)` | The `if ok` that follows is a junction, labelled `signaled` / `timeout` (the Go equivalent of TS `condition(fn, timeout)`). | |
| `for` loop / `for range` containing a Temporal call **or any junction** (amended 2026-09-28, see Decisions Log) | Loop junction with `iterate` / `exit`; the back-edge is `retry`. **Loop rule** (the one definition; everything else points here): *each loop edge appears at most once on a listed path; a trace with several trips is folded by keeping only the last trip.* **Analyzer half:** because `iterate`, `exit` and `retry` can each be used only once per path, a loop adds exactly three kinds of path: (a) *not entered*, meaning `exit` straight away; (b) *entered and left from inside the body*, meaning `iterate`, then a `return`/`break` inside the body; (c) *entered, went round, then left*, meaning `iterate`, the body, `retry`, then `exit` (after `retry` the only way on is `exit`, because `iterate` is used up). **Matcher half:** when a trace enters the same loop's body again (another `iterate` for that loop), the walker drops every step recorded since that loop's previous `iterate`, including the steps of any loops nested inside it, and carries on from the new `iterate`. Only the last trip through the body survives, followed by what came after the loop. So "went round twice, then returned from inside the body" folds into (b), and "went round three times, then left" folds into (c). The number of trips is never counted (same as TS). A `for {}` with no condition only leaves through `break`/`return`. | `iterate`, `exit`, `retry` |
| Loop with no Temporal call and no junction inside | Transparent (walked once), same as TS. | |
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

1. **One package builds the graph (`internal/model`).** It assigns every junction a stable ID (`J1`, `J2`, … in source order within the function) and every exit an edge ID, always `J<n>.<exit label>` (`J3.true`, `J5.case "approved"`, `J7.timeout`; rule confirmed 2026-09-28, see Decisions Log). `analyze`, the instrumenter, and the matcher all get IDs from this one package. The instrumenter never works out IDs on its own; it asks the model "which edge ID belongs to this exit of this AST node?". (This is the TS `outcomeEdgeIndex` idea, but as the only way in, not a later add-on.)
2. **The graph follows execution order.** Built on `go/cfg`, a junction appears in the graph exactly where it runs. The recorder calls `hit()` **at the top of each exit's road**, the moment the decision is made. Graph order and trace order are therefore the same thing by construction. (Go has no `try/catch`, so the TS bug's exact trigger, "try-success recorded at the end of the try block", doesn't exist here.)
3. **Matching walks the graph; it does not compare lists.** The matcher starts at `Start`, reads the trace one edge at a time, and follows that edge in the graph. If a step doesn't exist in the graph, the trace is reported as unmatched, with the exact step where it went off the map. The matcher half of the **loop rule** (defined once, in the D3 table's loop row: *each loop edge appears at most once on a listed path; a trace with several trips is folded by keeping only the last trip*) lives in this one walker.
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
- **Minimum Go version: 1.26.0 (verified in M0, see Decisions Log).** Originally: Research (2026-09-28) found that `golang.org/x/tools` and the Temporal Go SDK (v1.49.0) both declare `go 1.26.0`, which suggests 1.26. Owner's decision: this is **verified in M0** by reading the actual `go.mod` of the exact `x/tools` and `go.temporal.io/sdk` versions we pin (the SDK only matters for the test fixtures, but users need it too). PathKit's minimum is the highest `go` line among them, recorded in the Decisions Log.
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

- Entries name **workflows**, not files: `FuncName`, or `pkg.FuncName` / `pkg.(*Type).Method` when a name is ambiguous. (Also accepted: `Type.Method` and `pkg.Type.Method`. A short name that matches two workflows is an error, not a guess.)
- **The exact `include` rule (owner's clarification, M5, 2026-09-28): `include` means "only these count".** When `workflows.include` is given, the scope is exactly the workflows it lists. Every automatically found workflow that it leaves out is **excluded with the reason `not in include list`**, and listed like any other exclusion. So a workflow can only leave the scope visibly. A listed function the automatic rule missed is added (see below). An empty list (`"include": []`) is an error. With no `include`, every automatically found workflow is in scope. `exclude` then takes workflows out, each with its own required `reason`; `--exclude` adds exclusions with the reason `excluded by --exclude flag`. A name in both lists is an error.
- `include` can also name a function the automatic rule (D3) missed, e.g. an unexported workflow or one returning no `error`, as long as its first parameter is `workflow.Context`. This is how the scope config "covers the rest" now that `RegisterWorkflow` detection is out of v1. Two rules (approved 2026-09-28):
  - If an `include` name matches no function, or matches a function whose first parameter is not `workflow.Context`, that is an **error** (exit 1), the same as a misspelled workflow name. Example: `pathkit report: .pathkitrc.json: include "sendEmail" is not a workflow: its first parameter is not workflow.Context`.
  - Workflows that are in scope only because `include` added them are labelled **`added by config`** in `report` (text, `--json` and HTML) and in `analyze`, so it's always visible which workflows the automatic rule found and which a person added by hand.
- The same scope is applied by `analyze`, `coverage`, `report`, and `pathkit test` (only in-scope workflows are instrumented).
- The coverage % is computed **only over in-scope workflows**. `report` always prints a line like `2 workflows excluded by .pathkitrc.json (see "reason")`, so nobody can quietly raise coverage by excluding things.
- `analyze` shows a note `N workflows hidden by scope; pass --all to show them`. (Built in M5 as a list with every excluded workflow and its reason: `Excluded from scope (N), pass --all to show them:`. `--all` ignores the config and the flags, and prints a line saying the config file is ignored.)
- An `include`/`exclude` name that matches no workflow is an **error**, not a warning (TS warned). A misspelled include would otherwise silently shrink the scope and inflate the %.
- The config file is looked up in the current folder, then parent folders up to the folder containing `go.mod`. (TS only checked the current folder.) If there is no `go.mod` anywhere above, only the current folder is checked. `--config <file>` (added in M5) names the file instead. Relative paths inside the file are relative to the file's own folder.
- The `--include`/`--exclude` flags stay for one-off runs and now take workflow names.

### D9 — `--fail-under` and trace cleanup — APPROVED (2026-09-28)

- `--fail-under <percent>` on `coverage` and `report` (config key `failUnder`). Below the threshold: normal report printed, then `pathkit report: coverage 62.5% is below --fail-under 80%` on stderr, exit code 2. In v1 it checks **path coverage only** (owner's decision, 2026-09-28). Branch coverage is shown but not used for pass/fail.
- **Cleanup:** `pathkit test` starts each run by clearing old traces (pass `--keep-traces` to add to them instead). New `pathkit clean` command deletes `.pathkit/traces` (or `--older-than 7d`). `--clean` on `coverage`/`report` stays, for parity. Traces from a stale workflow version are skipped and counted in a warning.

### D10 — Output parity — APPROVED (2026-09-28)

Text output keeps the TS format line for line, including `Start -> <junction> --<label>--> ... -> End`. Junction descriptions use Go source text (`if input.AmountCents <= 0`, `switch status`, `select (Selector)`). The only intentional differences are: the end station shows its kind when known (`End (failed)`), and `report` adds the branch-coverage figure and the "excluded by scope" line. `--json` shapes are documented in `SETUP-GUIDE.md` and tested with golden files.

### D11 — Priority labels (High / Medium / Low) — APPROVED (2026-09-29)

`report` gives each **workflow** a priority label that says how urgently it needs more tests. It is shown in the text report, `--summary` and `--json` (M7b) and in the HTML report (M8). It is taken from the TypeScript version (`src/htmlReport.ts`, `priorityLabel`, and `test/htmlReport.test.ts`), where it appeared only in the HTML report.

- **The label belongs to the workflow, not to a path.** TypeScript never labelled individual paths. Every untested path of a workflow effectively carries its workflow's label. A per-path rule (for example "failure paths first") would be a new design and needs its own proposal.
- **The rule**, where `covered` and `total` are the workflow's covered and listed paths:
  - **High** when coverage is below 50%: `covered × 100 < 50 × total`.
  - **Medium** when it is from 50% up to and including 80%: `covered × 100 ≤ 80 × total`.
  - **Low** when it is above 80%, including 100%.
- **Exact whole-number comparison, never a rounded % (owner's requirement).** The boundaries are compared with the integer products above, never with a printed or rounded percentage. So 1/2 = 50% is Medium, 4/5 = 80% is Medium, 7999/10000 = 79.99% is Medium, 8001/10000 = 80.01% is Low, and 4999/10000 = 49.99% is High. Tests pin exactly these boundaries and the values just either side.
- **A workflow with 0 listed paths has no label.** This can only happen when every path ends in `panic`.
- `SETUP-GUIDE.md` says that labels are per workflow, and what each one means for a tester (owner's requirement).

### Trace file format (schemaVersion 1) — defined in M3, approved with the M3 plan

One JSON file per workflow run, written by the recorder that `pathkit test` adds to the package through the overlay. It lives in the trace folder (default `.pathkit/traces` in the folder `pathkit test` runs from, baked in as an absolute path) and is named `<workflow>.<12 random hex digits>.trace.json`. Example:

```json
{
  "schemaVersion": 1,
  "tool": "pathkit-go",
  "workflow": "orders.OrderWorkflow",
  "functionHash": "3f9a1c0b7e2d4a51",
  "status": "complete",
  "steps": ["J1.false", "J2.failure"],
  "workflowId": "default-test-workflow-id",
  "runId": "default-test-run-id",
  "recordedAt": "2026-09-28T10:15:00Z"
}
```

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Always `1` for this format. Any other value is reported as unsupported, never guessed at. |
| `tool` | Always `pathkit-go`. |
| `workflow` | The workflow's name as `analyze` prints it (`pkg.Func` or `pkg.Type.Method`). |
| `functionHash` | 16 hex digits: the start of a SHA-256 of the workflow function's code, printed by `go/printer` without comments (so formatting and comments don't change it). Computed only by `model.FunctionHash`. A different hash now means the function changed, and the trace is **stale**. |
| `status` | `complete` if the run **finished** a `return` of the workflow function, with all return values evaluated (see the M3 Decisions Log entry for why "reached" isn't enough). Otherwise `incomplete`: a panic (including one while computing a return value), the SDK stopping the run with `runtime.Goexit`, or a timeout. Only complete traces are matched. |
| `steps` | The exit IDs the run passed, in order, exactly as `model` produced them (`J1.false`, …). Every string written here came from `Graph.ExitFor(...).String()` at instrument time. |
| `workflowId`, `runId` | From `workflow.GetInfo`, for reference only. They are not used for matching. |
| `recordedAt` | UTC time the file was written (RFC 3339). |

What is deliberately **not** in the file: the Go test name (the workflow runs on its own goroutine and can't see it), the end kind (the matcher gets it from the graph), and the path number (display order can change; the path's identity is its steps plus end kind).

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

## 2026-09-28 — M0: skeleton built, Go minimum verified as 1.26.0

**Go installed:** Go 1.27.1 (latest stable), downloaded from go.dev, SHA-256 checked against go.dev's published checksum, unpacked to `~/.local/go` (no sudo). `~/.local/go/bin` and `~/go/bin` were added to `PATH` in `~/.bashrc`.

**Minimum Go version, checked from the real `go.mod` files** (downloaded with `go mod download -json <module>@latest` in a scratch folder, then reading each file's `go` line):

| Module | Version | `go` line |
| --- | --- | --- |
| `go.temporal.io/sdk` | v1.49.0 | 1.26.0 |
| `golang.org/x/tools` | v0.50.0 | 1.26.0 |
| `github.com/spf13/cobra` | v1.10.2 | 1.15 |
| `github.com/spf13/pflag` | v1.0.10 (cobra itself pins v1.0.9) | 1.12 |
| `honnef.co/go/tools` (staticcheck, CI only) | v0.8.1 | 1.26.0 |

The highest is 1.26.0, so PathKit's `go.mod` says `go 1.26.0`, with no `toolchain` line. This matches the research. Only cobra is in PathKit's `go.mod` so far. `x/tools` is added in M2 when code first uses it. The Temporal SDK is never added to PathKit's own `go.mod` (D7).

**What was built:**
- `cmd/pathkit/main.go` is the only place that calls `os.Exit`.
- `internal/cli` holds `Run(args, stdout, stderr) int` and the `analyze`/`coverage`/`report` placeholders, which do the real argument checks and then say `not implemented yet (planned for Mx)`.
- One error formatter produces `pathkit <command>: <message>`, or `pathkit: <message>` for an unknown command. Exit codes are named constants 0/1/2.
- `internal/version` gives `--version` in this order: the version stamped with `-ldflags` at build time, else Go's recorded build info, else `dev`.
- CI (`.github/workflows/ci.yml`) runs on Linux, macOS and Windows: gofmt check (skipped on Windows because of line endings), `go vet`, staticcheck v0.8.1 via `go run`, and `go test -race`.

**Choices made while building:**
- Cobra's own errors are rewritten to the one-line style. An unknown command drops cobra's ` for "pathkit"` suffix and its multi-line "Did you mean" block.
- `coverage` and `report` already register `--traces`. The plan said "no flags yet except `--summary`", but the "missing required --traces" check can't be tested without the flag existing.
- Cobra's automatic `completion` command is left on (the free shell completion from D7).

**Surprises:**
- (a) A `go build` inside a git checkout stamps a commit-based version, such as `v0.0.0-20260928070644-65ea0f53be27+dirty`, while `go run` gives `dev`. The plan only expected `dev`. This is documented in `SETUP-GUIDE.md` and `LIMITATIONS.md`.
- (b) `go test -race` needs cgo and a C compiler. The dev machine has none, so `-race` runs in CI only. The local check is `go test ./...`.

**Checks run:** `gofmt -l .` (clean), `go vet ./...` (clean), `go test -count=1 ./...` (3 packages ok, including a test that builds the real binary and checks its actual exit codes), staticcheck v0.8.1 (clean). CI has not run yet, because the repo is not pushed to GitHub.

## 2026-09-28 — M1: pilot project built, test-environment facts (D6) checked

**What was built.** `testdata/pilot/` is a separate Go module (`example.com/pilot`, `go 1.26.0`, `go.temporal.io/sdk` v1.49.0, `testify` v1.12.1). It has 8 workflow functions in 7 packages (orders, approval, polling, shipment, fulfillment with a saga parent and payment child, billing, reports), fake activities, 21 tests (covering 20 distinct paths; two polling tests land on the same path), and `cmd/worker/main.go` (registers everything and creates a Temporal Schedule with cron `0 6 * * *` for the daily report; compiled and vetted, never run). Go skips `testdata/`, so PathKit's own `go.mod` and `go test ./...` are untouched. CI now also runs `go vet`, staticcheck and `go test -race` inside `testdata/pilot` on all three OSes.

**The answer key.** `testdata/pilot/EXPECTED.md` was written by hand before PathKit can analyze anything. It lists 38 expected paths and which test covers which path: 20 covered, 52.6% expected project coverage. Later milestones are checked against it. A disagreement gets recorded in its "Disagreements" section, not silently "fixed" in the key.

**D6 facts, each checked by a real test:**

| Fact | Result | Where |
| --- | --- | --- |
| The fake clock skips long timers instantly | Confirmed. A 72h `NewTimer` and a 48h `AwaitWithTimeout` finish in milliseconds; tests assert under 10s. | shipment, approval |
| A non-retryable mock error fails the activity at once | Confirmed with `temporal.NewNonRetryableApplicationError` | orders, shipment, billing, reports |
| `QueryWorkflow` answers while the workflow is running | Confirmed from inside `RegisterDelayedCallback` | approval |
| `OnWorkflow` mocks a child workflow | Confirmed. The child's code doesn't run; the parent sees the mocked error. The child was registered with `env.RegisterWorkflow` first. | fulfillment |
| `SetLastCompletionResult` drives the cron "later run" branch | Confirmed. A `mock.MatchedBy` on `BuildReport`'s `since` argument proves the branch was taken. | reports |
| Continue-as-new comes back as a detectable error | Confirmed: `errors.As(err, &*workflow.ContinueAsNewError)` | billing |
| `AwaitWithTimeout` times out under the fake clock | Confirmed | approval |
| `workflow.Go` + `Receive` gets a signal sent with `SignalWorkflow` | Confirmed | approval |

Also learned: `env.OnActivity(fn, ...)` works without a separate `env.RegisterActivity(fn)`. Not checked in M1: whether the test environment ever replays workflow code (D6 says it doesn't; this matters for M3).

**Cases that M2 must handle, found while writing realistic code:**
- `if err := workflow.SetQueryHandler(...); err != nil` (approval) is a Temporal-package call that is *not* in D2's list, so it's transparent.
- `if err := workflow.GetLastCompletionResult(...); err == nil` (reports) is transparent. Its no-error side is the **true** side, so PathKit must walk into the `if` body, not skip it.
- Both are in `EXPECTED.md`.

**Checks run:** in `testdata/pilot`: `gofmt -l .` (clean), `go vet ./...` (clean), staticcheck v0.8.1 (clean), `go test -count=1 ./...` (7 packages ok). At the repo root: `go vet ./...` and `go test ./...` still green. `go test -race` runs in CI only (no C compiler locally, see M0).

## 2026-09-28 — M2: graph model and `analyze` (first slice)

**`ReceiveWithTimeout` exists (the open fact from research).** In `go.temporal.io/sdk` v1.49.0 the `ReceiveChannel` interface declares `ReceiveWithTimeout(ctx Context, timeout time.Duration, valuePtr any) (ok, more bool)`. It is in `internal/workflow.go` line 236, exported as `workflow.ReceiveChannel` in `workflow/deterministic_wrappers.go` line 18, and implemented in `internal/internal_workflow.go` line 859. It returns no `error`, so it never creates an err check. Its `ok` in an `if` is an M4 junction. The research sources that said it doesn't exist were wrong or out of date.

**What was built.**
- `internal/load` wraps `go/packages` (x/tools v0.50.0). It accepts a file, a folder or `folder/...`, and loads from the named folder so that folder's own `go.mod` applies.
- `internal/discover` implements the D3 rule. The `workflow.Context` check compares types with `go/types`, so aliases and renamed imports can't fool it.
- `internal/model` builds a `go/cfg` graph and shrinks it to junctions. It classifies every `if`, assigns every ID, lists paths, and rejects M4 constructs.
- `internal/render` prints text and Mermaid from model data only.
- `internal/cli/analyze.go` wires it all together, with `--summary`, `--limit`, `--mermaid` and `--out`.
- A new fixtures module, `testdata/fixtures`, holds one tiny workflow per rule (`rules/`) and a package with a type error (`broken/`).

**Single source of truth (D5), made concrete.**
- `model.EdgeID` has a hidden field, so only `model` can create IDs, and the compiler enforces it. Junctions are numbered `J1…` in source order, counting only real junctions. Exits are `J<n>.<label>`.
- `Graph.ExitFor(ifStmt, label)` and `Graph.LookupEdge("J2.failure")` are the only ways in, for M3's recorder and matcher.
- Each exit keeps the statement it leads into (`Road`), which is where M3 inserts its recording calls.
- `Path.Key()` is the exit IDs plus the end kind, and `Path.ID()` is a 10-hex-digit SHA-256 of that key.

**Exact rules implemented** (they refine D2/D3; all covered by `internal/model/rules_test.go`):
- **Temporal err check:** the condition is exactly `v != nil`, `v == nil`, `nil != v` or `nil == v`, with `v` an `error` variable. Its nearest assignment before the condition, by source position, in the function body (not inside closures, but including the `if`'s own init) must come from one of these:
  - `.Get` on a `Future` (so `ExecuteActivity`, `ExecuteLocalActivity`, `ExecuteChildWorkflow`, `NewTimer`, `SignalExternalWorkflow`, `RequestCancelExternalWorkflow`)
  - `workflow.Sleep`, `Await` or `AwaitWithTimeout`

  Exits are `failure` and `success`.
- **Transparent:** every other err check, including `SetQueryHandler` (M1 finding 1). PathKit walks only the no-error side, which is the true side for `== nil` (M1 finding 2; the fixture `TransparentEqNil` fails if this breaks, and that was checked by temporarily breaking the rule).
- **Plain `if`:** everything else, including compound conditions like `err != nil && x > 0`.
- **Pragmas:** `//pathkit:ignore` or `//pathkit:branch` on the `if`'s own line or the line above. An ignored plain `if` walks its false side.
- **End kinds:**
  - `nil` → completed
  - `workflow.NewContinueAsNewError` → continued-as-new
  - `fmt.Errorf` / `errors.New` / `temporal.New…Error` → failed
  - an error variable on the error side of its own nil check → failed
  - anything else → plain `End`
- **Dropped paths:** paths into `panic`, `os.Exit` or `log.Fatal*` are removed.
- **Unsupported until M4:** any `switch`, `select`, loop, label or `goto`, `Selector.Select`, the result of `AwaitWithTimeout` / `ReceiveWithTimeout` / `ReceiveAsync` in an `if`, or a `defer` that calls into the Temporal SDK. The workflow is skipped with `skipping <workflow>: <construct> at <file>:<line> is supported from M4`. Closures (func literals) are not looked into in M2.

**Answer key.** `internal/model/pilot_test.go` reads `testdata/pilot/EXPECTED.md` as-is and parses all 8 sections (it also checks each "**K paths:**" count against its lines).
- The 3 M2 workflows match exactly: `orders.OrderWorkflow` 3/3 paths, `fulfillment.PaymentWorkflow` 4/4, `reports.DailyReportWorkflow` 6/6.
- The 5 M4 workflows are skipped, each naming its construct: approval → AwaitWithTimeout result, polling and billing → for loop, shipment → `workflow.Selector`, fulfillment → defer.
- There were no disagreements, and `EXPECTED.md` was not edited.

**Choices made while building:**
- Error messages that match the setup guide's capitalized wording ("Workflow file not found", "Directory not found") carry a `//lint:ignore ST1005` comment for staticcheck.
- If any package in a `./...` load doesn't compile, the whole `analyze` fails (`package does not compile: ...`). Whether `report` (M7) should skip broken packages instead is left for M7.
- Path display order is a fixed depth-first walk (`true` before `false`, `failure` before `success`). So the printed numbers can differ from `EXPECTED.md`'s numbering; the comparison is by set.

**Checks run:**
- `gofmt -l .`: clean
- `go vet ./...`: clean
- staticcheck v0.8.1: clean
- `go test -count=1 ./...`: all ok
- `go vet` on `testdata/fixtures/rules`: clean
- the manual `analyze` commands from the plan, on the built binary: output as expected

## 2026-09-28 — M3: trace recording (spike) for the three M2 workflows

**The three risky assumptions, each proven by a real run:**
- **R1: overlay can replace a file and add a new one. Confirmed.** Spike in a throwaway module: `go test -overlay` compiled the replaced file and a brand-new file (a function defined only there was callable). Nothing was written to the module and `go.mod` didn't change. Without the overlay, the same test failed to build, which proves the overlay was really used. Now permanent: `internal/instrument` `TestOverlayJSON` checks the recorder never exists on disk, and `internal/e2e` `TestAnswerKey` hashes every file under `testdata/pilot` before and after and requires them identical.
- **R2: panics point at the real file and line. Confirmed.** In the spike, the stack trace named the original path (`.../mod/p/p.go:7`), not the overlay copy. Now permanent: `testdata/fixtures/panics` panics inside a `return` line that also gets inserted text, below other edited lines, and its test (run through `pathkit test` by `TestFixturesUnderOverlay`) requires the stack trace to name `panics/panics.go:<that line>` and never `.pathkit`.
- **R3: the test environment never replays. Confirmed by a real test.** In `testdata/fixtures/replay`, a workflow with an activity, a 1-hour timer, a signal wait and a second activity runs 3 times. Each time its function starts exactly once and `workflow.IsReplaying` is never true. Under `pathkit test`, the 3 runs give exactly 3 complete traces with identical steps. (The SDK source agrees: `internal/internal_workflow_testsuite.go` line 2673, `// this test environment never replay`.) The design doesn't rely on this. Each call of a workflow function has its own recorder, so a replayed run records its whole trip into a new trace. An abandoned call is stopped by the SDK with `runtime.Goexit` (`internal/internal_workflow.go` line 1198), never finishes a `return`, and is saved as `incomplete`.
- **R4: recorded order = listed order.** This holds by construction: the recorder punches at the start of each `Exit.Road` the model chose. `TestEveryExitRecordedOnce` checks every exit has exactly one `hit`, and every `hit` ID round-trips through `LookupEdge`, for all M2 fixture and pilot workflows. The end-to-end test confirms it on real runs.

**Answer key.** `internal/e2e` `TestAnswerKey` runs `pathkit test <pilot package> -- -run '^TestName$'` for each of the 8 M2 tests listed in `EXPECTED.md`. Each produced exactly one complete trace, and `Match` landed it on exactly the path `EXPECTED.md` names:
- orders: 1, 2, 3
- payment: 2, 4
- daily report: 3, 6, 4

There were no disagreements, and `EXPECTED.md` was not edited. The shared answer-key reader now lives in `internal/expected`, used by both the M2 and M3 tests.

**What was built:**
- `internal/instrument`: line-preserving text insertions (every edit is checked for line breaks), one generated recorder file per package, and `overlay.json`.
- `internal/trace`: read, list, clear (deletes only `*.trace.json`), and check against the graph.
- `model.FunctionHash`: the printed function (no doc comment), turned into Go tokens with comments dropped, then SHA-256, first 16 hex digits.
- `model.Match`: walks the graph step by step. It has five mismatch messages, each naming the step.
- `pathkit test`, with `--traces`, `--keep-traces`, and `--` to pass flags to go test.
- `pathkit traces`, a debug view.
- The trace format is written above, under "Trace file format (schemaVersion 1)".

**Two design corrections found while building. Both were real bugs in the plan, both fixed, both pinned by tests:**
1. **"Mark before return" was the TS `d0aa17a` trap again.** The approved plan put `pathkitRec.returned();` before each `return`. The panic fixture showed this records a run as `complete` when the *return value itself* panics (`return explode(x), nil`), claiming a path the run never finished. The fix, still insertion-only and on the same line:
   - `return a, b` becomes `return pathkitRet2[T0, T1](pathkitRec, a, b)`. The generated helper sets the flag only after `a` and `b` are evaluated. The explicit type arguments (the source text of the function's result types) keep untyped values such as `nil` working.
   - `return f()`, where `f` returns several values, becomes an inline `func() (T0, T1) { r0, r1 := f(); mark; return r0, r1 }()`.
   - A bare `return` is still marked just before it, because it has nothing to evaluate.
   - Workflow functions with more than 8 results can't be recorded; that fails with a clear error. `TestFixturesUnderOverlay` now requires the panicking run's trace to be `incomplete`.
2. **Go's test cache made a repeat run record nothing.** A second identical `pathkit test` printed `(cached)` and wrote 0 traces, because the tests didn't actually run. Go's docs say any `-count` flag disables the test cache, so `pathkit test` adds `-count=1` unless the user passed their own `-count`. `TestTraceClearingAndRepeatRuns` runs the same command twice and requires a fresh trace each time.

**Choices made while building:**
- Generated names are `pathkitRec`, `pathkitStart`, `pathkitRecorder`, `pathkitTrace`, `pathkitTraceDir` and `pathkitRet1…8`. A clash with user code is a clear error, tested with the `testdata/fixtures/clash*` fixtures.
- `.pathkit/overlay` is emptied and rewritten on every `pathkit test` run, because it belongs to pathkit.
- The recorder writes files from workflow code. That's fine in tests: the file is small, and the SDK's deadlock detector (1 s) never triggered.
- `pathkit test` prints its summary line on stderr, so it doesn't mix into `go test`'s stdout.
- `pathkit prepare` is deferred (noted for M6).

**Checks run:**
- `gofmt -l .`: clean
- `go vet ./...`: clean
- staticcheck v0.8.1: clean
- `go test -count=1 ./...`: all ok, including `internal/e2e` (about 20 s)
- pilot: `go vet` and `go test` ok
- fixtures (except the deliberately broken package): `go vet` ok

## 2026-09-28 — M4 plan approved; two decision changes (D3 loops, D5 edge IDs)

The owner approved the M4 plan: four slices (M4a switch + groundwork, M4b loops, M4c Selector + wait results, M4d saga note + finish), each committed separately, with a stop after each one. Two approved decisions changed, both now written into D3 and D5 above:

1. **D3, loops: a loop is a junction if its body contains a Temporal call *or any junction*.** A junction here means an `if`, `switch` or selector that counts. D3 used to say "a loop with no Temporal call is transparent (walked once)". That breaks on a loop like `for _, item := range items { if item.Priority { ... } }`: each trip records the `if` again, and with no loop markers the matcher can't fold the trips, so every such run would show as `unmatched`. Loops with neither a Temporal call nor a junction stay transparent, as before. **Effect on the answer key: none.** The pilot has exactly two loops, `polling.go:21` and `subscription.go:20`. Both call `ExecuteActivity` and `workflow.Sleep`, so they are junctions under both the old and the new rule. No other pilot workflow has a loop. So none of the 38 paths in `EXPECTED.md` can change. M4b adds a test that pins this.
2. **D5, edge IDs keep the one M2 rule: `J<n>.<exit label>`.** D5 sketched `J5.case:"approved"` (with a colon). The ID is now exactly the junction ID, a dot, and the label that is printed: `J3.case "complete"`, `J2.signal "delivery-update"`, `J1.iterate`. Reasons:
   - it is one rule with no translation step;
   - it is exactly how `EXPECTED.md` writes steps;
   - a trace file can be read against `analyze` output directly.

   In a trace file the quotes are escaped as JSON requires (`"J3.case \"complete\""`). Two exits of one junction that would get the same label are told apart with ` #2`, ` #3` (for example two identical `case x > 0` lines).

## 2026-09-28 — M4a: `switch`, groundwork for M4, strict answer-key reader

**What was built:**
- **`switch` and type switch are junctions** (`internal/model/switch.go`):
  - one exit per `case` in source order, labelled with the source text (`case "complete"`, `case "a", "b"`, `case *types.Basic`, `case nil`);
  - `default`, written or added when missing;
  - junction labels `switch status`, `switch` (no tag), `switch v.(type)`.

  go/cfg turns a switch into a chain of tests. The model follows that chain from the first test to find each case's code block and the "no case matched" block. `break` inside a `switch` leaves the switch. A `switch` with only a `default` has one outcome and is not a junction.
- **The model is no longer `if`-only.** `Junction.Stmt` and `ExitFor` take any statement, and `JunctionKind` has a new `Switch` value. IDs still come only from `model`.
- **Recording:**
  - `pathkitRec.hit(...)` is inserted right after each `case ...:` colon;
  - a missing default becomes `; default: pathkitRec.hit("J2.default")`, inserted just before the switch's `}`;
  - `fallthrough` gets `pathkitRec.fell();` in front of it, and the case it falls into records with `hitUnlessFell`.

  All of this is inserted on existing lines, so line numbers still don't move.
- **Skip messages.** `goto` and Go's own `select` statement will never be supported, so their message says why and promises nothing:
  - `goto at x.go:12 is not supported: PathKit maps break, continue and return, but not goto`
  - `... Temporal workflows must use workflow.Selector instead of Go's select`

  The constructs still planned (loops, labels, Selector, wait results, saga defer) keep "is supported from M4" until their slice.
- `pathkit test` and `pathkit traces` now say `1 trace`, not `1 traces`.

**`fallthrough` design.** The graph shows the chosen case only. Falling into the next case continues along the same road, the way go/cfg lowers it. The recorder must therefore not record the next case's hit when it is reached by falling through; the `fell` flag skips exactly one such hit. The live test `TestRouteHugeFallsThrough` checks the trace is exactly `J1.success`, `J2.case "huge"`, `J3.false`.

**Strict answer-key reader** (owner's condition). `internal/expected` was rewritten so it never skips anything. The old reader, checked while planning, **silently dropped two things**:
- the `retry` step in `retry --> J1` (polling path 5);
- the 4 polling tests, which are written as bullet lines under the `**Tests**` line.

Neither caused a wrong result yet, because polling was still skipped. Now every path line must be read completely:
- every `J<n> ... --label-->` step;
- `retry --> J<n>`, read as `J<n>.retry`;
- the `End (kind)`;
- an optional `[compensation (defer)]`;
- nothing else except a remark starting with `,` or `.` that contains no step or note.

Every test mention, on the `**Tests**` line or a bullet, must have the form `` `TestName` → N ``. A `-->` or a test mention anywhere else is an error. The reader also cross-checks:
- path numbering;
- `**K paths:**` against the list;
- `X/Y covered` against the tests;
- `Not covered:` against the rest;
- the `Found (8)` list, the Totals rows and the Total row (38 paths, 20 covered, 52.6%) against the sections.

Every error names the file and line. The only free text is the "Disagreements" section, which is for people, not data.
- `TestReadRealKey` reads the real key: 8 workflows, 38 paths, 20 covered, 21 tests. Polling path 5 is `J1.iterate J2.success J3.default J1.retry J1.exit|completed`, and fulfillment's notes are `false, true, true, true`.
- `TestRejectsWhatItCannotRead` has 19 broken inputs, each required to fail at its line.
- The pilot test now also compares the compensation note: a note in the key that the model doesn't produce counts as a disagreement.

`EXPECTED.md` was not edited.

**Tests added:**
- `TestRules`: 11 switch fixtures in `testdata/fixtures/rules/switches.go`. They cover a written default in the middle, a tagless switch with an init, a type switch with an assignment and `nil`, fallthrough, only-default, `break` in a case, a switch after an activity, duplicate case text (`#2`), and a qualified type case.
- `TestJunctionLabels`: 4 labels.
- `TestUnsupported`: renamed from `TestUnsupportedUntilM4`, now also checks the never-supported wording.
- `TestLiveFixtures` (`internal/e2e`): runs each of the 8 tests in the new live fixture package `testdata/fixtures/switches` alone under `pathkit test`, and requires its one trace to land on the stated path. That package has `RouteWorkflow` (expression switch, `fallthrough`, added default) and `LedgerWorkflow` (type switch with a written default).
- `TestTraceCountWording`.
- The consistency test `TestEveryExitRecordedOnce` and the compile and line-number tests now also cover the switch fixtures.

**Answer key after M4a:**
- **Match (3):** `orders.OrderWorkflow`, `fulfillment.PaymentWorkflow`, `reports.DailyReportWorkflow`. That is 13 of 38 paths, and 8 of 21 tests land on their path.
- **Still skipped (5):**
  - polling and billing, for the `for` loop (M4b; polling's `switch` is supported now, but the loop around it comes first);
  - shipment, for `workflow.Selector` (M4c; the `switch` inside its callback comes with it);
  - approval, for the `AwaitWithTimeout` result (M4c);
  - order fulfillment, for the saga `defer` (M4d).

**Found while building.** The dev machine has a git-ignored `testdata/pilot/.pathkit/` from an M3 hand check. `gofmt -l .` lists the overlay copies inside it (generated code isn't gofmt-formatted). It was left in place (working rule 5). The verification commands now run gofmt on tracked files only: `gofmt -l $(git ls-files '*.go')`.

**Checks run:**
- `gofmt` on all tracked and new Go files: clean
- `go vet ./...`: clean
- staticcheck v0.8.1 (root and pilot): clean
- `go test -count=1 ./...`: all packages ok
- fixtures `go vet` (every package except `broken`): clean
- pilot `go vet` and `go test`: ok

## 2026-09-28 — M4b: loops and the loop rule

**What was built:**
- **Loops are junctions** (`internal/model/loop.go`): `for init; cond; post`, `for cond`, `for {}` and `range`, with `break`, `continue`, labels, `continue outer` and `break outer`. The rule is the amended D3 one: a loop is a junction if it contains a Temporal call (any call into the Temporal SDK, including in closures inside it) or any junction (an `if` that counts under D2, or a `switch` with a case). Otherwise it is transparent and walked once.
- **Labels and exits.** A loop junction is labelled `for <cond>`, `for` or `for range <x>`. Its exits are `iterate` and `exit` (a `for {}` has no `exit`), plus a separate `Retry` edge (`J1.retry`). Retry is not in `Exits`, because it is never chosen at the loop's head: the end of the body, or a `continue`, leads to it.
- **Model.** go/cfg's blocks give each loop's head (where `continue` and the back-edge land), body and done block. Reaching the head again from inside the body becomes the new `Target.Retry`. The graph now has a cycle (retry leads back to the loop), and `Paths()` and `Match()` handle it.
- **Analyzer half of the loop rule** (`Paths()`): each exit and each retry edge is used at most once per path. So a loop adds exactly D3's paths (a), (b) and (c); a road that would need a second retry isn't listed. `LoopReasons(wf)` reports, for every loop, whether it has a Temporal call and whether it has a junction; tests use it.
- **Matcher half** (`Match()`): a `retry` step must come exactly where the graph goes round. A repeated `iterate` of a loop the trace is already in drops every step since that loop's previous `iterate`, including nested loops' steps, and carries on. **Bug found and fixed while building:** the first version accepted a lone `J1.retry` at the loop's head as if it could be chosen there. `TestMatchLoopMismatches` now requires `step 1 "J1.retry" does not fit`.
- **Recording** (`internal/instrument`):
  - just before the loop statement (before its label, if any), `pathkitRec.enter("J1", "J1.iterate", "J1.exit", "J1.retry");` hands over the model's IDs;
  - for `for init; cond; post` and `for cond`, the condition is wrapped as `pathkitRec.loop("J1", <cond>)`, which records `retry` when the body already ran, then `iterate` or `exit`;
  - `for {}` and `range` get `pathkitRec.loop("J1", true);` at the top of the body;
  - `range` also gets `; pathkitRec.rangeDone("J1")` after its `}` (the range ran out: `retry`, `exit`), and `pathkitRec.broke("J1");` before each `break` that leaves it, so a break records no `exit`. The breaks are found with Go's rules: an unlabelled `break` belongs to the innermost for, range, switch or select; a labelled one to its label.
  - `enter` also resets an inner loop each time the outer one goes round.

  Everything is still inserted on existing lines. The generated file adds the type `pathkitLoop`, now a reserved name.
- **Output:** a retry prints as `retry -->` (exactly how `EXPECTED.md` writes it). Mermaid draws it as `-->|default, then retry|` back to the loop.
- **Skip messages:** loops and labels are no longer skipped. `goto` stays never-supported.

**Tests added:**
- `TestPilotLoopRuleChangeIsNeutral`, the owner's condition: the pilot has exactly two loops (`subscription.go:20`, `polling.go:21`), and both contain a Temporal call. So the old rule and the amended rule classify them the same way. Together with the path-by-path comparison, this pins that the rule change moves none of the 38 paths.
- `TestRules`: 10 loop fixtures (`testdata/fixtures/rules/loops.go`):
  - 4 transparent loops, including one whose only `if` is a transparent error check and one whose `if` is `//pathkit:ignore`d;
  - a loop with an activity;
  - a loop with only a plain `if` (the new rule);
  - `for {}` with `break`;
  - `continue`;
  - nested loops;
  - labelled `continue outer` / `break outer`.
- `TestMatchFoldsLoopTrips`: 13 hand-written traces:
  - zero trips;
  - several trips ending in a return (b);
  - one and many trips then leaving (c);
  - trips with different choices;
  - `for {}`;
  - three nested cases (the inner loop going round twice on each of three outer trips; a failure on a later trip; the inner loop not entered on the last trip);
  - labelled `continue` and `break`;
  - the two pilot polling traces.

  Each folded result must also be one of the listed paths.
- `TestMatchLoopMismatches`: 4 new mismatch messages.
- `TestLiveFixtures`: 15 real runs in the new `testdata/fixtures/loops` package:
  - `NestedWorkflow` (3 outer × 2 inner trips; a failure on the 3rd outer trip; the inner loop empty; none);
  - `ScanWorkflow` (`range` with `continue` and `break`);
  - `WaitWorkflow` (`for {}` + `Sleep` + `break`);
  - `GridWorkflow` (`continue outer`, `break outer`);
  - `SumWorkflow` (a transparent loop over 3 items records nothing for the loop);
  - `CountBigWorkflow` (no Temporal call but an `if` inside: folds under the new rule).
- `TestIDsRoundTrip` now checks switch, loop and retry IDs. The consistency test counts the IDs handed over in `enter(...)` as recording sites.

**Answer key after M4b:**
- **Match (5):**
  - `orders.OrderWorkflow`, `fulfillment.PaymentWorkflow`, `reports.DailyReportWorkflow`;
  - `polling.ReportPollingWorkflow`: 5/5 paths, with the `retry` step exactly as the key writes it;
  - `billing.SubscriptionWorkflow`: 3/3 paths, `End (continued-as-new)` proven end to end.

  That is 21 of 38 paths. 14 of 21 tests land on their `EXPECTED.md` path, including `TestPollingPendingThenComplete` (two trips, folded onto path 3) and `TestPollingGivesUp` (two trips, folded onto path 5).
- **Still skipped (3):**
  - approval: the `AwaitWithTimeout` result, M4c;
  - shipment: `workflow.Selector`, M4c;
  - order fulfillment: the saga `defer`, M4d.
- No disagreements, and `EXPECTED.md` was not edited.

**Two notes the owner asked to record** (in `LIMITATIONS.md` and `SETUP-GUIDE.md`):
- `gofmt -l .` lists the generated files in `.pathkit/`, which Go's own tools otherwise ignore; add `.pathkit/` to `.gitignore`.
- The `default` exit PathKit adds to a `switch` with no `default` can be impossible when the cases already cover every possible value. That path is then never covered and lowers coverage. A possible future fix is an override comment on the `switch` (for example `//pathkit:exhaustive`). It is **not built**.

**Checks run:**
- `gofmt` on all tracked and new Go files: clean
- `go vet ./...`: clean
- staticcheck v0.8.1 (root and pilot): clean
- `go test -count=1 ./...`: all packages ok
- fixtures `go vet` (every package except `broken`): clean
- pilot `go vet` and `go test`: ok

## 2026-09-28 — M4c: `workflow.Selector`, timeouts and wait results

**What was built:**
- **Selector junction** (`internal/model/selector.go`) at `sel.Select(ctx)`, labelled `select (Selector)`. There is one exit per `Add…` call, in source order:
  - `signal "<name>"`: `AddReceive` on `workflow.GetSignalChannel(ctx, "<name>")` or `GetSignalChannelWithOptions`, directly or through a variable; a string constant is printed as its value;
  - `timeout`: `AddFuture` on a `workflow.NewTimer` or `NewTimerWithOptions` future;
  - `activity <Name>`, `local activity <Name>`, `child <Name>`: `AddFuture` on those futures;
  - `default`: `AddDefault`;
  - otherwise, as written: `receive <expr>`, `send <expr>`, `future <expr>`.

  Repeated labels get ` #2`, and chained `Add…` calls in one statement are fine.
- **Selector roads.** Each exit's road is its callback's body, walked with its own go/cfg graph. A `return` in the callback, or its end, goes on to the code right after `Select`. For that, the model's roads can now start in the middle of a block (`roadFrom`).
- **Selector numbering.** A selector is numbered at its **first `Add…` call** (the junction's new `order`), so it comes before any junction inside its callbacks. That's why shipment's selector is J2 and the `switch` in its callback is J3, as `EXPECTED.md` says.
- **Where `err` came from.** `nearestAssign` now looks in the innermost function first (for example a callback), then outward. An `if err != nil` inside a callback on an `err` set there is a Temporal error check when the `err` comes from `f.Get` (fixture `ErrCheckInCallback`, live `LookupWorkflow`).
- **The one shape PathKit maps** (never guessed at). Anything else is skipped with `workflow.Selector at <file>:<line> is not supported: <reason>`, and each case has a fixture in `testdata/fixtures/rules/selectors_unsupported.go`, 11 in all:
  - The selector is created exactly once, with `workflow.NewSelector` or `NewNamedSelector`, in this function. A parameter, a helper's return value, or a second assignment is rejected.
  - Every `Add…` call is a plain statement in the same block as the creation, before the `Select`. An `Add…` in an `if`, a loop or a closure, or after `Select`, is rejected, because then the exits would depend on the path.
  - Every callback is an inline func literal.
  - There is exactly one `Select` call. It may sit inside a loop (fixture `SelectInLoop`, live `CollectWorkflow`).
  - The selector variable is used for nothing else: passing it to a function, storing it or capturing it is rejected.
  - A selector with no `Add…` calls, or one not kept in a variable, is rejected.

  The owner asked for precise reasons, and the wording was fixed before the M4c commit:
  - A selector made by a helper gets the same reason as one passed in as a parameter: `must be created in this function, …`. `must be created exactly once` is kept for a second assignment only.
  - An `Add…` in the wrong place names what it is inside: `an if`, `a loop`, `a switch`, `a select`, `a function literal`, or `a nested block`.
- **Wait results.** An `if` whose condition is exactly `ok` or `!ok` is a `WaitResult` junction when `ok` is the first result of `AwaitWithTimeout` (exits `signaled` / `timeout`), `ReceiveWithTimeout` or `ReceiveAsyncWithMoreFlag`, or the inline form `c.ReceiveAsync(&v)` / `!c.ReceiveAsync(&v)` (exits `received` / `not received`). Its label is, for example, `if !ok (AwaitWithTimeout)`. A compound condition such as `ok && x > 0` is an ordinary `true`/`false` junction: less descriptive, never wrong. `//pathkit:ignore` works as on any `if`.
- **Recording** needed no new kind of insertion: a selector exit records at the top of its callback, and a wait result records like any `if`. The skip messages for Selector and the wait results are gone; only the saga `defer` still says "supported from M4".

**Tests added:**
- `TestRules`: 7 selector fixtures and 6 wait fixtures, with every label kind, a return inside a callback, an error check inside a callback, `Select` in a loop, chained adds, a compound condition and a pragma.
- `TestUnsupported`: the 11 unsupported selector shapes, each checked word for word.
- `TestJunctionLabels`: 4 more labels.
- `TestLiveFixtures`: 12 real runs in the new `testdata/fixtures/waits` package, **both sides of every race**:
  - Selector: signal wins and timer wins;
  - `AddDefault`: a signal waiting and nothing waiting;
  - an activity future winning, with its error check in the callback;
  - `Select` in a loop over 3 rounds (signal, nothing, signal), folded;
  - `AwaitWithTimeout`: arrived and timed out;
  - `ReceiveWithTimeout`: arrived and timed out;
  - `ReceiveAsync`: found and not found.
- The CLI "every workflow skipped" test now uses `selectors_unsupported.go`, which stays skipped forever, instead of the approval pilot, which is now supported.

**Not covered by a live test:** a timer beating an *activity* in a selector. In the Temporal test environment, the fake clock doesn't jump forward while an activity is running, so the activity always wins unless it is mocked with a delay (`OnActivity(...).After(...)`), which needs testify's `mock` in the fixtures module. Timer-wins is covered live by `RaceWorkflow` (a timer against a signal), and the activity/timer selector shape is covered by analysis fixtures.

**Answer key after M4c:**
- **Match (7):** orders, payment, daily report, polling, billing, and now:
  - `shipment.ShipmentWorkflow`: 9/9 paths, with the selector J2 and the `switch` J3 inside its callback;
  - `approval.ApprovalWorkflow`: 4/4 paths.

  That is 34 of 38 paths, and 19 of 21 tests land on their `EXPECTED.md` path, including `TestShipmentDelivered` → 3, `TestShipmentTimerFiresLost` → 9 and `TestApprovalTimesOut` → 2.
- **Still skipped (1):** `fulfillment.OrderFulfillmentWorkflow`, for the saga `defer` (M4d).
- No disagreements, and `EXPECTED.md` was not edited.

**Planned for M4d (owner's requirement, written into `PLAN.md`).** Once all 8 pilot workflows are supported, a skipped pilot workflow must make `TestPilotMatchesExpected` and the e2e `TestAnswerKey` **fail**:
- both take their workflow and test lists from `EXPECTED.md` itself;
- a workflow that doesn't build fails immediately, naming the construct;
- the checked-test count must equal the key's own count (21);
- the "must build" check is itself tested with a fixture that is always skipped.

## 2026-09-28 — M4d: saga compensation note, child label, final answer-key check

**What was built:**
- **Saga compensation is a note, never a branch** (D3). A `defer` is compensation when the deferred call starts an activity, local activity or child workflow (`ExecuteActivity`, `ExecuteLocalActivity`, `ExecuteChildWorkflow`), directly or inside a deferred func literal (`isCompensation`). No other defer counts:
  - `defer cancel()`, a deferred log call, or a deferred call to the user's own helper function;
  - a `defer` inside a selector callback, which runs when the callback ends, not the workflow.
- **How the note is attached.** A road that passes a compensation defer carries `Target.Compensation`. A path gets `Path.Compensation` when any road on it does. One helper (`Graph.compensationOn`) works it out from the steps for both `Paths()` and `Match()`, so the listing and the matcher always agree, including after the loop rule folds a trace. The note is not part of `Path.Key()`: the steps decide it.
- **Output.** The note prints as `End (failed) [compensation (defer)]` in `analyze` and in `pathkit traces`. Mermaid output doesn't show it.
- **Recording** needed nothing new. The deferred compensation runs after the `return`'s values are evaluated, so the run is already `complete`, and the recorder's own `flush` defer, registered first, runs last.
- **Child workflow label, end to end:** `PaymentWorkflow (child workflow)`, pinned by `TestPilotChildWorkflowLabel` and by the fulfillment tests landing on their paths.
- **No more planned skips.** `UnsupportedError` always carries a reason and prints `… is not supported: <reason>`. The "is supported from M4" wording is gone from the code, tests and user docs. It survives only in earlier Decisions Log entries, which record history and were not rewritten.

**The answer-key tests can no longer pass while a workflow is skipped** (owner's requirement, planned in M4c):
- `TestPilotMatchesExpected` takes its workflows from `EXPECTED.md`. The discovered pilot workflows must equal that list, and every workflow must build through the new `expected.Build`: not found, or skipped as unsupported, is a failure naming the construct. The paths must match with their notes, and 38 paths must match in all.
- The e2e `TestAnswerKey` builds every key workflow the same way before running anything, fails at once if one doesn't build, and requires the checked-test count to equal the key's own count (`expected.TestCount`, which must be 21).
- `TestBuildRejectsSkippedAndMissing` proves the rule itself: the always-skipped fixture `rules.UsesLabel` (it uses `goto`) and a missing name must both come back as errors.
- The hand-written pilot lists (`mappedWorkflows`, `m4Workflows`) are gone. `TestIDsRoundTrip` and `TestMatchEveryListedPath` also take the key's list.

**Timer beats an activity, run for real.** `OnActivity(...).After(...)` needs testify's `mock.Anything`. testify v1.10.0 was already required by the fixtures module (indirectly, with its full checksum in `go.sum`), so no new module was needed. An offline `go mod tidy` (`GOPROXY=off`) only moved it from indirect to direct, and left `go.sum` byte-for-byte unchanged. `TestLookupTimerWins` mocks the activity to take 2 hours against a 1-hour timer, and lands on `J1.timeout`.

**Tests added:**
- `TestRules`: 7 saga fixtures (`testdata/fixtures/rules/saga.go`):
  - compensation in a deferred func;
  - a plain defer;
  - a two-step saga where the note appears only after the defer;
  - a deferred activity call;
  - `defer cancel()` and a deferred log call, with no note;
  - a defer in a selector callback, with no note;
  - a defer in a loop, with the note only on the path that reaches it.
- `TestLiveFixtures`: 3 real runs of the new `testdata/fixtures/saga` `BookTripWorkflow`, all landing on their path, and 1 more wait run (`TestLookupTimerWins`):
  - reservation fails: no note, nothing compensated;
  - payment fails: the note, and the compensation really ran once;
  - success: the note, and the compensation did not run. The note is not a branch.
- `TestAnalyzeWholePilot` (renamed): all 8 printed, stderr empty.
- `TestBuildRejectsSkippedAndMissing` and `TestPilotChildWorkflowLabel`.

**Final answer key:**
- all **8 of 8** workflows mapped;
- **38 of 38** paths match (steps, end kinds and the 3 compensation notes);
- **21 of 21** tests land on their `EXPECTED.md` path;
- **20** paths covered, **52.6%**, exactly as the M1 key predicted.

There were no disagreements in all of M4, and `EXPECTED.md` was never edited.

## 2026-09-28 — M4 summary

M4 was built in four slices, each committed separately, with a stop for the owner after each one:
- **M4a:** `switch` and type switch (with `fallthrough`), and a strict answer-key reader that fails on anything it can't read. It found that the old reader was silently dropping `retry` steps and bullet-listed tests.
- **M4b:** loops and the loop rule, both halves: listing and folding. The owner widened D3: a loop is a junction if it contains a Temporal call *or any junction*; this was pinned to change none of the 38 paths.
- **M4c:** `workflow.Selector`, in one safe shape. 11 unsafe shapes are refused with a precise reason.
- **M4c:** `AwaitWithTimeout` / `ReceiveWithTimeout` / `ReceiveAsync` results.
- **M4d:** the saga note, the child label, and the final check.

Two decisions changed along the way, both approved by the owner:
- the D3 loop rule above;
- edge IDs keep the single `J<n>.<label>` rule, replacing D5's `case:"x"` sketch.

**Evidence:**
- every pilot workflow is mapped and recorded, and the whole answer key matches;
- 39 live fixture runs cover each construct under `pathkit test`, including both sides of every race;
- the consistency test proves every model exit has its recording site in every instrumented copy.

**What M4 does not do** (all in `LIMITATIONS.md`):
- concurrency (`workflow.Go`) is not modelled;
- trips round a loop are not counted;
- junctions are treated as independent;
- an added `default` can be impossible (a possible `//pathkit:exhaustive` is noted, not built);
- `goto`, Go's `select` and unsafe selector shapes are refused.

## 2026-09-28 — M5: workflow-level scope config (`.pathkitrc.json`)

**Decisions (owner, 2026-09-28; D8 updated above):**
1. **`include` means "only these count".** Every automatically found workflow left out is excluded with the reason `not in include list`. An empty `include` is an error.
2. **An in-scope workflow PathKit can't analyze is an error unless excluded.** `analyze`, `pathkit test` and `pathkit traces` keep working and always print `in scope but not analyzable: <workflow>: <reason> (fix it, or exclude it in .pathkitrc.json with a reason)`. From M6/M7, `coverage` and `report` must stop with exit 1 while one is in scope, so the % is always over exactly the in-scope set. M5 builds the shared list (`scoped.notAnalyzable`); M6/M7 make it fatal.
3. **New `--config <file>` flag** on `analyze`, `test` and `traces` (later `coverage`/`report`), with no search when given. It is a new flag name, not in the TS list.
4. **`--exclude` reason is `excluded by --exclude flag`**, and **`--all` prints `--all: <config path> is ignored; showing every workflow`**.
5. **`failUnder`, `html`, `out`, `json`, `noColor` and `allowStale` are validated but ignored until M6–M8.** No command applies or mentions them. `TestLaterKeysAreNotApplied` checks that a config setting all six changes no output and writes no file.

**Other choices made while building:**
- **Names are checked against the config's `packages`** (default `./...` from the config's folder), loaded once per command, so a typo is caught even when `analyze` gets a single file. With no config, the `--include`/`--exclude` flags are checked against what the command loaded.
- **`packages` and `traces` already work.** `packages` is the folder `pathkit test` and `pathkit traces` use when none is given; with more than one entry, pass the folder as an argument. `traces` is the default trace folder. Both are read relative to the config file.
- **The file is read strictly:**
  - unknown keys (`"workflow"`) are refused;
  - wrong types say what was expected in plain words (`expected a number, found text`), with the line;
  - JSON syntax errors give the line and the column of the bad character;
  - an exclude must be `{"name", "reason"}` with a non-empty reason.

  Every error starts with the file's absolute path.
- **Scope changes what is counted, never what is tested.** `pathkit test` still runs an excluded workflow's tests; it just doesn't record them (`not recording <wf>: excluded from scope (<reason>)`).
- **`pathkit traces`** lists a trace from an excluded workflow as `excluded from scope (<reason>)`, a new trace kind, instead of `unknown workflow`.
- **One code path.** `internal/scope` reads the file and resolves the names; the CLI's `loadScoped` applies the result. `analyze`, `test` and `traces` all go through `loadScoped`, which replaced M3's `loadRecordable`. `discover.All` lists every top-level function, so `include` can find the ones the automatic rule misses.
- **Wording change:** a workflow PathKit can't map now shows as `in scope but not analyzable: …` instead of `skipping …`.

**Tests added:**
- `internal/scope`: `TestFind` (search, the stop at `go.mod`, a config above the module ignored, the no-`go.mod` case), `TestLoad`, `TestReadValid`, `TestReadErrors` (14 cases), `TestScopeRules` (8 cases plus 4 name forms), `TestScopeErrors` (9 exact messages, including D8's `sendEmail` one and an ambiguous `Run`), `TestAbs`.
- `internal/cli`: no config file (all 8 workflows, 38 paths, no scope output); the no-shipment config (7 workflows, 29 paths, the exact excluded list) plus `--all`; the upward search; `--exclude`/`--include`; `added by config`; config errors, each with the file path; not-analyzable, and then excluded with a reason; `TestLaterKeysAreNotApplied`; an excluded trace.
- `internal/e2e`:
  - `TestScopeExcludesShipment`: real `analyze` gives 29 paths, and one real `pathkit test` of the whole pilot through `--config` gives 17 distinct covered paths = **58.6%**. Shipment's tests ran but weren't recorded, and the numbers agree with `EXPECTED.md` (38 − 9, 20 − 3).
  - `TestAddedByConfigRecorded`: the unexported `scope.lowerFlow`, added only by `include`, is recorded and matched.
- **New fixtures:**
  - `testdata/fixtures/scope`: `VisibleFlow`, `lowerFlow` (with a real test), `NoErrorFlow`, `sendEmail`, the method `Svc.Handle`, and `a.Run`/`b.Run`;
  - config folders `testdata/scopes/no-shipment` and `testdata/scopes/added`.

  `EXPECTED.md` was not edited.

## 2026-09-29 — M6: `pathkit coverage`, `--fail-under`, `pathkit clean`, `pathkit prepare`

**Owner's decisions (M6 plan, 2026-09-29):**
1. **`--traces` defaults for `coverage`** (the flag, else the config's `"traces"`, else `.pathkit/traces`), like `pathkit test` and `pathkit traces`. The M0 "missing required --traces" check is gone for `coverage`; `report` keeps it until M7.
2. **Traces that don't count warn but never change the exit code** (as in TS). Each gets a stderr warning (`pathkit coverage: warning: …`) naming the file and the reason, and the `Traces:` line counts every kind.
3. **`pathkit prepare` is built in M6** (deferred from M3).
4. **`--clean` deletes traces only when the report was fully produced** (exit 0 or 2), never on exit 1. Tested both ways, including an `--out` write that fails *after* the report printed: exit 1, and the traces are kept.
5. **When one decimal makes the value look equal to the `--fail-under` threshold, more decimals are printed** until the difference shows: `coverage 66.67% is below --fail-under 66.7%` (`render.Pct`).
6. **Branch coverage uses raw steps.** It counts every exit the run took, before loop folding, so a branch can show as taken even when no covered path uses it. This is written in `LIMITATIONS.md` and `SETUP-GUIDE.md`.

**What was built:**
- **`internal/coverage`**, a pure calculation. It classifies every trace with the existing `trace.Check`:
  - matched: counted;
  - unmatched: a warning with the exact step, from `Graph.Match`;
  - stale: a warning, not counted;
  - incomplete, unknown workflow, unreadable: warnings;
  - excluded: counted in the summary;
  - other (`--function` picked another workflow): counted in the summary.

  A path is covered when at least one counted trace lands on it; repeats count once. **`--allow-stale`** (or config `allowStale`) matches a stale trace against the current map. If it fits, it counts and the path is marked `(stale trace)`; if not, it's unmatched. The **branch** total is the exits on the listed paths, `retry` edges included.
- **`pathkit coverage <file|folder|folder/...>`**, in the TS format (`Workflow:`, `Total paths:`, `Covered: 2/3 (66.7%)`, `Branches:`, "Covered paths" / "Untested paths" with `analyze`'s numbers). It adds TS `report`'s total line (`38 paths total · 20 covered · 18 missed · 52.6% coverage`), M5's excluded list and the `Traces:` line.
  - It uses `loadScoped`, so the scope is identical to `analyze` and `test`. It refuses with exit 1 while a workflow in scope isn't analyzable (the M5 decision).
  - Flags: `--function`, `--json`, `--out`, `--fail-under`, `--allow-stale`, `--clean`, and the scope flags.
  - **`--fail-under`** compares the exact value (52.63 passes 52.6). The flag beats the config's `failUnder`. Below it, the report is printed, then the stderr line, then exit 2.
- **`--json` (schemaVersion 1)** is documented in `SETUP-GUIDE.md` and pinned by the golden file `internal/cli/testdata/coverage_orders.json`. `->` is kept readable (no HTML escaping).
- **`pathkit clean [--traces] [--older-than 30m|12h|7d] [--config]`** deletes only `*.trace.json` files. It prints `deleted N trace files from <dir>`, or `nothing to clean in <dir>`.
- **`pathkit prepare [folder]`** writes the same overlay as `pathkit test` (the overlay writing is now a shared `writeOverlay`) and prints exactly one line on stdout, the `go test -overlay=… -count=1 <pattern>` command. Where to run it, and the "plain go test records nothing" note, go to stderr.
- **Did-you-mean:** `scope.Suggest` finds the closest name by edit distance (at most 3 edits and a third of the length; case is ignored when measuring). It's added to every "matches no …" error: config `include`/`exclude`, `--include`/`--exclude`, and `--function` through the new `scope.MatchName`.

**Tests added:**
- **`internal/coverage`:** orders fully covered (a repeat counted once); `TestBranchesUseRawSteps` (polling, pending then complete: 1 path covered, 5 of 8 branches); `TestTraceKinds` (7 kinds, 5 exact warnings); `TestAllowStale`; `TestOthers`; `TestPercent`.
- **`internal/render`:** `TestPct`, 7 cases including 52.59 vs 52.6 → `52.59%`.
- **`internal/scope`:** `TestSuggest` (6 cases), and the hint in 4 exact errors plus one far-off name with no hint.
- **`internal/cli`:** the text output checked in full, `--out`, the JSON golden, `--fail-under` (5 cases), the config `failUnder` and the flag override, `--clean` at exit 0, exit 2 and two exit-1 cases, errors (no traces, not analyzable, `--function` typo with a hint), `--function`, warnings, and `pathkit clean`.
- **`cmd/pathkit` `TestBinaryCoverageExitCodes`:** 11 cases run against the **real built program**, checking exit codes 0, 1 and 2 and the stderr lines.
- **`internal/e2e`:**
  - `TestCoverageMatchesKey`: one real `pathkit test` of the pilot, then `coverage --json`. That gives **20/38 = 52.6%**, and **17/29 = 58.6%** with the no-shipment scope. For every workflow, **the set of covered paths equals the set `EXPECTED.md`'s tests point to**, notes included. The text total line is checked too.
  - `TestPrepare`: the printed command, run with `exec` (not through PathKit), records 3 traces, and a plain `go test` records none.

`EXPECTED.md` was not edited.

## 2026-09-29 — M7 plan approved: two slices, D11, broken packages, fast local tests

The owner approved the M7 plan with every recommendation, plus two additions to D11:

1. **Priority labels (D11, new):** per workflow, as in TypeScript: under 50% High, 50–80% (both ends included) Medium, over 80% Low. The comparison uses exact whole-number arithmetic, never a rounded %, and the boundaries 50% and 80% and the values just either side are tested exactly.
2. `SETUP-GUIDE.md` says labels are per workflow, and what each one means for a tester.

The request had asked how each *untested path* gets its label. TypeScript never labelled paths, only workflows, so the rule was written per workflow. A per-path rule would be a separate proposal.

**The M2 open question is decided: a package that doesn't compile stops `report` (exit 1); it is never skipped.** Skipping would hide that package's workflows and silently shrink the set the % is computed over, which is what D8 and the M5 "not analyzable is fatal" rule guard against. Every broken package is named, each with its first real error.

**Other approved choices for M7b:**
- `report` defaults `--traces` like `coverage`, and `report [folder|folder/...]` defaults to all of the config's `packages`, else `./...`. A single `.go` file is refused.
- The config keys `out`, `json` and `noColor` apply to `report`; a flag always beats the config.
- Colour is used only on a real terminal, with no new dependency.
- The excluded line is always printed, even for 0.
- `report --json` is the `coverage --json` shape plus `priority`, `file` and `excludedBy`.
- `--clean` follows the same rule as `coverage`.

**Order:** M7a (shared code path, loader message, fast local tests), stop for the owner to verify and commit, then M7b (`report`).

## 2026-09-29 — M7a: one shared code path, every broken package named, fast local tests

**What was built:**
- **One shared code path for the numbers.** `internal/cli/measure.go` holds `measure` and `finish`:
  - `measure` parses `--fail-under`, loads and scopes through `loadScoped`, refuses while an in-scope workflow isn't analyzable, applies the config's `failUnder` and `allowStale`, picks `--function`, reads the traces and calls `coverage.Compute`. Its warnings are printed as `pathkit <command>: warning: …`.
  - `finish` prints, writes `--out`, runs `--clean` (only once the output is written) and does the `--fail-under` check (exit 2).

  `coverage` is now its flags, `measure`, its rendering and `finish`. M7b's `report` will call the same two functions, so it cannot compute its numbers differently. **Coverage's output is unchanged:** its text tests, the JSON golden file `internal/cli/testdata/coverage_orders.json` and the real-binary exit-code test pass untouched.
- **Every broken package is named** (`internal/load`, `compileErrors`). One broken package prints `package does not compile: <package>: <file>:<line>:<col>: <error>`. Several print `N packages do not compile: <package>: <error>; <package>: <error>`, sorted, on one line. A package that only imports a broken one has no errors of its own (checked with a probe) and is not listed. The position is relative to the current folder when the file is inside it, else absolute.
- **Fast local tests.** Every e2e test and both real-binary tests already skipped under `-short`, from earlier milestones. So until now a `-short` run had **no** end-to-end check at all. Now:
  - `TestCoverageMatchesKey` runs under `-short` too: it records the whole pilot once and compares every workflow's covered paths with `EXPECTED.md`, both unscoped and with the no-shipment scope.
  - Every skip message ends with `(CI runs it)`.
  - The new `TestCIRunsEverything` fails if any `go test` line in `.github/workflows/ci.yml` contains `-short`. This was checked by temporarily adding `-short` to CI's two `go test` lines: the test failed and named lines 41 and 55. The file was then restored from git.

**Bug found and fixed.** Since M2, a broken package was reported as `package does not compile: -: # example.com/fixtures/broken`. `go/packages` puts go list's two-line build summary first, and PathKit's one-line error rule cut off its second line, which is the one with the real error. `firstError` now prefers a type or syntax error with a position. `TestAnalyzeErrors` now requires the full real error (`…/broken/broken.go:8:14: cannot use "not a number" …`), and `TestEveryBrokenPackageIsNamed` pins both wordings.

**Timings (this machine, warm build cache):**
- `go test -count=1 ./...`: **71 s**. Of this, `TestLiveFixtures` takes about 35 s and `TestAnswerKey` about 24 s.
- `go test -short -count=1 ./...`: **19 s**.

**Checks run:**
- `gofmt` on all tracked and new Go files: clean
- `go vet ./...`: clean
- staticcheck v0.8.1: clean
- `go test -count=1 ./...` (full): all ok
- `go test -short -count=1 ./...`: all ok

`EXPECTED.md` was not edited.

## 2026-09-29 — M7b: `pathkit report`

**What was built:**
- **`pathkit report [folder|folder/...]`** (`internal/cli/report.go`). It gets its numbers from the same `measure` and `finish` as `coverage` (M7a), so the two cannot disagree. The text layout follows TypeScript's report format with `analyze`'s path numbers added:
  - `<workflow> (<file>)`
  - `2/3 paths · 66.7% · branches 3/4 · priority Medium`
  - each path as `  N. <path>: covered|missed`
  - `N paths total · C covered · M missed · P% project coverage`, then `Branches:`
  - the excluded line (always printed) with each workflow's reason, and the `Traces:` line.

  `--summary` prints one aligned line per workflow, then the same footer.
- **D11 priority labels:** `coverage.Priority(covered, total)` uses only whole-number comparisons (`covered*100 < 50*total` → High, `covered*100 <= 80*total` → Medium, else Low; 0 paths → no label). `render.WorkflowPct` shows extra decimals when one decimal would put a workflow on a boundary it isn't on (`79.99%`, `80.01%`).
- **`report --json` (schemaVersion 1)** is `coverage --json` plus `workflows[].file`, `workflows[].priority` (null with no paths) and `excludedBy` (for example `[".pathkitrc.json", "--exclude"]`). The coverage fields come first and are built by the same `coverageJSON`. Pinned by the golden file `internal/cli/testdata/report_orders.json`.
- **Config keys `out`, `json` and `noColor` apply to `report`.** A flag always beats the config, including `--json=false`, and `out` is read relative to the config file. Only `html` is still unused (M8).
- **Colour** (`render.ColorEnabled`, `render.paint`): only on a real terminal (stdout is a character device), never with `--no-color`, config `noColor` or a non-empty `NO_COLOR`. It is never used in `--out` (which always gets the plain text) or in `--json`. There is no new dependency.
- **Folders:** with no folder, `report` loads every entry of the config's `packages` (`loadAll`; each package once). A single `.go` file is refused with a hint to use `coverage`. That check lives in `measure` (`foldersOnly`) after `--fail-under` is parsed, so `report` checks its flags in the same order as `coverage`.
- The M0 placeholder (`requireTraces`, `notImplemented`) is gone, and `--traces` defaults as for `coverage`.

**Choices made while building (not in the plan):**
- The excluded line's wording:
  - `0 workflows excluded (no scope in use)`
  - `0 workflows excluded by .pathkitrc.json`
  - `N workflow(s) excluded by <sources> (see "reason"):` followed by the list.

  The sources are the config file's own name, `--include` and `--exclude`. `--all` gives "no scope in use".
- `--summary` together with `--json` prints `--summary ignored because --json was passed.` on stderr (like analyze's `--limit`/`--summary` note).
- `report` has `--all` (ignore the scope), like `analyze`. It shows `0 workflows excluded (no scope in use)`, and stderr says the config is ignored.

**Tests added:**
- **`TestCoverageMatchesKey` (e2e, also runs with `-short`)** now also checks that `report` and `coverage` agree **field by field** on the recorded pilot, unscoped and with the no-shipment scope. Both `--json` outputs are decoded, report's three extra fields are removed, and the rest must be deep-equal: 8 top-level fields and every workflow, path, count and trace count. Each workflow's priority is checked against D11 from `EXPECTED.md`'s numbers, and the text totals are checked (52.6% and 58.6% project coverage). **Proven able to fail:** with `report`'s JSON temporarily changed to leave out one trace count, both subtests failed. The change was then reverted.
- `internal/coverage` `TestPriority`: 0%, 49.99%, 50%, 50.01%, 66.7%, 79.99%, 80% (4/5 and 8/10), 80.01%, 83.3%, 100%, 1/3, and 0 paths.
- `internal/render`: `TestWorkflowPct` (the boundary decimals), `TestColorEnabled`, `TestReportColor` (colour is decoration only: stripping the codes gives exactly the plain text), `TestExcludedLine`.
- `internal/cli`:
  - `TestReportText` (full text and `--summary`, checked in full, plus `--out`);
  - `TestReportSummaryOneLinePerWorkflow` (all 8 pilot workflows);
  - `TestReportJSONGolden`, `TestReportJSONIsCoverageJSONPlusThree`;
  - `TestReportExcludedLine` (6 cases);
  - `TestReportAddedByConfig`, `TestReportFailUnder`, `TestReportConfigKeys`;
  - `TestReportClean` (exit 0, 2, 1);
  - `TestReportErrors` (a file, two folders, not analyzable, a broken package, no traces, a missing folder);
  - `TestReportEveryConfiguredPackage` (overlapping entries count once).

  The M0 placeholder cases in `cli_test.go` were replaced.
- **`cmd/pathkit` `TestBinaryReportExitCodes`:** 14 cases against the real built program. Exit 0: plain, `--summary`, `--json`, above the threshold, the flag beating the config. Exit 2: below `--fail-under`, as text and as JSON, and config `failUnder`; the report is still printed first. Exit 1: a bad threshold, no traces, a single file, not analyzable, a broken package, an unknown flag.

**Pilot results (the real program):**
- `pathkit report ./...`: **38 paths, 20 covered, 18 missed, 52.6%**, branches 39/50. Shipment is High (33.3%), orders Low (100%), the other six Medium.
- With the no-shipment config: **29 / 17 / 58.6%**, with `1 workflow excluded by .pathkitrc.json (see "reason"):  shipment.ShipmentWorkflow: needs a real carrier sandbox`.

**Checks run:**
- `gofmt` on all tracked and new Go files: clean
- `go vet ./...` and pilot `go vet`: clean
- staticcheck v0.8.1: clean
- `go test -count=1 ./...`: 119 top-level tests (207 subtests) pass, none skipped, in 79 s
- `go test -short -count=1 ./...`: 109 pass, 10 skipped, in 26 s (M7a: 19 s; the new report tests in `internal/cli` load the pilot several times)

`EXPECTED.md` was not edited.
