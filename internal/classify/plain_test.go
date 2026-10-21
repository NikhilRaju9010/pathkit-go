package classify

import "testing"

// Every built-in label and bucket has a plain name, and it is not just the
// technical name written again.
func TestEveryLabelHasAPlainName(t *testing.T) {
	for _, d := range Definitions() {
		switch d.Kind {
		case KindLabel:
			p := PlainLabel(d.Name)
			if p == "" || p == d.Name {
				t.Errorf("label %q has no plain name", d.Name)
			}
		case KindBucket:
			p := PlainBucket(Bucket(d.Name))
			if p == "" || p == d.Name {
				t.Errorf("bucket %q has no plain name", d.Name)
			}
		}
	}
	if PlainLabel("my-own-label") != "my-own-label" {
		t.Error("a custom label keeps its own name")
	}
}
