# Changelog

All notable changes to PathKit for Go are listed here. PathKit has not had a tagged release yet, so everything below is part of the versions being prepared (v0.2.0 to v0.5.0, not tagged). The first version also contains the base tool: `analyze`, `test`, `coverage` and `report` (text, JSON and HTML), and the `.pathkitrc.json` scope settings.

## v0.5.0 (not tagged yet)

One milestone (M11): branch coverage as the headline, engine upgrades for more kinds of code, reports and commands made for the people who write tests, and the decision check. Nothing is tagged yet. Every existing JSON key keeps its name, value and meaning; new keys are added. Traces from v0.2.0 to v0.4.0 are still read and counted (what an old trace lacks is "not recorded"). The text of `report`, `report --summary` and `coverage` is redesigned; **`--technical` prints exactly the v0.4.0 text** (a test runs all the older text expectations and goldens through it).

### Branch coverage first

- **Branch coverage is the headline** everywhere (text, `--summary`, the page, JSON `branchCoverage`): decision outcomes that can happen, grade (`branches.grades`, default 60 / 75 / 90; `branches.target_grade`), outcomes proven impossible left out and listed ("can't happen"), outcomes blocked by a known bug kept in on a line of their own, two-step combinations, each part of a condition (`conditionCoverage`; trace field `conditions`, recorder 4), and one plain summary sentence. "N of M workflows at <grade> or better" replaces "meet their target" in the headline (the older data stays in the JSON and in More detail).
- **Stable branch IDs** (`b:…`, no line numbers) for every decision outcome; a helper's outcome is counted once per helper in every helpers mode. JSON `outcomes`.
- **The to-do list is missed outcomes**, riskiest first, with a plain sentence and a suggested test name (`pathkit next`, `--top`, `--scaffold`, `--json`; first list of the report, the page and `--summary`).
- **New commands**: `pathkit branches` (`--missed`, `--workflow`, `--json`, `--technical`), `pathkit next`, `pathkit check-decisions`.
- **Gates**: `classification.ci.branch_floor`, branch coverage in the baseline file (a drop below it or below the floor fails with exit 2; a baseline written again by this version therefore starts to enforce the floor, 75% by default, and the baseline from an older PathKit does not), `pathkit diff --min-new-branch-coverage`.
- **The decision check** (`check-decisions`, `--sample`, `--base`, `--workflow`, `-p`, `--timeout`, `--json`, `--allow-network`, `--list`): forces covered decisions to go the other way (a plain `if`; an error check of an activity, child workflow or timer) and reruns the tests that went through them; noticed, survived, no visible effect, flaky; "Tests notice wrong decisions" and the survivors in the reports (`decisionCheck`). Switches and loops are not forced yet.

### Engine

- **Smarter `&x`** (E1): forgotten only at the `Get` / `Receive` / `json.Unmarshal` call that writes through it.
- **Summaries of plain functions** of the same module (E2): types, nil-ness, constants and the types a map the function builds stores under each constant key.
- **"Possible bug" label** `likely-defect` (E3): a type switch case or comparison that can only be taken by dereferencing a pointer; needs-review, never "can't happen"; `detectors.disable` accepts it.
- **The scenario-merge safety check understands loops** (L1).
- **Chained selectors** (`workflow.NewSelector(ctx).AddFuture(...).Select(ctx)`) are mapped (O1).

### Reports

- **Plain names** (R1) and a **page for people** (R2, R3): branch coverage card, what to test next, workflows worst first, a decision checklist per workflow (with the cleanup after a cancel as a labelled block, O2), tooltips on every term, a plain glossary; the older page is the closed **More detail** part. Helper regions are offered next to the biggest section as split candidates (O3).
- **Scaffolds fill in the input** from the facts on the path, an activity's result and a signal's payload, and name the exact condition in a TODO when they cannot (L2). `pathkit doctor` reports a `.pathkit` folder copied from another folder, and `pathkit clean` removes that overlay (L4).
- **Changed output (listed on purpose)**: the default text of `report`, `report --summary` and `coverage`; the page layout; the unsolved-input TODO wording of scaffolds; the JSON keys `branchCoverage`, `outcomes`, `conditionCoverage`, `conditions`, `decisionCheck`, `mainNumber.splitHelperCandidates`, `ci.branchFloor`, `ci.branchDrops`; the baseline file's `branchCoverage` (an older PathKit refuses a baseline written by this version: it checks for unknown keys).
- **Changed expectations (listed on purpose)**: the fixtures `unknownIface`, `unknownMap` and `activityLike` (opaque stand-ins) now go through function values, because a plain function with a visible body is read (E2); the fixture `PossibleAddressTakenLater` became `ImpossibleBeforeAddressTaken` (its decision comes before the call that writes through `&x`); the old one-call chain fixture is now `SelectorChainedOne` and is mapped, while `SelectorNotInVariable` (kept in the frozen map) now uses a selector made by a helper, which is still refused.

## v0.4.0 (not tagged yet)

Two milestones: scenarios and the main number (M9), and advice and adoption (M10). Nothing is tagged yet. Every existing JSON key keeps its name, value and meaning; new keys are added. Traces from v0.2.0 and v0.3.0 are still read and counted.

### Scenarios and the main number

- **Scenarios**: paths grouped by what the workflow does outside itself (activity, child workflow, Nexus and timer outcomes, signals, version side, cancel, loop 0 / 1+ rounds, end). Counted exactly even over the path cap (JSON `scenarios`, path `scenarioId`). A path through a place PathKit could not read is never merged. Paths that repeat another path's behaviour say `same behaviour as path N` (JSON `sameBehaviourAs`); no number changes because of it.
- **The main number of each workflow** (handler, path, scenario or branch coverage, chosen by rules; JSON `mainNumber`), with one sentence on why, config `classification.main_number.force`, per-kind `classification.targets`, and **the project headline** as the first lines of the text, `--summary` and the page (`N of M workflows meet their target`; JSON `mainHeadline`). The `--summary` line of each workflow ends with its main number. Split advice for workflows not measured by paths.
- **Decision pairs** (JSON `decisionPairs`, one headline line). **Handler coverage** per workflow (`handlerCoverage`).
- **CI gate on the main number**: the baseline stores it; `classification.ci.tolerance` and `ci.main_floor`. Old baselines work as before.
- **Carry-overs**: dedicated soundness probes for helpers that write through pointer receivers, maps, slices and struct pointers (no false claim found); `cancel()` is a step; clearer reasons for the three selector shapes the sample corpus still excludes; `classification.direct_returns` stays `report`, now documented with when to turn on `step`.
- **A baseline written by this version has `mainNumber` entries that an older PathKit refuses to read** (it checks for unknown keys); write it again with the older binary if you need both.
- **Changed output (listed on purpose)**: the headline lines at the top of `report` and `coverage` text; the main-number lines under each workflow; ` (same behaviour as path N)` after a path; the ` · main number: …` suffix of each `--summary` line; the new JSON keys; the HTML main-number table. The CI check prints one more line when a main number falls below the baseline.

### Advice and adoption

- **Tests to write first**: the 10 riskiest missed scenarios, tagged (cancel, compensation, activity failure, continue-as-new, timers, signals and updates, plain logic, old versions), with hint, place, suggested name and id (`--summary`, full text, JSON `testsToWriteFirst`, first section of the HTML missed area).
- **`pathkit scaffold`** writes a Go test skeleton for an item (mocks, signals, cancel, input, assertions, TODO lines); `--out` never overwrites. **`pathkit init`**, **`pathkit doctor`**, **`pathkit diff --base`** (`--markdown`, `--json`, `--fail-on-untested-new`).
- **`pathkit test --no-network`** (Linux `unshare -rn`, refuses without it) and **`--skip-file`**.
- **Which test covers what**: traces carry `testName`; the report shows per test the scenarios and branches it covers and the tests that add nothing (JSON `tests`).
- **`report --format junit|cobertura|sarif --output <file>`**; the **trend chart** in the page; the **nightly public-corpus job** and **duplicate removal** in the code.

## v0.3.0 (not tagged yet)

Two milestones: honest numbers on huge workflows, and reading more Temporal code shapes. Nothing is tagged yet. Every existing JSON key keeps its name, value and meaning; new keys are added. Output for a workflow under the 2000-path cap is unchanged except for the new tables and lines listed under "Added to every report".

### Honest numbers on huge workflows

- **`not measured (<reason>)`** replaces `0/0 (0.0%)` everywhere a ratio has nothing to divide by (text, `--summary`, HTML cards). The JSON ratio gets `measured: false` and `reason` beside its old keys.
- **Classification block for a workflow over the cap** is titled "listed paths only", gets a `Left out of the lists as impossible` line (total and by label; JSON `leftOutImpossible`), says `Cannot be tested: 0 listed, plus N left out`, and spells out the testable denominator.
- **CI floor for big workflows.** `classification.ci.fail_under_testable` uses stage coverage (`ci.capped_basis: "stage"`, default) or branch coverage (`"branch"`) for each workflow over the cap, and says which. Exit codes are unchanged.
- **Helpers copied in or summarized** (`classification.helpers.mode`: `inline`, `summarize`, `auto`, default `auto`). When copying helpers into the map would pass the path cap, each helper is mapped once and a call is one step; the report says `<helper>: X of Y branches tested; called from N places`. Workflows under the cap keep every path id. Traces recorded before this change still count (they are converted while the workflow code is unchanged). In the sample fixture a workflow that calls one helper at twelve places goes from 8,191 paths to 13.
- **Stages cut further.** A stage with more paths than `stages.max_paths` is cut at the decisions of its nested regions instead of falling back to branch coverage; the report says when a stretch cannot be cut and why.
- **Old `GetVersion` branches.** A version check written as a yes/no variable, with `!`, or as a one-line function is read like the plain comparison; stage paths through the old side are labelled; each workflow prints `Old version branches: N paths (exact)` (`oldVersionPaths`).
- **Short text report for huge workflows.** The paths of a workflow over the cap are listed only with the new `--paths` flag (`report`, `coverage`).
- **`pathkit explain <id>`** prints warnings only for the workflow that owns the path.
- **Overlay safety.** A copied `.pathkit/overlay` that names files outside the module, or is a link out of it, is refused with a message; nothing is read or written there.
- **The HTML page** shows the missed-paths section only when there are groups (it already did; now tested).
- **Windows CI** runs the race test with `-p 2`. macOS and Linux are unchanged.

### Reading more Temporal shapes

- **"PathKit could not fully read" panel** in every report (text, `--summary`, JSON `unread`, HTML): calls it did not follow (interface methods, function values, other packages and modules), computed names, futures nobody reads, returned calls, closures, validators that panic, workflows it cannot map, the path cap and context cut-offs.
- **Nexus operations** are steps (`echo (nexus operation)`, success / failure).
- **Update handlers and validators** are mapped like small workflows and recorded; the handler table (`handlers`) covers signals, queries, updates and validators.
- **Call outcome table** (`callOutcomes`) with the kinds of failure the code tells apart, and the **loop rounds table** (`loopRounds`).
- **`classification.activity_wrappers`** makes a library function a step. **`classification.direct_returns`** (`report` default, `step`) decides what `return fut.Get(...)` is. **`classification.loops.model`** accepts `one`; `zero-one-many` is reserved.
- **Several Go modules in one folder** are read in one combined report (`--module` limits it).
- **`docs/CONSTRUCTS.md`** lists 26 shapes, each handled or reported, with a fixture and an `expected.json` in `testdata/constructs` that a test checks.
- **Trace files** gain an optional `recorder` field. Older traces are read and counted as before; a step kind they cannot hold is shown as not recorded.

### Things that move for some existing projects

- **Version gates under the cap.** A `workflow.GetVersion` check written as a yes/no variable, with `!`, or as a one-line function now gives the old side the label `version-gate-old-branch` (needs-special-test), where before those paths were plain testable. Testable counts, the testable denominator and `fail_under_testable` can change for a project with such gates. Path ids do not change.
- **Stage ids of a workflow that had a stage too big to list.** The stage is now cut into pieces, so the stages after it are numbered again and their stage path ids (`g5:...`) change. Workflows whose stages were all small keep every id.
- **A folder with no `go.mod` above it but modules below it** used to stop with "no go.mod found"; it is now read module by module.

### Added to every report (new lines and keys)

`PathKit read everything in scope` (or the panel), the handler table, the call outcome table and the loop rounds table in the text and HTML report; `unread`, `handlers`, `callOutcomes` and `loopRounds` in the JSON. The golden files of the sample project, `internal/cli/testdata` and the test expectations that pin the report text were updated for these additions (`PATHKIT_UPDATE_GOLDEN=1`, `PATHKIT_UPDATE_HTML_GOLDEN=1`); `testdata/golden/model/snapshot.txt` and the older `coverage_orders.json` / `report_orders.json` goldens were not touched.

## v0.2.0 (not tagged yet)

The next version collects everything below; nothing is tagged yet.

### Changed: headline numbers and JSON key style

- **Three headline numbers** (text, `--summary`, `coverage`, HTML) when a workflow is over the 2000-path cap: path coverage over the workflows under the cap, stage coverage over the ones over it, branch coverage over all (whole map). Listed-path coverage of a big workflow is no longer in the headline. Without such a workflow nothing changes. New JSON key `headline`; every existing key keeps its value and meaning (listed paths only).
- **JSON key names.** Output keys added in this version were written in snake_case; the keys of earlier versions are camelCase, so they were renamed to match (only unreleased keys, no earlier key changed): `path_count_exact`→`pathCountExact`, `paths_listed`→`pathsListed`, `pruned_groups`→`prunedGroups`, `pruned_groups_total`→`prunedGroupsTotal`, `pruned_scope`→`prunedScope`, `branches_all`→`branchesAll`, and in each `stages` entry `too_big_to_list`→`tooBigToList`, `cannot_be_tested`→`cannotBeTested`, `needs_review`→`needsReview`, `contexts_cut_off`→`contextsCutOff`, `path_list`→`pathList`, in each pruned group `under_review`→`underReview`, and `ci.stage_floor`→`ci.stageFloor`. Config-file keys stay snake_case, as before (`stage_floor`, `max_paths`, `max_contexts`).
- `rules check` no longer prints a stale rule twice.

### Added: review tools

- **Missed paths by first untested step.** `pathkit report --groups` groups the testable paths no test ran by the first step no test took, biggest group first, each with file and line and how to cover it. The JSON has `missedGroups` (with `--groups`), the HTML report has a section.
- **`pathkit rules check`** shows how many paths every rule, override, comment and explained trace hits. It refuses (exit 1) a rule, untestable override or comment that hits a path a test ran, and warns about entries that hit nothing or overlap.
- **`pathkit suggest`** drafts config rules for the needs-review paths. Every draft has the label and the reason `TODO`; the config now refuses `TODO` in rules and overrides too.
- **Label `known-defect`** (needs-review), set only by a person in a rule or `mark_untestable`: the path can happen, a known bug blocks it. The summary says "Blocked by known bugs: N" (`blockedByKnownBugs` in the JSON). This closes gap (h) of the shape fixtures.
- **Assumptions panel** in every report format: `closed-signal-channels`, `default-json-converter`, `no-custom-interceptor`, on or off, with the number of claims that rest on each (`assumptionPanel` in the JSON).
- **Label guide.** SETUP-GUIDE.md has a section on labels that look alike (no label was renamed); a test fails when a label is missing from the guide.
- **Nightly workflow** `.github/workflows/nightly.yml` runs the soundness test with 10,000 programs on Linux. The normal CI workflow is unchanged.

### Changed (stage view)

- Stage numbers print only for a workflow whose paths are not all listed; the `branches` total of such a workflow is again the listed exits (`branchesAll` is the whole map).
- Facts across stages: once a set of incoming states was cut at the limit, later stages are judged as cut too (a reviewer found the flag was forgotten after one stage).
- The `report_orders.html` golden gained the missed-paths section (new section only).

### Added: big workflows (exact counts, stages, pruning)

- **Exact path counts.** Every workflow's number of paths is counted exactly (up to hundreds of digits) without listing them. Reports show "Exact paths: N (2000 listed; path coverage is incomplete)" instead of a silent cap.
- **Stages.** A workflow is cut at the decisions every path past them goes through, and the stages are merged up to `classification.stages.max_paths` (256) paths. Reports show stage coverage and branch coverage; a stage too big on its own falls back to branch coverage. Stage path ids look like `g2:3fa94c0d17ab`. The HTML report has a Stages view. `classification.ci.stage_floor` is a floor for stage coverage.
- **Facts across stages.** A stage is checked from every different state the stages before it can leave, at most `classification.engine.max_contexts` (16): impossible in every one is cannot-be-tested, possible in one is testable, cut off at the limit is needs-review.
- **Traces beyond the list** are counted, not unmatched (`traces.beyondCap` in the JSON), and cover their stage paths.
- **Pruning.** For a workflow with more paths than the list holds, the parts the fact engine shows impossible are left out of the list and counted as groups with their proofs, so the 2000 listed paths can be taken. A trace that runs through such a part un-prunes it, warns, and moves its claims to needs-review. Listed paths keep their stable ids.
- **Helpers are always followed.** The milestone-4 fallback that kept helper calls as one step in a workflow that would pass the cap is gone; the path list is capped, the counts, stages and claims use the whole map.
- **New JSON keys only:** `pathCountExact`, `pathsListed`, `stages`, `prunedGroups`, `prunedGroupsTotal`, `prunedScope`, `branchesAll` per workflow (only when it has more paths than the list holds, or parts left out as impossible; a workflow whose paths are all listed prints as before) and `traces.beyondCap`.
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
