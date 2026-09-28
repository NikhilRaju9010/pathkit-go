# Known Limitations — PathKit for Go

PathKit either detects a pattern correctly or clearly does not detect it; it never silently guesses. This file is the honest list of what it does and doesn't cover.

**Status:** research phase. Nothing has been built yet, so everything below is a **planned** limitation that follows from the proposed design in `CLAUDE.md`. Each entry must be confirmed (or corrected) by a real test when its milestone is built, and then its "planned" tag removed. New limitations found while building go here too.

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

## New in Go, because of the proposed design (planned)

- **The package must compile.** PathKit uses Go's real type checker (`go/types` via `go/packages`), so a package with a compile error, or with modules not downloaded, can't be analyzed. A Go toolchain must be installed where PathKit runs (minimum version to be confirmed in M0; research points to 1.26).
- **Workflows are recognized by signature.** A function or method counts as a workflow when its first parameter is `workflow.Context`, its last result is `error`, and it's exported. `RegisterWorkflow` calls are not looked at in v1. An unexported workflow, or one that breaks one of these rules, has to be listed in `.pathkitrc.json`'s `workflows.include`. Dynamic workflows are not supported.
- **Only error checks right after a Temporal call are junctions.** An `if err != nil` counts only when `err` last came from an activity, child workflow, timer, sleep, await or external-signal call **in the same function**. Other error checks are assumed to take the "no error" side. An `err` passed through several variables, or set inside a closure, may not be traced back; use `//pathkit:branch` to force it.
- **Selectors are understood only in the simple shape.** The `Selector` must be created, have its `AddReceive`/`AddFuture`/`AddDefault` calls, and call `Select` in the same function, with inline function literals as callbacks. A selector passed to a helper, callbacks that are named functions, or `Add*` calls made inside a loop are shown with generic exit labels or cause a clear "cannot instrument" error.
- **Saga compensation in `defer` is noted, not branched.** Paths show that a compensation `defer` exists, but "compensation ran" vs "didn't run" is not a separate path in v1, because counting it would create impossible paths (a successful order that also compensated) that could never be covered.
- **`&&` / `||` inside a condition is one junction, not several.** `if a && b` has two exits (`true`/`false`), not one per part. (`go/cfg` doesn't model short-circuiting.)
- **`panic` is not a path.** A panic is not drawn as an exit; a trace from a run that panicked is marked incomplete and ignored.
- **Coverage is recorded only by `pathkit test`, not by plain `go test`.** A plain `go test` compiles your original workflow files, so it records nothing, while your tests still pass or fail as usual; `coverage`/`report` then show 0% and say that no trace files were found. Use `pathkit test`, or add the `-overlay` flag that `pathkit prepare` prints to your own `go test` command.
- **Trace files are only written when the workflow function returns.** A test that times out or leaves a workflow blocked forever produces no trace for that run.
- **Child workflows mocked with `OnWorkflow` record nothing** for the child (its code doesn't run). To cover a child workflow, test it directly.
- **Staleness hash ignores comments and formatting but nothing else.** Renaming a variable inside the workflow function marks its old traces stale, even though the paths didn't change. `--allow-stale` is the escape hatch.
- **`ReceiveWithTimeout` support is unconfirmed.** Research sources disagree on whether the SDK's signal channel has this method; it will be checked in M2 and this entry updated.

## Fixed by design compared to the TypeScript version (to be proven by tests)

These TS limitations should not exist in Go. Each needs a test in its milestone before this list is trusted:

- Listed-path order vs recorded-trace order drifting apart (one model owns all IDs; matching walks the graph).
- `break`, labeled `break`, `continue` handled wrongly (handled by `go/cfg`).
- `switch` not detected (it's a junction in Go v1).
- One instrumented function per test run, and boilerplate in every test (all in-scope workflows instrumented; existing tests unchanged).
- Leftover instrumented copies next to source files (overlay files live in `.pathkit/`).
- Relative trace folder depending on where the test ran (absolute path passed by `pathkit test`).
- Any edit to the file, even a comment, making all its traces stale (per-workflow, format-insensitive hash).
- Success and failure ending at one generic `End` (end kinds: completed / failed / continued-as-new).
- Scope config matching only file names and only in `report` (workflow names, applied to `analyze` and `report`).
- No CI threshold (`--fail-under`, exit code 2) and no trace cleanup (`pathkit clean`, cleared per `pathkit test` run).
