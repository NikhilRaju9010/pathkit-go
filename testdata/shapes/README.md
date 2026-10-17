# Shape fixtures

Small made-up Temporal workflows, one folder for each code shape PathKit v2 has to understand, plus the "must stay testable" safety cases. Nothing here comes from a real project.

Each folder has an `expected.json` next to its Go file. For every workflow it lists:

- `today`: what PathKit says now for each path (bucket and label). `internal/shapes` checks this is exact.
- `v2`: what v2 should say, written by hand. `status` is `today: correct` when today's answer is the v2 answer and `today: missed` when it is not. The test checks every status. A `provisional` entry is a path that does not exist today (v2 will split a step), so its key is only a name.

When v2 work closes a gap, `today` and `status` change in the same commit as the code, so a reviewer sees it. The `v2` answers are never edited to make a test pass.

| Folder | Shape |
| --- | --- |
| `valueflow` | (a) comma-ok assertion, copy under `if ok`, check of the copy |
| `typeswitch` | (b) type switch on values that were only a string or a `*string` |
| `typednil` | (c) `v == nil` where `v` holds a typed `*string` |
| `guardedtwice` | (d) a value an early return makes non-empty, checked again |
| `elemtype` | (e) `elem.(map[string]interface{})` on a slice built only with maps |
| `zerovalue` | (f) `var s string` then `if s == ""` |
| `helpererr` | (g) a helper that wraps `ExecuteActivity` and returns its error |
| `knowndefect` | (h) a branch dead only because of a bug (needs-review, `known-defect`, set by the rule in this folder's `pathkit.json`) |
| `addrtaken`, `closurewrite`, `ptrreceiver`, `loopchange`, `sidesignal` | (i) to (m) safety cases that must stay testable |
| `notunderstood`, `pointerfield`, `unsafefn`, `skipregions` | (o) to (r) more safety cases for the fact engine: values assigned something not understood, a field behind a pointer, a function that uses `unsafe`, and an `if` or loop PathKit does not decide |
| `recoverfn` | (n, first half) `recover`: must stay testable |
| `gotofn` | (n, second half) `goto`: PathKit refuses it with a clear message (`TestGotoIsRefused`) |

Run them with `go test ./internal/shapes -v`; the log lists every `today: missed` answer (the gap list for v2). To print today's answers for a new fixture: `PATHKIT_SHAPES_DUMP=1 go test ./internal/shapes -run Dump -v`.

A folder may also hold a `pathkit.json`: the entries a person writes for a shape only a person can decide. The test reads it when it is there.

The folder is its own Go module (`go vet ./...` inside it must pass).
