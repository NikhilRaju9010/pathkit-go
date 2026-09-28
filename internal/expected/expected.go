// Package expected reads testdata/pilot/EXPECTED.md, the answer key written
// by hand in M1. It is used only by tests. The answer key is never edited
// to make a test pass: if PathKit and the key disagree and the key looks
// wrong, the project owner decides.
package expected

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Workflow is one "## N. `pkg.Workflow` (dir/file.go)" section.
type Workflow struct {
	Name  string
	File  string   // e.g. "orders/orders.go", relative to testdata/pilot
	Count int      // the "**K paths:**" number
	Paths []string // path keys in the key's order, e.g. "J1.false J2.success|completed"
	Tests []Test   // from the "**Tests (...)**" line
}

// Test is one "`TestName` → N" entry: that test's run takes path N.
type Test struct {
	Name string
	Path int // 1-based, into Paths
}

var (
	sectionRe = regexp.MustCompile("^## \\d+\\. `([\\w.]+)` \\(([\\w./]+)\\)")
	countRe   = regexp.MustCompile(`^\*\*(\d+) paths:\*\*`)
	pathRe    = regexp.MustCompile(`^(\d+)\. (.*)$`)
	stepRe    = regexp.MustCompile(`J(\d+)\b.*?--(.+?)-->`)
	endRe     = regexp.MustCompile(`End \(([a-z-]+)\)`)
	testsRe   = regexp.MustCompile(`^\*\*Tests \(`)
	testRe    = regexp.MustCompile("`(Test\\w+)` → (\\d+)")
)

// Read parses the answer key.
func Read(path string) (map[string]*Workflow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]*Workflow{}
	var cur *Workflow
	inPaths := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			cur = &Workflow{Name: m[1], File: m[2]}
			out[cur.Name] = cur
			inPaths = false
			continue
		}
		if cur == nil {
			continue
		}
		if m := countRe.FindStringSubmatch(line); m != nil {
			cur.Count, _ = strconv.Atoi(m[1])
			inPaths = true
			continue
		}
		if testsRe.MatchString(line) {
			for _, m := range testRe.FindAllStringSubmatch(line, -1) {
				n, _ := strconv.Atoi(m[2])
				cur.Tests = append(cur.Tests, Test{Name: m[1], Path: n})
			}
			continue
		}
		if !inPaths {
			continue
		}
		m := pathRe.FindStringSubmatch(line)
		if m == nil {
			if strings.TrimSpace(line) != "" {
				inPaths = false
			}
			continue
		}
		key, err := keyFromLine(m[2])
		if err != nil {
			return nil, fmt.Errorf("%s, %s: %v", path, cur.Name, err)
		}
		cur.Paths = append(cur.Paths, key)
	}
	return out, sc.Err()
}

// keyFromLine turns "J1 --false--> J2 ChargeCard --success--> End (completed)"
// into "J1.false J2.success|completed".
func keyFromLine(line string) (string, error) {
	var steps []string
	for _, m := range stepRe.FindAllStringSubmatch(line, -1) {
		steps = append(steps, "J"+m[1]+"."+m[2])
	}
	end := endRe.FindStringSubmatch(line)
	if end == nil {
		return "", fmt.Errorf("no end kind in %q", line)
	}
	return strings.Join(steps, " ") + "|" + end[1], nil
}
