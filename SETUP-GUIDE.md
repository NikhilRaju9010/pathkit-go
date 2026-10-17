# PathKit for Go — Setup Guide

PathKit answers one question about Temporal Go workflows: **"which execution paths exist, and which ones do my tests actually run?"**

> **Status: early development, no tagged release yet.** `pathkit analyze` and `pathkit test` map and record every workflow construct PathKit supports: `if`/`else`, Temporal error checks, `switch`, loops, `workflow.Selector` races, timed waits, child workflows, saga compensation and continue-as-new. `coverage` shows which paths your tests ran, `report` gives the whole project at a glance, `--html` turns it into one shareable web page, and path classification tells impossible paths apart from real gaps.

> **Important: coverage is recorded only by `pathkit test`, not by plain `go test`.**
> A plain `go test` compiles your original workflow code, so it records nothing, and your tests still pass or fail as usual, which makes this easy to miss. `coverage` and `report` would then show 0% and say that no trace files were found. Always record coverage with `pathkit test`, or with the exact command `pathkit prepare` prints (section 5).

## Commands

| Command | What it does |
| --- | --- |
| `pathkit analyze <file>` | Lists every possible path through the workflows in one file (or a package folder, or `folder/...`). |
| `pathkit test [folder \| folder/...]` | Runs your Go tests and records which path each workflow run took. |
| `pathkit traces [folder \| folder/...]` | Shows which path each recorded run took (a debug view). |
| `pathkit coverage <file \| folder \| folder/...>` | Shows which paths your tests ran, workflow by workflow, with a total; `--fail-under` for CI. |
| `pathkit clean` | Deletes recorded trace files. |
| `pathkit prepare [folder \| folder/...]` | Prints the `go test -overlay=...` command that records traces, for running `go test` yourself. |
| `pathkit report [folder \| folder/...]` | The whole project at a glance: every workflow's coverage with a High/Medium/Low priority, the project total, the excluded workflows, `--summary`, `--json`; `--html` for a shareable web page. |
| `pathkit explain <stableId> [folder]` | Says why one path got its bucket and label, and who decided (section 5). |
| `pathkit --version` | Prints the installed version. |

## 1. Requirements

- **Go 1.26 or newer.** (PathKit's dependencies, `golang.org/x/tools` and the Temporal Go SDK, both require Go 1.26.0.)
- A Go project with Temporal workflows (`go.temporal.io/sdk`).

## 2. Install

Prebuilt binaries are not provided. Build from a checkout: the install steps are in the [README](README.md#install).

`--version` prints just the version. A build from a git checkout shows a version made from the commit, like `v0.0.0-20250101000000-abcdef123456` (with `+dirty` if you have uncommitted changes); `go run` shows `dev`.

## Sample project

The repo includes a small pretend Temporal project at `testdata/pilot/` (an online shop: orders, approvals, polling, shipping, a saga with a child workflow, a subscription that continues as new, and a daily scheduled report). It's what PathKit is tested against. `testdata/pilot/EXPECTED.md` lists, by hand, every path PathKit should find and which ones the sample tests cover. To run the sample's own tests: `cd testdata/pilot && go test ./...`.

**PathKit's own tests (for contributors).** `go test ./...` at the repo root runs everything, including the slow end-to-end tests that run `pathkit test` once per sample test (a few minutes on a typical laptop). For a quick check while you work, use `go test -short ./...` (much quicker). It skips the slow end-to-end tests but still records the whole sample project once and checks it against `EXPECTED.md`. CI always runs the full suite, never `-short`, and a test fails if `-short` ever appears in CI's `go test` line. Before committing, run the full `go test ./...` once.

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
| `--mermaid` | Print a Mermaid diagram instead (paste into https://mermaid.live; that sends the diagram text, which includes your decision text, to that website, so use a local Mermaid viewer if that matters). |
| `--out <path>` | Also write exactly what was printed to a file. |
| `--config <file>` | Use this config file instead of searching for `.pathkitrc.json` (see section 8). |
| `--include a,b` / `--exclude a,b` | Only these workflows count / leave these out, for this run (section 8). |
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

  **Watch out:** when you don't write a `default` but your cases already cover every possible value, the added `default` path can never happen. It then shows as never covered and lowers your coverage. For now, write the `default` yourself (for example one that returns an error) so it's a real, testable path. (Or, with classification switched on, mark that path with a `//pathkit:untestable` comment: see "Marking paths as untestable".)
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
  pathkit analyze: in scope but not analyzable: rules.SelectorNamedCallback: workflow.Selector at selectors_unsupported.go:61 is not supported: the callback of AddFuture must be an inline func literal, so PathKit can follow it
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
pathkit analyze: in scope but not analyzable: rules.UsesLabel: goto at unsupported.go:21 is not supported: PathKit maps break, continue and return, but not goto (fix it, or exclude it in .pathkitrc.json with a reason)
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
pathkit test: recorded 21 traces (21 complete) in .pathkit/traces
```

What happens:
1. PathKit makes a marked-up copy of each workflow file in `.pathkit/overlay/`. The copy writes down every junction exit the run passes.
2. It runs `go test -overlay=...`, so Go compiles the copies instead of your files. **Your source files and `go.mod` are never changed.**
3. Each workflow run writes one small JSON trace file to `.pathkit/traces/`.

Line numbers stay the same in the copy, so test failures and panics still point at your real file and line.

Flags for `pathkit test`:

| Flag | Effect |
| --- | --- |
| `--traces <dir>` | Where to write trace files (default: the config's `"traces"`, else `.pathkit/traces`). |
| `--config`, `--include`, `--exclude` | Which workflows are recorded (section 8). Excluded workflows' tests still run; they just aren't recorded. |
| `--keep-traces` | Keep old trace files. By default each run first deletes the old ones (only `*.trace.json` files). |
| `-- <go test flags>` | Everything after `--` goes to `go test`, e.g. `pathkit test ./... -- -run TestOrder -v`. |

`pathkit test` always runs the tests fresh (it adds `-count=1` unless you pass your own `-count`), because cached test results would record nothing. If a test fails, the traces from the other tests are still kept and PathKit exits 1.

Then see which path each run took:

```bash
pathkit traces ./...
```

```
approval.ApprovalWorkflow: path 2 (J1.success J2.signaled J3.true → End (completed))
approval.ApprovalWorkflow: path 4 (J1.success J2.timeout → End (completed))
billing.SubscriptionWorkflow: path 1 (J1.iterate J2.failure → End (failed))
...
21 traces: 21 matched
```

The path numbers are the ones `pathkit analyze` prints. A trace can also show as:
- **`unmatched`**, naming the step that doesn't fit the workflow's map
- **`stale`**, when the workflow changed since the run was recorded (re-run `pathkit test`)
- **`incomplete`**, when the run panicked, timed out, or never reached a `return`

A trace doesn't know which test produced it; to see one test's path, run only that test (`-- -run '^TestName$'`).

Add `.pathkit/` to your `.gitignore`. It holds PathKit's marked-up copies (`.pathkit/overlay/`) and your traces. Go's own build and test commands never look inside it, but `gofmt -l .` does, and lists the copies as unformatted. That's harmless; to keep gofmt's output clean, run it on your tracked files only (`gofmt -l $(git ls-files '*.go')`) or on your source folders.

### What PathKit writes (and never changes)

**PathKit never modifies the project it reads.** It does not change your source files, `go.mod`, `go.sum` or tests. It writes only:

- to the `.pathkit/` folder in the folder you run it from (the marked-up copies and the recorded traces); add `.pathkit/` to your `.gitignore`;
- to files and folders you ask for with a flag or a config key: `--write-baseline` (the baseline file), `--html` (the page, plus a `report-history.json` next to it), `--out` (a copy of the output) and `--traces` (where the traces go).

## 5. See your coverage (`coverage`)

After `pathkit test`, ask which paths your tests ran:

```bash
pathkit coverage testdata/pilot/...
```

```
Workflow: approval.ApprovalWorkflow
Total paths: 4
Covered: 2/4 (50.0%)
Branches: 4/6 (66.7%)

Covered paths:
  2. Start -> AwaitWithTimeout (wait) --success--> if !ok (AwaitWithTimeout) --signaled--> if decision == "approved" --true--> End (completed)
  4. Start -> AwaitWithTimeout (wait) --success--> if !ok (AwaitWithTimeout) --timeout--> End (completed)
Untested paths:
  1. Start -> AwaitWithTimeout (wait) --failure--> End (failed)
  3. Start -> AwaitWithTimeout (wait) --success--> if !ok (AwaitWithTimeout) --signaled--> if decision == "approved" --false--> End (completed)
...
38 paths total · 20 covered · 18 missed · 52.6% coverage
Branches: 39/50 (78.0%)

0 workflows excluded (no scope in use)
Traces: 21 read · 21 counted · 0 unmatched · 0 stale · 0 incomplete · 0 excluded · 0 unknown workflow · 0 unreadable
```

- **A path counts as covered** when at least one recorded run took it. Running it twice still counts once. The path numbers are the ones `analyze` prints.
- **Loops:** a run that went round a loop several times counts as the path of its last trip (the loop rule in section 3).
- **Branches** count exits instead of whole paths: of all the exits on the listed paths, how many some run took. Branch coverage uses each run's raw steps (before loop folding), so a branch can show as taken even when no covered path uses it. It is shown to help you, but `--fail-under` never looks at it.
- **The scope** (section 8) applies: the excluded line is always printed (even `0 workflows excluded (no scope in use)`), followed by each excluded workflow with its reason, in exactly the same wording as `report` (section 6). Their traces count as "excluded".

| Flag | Effect |
| --- | --- |
| `--traces <dir>` | Trace folder (default: the config's `"traces"`, else `.pathkit/traces`, the same place `pathkit test` writes). |
| `--function <name>` | Only this workflow. |
| `--fail-under <percent>` | Exit code **2** when path coverage is below it (config: `"failUnder"`; the flag wins). The report is still printed first. |
| `--coverage-basis testable\|overall` | Which coverage `--fail-under` checks (see "Testable and overall coverage" below). Default: `overall`, or `testable` when the config switches classification on. |
| `--allow-stale` | Also count traces recorded for an older version of a workflow, when they still fit a path (config: `"allowStale"`). |
| `--json` | Print JSON instead (shape below). |
| `--out <file>` | Also write exactly what was printed to a file. |
| `--clean` | Delete the trace files after the report. Only when the report was produced (exit 0 or 2), never after an error. |
| `--config`, `--include`, `--exclude` | Scope, as in section 8. |

**Exit codes:** `0` all good, `1` a real error, `2` coverage below `--fail-under` or the CI check failed. The full table is in section 9.

If the rounded number would look the same as the threshold, PathKit prints more decimals so you can see why: `coverage 66.67% is below --fail-under 66.7%`.

**Traces that don't count are always shown, never dropped.** Each one gets a warning on stderr, and the `Traces:` line counts them. They never change the exit code.

| Kind | What it means | Fix |
| --- | --- | --- |
| `stale` | The workflow's code changed after this run was recorded (comments and formatting don't count). Not counted. | Re-run `pathkit test`. For a change you know is cosmetic (a renamed variable), pass `--allow-stale`: a stale trace that still fits a path then counts, and that path is marked `(stale trace)`. One that no longer fits is reported as unmatched. |
| `unmatched` | The run doesn't fit any path; the warning names the exact step where it left the map. | Usually the workflow changed; re-record. If it persists, it's a PathKit bug worth reporting. |
| `incomplete` | The run never finished (panic, timeout, or stopped). | Look at that test. |
| `excluded` | The workflow is out of scope. | Nothing (it's listed on purpose). |
| `unknown workflow` | No such workflow in what you asked `coverage` about. | Point `coverage` at the right folder. |
| `unreadable` | Not a valid trace file (or a different trace format version). Traces recorded by an older PathKit (trace format 1) are one warning for all of them: "traces are from an older PathKit ... re-run `pathkit test`". | Delete it and re-record (`pathkit test`). |

**`--json`** prints one object:
- `schemaVersion` (1) and `tool` (this is the version of the report, not of the trace files);
- `workflows`: for each, `name`, `addedByConfig`, `paths` and `branches` (each `{total, covered, percent}`), `truncated`, and `pathList` (for each path: `number`, `id`, `text`, `steps`, `end`, `compensation`, `covered`, `traces`, `staleTrace`);
- `excluded`: `[{name, reason}]`;
- `total` and `branches`;
- `traces`: the counts;
- `failUnder`: `{threshold, passed, basis}`, or `null` when no threshold is set;
- added with path classification (everything above is unchanged): `classification`, `coverageBasis` and `classificationShown` at the top; `classification` inside each workflow; `stableId` (a path's id that survives unrelated edits; explained under "`stableId`" below), `bucket`, `label`, `source`, `confidence`, `possibleInProduction`, `explanation` and `ruleId` on each path. See "Testable and overall coverage" below;
- added with claim proofs: `proof` on a path that an automatic detector made a claim about, needs-review claims included (a list of `{step, file, line, text}`; `step` is the 1-based step on the path, and 0 with no `file`/`line` for the closing sentence). A path with no detector claim has no `proof` key, and neither does a claim PathKit no longer trusts because the workflow has a trace that fits no path. It is the same reasoning `pathkit explain` prints as `Proof:`;
- added with the path-cap warning, only when a workflow reached the 2000-path cap: `pathCapWarning`: `{limit, workflows, message}`. It says that the workflow's path list and classification are incomplete (see "Path enumeration stops at 2000 paths per workflow" in LIMITATIONS.md). Absent otherwise.

`percent` has one decimal; use `covered`/`total` for exact math.

### Testable and overall coverage (path classification)

Path classification tells the paths a test **can** run apart from paths that can never run (for example two checks of the same unchanged value that contradict each other). Every path is put in one **bucket**: `testable`, `cannot-be-tested`, `needs-special-test` or `needs-review`, and gets a reason label. Two coverage numbers follow from it:

- **Testable coverage:** covered testable paths out of testable paths. This is the number a team works on.
- **Overall coverage:** covered paths out of all paths. Nothing is hidden. It is the number `coverage` and `report` have always shown.

**How paths are sorted:** every path starts as `testable`. With classification enabled, the automatic detectors (below), your rules and your overrides can move a path into another bucket; until something does, the two numbers are equal. A path that a real test ran is always `testable`, whatever anything else says.

To see the new block in the text and HTML reports, either pass `--coverage-basis testable` (or `overall`), or switch it on in `.pathkitrc.json`:

```json
{ "classification": { "enabled": true } }
```

Without either, the text reports and the `--fail-under` message are exactly what they were before. `--all` ignores the config file, so with `--all` the config's switch does nothing (a `--coverage-basis` flag still works). The block looks like this:

```
Classification:
  Total paths:         38
  Testable:            38
  Cannot be tested:    0
  Needs special test:  0
  Needs review:        0
Testable coverage:     20 / 38 = 52.6%  (testable paths only)
Special-test coverage: 0 / 0
Overall coverage:      20 / 38 = 52.6%  (all paths)
Coverage basis used:   testable (for --fail-under)
```

### Marking paths as untestable (path classification)

Some paths can never run, and PathKit cannot always tell by itself. You can tell it, in three ways. A marked path stays on the map and stays in overall coverage; it only leaves testable coverage. All of this applies only when `"classification": {"enabled": true}` is set. Every override, rule and comment needs a **reason**; without one PathKit stops with an error that names the entry.

**1. By path, in `.pathkitrc.json`.** Find the path's `stableId` with `pathkit coverage <folder> --json` (or `report --json`), then:

```json
{
  "classification": {
    "enabled": true,
    "paths": {
      "mark_untestable": [
        { "workflow": "billing.SubscriptionWorkflow", "stable_id": "s1:59aa014811bd",
          "label": "manual", "reason": "input count is never 0",
          "added_by": "example-user", "added_on": "2026-10-09" }
      ],
      "mark_testable": [
        { "workflow": "orders.OrderWorkflow", "stable_id": "s1:0123456789ab",
          "reason": "PathKit was wrong about this one" }
      ]
    }
  }
}
```

- `workflow` is written like in `include`/`exclude` (`OrderWorkflow`, `orders.OrderWorkflow`); a misspelled one is an error with a hint. `label` is optional (default `manual`) and must be a label whose bucket is cannot-be-tested; `added_by` and `added_on` (a date, `2026-10-09`) are optional notes.
- `mark_testable` forces a path to stay testable, whatever a rule or comment says.

**2. By rule.** A rule says that a combination of decisions is impossible:

```json
"rules": [
  { "id": "polls-at-least-once", "workflow": "ReportPollingWorkflow", "label": "loop-minimum",
    "when": ["for attempt <= in.MaxPolls --exit"], "without": ["for attempt <= in.MaxPolls --iterate"],
    "impossible": true, "reason": "MaxPolls is always at least 1 (checked by the caller)" }
]
```

(The `without` part is explained below: here it means "the loop was skipped", not "the loop ran and then finished".)

`when` entries are decisions written exactly as `pathkit analyze` prints them: the decision's text, then `--`, then the exit (`if o.AmountCents > 100_000 --true`). A path is marked when it takes **all** of them; extra spaces don't matter. `"impossible": true` is required (a rule can only say "impossible"). The label must be a cannot-be-tested label. The first matching rule is the main reason; others are listed as `extraReasons` in the JSON.

**`without` (optional).** A list of decisions the path must **not** take. A rule marks a path only when it takes every `when` decision and none of the `without` ones. This is how to say "this loop was skipped": a skipped loop takes `--exit` and never `--iterate`, but a loop that ran and then finished also takes `--exit`.

```json
{ "id": "second-loop-needs-first", "workflow": "BuiltTwoLoops", "label": "same-list-loops",
  "when": ["for range in.Items --exit", "for range timers --iterate"],
  "without": ["for range in.Items --iterate"],
  "impossible": true, "reason": "timers gets one timer per item, so it is empty when the first loop did not run" }
```

A misspelled `without` entry is an **error** (it would make the rule match more paths); a misspelled `when` entry only makes the rule match nothing, and it is reported as stale.

**Decisions with the same text.** When a workflow has several decisions with exactly the same text (three `if more` checks, one per signal), `analyze` prints them all the same, but a rule must say which one it means:
- `"if more #2 --false"` the second one; `"if more #1 --false"` the first (the number counts in source order);
- `"any: if more --false"` any of them.

Write the number with the count, `"if more #2/3 --false"` ("the second of 3"), to be warned when the number may have shifted: if the workflow has another number of decisions with that text, the rule is **not applied** and a warning says why, until you check it and update the entry. (The text of the decisions is the same by definition, so the count is what can change.)

A plain `"if more --false"` is an **error** in such a workflow, and the message lists the decisions it could mean. A plain entry stays fine when its text is unique.

**3. By comment, in your code.** On the decision's line, or the line above it:

```go
//pathkit:untestable exit="true" reason="express is never combined with a zero amount"
if express && amount == 0 {
```

`exit` is the exit label as `analyze` prints it (`true`, `failure`, `case "legacy"`; put a `\"` for a quote inside it), and both `exit` and `reason` are required. Every path that takes that exit at that decision is marked. A comment PathKit cannot use (no reason, an unknown exit, nothing to mark on that line) makes the workflow "not analyzable" with the file and line, so `coverage` and `report` stop until it is fixed: a comment that silently did nothing would hide a decision someone made. This is **not** `//pathkit:ignore`, which takes a decision off the map entirely.

**Which one wins** (strongest first): `mark_testable`/`mark_untestable` in the config, then a comment, then a rule, then the automatic detectors, then testable. The strongest is the main reason; the others that also applied are shown as `extraReasons`.

**In the reports,** a path marked by a rule, an override or a comment is followed by a tag that names the source, e.g. `[cannot-be-tested: loop-minimum · manual · rule "polls-at-least-once"]` (or `config override`, or `//pathkit:untestable`). The HTML page shows the same tag.

**Two safety checks, both warnings on stderr (exit code unchanged):**
- **Conflict.** If a test ran a path that something marked untestable, the path **stays testable** and PathKit says so: `... was classified cannot-be-tested (Marked untestable in the config (manual): ...), but a test ran it; counting it as testable`. A real run proves a path is possible.
- **Stale.** An entry whose `stable_id` matches no path of the current code, or a rule none of whose paths takes all its decisions, is reported: `classification: stale entry: billing.SubscriptionWorkflow s1:... (reason) matches no path of the current code`. Usually the code changed (a changed condition changes the `stableId`), or the id was mistyped. Entries for a workflow you excluded, or did not pick with `--function`, are not checked. **The path cap:** PathKit lists at most 2000 paths per workflow. For a workflow with more, an entry that matches none of the listed ones is reported as `beyond the path cap` instead of stale, because it may simply not be listed.

**What you see.** The classification block lists the reasons under each bucket with how many were decided automatically and by a person, and where the markings came from:

```
Classification:
  Total paths:         38
  Testable:            36
  Cannot be tested:    2
    manual: 2 [auto 0 | manual 2]
  Needs special test:  0
  Needs review:        0
  Config overrides: 2 paths marked untestable, 0 paths marked testable
Testable coverage:     20 / 36 = 55.6%  (testable paths only)
Special-test coverage: 0 / 0
Overall coverage:      20 / 38 = 52.6%  (all paths)
Coverage basis used:   testable
```

**Other keys of the section.** `custom_labels` (`[{"name": "my-label", "definition": "...", "bucket": "cannot-be-tested"}]`) defines your own label for overrides and rules; its name may not be a built-in one. `unclassified_policy` is `count_as_testable` (default: paths PathKit is unsure about count in testable coverage) or `exclude`.

**If you set entries but not `"enabled": true`,** they are ignored, and PathKit says so on stderr. `--all` ignores the whole config file, the classification section too.

### Automatic detectors (path classification)

With `"classification": {"enabled": true}` PathKit also marks some paths by itself, where the code makes the answer clear. These show as `source: auto` and say why. A person's override, comment or rule always wins; the detector's finding is then listed as an extra reason. A path a test ran is never marked: it stays testable and PathKit warns.

| Detector | What it marks | Bucket |
| --- | --- | --- |
| `closed-signal-channel` | The "channel closed" side (`more == false`) of a receive on a channel from `workflow.GetSignalChannel`. The Temporal SDK and server do not close signal channels (checked in the SDK source). Only code can: a type assertion that turns the channel into one that can be closed (PathKit looks for this and then makes no claim), or a custom worker interceptor that hands the workflow a closed channel (PathKit does not look for this). It is an assessment, not a proof. | cannot-be-tested |
| `constant-condition` | The side of an `if` that can never run because the compiler knows the condition (`if false`, `if debug` with a `const`). A package-level `var` is not a constant. A value that depends on the machine (`runtime.GOOS`, `strconv.IntSize`, `math.MaxInt`, shifts and `^`), a constant from another package, and a constant declared in a file with a build constraint are not trusted: those paths stay testable. | cannot-be-tested |
| `version-gate-old-branch` | The old side of a `workflow.GetVersion` check (`v == workflow.DefaultVersion`, `v < N`, `v >= N`, a `switch v` case older than the newest version). Normal runs get the newest version. Reach it with `env.OnGetVersion(changeID, min, max).Return(workflow.DefaultVersion)`. | needs-special-test |
| `history-limit` | The side taken when `GetInfo(ctx).GetContinueAsNewSuggested()` is true. Reach it with `env.SetContinueAsNewSuggested(true)`. A comparison of `GetCurrentHistoryLength()` or `GetCurrentHistorySize()` with a number is only a hint and goes to needs-review (fake it with `SetCurrentHistoryLength` / `SetCurrentHistorySize`). A `for` loop condition is not read. | needs-special-test |
| `contradictory` | Two plain `if` decisions that test the **same value**, unchanged in between, where the path takes opposite answers (`if valid {…}` … `if !valid {…}`; `if len(in.Items) > 3 {…}` twice). The value must be a constant, a parameter, a field of a parameter nobody writes, or a value computed once; two calls of the same function are two values. Decisions inside a loop are not paired. A result filled in by `Get(ctx, &res)` is handled by `contradictory-get-result`. | cannot-be-tested |
| `same-list-loops` | One range loop is skipped (empty list) and another range loop over the **same list** runs, or the other way round. The list must be a slice, array or string that is the same value in both (`range in.Items` twice, `items := in.Items` then two loops). A map, a re-assigned or appended list, and a re-sliced list are not claimed. | cannot-be-tested |
| `list-built-from-list` | A loop that appends one element per trip to a local list (`var futures []workflow.Future` … `futures = append(futures, f)`), and a later loop over that list: one is skipped and the other runs. The list must start empty, have this one append as its only write, nothing before the append in the loop body may `break`, `continue`, `return` or jump, and neither loop may be inside another loop or a closure. | cannot-be-tested |
| `loop-exit-before-first-trip` | A `for cond { … }` loop (no init, no post) with `cond` or `!cond` on a local plain `bool` that is a constant when the loop starts and makes the condition true (`done := false; for !done { … }`), left without running its body. Not claimed when the variable is written, has its address taken or is used in a closure before the loop. | cannot-be-tested |
| `value-flow` | A path that the **fact engine** proves impossible by following it step by step and keeping notes on what each local value holds. Labels: `contradictory` (the same unchanged value, tested twice, opposite answers; includes `len(x) == 0` against `x == ""` when the first answer was not an early return), `constant-condition` (the answer follows from a declaration or constant with no decision in between, such as `var s string` then `if s == ""`), and `value-flow` (the answer follows through a copy, an assignment, an early return or a skipped assignment, such as `id := ""`, `if ok { id = v }`, `if id == ""`). It forgets a value when it cannot be sure: its address is taken (`&x`, `Get(ctx, &x)`, `Receive(ctx, &x)`), a pointer-receiver method is called on it, a closure writes it, a loop assigns it, it comes from a call, or the code is in an `if` or loop PathKit does not decide. A function with `goto`, `recover` or `unsafe` is left alone. `pathkit explain` shows the proof. | cannot-be-tested |
| `impossible-type` | A type switch case, or a type assertion, that no type the value can hold takes. The value is one the function built (a map or slice that only received a `string` and a `*string`, an interface variable assigned a `string`), or one Temporal decoded (assumption `default-json-converter`: an `interface{}` filled by the default JSON converter holds only a string, float64, bool, nil, `[]interface{}` or `map[string]interface{}`). `pathkit explain` shows the stores that built the list of types. | cannot-be-tested |
| `impossible-nil` | `v == nil` taken true, for an interface that holds a typed value: a nil `*string` stored in an `interface{}` makes a non-nil interface. For a map element, only when the key is proven to be in the map (a missing key gives nil). | cannot-be-tested |
| `unknown-input-type` | An untested path takes a type switch case on a value PathKit cannot follow, and no test has taken that case. The reports list these by type switch (file:line, number of paths): one test per switch usually settles a whole group. Never for a path a test ran. | needs-review |
| `possible-correlation` | An untested path that asks about the same value twice and would be impossible if nothing had changed it, but PathKit had to forget the value in between (a loop that assigns it, an `if` or loop PathKit does not decide; for a value whose address is taken, two checks with no call in between). PathKit cannot tell, so it asks a person: needs-review, naming both steps. Never set for a path a test ran. | needs-review |
| `contradictory-get-result` | A value Temporal fills in once with `fut.Get(ctx, &res)` (a local struct or pointer-to-struct variable, `var res T`), read by two decisions after the Get, that together need one bool field to be both true and false. Reads inside `&&` (when true) and `\|\|` (when false) count: `if res.Approved` false and `if res.Approved && other` true is impossible. Refused when anything else could write the result: a second Get, another `&`, an assignment, `++`, a method call on it (pointer receivers take the address themselves), a copy of the pointer, a closure, a `defer` or loop around the Get. | cannot-be-tested |

Reading the `more` result: `Receive` (its only result), `ReceiveWithTimeout` and `ReceiveAsyncWithMoreFlag` (their second result) and the `more` parameter of an `AddReceive` callback, tested as `!more`, `more == false`, `more != true` (either order), or as the `else` side of `if more`. Anything compound (`!more && x > 0`) is not read.

Where the channel came from is followed only in the simple, certain cases: a local variable set once with `:=` or `var x = ...`, or declared with no value and assigned once (`var ch workflow.ReceiveChannel` then `ch = workflow.GetSignalChannel(...)`, in the same block, before any use, never in a closure), from `GetSignalChannel` / `GetSignalChannelWithOptions` (signal) or `NewChannel` / `NewBufferedChannel` (the workflow's own). A channel the workflow makes itself can be closed by the workflow, so its closed side stays **testable**. A channel passed in as a parameter, kept in a struct field or a slice, returned by a helper, assigned more than once, or made outside the callback that uses it, turned into another type (`ch.(workflow.Channel)`), passed to another function, or a named result read before it is assigned, is **unknown**, and an unknown origin goes to **needs-review**, never to cannot-be-tested.

**Turning a detector off.** Each one needs a reason, and every report then says the detector is off:

```json
{ "classification": { "enabled": true,
  "detectors": { "disable": [ { "name": "closed-signal-channel", "reason": "our tests close signal channels with a fake" } ] } } }
```

A misspelled detector name is an error that lists the real names. The fact engine's three names are `value-flow`, `possible-correlation` and `unknown-input-type`; to turn the whole engine off write `"engine": { "enabled": false }` inside `classification` (the default is on).

**The assumption `no-custom-interceptor`.** A map or slice you pass to an activity, a child workflow or a signal (`workflow.ExecuteActivity`, `ExecuteChildWorkflow`, `SignalExternalWorkflow`) stays as it was after the call, because the SDK turns the arguments into bytes when you make the call; so a later type switch or length check on it can be settled. Those claims say `no-custom-interceptor` in their reason, in the text report (`Assumption no-custom-interceptor: on; N claims rest on it`), the HTML glossary and the JSON (`assumptions`, in `coverage` and in `report`). A local activity (`ExecuteLocalActivity`) does not copy its arguments, so a map passed to one is not followed. If your code registers an interceptor (an `Interceptors` option on the client or worker, a type built on the SDK's interceptor base types, or an `InterceptWorkflow` method), PathKit switches this off by itself and says so; to switch it off yourself: `"classification": { "assumptions": { "no_custom_interceptor": "off" } }`.

**The assumption `default-json-converter`.** Claims about values Temporal decodes assume the default JSON converter (see the `impossible-type` row). PathKit reads every Go file of the module (code, not comments or text) for a client or worker option named `DataConverter`, a call of `NewCodecDataConverter` or `NewCompositeDataConverter`, and a type with the methods of a data converter (`ToPayloads` and `FromPayloads`), a payload converter (`ToPayload` and `FromPayload`) or a payload codec (`Encode` and `Decode` taking payloads); if it finds one, the rule is off for the whole run, and the text report (`Assumption default-json-converter: OFF, because ...`), the HTML glossary and the JSON (`assumptions`: name, on, why, claims) all say so. When it is on and claims rest on it, the same places say how many. To turn it off yourself: `"classification": { "assumptions": { "default_json_converter": "off" } }` (`"auto"`, the default, is on unless the search finds a hit). `UseNumber` and `json.Number` do not switch it off: they change only what the decoder that uses them produces. Separately, a value that your workflow decodes itself with `json.Unmarshal`, or with `Decode` on a decoder made in the same function by `json.NewDecoder` that is never handed on and never calls `UseNumber`, holds only a string, float64, bool, nil, `[]interface{}` or `map[string]interface{}`: that is how the standard library works, so claims about it name no assumption and stay on even with the rule off.

### Helper functions (the workflow's own package)

A workflow often calls functions of its own package that start activities or wait on timers (`o.charge(ctx)`, `reserve(ctx, items)`). PathKit follows those calls: the decisions inside the helper are decisions of the workflow, one set for each call, and show in `analyze` and `coverage` as `Sleep (timer) [in reserve]`. The error check after the call (`if err != nil`) is a decision too, with only the exit the helper's `return` allows. A helper with no call into the Temporal SDK (a validation function with `if`s) stays ordinary code, one step, as before. What is and is not followed, and how deep, is in LIMITATIONS.md ("Helper functions of the workflow's own package").

Following helpers can multiply the paths of a workflow (every helper with an activity and its error check doubles the paths after it). If that would pass the path cap (2000 paths), PathKit does not follow the helpers of **that** workflow: each call stays one step, as before, and a warning says so (`helper functions are not followed: ...`). The other workflows are not affected.

What changes for you:

- **Only workflows that call such helpers change**: their paths, `J` numbers and stable ids differ (a decision in a helper is numbered right after its call; the stable text names the helper and the call). A workflow that calls none keeps every id as it was. A `stable_id` in the config that no path has any more is reported as stale, and the warning lists the paths that replace it when helper functions are the reason.
- **`pathkit test` marks up the helper code too** (in its copy, never in your files) and records the trace format version 2. Traces recorded by an older PathKit are refused with one message: run `pathkit test` again.
- **After upgrading, do this:** run `pathkit test` again (old traces are not counted, and until you do, coverage reads 0% with one warning saying why); read the warnings for any workflow that says its helpers are not followed; for saved `stable_id` entries and baselines, look for "stale entry" warnings, which now name the paths that replace a vanished id. A `//pathkit:branch` you added on an error check after a helper is not needed any more when the helper is followed.
- **Marks and rules work in a helper.** `//pathkit:untestable` on a decision in a helper marks that decision in every copy; a rule's `when` entry for it is written as `analyze` prints it.

### Two workflows with the same name in different folders

If two folders each have a package called `dup` with a workflow `Work`, PathKit calls them `a/dup.Work` and `b/dup.Work` (the shortest unique end of the import path) everywhere: reports, JSON, traces, `include`, `exclude`, `--function` and classification overrides. A short name that fits both (`dup.Work`, `Work`) is an error that lists both. A workflow that is alone with its name keeps the plain name.

### The HTML report's glossary and detector audit (path classification)

When the classification block is shown (`--coverage-basis`, or `"classification": {"enabled": true}`), `report --html` adds, below the paths and linked from the bar at the top ("Glossary", "Detector audit"):

- **A label is a link.** Every label in the summary table, the audit and the definitions jumps to its definition. In a path's details row stand the **bucket**, the **label**, who decided (`auto` or `manual`, then `detector <name>`, `rule "<id>"`, `config override`, or `//pathkit:untestable`) and PathKit's reason. Everything is plain text as well as colour, and links and buttons are keyboard focusable.
- **Definitions.** For each label used in the run: what it means, why, an example (and a real example from this run), whether it is possible in production, who decides and how sure, what to do, how many paths have it, and links to those paths (the first 10). Labels not used in this run are in a collapsed group. A label PathKit never sets by itself is marked **"Not detected yet"** (the list is in LIMITATIONS.md; a test keeps the two in step). Custom labels from the config are listed too; the built-in text never changes. The footer shows the **definitions version**, a short hash of the wording, so a saved report says which wording it used.
- **Detector audit.** A paginated table of every path that a **detector** (not a rule and not a person) marked **cannot-be-tested**, with its label, detector, confidence and one-line reason, so you can spot-check the automatic decisions against the code. The path number opens the path in its workflow. Paths a detector sent to needs-review or needs-special-test are counted below the table.

The page loads nothing and uses only links inside the page. With classification off there is no glossary, no audit, and no bucket or label columns.

**`--fail-under` and the basis.** Until classification is switched on, `--fail-under` checks overall coverage, exactly as before. With `--coverage-basis`, or with classification enabled in the config (then the default basis is `testable`), it checks the basis you chose, and the report and the exit-code message say which: `coverage 66.7% is below --fail-under 70% (basis: testable)`. The JSON always says (`coverageBasis`, `failUnder.basis`).

**A trace that fits no path.** When a test run takes a road that PathKit's map of that workflow does not have (the stderr line says `fits no path`), the map is known to be incomplete from the step where the run left it, so PathKit does not trust a "cannot be tested" claim made by a detector or a config rule in **that workflow** that rests on that step or a later one: those paths are shown as `needs-review` (label `model-limit`), which counts as testable, so the number cannot go up. A claim whose reasoning uses only steps the run did follow (the run went through the first three decisions as the map says, so a claim about decisions one and two stays) keeps its label; the message names the step. A claim made by a detector that does not say which steps it uses is not trusted whenever the workflow has such a trace. Your own decisions (`mark_untestable` entries and `//pathkit:untestable` comments) are kept, with a warning. To make the claims count again: fix the map (for example `//pathkit:branch` on the `if`), re-record, or accept the trace in the config. The report prints the trace's *steps id* (`t1:` and 12 hex digits):

```json
"classification": {
  "enabled": true,
  "explained_traces": [
    { "workflow": "helpererr.Flow", "steps_id": "t1:32285e72cef2",
      "reason": "the run returns early when parse fails; the error comes from a helper" }
  ]
}
```

A `reason` is required. The warning for a trace that fits no path prints a ready-made entry under it, with `"reason": "TODO"`; copy it into `explained_traces` and write the real reason. A reason of `TODO` is refused when the config is read, because accepting a trace is a decision and the reason is shown in every report. The report keeps listing the trace, marked *acknowledged* with the reason (`unexplainedTraces` in `--json`, a line in the classification block and in the HTML). An entry that matches no unmatched trace in the current run is reported as stale. The steps id changes if the workflow's decisions are renumbered, so an acknowledgement belongs to one recording of one version of the code.

**`stableId`.** Each path has an `id` (unchanged: it is built from the junction numbers J1, J2, …, so it changes when a junction is added or removed) and a new `stableId` such as `s1:3fa94c0d17ab`. The `stableId` is built from the *words* of each decision and the exit taken, not from numbers, so adding or removing an unrelated junction does not change it. It is only unique inside one workflow, so always keep the workflow name next to it. Two junctions with exactly the same text are told apart by `#2`, `#3`. The `s1:` prefix is the version of the recipe. Editing the text of a condition does change the `stableId` of the paths through it.

### The CI check: baseline file and testable floor (path classification)

Only with `"classification": {"enabled": true}`. Without it nothing below happens, `--baseline` and `--write-baseline` are refused, and `--fail-under` works exactly as before.

**The idea.** A project starts with gaps it already knows about. The CI check should not fail on those every day; it should fail when something **new** needs attention. So you record the current state once in a **baseline file**, commit it, and every later CI run compares itself with it.

```json
"classification": {
  "enabled": true,
  "ci": { "baseline": "pathkit-baseline.json", "fail_under_testable": 80 }
}
```

- `baseline` is the file, relative to the folder of `.pathkitrc.json`. `--baseline <file>` names another one for one run.
- `fail_under_testable` (0 to 100) is a floor for **testable coverage**. It is checked on its own, next to `--fail-under` (which keeps its own meaning).

**Write the baseline** (and again whenever you have looked at the new paths and accept them):

```bash
pathkit report --write-baseline          # prints: wrote the baseline pathkit-baseline.json: 38 paths in 8 workflows
```

It holds every listed path's `stableId`, bucket and whether a test ran it, sorted, so a re-written file only changes where something really changed. It must hold every workflow in the config's scope, so `--write-baseline` exists only on `pathkit report`, run with **no folder** (it then uses every entry of the config's `packages`), and is refused together with a folder, `--include`, `--exclude` or `--all`. Do not put `--all` in a CI script: it ignores the config, and with it the CI check. Likewise a one-off `--exclude` or `--include` on the check run leaves those workflows out of the comparison (they are listed as excluded); exclude workflows in the config, with a reason, instead.

**Check against it** (what CI runs): a plain `pathkit report` (or `coverage`, which checks only the workflows it measures). It exits **2**, after printing the whole report, when:

1. a **testable path with no test** is not in the baseline as testable: a new path, a new workflow, or a path that was cannot-be-tested before;
2. a **needs-review path** is not in the baseline as needs-review (for example a detector or rule claim that is no longer trusted because a trace fits no path);
3. a path that **had a test** when the baseline was written has **no test now** (a test was deleted, or changed so it takes another road): it fails even if coverage is still above the floor. Pass `--allow-lost-tests` to let it pass; it is then printed as a warning. Each line says the path's bucket now (`now testable`, `now cannot-be-tested`, ...), so a path that was marked impossible after its test was removed, or that also appears in the needs-review list, is easy to tell apart. `--allow-lost-tests` is refused when there is no baseline to compare with. A path that no longer exists (its stable id is gone, because the code changed) is not "lost";
4. testable coverage is below `fail_under_testable`.

Each problem is printed on stderr with the paths it is about (the path and id below only illustrate the shape; on the sample project that path is covered):

```
pathkit report: 1 testable path with no test not in the baseline pathkit-baseline.json:
  orders.OrderWorkflow path 3 (s1:5179b1c99bbb, new): Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)
```

`--json` adds a `ci` object (`baseline`, `baselineMode`, `floor`, `newUntested`, `newNeedsReview`, `lostTests`, `lostTestsAllowed`, `passed`); it is absent when the check is off. A missing or damaged baseline file is exit 1.

**Exit code 2 has two causes:** coverage below `--fail-under` (or `failUnder`), **or** the CI check above failed (baseline or floor). Both print the report first and then say on stderr which one it was; several reasons are all printed. Exit 1 is always a real error (PathKit could not do its job), never a coverage problem.

**Numbered rule entries (`#2`).** A rule entry like `if more #2 --false` names "the second decision with this text". If someone later adds or removes an identical decision above it, `#2` quietly points at another one: the text cannot show it, because identical decisions have identical text. So the baseline file also records, for every numbered entry (`#1`, `#2`, `#2/3`; not `any:`), where its decision sits on the map: what comes right before it (`before`), where each of its exits leads (`after`), and how many decisions have that text (`count`). A later check prints a **warning** when that changed:

```
pathkit report: warning: rule "second-needs-first" (orders.W) entry "if a || b #2 --true": since the baseline was written, the decision it names changed place (it came right after ..., now after ...). An identical decision may have been added, removed or moved, so the number may point at a different decision now; check the rule, then write the baseline again
```

It is a warning only (the exit code does not change), and it needs a baseline: without `classification.ci.baseline` (or `--baseline`) there is nothing to compare with. A baseline written before this existed has no anchors and gives no warning until it is written again. Adding or removing **any** decision with the same text warns (even one below it, which does not change what `#2` means), and so does an unrelated edit right next to the decision (a new `if` just above it, or a renamed activity it leads to): the warning can fire when `#2` is still right; check, then write the baseline again.

### Why does a path have its label? `pathkit explain` (path classification)

```bash
pathkit explain s1:5179b1c99bbb            # every package of the config, else ./...
pathkit explain s1:5179b1c99bbb ./orders   # one folder
```

Take the stable id from `report --json` / `coverage --json` (`stableId`), from the CI check's message, or from the HTML report. It prints, in plain words (this example is illustrative: it shows a path with a rule on it, which the sample project does not have):

```
s1:5179b1c99bbb is path 3 of orders.OrderWorkflow (orders/orders.go)
  Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)

Tested:      no; no recorded run took this path
Bucket:      cannot-be-tested: ...
Label:       contradictory: ...
Decided by:  a person: config rule "never-charged" in /path/.pathkitrc.json
Why:         Rule "never-charged": ...
What to do:  ...
```

When a detector decided, a `Proof:` block follows `Why:`. It is the reasoning in numbered plain sentences. A sentence about one decision on the path names its step and its place in the code (`[step 2, orders.go:41]`); a sentence about code between two decisions (a declaration, an assignment) names only its line (`[orders.go:30]`); the last sentence says what the steps add up to. The step number is the position of the decision on the path (the first decision is step 1). For example, for a path that takes `if valid` true and later `if !valid` true: `1. J1 (if valid) takes the true side. [step 1, orders.go:30]`, `2. J2 (if !valid) takes the true side, but it tests the same value as the step before. [step 2, orders.go:36]`, `3. The value cannot change in between, so the two steps cannot give these two answers.` A claim a person made (config rule, override or comment) has no proof block: the person's reason is the explanation.

"Decided by" is one of: a `detector <name>` (automatic, with how sure it is), a config rule, a config override (`mark_untestable` / `mark_testable`), a `//pathkit:untestable` comment, PathKit itself (a claim it does not trust because the workflow has a trace that fits no path), or nobody (testable by default). `Also:` lists other rules or detectors that say the same thing. When a test ran the path, it always counts as testable, and a `Note:` names the rule or detector whose claim is therefore wrong. With classification off, a `Note:` says so. Trace files are optional (without them `Tested:` says it does not know). A stable id is only unique inside one workflow; if it fits paths of several, each is explained (`--function` picks one).

### "How to cover it" hints: `--hints` (path classification)

`pathkit report --hints` and `pathkit coverage --hints` add, under every **testable** path no test ran, what a test must make happen, one line per decision on the path, then how the run ends:

```
  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed): missed
       how to cover it:
         - make `in.AmountCents <= 0` false
         - let activity ChargeCard succeed (mock it with env.OnActivity, passing the same activity the workflow starts (ChargeCard), with .Return(...); or let it run)
         - expect the workflow to complete
```

The hints name the Temporal test tools for each kind of decision: `env.OnActivity(...).Return(...)` with a non-retryable error for an activity that must fail, `env.OnWorkflow` for a child workflow, `env.RegisterDelayedCallback` + `env.SignalWorkflow` for a signal that must win a select or an await, `.After(...)` for an activity that must lose to a timer, and so on. They say *what the code must see* (for example "make `o.AmountCents > 100_000` true"), not how to build your input. Loops read "skip the loop entirely", "go into it", "let that trip end normally", "then leave it". PathKit keeps only the **last** trip of a run through a loop, so what a hint says about decisions inside the loop must happen on the last trip; one trip is enough. A hint names an activity or child workflow as the workflow code does (`Ship` for `a.Ship`): pass the same value to `env.OnActivity` / `env.OnWorkflow`. Paths that are needs-special-test or needs-review get no hint; for those, `pathkit explain` shows the label's "What to do". `--json --hints` adds `howToCover` (a list of lines) to those paths. `pathkit explain` shows the same hint for an untested testable path. Without `--hints` nothing changes. `--summary` lists no paths, so it drops the hints (with a note). Hints are not in the HTML page yet (`--html` ignores `--hints`).

### Traces that fit no path: `--unmatched`

Normally such a trace is one warning line on stderr. `pathkit report --unmatched` (or `coverage --unmatched`) adds a section that explains each one, grouped by workflow and recorded steps (classification does not need to be on). The example below is the fixture `testdata/fixtures/helpererr`, recorded with `pathkit test`:

```
Traces that fit no path: 1 group, 1 trace file

  helpererr.Flow, steps id t1:32285e72cef2, 1 trace file, not acknowledged
    recorded steps:    J1.false
    followed the map:  Start -> if len(in) > 10 --false-->
    then:              the trace stops at J2 (Sleep (timer)) before reaching an end
    the map expected:  Sleep (timer) --failure (J2.failure)
                       Sleep (timer) --success (J2.success)
    likely: the run ended (returned or failed) earlier than the map allows; PathKit assumes an `if err != nil` on an error from a helper ... put //pathkit:branch on it (for a //pathkit:ignore, remove the comment) ...
```

- **followed the map** is the part of a path the run did take; **then** is the exact step where it left the map; **the map expected** lists what the map allows at that point.
- **likely** is PathKit's best guess, and says so. The most common cause is an error check on an error from your own helper function: PathKit assumes it never fails (see "Only error checks right after a Temporal call are junctions" in LIMITATIONS.md), so a run that does fail there takes a road the map does not draw. `//pathkit:branch` on that `if` makes the map draw it.
- With classification on, a group accepted in `classification.explained_traces` says `acknowledged: <reason>` (with classification off, acceptances are not read, so every group says `not acknowledged`).
- When the run went round a loop several times, "followed the map" shows only the last trip (the loop rule) and says `(repeated loop trips folded: only the last one is kept)`; the step number in "then" counts the steps as recorded.
- A trace that fits a path beyond the 2000-path cap is counted as unmatched on the `Traces:` line but is not listed here (it fits a real path); a line under the heading says how many.
- With no such trace, the section says `Traces that fit no path: none`. `--json --unmatched` adds `unmatchedTraces` (a list, empty when none) with the same fields. Without `--unmatched` nothing changes.

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

## 6. The whole project at a glance (`report`)

`coverage` is the close-up of each workflow. `report` is the **whole-project view**: one block per workflow, a priority label saying where tests are most needed, the project total, the workflows left out and why, and what happened to every trace file. Its numbers are exactly `coverage`'s: both commands use the same code to count, and a test checks they agree field by field.

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
- when a workflow in scope can't be analyzed: fix it, or exclude it with a reason (section 8).

| Flag | Effect |
| --- | --- |
| `--summary` | One line per workflow instead of every path. |
| `--json` | Print JSON instead (shape below). Config: `"json"`. |
| `--out <file>` | Also write the report to a file, **always plain text** (no colour codes). Config: `"out"`, relative to the config file. |
| `--no-color` | No colour. Colour (`covered` green, `missed` red; High red, Medium yellow, Low green) appears only on a real terminal, never when piped or redirected, and never with `--no-color`, config `"noColor": true`, or the `NO_COLOR` environment variable set to anything. The words are always there; colour is decoration only. |
| `--traces`, `--fail-under`, `--allow-stale`, `--clean` | As for `coverage` (section 5). `--fail-under` checks the project's path coverage. |
| `--config`, `--include`, `--exclude`, `--all` | Scope, as in section 8. `--all` ignores the scope and shows every workflow. |

A flag always beats the config file, including `--json=false`. The exit codes are the same as for `coverage`: `0` fine, `1` a real error, `2` below `--fail-under` (the report is printed first).

**`report --json`** has exactly `coverage --json`'s shape (section 5), with three additions:
- each workflow also has `file` (where it's declared, always with forward slashes, `orders/orders.go`, even on Windows, so the JSON is the same on every system; `pathkit explain` also shows it with forward slashes, while the text `report` uses your system's own separator) and `priority` (`"High"`, `"Medium"`, `"Low"`, or `null` when it has no paths);
- at the top, `excludedBy` lists what excluded workflows (`[".pathkitrc.json", "--exclude"]`, or `[]`).

Everything else (`workflows`, `excluded`, `total`, `branches`, `traces`, `failUnder`) means exactly the same as in `coverage --json`.

## 7. A shareable web page (`--html`)

`report --html` also writes the report as **one web page**, which you can open in any browser, attach to an issue or an email, or send to someone without Go:

```bash
cd testdata/pilot
pathkit test ./...
pathkit report ./... --html          # writes .pathkit/report.html
pathkit analyze ./... --html         # writes .pathkit/analysis.html (the map only)
```

Open the file in your browser: double-click it, or run `xdg-open .pathkit/report.html` (Linux), `open .pathkit/report.html` (macOS), or `start .pathkit\report.html` (Windows).

**What's on it,** top to bottom:
- **Header:** the folder, the time, the config used and the PathKit version.
- **A bar** with links to *Summary*, *Workflows*, *Paths*, *Glossary* and *Detector audit*, and a **search box** that looks in every path table (by path id, label, reason or the text of the path).
- **Summary:** the project total and branch coverage, worded exactly like the terminal; five cards (**testable coverage**, **overall coverage**, **paths**, **covered**, **missed**) with one plain sentence saying what testable and overall mean; and, with classification on, one bar and a count for each of the four buckets.
- **Workflows:** one row per workflow with its file, its **High/Medium/Low label** (the word as well as the colour), its coverage and how many paths it has (`truncated` when the list was cut at 2000 paths).
- **Paths:** one collapsible block per workflow with a table of its paths: number, path id (with a **Copy** button), **Covered** or **Missed** (a mark and a word), bucket, reason label, a short explanation, and a **Show** button for the details and the path step by step. Filters for status, bucket and label; click a column heading to sort; **25, 50 or 100 rows per page**, with Prev, Next and page numbers. Filters, search and paging work together.
- **Run details:** the `--fail-under` result if you set one, the `Traces:` counts, the excluded workflows with their reasons and the **trend**.
- **Glossary** and **Detector audit** (see the classification section).

The page is light only, fits a phone, a tablet or a desktop, and prints. It is drawn by a small script from the data in the file, so a project with thousands of paths still opens at once and the file stays small. Without JavaScript the summary, the workflow list, the run details and the glossary still show, and a note says the path tables need JavaScript.

**Safe to send, works offline.** The page is a single file with everything inside it. It loads no fonts, images, stylesheets or scripts from the internet, and it tells the browser to refuse any network request and to run only its own script. Everything that comes from your code or config (workflow names, conditions such as `if decision == "approved"`, file paths, exclusion reasons) is escaped, so it's shown exactly as written and can never act as HTML or script.

**`analyze --html` and `report --html` write separate files** (`analysis.html` and `report.html`), each complete from its own run, so neither overwrites the other. The `analyze` page lists the paths and says "Not measured".

**The trend.** Each `report --html` run adds a line to a small file, `report-history.json`, **next to the HTML file**, and the page shows the last 5 runs: time, covered/total, and a bar. If the set of counted workflows changed since the run before (for example you excluded one), that line says `scope changed: 7 in scope, 1 excluded`, so a jump in % that only comes from excluding something is visible.
- A run that fails (exit code 1) adds nothing. A run below `--fail-under` (exit code 2) still writes the page and adds its line.
- If `report-history.json` is missing (the first run) or damaged, PathKit prints a warning, `pathkit report: warning: ... starting a new trend`, and starts a fresh trend. The report itself never fails because of it.
- `pathkit test`, `pathkit clean` and `--clean` **never delete** `report-history.json`: they delete only `*.trace.json` files and PathKit's own copies in `.pathkit/overlay/`. Delete the file yourself to reset the trend.

**The trend in CI.** A CI job usually starts from a fresh checkout, so `.pathkit/report-history.json` isn't there, and the trend starts empty on every run (with the "no report history yet" warning). To keep it, save the history file at the end of each run and put it back at the start of the next. In GitHub Actions, for example:

```yaml
      - name: Restore the coverage trend
        uses: actions/cache/restore@v6
        with:
          path: .pathkit/report-history.json
          key: pathkit-trend-${{ github.ref_name }}-${{ github.run_id }}
          restore-keys: pathkit-trend-${{ github.ref_name }}-

      - run: pathkit test ./...
      - run: pathkit report ./... --html

      - name: Save the coverage trend
        if: always()
        uses: actions/cache/save@v6
        with:
          path: .pathkit/report-history.json
          key: pathkit-trend-${{ github.ref_name }}-${{ github.run_id }}

      - name: Keep the report page
        if: always()
        uses: actions/upload-artifact@v7
        with:
          name: pathkit-report
          path: .pathkit/report.html
```

Upload the page as a CI **artifact** to download and open it after the run. The cache keeps one trend per branch. Any other way of keeping a file between runs works too; PathKit only needs `report-history.json` to be next to the HTML file when `report --html` runs.

| Flag or key | Effect |
| --- | --- |
| `--html` | Write the page to the default file (`.pathkit/report.html`, or `.pathkit/analysis.html` for `analyze`). |
| `--html=<file>` | Write it there; the trend file goes next to it. Write it **with `=`**: `--html report.html` (with a space) would read `report.html` as the folder. With no folder given, PathKit stops with `write --html=report.html (with =)`; with a folder given, it says it expected at most one folder. |
| `--html=false` | No page, even if the config says so. |
| config `"html"` | `report` only: `true`, a file path (relative to the config file), or `false`. |

The page's "Generated" time is in UTC. File paths on the page use forward slashes on every system.

## 8. Choose which workflows count (`.pathkitrc.json`)

Some workflows can't be tested yet (say, they need an external test system). Counting them would drag your coverage down and make the number mean less. A `.pathkitrc.json` file says which workflows count. Think of a school report where some subjects are marked "not graded, because …": they are listed with the reason, and the average is taken over the rest.

```json
{
  "packages": ["./internal/workflows/..."],
  "workflows": {
    "exclude": [
      { "name": "LegacyPaymentsWorkflow", "reason": "needs an external test system" }
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

(`analyze` has `--all`, so it says so. `coverage` and `report` print the same list with their own shared wording, `1 workflow excluded by .pathkitrc.json (see "reason"):`.)

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

**Example with the sample project:** excluding `ShipmentWorkflow` changes the project from 38 paths with 20 covered (52.6%) to 29 paths with 17 covered (58.6%). The number went up only because something was taken out. That's why every exclusion shows its reason, and why `report` always prints how many workflows were excluded (section 6).

**A workflow in scope that PathKit can't analyze** (for example one using `goto`) is always printed as `in scope but not analyzable: … (fix it, or exclude it in .pathkitrc.json with a reason)`. `coverage` and `report` refuse to run while one is in scope (exit 1), so the percentage always covers exactly what's in scope.

**All keys:**

| Key | Meaning |
| --- | --- |
| `packages` | Where your workflows are (default `./...` from the file's folder). Names are checked against these. It is also the folder `pathkit test`/`traces` use when you give none (only when it lists one). |
| `traces` | Trace folder (default `.pathkit/traces`). |
| `workflows.include` / `workflows.exclude` | See above. |
| `failUnder`, `allowStale` | Coverage threshold (0–100) / accept traces from changed code (see section 5). |
| `out`, `json`, `noColor` | `report` output: write to a file (relative to the config file), print JSON, no colour (section 6). A flag always wins. |
| `classification` | Marking paths yourself, and switching on the testable/overall block (`enabled`). See "Marking paths as untestable" in section 5. The section's `ci` key is described in "The CI check" in section 5. |
| `html` | `report` only: `true` writes `.pathkit/report.html` (from the current folder), a path writes there (relative to the config file), `false` writes none (section 7). `--html` always wins, including `--html=false`. |

Every key is used and covered by a test. (`out`, `json`, `noColor` and `html` in the config are for `report` only; `coverage` and `analyze` use only their own flags.) Any other key (such as `"workflow"` instead of `"workflows"`) is an error.

## 9. Errors and exit codes

Every error starts with `pathkit <command>:` on stderr; a few (a list of paths, a warning block) continue on the lines after it.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | A real error (bad argument, missing file, ...). The message says what. |
| `2` | The report was produced, but **either** coverage is below `--fail-under` (or the config's `failUnder`), **or** (with classification enabled) the CI baseline check failed: a new untested testable path, a new needs-review path, a path that lost its test, or testable coverage below `classification.ci.fail_under_testable` (see "The CI check" in section 5). The stderr lines say which. Lets CI tell "coverage too low" apart from "PathKit failed". |

| Message | Cause | Fix |
| --- | --- | --- |
| `missing <file> argument; give a .go file, a folder, or folder/... (for example: pathkit analyze ./...)` | No path given. | Pass the file or folder. |
| `no go.mod found in . or any folder above it; ...` | PathKit reads a Go module and found no `go.mod`. | Run it from inside your project (the folder that holds `go.mod`), or name a folder inside it. |
| `unknown flag: --xyz` | Misspelled or unsupported flag. | Check the spelling (`pathkit <command> --help` lists flags). |
| `pathkit: unknown command "xyz"` | Misspelled command. | Run `pathkit --help` for the list of commands. |
| `no trace files found in <dir>; coverage is recorded only by "pathkit test" ...` | No runs were recorded, usually because the tests ran with plain `go test`, or you run this command from another folder than `pathkit test`. | Run `pathkit test ./...` first, from the folder that holds `go.mod`, then run this command from the same folder (or give `--traces`). |
| `go test failed (exit status 1); traces recorded so far are kept. ...` | One of your tests failed under `pathkit test`. | Fix the failing test and run `pathkit test` again. |
| `expected a package folder or folder/..., not a file` | `pathkit test` runs packages, not single files. | Pass the folder. |
| `expected a folder or folder/..., not a file: ... (report covers a whole project; ...)` | `report` is for whole projects. | Pass the folder, or use `pathkit coverage <file>`. |
| `cannot record <workflow>: ... a name pathkit needs` | Your code already uses one of pathkit's generated names. | Rename yours (see LIMITATIONS.md). |
| `Workflow file not found: ...` / `Directory not found: ...` | Wrong path. | Check the path and your working directory. |
| `package does not compile: <package>: <file>:<line>:<col>: <error>; fix the build error ...` | PathKit needs code that builds. The message names the package and its first real compile error. | Fix that error (`go build ./...` shows them all). |
| `3 packages do not compile: <package>: <error>; <package>: <error>; ...` | Several packages under a `folder/...` don't build. PathKit stops rather than skip them, so no workflow drops out of the numbers unnoticed. Each broken package is named once, with its first error. A package that only *imports* a broken one is not listed. | Fix them, or leave those folders out of the pattern you pass. |
| `... has no exported workflow functions to analyze; ...` | Nothing in that file matches the workflow rule above. | Check the function is exported, takes `workflow.Context` first and returns `error` last. |
| `no workflows in scope to measure: ...` | `report`, `coverage` or `pathkit test` found no workflow, or the scope leaves none. | Same check as above, then look at `include`/`exclude` in `.pathkitrc.json`. |
| `WARNING: PathKit found no decision points to measure in N workflow(s), which have no paths: ...` (top of the `report` and `coverage` text, and on stderr of `analyze`, `coverage` and `report`) | A workflow has no path at all, for example a loop with no `break` and no `return`. Its 0 of 0 is not a measurement and it is not counted in the totals. Not an error; exit code unchanged; the JSON is unchanged. | Give the loop a `break` or a `return` (a continue-as-new counts), or leave the workflow out in `.pathkitrc.json` with a reason. |
| `WARNING: N workflow(s) have more than 2000 paths ...` (top of the report) | A workflow reached the path cap, so its path list is incomplete. Not an error; exit code unchanged. | Split the workflow into smaller ones; read its numbers as a lower bound. |
| `no workflows could be analyzed` | Every workflow found uses something PathKit cannot map (see the `in scope but not analyzable` lines). | Rewrite the construct named in that line (see "Never supported" above), exclude the workflow in `.pathkitrc.json` with a reason, or analyze another file. |
| `in scope but not analyzable: <workflow>: goto at ... is not supported: ...` | The workflow uses `goto` or Go's `select`, which PathKit never maps. | Rewrite with `break`/`continue`/`return`, or `workflow.Selector`; or exclude the workflow in `.pathkitrc.json` with a reason. |
| `<path>/.pathkitrc.json: invalid JSON at line 3, column 32: ...` | The config file isn't valid JSON. | Fix the file at that line (a missing comma, say). |
| `<path>/.pathkitrc.json: the file is saved as UTF-16; save it again as UTF-8 ...` | The file was saved in UTF-16 with a byte order mark, which is what Windows PowerShell's `>` writes. PathKit reads UTF-8 (with or without a byte order mark). | Open the file in an editor and save it as UTF-8. |
| `<path>/.pathkitrc.json: unknown key "workflow"; check its spelling ...` | A misspelled key. | Check the spelling against section 8. |
| `<path>/.pathkitrc.json: workflows.include is empty: ...` | `"include": []` would be ambiguous. | Remove it, or list the workflows that count. |
| `<path>/.pathkitrc.json: exclude "X" needs a "reason"` | Every exclusion must say why. | Add `"reason": "..."`. |
| `... include "X" matches no function ...` / `... exclude "X" matches no workflow ...` | A misspelled or missing name. | Fix the name; `pathkit analyze --all` lists every workflow. |
| `... include "sendEmail" is not a workflow: its first parameter is not workflow.Context` | `include` can only add workflow-shaped functions. | Remove it from `include`. |
| `... matches 2 functions (a.Run, b.Run); write it with its package ...` | A short name is ambiguous. | Write it as `a.Run`. |
| `in scope but not analyzable: <workflow>: ...` | The workflow uses something PathKit never maps. | Fix it, or exclude it with a reason. |
| `no workflows in scope to analyze (every workflow is excluded)` | The scope left nothing to show. | Check `include`/`exclude`, or use `--all`. |
| `coverage 52.6% is below --fail-under 80%` (exit 2) | Path coverage is under your threshold. | Write tests for the untested paths listed above it. |
| `N paths had a test in the baseline <file> and have none now:` (exit 2) | A test that used to run these paths no longer does (CI check). | Restore the test, or, if that is intended, pass `--allow-lost-tests` once and write the baseline again. |
| `N testable paths with no test not in the baseline <file>:` (exit 2) | New paths nobody tests yet (CI check). | Write tests for them, or look at them and run `pathkit report --write-baseline`. |
| `N needs-review paths not in the baseline <file>:` (exit 2) | PathKit is not sure about these paths (CI check). | Look at each one (`pathkit explain`), then fix the cause or re-write the baseline. |
| `testable coverage 62.5% is below classification.ci.fail_under_testable 80% (in <config>)` (exit 2) | Testable coverage under the floor. | Write tests for the untested testable paths. |
| `baseline file <file> not found; write it with --write-baseline` | The CI check has no baseline yet. | Run `pathkit report --write-baseline` once and commit the file. |
| `baseline file <file> is saved as UTF-16; save it again as UTF-8 ...` | The baseline file was saved in UTF-16 with a byte order mark (Windows PowerShell's `>` does this). A UTF-8 file with a byte order mark in front is read normally. | Save it again as UTF-8, or run `pathkit report --write-baseline`. |
| `no listed path has the stable id s1:... in N measured workflows` | `pathkit explain` found no such path: a typo, or the code changed (the id is built from the decisions' text). | Take the id from a fresh `pathkit report --json`. |
| `--baseline and --write-baseline need path classification ...` | The baseline only means something with classification on. | Set `"enabled": true` under `"classification"`. |
| `invalid --fail-under value: ...` | `--fail-under` needs a number from 0 to 100. | e.g. `--fail-under 80`. |
| `invalid --older-than value: ...` | `pathkit clean --older-than` needs an age. | e.g. `7d`, `12h`, `30m`. |
| `... matches no workflow ...; did you mean "ShipmentWorkflow"?` | A typo in a workflow name (config, `--include`/`--exclude`, `--function`). | Use the suggested name. |
| `invalid --limit value: ...` | `--limit` needs a positive whole number. | e.g. `--limit 20`. |
| `--limit ignored because --summary was passed.` | Both flags together. A note only. | Use one or the other. |

Flags can go before or after the file name: `pathkit analyze orders.go --summary` and `pathkit analyze --summary orders.go` mean the same thing.
