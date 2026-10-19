package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/version"
)

// The export formats of "report --format".
var exportFormats = []string{"junit", "cobertura", "sarif"}

func exportFor(format string, m *measured) ([]byte, error) {
	switch format {
	case "junit":
		return junitXML(m.res)
	case "cobertura":
		return coberturaXML(m)
	case "sarif":
		return sarifJSON(m)
	}
	return nil, userError("--format must be one of %s, got %q", strings.Join(exportFormats, ", "), format)
}

// ---- JUnit: one test case per workflow, passing when its main number meets its target ----

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Suite    []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

func junitXML(res coverage.Result) ([]byte, error) {
	suite := junitSuite{Name: "pathkit main numbers"}
	for _, w := range res.Workflows {
		c := junitCase{Name: w.Name, Classname: w.Main.Kind + " coverage", SystemOut: render.MainLine(w)}
		switch {
		case !w.Main.Measured:
			c.Skipped = &junitSkipped{Message: "nothing to measure for the main number"}
			suite.Skipped++
		case !w.Main.Met:
			c.Failure = &junitFailure{Type: "below-target",
				Message: fmt.Sprintf("%s %d/%d (%s) is below the target %s", w.Main.Kind+" coverage", w.Main.Covered, w.Main.Total, render.Pct(w.Main.Percent, nil), render.Threshold(w.Main.Target)),
				Text:    w.Main.Why}
			suite.Failures++
		}
		suite.Cases = append(suite.Cases, c)
	}
	suite.Tests = len(suite.Cases)
	doc := junitSuites{Name: "pathkit", Tests: suite.Tests, Failures: suite.Failures, Skipped: suite.Skipped, Suite: []junitSuite{suite}}
	return xmlDoc(doc)
}

func xmlDoc(v any) ([]byte, error) {
	out, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

// ---- Cobertura: branch coverage per decision line ----

type coberturaDoc struct {
	XMLName         xml.Name       `xml:"coverage"`
	LineRate        string         `xml:"line-rate,attr"`
	BranchRate      string         `xml:"branch-rate,attr"`
	LinesCovered    int            `xml:"lines-covered,attr"`
	LinesValid      int            `xml:"lines-valid,attr"`
	BranchesCovered int            `xml:"branches-covered,attr"`
	BranchesValid   int            `xml:"branches-valid,attr"`
	Complexity      int            `xml:"complexity,attr"`
	Version         string         `xml:"version,attr"`
	Timestamp       int64          `xml:"timestamp,attr"`
	Sources         []string       `xml:"sources>source"`
	Packages        []coberturaPkg `xml:"packages>package"`
}

type coberturaPkg struct {
	Name       string           `xml:"name,attr"`
	LineRate   string           `xml:"line-rate,attr"`
	BranchRate string           `xml:"branch-rate,attr"`
	Complexity int              `xml:"complexity,attr"`
	Classes    []coberturaClass `xml:"classes>class"`
}

type coberturaClass struct {
	Name       string          `xml:"name,attr"`
	Filename   string          `xml:"filename,attr"`
	LineRate   string          `xml:"line-rate,attr"`
	BranchRate string          `xml:"branch-rate,attr"`
	Complexity int             `xml:"complexity,attr"`
	Lines      []coberturaLine `xml:"lines>line"`
}

type coberturaLine struct {
	Number            int    `xml:"number,attr"`
	Hits              int    `xml:"hits,attr"`
	Branch            bool   `xml:"branch,attr"`
	ConditionCoverage string `xml:"condition-coverage,attr"`
}

func rate(c, t int) string {
	if t == 0 {
		return "1"
	}
	return fmt.Sprintf("%.4f", float64(c)/float64(t))
}

func coberturaXML(m *measured) ([]byte, error) {
	type lineKey struct {
		file string
		line int
	}
	type lineAcc struct{ taken, total int }
	lines := map[lineKey]*lineAcc{}
	for _, wf := range m.workflows {
		var w *coverage.WorkflowResult
		for i := range m.res.Workflows {
			if m.res.Workflows[i].Name == wf.Name {
				w = &m.res.Workflows[i]
			}
		}
		if w == nil || wf.Graph == nil {
			continue
		}
		took := w.TookExits()
		for _, j := range wf.Graph.AllJunctions() {
			k := lineKey{filepath.ToSlash(load.DisplayPath(j.Pos.Filename)), j.Pos.Line}
			a := lines[k]
			if a == nil {
				a = &lineAcc{}
				lines[k] = a
			}
			for _, e := range j.Exits {
				a.total++
				if took[e.ID.String()] {
					a.taken++
				}
			}
		}
	}
	byFile := map[string][]coberturaLine{}
	files := map[string]*lineAcc{}
	allLines, hitLines, allBr, hitBr := 0, 0, 0, 0
	for k, a := range lines {
		hits := 0
		if a.taken > 0 {
			hits = 1
			hitLines++
		}
		allLines++
		allBr += a.total
		hitBr += a.taken
		pct := 0
		if a.total > 0 {
			pct = a.taken * 100 / a.total
		}
		byFile[k.file] = append(byFile[k.file], coberturaLine{Number: k.line, Hits: hits, Branch: true, ConditionCoverage: fmt.Sprintf("%d%% (%d/%d)", pct, a.taken, a.total)})
		if files[k.file] == nil {
			files[k.file] = &lineAcc{}
		}
		files[k.file].taken += a.taken
		files[k.file].total += a.total
	}
	doc := coberturaDoc{LineRate: rate(hitLines, allLines), BranchRate: rate(hitBr, allBr), LinesCovered: hitLines, LinesValid: allLines,
		BranchesCovered: hitBr, BranchesValid: allBr, Version: "pathkit-go " + version.Version, Timestamp: time.Now().Unix(), Sources: []string{"."}}
	pkgs := map[string]*coberturaPkg{}
	var names []string
	for _, f := range sortedKeys(byFile) {
		ls := byFile[f]
		sort.Slice(ls, func(i, j int) bool { return ls[i].Number < ls[j].Number })
		covered := 0
		for _, l := range ls {
			if l.Hits > 0 {
				covered++
			}
		}
		dir := path.Dir(f)
		p := pkgs[dir]
		if p == nil {
			p = &coberturaPkg{Name: dir}
			pkgs[dir] = p
			names = append(names, dir)
		}
		acc := files[f]
		p.Classes = append(p.Classes, coberturaClass{Name: path.Base(f), Filename: f, LineRate: rate(covered, len(ls)), BranchRate: rate(acc.taken, acc.total), Lines: ls})
	}
	sort.Strings(names)
	for _, n := range names {
		p := pkgs[n]
		taken, total, cov, n2 := 0, 0, 0, 0
		for _, c := range p.Classes {
			for _, l := range c.Lines {
				n2++
				if l.Hits > 0 {
					cov++
				}
			}
			a := files[c.Filename]
			taken += a.taken
			total += a.total
		}
		p.LineRate, p.BranchRate = rate(cov, n2), rate(taken, total)
		doc.Packages = append(doc.Packages, *p)
	}
	return xmlDoc(doc)
}

// ---- SARIF 2.1.0: the top missed items and the places PathKit could not read ----

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Rules   []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	ShortDescription sarifMessage `json:"shortDescription"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifLocation struct {
	Physical sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	Artifact sarifArtifact `json:"artifactLocation"`
	Region   *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

func sarifJSON(m *measured) ([]byte, error) {
	driver := sarifDriver{Name: "PathKit", Version: version.Version, Rules: []sarifRule{
		{ID: "pathkit/missed-scenario", Name: "MissedScenario", ShortDescription: sarifMessage{"A scenario of a workflow that no test ran"}},
		{ID: "pathkit/unread", Name: "NotFullyRead", ShortDescription: sarifMessage{"A place PathKit could not fully read, so the numbers around it are less sure"}},
	}}
	run := sarifRun{Tool: sarifTool{Driver: driver}, Results: []sarifResult{}}
	loc := func(file string, line int) []sarifLocation {
		if file == "" {
			return nil
		}
		l := sarifLocation{Physical: sarifPhysical{Artifact: sarifArtifact{URI: filepath.ToSlash(file)}}}
		if line > 0 {
			l.Physical.Region = &sarifRegion{StartLine: line}
		}
		return []sarifLocation{l}
	}
	for _, it := range advice.TestsToWriteFirst(m.res, load.DisplayPath, render.TopTests) {
		run.Results = append(run.Results, sarifResult{RuleID: "pathkit/missed-scenario", Level: "warning",
			Message:   sarifMessage{fmt.Sprintf("[%s] %s: %s (suggested test %s, covers %s)", it.Tag, it.Workflow, it.Hint, it.TestName, it.Covers())},
			Locations: loc(it.File, it.Line)})
	}
	for _, u := range m.res.Unread {
		run.Results = append(run.Results, sarifResult{RuleID: "pathkit/unread", Level: "note",
			Message:   sarifMessage{fmt.Sprintf("%s (%s): %s", u.Kind, u.Workflow, u.Effect)},
			Locations: loc(u.File, u.Line)})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(sarifLog{Schema: "https://json.schemastore.org/sarif-2.1.0.json", Version: "2.1.0", Runs: []sarifRun{run}}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
