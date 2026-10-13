package model_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func buildUntestable(t *testing.T, name string) (*model.Graph, error) {
	t.Helper()
	pkg := "untestable"
	switch name {
	case "NoReason", "BlankReason", "NoExit", "UnknownExit", "UnknownSetting", "Unquoted", "NothingToMark", "OnTransparent", "StackedMarks", "NearMissSpace", "NearMissTypo", "NearMissPlural":
		pkg = "untestablebad"
	}
	wf, ok := workflowsIn(t, "../../testdata/fixtures/"+pkg)[pkg+"."+name]
	if !ok {
		t.Fatalf("fixture workflow untestable.%s not found", name)
	}
	return model.Build(wf)
}

func TestUntestableMarks(t *testing.T) {
	tests := []struct {
		workflow string
		want     []string // "junction label --exit: reason"
	}{
		{"Checkout", []string{"if express && amount == 0 --true: express is never combined with a zero amount"}},
		{"SameLine", []string{"if n < 0 --true: n is a count"}},
		{"Switched", []string{`switch kind --case "legacy": legacy orders are gone`}},
		{"TwoMarks", []string{"if a --true: first reason", "if b --false: second reason"}},
		{"NotAComment", nil}, // //pathkit:untestables is some other comment
	}
	for _, tt := range tests {
		t.Run(tt.workflow, func(t *testing.T) {
			g, err := buildUntestable(t, tt.workflow)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, m := range g.Marks {
				got = append(got, m.Junction.Label+" --"+m.Exit.Label+": "+m.Reason)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("marks %q, want %q", got, tt.want)
			}
		})
	}
}

// Every way a comment can be wrong is a PragmaError that says what to fix,
// with the file and line.
func TestUntestableErrors(t *testing.T) {
	tests := []struct{ workflow, want string }{
		{"NoReason", `it needs a reason, like reason="the carrier never sends this"`},
		{"NearMissSpace", "looks like //pathkit:untestable but is not written exactly that way"},
		{"NearMissTypo", "looks like //pathkit:untestable but is not written exactly that way"},
		{"NearMissPlural", "looks like //pathkit:untestable but is not written exactly that way"},
		{"StackedMarks", "there is no decision PathKit maps on this line or the next"},
		{"BlankReason", "it needs a reason"},
		{"NoExit", `it needs the exit it marks, like exit="false"`},
		{"UnknownExit", `has no exit "maybe"; its exits are: true, false`},
		{"UnknownSetting", `unknown setting "colour"`},
		{"Unquoted", `could not read "exit=true"`},
		{"NothingToMark", "there is no decision PathKit maps on this line or the next"},
		{"OnTransparent", "there is no decision PathKit maps on this line or the next"},
	}
	for _, tt := range tests {
		t.Run(tt.workflow, func(t *testing.T) {
			g, err := buildUntestable(t, tt.workflow)
			var pe *model.PragmaError
			if g != nil || !errors.As(err, &pe) {
				t.Fatalf("graph=%v err=%v, want a PragmaError", g != nil, err)
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.HasPrefix(err.Error(), "//pathkit:untestable at untestablebad.go:") {
				t.Errorf("err = %q\nwant it to contain %q", err, tt.want)
			}
		})
	}
}
