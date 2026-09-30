# Getting Started with PathKit for Go

A quick, plain-English walkthrough: what PathKit is, how to get it, and how to point it at **your own** Temporal Go project to see your workflow's paths and how well your tests cover them.

For the full reference (every flag, every config key, every error message), see [SETUP-GUIDE.md](SETUP-GUIDE.md). This file is just the shortest path from "never used it" to "it's working on my project."

---

## 1. What PathKit is, in one paragraph

Think of a Temporal workflow like a subway map. Every `if`, every "did the activity fail?", every "did the signal arrive before the timer?" is a junction — a fork in the tracks. A **path** is one complete trip from the start station to an end station. PathKit's `analyze` command draws that map for you. Its `coverage` and `report` commands then colour in which trips your tests actually took, so you can see which parts of your workflow have never been run by a test.

PathKit is a **command-line tool**, not a Go library. You never `import` it in your code, and it never goes in your `go.mod`. You install it once (like `git` or `golangci-lint`), then run it from the terminal, pointed at your own project's folder.

---

## 2. What you need first

- **Go 1.26 or newer** installed (`go version` to check). PathKit needs this because it uses Go's own compiler tools to read your code correctly.
- **A Go project that compiles.** PathKit reads your code the same way `go build` does, so `go build ./...` must succeed in your project before PathKit can analyze it. (It doesn't need to *pass its tests* — just build.)
- **Workflows written with the real Temporal Go SDK** (`go.temporal.io/sdk/workflow`), since PathKit recognizes Temporal's own function names (`ExecuteActivity`, `workflow.Selector`, and so on) — not a custom wrapper around them.

---

## 3. Install PathKit

PathKit hasn't had its first tagged release yet (that's milestone M9 — see `PLAN.md`), so for now you build it from a checkout of this repo. This is a one-time step; the binary you produce works on any of your own Go projects afterwards.

```bash
git clone https://github.com/NikhilRaju9010/pathkit-go.git
cd pathkit-go
go build -o pathkit ./cmd/pathkit
```

That gives you one file, `pathkit`, in the current folder. Check it works:

```bash
./pathkit --version
```

Put it somewhere on your `PATH` so you can run `pathkit` from any folder, for example:

```bash
mv pathkit ~/go/bin/          # or anywhere already on your PATH
pathkit --version
```

Once PathKit has a tagged release, this step will shrink to one line: `go install github.com/NikhilRaju9010/pathkit-go/cmd/pathkit@latest`.

---

## 4. "Adding" PathKit to your project

There's nothing to add to your `go.mod`, and nothing to import. PathKit reads your project's files from the outside, the same way a linter does. All you do is **run the `pathkit` command from inside your project**, pointing it at the folder (or file) with your workflows.

```bash
cd /path/to/your-temporal-project
pathkit --version
```

If that prints a version, you're set up. Everything from here on is run from your own project's folder.

---

## 5. Step one: see your workflow's paths (`analyze`)

```bash
pathkit analyze ./path/to/your/workflow.go
```

or point it at a whole package (or your whole project with `./...`):

```bash
pathkit analyze ./internal/workflows/...
```

You'll see output like this (from PathKit's own sample project):

```
Workflow: orders.OrderWorkflow
Total paths: 3

  1. Start -> if in.AmountCents <= 0 --true--> End (completed)

  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)

  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)
```

Each numbered line is one complete trip through the workflow. If a workflow you expect to see doesn't show up, it's almost always one of these two things:

- **It isn't recognized as a workflow.** PathKit finds workflows automatically by shape: the function is **exported** (capitalized name), its **first parameter is `workflow.Context`**, and its **last return value is `error`**. A helper function or an unexported workflow won't be picked up on its own — see section 6 of `SETUP-GUIDE.md` (`.pathkitrc.json`'s `workflows.include`) to add it by name.
- **It uses a construct PathKit can't map yet** (`goto`, Go's own `select`, or an unusual shape of `workflow.Selector`). PathKit will say exactly why in an error, naming the file and line.

Useful flags: `--summary` (just the count per workflow), `--limit N` (only the first N paths), `--mermaid` (a diagram instead of text), `--out file.txt` (also save the output).

---

## 6. Step two: run your tests and record which paths they took

This is the part people most often get wrong, so read it carefully:

> **A plain `go test` records nothing.** Your tests still pass or fail normally, so it's easy not to notice. To record which paths your tests actually took, you must run your tests **through PathKit**:

```bash
pathkit test ./...
```

This is a drop-in replacement for `go test ./...` — it runs your existing tests exactly as they are (no changes needed to your test files), but it also secretly compiles a marked-up copy of your workflow code (never touching your real files) so it can watch which path each test run takes. You'll see something like:

```
pathkit test: recorded 21 traces (21 complete) in .pathkit/traces
```

If you'd rather run `go test` yourself (for example your CI already has its own `go test` step), run `pathkit prepare ./...` first — it prints the exact `go test -overlay=...` command to use instead of your normal one.

Add `.pathkit/` to your `.gitignore` — it holds PathKit's working files and your recorded traces.

---

## 7. Step three: see your coverage

For one workflow or one file:

```bash
pathkit coverage ./internal/workflows/orders/...
```

```
Workflow: orders.OrderWorkflow
Total paths: 3
Covered: 2/3 (66.7%)
Branches: 3/4 (75.0%)

Covered paths:
  1. Start -> if in.AmountCents <= 0 --true--> End (completed)
  2. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --failure--> End (failed)
Untested paths:
  3. Start -> if in.AmountCents <= 0 --false--> ChargeCard (activity) --success--> End (completed)
```

(This example is illustrative: it shows what output looks like when one path has no test. In the sample project from section 11, all three `OrderWorkflow` paths are tested, so you would see `3/3 (100.0%)` and `Untested paths: (none)`.)

If you ran `pathkit test ./...` (the whole project) but ask `coverage` about one package, the `Traces:` line at the bottom counts the other packages' traces as `unknown workflow`, and PathKit prints a warning on stderr for each one. That is harmless: those traces just belong to workflows you didn't ask about, and the numbers above them are still correct.

For the whole project at once, with a High/Medium/Low priority per workflow telling you where to add tests first:

```bash
pathkit report ./...
```

Add `--fail-under 80` to either command to make PathKit exit with code `2` when coverage is below that percentage — handy for CI. And add `--html` to `report` to get one shareable web page (`.pathkit/report.html`) you can open in a browser or send to a teammate — no server, no internet needed.

If you ever see `no trace files found ...`, it means step 6 was skipped, or you ran plain `go test` instead of `pathkit test` — go back and re-run your tests through PathKit.

---

## 8. A complete first run, start to finish

```bash
cd your-temporal-project
pathkit analyze ./internal/workflows/...     # see the map
pathkit test ./...                           # run your tests and record which paths they took
pathkit coverage ./internal/workflows/...    # see the results for one package
pathkit report ./... --html                  # or the whole project, as a web page
open .pathkit/report.html                    # (or xdg-open / start, depending on your OS)
```

---

## 9. Narrowing things down (optional, but worth knowing early)

If some workflows can't be tested yet (they need a real external system, say), or PathKit finds a workflow it can't map, you don't have to live with a lower coverage number forever. A `.pathkitrc.json` file in your project lets you say which workflows count, each with a required reason:

```json
{
  "packages": ["./internal/workflows/..."],
  "workflows": {
    "exclude": [
      { "name": "LegacyBillingWorkflow", "reason": "needs a real bank sandbox" }
    ]
  },
  "failUnder": 80
}
```

Every exclusion is always shown in `report`'s output with its reason, so nobody can quietly inflate the coverage number. Full details are in section 6 of `SETUP-GUIDE.md`.

---

## 10. Quick troubleshooting

| You see | It means | Fix |
| --- | --- | --- |
| `package does not compile: ...` | Your project (or the folder you pointed at) doesn't build. | Run `go build ./...` in your project and fix the error shown. |
| `no trace files found in .pathkit/traces; coverage is recorded only by "pathkit test" ...` | You ran plain `go test`, or haven't run tests yet. | Run `pathkit test ./...`, then try `coverage`/`report` again. |
| `... has no exported workflow functions to analyze` | Nothing in that file/folder matches the workflow rule (section 5 above). | Point PathKit at the right file, or check the function is exported with `workflow.Context` first and `error` last. |
| `in scope but not analyzable: <workflow>: ...` | PathKit found the workflow but can't map one of its constructs yet. | The message names the construct and line. Rewrite it, or exclude it in `.pathkitrc.json` with a reason. |
| `Directory not found: ...` / `Workflow file not found: ...` | Wrong path, or you're not in the folder you think you are. | Check the path and your current folder (`pwd`). |

For the complete error table and every flag, see [SETUP-GUIDE.md, section 7](SETUP-GUIDE.md#7-errors-and-exit-codes). For known gaps and edge cases, see [LIMITATIONS.md](LIMITATIONS.md).

---

## 11. Try it on PathKit's own sample project first

If you want to see all of this working before pointing PathKit at your real project, this repo ships a small pretend Temporal project you can practice on:

```bash
cd pathkit-go/testdata/pilot
../../pathkit analyze ./...
../../pathkit test ./...
../../pathkit report ./... --html
```

It has 8 small workflows (orders, approvals, polling, shipping, a saga, a scheduled report) with real tests already written, so you can see `analyze`, `test`, `coverage` and `report` all working together with no setup of your own.
