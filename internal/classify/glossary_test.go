package classify

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every label PathKit can print has a definition. The label strings are
// found in the source: any string literal assigned to a Label field, passed
// to label(...), or listed by a detector's Labels(), in every non-test file
// of the packages that decide or print a label.
func TestEveryPrintableLabelIsDefined(t *testing.T) {
	assigned := regexp.MustCompile(`\.Label\s*(?:,[^=\n]*)?=\s*"([a-z][a-z0-9-]*)"|Label:\s*"([a-z][a-z0-9-]*)"|\bLabels\(\) \[\]string\{([^}]*)\}|c\.Bucket, c\.Label[^=\n]*= [^"\n]*, "([a-z][a-z0-9-]*)"`)
	quoted := regexp.MustCompile(`"([a-z][a-z0-9-]*)"`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range assigned.FindAllStringSubmatch(string(src), -1) {
			for _, g := range m[1:] {
				for _, q := range quoted.FindAllStringSubmatch(`"`+g+`"`, -1) {
					found[q[1]] = f
				}
			}
		}
		// c.Bucket, c.Label, c.Confidence, ... = Bucket, "label", ...
		multi := regexp.MustCompile(`c\.Bucket, c\.Label, [^=]*=\s*[A-Za-z]+, "([a-z][a-z0-9-]*)"`)
		for _, m := range multi.FindAllStringSubmatch(string(src), -1) {
			found[m[1]] = f
		}
	}
	if len(found) < 6 {
		t.Fatalf("the scan found only %v: the pattern no longer matches the code", found)
	}
	for label, file := range found {
		if label == "manual" || label == "needs-review" {
			continue // defined; listed to keep the check honest below
		}
		if _, ok := Lookup(KindLabel, label); !ok {
			t.Errorf("label %q (in %s) has no definition in definitions.go", label, file)
		}
	}
	for _, label := range []string{"manual", "needs-review"} {
		if _, ok := Lookup(KindLabel, label); !ok {
			t.Errorf("label %q has no definition", label)
		}
	}
	// Every detector's labels are defined (also checked in detect_test.go).
	for _, d := range detectors {
		for _, l := range d.Labels() {
			if _, ok := Lookup(KindLabel, l); !ok {
				t.Errorf("detector %s: label %q has no definition", d.Name(), l)
			}
		}
	}
}

// LIMITATIONS.md lists, by name, every label PathKit never sets by itself
// ("not detected yet"). The report says the same, from the definitions.
func TestNotDetectedYetLabelsAreInLimitations(t *testing.T) {
	src, err := os.ReadFile("../../LIMITATIONS.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	n := 0
	for _, d := range Definitions() {
		if d.Kind != KindLabel || !d.NotDetectedYet {
			continue
		}
		n++
		if !strings.Contains(text, "`"+d.Name+"`") {
			t.Errorf("label %q is marked 'not detected yet' but LIMITATIONS.md does not mention it", d.Name)
		}
	}
	if n == 0 {
		t.Error("no label is marked 'not detected yet'; this test would pass for the wrong reason")
	}
}

func TestDefinitionsVersion(t *testing.T) {
	v := DefinitionsVersion()
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(v) {
		t.Errorf("version %q is not 8 hex digits", v)
	}
	if v != DefinitionsVersion() {
		t.Error("the version is not stable")
	}
}

// Every definition has a plain explanation, a reason, an example, an
// answer to "can it happen in production?" and a "what to do" line.
func TestEveryDefinitionIsComplete(t *testing.T) {
	for _, d := range Definitions() {
		for field, v := range map[string]string{
			"Meaning": d.Meaning, "Why": d.Why, "Example": d.Example,
			"PossibleInProduction": d.PossibleInProduction, "WhatToDo": d.WhatToDo,
		} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s %q has an empty %s", d.Kind, d.Name, field)
			}
		}
	}
}
