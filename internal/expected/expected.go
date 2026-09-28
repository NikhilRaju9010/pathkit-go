// Package expected reads testdata/pilot/EXPECTED.md, the answer key written
// by hand in M1. It is used only by tests. The answer key is never edited
// to make a test pass: if PathKit and the key disagree and the key looks
// wrong, the project owner decides.
//
// The reader is strict: it never skips something it doesn't understand.
// Every path line must be read completely (every step, the end station,
// the compensation note), every test mention must be read, and the key's
// own totals must add up. Anything else is an error naming the line.
package expected

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Workflow is one "## N. `pkg.Workflow` (dir/file.go)" section.
type Workflow struct {
	Name       string
	File       string // e.g. "orders/orders.go", relative to testdata/pilot
	Count      int    // the "**K paths:**" number
	Paths      []Path // in the key's order
	Tests      []Test // from the "**Tests (...)**" line and the bullets under it
	Covered    int    // X in "X/Y covered"
	NotCovered []int  // from "Not covered: ...", nil when the key doesn't say
	Line       int    // line of the section header
}

// Path is one numbered path line.
type Path struct {
	Key          string // e.g. "J1.false J2.success|completed"
	Compensation bool   // the line carries "[compensation (defer)]"
	Line         int
}

// Test is one "`TestName` → N" entry: that test's run takes path N.
type Test struct {
	Name string
	Path int // 1-based, into Paths
	Line int
}

const compensationNote = "[compensation (defer)]"

var (
	sectionRe   = regexp.MustCompile("^## (\\d+)\\. `([\\w.]+)` \\(([\\w./]+)\\)$")
	countRe     = regexp.MustCompile(`^\*\*(\d+) paths:\*\*$`)
	pathLineRe  = regexp.MustCompile(`^(\d+)\. (.*)$`)
	testsRe     = regexp.MustCompile(`^\*\*Tests \([\w./]+\), (\d+)/(\d+) covered:\*\*(.*)$`)
	testRe      = regexp.MustCompile("`(Test\\w+)` → (\\d+)")
	bulletRe    = regexp.MustCompile("^- `(Test\\w+)` → (\\d+)(.*)$")
	foundRe     = regexp.MustCompile(`^Found \((\d+)\): (.*)$`)
	backtickRe  = regexp.MustCompile("`([\\w.]+)`")
	rowRe       = regexp.MustCompile(`^\| (.+?) \| (.+?) \| (.+?) \|$`)
	parensRe    = regexp.MustCompile(`\([^)]*\)`)
	numberRe    = regexp.MustCompile(`\d+`)
	junctionRe  = regexp.MustCompile(`^J(\d+)\b`)
	anyJRe      = regexp.MustCompile(`\bJ\d+\b`)
	endRe       = regexp.MustCompile(`^End(?: \(([a-z-]+)\))?`)
	totalCellRe = regexp.MustCompile(`^\*\*(\d+)\*\*$`)
	totalCovRe  = regexp.MustCompile(`^\*\*(\d+) \((\d+\.\d)%\)\*\*$`)
)

// Read parses and cross-checks the answer key. Any line it can't fully
// understand is an error of the form "<file>:<line>: <reason>".
func Read(path string) (map[string]*Workflow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := &reader{file: path, out: map[string]*Workflow{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		r.line++
		if err := r.readLine(sc.Text()); err != nil {
			return nil, r.at(r.line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := r.finishSection(); err != nil {
		return nil, r.at(r.line, err)
	}
	if err := r.crossCheck(); err != nil {
		return nil, err
	}
	return r.out, nil
}

// sectionError is a problem with a whole section, reported at its header.
type sectionError struct {
	line int
	err  error
}

func (e *sectionError) Error() string { return e.err.Error() }

// at puts "<file>:<line>: " in front of err.
func (r *reader) at(line int, err error) error {
	var se *sectionError
	if errors.As(err, &se) {
		line, err = se.line, se.err
	}
	return fmt.Errorf("%s:%d: %v", r.file, line, err)
}

type reader struct {
	file string
	line int
	out  map[string]*Workflow

	part  string // "intro", "found", "workflow", "totals", "disagreements"
	cur   *Workflow
	mode  string // inside a workflow section: "", "paths", "tests"
	found []string
	rows  map[string][2]int // Totals table: name -> paths, covered
	total *[3]float64       // Totals row: paths, covered, percent

	sawTests      bool
	sawNotCovered bool
	countLine     int // line of the current "**K paths:**"
}

func (r *reader) readLine(line string) error {
	if strings.HasPrefix(line, "## ") {
		if err := r.finishSection(); err != nil {
			return err
		}
		return r.header(line)
	}
	consumed, err := r.body(line)
	if err != nil || consumed {
		return err
	}
	// Nothing read this line. Make sure it holds nothing that should have
	// been: a path step or a test mention outside its place.
	if strings.Contains(line, "-->") {
		return fmt.Errorf("a path step (-->) outside a path list; the reader can't place it")
	}
	if strings.Contains(line, "`Test") || strings.Contains(line, "→") {
		return fmt.Errorf("a test mention outside a **Tests** line or its bullets; the reader can't place it")
	}
	return nil
}

func (r *reader) header(line string) error {
	r.cur, r.mode = nil, ""
	switch {
	case strings.HasPrefix(line, "## Workflows PathKit must find"):
		r.part = "found"
	case line == "## Totals":
		r.part = "totals"
		r.rows = map[string][2]int{}
	case line == "## Disagreements":
		r.part = "disagreements"
	default:
		m := sectionRe.FindStringSubmatch(line)
		if m == nil {
			return fmt.Errorf("unknown section header %q", line)
		}
		if n, _ := strconv.Atoi(m[1]); n != len(r.out)+1 {
			return fmt.Errorf("section number %d, want %d", n, len(r.out)+1)
		}
		if r.out[m[2]] != nil {
			return fmt.Errorf("%s has two sections", m[2])
		}
		r.part = "workflow"
		r.cur = &Workflow{Name: m[2], File: m[3], Line: r.line}
		r.out[r.cur.Name] = r.cur
		r.sawTests, r.sawNotCovered = false, false
	}
	return nil
}

// body reads one line inside the current part. consumed reports whether
// the line carried data that was read.
func (r *reader) body(line string) (consumed bool, err error) {
	switch r.part {
	case "found":
		m := foundRe.FindStringSubmatch(line)
		if m == nil {
			return false, nil
		}
		for _, n := range backtickRe.FindAllStringSubmatch(m[2], -1) {
			r.found = append(r.found, n[1])
		}
		if want, _ := strconv.Atoi(m[1]); want != len(r.found) {
			return false, fmt.Errorf("the Found (%d) line lists %d workflows", want, len(r.found))
		}
		return true, nil
	case "totals":
		return r.totalsRow(line)
	case "workflow":
		return r.workflowLine(line)
	case "disagreements":
		return true, nil // free text written by people, not answer data
	}
	return false, nil
}

func (r *reader) workflowLine(line string) (bool, error) {
	w := r.cur
	trimmed := strings.TrimSpace(line)

	if r.mode == "paths" {
		if trimmed == "" {
			return false, nil
		}
		if m := pathLineRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n != len(w.Paths)+1 {
				return false, fmt.Errorf("path number %d, want %d", n, len(w.Paths)+1)
			}
			key, comp, err := ParsePathLine(m[2])
			if err != nil {
				return false, err
			}
			w.Paths = append(w.Paths, Path{Key: key, Compensation: comp, Line: r.line})
			return true, nil
		}
		r.mode = ""
		if !strings.HasPrefix(line, "**Tests (") {
			return false, fmt.Errorf("expected the **Tests (...)** line right after the path list, got %q", line)
		}
		if len(w.Paths) != w.Count {
			return false, &sectionError{r.countLine, fmt.Errorf("%s: says %d paths but lists %d", w.Name, w.Count, len(w.Paths))}
		}
	}

	if r.mode == "tests" {
		if m := bulletRe.FindStringSubmatch(line); m != nil {
			if err := r.addTest(m[1], m[2]); err != nil {
				return false, err
			}
			if strings.Contains(m[3], "`Test") || strings.Contains(m[3], "→") {
				return false, fmt.Errorf("more than one test on a bullet line")
			}
			return true, nil
		}
		if trimmed != "" {
			r.mode = ""
		}
	}

	if m := countRe.FindStringSubmatch(line); m != nil {
		if w.Count != 0 {
			return false, fmt.Errorf("a second **K paths:** line in %s", w.Name)
		}
		w.Count, _ = strconv.Atoi(m[1])
		r.countLine = r.line
		r.mode = "paths"
		return true, nil
	}
	if strings.HasPrefix(line, "**Tests") {
		m := testsRe.FindStringSubmatch(line)
		if m == nil {
			return false, fmt.Errorf("can't read the **Tests** line")
		}
		if r.sawTests {
			return false, fmt.Errorf("a second **Tests** line in %s", w.Name)
		}
		r.sawTests = true
		w.Covered, _ = strconv.Atoi(m[1])
		if total, _ := strconv.Atoi(m[2]); total != w.Count {
			return false, fmt.Errorf("\"%s/%s covered\" but the section lists %d paths", m[1], m[2], w.Count)
		}
		rest := m[3]
		entries := testRe.FindAllStringSubmatch(rest, -1)
		if len(entries) != strings.Count(rest, "`Test") || len(entries) != strings.Count(rest, "→") {
			return false, fmt.Errorf("a test mention on the **Tests** line doesn't have the form `TestName` → N")
		}
		for _, e := range entries {
			if err := r.addTest(e[1], e[2]); err != nil {
				return false, err
			}
		}
		if err := r.notCovered(rest); err != nil {
			return false, err
		}
		r.mode = "tests"
		return true, nil
	}
	if strings.Contains(line, "Not covered:") {
		return true, r.notCovered(line)
	}
	return false, nil
}

func (r *reader) addTest(name, n string) error {
	path, _ := strconv.Atoi(n)
	if path < 1 || path > r.cur.Count {
		return fmt.Errorf("%s → %d, but %s has paths 1..%d", name, path, r.cur.Name, r.cur.Count)
	}
	for _, t := range r.cur.Tests {
		if t.Name == name {
			return fmt.Errorf("%s is listed twice", name)
		}
	}
	r.cur.Tests = append(r.cur.Tests, Test{Name: name, Path: path, Line: r.line})
	return nil
}

// notCovered reads "Not covered: 1 (cancelled), 4 (rejected)." if present.
func (r *reader) notCovered(text string) error {
	_, list, ok := strings.Cut(text, "Not covered:")
	if !ok {
		return nil
	}
	if r.sawNotCovered {
		return fmt.Errorf("a second \"Not covered:\" in %s", r.cur.Name)
	}
	r.sawNotCovered = true
	r.cur.NotCovered = []int{}
	for _, s := range numberRe.FindAllString(parensRe.ReplaceAllString(list, ""), -1) {
		n, _ := strconv.Atoi(s)
		r.cur.NotCovered = append(r.cur.NotCovered, n)
	}
	return nil
}

func (r *reader) totalsRow(line string) (bool, error) {
	m := rowRe.FindStringSubmatch(line)
	if m == nil {
		return false, nil
	}
	name, paths, covered := m[1], m[2], m[3]
	if name == "Workflow" || name == "---" {
		return true, nil
	}
	if name == "**Total**" {
		p := totalCellRe.FindStringSubmatch(paths)
		c := totalCovRe.FindStringSubmatch(covered)
		if p == nil || c == nil {
			return false, fmt.Errorf("can't read the Total row")
		}
		pn, _ := strconv.Atoi(p[1])
		cn, _ := strconv.Atoi(c[1])
		pct, _ := strconv.ParseFloat(c[2], 64)
		r.total = &[3]float64{float64(pn), float64(cn), pct}
		return true, nil
	}
	pn, err1 := strconv.Atoi(paths)
	cn, err2 := strconv.Atoi(covered)
	if err1 != nil || err2 != nil {
		return false, fmt.Errorf("can't read the numbers in the Totals row for %s", name)
	}
	if _, dup := r.rows[name]; dup {
		return false, fmt.Errorf("%s appears twice in Totals", name)
	}
	r.rows[name] = [2]int{pn, cn}
	return true, nil
}

// finishSection checks the workflow section that just ended.
func (r *reader) finishSection() error {
	w := r.cur
	if w == nil {
		return nil
	}
	fail := func(format string, args ...any) error {
		return &sectionError{w.Line, fmt.Errorf("%s: %s", w.Name, fmt.Sprintf(format, args...))}
	}
	switch {
	case w.Count == 0:
		return fail("no **K paths:** line")
	case len(w.Paths) != w.Count:
		return fail("says %d paths but lists %d", w.Count, len(w.Paths))
	case !r.sawTests || len(w.Tests) == 0:
		return fail("no tests listed")
	}
	covered := map[int]bool{}
	for _, t := range w.Tests {
		covered[t.Path] = true
	}
	if len(covered) != w.Covered {
		return fail("says %d paths are covered, but its tests cover %d different paths", w.Covered, len(covered))
	}
	if w.NotCovered != nil {
		for _, n := range w.NotCovered {
			if n < 1 || n > w.Count || covered[n] {
				return fail("\"Not covered\" lists path %d, which is out of range or covered by a test", n)
			}
		}
		if len(covered)+len(w.NotCovered) != w.Count {
			return fail("covered (%d) plus not covered (%d) is not all %d paths", len(covered), len(w.NotCovered), w.Count)
		}
	}
	r.cur = nil
	return nil
}

// crossCheck compares the workflow sections with the "Found" list and the
// Totals table, so a missing or miscounted section can't go unnoticed.
func (r *reader) crossCheck() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s: %s", r.file, fmt.Sprintf(format, args...))
	}
	var names []string
	for name := range r.out {
		names = append(names, name)
	}
	found := slices.Clone(r.found)
	slices.Sort(names)
	slices.Sort(found)
	if !slices.Equal(names, found) {
		return fail("the \"Found\" list %v and the workflow sections %v differ", found, names)
	}
	if r.total == nil {
		return fail("no **Total** row in the Totals table")
	}
	paths, covered := 0, 0
	for _, name := range names {
		w := r.out[name]
		row, ok := r.rows[name]
		if !ok {
			return fail("%s is missing from the Totals table", name)
		}
		if row != [2]int{w.Count, w.Covered} {
			return fail("Totals says %s has %d paths, %d covered; its section says %d, %d", name, row[0], row[1], w.Count, w.Covered)
		}
		paths += w.Count
		covered += w.Covered
	}
	if len(r.rows) != len(names) {
		return fail("the Totals table has %d workflow rows, want %d", len(r.rows), len(names))
	}
	pct := math.Round(float64(covered)/float64(paths)*1000) / 10
	if r.total[0] != float64(paths) || r.total[1] != float64(covered) || r.total[2] != pct {
		return fail("the Total row says %v paths, %v covered (%v%%); the sections add up to %d, %d (%.1f%%)",
			r.total[0], r.total[1], r.total[2], paths, covered, pct)
	}
	return nil
}

// ParsePathLine reads one path, e.g.
//
//	J1 --success--> J2 child PaymentWorkflow --failure--> End (failed) [compensation (defer)]
//	J3 --default--> retry --> J1 --exit--> End (completed), zero polls
//
// into its key ("J1.success J2.failure|failed") and whether it carries the
// compensation note. "retry --> J1" is the loop J1's retry step. Text
// after the end station is allowed only as a remark starting with "," or
// ".", and may not contain anything that looks like a step or a note.
func ParsePathLine(line string) (key string, compensation bool, err error) {
	var steps []string
	rest := line
	retry := false
	for {
		rest = strings.TrimLeft(rest, " ")
		if m := endRe.FindStringSubmatch(rest); m != nil {
			if retry {
				return "", false, fmt.Errorf(`"retry -->" must be followed by the loop's junction, not the end`)
			}
			rest = rest[len(m[0]):]
			key = strings.Join(steps, " ") + "|" + m[1]
			break
		}
		if strings.HasPrefix(rest, "retry -->") {
			if retry {
				return "", false, fmt.Errorf(`two "retry -->" in a row`)
			}
			retry = true
			rest = rest[len("retry -->"):]
			continue
		}
		m := junctionRe.FindStringSubmatch(rest)
		if m == nil {
			return "", false, fmt.Errorf("can't read the step at %q", rest)
		}
		rest = rest[len(m[0]):]
		open := strings.Index(rest, "--")
		if open < 0 {
			return "", false, fmt.Errorf("junction J%s has no --exit--> after it", m[1])
		}
		desc := rest[:open]
		if anyJRe.MatchString(desc) {
			return "", false, fmt.Errorf("the description %q of J%s names another junction", strings.TrimSpace(desc), m[1])
		}
		rest = rest[open+2:]
		close := strings.Index(rest, "-->")
		if close < 0 {
			return "", false, fmt.Errorf("the exit of J%s has no closing -->", m[1])
		}
		label := rest[:close]
		if label == "" || strings.HasPrefix(label, ">") || strings.Contains(label, "--") {
			return "", false, fmt.Errorf("junction J%s has an empty or malformed exit label", m[1])
		}
		rest = rest[close+3:]
		if retry {
			steps = append(steps, "J"+m[1]+".retry")
			retry = false
		}
		steps = append(steps, "J"+m[1]+"."+label)
	}

	rest = strings.TrimLeft(rest, " ")
	if strings.HasPrefix(rest, compensationNote) {
		compensation = true
		rest = strings.TrimLeft(rest[len(compensationNote):], " ")
	}
	if rest != "" && !strings.HasPrefix(rest, ".") && !strings.HasPrefix(rest, ",") {
		return "", false, fmt.Errorf("unexpected text after the end station: %q", rest)
	}
	if strings.Contains(rest, "-->") || strings.Contains(rest, "[") || anyJRe.MatchString(rest) {
		return "", false, fmt.Errorf("the remark after the end station looks like a step or a note: %q", rest)
	}
	return key, compensation, nil
}
