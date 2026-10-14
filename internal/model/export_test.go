package model

// SetFactsEnabled turns the extractor off or on for a test and returns a
// function that restores the old setting.
func SetFactsEnabled(on bool) func() {
	old := factsEnabled
	factsEnabled = on
	return func() { factsEnabled = old }
}
