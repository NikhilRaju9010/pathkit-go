# PathKit for Go — Setup Guide

PathKit answers one question about Temporal Go workflows: **"which execution paths exist, and which ones do my tests actually run?"**

> **Status: early development (M4 of M9 done).** `pathkit analyze` and `pathkit test` map and record all the workflow constructs PathKit v1 supports: `if`/`else`, Temporal error checks, `switch`, loops, `workflow.Selector` races, timed waits, child workflows, saga compensation and continue-as-new. `coverage` and `report` are not implemented yet; `pathkit traces` shows recorded runs in the meantime. This guide grows with each milestone. See `PLAN.md` for progress.

> **Important: coverage is recorded only by `pathkit test`, not by plain `go test`.**
> A plain `go test` compiles your original workflow code, so it records nothing, and your tests still pass or fail as usual, which makes this easy to miss. `coverage` and `report` would then show 0% and say that no trace files were found. Always record coverage with `pathkit test`. *(`pathkit prepare`, for adding the overlay flag to your own `go test` command, arrives in M6.)*

## Commands

| Command | What it does | Available |
| --- | --- | --- |
| `pathkit analyze <file>` | Lists every possible path through the workflows in one file (or a package folder, or `folder/...`). | yes (M2; all constructs since M4) |
| `pathkit test [folder \| folder/...]` | Runs your Go tests and records which path each workflow run took. | yes (M3) |
| `pathkit traces [folder \| folder/...]` | Shows which path each recorded run took (a debug view until `coverage` exists). | yes (M3) |
| `pathkit coverage <file> --traces <dir>` | Shows which of one workflow's paths your tests ran. | coming in M6 |
| `pathkit report <dir> --traces <dir>` | The same across every workflow in a project, with a project-wide total. | coming in M7 |
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
| `--traces <dir>` | Where to write trace files (default `.pathkit/traces`). |
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

## 5. Errors and exit codes

Every error is one line on stderr, in the form `pathkit <command>: <message>`.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | A real error (bad argument, missing file, ...). The message says what. |
| `2` | The report was produced, but coverage is below `--fail-under`. *(From M6.)* Lets CI tell "coverage too low" apart from "PathKit failed". |

| Message | Cause | Fix |
| --- | --- | --- |
| `missing <file> argument` / `missing <dir> argument` | No path given. | Pass the file or folder. |
| `missing required --traces <dir> argument` | `coverage`/`report` need the trace folder. | Add `--traces <dir>`. |
| `unknown flag: --xyz` | Misspelled or unsupported flag. | Check the spelling (`pathkit <command> --help` lists flags). |
| `pathkit: unknown command "xyz"` | Misspelled command. | Use `analyze`, `test`, `traces`, `coverage` or `report`. |
| `no trace files found in <dir>; coverage is recorded only by "pathkit test" ...` | No runs were recorded, usually because the tests ran with plain `go test`. | Run `pathkit test`. |
| `go test failed (exit status 1)` | One of your tests failed under `pathkit test`. | Fix the test; traces from passing tests are still kept. |
| `expected a package folder or folder/..., not a file` | `pathkit test` runs packages, not single files. | Pass the folder. |
| `cannot record <workflow>: ... a name pathkit needs` | Your code already uses one of pathkit's generated names. | Rename yours (see LIMITATIONS.md). |
| `not implemented yet (planned for Mx)` | The command exists but its work arrives in a later milestone. | Wait for that milestone. |
| `Workflow file not found: ...` / `Directory not found: ...` | Wrong path. | Check the path and your working directory. |
| `package does not compile: ...` | PathKit needs code that builds. | Fix the compile error shown (run `go build ./...`). |
| `... has no exported workflow functions to analyze` | Nothing in that file matches the workflow rule above. | Check the function is exported, takes `workflow.Context` first and returns `error` last. |
| `no workflows could be analyzed` | Every workflow found was skipped (see the `skipping ...` lines). | Rewrite the construct named in the `skipping` line (see "Never supported" above), or analyze another file. |
| `skipping <workflow>: goto at ... is not supported: ...` | The workflow uses `goto` or Go's `select`, which PathKit never maps. | Rewrite with `break`/`continue`/`return`, or `workflow.Selector`; other workflows are still analyzed. |
| `invalid --limit value: ...` | `--limit` needs a positive whole number. | e.g. `--limit 20`. |
| `--limit ignored because --summary was passed.` | Both flags together. A note only. | Use one or the other. |

Flags can go before or after the file name: `pathkit analyze orders.go --summary` and `pathkit analyze --summary orders.go` mean the same thing.
