package soundness

import (
	"fmt"
	"math/rand"
	"strings"
)

// Values handed to other code (milestone M11, found by seed 5 / program 1371):
// every kind of value the workflow can hand on is built here, checked, handed to
// a plain function or a goroutine that changes it, and checked again. What the
// workflow learned before the hand-off must not be used after it, so a claim
// made on the second check would be false. The program really runs.
//
// Only some programs get these blocks (every seventh), and they are drawn from
// a random source of their own, so the programs the other probes write do not
// change when this one is added: the known programs (TestKnownPrograms) stay
// the programs they were.

// handoffBlocks reports whether the program with this number gets blocks.
func handoffBlocks(number int) bool { return number%7 == 0 }

// handoffProbe returns the helper declarations and the statements to put in the
// function body.
func handoffProbe(seed int64, number int) (helpers []string, body string) {
	r := rand.New(rand.NewSource(seed*7_000_003 + int64(number)))
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, "\t"+format+"\n", args...) }
	n := 1 + r.Intn(3)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%d_%d_%d", seed, number, i) // the seed too: one batch holds programs of several seeds with the same number
		switch kind := r.Intn(6); kind {
		case 0: // the element of a slice, changed by a plain function
			line("hs%s := []interface{}{\"x\", 1.5}", id)
			typeCheck(line, "hs"+id+"[0]")
			line("mutateS(hs%s)", id)
			typeCheck(line, "hs"+id+"[0]")
			line("_ = len(hs%s)", id)
		case 1: // the length of a slice, changed through a pointer to it
			helpers = append(helpers, fmt.Sprintf("func growStrs%s(p *[]string) { *p = append(*p, \"more\") }\n", id))
			line("hl%s := []string{}", id)
			lenCheck(line, "hl"+id, r)
			line("growStrs%s(&hl%s)", id, id)
			lenCheck(line, "hl"+id, r)
		case 2: // a field of a struct reached through a pointer
			line("hr%s := &Rec{A: \"\"}", id)
			line("if hr%s.A == \"\" {", id)
			line("\tnoop()")
			line("}")
			line("hr%s.setA(%s)", id, []string{`"z"`, `""`}[r.Intn(2)])
			line("if hr%s.A == \"\" {", id)
			line("\tnoop()")
			line("}")
		case 3: // a map inside a struct that is handed on by value
			helpers = append(helpers, fmt.Sprintf("type holder%[1]s struct {\n\tm map[string]interface{}\n\tn string\n}\n\nfunc fillHolder%[1]s(h holder%[1]s) { h.m[\"a\"] = 1 }\n", id))
			line("hh%s := holder%s{m: map[string]interface{}{}}", id, id)
			lenCheck(line, "hh"+id+".m", r)
			line("fillHolder%s(hh%s)", id, id)
			lenCheck(line, "hh"+id+".m", r)
		case 4: // a field and a map of a struct handed on by pointer
			helpers = append(helpers, fmt.Sprintf("type pholder%[1]s struct {\n\tm map[string]interface{}\n\tn string\n}\n\nfunc fillPHolder%[1]s(h *pholder%[1]s) {\n\th.m[\"a\"] = 1\n\th.n = \"set\"\n}\n", id))
			line("hp%s := &pholder%s{m: map[string]interface{}{\"k\": \"x\"}}", id, id)
			typeCheck(line, "hp"+id+".m[\"a\"]")
			line("if hp%s.n == \"\" {", id)
			line("\tnoop()")
			line("}")
			line("fillPHolder%s(hp%s)", id, id)
			typeCheck(line, "hp"+id+".m[\"a\"]")
			line("if hp%s.n == \"\" {", id)
			line("\tnoop()")
			line("}")
		case 5: // a channel: a goroutine sends a value of another type than the variable holds
			line("hc%s := workflow.NewChannel(ctx)", id)
			line("var hv%s interface{} = \"x\"", id)
			line("workflow.Go(ctx, func(gctx workflow.Context) { hc%s.Send(gctx, 1) })", id)
			typeCheck(line, "hv"+id)
			line("hc%s.Receive(ctx, &hv%s)", id, id)
			typeCheck(line, "hv"+id)
		}
	}
	return helpers, b.String()
}

func typeCheck(line func(string, ...any), expr string) {
	line("switch %s.(type) {", expr)
	line("case int:")
	line("\tnoop()")
	line("case string:")
	line("\tnoop()")
	line("}")
}

func lenCheck(line func(string, ...any), expr string, r *rand.Rand) {
	op := []string{"==", "!="}[r.Intn(2)]
	line("if len(%s) %s 0 {", expr, op)
	line("\tnoop()")
	line("}")
}
