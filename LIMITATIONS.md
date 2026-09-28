# Known Limitations — PathKit for Go

PathKit either detects a pattern correctly or clearly does not detect it; it never silently guesses. This file is the honest list of what it does and doesn't cover.

**Status:** building (M3 done). Entries under "Found while building" are confirmed by real code and tests. Everything else is a **planned** limitation that follows from the proposed design in `CLAUDE.md`. Each entry must be confirmed (or corrected) by a real test when its milestone is built, and then its "planned" tag removed. New limitations found while building go here too.

## Carried over from the TypeScript version (planned)

- **No following into other packages or child workflows.** A helper function in another package, or a child workflow started with `ExecuteChildWorkflow`, shows up as one step. What can go wrong *inside* it is not on this workflow's map. (TS had the same limit with files; in Go the boundary is the package.)
- **Concurrency is not modelled.** Activities started together and waited on later (the Go version of TS `Promise.all`), and coroutines started with `workflow.Go`, don't create junctions. A workflow whose only branching is "which parallel steps failed" shows one path.
- **Loops are "taken at least once" or "not taken", never counted.** "Succeeded on attempt 1" and "succeeded on attempt 3" are the same path.
- **Path enumeration stops at 2000 paths** per workflow and says so (`truncated at maxPaths=2000`). Branch coverage (planned) keeps giving a useful number beyond that.
- **Activity, timer and signal calls are recognized only when made directly** through the Temporal `workflow` package's functions and types. A call made through your own wrapper function (`myExecute(ctx, ...)`) is not recognized as a Temporal call.
- **Only code that runs in the workflow function itself is on the map.** Signal/Query/Update handlers registered with `SetQueryHandler`, `SetUpdateHandler` or a callback are separate entry points and are not followed.

## Found while building

- **`--version` shows a commit-based version for local builds (M0).** A `go build` inside a git checkout prints something like `v0.0.0-20260928070644-65ea0f53be27+dirty` (Go stamps it from git automatically); `go run` prints `dev`. Only release builds (M9) and `go install ...@vX.Y.Z` show a clean `vX.Y.Z`. This is how Go works, not a bug.
- **The race detector (`go test -race`) needs a C compiler (M0).** CI runs it on Linux, macOS and Windows, where one is installed. On the development machine there is no C compiler, so the local check is plain `go test ./...`.
- **Until M4, a workflow that uses any of these is skipped, not mapped (M2):** `switch`, type switch, `select`, any `for`/`range` loop (even one with no Temporal call), labels or `goto`, `workflow.Selector`, the result of `AwaitWithTimeout`/`ReceiveWithTimeout`/`ReceiveAsync` inside an `if`, or a `defer` that calls the Temporal SDK. `analyze` prints `skipping <workflow>: <construct> at <file>:<line> is supported from M4` and carries on with the other workflows. In the pilot, 5 of 8 workflows are skipped for this reason.
- **Some returns end at a plain `End`, with no kind (M2).** PathKit names the end kind only when it's certain: `nil` is completed; `workflow.NewContinueAsNewError(...)` is continued-as-new; `fmt.Errorf`, `errors.New`, `temporal.New…Error`, or an error variable returned on the error side of its own nil check is failed. A bare `return` with named results, `return doSomething()`, or an error variable nobody checked prints plain `End`.
- **"Where did this `err` come from?" uses the nearest assignment above the `if` (M2).** PathKit looks for the last assignment to that variable that appears **above** the `if` in the source, in the workflow function itself (including the `if`'s own `err := ...;` part, but not inside closures). If `err` is set in different ways in different branches before the check, only the one written last counts. `//pathkit:branch` or `//pathkit:ignore` override the result.
- **An error check must compare the variable with `nil` and nothing else (M2).** `if err != nil && retries > 3` is a plain `if` with `true`/`false` exits, not a `failure`/`success` check. When its true side returns `err`, the end is plain `End`, because the condition doesn't prove `err` is non-nil.
- **`//pathkit:ignore` on a plain `if` assumes the condition is false (M2)**, because an ignored `if` is usually a defensive check that doesn't fire. On an error check it assumes "no error". A pragma counts when it's on the `if`'s own line or the line directly above. That includes a comment trailing the previous statement on that line.
- **Coverage is recorded only by `pathkit test`, not by plain `go test` (M3).** A plain `go test` compiles your original workflow files, so it records nothing, while your tests still pass or fail as usual. `pathkit traces` (and, from M6/M7, `coverage`/`report`) then say `no trace files found in <dir>; coverage is recorded only by "pathkit test" (plain "go test" records nothing)`. (`pathkit prepare`, which prints the `-overlay` flag for your own `go test` command, is planned for M6.)
- **Only workflows `analyze` can map are recorded (M3).** Until M4, a workflow with a loop, `switch`, selector, saga `defer` and so on is not recorded; `pathkit test` prints `not recording <workflow>: ...` and runs its tests normally.
- **A trace doesn't say which test produced it (M3).** The workflow runs on its own goroutine and can't see the `TestXxx` that started it. Reports can show which paths were run, but not by which test. To connect one test to its path, run just that test: `pathkit test <folder> -- -run '^TestName$'`.
- **A run counts only if it finished a `return` of the workflow function (M3).** A run that panics (even while computing its return value), times out, or is stopped by the SDK is saved as `incomplete` and never matched. A panic is never a path.
- **Trace files are written when the workflow function exits (M3).** A test that leaves a workflow blocked forever (for example waiting on a signal that never comes) produces no trace for that run.
- **Child workflows mocked with `OnWorkflow` record nothing (M3)**, because the child's code doesn't run. To cover a child workflow, test it directly (the pilot does this for `PaymentWorkflow`).
- **The staleness hash ignores comments and formatting, but nothing else (M3).** Renaming a variable inside the workflow function marks its old traces `stale`, even though the paths didn't change. `--allow-stale` arrives with `coverage` in M6. Editing a *different* function, or another file, doesn't make a workflow's traces stale.
- **`pathkit test` always runs tests fresh (M3).** It adds `-count=1` (unless you pass your own `-count`), because Go's cached test results would skip the tests and record nothing. So every run takes as long as a fresh `go test -count=1`.
- **A workflow function with more than 8 return values can't be recorded (M3).** `pathkit test` stops with a clear error. Temporal workflows return at most a value and an error, so this shouldn't happen in practice.
- **Some names are reserved in recorded packages (M3).** `pathkit test` adds `pathkitRec`, `pathkitStart`, `pathkitRecorder`, `pathkitTrace`, `pathkitTraceDir` and `pathkitRet1`…`pathkitRet8`. If your package already uses one, it stops with a clear error instead of producing broken code.
- **One broken package stops `analyze` (M2).** If any package in `analyze <folder>/...` doesn't compile, the command fails with `package does not compile: ...` instead of analyzing the others.

## New in Go, because of the proposed design (planned)

- **The package must compile.** PathKit uses Go's real type checker (`go/types` via `go/packages`), so a package with a compile error, or with modules not downloaded, can't be analyzed. A Go toolchain must be installed where PathKit runs (minimum version to be confirmed in M0; research points to 1.26).
- **Workflows are recognized by signature.** A function or method counts as a workflow when its first parameter is `workflow.Context`, its last result is `error`, and it's exported. `RegisterWorkflow` calls are not looked at in v1. An unexported workflow, or one that breaks one of these rules, has to be listed in `.pathkitrc.json`'s `workflows.include`. Dynamic workflows are not supported.
- **Only error checks right after a Temporal call are junctions.** An `if err != nil` counts only when `err` last came from an activity, child workflow, timer, sleep, await or external-signal call **in the same function**. Other error checks are assumed to take the "no error" side. An `err` passed through several variables, or set inside a closure, may not be traced back; use `//pathkit:branch` to force it.
- **Selectors are understood only in the simple shape.** The `Selector` must be created, have its `AddReceive`/`AddFuture`/`AddDefault` calls, and call `Select` in the same function, with inline function literals as callbacks. A selector passed to a helper, callbacks that are named functions, or `Add*` calls made inside a loop are shown with generic exit labels or cause a clear "cannot instrument" error.
- **Saga compensation in `defer` is noted, not branched.** Paths show that a compensation `defer` exists, but "compensation ran" vs "didn't run" is not a separate path in v1, because counting it would create impossible paths (a successful order that also compensated) that could never be covered.
- **`&&` / `||` inside a condition is one junction, not several.** `if a && b` has two exits (`true`/`false`), not one per part. (`go/cfg` adds the whole condition as one node; confirmed in M2.)
- **`ReceiveWithTimeout` is confirmed (M2).** It exists in SDK v1.49.0 (`internal/workflow.go` line 236) and returns `(ok, more bool)` with no error. Using its `ok` in an `if` becomes a `received`/`not received` junction in M4; until then such a workflow is skipped.

## Fixed by design compared to the TypeScript version (to be proven by tests)

These TS limitations should not exist in Go. Each needs a test in its milestone before this list is trusted:

- Listed-path order vs recorded-trace order drifting apart (one model owns all IDs; matching walks the graph). **Proven in M3** (`TestEveryExitRecordedOnce`, `TestAnswerKey`).
- `break`, labeled `break`, `continue` handled wrongly (handled by `go/cfg`).
- `switch` not detected (it's a junction in Go v1).
- One instrumented function per test run, and boilerplate in every test (all in-scope workflows instrumented; existing tests unchanged). **Proven in M3**: the pilot's tests are unchanged, and one `pathkit test ./...` records all three M2 workflows.
- Leftover instrumented copies next to source files (overlay files live in `.pathkit/`). **Proven in M3** (`TestAnswerKey` hashes the pilot before and after).
- Relative trace folder depending on where the test ran (the absolute path is baked into the recorder by `pathkit test`). **Proven in M3.**
- Any edit to the file, even a comment, making all its traces stale (per-workflow, format-insensitive hash). **Proven in M3** (`TestFunctionHash`).
- Success and failure ending at one generic `End` (end kinds: completed / failed / continued-as-new). **Proven in M2** (`TestRules/EndKinds`).
- Scope config matching only file names and only in `report` (workflow names, applied to `analyze` and `report`).
- No CI threshold (`--fail-under`, exit code 2) and no trace cleanup (`pathkit clean`, cleared per `pathkit test` run).
