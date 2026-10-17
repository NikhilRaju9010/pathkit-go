package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// StableIDVersion prefixes every stable ID. If the way stable IDs are
// built ever changes, the prefix changes too ("s2:"), so an old ID in a
// config file is recognised as an old kind and never mistaken for a new one.
const StableIDVersion = "s1"

// assignStable gives every junction its stable description: its label
// (the normalized source text, e.g. "if input.Amount <= 0"), with
// whitespace collapsed. When two junctions of one workflow have exactly
// the same text, the later ones get " #2", " #3" (the same idea as the
// duplicate case labels), in source order.
//
// Unlike a junction's ID (J1, J2, ...), this text does not change when an
// unrelated junction is added or removed elsewhere in the function.
func (g *Graph) assignStable() {
	seen := map[string]int{}
	for _, j := range g.Junctions {
		d := strings.Join(strings.Fields(j.Label), " ")
		if j.Site != nil {
			// A decision in a helper is told apart by the helper and the call
			// it was reached by, in words (no line numbers).
			d += " " + j.Site.Words()
		}
		seen[d]++
		if n := seen[d]; n > 1 {
			d += fmt.Sprintf(" #%d", n)
		}
		j.stable = d
	}
}

// StableKey is the path's identity written in words, not numbers: each
// step's junction text and the exit taken, then the end kind. It has no
// J-numbers and no line numbers. StableID is its hash.
func (p Path) StableKey() string {
	parts := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		parts[i] = s.Junction.stable + "\x1f" + strings.Join(strings.Fields(s.Exit.Label), " ")
	}
	return strings.Join(parts, "\n") + "|" + string(p.End)
}

// StableID is a short hash of StableKey with a version prefix, like
// "s1:3fa94c0d17ab". It is meant to stay the same when unrelated code
// around the path changes, so a saved override keeps pointing at the same
// path. It is scoped per workflow: two workflows can have the same
// StableID, so always keep the workflow name next to it. ID, the older
// J-number hash, is unchanged and is still what traces and reports use
// to match.
func (p Path) StableID() string {
	sum := sha256.Sum256([]byte(StableIDVersion + "\n" + p.StableKey()))
	return StableIDVersion + ":" + hex.EncodeToString(sum[:])[:12]
}

// Decisions lists the path's steps the way `pathkit analyze` prints them:
// the decision's text, then --, then the exit taken ("if x > 0 --true").
// A config rule's "when" entries are written in this form. Whitespace is
// collapsed, like in StableKey.
func (p Path) Decisions() []string {
	out := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		out[i] = s.Junction.stable + " --" + strings.Join(strings.Fields(s.Exit.Label), " ")
	}
	return out
}

// StableText is the junction's decision text as `pathkit analyze` prints it
// and as a rule's "when" writes it, with " #2", " #3" added to repeated
// texts of one workflow (see assignStable).
func (j *Junction) StableText() string { return j.stable }

// LegacyStableIDs are the stable IDs the path had before PathKit followed
// helper functions: its steps without the decisions inside helpers, without
// the error checks and loops that exist only because a helper is followed,
// and with whatever end kind (an older PathKit could not tell how a path
// that returns a helper's result ended). A path with none of those has its
// own ID among them. A saved override that points at an ID that has vanished
// is looked up in these.
func (p Path) LegacyStableIDs() []string {
	var parts []string
	for _, s := range p.Steps {
		if s.Junction.Site != nil || s.Junction.viaHelper {
			continue
		}
		parts = append(parts, s.Junction.stable+"\x1f"+strings.Join(strings.Fields(s.Exit.Label), " "))
	}
	var out []string
	for _, end := range []EndKind{EndCompleted, EndFailed, EndContinuedAsNew, EndUnknown} {
		key := strings.Join(parts, "\n") + "|" + string(end)
		sum := sha256.Sum256([]byte(StableIDVersion + "\n" + key))
		out = append(out, StableIDVersion+":"+hex.EncodeToString(sum[:])[:12])
	}
	return out
}
