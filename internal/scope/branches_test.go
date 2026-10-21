package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCfg(t *testing.T, body string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".pathkitrc.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Read(p)
}

func TestDefaultGradeBands(t *testing.T) {
	g := DefaultGrades()
	for _, c := range []struct {
		pct  float64
		want string
	}{{100, GradeExemplary}, {90, GradeExemplary}, {89.9, GradeCommendable}, {75, GradeCommendable}, {74.9, GradeAcceptable}, {60, GradeAcceptable}, {59.9, GradeNeedsWork}, {0, GradeNeedsWork}} {
		if got := g.Grade(c.pct); got != c.want {
			t.Errorf("%v%%: %s, want %s", c.pct, got, c.want)
		}
	}
	var none *Config
	if none.BranchSettings().TargetGrade != GradeCommendable {
		t.Error("no config: the target grade is commendable")
	}
}

func TestBranchesSectionRead(t *testing.T) {
	c, err := readCfg(t, `{"branches": {"grades": {"acceptable": 50, "commendable": 70, "exemplary": 95}, "target_grade": "exemplary"}}`)
	if err != nil {
		t.Fatal(err)
	}
	b := c.BranchSettings()
	if b.Grades.Grade(72) != GradeCommendable || b.TargetGrade != GradeExemplary || b.Grades.Floor(b.TargetGrade) != 95 {
		t.Errorf("bad settings: %+v", b)
	}
}

func TestBranchesSectionRefusesNonsense(t *testing.T) {
	for body, want := range map[string]string{
		`{"branches": {"grades": {"acceptable": 80, "commendable": 70}}}`: "acceptable < commendable",
		`{"branches": {"grades": {"exemplary": 101}}}`:                    "exemplary <= 100",
		`{"branches": {"target_grade": "great"}}`:                         "target_grade",
		`{"branches": {"grade": {}}}`:                                     "unknown key",
	} {
		if _, err := readCfg(t, body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want it to mention %q", body, err, want)
		}
	}
}
