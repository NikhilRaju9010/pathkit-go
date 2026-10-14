# Contributing to PathKit for Go

PathKit lists the execution paths of Temporal Go workflows and shows which of them the tests ran. User documentation: `README.md`, `GETTING-STARTED.md`, `SETUP-GUIDE.md`, `docs/TEAM-GUIDE.md`, `LIMITATIONS.md`. Design: `docs/DESIGN.md`.

## Rules

1. **Read `docs/DESIGN.md` before changing behaviour.** A change to a design decision there is a design change: describe the problem and the options first, agree on it, then update that file together with the code.
2. **One source of truth.** Junction and edge IDs come only from `internal/model`; the instrumenter and the matcher never compute their own (DESIGN D5). Every label PathKit can print has a definition in `internal/classify/definitions.go`.
3. **Never silently wrong.** When PathKit cannot map or record something safely, it says so with a clear message instead of guessing. When unsure about a path, it stays testable or needs-review; a path a test ran is never hidden.
4. **Examples and test data are made up.** They come from the sample project in `testdata/pilot` (with its answer key `testdata/pilot/EXPECTED.md`) or from the small fixtures in `testdata/fixtures`. Nothing taken from a real-world codebase goes into the repo: no names, code, numbers, paths or reports.
5. **Output changes are additive.** Existing text, `--json` keys and HTML stay the same unless the change is agreed; new fields are added. Golden files and the frozen model snapshot (`testdata/golden/model/snapshot.txt`) are never edited to make a test pass: a different result is a bug or a design question.
6. **Plain English** in messages, docs and comments. Explain a Go or Temporal term the first time it appears in user documentation.
7. **Every change ships with tests and docs:** a case it must handle and a near-miss it must not; `SETUP-GUIDE.md` and `LIMITATIONS.md` updated when behaviour or limits change.

## Before every commit

```bash
gofmt -l $(git ls-files '*.go')     # must print nothing
go vet ./...
go test ./...                       # full run, a few minutes; go test -short ./... for a quick one
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...   # as in CI
(cd testdata/pilot && go vet ./... && go test ./...)
```

`go test -race` runs in CI (it needs a C compiler).
