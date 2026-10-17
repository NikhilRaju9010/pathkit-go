# Changelog

All notable changes to PathKit for Go are listed here. PathKit has not had a tagged release yet, so everything below is "Unreleased". The first version also contains the base tool: `analyze`, `test`, `coverage` and `report` (text, JSON and HTML), and the `.pathkitrc.json` scope settings.

## Unreleased

### Added: big workflows (exact counts, stages, pruning)

- **Exact path counts.** Every workflow's number of paths is counted exactly (up to hundreds of digits) without listing them. Reports show "Exact paths: N (2000 listed; path coverage is incomplete)" instead of a silent cap.
- **Stages.** A workflow is cut at the decisions every path past them goes through, and the stages are merged up to `classification.stages.max_paths` (256) paths. Reports show stage coverage and branch coverage; a stage too big on its own falls back to branch coverage. Stage path ids look like `g2:3fa94c0d17ab`. The HTML report has a Stages view. `classification.ci.stage_floor` is a floor for stage coverage.
- **Facts across stages.** A stage is checked from every different state the stages before it can leave, at most `classification.engine.max_contexts` (16): impossible in every one is cannot-be-tested, possible in one is testable, cut off at the limit is needs-review.
- **Traces beyond the list** are counted, not unmatched (`traces.beyondCap` in the JSON), and cover their stage paths.
- **Pruning.** For a workflow with more paths than the list holds, the parts the fact engine shows impossible are left out of the list and counted as groups with their proofs, so the 2000 listed paths can be taken. A trace that runs through such a part un-prunes it, warns, and moves its claims to needs-review. Listed paths keep their stable ids.
- **Helpers are always followed.** The milestone-4 fallback that kept helper calls as one step in a workflow that would pass the cap is gone; the path list is capped, the counts, stages and claims use the whole map.
- **New JSON keys only:** `path_count_exact`, `paths_listed`, `stages`, `pruned_groups`, `pruned_groups_total`, `pruned_scope`, `branches_all` per workflow (only when it has more paths than the list holds, or parts left out as impossible; a workflow whose paths are all listed prints as before) and `traces.beyondCap`.
- **Wording changed (on purpose):** the path-cap warning and its JSON `pathCapWarning.message` no longer say the capped paths are "not counted" or that coverage is a "lower bound"; both are now wrong. The headline is the same. The stderr note "helper functions are not followed" is gone with the fallback.
- **Tests:** fixtures `bigflow` (24 decisions in a row; early exits; pruning; facts across stages with one, several and too many states), a capped workflow with helpers in `helperrun`, counts checked against the listing everywhere, stage and pruning checks in the soundness test (small stages, few states, small cap).
- **Page script:** the two `.sort()` calls have explicit compare functions, the smoke test reads the page from standard input, and the page golden holds a marker instead of the embedded script.

### Added: path classification

Some paths can never run (a loop the code always enters, the same value tested twice with opposite answers). PathKit can now tell those apart from real gaps. It is off until you turn it on with `"classification": {"enabled": true}` in `.pathkitrc.json`; with it off, the coverage numbers and exit codes are as before and the JSON only gains new fields.

- **Buckets and labels.** Every path is `testable`, `cannot-be-tested`, `needs-special-test` or `needs-review`, and an unusual path carries a reason label with a plain-English definition.
- **Two coverage numbers.** *Testable coverage* counts only the paths a test can run; *overall coverage* counts every path. `--coverage-basis` picks which one `--fail-under` checks. A path a test ran always counts as testable.
- **Config overrides and rules.** Mark one path by its stable id (`mark_untestable`, `mark_testable`), or write a rule that names a combination of decisions that cannot happen (with an optional `without` list). Every entry needs a reason; entries that no longer match are reported as stale.
- **Source comments.** `//pathkit:untestable` on a decision marks one exit of it, with a reason.
- **Automatic detectors**, each with a definition and a reason on every path it marks: `closed-signal-channel`, `constant-condition`, `contradictory`, `contradictory-get-result`, `same-list-loops`, `list-built-from-list`, `loop-exit-before-first-trip`, `version-gate-old-branch` and `history-limit`. A detector can be switched off with a reason. When unsure, PathKit leaves a path testable or marks it needs-review.
- **HTML glossary.** The shareable web page shows each label's definition and which detector or rule decided it.

### Added: the fact engine

- **New claims from following values.** PathKit now walks each path step by step and keeps notes on what each local value holds. A decision that the notes settle the other way is impossible: `var s string` then `if s == ""` (false side), `id := ""` then `if ok { id = v }` then `if id == ""`, a value an early `return` makes non-empty and then checked as `len(x) == 0`, a flag set in one branch and read right after. New label `value-flow`; `contradictory` and `constant-condition` also come from it where the existing detectors say nothing. Each claim has a proof (`pathkit explain`, and `proof` in `--json`). It forgets whenever it cannot be sure (address taken, closure write, loop, call, assumed `if`); see LIMITATIONS.md.
- **`possible-correlation`** (needs-review): an untested path that asks about the same value twice, where PathKit had to forget the value in between.
- **Config:** `classification.engine.enabled` (default true); `detectors.disable` accepts `value-flow` and `possible-correlation`.
- **A loop condition of a named bool type** (`type Flag bool`, `for !done`) or of a type parameter constrained by `~bool` can now be recorded by `pathkit test`: the recorder converts it with `bool(...)`. Before, `pathkit test` and `pathkit prepare` stopped with a message.
- **Internal check:** if PathKit's own check "a path a test ran is never hidden" ever fails, the command now stops with an error message instead of crashing.

### Added: type, nil and length facts

- **New claims** (cannot-be-tested): `impossible-type` (a type switch case or a type assertion no possible type takes) and `impossible-nil` (`v == nil` for an interface that holds a typed value). Leaving a loop before its first round is claimed with the existing label `impossible-loop-exit` when the loop always has a first round (a `range` over something not empty, a condition true at the first test).
- **Decoded values.** With Temporal's default JSON converter, an `interface{}` it fills holds only a string, float64, bool, nil, `[]interface{}` or `map[string]interface{}`; claims that use this name the assumption `default-json-converter`. PathKit turns it off by itself when the module sets its own converter or uses `UseNumber` / `json.Number`, and says so in the text report, the HTML glossary and the JSON (`assumptions`). Config: `classification.assumptions.default_json_converter` (`"auto"` or `"off"`).
- **`unknown-input-type`** (needs-review): an untested path takes a type switch case on a value PathKit cannot follow and no test has taken. The reports group these by switch.
- In the fixtures: `PossibleTypedNil` became `ImpossibleTypedNil` (the typed-nil rule now claims it), and the shape fixture `SwitchWithUnknownValue` now gets its unknown value from a call (a workflow input of type `interface{}` is a decoded value, which the new rule follows; that case has its own fixture, `SwitchOnDecodedInput`).

### Added: helper functions and local distrust

- **Helper functions are followed.** A function or method of the workflow's own package that calls the Temporal SDK (or calls one that does) becomes part of the workflow's map, one copy per call, up to three helpers deep: its activities, timers, branches, loops and error checks are steps of the workflow, printed `[in name]`. The error check after a call is a real decision with only the exit the helper's `return` allows; a helper whose errors PathKit cannot follow gives an *unknown* error (no longer "no error"). This fixes the old limit "an error from your own helper function fits no path" for these helpers. Helpers in other packages, calls through interfaces or function values, calls inside bigger expressions, `go`/`defer` calls, and helpers with `goto`, `select`, a selector or type parameters are still one step (LIMITATIONS.md).
- **Changed output, only for workflows that call such helpers:** their paths, `J` numbers, stable ids, function hash and counts. Every workflow that calls no helper that touches the Temporal API is unchanged, except that its traces must be recorded again (trace format 2). A saved `stable_id` that vanished is reported with the paths that replace it.
- **Trace format 2.** `pathkit test` also marks up the helper code and records `>S1` / `<S1` markers around a helper's steps. Traces from an older PathKit are refused with one message: "traces are from an older PathKit ... re-run `pathkit test`".
- **Local distrust of unmatched traces.** A trace that fits no path used to put every detector and rule claim of its workflow under review. Now only the claims that rest on the step where the trace leaves the map, or a later one, go under review; a claim whose reasoning stays before it keeps its label. The `fits no path` warning prints a ready-made `classification.explained_traces` entry with `"reason": "TODO"`, and the config refuses a reason of `TODO` (a config that already used `TODO` as a reason must change it). A function that is itself a workflow is not followed as a helper: it has a map of its own.
- **Tests:** fixtures `helpers`, `helperrun` (runs for real, with 21 recorded runs checked end to end), `clashhelper`, helper cases for the fact engine, and helper programs in the soundness generator (function with and without error results, branches, loops, up to three deep, reused with other arguments, closures and pointers that write the caller's variables, methods).
- **In the fixtures:** the loops of `ImpossibleLoopKeepsOthers` and `SuspectLoopBetween` no longer call a helper that uses the Temporal API, which would make them loops with a decision in them; they call an ordinary function, so they still test a plain loop.

### Changed: where the JSON rule applies

- **`default-json-converter` switches off only for what can affect it.** It is off for the whole module when the code sets its own converter (a `DataConverter` option, a codec constructor, or a type with a converter's methods). `UseNumber` and `json.Number` elsewhere no longer switch it off: they change only the values that decoder produces. Comments and strings no longer count either.
- **New:** values the workflow decodes itself with `json.Unmarshal`, or with `Decode` on a decoder made by `json.NewDecoder` in the same function that is never handed on and never calls `UseNumber`, are followed like decoded values, with no assumption (the standard library's rule).
- **Duplicate stable ids:** a test now fails if two paths of any workflow share a stable id (run on the pilot, every fixture and shape fixture, and any folders in `PATHKIT_UNIQUE_STABLE_DIRS`). None did.

### Changed: maps and slices handed to activities

- **A map or slice passed to `ExecuteActivity`, `ExecuteChildWorkflow` or `SignalExternalWorkflow` is no longer forgotten.** The SDK encodes those arguments when the call is made (checked in the SDK source), so the callee gets a copy. `ExecuteLocalActivity` and every other call stay as before. Claims that rest on this name the new assumption `no-custom-interceptor`; it is switched off by itself when the module registers a custom interceptor, or by `classification.assumptions.no_custom_interceptor = "off"`, and the text report, the HTML glossary and the JSON say so.
- **`report --json` now has the `assumptions` list**, as `coverage --json` already had.
- In the fixtures: `PossibleMapPassedToActivity` became `ImpossibleMapPassedToActivity` (the new rule claims it); `PossibleMapPassedToLocalActivity` keeps the old behaviour under test.

### Added: proofs for automatic claims

- **Claim proofs.** Every claim an automatic detector makes now carries its reasoning as an ordered list of plain sentences: one for each decision on the path that matters (with its step number and line of code), then a closing sentence. This includes the needs-review claims of the closed-channel and history-size detectors; a claim PathKit no longer trusts (because a trace fits no path) has no proof. `pathkit explain` prints it as a numbered `Proof:` block, and `--json` has a new `proof` key on those paths only. Nothing else in any output changed.

### Added: safety nets for the next claims (tests only)

- **Shape fixtures** (`testdata/shapes`, checked by `internal/shapes`): made-up workflows for the code shapes PathKit does not understand yet, each with a hand-written answer for every path and today's actual answer. The test log lists the gaps ("today: missed").
- **Soundness test** (`internal/soundness`): writes random small workflow-shaped functions, runs every input combination, and fails if a path PathKit calls cannot-be-tested is ever taken. 500 programs by default (50 with `-short`); `PATHKIT_SOUNDNESS_N=10000` for the long run; `PATHKIT_SOUNDNESS_SEED=<n>` repeats one failing seed.
- **Covered-path check.** A path a test ran that is not testable is now an internal error, checked on every run and by tests.

### Added: new commands and flags

- **`pathkit explain <stableId>`** says whether a test ran a path, its bucket and label, what decided it, and why.
- **`--hints`** under every missed testable path says what a test must make happen to run it.
- **`--unmatched`** explains a recorded run that fits no path: where it left the map and the likely cause.
- **CI baseline check.** `--write-baseline` saves the current state; `--baseline` then exits with code 2 when a new testable path has no test, a new needs-review path appears, a path that had a test has lost it, or testable coverage is below `fail_under_testable`.

### Added: smaller things

- A loud warning (text, HTML and the JSON field `pathCapWarning`) when a workflow reaches the 2000-path cap and its list is incomplete.
- Every first-run error says what to do next; every command's help has an example.
- A `.pathkitrc.json` or a baseline file saved with a UTF-8 byte order mark is read; one saved as UTF-16 with a byte order mark (what Windows PowerShell's `>` writes) gets a message that says how to fix it.
- A warning (text report, and stderr of `analyze`, `coverage` and `report`) when a workflow has no paths at all, for example a loop with no `break` and no `return`. Before, it printed `Total paths: 0` and nothing else. The JSON and the exit code are unchanged.
- `pathkit explain` shows the workflow's file with forward slashes on every system.
- The `report --unmatched` text explains in plain words that an error check on a helper's error is assumed never to fail.

### Changed: the HTML report

- `report --html` and `analyze --html` have a new page: light theme only, a summary with five cards and a bucket bar, a workflow overview, one collapsible block per workflow with a path table (filters, search, sorting, a details row, a Copy button for the path id), **pagination** (25, 50 or 100 rows), a bar with section links, and a print stylesheet. It fits a phone, a tablet and a desktop.
- The paths are embedded once as compact data and drawn by one small inline script, so a large project's file is a fraction of its former size. The page still loads nothing from the network, and its Content-Security-Policy allows only its own script by hash. Without JavaScript the summary, the workflow list and the glossary still show.
- Not changed: the numbers, the text and `--json` output.

### Fixed

- `pathkit test` recorded no traces when the project folder was reached through a symbolic link (macOS temporary folders are one). It now records them. The command printed by `pathkit prepare` still needs the real folder; see [LIMITATIONS.md](LIMITATIONS.md).

### Changed

- Output changes are additive: existing text, `--json` keys and HTML stay as they were, and new fields are added.

### Known limits

See [LIMITATIONS.md](LIMITATIONS.md). In short: PathKit cannot tell which tests were skipped, and the automatic detectors assume the Temporal SDK does not close signal channels, that no custom interceptors are used, and that activity results are not changed after `Get`.
