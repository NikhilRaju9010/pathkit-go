# PathKit for Go: design

These are the design decisions PathKit is built on. A change to one of them is a design change: describe the problem and the options, get the maintainer's agreement, and update this file.

### D1 — Code parsing: `golang.org/x/tools/go/packages` + `go/types` + `golang.org/x/tools/go/cfg`

| Option | What it is (plain English) | Pros | Cons |
| --- | --- | --- | --- |
| `go/ast` only | Reads the text of one file into a tree. Knows *shapes*, not *meanings*. | Fast. Works on a single file, even if the project doesn't compile. No extra dependencies. | Can't be sure `wf.ExecuteActivity` is Temporal's function (import aliases, dot-imports, wrapper types). Can't tell what type a variable is. Activity detection would be a "guess from the text". |
| `go/types` | Adds meaning: "this `x` is a `workflow.Future`", "this call is `go.temporal.io/sdk/workflow.ExecuteActivity`". | Exact identification of Temporal calls, the `workflow.Context` parameter, `error` variables. No guessing. | Needs *all* files of the package and its imports; the code must compile. You need a loader to feed it. |
| `go/packages` | The standard loader. Runs `go list` under the hood and hands you files + type info for a package, the same way `go build` sees it. | Handles modules, build tags, vendoring, and **overlays** (see D4). The same loader `gopls` and `go vet` tools use. | The user's project must compile and its modules must be downloaded. A Go toolchain must be installed where PathKit runs. Slower than raw parsing (a few seconds on a big package). |
| `go/cfg` (extra) | Builds a "control-flow graph" of one function: which block of code can run after which. | Already correctly handles `break`, `continue`, labels, `goto`, `switch`, `select`, `return` — the exact things that are easy to get wrong one bug at a time (unlabeled `break`, labeled `break`, `continue`, `goto`). | Low-level (plain blocks, no idea what Temporal is). We still have to shrink it down to "only the junctions that matter". Does not model `panic` or `&&`/`||` short-circuits. |

**Choice:** load one package with `go/packages` (type info on), identify Temporal constructs with `go/types`, and build our junction graph on top of `go/cfg`'s block graph rather than a hand-written statement walker. Everyday analogy: `go/ast` is reading a street map by the shapes of the roads; `go/types` adds the street names; `go/cfg` adds the one-way signs. We want all three.

Cost we accept: PathKit only works on code that compiles. That is fine — the user is running `go test` on it anyway.

### D2 — Error branches without a path explosion

Go workflows are full of `if err != nil { return err }`. If every one counted as a junction, a workflow with 12 of them would have 4,096 paths, and most of those junctions are boring (a JSON decode that can't really fail).

**Choice — a "Temporal error check" rule:** an `if err != nil` (or `if err == nil`, or `if errors.As/Is(err, ...)`) counts as a junction **only when that `err` was last assigned by a Temporal call**:
`ExecuteActivity(...).Get`, `ExecuteLocalActivity(...).Get`, `ExecuteChildWorkflow(...).Get`, `SignalExternalWorkflow(...).Get`, `RequestCancelExternalWorkflow(...).Get`, `workflow.Sleep`, `workflow.Await*`, a timer future's `.Get`, and `Selector`-less `Future.Get` on any of those futures. `go/types` lets us follow the `err` variable back to where it was set (inside the same function).

Every other `if err != nil` is **transparent**: PathKit assumes the "no error" side and does not create a junction.

Its outcomes are labelled `failure` / `success`.

Two escape hatches:
- `//pathkit:ignore` comment on an `if` → never a junction (e.g. a defensive check you can't test).
- `//pathkit:branch` comment on an `if` → always a junction (e.g. a non-Temporal error you do want tracked).

Plain `if` statements that are not error checks (`if order.Amount <= 0`) are always junctions.

**Error checks on a helper's result (milestone M4).** `err := charge(ctx, o)` where `charge` is a function of the same package that calls the Temporal SDK (D15): the check after it is a junction too. If the helper fails exactly when modelled Temporal calls fail (every error it returns is the error of such a call, or nil) the junction is labelled with those calls and the helper (`Charge (activity), via charge`); if its errors are not followed (a function value's error, a wrapped error), it is labelled `charge (helper)` and the error is *unknown*, no longer "no error"; a helper that never returns an error leaves the check transparent. When the helper's code is followed, the check is a real step of the path but has only the exit the helper's `return` allows (`return nil`: `success`; `return fmt.Errorf(...)`, or a variable an error check just found non-nil: `failure`); the other exit is not listed. A helper that never touches the Temporal API, however many `if`s it has, is ordinary code: its error check stays transparent.

Second defence against explosion: `report`/`coverage` also print **branch coverage** (how many junction *exits* were taken at least once) next to path coverage. Branch coverage grows linearly, so it stays meaningful even when a workflow has thousands of paths. `maxPaths` is 2000. A workflow that reaches it gets a loud warning at the top of the text report, a banner in the HTML page and a `pathCapWarning` field in the JSON: its path list and classification are incomplete, paths beyond the cap are not listed (so never called stale), and the numbers for it are a lower bound.

### D3 — Which Temporal Go constructs become junctions

| Construct | v1 decision | Outcome labels |
| --- | --- | --- |
| `if` / `else`, `else if` | Junction (except transparent error checks, D2) | `true` / `false` |
| `switch`, type switch | **Junction** (Go workflows use it a lot, e.g. on a signal's type). A missing `default` adds an implicit `default` exit. `fallthrough` handled by `go/cfg`: falling into the next case is not a new decision, so it adds no step. A `switch` with only a `default` has one outcome and is not a junction. | `case <expr>` / `default` |
| `workflow.Selector` + `Select(ctx)` | Junction at `Select`. Each `AddReceive`/`AddFuture`/`AddDefault` registered **on the same selector variable, in the same function, with an inline func literal** is one exit. The callback's body becomes that exit's road. | `signal "<name>"` (receive on a signal channel), `timeout` (future from `workflow.NewTimer`), `activity <name>` / `child <name>` (activity/child future), `default` |
| `workflow.NewTimer` in a Selector | Covered by the Selector rule → `timeout` label. | |
| `GetSignalChannel(...).Receive(ctx, &v)` | **Not** a junction — it just waits, only one thing can happen. | |
| `ReceiveAsync` / `ReceiveWithTimeout` used in an `if` (`ReceiveWithTimeout` exists in SDK v1.49.0) | That `if` is a junction; labelled `received` / `not received`. | |
| `workflow.Await(ctx, cond)` | Not a junction (one outcome). | |
| `workflow.AwaitWithTimeout(ctx, d, cond)` | The `if ok` that follows is a junction, labelled `signaled` / `timeout`. | |
| `for` loop / `for range` containing a Temporal call **or any junction** | Loop junction with `iterate` / `exit`; the back-edge is `retry`. **Loop rule** (the one definition; everything else points here): *each loop edge appears at most once on a listed path; a trace with several trips is folded by keeping only the last trip.* **Analyzer half:** because `iterate`, `exit` and `retry` can each be used only once per path, a loop adds exactly three kinds of path: (a) *not entered*, meaning `exit` straight away; (b) *entered and left from inside the body*, meaning `iterate`, then a `return`/`break` inside the body; (c) *entered, went round, then left*, meaning `iterate`, the body, `retry`, then `exit` (after `retry` the only way on is `exit`, because `iterate` is used up). **Matcher half:** when a trace enters the same loop's body again (another `iterate` for that loop), the walker drops every step recorded since that loop's previous `iterate`, including the steps of any loops nested inside it, and carries on from the new `iterate`. Only the last trip through the body survives, followed by what came after the loop. So "went round twice, then returned from inside the body" folds into (b), and "went round three times, then left" folds into (c). The number of trips is never counted. A `for {}` with no condition only leaves through `break`/`return`. | `iterate`, `exit`, `retry` |
| Loop with no Temporal call and no junction inside | Transparent (walked once). | |
| `ExecuteActivity` error handling | Via D2. | `success` / `failure` |
| `ExecuteChildWorkflow` | Error check via D2. The child's own code is **not** followed (it's another workflow with its own map). Shown as a labelled step. | |
| `defer` compensation (saga) | **Recognized and labelled, not a junction in v1.** See reasoning below. | |
| `workflow.NewContinueAsNewError` | Not a junction, but a **different end station**. | End kinds: `End (completed)`, `End (failed)`, `End (continued-as-new)` |
| `workflow.Go` (coroutines), futures awaited together | Not junctions in v1. | |
| Query / Update / Signal handlers set with `SetQueryHandler` / `SetUpdateHandler` / signal callbacks | Not followed in v1 (separate entry points). | |

**Why saga `defer` is not a junction in v1:** a saga usually looks like
`defer func() { if err != nil { compensate() } }()`. Whether that inner `if` fires depends on *how the function ended*: it fires on failure exits and never on success exits. If PathKit multiplied every exit by "compensated / not compensated", it would invent paths that can never happen ("order succeeded **and** compensation ran"). Those would show up as "missed" forever, and coverage would be wrong — a real problem for any tool that multiplies exits this way. v1 shows a `compensation (defer)` note on the paths instead; modelling it properly is a candidate for v1.1.

**Why different end stations matter:** a single `End` for every ending would hide the difference. In Go we can see `return nil`, `return err`, and `return workflow.NewContinueAsNewError(...)` directly, so the output can say which kind of end each path reaches.

**How a workflow function is recognized:** a top-level function or method that meets **all three** rules: its **first parameter's type is `go.temporal.io/sdk/workflow.Context`** (checked with `go/types`, not by name), its **last result is `error`**, and it is **exported** (name starts with a capital letter). Unexported helpers that take `workflow.Context` are not workflows. v1 does **not** look at `RegisterWorkflow` calls; the scope config (D8) handles anything this rule gets wrong.

### D4 — Trace recording: instrument in memory, swap in with `go test -overlay`

The problem: when a test runs a workflow, something must write down which exits it took at each junction, in order.

| Option | How it works | Pros | Cons |
| --- | --- | --- | --- |
| **(a) Instrumented copy + `go test -overlay`** | PathKit writes an instrumented version of each workflow file into `.pathkit/overlay/` and an `overlay.json` that says "when compiling `orders.go`, use this file instead". `go test -overlay=...` compiles the copy. Your real files are never touched. | Sees every junction, including plain `if`s. No copies left next to your source. **Your existing tests work unchanged** — no setup code to add to them. Every workflow in scope is instrumented at once. Standard Go feature (since Go 1.16), also understood by `go/packages`. | Tests must be run through `pathkit test` (or with the `-overlay` flag added by hand). Line numbers in stack traces would shift — fixed with `//line` directives so errors still point at your real file and line. |
| (b) Query handler | Instrumented code keeps the trace in memory; the test calls `env.QueryWorkflow("__pathkit_trace")`. | `QueryWorkflow` works synchronously in the Go test env. | Still needs (a)'s instrumentation to *collect* the trace — the query is only a way to fetch it. Adds boilerplate to every test. Adds nothing over (a). |
| (c) Read the event history | After the test, look at which activities/timers/signals happened. | No instrumentation at all. | The Go test environment does not expose the history. And history only shows Temporal commands, not plain `if`s — `if order.Amount <= 0` leaves no trace in history. Can't tell paths apart. |
| (d1) Go's built-in coverage (`go test -coverprofile`) | Go already counts which code blocks ran. | Zero work from us. | Counts are added up across the whole test run: you learn "this block ran at some point", never "*this* run took *this* path". Gives branch coverage, not path coverage. |
| (d2) Interceptors | Temporal lets you wrap activity/timer/child calls. | Official extension point. | Only sees Temporal calls, not plain junctions. |
| (d3) Replay real histories | Run histories exported from staging/production through `worker.WorkflowReplayer` with the instrumented code. | Answers "which paths does *real traffic* take?" | Not part of v1; it answers a different question. |

**Choice: (a).** How it fits together (everyday version: we hand the compiler a marked-up photocopy and it never knows the difference):

1. `pathkit test ./...` loads the packages, finds workflows in scope, and builds the junction graph (D5).
2. For each workflow file, it writes an instrumented copy into `.pathkit/overlay/`. It also **adds one extra file** to the workflow's package through the overlay (`zz_pathkit_recorder.go`) that contains the tiny recorder. Because the recorder is generated into your package, **your `go.mod` never needs a PathKit dependency**. The recorder only uses the Go standard library and the Temporal `workflow` package you already have.
3. Each instrumented workflow function starts with `pk := pathkitStart(ctx, "<workflow ID>")` and `defer pk.flush()`. At each junction exit it calls `pk.hit("<edge ID>")`. The recorder is a **local variable** of that one function call — not a global map — so parallel tests and repeated runs can't mix their traces. (The Go test env gives every execution the same default run ID, so keying a global map by run ID would collide.)
4. When the workflow function returns, `flush()` writes one JSON trace file to the trace folder. If the function exits abnormally (panic, or the SDK stopping it), the trace is marked `incomplete` and ignored by reports.
5. `pathkit test` runs `go test -overlay=.pathkit/overlay/overlay.json <your packages> <your flags>`, and passes the **absolute** trace folder in an environment variable. (`go test` runs each package in its own folder, so a relative path would scatter traces.)
6. If you prefer to run `go test` yourself: `pathkit prepare ./...` writes the overlay and prints the flag to add.

**Coverage needs `pathkit test`, not plain `go test`.** A plain `go test` compiles your original, unmarked workflow files, so it records **no traces**, and `coverage`/`report` will show 0% for runs made that way. Your tests still pass or fail normally, so the missing traces are easy to miss. Only `pathkit test` (or `go test` with the `-overlay` flag printed by `pathkit prepare`) records coverage. `SETUP-GUIDE.md` (at the top of the coverage section, and in the errors table), `pathkit test --help` and `LIMITATIONS.md` all say this. When `coverage`/`report` find no trace files at all, the message says so directly: `no trace files found in <dir>; coverage is recorded only by "pathkit test" (plain "go test" records nothing)`.

No `workflow.IsReplaying` guard: each run of the function records the whole trip it made, so a replay just produces another complete, correct trace.

### D5 — One single source of truth for paths and edge IDs

The rule: **one thing decides the order of a path's steps.** If `analyze`, the recorder and the matcher each worked the order out on their own, they could disagree, and real runs would show as "missed".

The Go design removes that possibility:

1. **One package builds the graph (`internal/model`).** It assigns every junction a stable ID (`J1`, `J2`, … in source order within the function) and every exit an edge ID, always `J<n>.<exit label>` (`J3.true`, `J5.case "approved"`, `J7.timeout`). `analyze`, the instrumenter, and the matcher all get IDs from this one package. The instrumenter never works out IDs on its own; it asks the model "which edge ID belongs to this exit of this AST node?".
2. **The graph follows execution order.** Built on `go/cfg`, a junction appears in the graph exactly where it runs. The recorder calls `hit()` **at the top of each exit's road**, the moment the decision is made. Graph order and trace order are therefore the same thing by construction.
3. **Matching walks the graph; it does not compare lists.** The matcher starts at `Start`, reads the trace one edge at a time, and follows that edge in the graph. If a step doesn't exist in the graph, the trace is reported as unmatched, with the exact step where it went off the map. The matcher half of the **loop rule** (defined once, in the D3 table's loop row: *each loop edge appears at most once on a listed path; a trace with several trips is folded by keeping only the last trip*) lives in this one walker.
4. **Built-in consistency test.** For every test fixture, an automated check proves that every edge ID the instrumenter writes exists in the graph, and every graph exit has exactly one `hit()` call. Plus end-to-end tests that run fixtures in the Temporal test env and check each trace lands on the expected path.
5. **Path IDs** are a short hash of the path's edge-ID list, so "path 3" in `analyze` and "path 3" in `report` are the same trip.

**Staleness is per workflow, not per file:** the trace stores a hash of that one function's code with comments and formatting removed (gofmt-normalized). Editing a comment, reformatting, or changing *another* workflow in the same file does not make traces "stale" (a plain byte-level hash would flag any change to the file).

### D6 — Test environment: `go.temporal.io/sdk/testsuite`

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

Under `pathkit test`, the instrumented `OrderWorkflow` is compiled in instead, and a trace file appears when `ExecuteWorkflow` finishes. Facts that matter:

- `ExecuteWorkflow` runs the workflow to the end synchronously, with a fake clock, so timers skip instantly.
- `OnActivity(...).Return(...)` makes it easy to force the `failure` exit of any activity.
- `RegisterDelayedCallback` + `SignalWorkflow` drive signal and timeout exits.
- `OnWorkflow` mocks a child workflow: the child's code doesn't run, so it records nothing (correct — it's a separate map).
- Activity retries still apply in the test env: to test a failure path quickly, return `temporal.NewNonRetryableApplicationError(...)` from the mock or set `MaximumAttempts: 1`.
- The test env does not replay workflow code and does not expose event history (another reason for D4 option a).

### D7 — CLI, Go version, module, install

- **CLI library: `cobra` (with `pflag`).** The standard `flag` package stops reading flags at the first non-flag word, so `pathkit analyze orders.go --summary` would silently ignore `--summary` — a classic "flag swallowed" bug. `cobra`/`pflag` reads flags in any position, supports `--flag` and `--flag=value`, and gives `--help` and shell completion for free. Cost: two small, very widely used dependencies. One behavior difference: an optional value is written `--html=report.html` (with `=`); `--html` alone still means "default path". `--html report.html` (with a space) would read `report.html` as the positional argument. The alternative is a hand-written parser (zero dependencies) — PathKit uses cobra.
- **Minimum Go version: 1.26.0.** It is the highest `go` line in the `go.mod` files of the exact `golang.org/x/tools` and `go.temporal.io/sdk` versions PathKit pins (the SDK only matters for the test fixtures, but users need it too).
- **Module path:** `github.com/NikhilRaju9010/pathkit-go`. Binary entry point: `cmd/pathkit`.
- **Install:** `go install github.com/NikhilRaju9010/pathkit-go/cmd/pathkit@latest`. Prebuilt binaries for Linux/macOS/Windows are not provided. `pathkit --version` reads the version stamped at build time, falling back to Go's build info for `go install`.
- **PathKit's own `go.mod` does not depend on the Temporal SDK.** Only the test fixtures need it, and they live in a separate module under `testdata/` (Go ignores `testdata/`).
- **Exit codes:** `0` success; `1` real error (`pathkit <command>: <message>`); `2` coverage below `--fail-under`, or the CI baseline check failed (see SETUP-GUIDE). Using 2 lets CI tell "PathKit broke" apart from "coverage too low".

### D8 — Workflow-level scope config (applies to `analyze` AND `report`)

Without a scope, untestable workflows would drag coverage down and the number would not mean much. The scope works like this:

```json
{
  "packages": ["./internal/workflows/..."],
  "traces": ".pathkit/traces",
  "workflows": {
    "include": ["OrderWorkflow", "PaymentWorkflow", "shipping.ShippingWorkflow"],
    "exclude": [
      { "name": "LegacyPaymentsWorkflow", "reason": "needs an external test system" }
    ]
  },
  "failUnder": 80,
  "html": true
}
```

- Entries name **workflows**, not files: `FuncName`, or `pkg.FuncName` / `pkg.(*Type).Method` when a name is ambiguous. (Also accepted: `Type.Method` and `pkg.Type.Method`. A short name that matches two workflows is an error, not a guess.)
- **The exact `include` rule: `include` means "only these count".** When `workflows.include` is given, the scope is exactly the workflows it lists. Every automatically found workflow that it leaves out is **excluded with the reason `not in include list`**, and listed like any other exclusion. So a workflow can only leave the scope visibly. A listed function the automatic rule missed is added (see below). An empty list (`"include": []`) is an error. With no `include`, every automatically found workflow is in scope. `exclude` then takes workflows out, each with its own required `reason`; `--exclude` adds exclusions with the reason `excluded by --exclude flag`. A name in both lists is an error.
- `include` can also name a function the automatic rule (D3) missed, e.g. an unexported workflow or one returning no `error`, as long as its first parameter is `workflow.Context`. This is how the scope config "covers the rest", because `RegisterWorkflow` calls are not used to find workflows. Two rules:
  - If an `include` name matches no function, or matches a function whose first parameter is not `workflow.Context`, that is an **error** (exit 1), the same as a misspelled workflow name. Example: `pathkit report: .pathkitrc.json: include "sendEmail" is not a workflow: its first parameter is not workflow.Context`.
  - Workflows that are in scope only because `include` added them are labelled **`added by config`** in `report` (text, `--json` and HTML) and in `analyze`, so it's always visible which workflows the automatic rule found and which a person added by hand.
- The same scope is applied by `analyze`, `coverage`, `report`, and `pathkit test` (only in-scope workflows are instrumented).
- The coverage % is computed **only over in-scope workflows**. `report` always prints a line like `2 workflows excluded by .pathkitrc.json (see "reason")`, so nobody can quietly raise coverage by excluding things.
- `analyze` shows a note `Excluded from scope (N), pass --all to show them:` followed by every excluded workflow and its reason. `--all` ignores the config and the flags, and prints a line saying the config file is ignored.)
- An `include`/`exclude` name that matches no workflow is an **error**, not a warning. A misspelled include would otherwise silently shrink the scope and inflate the %.
- The config file is looked up in the current folder, then parent folders up to the folder containing `go.mod`. If there is no `go.mod` anywhere above, only the current folder is checked. `--config <file>` names the file instead. Relative paths inside the file are relative to the file's own folder.
- The `--include`/`--exclude` flags are for one-off runs and take workflow names.

### D9 — `--fail-under` and trace cleanup

- `--fail-under <percent>` on `coverage` and `report` (config key `failUnder`). Below the threshold: normal report printed, then `pathkit report: coverage 62.5% is below --fail-under 80%` on stderr, exit code 2. By default it checks **overall path coverage** (`--coverage-basis` picks the testable basis). Branch coverage is shown but never used for pass/fail.
- **Cleanup:** `pathkit test` starts each run by clearing old traces (pass `--keep-traces` to add to them instead). `pathkit clean` deletes `.pathkit/traces` (or `--older-than 7d`). `--clean` on `coverage`/`report` does the same, for parity. Traces from a stale workflow version are skipped and counted in a warning.

### D10 — Output parity

Text output uses the following format, including `Start -> <junction> --<label>--> ... -> End`. Junction descriptions use Go source text (`if input.AmountCents <= 0`, `switch status`, `select (Selector)`). The only intentional differences are: the end station shows its kind when known (`End (failed)`), and `report` adds the branch-coverage figure and the "excluded by scope" line. `--json` shapes are documented in `SETUP-GUIDE.md` and tested with golden files.

### D11 — Priority labels (High / Medium / Low)

`report` gives each **workflow** a priority label that says how urgently it needs more tests. It is shown in the text report, `--summary`, `--json` and the HTML report.

- **The label belongs to the workflow, not to a path.** Paths are not labelled individually. Every untested path of a workflow effectively carries its workflow's label. A per-path rule (for example "failure paths first") is not part of this design.
- **The rule**, where `covered` and `total` are the workflow's covered and listed paths:
  - **High** when coverage is below 50%: `covered × 100 < 50 × total`.
  - **Medium** when it is from 50% up to and including 80%: `covered × 100 ≤ 80 × total`.
  - **Low** when it is above 80%, including 100%.
- **Exact whole-number comparison, never a rounded %.** The boundaries are compared with the integer products above, never with a printed or rounded percentage. So 1/2 = 50% is Medium, 4/5 = 80% is Medium, 7999/10000 = 79.99% is Medium, 8001/10000 = 80.01% is Low, and 4999/10000 = 49.99% is High. Tests pin exactly these boundaries and the values just either side.
- **A workflow with 0 listed paths has no label.** This happens when PathKit finds no way for the workflow to finish (for example a loop with no `break` and no `return`), or when every path ends in `panic`. `report` and `coverage` print a warning for such a workflow.
- `SETUP-GUIDE.md` says that labels are per workflow, and what each one means for a tester.

### D12 — HTML report

`report --html` and `analyze --html` write one self-contained web page.

- **Separate files, no merged store.** `report --html` writes `.pathkit/report.html` from its own single run. `analyze --html` writes `.pathkit/analysis.html` (the paths only; the page says "Not measured"). A merged store would let the paths come from an older version of the code than the coverage. That is the D5 bug class, so it is not used.
- **The same numbers as `report`.** The page is filled from the shared `measure`'s `coverage.Result`, and every sentence (total line, branches, stats, excluded line, Traces line, path text) comes from package `render`, exactly as the terminal prints it. There is no calculation in `internal/htmlreport`.
- **Light theme, paginated, one small script, nothing loaded.** The page is light only (`color-scheme: light`), laid out for a phone, a tablet and a desktop, and works when printed. The paths are not written as HTML: they are embedded once as compact JSON (a table of strings, so every label and every step text is stored once) and a small inline script draws the page of the table being looked at (25, 50 or 100 rows), with filters, search, sorting and a details row. The script puts data on the page with `textContent` only. Without JavaScript the summary, the workflow overview, the glossary and the numbers are still there, and a note says that the path tables need JavaScript. There are no external stylesheets, fonts, images or scripts, and no network request. A Content-Security-Policy `<meta>` (`default-src 'none'; style-src 'unsafe-inline'; script-src 'sha256-…'; img-src data:`) allows exactly the page's own script, by its hash, and makes the browser refuse any other script or request. The fonts are the system's own.
- **Escaping.** The page uses Go's `html/template`. No value is ever marked safe: `template.HTML`, `JS`, `CSS`, `URL`, `HTMLAttr`, `JSStr` and `Srcset` are banned by a source-scanning test. The data block is a Go value that the template encodes for a script; the stylesheet and the script are part of the template text, not values.
- **The trend.** The last 5 `report --html` runs are kept in `report-history.json` **next to the HTML file** (`schemaVersion` 1). Each run stores its totals, the in-scope and excluded counts, and a scope fingerprint; when the fingerprint changes, the trend line says `scope changed: N in scope, M excluded`. A run is added only when the page was written (exit 0 or 2).
- **Config:** `html` applies to `report` only (`true` → `.pathkit/report.html`, from the current folder; a path → relative to the config file; `false` → none). `--html` / `--html=path` / `--html=false` always win. `--html path` with a space is caught with a hint to write `--html=path`.
- `pathkit test`, `pathkit clean` and `--clean` never delete `report-history.json`. That's tested, including when it sits in the trace folder or in `.pathkit/overlay`. To make it hold, `pathkit test` deletes only its own generated files in `.pathkit/overlay`; it never empties the folder.
- a missing or damaged `report-history.json` gives a warning (`pathkit report: warning: … starting a new trend`) and a fresh trend; it never fails the report. Both are tested.
- SETUP-GUIDE explains that in CI the trend starts empty each run, and how to keep it: restore and save `report-history.json` with the cache action, and upload the page as an artifact.
- a workflow truncated at the path cap shows "truncated" in the HTML, with the same `(truncated at maxPaths=2000)` wording as the text report.

### D13 — Path classification

Some listed paths can never run (the same value tested twice with opposite answers, a loop that always runs once, a signal channel Temporal never closes). Counting them as "missed" makes coverage look low and hides the real gaps. With classification enabled (`"classification": {"enabled": true}` in `.pathkitrc.json`), every path gets a **bucket** and, unless it is an ordinary path, a **reason label**:

| Bucket | Meaning |
| --- | --- |
| `testable` | A normal test can run it. |
| `cannot-be-tested` | No run, test or production, can take it. |
| `needs-special-test` | Possible, but needs a special setup (a replayed history, a faked history size). |
| `needs-review` | PathKit is not sure. |

- **One definitions list.** Every bucket, label and `//pathkit:...` comment is defined once, in `internal/classify/definitions.go`: buckets first, then labels grouped as condition logic, loops, Temporal guarantees, special tests, PathKit's own limits, manual. The text report, the HTML glossary and the tests all read it; a test fails if a printable label has no definition.
- **Who decides, strongest first:** a config override (`mark_testable` / `mark_untestable`) > a `//pathkit:untestable` comment > a config rule > a detector > testable (the default). The first that applies is the main reason; the others are kept as extra reasons.
- **Principles.** When unsure, a path stays testable or goes to needs-review, never to cannot-be-tested. A path a test ran is always testable, whatever was claimed. Every override and rule needs a reason. A detector's claim in a workflow that has a trace fitting no path is not trusted (needs-review, label `model-limit`) when it rests on the step where that trace leaves the map or a later one (**local distrust**: the matcher knows how many steps of the trace fitted, `d-1`; a claim whose reasoning uses only steps before `d` keeps its label; one whose steps are not known is not trusted, as before). The unmatched-trace warning prints a ready-made `classification.explained_traces` entry with `"reason": "TODO"`; the config refuses a reason of `TODO`, because accepting a trace is a decision. A saved `stable_id` that no path has any more is reported as stale; when the workflow now follows helper functions, the warning lists the paths whose older id it was.
- **Detectors** read only the facts the extractor attached to the junctions (`internal/model/facts*.go`), never source text, and never coverage. They return `certain` only when they are sure; anything less (`likely`) becomes needs-review. Each one is conservative: a near-miss it must not claim is tested next to every case it must claim.
- **Coverage numbers.** Testable coverage is covered testable paths over testable paths (needs-review paths count as testable under the default policy `count_as_testable`, and are left out under `exclude`, except untrusted claims, which always count). Overall coverage is covered paths over all paths. Branch coverage is unchanged.
- **The classification block** prints, in this order: the bucket counts with labels and where they came from, testable coverage, special-test coverage, overall coverage, and the basis `--fail-under` uses. With classification off the report is exactly the report without it.
- **Additive output.** The JSON and HTML only gain fields; existing keys and values stay the same, and a test proves it.

### Trace file format (schemaVersion 2)

One JSON file per workflow run, written by the recorder that `pathkit test` adds to the package through the overlay. It lives in the trace folder (default `.pathkit/traces` in the folder `pathkit test` runs from, baked in as an absolute path) and is named `<workflow>.<12 random hex digits>.trace.json`. Example:

```json
{
  "schemaVersion": 2,
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
| `schemaVersion` | Always `2` for this format. Version 1 (before helper functions were followed, D15) is refused with "traces are from an older PathKit ... re-run `pathkit test`"; any other value is reported as unsupported. Never guessed at. |
| `tool` | Always `pathkit-go`. |
| `workflow` | The workflow's name as `analyze` prints it (`pkg.Func` or `pkg.Type.Method`). |
| `functionHash` | 16 hex digits: the start of a SHA-256 of the workflow function's code, printed by `go/printer` without comments (so formatting and comments don't change it). Computed only by `model.FunctionHash`; for a workflow whose graph follows declared helper functions (D15) it is `Graph.CodeHash`, which also covers their code (a workflow without helpers keeps exactly its `FunctionHash`). A different hash means the code changed, and the trace is **stale**. |
| `status` | `complete` if the run **finished** a `return` of the workflow function, with all return values evaluated (a `return` that is reached but panics while computing its values is not complete, so the mark is set only after the values are evaluated). Otherwise `incomplete`: a panic (including one while computing a return value), the SDK stopping the run with `runtime.Goexit`, or a timeout. Only complete traces are matched. |
| `steps` | The exit IDs the run passed, in order, exactly as `model` produced them (`J1.false`, …). Every string written here came from the model at instrument time (`Graph.ExitFor(...)`, or, for a decision inside a helper, the table `Graph.RecorderTable`). A run that goes through the code of a followed helper also records `>S1` when it enters the copy `S1` and `<S1` when it leaves it; the matcher takes the markers out, checks they nest and that each step was recorded in the copy the map has it in, and matches the rest. |
| `workflowId`, `runId` | From `workflow.GetInfo`, for reference only. They are not used for matching. |
| `recordedAt` | UTC time the file was written (RFC 3339). |

What is deliberately **not** in the file: the Go test name (the workflow runs on its own goroutine and can't see it), the end kind (the matcher gets it from the graph), and the path number (display order can change; the path's identity is its steps plus end kind).

### D14 — The fact engine (value claims)

**Problem.** The automatic detectors (D13) look for exact code shapes. The same idea written in a slightly different shape was missed: `var s string` then `if s == ""`; `id := ""`, then `if ok { id = v }`, then `if id == ""`; a value an early `return` makes non-empty, checked again as `len(x) == 0`.

**Choice.** One engine, `internal/facts`, that walks each listed path step by step and keeps a small notebook of facts it is sure of. When a decision asks something the notebook answers, and the path takes the other answer, the path is impossible and the notebook entries are the proof. Options that were left out: one more detector per shape (does not scale; each new project showed a new shape), and a full symbolic solver (slow, and a wrong "impossible" would hide a real missing test).

**How it fits.** The graph builder (D5) records, for each road between decisions, the statements a run executes there (`Target.Nodes`). It changes no junction, exit, id or stable id. The engine reads those statements along a path; nothing else about the graph changes.

**What it knows.** For locals and parameters (not package-level variables, not map or slice elements), and for field paths under a struct held *by value*: one constant, up to 8 constants it is not, and nil / not nil. Facts come from zero-value declarations, constant assignments, `&T{}`, `new`, `make` and slice/map literals (not nil), the outcome of a decision (`==`, `!=`, `!`, `&&` when true, `||` when false, `len(s) == 0` forms, bools), early returns (the other outcome of the `if`), comma-ok forms (`v, ok := x.(T)` or `m[k]`, with `ok` false, give `v` its zero value), and copies (`y := x` copies everything known).

**When it forgets (the safety list).** Never wrongly kept: a value assigned something it does not understand; any value whose address is taken anywhere in the function (`&x`, which covers `Get(ctx, &x)`, `SideEffect(...).Get(&v)`, `Receive(ctx, &v)`, `json.Unmarshal`), or on which a pointer-receiver method is called, or that is sliced as an array; any value written inside a closure (`workflow.Go`, a selector callback, `defer`); every value a loop assigns, at each step on that loop; every value an `if` or loop assigns that PathKit walks without deciding it (an error check it assumes succeeds, an `if` marked `//pathkit:ignore`, a loop with nothing to decide), from there on, with the code inside it ignored; a field behind a pointer is never tracked. A function that uses `goto`, `recover` or `unsafe` is left alone.

**Answers.** Three-valued: true, false, unknown. Unknown never produces a claim.

**Labels.** `contradictory` when the same unchanged value was tested earlier on the path (not by an early return) and this decision gives the opposite answer; `constant-condition` when the answer follows from declarations and constants alone with no decision in between; `value-flow` for everything else (copies, assignments, early returns, decisions that skipped an assignment). If a detector, rule, override or comment already decided the path, the engine says nothing: their label and wording stay.

**Proof.** Every fact keeps where it came from; a claim prints them in order as numbered plain sentences (`pathkit explain`, and the `proof` JSON key).

**Possible correlation.** On a path no test ran, two decisions on the same value that would contradict each other if the value had not changed, with a point in between where the engine had to forget it (a loop that assigns it, an `if` or loop PathKit does not decide); a value whose address is taken or that a closure writes is remembered only until the next call, go to needs-review (`possible-correlation`), naming both steps. Never for a path a test ran.

**Config.** `classification.engine.enabled` (default true) turns the engine off; `detectors.disable` accepts `value-flow` (no engine claims), `possible-correlation` and `unknown-input-type` (no suspects of either kind).

**Types, nil and length (milestone M3).** The notebook also holds, for an interface value, the list of dynamic types it can hold (a closed list: a type not in it is impossible), and for a slice or map built in the function, the list of types of its elements, whether it is empty, and which constant keys it certainly has. A concrete value stored in an interface (assignment, map or slice literal, `m[k] = v`, `append`, an argument of interface type) adds its static type. Claims: a type switch case, or a type assertion that must fail or must succeed, that no type in the list takes (`impossible-type`); `v == nil` for an interface that holds a typed value, a typed nil pointer included (`impossible-nil`; for a map element only when the key is proven to be in the map, since a missing key gives nil); leaving a loop before its first round when the loop always has one (a `range` over something not empty, a condition that is true at the first test), with the existing label `impossible-loop-exit`. A slice or map that something else can reach is not followed at all: one that is passed to a call (activity arguments too), copied, deleted from, sliced, captured by a closure, written through an alias, or has its address taken (`escapes`); a slice literal with indexes (`[]T{2: x}`) and a map with interface keys (where 1, 1.0 and int64(1) are three keys) are not followed for the same reason, because those can change its elements or length behind the engine's back.

**Values Temporal decodes (assumption `default-json-converter`).** A value Temporal's default JSON converter decodes into an `interface{}` holds only a `string`, `float64`, `bool`, nil, `[]interface{}` or `map[string]interface{}`; a typed target holds its static type. The engine uses this for workflow inputs of those types and for variables filled by `Future.Get(ctx, &x)`, `ReceiveChannel.Receive(ctx, &x)` and a `SideEffect` result's `Get(&x)` (the only place such an address is allowed; `x` must have been nil, because the decoder fills what an interface already points to). It is an assumption, named in every claim that rests on it, counted in every report, and turned off by itself when the module sets its own converter: a client or worker option named `DataConverter`, a call of `NewCodecDataConverter` or `NewCompositeDataConverter`, or a type with the methods of a data converter, payload converter or payload codec (a syntax search of every Go file of the module, comments and strings excluded, except folders named vendor, testdata, node_modules, .git and .pathkit; every module the workflows are in is searched), or by `classification.assumptions.default_json_converter = "off"`.

**Slices and maps handed to calls that encode their arguments (assumption `no-custom-interceptor`).** A slice or map used as an argument of `workflow.ExecuteActivity`, `workflow.ExecuteChildWorkflow` or `workflow.SignalExternalWorkflow`, directly or inside a struct (or its address) written in the call, is not forgotten: the SDK encodes those arguments when the call is made, so the callee works on a copy. The evidence is in the SDK source, for the versions the projects use (v1.25.1, v1.36.0 and v1.49.0 checked): `encodeArgs(dataConverter, args)` in `(*workflowEnvironmentInterceptor).ExecuteActivity` (`internal/workflow.go`), the same `encodeArgs` inside `getValidatedWorkflowFunction` (`internal/internal_workflow.go`), called by `(*workflowEnvironmentInterceptor).ExecuteChildWorkflow`, and `encodeArg` in `signalExternalWorkflow` (`internal/workflow.go`). `ExecuteLocalActivity` does not encode: its parameters keep the Go values (`InputArgs: args`, run with `ExecuteWithActualArgs`), so the local activity gets the very map and may change it; it and every other call stay as before (the map is forgotten). A struct kept in a variable first (`in := &T{Data: m}`) is an alias and is not covered. The one thing that sees the original arguments is an interceptor (they run before the encoding), so claims that keep facts across such a call name the assumption `no-custom-interceptor`; it is turned off by itself when the module registers one (an `Interceptors` option, a type built on the SDK's interceptor base types, or an `InterceptWorkflow` method; a syntax search like the converter one), or by `classification.assumptions.no_custom_interceptor = "off"`.

**Values encoding/json decodes (no assumption).** `json.Unmarshal(data, &v)` and `dec.Decode(&v)` onto a variable of type `interface{}`, `map[string]interface{}` or `[]interface{}` that is known to be nil give the same closed list of types. A Decode counts only when `dec` is a variable set once by `json.NewDecoder(...)` in the code the paths run, every use of it is a method call on it, and `UseNumber` is never called on it (then a number would be a `json.Number`); anything else is not followed. `UseNumber` or `json.Number` elsewhere changes nothing for other values: the rule asks about the decoder that produced the value in question. Claims on these values name no assumption.

**`unknown-input-type`.** An untested path that takes a type switch case on a value whose origin PathKit cannot follow, when no test has taken that case, is needs-review, grouped by switch line in the reports.

**How it is kept honest.** Shape fixtures with hand-written answers, a safety fixture for each forget rule, and `internal/soundness`: random programs, run for every input, fail the build if a path called impossible is ever taken.

### D15 — Helper functions (milestone M4)

**Problem.** A workflow usually calls its own package's helper functions (`o.charge(ctx)`, `reserve(ctx, items)`). Their activities, timers and branches are real steps of the workflow, but a call was one opaque step, so the decisions inside were not paths, the error check after the call was assumed to pass (a run in which a helper failed fit no path), and a trace through such a decision fit no path.

**Choice.** The graph builder follows a call into the helper's code and builds **one copy of the helper per call** (a *site*, `model.Site`), at most three helpers deep, recursion followed once and stopped. Each copy has its own junctions, numbered after the call in source order, so the same helper called twice is two sets of decisions. The code of a helper returns to the code after the call: a `return` in the helper, or its end, continues at the caller (`Target` roads continue exactly as a selector callback's do); `return helper(ctx)` makes the helper's return the caller's end.

*What is followed.* A function or method of the workflow's package (a method needs a receiver of a known concrete type, an interface method is not followed), or a local variable set exactly once to a function literal and called by name; whose code calls the Temporal SDK, or calls a helper that does (a helper with `if`s and no SDK call is ordinary code and stays one step); with a parameter of type `workflow.Context` that has a name; with no `goto`, Go `select`, `workflow.Selector` or type parameter. The call has to be a whole statement: `h(ctx)`, `x, err := h(ctx)`, `err = h(ctx)`, `var err = h(ctx)`, `return h(ctx)`; a call inside a bigger expression, a `go` or `defer` call, or one in the condition of an `if` is not followed (the old behaviour: one step). A call through an interface or a function value other than a once-set literal is opaque. Helpers in other packages are not followed.

*The path cap.* Following helpers multiplies paths (a helper with an activity and its error check doubles the paths after it). They are followed all the same: the workflow is counted exactly, cut into stages and covered stage by stage (D16), and only the path list is capped. (Until milestone M5 a workflow whose followed helpers passed the cap kept each helper call as one step; that fallback is gone.)

*Ids.* The workflow function's own junctions keep their ids and stable ids exactly: a workflow that calls no helper that touches the Temporal API has the same `J` numbers, path ids, stable ids, function hash and report as before (one that calls such a helper PathKit cannot follow, or does not follow because of the path cap, still gets the new error check after the call, see D2). In a workflow that follows helpers, decisions inside a copy are numbered right after the call that leads to them; their stable text is the decision's text plus the way in, in words and without line numbers (`if n > 0 [in charge called as charge(ctx, 1)]`), made unique like any repeated text; an error check or loop that exists only because a helper is followed is new too. Printed paths mark such a decision `[in charge]`. `Path.LegacyStableIDs` computes the ids a path had before, so a saved override that points at a vanished id gets a warning that lists the paths that replace it.

*Errors.* A helper's *error summary* says how it can fail: never, exactly when modelled calls fail (which), or not followed (unknown). The check after the call is a junction unless the helper never fails; when the helper's `return` decides it, that check has the one exit that return allows (`Target.Only`). The decision stays in the graph, and in the recorded steps, because the marked-up code of the check runs in every case.

*How it is recorded.* `pathkit test` marks up the helper once (all workflows that use it share the marks). A followed call hands its site over through the context argument (`pathkitSite(pathkitRec, ctx, "c57:8")`); the helper's first line takes it (`pathkitH, ctx := pathkitEnter(ctx, key); defer pathkitH.exit()`; taking it out of the context keeps a call further down that is not followed from using it). The helper records its decisions under keys of its own (`line:column.label`); the recorder's table, written by the model (`Graph.RecorderTable`), turns them into the graph's exit ids, and records `>S1` and `<S1` around them. A helper entered from anywhere else records nothing. The trace format is therefore version 2.

*The fact engine.* A copy's statements are part of the road, so the engine follows them. On entering a copy, everything known about the helper's own variables (parameters and locals) is forgotten, because a second call starts them again; the safety list is applied to helper bodies too (address taken, closure writes, goto, recover, unsafe). A value the caller passes in is not tracked into the helper.

*Not claimed.* No fact is attached to a decision inside a helper by the older, structural extractors (`internal/model/facts*.go`); a claim there can only come from the engine.

### D16 — Big workflows: exact counts, stages, pruning (milestone M5)

**Problem.** A workflow with twenty independent decisions has a million paths. Listing stops at 2000, so the rest was invisible: no count, no classification, and a test that ran a path beyond the list looked like an unmatched trace. Branch coverage was the only number left.

**Exact count.** `Graph.PathCount` counts the paths with dynamic programming over the decision graph (`math/big`), the way `Paths` lists them: each exit, and each loop's retry edge, at most once per path. A road's count depends only on where it leads, because a path meets every decision at most once, except a loop's head, met again after its body, where only the exit is left. A test checks the count against the listing on the pilot and every fixture. Reports print "Exact paths: N" next to what the list holds.

**Stages.** A *gate* is a decision that every road getting past it goes through: it dominates everything it can reach (computed with dominators on the decision graph, loop retry edges left out), no loop is cut by it, and no road reaches it with one exit already decided (`Target.Only`). Gates are ordered, and the stretch from one gate to the next is a *stage*. A path ends inside a stage or goes on to the next, so the paths of a workflow are the stage paths of stage 1, followed (for those that go on) by those of stage 2, and so on. Neighbouring stages are merged while the merged stage has at most `classification.stages.max_paths` (256) stage paths. A stage that is bigger on its own is not listed; the reports say so and use branch coverage inside it. Early returns make later decisions "not on every path" for the plain dominator idea, so a gate is defined as dominating what it reaches, not as being on every path to an end.

**Stage path ids.** `g<k>:<12 hex digits>`, a hash of the stage's number and its decision words (like stable ids), unique within a workflow (a test checks it with several stage sizes).

**Facts across stages.** The fact engine walks a stage from every different notebook the earlier stages can leave at its first decision (`Walk.Signature` says when two are the same), at most `classification.engine.max_contexts` (16). A stage path is cannot-be-tested when some decision on it is impossible in every one of them, testable when one of them allows it, and needs-review (`model-limit`) when the states were cut off at the limit and none of the examined ones allows it. When a stage is too big to follow, the next one starts with nothing known. Proofs name steps of the stage; a fact from an earlier stage has no step number. A test checks that a stage path called impossible is impossible on every whole path through it, on the engine fixtures, and the soundness test checks it on random programs with small stages and few states. *A cut stays cut:* once a stage had more different outgoing notebooks than the limit, every later stage is judged as cut (needs-review instead of impossible) until a stage whose incoming notebook is empty (a stage too big to follow, or none went on), because the notebooks left out may be the ones that make a later path possible. (A reviewer found this the first time: the flag was reset after one stage.)

**Where stages are printed.** Only for a workflow whose paths are not all listed (more paths than the list holds, or parts left out as pruned groups). A workflow whose paths are all listed prints as before: its path numbers use every rule, override and detector, which the stage numbers (the fact engine only) do not. The `branches` total of a capped workflow stays the listed exits; `branchesAll` is the whole map.

**Stage coverage.** A trace is matched to a path of the whole map (a path beyond the list is fine), cut into its stage pieces, and each piece marks the stage path of that stage. No new test run is needed. The number shown is covered stage paths over stage paths not called impossible, over the stages that are small enough to list. A trace that fits the whole map but a path beyond the list is counted (`traces.beyondCap` in the JSON), never unmatched.

**Pruning.** For a workflow with more paths than the list holds, the listing follows the fact engine along each path (`Graph.PathsPruned`, `Walk`) and leaves out every exit the engine shows impossible, with its exact count and its proof, as one group; so the 2000 listed paths are paths that can be taken. A listed path keeps its stable id. If a trace runs through a pruned group, the proof was wrong: the group is un-pruned (its paths count as needs-review, `model-limit`, and not as impossible), the path the trace ran is listed, and a warning says so. The groups cover the part of the map the list reached before it was full; the rest of the map is counted but not classified.

**Config.** `classification.stages.max_paths`, `classification.engine.max_contexts`, `classification.ci.stage_floor` (a floor for stage coverage, checked like `fail_under_testable`).

**Output.** New JSON keys only (per workflow, when it has several stages or more paths than the list holds): `pathCountExact` (a decimal string), `pathsListed`, `stages`, `prunedGroups` (the 100 biggest, with `prunedGroupsTotal`), `branchesAll`; and `traces.beyondCap`. The text and HTML reports gain "Exact paths", "Stage coverage", the stage lines and a Stages view.

## Non-goals for v1

- **One package at a time for analysis.** `analyze` takes a file (analyzes the workflows in it, but type-checks its whole package, since Go needs that) or a package; `report` takes package patterns like `./...` and analyzes each package separately.
- **No following calls into other packages or into child workflows.** A helper function in another package, or a child workflow started with `ExecuteChildWorkflow`, is shown as a single step. (Helpers of the workflow's own package are followed, D15.)
- No saga/`defer` junctions (see D3), no `workflow.Go` concurrency modelling, no Update/Query handler paths.
- No replay of production histories (see D4 d3).
- No non-determinism checking — that's `workflowcheck`'s job; users should run it too.
- No IDE plugin, no server, no database. The HTML report is one self-contained file.

## How PathKit relates to Temporal's `workflowcheck`

- It finds workflows **by type** (first parameter is `workflow.Context`, or registered with `RegisterWorkflow`), not by name. → D3 uses the type check too, but in v1 only together with "last result is `error`" and "exported"; it does not follow `RegisterWorkflow` calls.
- It is built on Go's standard analysis tooling (`go/analysis`), so it plugs into `go vet`. → We use the same loading/type-checking stack (`go/packages`, `go/types`).
- It follows calls across packages and prints the chain (`X -> Y -> Z`). → Powerful but a big scope jump; we deliberately don't in v1.
- It supports `//workflowcheck:ignore` comments (no space after `//`) and a config with overrides. → `//pathkit:ignore` and `//pathkit:branch` (D2).
- It says openly that it can't catch everything. → Same honesty rule as `LIMITATIONS.md`.

**Headline (after M6).** When a workflow is over the cap (`Capped`), the project headline is three numbers (`coverage.Result.Headline`): path coverage over the workflows under the cap, stage coverage over the ones over it, branch coverage over all (the whole map). The listed paths of a capped workflow are not in the path number. With no such workflow the headline is the plain totals as before, so existing output does not change. JSON: the old keys keep their value and meaning (listed paths only); `headline` is new and present only when a workflow is over the cap. Output JSON keys are camelCase like every key from before v2; config keys are snake_case like theirs.

### D17 — Missed paths by first untested step (milestone M6)

**Problem.** A list of 300 missed paths is not a to-do list. Most of them differ from a tested path only after the same untested step, so one test covers many.

**Rule.** Build a prefix tree of the steps of the paths the tests ran. Follow each missed testable path (not covered, bucket testable) down the tree; the step where it leaves the tree is its group. Groups are counted, biggest first. A group names the decision (with file and line), the road the tests walked to it, and the first line of the "how to cover it" hint for that step. A workflow no test ran has an empty tree: its groups are the exits of its first decision, and the report says "no test ran this workflow". Only listed paths are grouped; a capped workflow says so.

**Output.** `pathkit report --groups` (text, and `missedGroups` in `--json`, all groups up to 500 with `missedGroupsTotal`), and a section in the HTML report (the first 100). The JSON key is added only with `--groups`; the HTML section is added whenever a testable path is missed.

### D18 — Reviewing what a person wrote: rules check, suggest, known-defect (milestone M6)

**rules check.** For every rule, override, `//pathkit:untestable` comment and explained trace, the number of listed paths (or traces) it hits now. A rule, untestable override or comment that hits a path a test ran is refused (exit 1): a run proves a path possible, so the claim is wrong. An entry that hits nothing, and two entries that hit the same path, are warnings. A workflow with more paths than the list holds never gets the "stale" warning, only "may hit a path that is not listed".

**suggest.** Drafts a rule for each group of needs-review paths no test ran (grouped by workflow and label). The draft's `when` is the decisions every path of the group takes; if that would also hit a path outside the group, it is one rule per path with all its decisions. The reason and the label are `TODO` on purpose: the loader refuses `TODO` in a rule's label and reason, in overrides and in explained traces, so a draft cannot take effect unread (write `known-defect` if a bug blocks the path, or a cannot-be-tested label with `"impossible": true`).

**known-defect.** A built-in label that only a person can set (rule or `mark_untestable`, reason required). The bucket is needs-review (the path can happen; a bug blocks the test), never cannot-be-tested. A rule with this label needs no `"impossible": true`. The summary counts these on their own line ("Blocked by known bugs"), and `blockedByKnownBugs` in the JSON. A path a test runs anyway is testable, as always. PathKit never sets it.

### D19 — The assumptions panel (milestone M6)

Every report shows, when classification is on, the three assumptions claims can rest on, each on or off, with how many claims rest on it: `closed-signal-channels`, `default-json-converter`, `no-custom-interceptor`. Text (also with `--summary`), `assumptionPanel` in the JSON, a list in the HTML summary and glossary entries for all three. A part of the map left out as a pruned group counts as one claim. The older `assumptions` JSON key, which lists only the relevant ones, is unchanged.

### D20 — Honest numbers on huge workflows (milestone M7)

**Problem.** For a workflow over the path cap, the numbers of the classification block counted only the listed paths, a ratio with an empty denominator printed `0/0 (0.0%)`, pruned paths were nowhere in the block, and the CI floor mixed the listed paths of a big workflow into the project number.

**Decisions.** (1) A ratio with nothing to divide by prints `not measured (<reason>)`; the JSON gets `measured: false` and `reason` next to the old keys (the percent keeps its value). (2) With a workflow over the cap, the classification block is titled "listed paths only", says how many paths were left out as impossible (total and by label), and spells out the testable denominator. (3) `ci.fail_under_testable` is checked against the path coverage of the workflows under the cap together with the stage (default) or branch coverage of each workflow over it (`ci.capped_basis`); the report says which was used. (4) The text report prints the paths of a workflow over the cap only with `--paths`. (5) The old side of a `GetVersion` check is counted exactly (`oldVersionPaths`) and labelled in stage paths too; it is never hidden. Output for workflows under the cap does not change.

### D21 — Helpers copied in, or summarized (milestone M7; extends D15)

**Problem.** Copying a helper's decisions into the map at every call multiplies the paths: eight calls of a helper with two decisions are 3^8 paths.

**Decision.** `classification.helpers.mode`: `inline` (D15 as it was), `summarize`, or `auto` (default: inline while the exact count is within the cap, else summarize). In summarize mode a helper call is an ordinary step; its error check is the existing decision on the result of a helper (`<helper> (helper)`, or the labels of the calls it returns the errors of). The helper's own decisions are mapped once as a **HelperUnit** (`H1`, junctions `H1.J1`, exits `H1.J1.true`), outside the workflow's junction list, so the workflow's `J` numbers and stable ids do not move. The recorder hands the unit's id over at each call (a unit is the only "site" every call shares); a trace has `>H1` / `<H1` markers and the unit's exit ids, which the matcher takes out of the path steps and counts as branch hits. The fact engine sees no facts in a unit (claims there are made with unknown inputs). The code hash of a summarized graph differs, and a trace recorded against the inline graph is converted step by step (`Graph.FromInline`) when its hash is the inline graph's, so older traces still count. Everything that numbers or ids the workflow is unchanged for a workflow that stays inline (D5 holds: only `internal/model` makes ids).

### D22 — The "could not fully read" panel (milestone M7)

Every place where PathKit treated something as unknown or cut it off is collected by `internal/model` (`Graph.Unread`: calls that take a `workflow.Context` and were not followed, futures nobody reads, closures not mapped, returns of a call) and by the measurement (path cap, context cut-offs, workflows that cannot be mapped). It is shown in every report format; nothing is hidden when the list is long (text shows the count and the top 10, the JSON and HTML show all). It lists; it never changes a path or a bucket.

### D23 — More Temporal shapes (milestone M8; extends D3)

**Decisions.** (1) Exit labels (`success`, `failure`, `received`, ...) are never renamed, so ids hold; "fired / cancelled" for a timer are `success` / `failure` of the read. (2) A step happens where a future is read. (3) `return <call>` of a future read, `Sleep` or `Await` is a decision only with `classification.direct_returns: "step"` (default `report`): making it one changes the stable id of every path through it, so it is opt-in; its junction carries `viaHelper` so older stable ids still resolve through the legacy lookup. (4) Update handlers and validators are mapped as small workflows (`Handler`, `FindHandlers`), named `<workflow>.update[<name>]` / `.validator[<name>]`, recorded like workflows; a query handler and a signal have a row in the handler table but no map. (5) `classification.activity_wrappers` makes a named function a step. (6) Failure kinds are listed per call (call outcome table), never made paths. (7) Loop rounds are read from the raw steps of a run; the path model stays one round (D3). (8) The trace file gets an optional `recorder` field (3 for this recorder); a step kind an older recorder did not write is `not recorded`, never unmatched. (9) Several modules in one folder are read module by module (`load.FindModules`, `--module`).

### D24 — Stages cut further (milestone M7; extends D16)

A stage with more paths than `stages.max_paths` is cut, recursively, at the *local gates* of its nested regions: a decision that every path entering the region passes. The top of the stretch and each region become **parts** (`Stage.Part`, `Within`): a stage path of a part ends at the first decision outside it. A part is examined by the fact engine with an empty notebook and leaves an empty notebook (the same safe rule as after a stage that is too big to list), so a claim in a part rests on the part's decisions only. The cut is refused (with a reason line) when a region leads back to the top of the stretch or into another region in no fixed order. The test `TestStagesAddUp` checks that the stage paths add up to `PathCount` through the stage graph for every fixture at several limits.

### D25 — Behaviour fingerprints and scenarios (milestone M9)

**Problem.** For a workflow with thousands of paths, "paths covered" says little: most paths only differ in decisions that compute something. What matters to a person is what the workflow *does*.

**Decisions.** (1) The *fingerprint* of a path is the list of its external steps in order (`internal/model/fingerprint.go`): the outcome of every activity, local activity, child workflow, Nexus call and timer that a decision reads (`Name (kind): success|failure`, plus `failure kind: …` when the code tells kinds apart), calls without a decision (`… : unchecked`, `timer`, `signal sent`, `signal received`, `side effect`, `upsert …`, `continue-as-new`, `cancel context`, `helper name(args)` for a summarized helper), the exits of selectors and waits, the old or new side of a version check, loops as `0 rounds` / `1+ rounds`, and the end. Logging, local computation, queries and the error check on a followed helper's result (its own decisions already say it) are left out. (2) A path through a place the model did not read (an unread entry that can hide a step: interface, function value, other package, computed name, helper not followed, closure) gets the step `unknown <path id>`, which no other path has: **never merge two paths whose behaviour might differ**. A summarized helper call counts as known only when its arguments are constants or unchanged parameters (or it has no decision inside). "Result returned directly" does not hide steps. (3) A *scenario* is one distinct fingerprint; its id is `sc:` plus ten hex digits of the hash of the steps. (4) Under the cap the scenarios are the groups of the listed possible paths. Over the cap they are counted exactly (`math/big`) from the shape of the map: an automaton whose edges are the steps (internal decisions are empty moves, a loop is read once the way the path list reads it), made deterministic by the subset construction and counted by its accepted words. A workflow with an unread step that can lie on a path, or an automaton past `classification.scenarios.max_states`, is *not measured*, with the reason. Pruning by the fact engine works path by path and so only reaches scenarios through the listing; over the cap the count can include impossible scenarios, and scenarios no listed path makes count as testable (pessimistic, never optimistic). (5) A scenario is cannot-be-tested only if every path in it is; else testable if any is; else needs-review; else needs-special-test; a covered scenario is testable. (6) Checked by running programs: in the soundness test every run's real activities and timers equal the steps its path says, and two runs of one scenario made the same steps.

### D26 — The main number and the project headline (milestone M9)

**Problem.** One number per project hides the workflows that matter; path coverage of a 33-million-path workflow is 0%.

**Decisions.** (1) Each workflow gets *one* main number by rules in this order: handler coverage for an event loop (an unbounded `for` whose body holds a Selector's Select or a signal receive), path coverage under the cap, scenario coverage when the scenarios were counted and are at most `scenarios.max_listed`, else branch coverage over the whole map; `main_number.force` overrides with a reason. (2) The text, `--summary` and the page start with `N of M workflows meet their target (main number ≥ T%)` and the branch coverage of all workflows; the cap and no-path warnings stay above it, the older headline lines below. A workflow with nothing to measure is listed and not counted as passing. (3) Ranking, advice and labels only order and explain; they never change a number, a bucket or a claim. (4) The CI baseline stores each workflow's main number; the gate fails when it drops by more than `ci.tolerance` or below `ci.main_floor`; old baselines work as before. (5) Split advice names the biggest stage as a candidate child workflow: advice only. (6) Decision pairs (D27) are not a main number.

### D27 — Decision pairs (milestone M9)

A pair is an outcome of one decision followed by an outcome of the next decision on a road (`J1.false J2.failure`); after a loop's body the next decision is the loop's own exit. Under the cap, pairs no possible listed path contains are not counted; over the cap every pair of the map counts. A pair is covered when a counted trace took both outcomes in a row. It is a finer level than branches and a coarser one than paths, shown in JSON and the headline, never a main number.

### D28 — Tests to write first (milestone M10)

`internal/advice` turns the result into items: missed testable scenarios (or groups of missed paths where scenarios are not measured, or handlers no test used), each tagged by the riskiest step it contains (cancel > compensation > activity failure > continue-as-new > timers and timeouts > signals and updates > plain logic > old version branches), ordered by tag and then by what they cover. The hint, the test name and the place come from the scenario's steps and the first decision no test took. Item ids are scenario ids, which are not unique across workflows. Ranking never changes a number.

### D29 — Scaffolds (milestone M10)

`internal/scaffold` writes a test from the main listed path of an item: mocks per checked call in the order the path makes them, delayed signal and cancel callbacks, input assignments solved from plain conditions on the workflow's parameters, assertions by end kind, a `// TODO:` for everything it cannot work out. The acceptance test writes the top ten scaffolds for a fixture, compiles them, runs them with the recorder and requires each scenario to be covered. A default failure is a non-retryable application error so a default retry policy cannot loop.

### D30 — Adoption tools (milestone M10)

`init` (starting config, never overwrites), `doctor` (plain-language checks, exit 1 on problems), `test --no-network` (`unshare -rn`, refuses without it, never falls back) and `--skip-file`, `diff --base` (a temporary git worktree, removed on every path out), per-test coverage (the recorder finds the `Test…` function on the goroutine waiting in `ExecuteWorkflow`; blank when parallel tests make it ambiguous; `testName` is an additive trace field), exports (`junit`, `cobertura`, `sarif`), the trend chart (inline SVG drawn by the page's template; history fields are additive) and the nightly public-corpus job (pinned commit, stored baseline, no secret).


### D31 — Branch coverage, how it was counted and how it is counted now (milestone M11)

**How branches were counted up to v0.4.0 (the audit).** A *decision* is a junction (D2, D3): an `if` that is not transparent, an error check on a Temporal call, a `switch` or type switch, a `for` or `range` loop with a Temporal call or a decision in it, a Selector's Select, or a wait-result `if`. Its *outcomes* are its exits (`true`/`false`, `failure`/`success`, one per case, `iterate`/`exit`, one per Selector callback); a loop's retry edge is not an outcome. An outcome is *covered* when a counted trace recorded that exit. `branches` counted the exits on the listed paths; `branchesAll` counted the exits some path of the whole map takes (`Graph.ReachableExits`), and the two are equal when the list is whole. The six questions:

1. Outcomes proven impossible stayed in the total: every listed path counted, whatever its bucket.
2. Known-defect outcomes were not shown apart.
3. Helper branches: with helpers copied in (`inline`), once per call site, because each copy is its own junction; with `summarize`, once per helper (a `HelperUnit`, counted separately from the workflow's branches).
4. `a && b` is one decision (its text is the whole condition); the parts were not recorded.
5. A `switch` always had a "default" outcome, written or added by PathKit when none is written (`internal/model/switch.go`), so no hidden outcome was missing.
6. Update handlers and validators are small workflows of their own with their own branch counts in the handler table; they were not in the owning workflow's total. Signal and query handlers have no map.

**Branch IDs (B7).** A branch is one outcome of one decision. Its ID is `b:` and 12 hex digits of a hash of: the file relative to the module, the function the decision is written in (a helper's own name for a helper's decision), the decision's text with whitespace collapsed, the outcome's label, and the order of identical decisions in the same function. No line numbers, no J-numbers. The IDs come only from `internal/model` (`Graph.Branches`). A helper's decision has one ID however many places call it, and the same ID with helpers copied in or summarized.

**The rules now (B2, B3).** An *outcome* that can happen is one that is not shown impossible. An outcome is **"can't happen"** (left out of the total, listed with its proof) only when the workflow is fully known (the listed paths and the pruned groups add up to every path, so a workflow over the cap removes nothing) and every listed path and pruned group through it is cannot-be-tested. It is **blocked by a known bug** (kept in the total, shown on a line of its own) when every path through it is cannot-be-tested or known-defect and one is known-defect. Needs-review outcomes stay in. An outcome a test took is never removed; one that a trace which did not count (unfinished, or fitting no path) ran is never removed either (it stays "missed"). **Helper outcomes are counted once per helper function, in every helpers mode**: the branch ID is shared by the copies and by the summarized unit, an outcome is covered when any call site covers it, and the per-call-site detail (`sites`, `sitesTaken`) is in the JSON only. A `switch` has always had its "default" outcome (written, or added when none is written), so there is no hidden outcome to add; handlers (update handlers and validators) are workflows of their own and are in the totals. **Condition coverage**: for an `if` or `for` condition that is a chain of `&&` and `||`, each atomic part (its ID `c:…`, made in `internal/model/conditions.go`) is recorded true or false when it is evaluated; the recorder (trace field `conditions`, recorder 4) wraps each part in a generic call that gives its value back unchanged, so the parts after the one that decides are never evaluated and never recorded. A trace from an older recorder says nothing about the parts ("not recorded").

**Grades and the headline (B4).** `branches.grades` (acceptable 60, commendable 75, exemplary 90; below 60 is "needs work") name the percentage; `branches.target_grade` (default commendable) is the grade "N of M workflows at <grade> or better" counts. A workflow with no outcome that can happen says "no decisions to test" and is not counted. The headline is: the branch coverage line, then two-step combinations, each part of a condition and blocked by known bugs, then the decision-check score and the possible-bug count when they exist, then one plain sentence. Ranking and advice (the to-do list of missed outcomes, `advice.MissedOutcomes`) never change a number, a bucket or a claim: an outcome's risk is that of the least risky untested path through it.

### D32 — Engine upgrades (milestone M11: E1, E2, E3, L1)

**E1.** `&x` as a direct argument of a call known to write through it during the call and not keep it (a `Get` or `Receive` of a Temporal future, channel or encoded value, `json.Unmarshal`, a decoder's `Decode`) forgets x at that call only; x is tracked before and after (an assignment after the call gives new facts). Any other `&x` (stored, passed to code PathKit does not read, in a closure, a method with a pointer receiver) keeps x untracked in the whole function.

**E2.** A function or method of the same module that takes no `workflow.Context` is summarized once (`internal/facts/summary.go`): the dynamic types of an interface result, nil-ness, a constant that every return gives (written out in the return itself, never a named constant: it may be declared in a file built on some systems only), and, for a map the function builds with constant string keys, what is stored under each key. Depth 3, a function that calls itself and calls through interfaces or function values are unknown; named results, `defer`, `go`, `recover`, a map handed to other code or used in a closure, a delete, a write under a computed key, a file with a build condition, generics: unknown. The map's per-key types are kept on the element fact of the variable (`perKey`) and dropped by any later store. Unknown means nothing is known: a path can only look more possible, never impossible.

**E3.** A type switch case for T (or `iface == const` of type T) that cannot be taken only because the value holds `*T` is a *likely defect*: needs-review with the hint "This value is a pointer (*T). Did you mean to dereference it?", no claim, and no later claim on that path (the notebook then holds a premise the code contradicts). Other impossible cases of the same switch keep impossible-type. `likely-defect` can be turned off in `detectors.disable` (the path then stays testable). A type assertion `x.(T)` on such a value is not covered yet.

**L1.** The soundness suite's scenario check reads loops: a run's external steps must match its path's fingerprint with the earlier rounds of a loop allowed to be anything and the last round exact (`Graph.FingerprintSpans`).

### D33 — The decision check (milestone M11: B8)

`pathkit check-decisions` forces a covered decision to take another outcome and re-runs the tests that went through it. Forcing is done in the marked-up copy only, by `PATHKIT_FLIP=<branch id>`: a plain `if` condition, or the error variable of an `err != nil` / `err == nil` check, goes through `pathkitFlip` / `pathkitFlipErr` (one comparison of a string with "" when the variable is not set). `model.FlipOf` is the one place that says what can be forced and why not (switches, loops and waits on several things cannot yet). Forced runs write no trace (`PATHKIT_TRACES=off`). A package whose tests fail before any change is skipped; a survivor is run a second time and a changed result is "flaky"; a survivor whose forced outcome has the same scenarios as the others is "no visible effect" (scenario model). The score is noticed / (noticed + survived).

### D34 — Reports for people who test (milestone M11: R1–R5, O5)

The default text of `report` and `coverage`, the new `branches` and `next` commands and the page start with branch coverage in plain names; the technical names are in tooltips, the glossary and the JSON, and `--technical` prints exactly the text of v0.4.0 (the tests run the old expectations through it). The older text report is the "More detail" part of the full report (its technical words rewritten by `render.Plain`). The page keeps the older page unchanged inside a closed "More detail" element. Tooltips come from the glossary entries of the page (`htmlreport/terms.go`), so a printed page, which has no tooltips, loses nothing.

### D35 — Gates on branch coverage (milestone M11: B6)

`classification.ci.branch_floor` and the baseline's branch coverage (project and per workflow) fail the check (exit 2) when branch coverage is below the floor or has fallen below the baseline by more than `ci.tolerance`. A baseline that holds branch data brings the commendable band as the floor when none is set; with `--allow-lost-tests` that default floor and a drop do not fail the check (the lost tests are listed as a warning). `pathkit diff --min-new-branch-coverage N` fails when the new or changed outcomes (a changed decision has a new branch ID) are covered below N%.
