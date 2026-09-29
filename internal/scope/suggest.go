package scope

import "strings"

// Suggest returns the known name closest to name, for a "did you mean"
// hint, or "" when nothing is close. Close means at most 3 single-letter
// edits and at most a third of the name's length, so a typo gets a hint
// and an unrelated name doesn't. Each known name is compared in full
// ("orders.OrderWorkflow") and by its last part ("OrderWorkflow"); the
// hint is written in the same form as the closest match.
func Suggest(name string, known []string) string {
	limit := min(3, max(1, len(name)/3))
	best, bestDist := "", limit+1
	for _, full := range known {
		for _, form := range forms(full) {
			if form == name {
				continue // not a typo of itself
			}
			// Case is ignored when measuring, so "shipmentworkflow" is
			// 0 edits from "ShipmentWorkflow": the best possible hint.
			d := editDistance(strings.ToLower(name), strings.ToLower(form))
			if d < bestDist {
				best, bestDist = form, d
			}
		}
	}
	return best
}

// forms lists the ways a full name can be written: "a.T.M", "T.M", "M".
func forms(full string) []string {
	out := []string{full}
	for i := 0; i < len(full); i++ {
		if full[i] == '.' {
			out = append(out, full[i+1:])
		}
	}
	return out
}

// editDistance is the number of single-letter insertions, deletions or
// changes that turn a into b (Levenshtein distance).
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// withHint adds "; did you mean ...?" to msg when a close name exists.
func withHint(msg, name string, known []string) string {
	if s := Suggest(name, known); s != "" {
		return msg + "; did you mean " + quote(s) + "?"
	}
	return msg
}

func quote(s string) string { return `"` + s + `"` }
