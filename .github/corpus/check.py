#!/usr/bin/env python3
"""Nightly check on the public sample corpus (a clone of the Temporal Go samples
at a pinned commit; see .github/workflows/nightly.yml).

usage: check.py <pathkit-binary> <corpus-folder> <baseline.json> [--write-baseline]

It runs PathKit on the corpus (analyze, then the tests with the recorder, then
report) and fails when
  - a command that must work does not (analyze; report: it also stops with an
    internal error if a path a test ran is not testable, which is a false claim),
  - a path a test ran is not in the testable bucket (a false claim),
  - the "could not fully read" panel has more entries than the baseline,
  - fewer workflows are read, or fewer traces are recorded, than the baseline,
  - more traces fit no path than the baseline allows.
Nothing here needs a secret. The corpus is public.
"""
import json
import os
import shutil
import subprocess
import sys

CONFIG = {
    "workflows": {"exclude": [
        {"name": "splitmerge_selector.SampleSplitMergeSelectorWorkflow", "reason": "PathKit does not map this selector shape"},
        {"name": "timer.SampleTimerWorkflow", "reason": "PathKit does not map this selector shape"},
    ]},
    "classification": {"enabled": True},
}


def run(cmd, cwd, check=True, **kw):
    print("+", " ".join(cmd), flush=True)
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, **kw)
    if check and p.returncode != 0:
        print(p.stdout[-3000:])
        print(p.stderr[-3000:], file=sys.stderr)
        sys.exit(f"FAILED: {' '.join(cmd)} exited {p.returncode}")
    return p


def main():
    if len(sys.argv) < 4:
        sys.exit(__doc__)
    pathkit, corpus, baseline_file = os.path.abspath(sys.argv[1]), os.path.abspath(sys.argv[2]), sys.argv[3]
    write = "--write-baseline" in sys.argv
    with open(os.path.join(corpus, ".pathkitrc.json"), "w") as f:
        json.dump(CONFIG, f, indent=2)
    shutil.rmtree(os.path.join(corpus, ".pathkit"), ignore_errors=True)

    run([pathkit, "analyze", "./..."], corpus)

    # The tests of the corpus use the Temporal test environment; switch the
    # network off when this machine can (a test that needs a server then fails
    # instead of reaching one). Failing tests do not stop the check: the traces
    # of the tests that ran are what is measured.
    test = [pathkit, "test", "./..."]
    if subprocess.run(["unshare", "-rn", "true"], capture_output=True).returncode == 0:
        test.append("--no-network")
    else:
        print("note: unshare -rn is not available here; the tests run with the network on")
    p = run(test, corpus, check=False)
    print(p.stderr[-1500:])

    p = run([pathkit, "report", "./...", "--json"], corpus)
    report = json.loads(p.stdout)

    workflows = report["workflows"]
    unread = len(report.get("unread", []))
    traces = report["traces"]
    false_claims = [
        f'{w["name"]} path {x["number"]} ({x["stableId"]}) was run by a test but is {x["bucket"]}'
        for w in workflows for x in w["pathList"] if x["covered"] and x["bucket"] != "testable"
    ]
    now = {
        "pinnedCommit": os.environ.get("CORPUS_COMMIT", ""),
        "workflowsRead": len(workflows),
        "unreadPanel": unread,
        "tracesCounted": traces["counted"],
        "tracesUnmatched": traces["unmatched"],
    }
    print(json.dumps(now, indent=2))

    if write:
        with open(baseline_file, "w") as f:
            json.dump(now, f, indent=2)
            f.write("\n")
        print("baseline written to", baseline_file)
        return

    with open(baseline_file) as f:
        base = json.load(f)
    problems = []
    problems += false_claims
    if unread > base["unreadPanel"]:
        problems.append(f'the "could not fully read" panel has {unread} entries, the baseline allows {base["unreadPanel"]}')
    if now["workflowsRead"] < base["workflowsRead"]:
        problems.append(f'{now["workflowsRead"]} workflows are read, the baseline has {base["workflowsRead"]}')
    # a few tests may be skipped by the machine; a big drop means the recorder stopped working
    if now["tracesCounted"] < int(base["tracesCounted"] * 0.9):
        problems.append(f'{now["tracesCounted"]} traces counted, the baseline has {base["tracesCounted"]}')
    if now["tracesUnmatched"] > base["tracesUnmatched"]:
        problems.append(f'{now["tracesUnmatched"]} traces fit no path, the baseline allows {base["tracesUnmatched"]}')
    if problems:
        print("\nPROBLEMS:")
        for x in problems:
            print(" -", x)
        sys.exit(1)
    print("\nThe corpus is as good as the baseline.")


if __name__ == "__main__":
    main()
