# PathKit for Go — Setup Guide

PathKit answers one question about Temporal Go workflows: **"which execution paths exist, and which ones do my tests actually run?"**

> **Status: early development (milestone M0 of M9).** The `pathkit` command exists, but its three commands are not implemented yet. This guide grows with each milestone. See `PLAN.md` for progress.

> **Important: coverage is recorded only by `pathkit test`, not by plain `go test`.**
> A plain `go test` compiles your original workflow code, so it records nothing, and your tests still pass or fail as usual, which makes this easy to miss. `coverage` and `report` would then show 0% and say that no trace files were found. Always record coverage with `pathkit test` (or add the `-overlay` flag that `pathkit prepare` prints to your own `go test` command). *(Both commands arrive in M3.)*

## Commands

| Command | What it does | Available |
| --- | --- | --- |
| `pathkit analyze <file>` | Lists every possible path through the workflows in one file. | coming in M2 |
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

## 3. Errors and exit codes

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
| `pathkit: unknown command "xyz"` | Misspelled command. | Use `analyze`, `coverage` or `report`. |
| `not implemented yet (planned for Mx)` | The command exists but its work arrives in a later milestone. | Wait for that milestone. |

Flags can go before or after the file name: `pathkit analyze orders.go --summary` and `pathkit analyze --summary orders.go` mean the same thing.
