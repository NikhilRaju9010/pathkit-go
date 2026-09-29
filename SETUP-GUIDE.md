# PathKit for Go — Setup Guide

PathKit answers one question about Temporal Go workflows: **"which execution paths exist, and which ones do my tests actually run?"**

> **Status: early development (M7 of M9 done).** `pathkit analyze` and `pathkit test` map and record all the workflow constructs PathKit v1 supports: `if`/`else`, Temporal error checks, `switch`, loops, `workflow.Selector` races, timed waits, child workflows, saga compensation and continue-as-new. `coverage` shows which paths your tests ran, and `report` gives the whole project at a glance. This guide grows with each milestone. See `PLAN.md` for progress.

> **Important: coverage is recorded only by `pathkit test`, not by plain `go test`.**
> A plain `go test` compiles your original workflow code, so it records nothing, and your tests still pass or fail as usual, which makes this easy to miss. `coverage` and `report` would then show 0% and say that no trace files were found. Always record coverage with `pathkit test`, or with the exact command `pathkit prepare` prints (section 5).

## Commands

| Command | What it does | Available |
| --- | --- | --- |
| `pathkit analyze <file>` | Lists every possible path through the workflows in one file (or a package folder, or `folder/...`). | yes (M2; all constructs since M4) |
| `pathkit test [folder \| folder/...]` | Runs your Go tests and records which path each workflow run took. | yes (M3) |
| `pathkit traces [folder \| folder/...]` | Shows which path each recorded run took (a debug view). | yes (M3) |
| `pathkit coverage <file \| folder \| folder/...>` | Shows which paths your tests ran, workflow by workflow, with a total; `--fail-under` for CI. | yes (M6) |
| `pathkit clean` | Deletes recorded trace files. | yes (M6) |
| `pathkit prepare [folder \| folder/...]` | Prints the `go test -overlay=...` command that records traces, for running `go test` yourself. | yes (M6) |
| `pathkit report [folder \| folder/...]` | The whole project at a glance: every workflow's coverage with a High/Medium/Low priority, the project total, the excluded workflows, `--summary`, `--json`. | yes (M7) |
| `pathkit --version` | Prints the installed version. | yes |

## 1. Requirements

- **Go 1.26 or newer.** (PathKit's dependencies, `golang.org/x/tools` and the Temporal Go SDK, both require Go 1.26.0.)
- A Go project with Temporal workflows (`go.temporal.io/sdk`). *(Needed from M2.)*

## 2. Install

Release binaries and `go install ...@latest` arrive with the first release (M9). Until then, build from a checkout:

```bash
git clone https://github.com/NikhilRaju9010/pathkit-go.git
cd pathkit-go
go build -o pathkit ./cmd/pathkit
./pathkit --version
```

`--version` prints just the version. A build from a git checkout shows a version made from the commit, like `v0.0.0-20260928070644-65ea0f53be27` (with `+dirty` if you have uncommitted changes); `go run` shows `dev`.

## Sample project

The repo includes a small pretend Temporal project at `testdata/pilot/` (an online shop: orders, approvals, polling, shipping, a saga with a child workflow, a subscription that continues as new, and a daily scheduled report). It's what PathKit is tested against. `testdata/pilot/EXPECTED.md` lists, by hand, every path PathKit should find and which ones the sample tests cover. To run the sample's own tests: `cd testdata/pilot && go test ./...`.

**PathKit's own tests (for contributors).** `go test ./...` at the repo root runs everything, including the slow end-to-end tests that run `pathkit test` once per sample test (about 70 seconds). For a quick check while you work, use `go test -short ./...` (about 20 seconds). It skips the slow end-to-end tests but still records the whole sample project once and checks it against `EXPECTED.md`. CI always runs the full suite, never `-short`, and a test fails if `-short` ever appears in CI's `go test` line. Before committing, run the full `go test ./...` once.

## 3. See all paths (`analyze`)

```bash
pathkit analyze testdata/pilot/orders/orders.go
```

```
Workflow: orders.OrderWorkflow
Total paths: 3

  1. Start -> if in.AmountCents <= 0 --true--> End (completed)

  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)

  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)
```

You can pass a `.go` file (only the workflows declared in that file), a package folder, or `folder/...` for every package under it. The package must compile, because PathKit uses Go's type checker to recognize Temporal calls exactly.

| Flag | Effect |
| --- | --- |
| `--summary` | Only the workflow name and total path count. Best for big workflows. |
| `--limit <n>` | Print at most `n` paths, plus a "... and N more" note. |
| `--mermaid` | Print a Mermaid diagram instead (paste into https://mermaid.live). |
| `--out <path>` | Also write exactly what was printed to a file. |
| `--config <file>` | Use this config file instead of searching for `.pathkitrc.json` (see section 6). |
| `--include a,b` / `--exclude a,b` | Only these workflows count / leave these out, for this run (section 6). |
| `--all` | Show every workflow, ignoring the config file and `--include`/`--exclude`; a line on stderr says the config was ignored. |

**Which functions are workflows:** exported functions (or methods) whose first parameter is `workflow.Context` and whose last result is `error`.

**What counts as a junction (a decision point):**
- Every `if` / `else if` has exits `true` / `false`.
- An `if err != nil` (or `err == nil`) right after a Temporal call has exits `failure` / `success`. A Temporal call here means an activity, local activity or child workflow `.Get`, a timer, `workflow.Sleep`, `workflow.Await` or `workflow.AwaitWithTimeout`. The path shows the call's name, e.g. `ChargeCard (activity)`.
- Any other error check (for example after `json.Unmarshal` or `workflow.SetQueryHandler`) is **not** a junction. PathKit follows its "no error" side.
- `//pathkit:ignore` on the `if` line or the line above makes an `if` never count (a plain `if` is then assumed false). `//pathkit:branch` makes an error check count even when it isn't after a Temporal call.
- Every `switch` and type switch with at least one `case` has one exit per `case`, labelled as written (`case "complete"`, `case "a", "b"`, `case *MyError`), plus `default`. When you don't write a `default`, PathKit adds one, because "none of the cases matched" is a real outcome. A `switch` with only a `default` isn't a junction. Falling into the next case with `fallthrough` isn't a new decision, so the path shows only the case the switch chose:

  ```
  Start -> Measure (activity) --success--> switch size --case "huge"--> if lane == "" --false--> End (completed)
  ```

  **Watch out:** when you don't write a `default` but your cases already cover every possible value, the added `default` path can never happen. It then shows as never covered and lowers your coverage. For now, write the `default` yourself (for example one that returns an error) so it's a real, testable path. (An override comment for this may come later; it doesn't exist yet.)
- A `for` or `range` loop is a junction when it contains a Temporal call (any call into the Temporal SDK) or any junction, such as an `if`. It has the exits `iterate` (go into the loop) and `exit` (the condition is false, or the range ran out). When the loop body finishes, or hits `continue`, the path goes `retry -->` back to the loop. A loop with neither a Temporal call nor a junction inside is not a junction: PathKit walks through it once.

  **The loop rule.** PathKit never counts trips. Think of a roundabout: it only matters which exit you finally took, not how many times you went round. So a loop gives at most three kinds of path:
  1. never went in: `--exit-->`;
  2. went in and left from inside, by `return` or `break`: `--iterate--> ... End`;
  3. went round, then left normally: `--iterate--> ... retry --> for ... --exit-->`.

  A test run that went round five times is matched by keeping only its **last** trip. So "the job was still pending twice, then complete" counts as the same path as "complete at once". A `for {}` loop has no `exit`; it leaves only by `break` or `return`. Example:

  ```
  Start -> for attempt <= in.MaxPolls --iterate--> CheckStatus (activity) --success--> switch status --default--> retry --> for attempt <= in.MaxPolls --exit--> End (completed)
  ```
- A `workflow.Selector`'s `Select(ctx)` is a junction: a race between the things you added to it. It has one exit per `Add…` call, named after what it waits for:
  - `signal "<name>"`: `AddReceive` on `workflow.GetSignalChannel(ctx, "<name>")`;
  - `timeout`: `AddFuture` on a `workflow.NewTimer`;
  - `activity <Name>` / `child <Name>`: `AddFuture` on an activity or child-workflow future;
  - `default`: `AddDefault`, meaning nothing was ready.

  What your callback does comes next on the path, then the code after `Select`:

  ```
  Start -> CreateLabel (activity) --success--> select (Selector) --timeout--> NotifyCustomer (activity) --success--> End (completed)
  ```

  PathKit needs the simple, common shape:
  - create the selector with `workflow.NewSelector` in the workflow function;
  - call its `Add…` methods one after another, right there (not inside an `if`, a loop or another function), each with an inline `func(...) { ... }` callback;
  - then call `Select` once (it may be inside a loop).

  Anything else is skipped with a reason, for example:

  ```
  pathkit analyze: skipping rules.SelectorNamedCallback: workflow.Selector at selectors_unsupported.go:61 is not supported: the callback of AddFuture must be an inline func literal, so PathKit can follow it
  ```
- An `if` on the "did it arrive in time?" answer is a junction:
  - `if !ok` after `ok, err := workflow.AwaitWithTimeout(...)` has the exits `signaled` / `timeout`;
  - `if ok` after `ch.ReceiveWithTimeout(...)`, or `if ch.ReceiveAsync(&v)`, has the exits `received` / `not received`.

  Write the check as plain `ok` or `!ok`; a combined condition like `ok && x > 0` is shown with ordinary `true` / `false` exits instead.

  **Testing a timer that beats an activity:** the test environment's clock doesn't jump forward while an activity runs, so make the activity slow with `env.OnActivity(MyActivity, ...).After(2 * time.Hour).Return(...)`. A timer racing a signal needs nothing special.

- A child workflow is one step, labelled with its name, e.g. `PaymentWorkflow (child workflow)`, with the exits `failure` / `success` when you check its error. PathKit doesn't follow into the child's own code; it has its own map. A child mocked with `env.OnWorkflow` in a test records nothing for the child.
- **Saga compensation is a note, not a branch.** A `defer` that starts an activity, local activity or child workflow (usually "undo the earlier steps if we fail") puts `[compensation (defer)]` after the end station of every path that gets past that `defer`:

  ```
  Start -> ReserveInventory (activity) --success--> PaymentWorkflow (child workflow) --failure--> End (failed) [compensation (defer)]
  ```

  Why not a branch? Whether the undo runs depends on how the workflow ended: it runs on failures and not on success. Counting "undo ran / didn't run" as separate paths would invent impossible paths (such as "the order succeeded **and** was undone") that no test could ever cover. Other `defer`s, such as `defer cancel()`, get no note.

**How paths end:** `End (completed)` for `return ..., nil`; `End (failed)` for a returned error; `End (continued-as-new)` for `workflow.NewContinueAsNewError`. It's a plain `End` when PathKit can't tell.

**Never supported:** `goto`, and Go's own `select` statement (Temporal workflows must use `workflow.Selector`). The note says why:

```
pathkit analyze: skipping rules.UsesLabel: goto at unsupported.go:21 is not supported: PathKit maps break, continue and return, but not goto
```

## 4. Record which paths your tests run (`pathkit test`)

Run your normal tests through PathKit. No changes to your test files are needed:

```bash
cd testdata/pilot
pathkit test ./...
```

```
ok  	example.com/pilot/fulfillment	0.086s
ok  	example.com/pilot/orders	0.086s
ok  	example.com/pilot/reports	0.081s
...
pathkit test: recorded 8 traces (8 complete) in .pathkit/traces
```

What happens:
1. PathKit makes a marked-up copy of each workflow file in `.pathkit/overlay/`. The copy writes down every junction exit the run passes.
2. It runs `go test -overlay=...`, so Go compiles the copies instead of your files. **Your source files and `go.mod` are never changed.**
3. Each workflow run writes one small JSON trace file to `.pathkit/traces/`.

Line numbers stay the same in the copy, so test failures and panics still point at your real file and line.

| Flag | Effect |
| --- | --- |
| `--traces <dir>` | Where to write trace files (default: the config's `"traces"`, else `.pathkit/traces`). |
| `--config`, `--include`, `--exclude` | Which workflows are recorded (section 6). Excluded workflows' tests still run; they just aren't recorded. |
| `--keep-traces` | Keep old trace files. By default each run first deletes the old ones (only `*.trace.json` files). |
| `-- <go test flags>` | Everything after `--` goes to `go test`, e.g. `pathkit test ./... -- -run TestOrder -v`. |

`pathkit test` always runs the tests fresh (it adds `-count=1` unless you pass your own `-count`), because cached test results would record nothing. If a test fails, the traces from the other tests are still kept and PathKit exits 1.

Then see which path each run took:

```bash
pathkit traces ./...
```

```
orders.OrderWorkflow: path 1 (J1.true → End (completed))
orders.OrderWorkflow: path 2 (J1.false J2.failure → End (failed))
orders.OrderWorkflow: path 3 (J1.false J2.success → End (completed))
...
8 traces: 8 matched
```

The path numbers are the ones `pathkit analyze` prints. A trace can also show as:
- **`unmatched`**, naming the step that doesn't fit the workflow's map
- **`stale`**, when the workflow changed since the run was recorded (re-run `pathkit test`)
- **`incomplete`**, when the run panicked, timed out, or never reached a `return`

A trace doesn't know which test produced it; to see one test's path, run only that test (`-- -run '^TestName$'`).

Add `.pathkit/` to your `.gitignore`. It holds PathKit's marked-up copies (`.pathkit/overlay/`) and your traces. Go's own build and test commands never look inside it, but `gofmt -l .` does, and lists the copies as unformatted. That's harmless; to keep gofmt's output clean, run it on your tracked files only (`gofmt -l $(git ls-files '*.go')`) or on your source folders.

## 5. See your coverage (`coverage`)

After `pathkit test`, ask which paths your tests ran:

```bash
pathkit coverage testdata/pilot/...
```

```
Workflow: orders.OrderWorkflow
Total paths: 3
Covered: 3/3 (100.0%)
Branches: 4/4 (100.0%)

Covered paths:
  1. Start -> if in.AmountCents <= 0 --true--> End (completed)
  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)
  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)
Untested paths:
  (none)
...
38 paths total · 20 covered · 18 missed · 52.6% coverage
Branches: 39/50 (78.0%)

Traces: 21 read · 21 counted · 0 unmatched · 0 stale · 0 incomplete · 0 excluded · 0 unknown workflow · 0 unreadable
```

- **A path counts as covered** when at least one recorded run took it. Running it twice still counts once. The path numbers are the ones `analyze` prints.
- **Loops:** a run that went round a loop several times counts as the path of its last trip (the loop rule in section 3).
- **Branches** count exits instead of whole paths: of all the exits on the listed paths, how many some run took. Branch coverage uses each run's raw steps (before loop folding), so a branch can show as taken even when no covered path uses it. It is shown to help you, but `--fail-under` never looks at it.
- **The scope** (section 6) applies: excluded workflows are listed with their reasons at the end, and their traces count as "excluded".

| Flag | Effect |
| --- | --- |
| `--traces <dir>` | Trace folder (default: the config's `"traces"`, else `.pathkit/traces`, the same place `pathkit test` writes). |
| `--function <name>` | Only this workflow. |
| `--fail-under <percent>` | Exit code **2** when path coverage is below it (config: `"failUnder"`; the flag wins). The report is still printed first. |
| `--allow-stale` | Also count traces recorded for an older version of a workflow, when they still fit a path (config: `"allowStale"`). |
| `--json` | Print JSON instead (shape below). |
| `--out <file>` | Also write exactly what was printed to a file. |
| `--clean` | Delete the trace files after the report. Only when the report was produced (exit 0 or 2), never after an error. |
| `--config`, `--include`, `--exclude` | Scope, as in section 6. |

**Exit codes:**
- `0`: all good;
- `1`: a real error (bad argument, no trace files, a workflow in scope that can't be analyzed, …);
- `2`: coverage is below `--fail-under`.

If the rounded number would look the same as the threshold, PathKit prints more decimals so you can see why: `coverage 66.67% is below --fail-under 66.7%`.

**Traces that don't count are always shown, never dropped.** Each one gets a warning on stderr, and the `Traces:` line counts them. They never change the exit code.

| Kind | What it means | Fix |
| --- | --- | --- |
| `stale` | The workflow's code changed after this run was recorded (comments and formatting don't count). Not counted. | Re-run `pathkit test`. For a change you know is cosmetic (a renamed variable), pass `--allow-stale`: a stale trace that still fits a path then counts, and that path is marked `(stale trace)`. One that no longer fits is reported as unmatched. |
| `unmatched` | The run doesn't fit any path; the warning names the exact step where it left the map. | Usually the workflow changed; re-record. If it persists, it's a PathKit bug worth reporting. |
| `incomplete` | The run never finished (panic, timeout, or stopped). | Look at that test. |
| `excluded` | The workflow is out of scope. | Nothing (it's listed on purpose). |
| `unknown workflow` | No such workflow in what you asked `coverage` about. | Point `coverage` at the right folder. |
| `unreadable` | Not a valid trace file (or a different trace format version). | Delete it and re-record. |

**`--json`** prints one object:
- `schemaVersion` (1) and `tool`;
- `workflows`: for each, `name`, `addedByConfig`, `paths` and `branches` (each `{total, covered, percent}`), `truncated`, and `pathList` (for each path: `number`, `id`, `text`, `steps`, `end`, `compensation`, `covered`, `traces`, `staleTrace`);
- `excluded`: `[{name, reason}]`;
- `total` and `branches`;
- `traces`: the counts;
- `failUnder`: `{threshold, passed}`, or `null` when no threshold is set.

`percent` has one decimal; use `covered`/`total` for exact math.

**Cleaning up.** `pathkit test` clears old traces at the start of each run (unless `--keep-traces`). To delete them yourself:

```bash
pathkit clean                    # every *.trace.json in the trace folder
pathkit clean --older-than 7d    # only ones older than 7 days (also 12h, 30m)
```

Only `*.trace.json` files are ever deleted.

**Running `go test` yourself (`pathkit prepare`).** If your CI or IDE runs `go test` directly, let PathKit write its marked-up copies and print the command to use:

```bash
pathkit prepare ./...
go test -overlay=/your/project/.pathkit/overlay/overlay.json -count=1 ./...
```

Only that command records traces; a plain `go test` records nothing. Run `prepare` again after you change a workflow, because the copies are made from the code as it was when you ran it.

## 5b. The whole project at a glance (`report`)

`coverage` is the close-up of each workflow. `report` is the **term report for the whole project**: one block per workflow, a priority label saying where tests are most needed, the project total, the workflows left out and why, and what happened to every trace file. Its numbers are exactly `coverage`'s: both commands use the same code to count, and a test checks they agree field by field.

```bash
cd testdata/pilot
pathkit test ./...
pathkit report ./...
```

```
approval.ApprovalWorkflow (approval/approval.go)
2/4 paths · 50.0% · branches 4/6 · priority Medium
  1. Start -> AwaitWithTimeout (wait) --failure--> End (failed): missed
  2. Start -> AwaitWithTimeout (wait) --success--> if !ok (AwaitWithTimeout) --signaled--> if decision == "approved" --true--> End (completed): covered
  ...
orders.OrderWorkflow (orders/orders.go)
3/3 paths · 100.0% · branches 4/4 · priority Low
  1. Start -> if in.AmountCents <= 0 --true--> End (completed): covered
  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed): covered
  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed): covered
...
38 paths total · 20 covered · 18 missed · 52.6% project coverage
Branches: 39/50 (78.0%)

0 workflows excluded (no scope in use)
Traces: 21 read · 21 counted · 0 unmatched · 0 stale · 0 incomplete · 0 excluded · 0 unknown workflow · 0 unreadable
```

With `--summary`, each workflow gets one line:

```
approval.ApprovalWorkflow             2/4 paths · 50.0% · priority Medium
billing.SubscriptionWorkflow          2/3 paths · 66.7% · priority Medium
fulfillment.OrderFulfillmentWorkflow  2/4 paths · 50.0% · priority Medium
fulfillment.PaymentWorkflow           2/4 paths · 50.0% · priority Medium
orders.OrderWorkflow                  3/3 paths · 100.0% · priority Low
polling.ReportPollingWorkflow         3/5 paths · 60.0% · priority Medium
reports.DailyReportWorkflow           3/6 paths · 50.0% · priority Medium
shipment.ShipmentWorkflow             3/9 paths · 33.3% · priority High

38 paths total · 20 covered · 18 missed · 52.6% project coverage
...
```

**Priority labels are per workflow, not per path.** Each one comes from that workflow's path coverage, and every untested path of the workflow shares its label. They tell a tester where to spend the next test:

| Label | Path coverage | What it means for you |
| --- | --- | --- |
| **High** | below 50% | Most of this workflow's paths have never run in a test. Write tests here first. |
| **Medium** | 50% to 80%, both included | The main paths are tested, but several paths (often failures and timeouts) are not. Add tests for the paths marked `missed`. |
| **Low** | above 80% | Well covered. Close the last gaps when convenient. |

The boundaries are exact: 4 of 5 paths is exactly 80%, so it's Medium, and 79.99% is Medium too, while 80.01% is Low. When a % would print as "80.0%" or "50.0%" without being exactly that, more decimals are shown (`79.99%`), so the label never looks wrong. A workflow with no paths at all has no label.

**The total** adds up the paths of every in-scope workflow (it never averages percentages). **The excluded line is always printed**, even when nothing is excluded, so nobody can raise the % quietly:

```
1 workflow excluded by .pathkitrc.json (see "reason"):
  shipment.ShipmentWorkflow: needs a real carrier sandbox
```

It names what excluded them: `.pathkitrc.json`, `--include`, `--exclude`, or `(no scope in use)`.

**Which folder.** `pathkit report ./...` or `pathkit report some/folder` (on Windows, `.\...` and `some\folder\...` work too). With no folder, `report` uses **every** entry of the config's `"packages"` (a workflow listed twice counts once), else `./...`. A single `.go` file is refused, because `report` is for the whole project; use `coverage` for one file.

**`report` stops with exit code 1** rather than print a number that leaves something out:
- when a package doesn't compile: every broken package is named, each with its first error. Fix them, or leave them out of the folder pattern or `"packages"`.
- when a workflow in scope can't be analyzed: fix it, or exclude it with a reason (section 6).

| Flag | Effect |
| --- | --- |
| `--summary` | One line per workflow instead of every path. |
| `--json` | Print JSON instead (shape below). Config: `"json"`. |
| `--out <file>` | Also write the report to a file, **always plain text** (no colour codes). Config: `"out"`, relative to the config file. |
| `--no-color` | No colour. Colour (`covered` green, `missed` red; High red, Medium yellow, Low green) appears only on a real terminal, never when piped or redirected, and never with `--no-color`, config `"noColor": true`, or the `NO_COLOR` environment variable set to anything. The words are always there; colour is decoration only. |
| `--traces`, `--fail-under`, `--allow-stale`, `--clean` | As for `coverage` (section 5). `--fail-under` checks the project's path coverage. |
| `--config`, `--include`, `--exclude`, `--all` | Scope, as in section 6. `--all` ignores the scope and shows every workflow. |

A flag always beats the config file, including `--json=false`. The exit codes are the same as for `coverage`: `0` fine, `1` a real error, `2` below `--fail-under` (the report is printed first).

**`report --json`** has exactly `coverage --json`'s shape (section 5), with three additions:
- each workflow also has `file` (where it's declared, always with forward slashes, `orders/orders.go`, even on Windows, so the JSON is the same on every system; the text report uses your system's own separator) and `priority` (`"High"`, `"Medium"`, `"Low"`, or `null` when it has no paths);
- at the top, `excludedBy` lists what excluded workflows (`[".pathkitrc.json", "--exclude"]`, or `[]`).

Everything else (`workflows`, `excluded`, `total`, `branches`, `traces`, `failUnder`) means exactly the same as in `coverage --json`.

## 6. Choose which workflows count (`.pathkitrc.json`)

Some workflows can't be tested yet (say, they need a real bank sandbox). Counting them would drag your coverage down and make the number mean less. A `.pathkitrc.json` file says which workflows count. Think of a report card where some subjects aren't graded this term: they're listed as "not graded, because …", and the average is taken over the rest.

```json
{
  "packages": ["./internal/workflows/..."],
  "workflows": {
    "exclude": [
      { "name": "LegacyBillingWorkflow", "reason": "needs real bank sandbox" }
    ]
  }
}
```

**Where PathKit finds it:**
- **Default:** in the folder you run PathKit from, then its parent folders, up to the folder that holds your `go.mod`. A file above your module is never used.
- **`--config <file>`:** uses that file instead.
- **No file:** every workflow counts, just as without a config.
- **Paths inside the file** (such as `packages`) are relative to the file's own folder.

**`workflows.exclude`: leave workflows out.** Each entry needs a `"reason"`. `analyze` lists every excluded workflow with its reason at the end, so nothing disappears silently:

```
Excluded from scope (1), pass --all to show them:
  shipment.ShipmentWorkflow: needs a real carrier sandbox
```

**`workflows.include`: only these count.** Every other workflow PathKit finds is listed as excluded with the reason `not in include list`.

`include` can also name a function the automatic rule misses: an unexported one, or one with no `error` result. Its first parameter must be `workflow.Context`. It is then labelled, like this:

```
Workflow: scope.lowerFlow (added by config)
```

**Names** can be written as `OrderWorkflow`, `orders.OrderWorkflow`, `Service.Run` or `orders.(*Service).Run`. A name that matches nothing, or more than one workflow, is an error: a typo must never quietly change what counts.

**One-off runs:**
- `--include a,b` replaces the config's include list for this run.
- `--exclude a,b` leaves workflows out with the reason `excluded by --exclude flag`.
- `analyze --all` ignores the config and the flags.

**Scope changes what is counted, never what is tested.** `pathkit test` still runs every test; it records only the workflows in scope.

**Example with the sample project:** excluding `ShipmentWorkflow` changes the project from 38 paths with 20 covered (52.6%) to 29 paths with 17 covered (58.6%). The number went up only because something was taken out. That's why every exclusion shows its reason, and why `report` always prints how many workflows were excluded (section 5b).

**A workflow in scope that PathKit can't analyze** (for example one using `goto`) is always printed as `in scope but not analyzable: … (fix it, or exclude it in .pathkitrc.json with a reason)`. `coverage` and `report` refuse to run while one is in scope (exit 1), so the percentage always covers exactly what's in scope.

**All keys:**

| Key | Meaning | Works |
| --- | --- | --- |
| `packages` | Where your workflows are (default `./...` from the file's folder). Names are checked against these. It is also the folder `pathkit test`/`traces` use when you give none (only when it lists one). | now |
| `traces` | Trace folder (default `.pathkit/traces`). | now |
| `workflows.include` / `workflows.exclude` | See above. | now |
| `failUnder`, `allowStale` | Coverage threshold (0–100) / accept traces from changed code (see section 5). | now (`coverage` and `report`) |
| `out`, `json`, `noColor` | `report` output: write to a file (relative to the config file), print JSON, no colour (section 5b). A flag always wins. | now (`report`) |
| `html` | `true` or a file path for the HTML report. | checked now, used from M8 |

Keys marked "checked now" are validated, so a typo or wrong type is still reported, but they do nothing yet, and no command claims to apply them. (`out`, `json` and `noColor` in the config are for `report` only; `coverage` uses only its own `--out` and `--json` flags.) Any other key (such as `"workflow"` instead of `"workflows"`) is an error.

## 7. Errors and exit codes

Every error is one line on stderr, in the form `pathkit <command>: <message>`.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | A real error (bad argument, missing file, ...). The message says what. |
| `2` | The report was produced, but coverage is below `--fail-under`. Lets CI tell "coverage too low" apart from "PathKit failed". |

| Message | Cause | Fix |
| --- | --- | --- |
| `missing <file> argument` / `missing <dir> argument` | No path given. | Pass the file or folder. |
| `unknown flag: --xyz` | Misspelled or unsupported flag. | Check the spelling (`pathkit <command> --help` lists flags). |
| `pathkit: unknown command "xyz"` | Misspelled command. | Use `analyze`, `test`, `traces`, `coverage` or `report`. |
| `no trace files found in <dir>; coverage is recorded only by "pathkit test" ...` | No runs were recorded, usually because the tests ran with plain `go test`. | Run `pathkit test`. |
| `go test failed (exit status 1)` | One of your tests failed under `pathkit test`. | Fix the test; traces from passing tests are still kept. |
| `expected a package folder or folder/..., not a file` | `pathkit test` runs packages, not single files. | Pass the folder. |
| `expected a folder or folder/..., not a file: ... (report covers a whole project; ...)` | `report` is for whole projects. | Pass the folder, or use `pathkit coverage <file>`. |
| `cannot record <workflow>: ... a name pathkit needs` | Your code already uses one of pathkit's generated names. | Rename yours (see LIMITATIONS.md). |
| `Workflow file not found: ...` / `Directory not found: ...` | Wrong path. | Check the path and your working directory. |
| `package does not compile: <package>: <file>:<line>:<col>: <error>` | PathKit needs code that builds. The message names the package and its first real compile error. | Fix that error (`go build ./...` shows them all). |
| `3 packages do not compile: <package>: <error>; <package>: <error>; ...` | Several packages under a `folder/...` don't build. PathKit stops rather than skip them, so no workflow drops out of the numbers unnoticed. Each broken package is named once, with its first error. A package that only *imports* a broken one is not listed. | Fix them, or leave those folders out of the pattern you pass. |
| `... has no exported workflow functions to analyze` | Nothing in that file matches the workflow rule above. | Check the function is exported, takes `workflow.Context` first and returns `error` last. |
| `no workflows could be analyzed` | Every workflow found was skipped (see the `skipping ...` lines). | Rewrite the construct named in the `skipping` line (see "Never supported" above), or analyze another file. |
| `skipping <workflow>: goto at ... is not supported: ...` | The workflow uses `goto` or Go's `select`, which PathKit never maps. | Rewrite with `break`/`continue`/`return`, or `workflow.Selector`; other workflows are still analyzed. |
| `<path>/.pathkitrc.json: invalid JSON at line 3, column 32: ...` | The config file isn't valid JSON. | Fix the file at that line (a missing comma, say). |
| `<path>/.pathkitrc.json: unknown key "workflow"` | A misspelled key. | Check the spelling against section 6. |
| `<path>/.pathkitrc.json: workflows.include is empty: ...` | `"include": []` would be ambiguous. | Remove it, or list the workflows that count. |
| `<path>/.pathkitrc.json: exclude "X" needs a "reason"` | Every exclusion must say why. | Add `"reason": "..."`. |
| `... include "X" matches no function ...` / `... exclude "X" matches no workflow ...` | A misspelled or missing name. | Fix the name; `pathkit analyze --all` lists every workflow. |
| `... include "sendEmail" is not a workflow: its first parameter is not workflow.Context` | `include` can only add workflow-shaped functions. | Remove it from `include`. |
| `... matches 2 functions (a.Run, b.Run); write it with its package ...` | A short name is ambiguous. | Write it as `a.Run`. |
| `in scope but not analyzable: <workflow>: ...` | The workflow uses something PathKit never maps. | Fix it, or exclude it with a reason. |
| `no workflows in scope to analyze (every workflow is excluded)` | The scope left nothing to show. | Check `include`/`exclude`, or use `--all`. |
| `coverage 52.6% is below --fail-under 80%` (exit 2) | Path coverage is under your threshold. | Write tests for the untested paths listed above it. |
| `invalid --fail-under value: ...` | `--fail-under` needs a number from 0 to 100. | e.g. `--fail-under 80`. |
| `invalid --older-than value: ...` | `pathkit clean --older-than` needs an age. | e.g. `7d`, `12h`, `30m`. |
| `... matches no workflow ...; did you mean "ShipmentWorkflow"?` | A typo in a workflow name (config, `--include`/`--exclude`, `--function`). | Use the suggested name. |
| `invalid --limit value: ...` | `--limit` needs a positive whole number. | e.g. `--limit 20`. |
| `--limit ignored because --summary was passed.` | Both flags together. A note only. | Use one or the other. |

Flags can go before or after the file name: `pathkit analyze orders.go --summary` and `pathkit analyze --summary orders.go` mean the same thing.
