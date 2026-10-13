# Changelog

All notable changes to PathKit for Go are listed here. PathKit has not had a tagged release yet, so everything below is "Unreleased". The first version also contains the base tool: `analyze`, `test`, `coverage` and `report` (text, JSON and HTML), and the `.pathkitrc.json` scope settings.

## Unreleased

### Added: path classification

Some paths can never run (a loop the code always enters, the same value tested twice with opposite answers). PathKit can now tell those apart from real gaps. It is off until you turn it on with `"classification": {"enabled": true}` in `.pathkitrc.json`; with it off, the coverage numbers and exit codes are as before and the JSON only gains new fields.

- **Buckets and labels.** Every path is `testable`, `cannot-be-tested`, `needs-special-test` or `needs-review`, and an unusual path carries a reason label with a plain-English definition.
- **Two coverage numbers.** *Testable coverage* counts only the paths a test can run; *overall coverage* counts every path. `--coverage-basis` picks which one `--fail-under` checks. A path a test ran always counts as testable.
- **Config overrides and rules.** Mark one path by its stable id (`mark_untestable`, `mark_testable`), or write a rule that names a combination of decisions that cannot happen (with an optional `without` list). Every entry needs a reason; entries that no longer match are reported as stale.
- **Source comments.** `//pathkit:untestable` on a decision marks one exit of it, with a reason.
- **Automatic detectors**, each with a definition and a reason on every path it marks: `closed-signal-channel`, `constant-condition`, `contradictory`, `contradictory-get-result`, `same-list-loops`, `list-built-from-list`, `loop-exit-before-first-trip`, `version-gate-old-branch` and `history-limit`. A detector can be switched off with a reason. When unsure, PathKit leaves a path testable or marks it needs-review.
- **HTML glossary.** The shareable web page shows each label's definition and which detector or rule decided it.

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

### Changed

- Output changes are additive: existing text, `--json` keys and HTML stay as they were, and new fields are added.

### Known limits

See [LIMITATIONS.md](LIMITATIONS.md). In short: PathKit cannot tell which tests were skipped, and the automatic detectors assume the Temporal SDK does not close signal channels, that no custom interceptors are used, and that activity results are not changed after `Get`.
