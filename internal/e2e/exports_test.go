package e2e

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The exports are well-formed and have the fields tools read.
func TestExports(t *testing.T) {
	fixture, traces := recordScenarios(t)
	cfg := writeConfig(t, `{"classification": {"enabled": true, "targets": {"default": 50}}}`)
	dir := t.TempDir()
	run := func(format string) []byte {
		t.Helper()
		out := filepath.Join(dir, "out."+format)
		if _, stderr, code := pathkit(t, "report", fixture, "--traces", traces, "--config", cfg, "--format", format, "--output", out); code != 0 {
			t.Fatalf("%s: exit %d\n%s", format, code, stderr)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	// JUnit: one test case per workflow, failing below its target
	var junit struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Suites   []struct {
			Cases []struct {
				Name    string `xml:"name,attr"`
				Failure *struct {
					Message string `xml:"message,attr"`
				} `xml:"failure"`
				Skipped *struct{} `xml:"skipped"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	raw := run("junit")
	if !strings.HasPrefix(string(raw), "<?xml") {
		t.Errorf("junit has no XML header:\n%.200s", raw)
	}
	if err := xml.Unmarshal(raw, &junit); err != nil {
		t.Fatalf("junit is not well-formed: %v\n%s", err, raw)
	}
	if junit.Tests != 7 || len(junit.Suites) != 1 || len(junit.Suites[0].Cases) != 7 {
		t.Errorf("junit: %d tests, %d suites", junit.Tests, len(junit.Suites))
	}
	failed, passed := 0, 0
	for _, c := range junit.Suites[0].Cases {
		if c.Failure != nil {
			failed++
			if !strings.Contains(c.Failure.Message, "below the target 50%") {
				t.Errorf("failure message: %q", c.Failure.Message)
			}
		} else if c.Skipped == nil {
			passed++
		}
	}
	if failed != junit.Failures || failed == 0 || passed == 0 {
		t.Errorf("junit: %d failures (attribute says %d), %d passed", failed, junit.Failures, passed)
	}

	// Cobertura: branch coverage per decision line
	var cob struct {
		LineRate    string `xml:"line-rate,attr"`
		BranchRate  string `xml:"branch-rate,attr"`
		BranchValid int    `xml:"branches-valid,attr"`
		Packages    []struct {
			Name    string `xml:"name,attr"`
			Classes []struct {
				Filename string `xml:"filename,attr"`
				Lines    []struct {
					Number int    `xml:"number,attr"`
					Branch bool   `xml:"branch,attr"`
					Cond   string `xml:"condition-coverage,attr"`
				} `xml:"lines>line"`
			} `xml:"classes>class"`
		} `xml:"packages>package"`
	}
	raw = run("cobertura")
	if err := xml.Unmarshal(raw, &cob); err != nil {
		t.Fatalf("cobertura is not well-formed: %v\n%.300s", err, raw)
	}
	lines := 0
	for _, p := range cob.Packages {
		for _, c := range p.Classes {
			if !strings.HasSuffix(c.Filename, "scenarios.go") {
				t.Errorf("filename %q", c.Filename)
			}
			for _, l := range c.Lines {
				lines++
				if !l.Branch || !strings.Contains(l.Cond, "%") || l.Number == 0 {
					t.Errorf("line %+v", l)
				}
			}
		}
	}
	if lines == 0 || cob.LineRate == "" || cob.BranchRate == "" || cob.BranchValid == 0 {
		t.Errorf("cobertura: %d lines %+v", lines, cob)
	}

	// SARIF 2.1.0
	var sarif struct {
		Version string `json:"version"`
		Schema  string `json:"$schema"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					Physical struct {
						Artifact struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	raw = run("sarif")
	if err := json.Unmarshal(raw, &sarif); err != nil {
		t.Fatalf("sarif is not JSON: %v", err)
	}
	if sarif.Version != "2.1.0" || !strings.Contains(sarif.Schema, "sarif") || len(sarif.Runs) != 1 || sarif.Runs[0].Tool.Driver.Name != "PathKit" || len(sarif.Runs[0].Tool.Driver.Rules) < 2 {
		t.Fatalf("sarif header: %+v", sarif)
	}
	missed, located := 0, 0
	for _, r := range sarif.Runs[0].Results {
		if r.RuleID == "" || r.Message.Text == "" || (r.Level != "warning" && r.Level != "note") {
			t.Errorf("result %+v", r)
		}
		if r.RuleID == "pathkit/missed-scenario" {
			missed++
		}
		if len(r.Locations) == 1 && r.Locations[0].Physical.Region.StartLine > 0 && r.Locations[0].Physical.Artifact.URI != "" {
			located++
		}
	}
	if missed == 0 || located == 0 {
		t.Errorf("sarif has %d missed-scenario results, %d with a location", missed, located)
	}

	// without --output the report goes to stdout; a wrong format is refused
	if out, _, code := pathkit(t, "report", fixture, "--traces", traces, "--config", cfg, "--format", "junit"); code != 0 || !strings.HasPrefix(out, "<?xml") {
		t.Errorf("junit to stdout: exit %d %.100s", code, out)
	}
	if _, stderr, code := pathkit(t, "report", fixture, "--traces", traces, "--format", "html"); code == 0 || !strings.Contains(stderr, "--format must be one of") {
		t.Errorf("wrong format: exit %d %q", code, stderr)
	}
}
