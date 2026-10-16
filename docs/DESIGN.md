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
- **Principles.** When unsure, a path stays testable or goes to needs-review, never to cannot-be-tested. A path a test ran is always testable, whatever was claimed. Every override and rule needs a reason. A detector's claim in a workflow that has a trace fitting no path is not trusted (needs-review, label `model-limit`).
- **Detectors** read only the facts the extractor attached to the junctions (`internal/model/facts*.go`), never source text, and never coverage. They return `certain` only when they are sure; anything less (`likely`) becomes needs-review. Each one is conservative: a near-miss it must not claim is tested next to every case it must claim.
- **Coverage numbers.** Testable coverage is covered testable paths over testable paths (needs-review paths count as testable under the default policy `count_as_testable`, and are left out under `exclude`, except untrusted claims, which always count). Overall coverage is covered paths over all paths. Branch coverage is unchanged.
- **The classification block** prints, in this order: the bucket counts with labels and where they came from, testable coverage, special-test coverage, overall coverage, and the basis `--fail-under` uses. With classification off the report is exactly the report without it.
- **Additive output.** The JSON and HTML only gain fields; existing keys and values stay the same, and a test proves it.

### Trace file format (schemaVersion 1)

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
| `functionHash` | 16 hex digits: the start of a SHA-256 of the workflow function's code, printed by `go/printer` without comments (so formatting and comments don't change it). Computed only by `model.FunctionHash`. A different hash means the function changed, and the trace is **stale**. |
| `status` | `complete` if the run **finished** a `return` of the workflow function, with all return values evaluated (a `return` that is reached but panics while computing its values is not complete, so the mark is set only after the values are evaluated). Otherwise `incomplete`: a panic (including one while computing a return value), the SDK stopping the run with `runtime.Goexit`, or a timeout. Only complete traces are matched. |
| `steps` | The exit IDs the run passed, in order, exactly as `model` produced them (`J1.false`, …). Every string written here came from `Graph.ExitFor(...).String()` at instrument time. |
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

**Config.** `classification.engine.enabled` (default true) turns the engine off; `detectors.disable` accepts `value-flow` (no engine claims) and `possible-correlation` (no suspects).

**How it is kept honest.** Shape fixtures with hand-written answers, a safety fixture for each forget rule, and `internal/soundness`: random programs, run for every input, fail the build if a path called impossible is ever taken.

## Non-goals for v1

- **One package at a time for analysis.** `analyze` takes a file (analyzes the workflows in it, but type-checks its whole package, since Go needs that) or a package; `report` takes package patterns like `./...` and analyzes each package separately.
- **No following calls into other packages or into child workflows.** A helper function in another package, or a child workflow started with `ExecuteChildWorkflow`, is shown as a single step.
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

