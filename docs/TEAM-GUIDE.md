# PathKit for Go: short team guide

PathKit lists every **path** through a Temporal Go workflow (every way a run can go through its `if`s, loops, switches, selectors and error checks) and shows which paths your tests ran. **Classification** adds one thing: some paths can never happen (for example a loop the code always enters), so PathKit puts each path in a **bucket**, and you can measure coverage over the paths that *can* be tested.

The output below comes from the sample project in `testdata/pilot` (shortened where it shows `...`). The full manual is `SETUP-GUIDE.md`.

## 1. Record, then report

```bash
pathkit test ./...     # runs your normal tests and records which path each run took
pathkit report         # every workflow, every path, covered or missed
```

Plain `go test` records nothing. Always use `pathkit test` for coverage.

## 2. Turn classification on (`.pathkitrc.json` in the project folder)

```json
{
  "classification": {
    "enabled": true,
    "rules": [
      { "id": "polls-at-least-once", "workflow": "ReportPollingWorkflow", "label": "loop-minimum",
        "when": ["for attempt <= in.MaxPolls --exit"], "without": ["for attempt <= in.MaxPolls --iterate"],
        "impossible": true, "reason": "MaxPolls is always at least 1 (checked by the caller)" }
    ]
  }
}
```

The config has these sections:
- **`enabled`**: turns classification on. Off, PathKit behaves exactly as before.
- **`rules`**: a combination of decisions your team knows is impossible, written as `pathkit analyze` prints them. Every rule needs a `reason`.
- **`paths`**: `mark_untestable` / `mark_testable` for one path at a time, by its stable id.
- **`detectors`**: switch off an automatic detector (with a reason).
- **`ci`**: the CI check (section 6): `"ci": {"baseline": "pathkit-baseline.json", "fail_under_testable": 50}`. Add it only when you write the baseline (a baseline setting without the file makes `pathkit report` stop).

**Buckets.** `testable` (a normal test can run it), `cannot-be-tested` (impossible), `needs-special-test` (possible, but needs a special test setup), and `needs-review` (PathKit is not sure). Automatic detectors and your rules fill them. A path a test ran is always testable.

## 3. Read the report

```
  5. Start -> for attempt <= in.MaxPolls --exit--> End (completed): missed [cannot-be-tested: loop-minimum · manual · rule "polls-at-least-once"]
...
Testable coverage:     20 / 37 = 54.1%  (testable paths only)
Overall coverage:      20 / 38 = 52.6%  (all paths)
Coverage basis used:   testable
```

Path 5 can never happen, so it no longer counts against you: testable coverage is 20 of 37, not 20 of 38.

## 4. Ask why: `pathkit explain <stableId>`

Each path has a stable id (`report --json`, field `stableId`).

```
$ pathkit explain s1:c048fc889a4c
s1:c048fc889a4c is path 5 of polling.ReportPollingWorkflow (polling/polling.go)
  Start -> for attempt <= in.MaxPolls --exit--> End (completed)

Tested:      no; no recorded run took this path
Bucket:      cannot-be-tested: The path is impossible: no run, test or production, can take it.
Label:       loop-minimum: A loop that always runs at least once, for example a retry loop.
Decided by:  a person: config rule "polls-at-least-once" in /path/to/.pathkitrc.json
Why:         Rule "polls-at-least-once": MaxPolls is always at least 1 (checked by the caller)
What to do:  Nothing.
```

## 5. Write the missing tests: `--hints`, and check odd runs: `--unmatched`

```
$ pathkit report ./polling --hints
  1. Start -> for attempt <= in.MaxPolls --iterate--> CheckStatus (activity) --failure--> End (failed): missed
       how to cover it:
         - go into `for attempt <= in.MaxPolls` (one trip is enough; what follows inside the loop must happen on its last trip)
         - make activity CheckStatus fail: mock it with env.OnActivity, passing the same activity the workflow starts (CheckStatus), with .Return(..., temporal.NewNonRetryableApplicationError(...))
         - expect the workflow to fail (env.GetWorkflowError() is not nil)
```

`pathkit report --unmatched` explains any run that took a road the map does not have: where it left the map, what the map expected there, and the likely cause. Here it says `Traces that fit no path: none`.

## 6. The CI check

```bash
pathkit report --write-baseline   # once; commit pathkit-baseline.json
pathkit report                    # in CI: exit 2 if something new needs attention
```

CI then fails (exit 2) when a new testable path has no test, a new needs-review path appears, a path that had a test has lost it, or testable coverage is below `fail_under_testable`. In the sample project, skipping one test gives:

```
pathkit report: 1 path had a test in the baseline pathkit-baseline.json and has none now:
  polling.ReportPollingWorkflow path 3 (s1:211be55ebb7c, now testable): Start -> ... switch status --case "failed"--> End (completed)
```

To accept a lost test on purpose, run once with `--allow-lost-tests`. After you have looked at new paths, write the baseline again. **Exit codes:** 0 fine; 1 PathKit could not do its job; 2 coverage too low or the CI check failed. The full table is in `SETUP-GUIDE.md`, section 9.

## 7. Test it on your own project

PathKit **never changes your source files, `go.mod`, `go.sum` or tests**; it writes only to `.pathkit/` and to files you ask for (see [what PathKit writes](../SETUP-GUIDE.md#what-pathkit-writes-and-never-changes)). Everything below is run from the folder that holds your `go.mod`.

```bash
# 1. Install once and put pathkit on your PATH (README, "Install")

# 2. In your project: list every workflow's paths (reads code only)
cd your-project
pathkit analyze ./...

# 3. Record traces: runs your normal tests; plain "go test" records nothing
pathkit test ./...

# 4. Report: covered and missed paths per workflow
pathkit report

# 5. Switch classification on, then report again. Create .pathkitrc.json in
#    this folder (in an editor) with exactly this line:
#      {"classification": {"enabled": true}}
pathkit report

# 6. Ask why one path got its bucket (ids: pathkit report --json, field "stableId")
pathkit explain s1:5179b1c99bbb

# 7. What a test must do to cover each missed testable path
pathkit report --hints

# 8. Runs that fit no path, with the likely cause
pathkit report --unmatched

# 9. CI baseline: write it once, commit it, then check on every build.
#    First change .pathkitrc.json to (the file name is the "baseline" setting):
#      {"classification": {"enabled": true, "ci": {"baseline": "pathkit-baseline.json"}}}
pathkit report --write-baseline
pathkit report                     # exit 2 when something new needs attention
```

Two things to know about this walkthrough:

- **The numbers.** On the sample project, step 5 shows `20 / 38` testable paths. Section 3 shows `20 / 37` because it also has the rule from section 2, which marks one path as impossible.
- **Windows.** Make `.pathkitrc.json` in an editor and save it as UTF-8. Windows PowerShell's `>` redirect writes UTF-16; PathKit then says so and stops (it recognises UTF-16 by its byte order mark). (A UTF-8 file with a "byte order mark" in front is fine.)

If something goes wrong on a first run, the message says what happened and what to do. The usual causes: no `go.mod` in the folder you are in; the code does not compile (`go build ./...` shows why); no exported workflow functions found; a test that failed or was skipped (a skipped test records nothing, so its paths show as missed; `pathkit test ./... -- -v` shows the skips). A workflow with more than 2000 paths gets a `WARNING` at the top of the report: its list is incomplete, and the fix is to split it.

## 8. Words used in the output

| You see | It means |
| --- | --- |
| `cannot-be-tested` | The bucket for impossible paths. In your config and comments you write `untestable` (`mark_untestable`, `//pathkit:untestable`); the path then lands in this bucket. |
| `Overall coverage` | Covered paths out of all paths. The `project coverage` figure on the totals line is the same number. |
| `Testable coverage` | Covered paths out of the testable ones only (impossible paths left out). |
| `missed` | No recorded test run took this path. |
| `stale trace` | A trace recorded for an older version of the workflow's code. |

## 9. Temporal words, in plain terms (from the sample project)

- **Workflow:** a Go function that describes a business process and that Temporal can run for hours or days. `OrderWorkflow` in the sample project (`testdata/pilot/orders`; see "Sample project" in `SETUP-GUIDE.md`) is one: it checks the amount, charges a card, and finishes.
- **Activity:** one unit of outside work that a workflow asks for, which can fail. `ChargeCard` is an activity. The workflow calls it and then checks whether it failed; that check is a decision with two exits, `failure` and `success`, so it adds two of the workflow's three paths (`--failure--> End (failed)` and `--success--> End (completed)`).
- **Signal:** a message sent into a workflow that is already running. In `ShipmentWorkflow`, the carrier's `"delivery-update"` message is a signal. The workflow reads it and does something different for `"delivered"`, `"returned"` and anything else.
- **Selector:** a way to wait for several things and act on whichever happens first. `ShipmentWorkflow` waits for either the `"delivery-update"` signal or a timer. Each possible winner is one exit of that decision, so each is a separate path.
- **Child workflow:** a workflow started by another workflow. `OrderFulfillmentWorkflow` starts `PaymentWorkflow`; PathKit shows the child as one step of the parent, and maps the child's own paths separately.
- **Continue-as-new:** a long-running workflow ends itself and starts a fresh run of itself, to keep its recorded event log (Temporal calls it the "history") short. `SubscriptionWorkflow` does this after a few billing cycles; PathKit prints that ending as `End (continued-as-new)`.
