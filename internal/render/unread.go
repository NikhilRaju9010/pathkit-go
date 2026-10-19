package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// UnreadTop is how many entries of the "could not fully read" panel the text
// reports list; the JSON and the HTML page list them all.
const UnreadTop = 10

// UnreadNone is the panel's line when there is nothing to list.
const UnreadNone = "PathKit read everything in scope"

// UnreadLines is the "PathKit could not fully read" panel as text lines: the
// count and the first top entries (all of them when top is 0), or UnreadNone.
func UnreadLines(res coverage.Result, top int) []string {
	n := len(res.Unread)
	if n == 0 {
		return []string{UnreadNone}
	}
	shown := res.Unread
	if top > 0 && n > top {
		shown = shown[:top]
	}
	head := fmt.Sprintf("PathKit could not fully read %s", count(n, "place"))
	if len(shown) < n {
		head += fmt.Sprintf(" (the first %d; %s list every one)", len(shown), "--json and --html")
	}
	lines := []string{head + ":"}
	for _, u := range shown {
		lines = append(lines, "  "+UnreadLine(u))
	}
	return lines
}

// UnreadLine is one entry on one line: where, what kind, the code and what
// PathKit did.
func UnreadLine(u coverage.UnreadEntry) string {
	where := filepath.ToSlash(u.File)
	if u.Line > 0 {
		where = fmt.Sprintf("%s:%d", filepath.ToSlash(u.File), u.Line)
	}
	s := where + ": " + u.Kind
	if u.Call != "" {
		s += " " + u.Call
	}
	return s + " (" + u.Workflow + ") — " + u.Effect
}

// UnreadBlock is UnreadLines as text ending in a newline.
func UnreadBlock(res coverage.Result, top int) string {
	return strings.Join(UnreadLines(res, top), "\n") + "\n"
}
