# PathKit for Go

PathKit answers one question about [Temporal](https://temporal.io) workflows written in Go: **which execution paths exist, and which of them do my tests actually run?**

Every `if`, every "did the activity fail?", every "did the signal arrive before the timer?" in a workflow is a decision. A *path* is one complete way through those decisions. `pathkit analyze` lists the paths; `pathkit test` runs your normal Go tests and records which path each workflow run took; `pathkit report` shows which paths are covered and which are missing. With path classification switched on, PathKit also tells impossible paths apart from real gaps, and a CI check can fail the build when new untested paths appear.

PathKit is a command-line tool. It does not go into your `go.mod`, and your tests stay unchanged.

## Install

Requires Go 1.26 or newer.

```bash
git clone https://github.com/NikhilRaju9010/pathkit-go.git
cd pathkit-go
go build -o pathkit ./cmd/pathkit
./pathkit --version
```

Move `pathkit` to a folder on your `PATH`. On Windows, build with `go build -o pathkit.exe ./cmd/pathkit` (Windows only runs programs whose name ends in `.exe`) and run it as `pathkit`.

## Five-minute start on your Temporal Go project

```bash
cd your-project
pathkit analyze ./...    # list every workflow's paths
pathkit test ./...       # run your tests and record which paths they took (plain "go test" records nothing)
pathkit report ./...     # covered and missed paths, per workflow and in total
```

PathKit never changes your source files, `go.mod`, `go.sum` or tests; it writes only a `.pathkit/` folder and files you ask for (details: [what PathKit writes](SETUP-GUIDE.md#what-pathkit-writes-and-never-changes)). Run the commands from the folder that holds `go.mod`. A step-by-step run with classification, `explain`, `--hints`, `--unmatched` and the CI baseline is in [docs/TEAM-GUIDE.md](docs/TEAM-GUIDE.md), section 7; every command and flag is in [SETUP-GUIDE.md](SETUP-GUIDE.md).

## Documentation

- [GETTING-STARTED.md](GETTING-STARTED.md): a first walkthrough.
- [docs/TEAM-GUIDE.md](docs/TEAM-GUIDE.md): one page on path classification, `explain`, `--hints`, `--unmatched` and the CI check.
- [SETUP-GUIDE.md](SETUP-GUIDE.md): every command, flag, config key and error message.
- [LIMITATIONS.md](LIMITATIONS.md): what PathKit does and does not cover.
- [docs/DESIGN.md](docs/DESIGN.md): how it works inside.

The sample project in `testdata/pilot` is a small set of made-up workflows with tests; every example in the documentation can be run on it.
