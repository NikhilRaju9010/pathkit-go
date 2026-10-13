#!/usr/bin/env bash
# Run PathKit on a project WITHOUT changing the project, and collect all the
# results (reports, traces, versions) in one folder to review or share.
#
#   scripts/project-baseline.sh <project-folder> [output-folder]
#
# What it does:
#   1. Copies the project (without .git and .pathkit) into a temporary
#      folder. Everything below runs in the COPY: pathkit test writes its
#      marked-up files into a .pathkit folder, and the copy takes that, not
#      your project.
#   2. Runs: pathkit test, analyze, report (text), report (JSON), coverage (JSON).
#   3. Saves the results, the recorded traces and the versions used into the
#      output folder (default: ./pathkit-baseline-<date-time>).
#   4. Checks that your project folder did not change while it ran.
#
# Limits: the copy is built on its own, so a go.mod with a relative
# "replace ../sibling" or a go.work that points outside the project will not
# build in the copy (copy those folders too, or run pathkit by hand). The
# copy includes vendor/ and node_modules/ if you have them.
#
# Optional: PATHKIT_TEST_ARGS="..." passes flags to go test (see below).
#
# Needs: bash, tar, go (the Go toolchain), and the pathkit program (on PATH,
# or set PATHKIT=/path/to/pathkit). Works on Linux, macOS, and Windows with
# Git Bash or WSL.
#
# Nothing here is committed anywhere. The output folder has traces and
# reports (workflow names, decision text such as "if x > 0", file names),
# but not your source files. Look through it before sharing it.
set -uo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <project-folder> [output-folder]" >&2
  exit 1
fi
PROJECT=$(cd "$1" 2>/dev/null && pwd) || { echo "not a folder: $1" >&2; exit 1; }
OUT=${2:-./pathkit-baseline-$(date +%Y%m%d-%H%M%S)}
PATHKIT=${PATHKIT:-pathkit}

PATHKIT_FOUND=$(command -v "$PATHKIT" 2>/dev/null) || { echo "pathkit not found. Put it on PATH, or run: PATHKIT=/path/to/pathkit $0 ..." >&2; exit 1; }
# Make it an absolute path: the script changes folder below.
case "$PATHKIT_FOUND" in
  /*) PATHKIT=$PATHKIT_FOUND ;;
  *) PATHKIT="$(cd "$(dirname "$PATHKIT_FOUND")" && pwd)/$(basename "$PATHKIT_FOUND")" ;;
esac
command -v go >/dev/null 2>&1 || { echo "go not found. PathKit needs the Go toolchain to build your project." >&2; exit 1; }
[ -f "$PROJECT/go.mod" ] || echo "note: $PROJECT has no go.mod at its top; pathkit will run from there anyway." >&2

# Refuse an output folder inside the project BEFORE creating anything.
case "$OUT" in /*) OUT_ABS=$OUT ;; *) OUT_ABS="$PWD/$OUT" ;; esac
case "$OUT_ABS" in
  "$PROJECT"|"$PROJECT"/*) echo "the output folder must be outside the project ($PROJECT)" >&2; exit 1 ;;
esac
mkdir -p "$OUT_ABS"
OUT=$(cd "$OUT_ABS" && pwd)
case "$OUT" in # again, now that symbolic links are resolved
  "$PROJECT"|"$PROJECT"/*) echo "the output folder must be outside the project ($PROJECT)" >&2; rmdir "$OUT" 2>/dev/null; exit 1 ;;
esac

# A fingerprint of the project's files (names, sizes, times), to prove it did
# not change. Uses only commands that Linux and macOS both have.
hash_stdin() { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi | cut -d' ' -f1; }
fingerprint() {
  (cd "$PROJECT" && LC_ALL=C find . -path ./.git -prune -o -type f -exec ls -l {} + | LC_ALL=C sort -k9) | hash_stdin
}
BEFORE=$(fingerprint)

WORK=$(mktemp -d)
cleanup() { [ "${KEEP_WORK:-}" = "1" ] && echo "kept the working copy: $WORK" || rm -rf "$WORK"; }
trap cleanup EXIT

echo "copying the project to a temporary folder..."
(cd "$PROJECT" && tar --exclude=./.git --exclude=./.pathkit -cf - .) | (cd "$WORK" && tar xf -)
cd "$WORK" || exit 1

{
  echo "date:    $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "project: $PROJECT"
  echo "pathkit: $("$PATHKIT" --version 2>&1)"
  echo "go:      $(go version)"
  if [ -d "$PROJECT/.git" ] && command -v git >/dev/null 2>&1; then
    echo "git:     $(git -C "$PROJECT" rev-parse HEAD 2>/dev/null) ($(git -C "$PROJECT" status --porcelain 2>/dev/null | wc -l | tr -d ' ') changed files)"
  fi
} > "$OUT/versions.txt"
[ -f "$PROJECT/.pathkitrc.json" ] && cp "$PROJECT/.pathkitrc.json" "$OUT/pathkitrc.json.copy"

rm -rf "$OUT/traces"
STATUS="$OUT/exit-codes.txt"
: > "$STATUS"
FAILED=0
run() { # run <name> <command...>: save stdout and stderr, note the exit code
  local name=$1 rc; shift
  echo "running: pathkit $*"
  "$PATHKIT" "$@" > "$OUT/$name.out" 2> "$OUT/$name.err"
  rc=$?
  echo "$name: exit $rc" >> "$STATUS"
  [ "$rc" -eq 0 ] || FAILED=$((FAILED + 1))
}

# The project's own tests run here (a failing test does not stop the rest).
# PATHKIT_TEST_ARGS (optional) is passed to "go test" after "--", for example
# PATHKIT_TEST_ARGS="-count=1 -skip ^TestNeedsDatabase$" to leave out tests
# that need a database or the network. Not set: nothing is added.
TEST_ARGS=()
if [ -n "${PATHKIT_TEST_ARGS:-}" ]; then
  read -r -a EXTRA <<< "$PATHKIT_TEST_ARGS"
  TEST_ARGS=(-- "${EXTRA[@]}")
fi
run test        test ./... --traces "$OUT/traces" ${TEST_ARGS[@]+"${TEST_ARGS[@]}"}
run analyze     analyze ./...
run report      report ./... --traces "$OUT/traces" --no-color
run report-json report ./... --traces "$OUT/traces" --json
run coverage-json coverage ./... --traces "$OUT/traces" --json
mv "$OUT/report.out" "$OUT/report.txt"
mv "$OUT/report-json.out" "$OUT/report.json"
mv "$OUT/coverage-json.out" "$OUT/coverage.json"
mv "$OUT/analyze.out" "$OUT/analyze.txt"
mv "$OUT/test.out" "$OUT/test-output.txt"
for f in test analyze report report-json coverage-json; do
  [ -s "$OUT/$f.err" ] || rm -f "$OUT/$f.err"
done

AFTER=$(fingerprint)
if [ "$BEFORE" = "$AFTER" ]; then
  echo "project folder unchanged: yes" | tee -a "$OUT/versions.txt"
else
  echo "project folder unchanged: NO (something else changed it while this ran)" | tee -a "$OUT/versions.txt"
fi

echo
echo "done. Results are in: $OUT"
echo "  report.txt / report.json / coverage.json / analyze.txt / test-output.txt"
echo "  traces/ (one file per recorded workflow run)"
echo "  exit-codes.txt, versions.txt, *.err (only if something was written to stderr)"
echo
cat "$STATUS"
echo
if [ "$FAILED" -ge 5 ]; then
  echo "every PathKit command failed; see the .err files in $OUT" >&2
  exit 1
fi
echo "Results are in $OUT"
