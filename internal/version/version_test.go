package version

import "testing"

func TestStampedVersionWins(t *testing.T) {
	old := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = old })

	if got := String(); got != "v1.2.3" {
		t.Errorf("String() = %q, want v1.2.3", got)
	}
}

func TestUnstampedVersionIsNeverEmpty(t *testing.T) {
	old := Version
	Version = ""
	t.Cleanup(func() { Version = old })

	if got := String(); got == "" || got == "(devel)" {
		t.Errorf("String() = %q, want a real version or \"dev\"", got)
	}
}
