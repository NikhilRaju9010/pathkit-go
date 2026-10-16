// Package soundness checks that PathKit never calls a path impossible when
// a real run can take it.
//
// It writes small random Go functions shaped like workflow code (a few
// decisions over strings, numbers, booleans, pointers and interface values,
// with constants, copies, early returns, comma-ok forms, &x handed to a
// helper, closures and loops), runs each one for every combination of a
// small set of input values (so the run is exhaustive), and records the
// paths the runs really took. A path PathKit classified as
// cannot-be-tested that a run took is a false claim, and fails the test.
//
// The generator is plain Go and deterministic: the same seed and program
// number always give the same program, so a failure can be reproduced.
package soundness

import (
	"fmt"
	"math/rand"
	"strings"
)

// kinds of values a generated function works with.
const (
	kString = "string"
	kInt    = "int"
	kBool   = "bool"
	kPtr    = "*string"
	kIface  = "interface{}"
)

type variable struct {
	name string
	kind string
}

// Program is one generated workflow-shaped function and the inputs to run it with.
type Program struct {
	Seed   int64
	Number int
	Name   string // "Prog0007"
	Source string // the function and nothing else
	// Inputs: one Go argument list (as source text) per input combination.
	Inputs []string
}

type gen struct {
	r        *rand.Rand
	b        strings.Builder
	vars     []variable // variables usable now (top-level declarations and parameters)
	locals   []variable // top-level declared locals, to be "used" at the end
	budget   int        // decisions still to write
	next     int
	hasLoop  bool
	closures []string // names of closures that can be called
	strParam []string
}

// Generate writes the program numbered number of the given seed.
func Generate(seed int64, number int) Program {
	g := &gen{r: rand.New(rand.NewSource(seed*1_000_003 + int64(number)))}
	p := Program{Seed: seed, Number: number, Name: fmt.Sprintf("Prog%04d", number)}
	if seed != 0 {
		p.Name = fmt.Sprintf("Prog%d_%04d", seed, number)
	}

	// Parameters: two or three kinds, each with a small set of values.
	domains := map[string][]string{
		kString: {`""`, `"a"`},
		kInt:    {`0`, `1`},
		kBool:   {`false`, `true`},
		kPtr:    {`nil`, `sp("x")`},
		kIface:  {`nil`, `"a"`, `float64(1)`, `true`}, // what the JSON data converter hands an interface{} (a number is a float64)
	}
	order := []string{kString, kInt, kBool, kPtr, kIface}
	g.r.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	nparams := 2 + g.r.Intn(2)
	var params []variable
	counts := map[string]int{}
	for i := 0; i < nparams; i++ {
		k := order[i]
		counts[k]++
		short := map[string]string{kString: "s", kInt: "n", kBool: "b", kPtr: "p", kIface: "i"}[k]
		params = append(params, variable{fmt.Sprintf("%s%d", short, counts[k]), k})
		if k == kString {
			g.strParam = append(g.strParam, params[len(params)-1].name)
		}
	}
	g.vars = append(g.vars, params...)

	// Inputs: every combination of the parameters' values.
	combos := [][]string{{}}
	for _, pr := range params {
		var next [][]string
		for _, c := range combos {
			for _, v := range domains[pr.kind] {
				next = append(next, append(append([]string{}, c...), v))
			}
		}
		combos = next
	}
	for _, c := range combos {
		p.Inputs = append(p.Inputs, strings.Join(c, ", "))
	}

	var sig []string
	for _, pr := range params {
		sig = append(sig, pr.name+" "+pr.kind)
	}
	g.budget = 3 + g.r.Intn(6)
	g.block(0, 1)
	for g.budget > 0 { // the budget is a promise to write that many decisions
		g.stmt(0, 1)
	}
	body := g.b.String()
	var used strings.Builder
	for _, l := range g.locals {
		fmt.Fprintf(&used, "\t_ = %s\n", l.name)
	}
	for _, c := range g.closures {
		fmt.Fprintf(&used, "\t_ = %s\n", c)
	}
	p.Source = fmt.Sprintf("func %s(ctx workflow.Context, %s) error {\n\t_ = ctx\n%s%s\treturn nil\n}\n", p.Name, strings.Join(sig, ", "), body, used.String())
	return p
}

func (g *gen) line(depth int, format string, args ...any) {
	fmt.Fprintf(&g.b, "%s%s\n", strings.Repeat("\t", depth+1), fmt.Sprintf(format, args...))
}

func (g *gen) fresh(prefix string) string {
	g.next++
	return fmt.Sprintf("%s%d", prefix, g.next)
}

func (g *gen) pick(kind string) (variable, bool) {
	var c []variable
	for _, v := range g.vars {
		if v.kind == kind {
			c = append(c, v)
		}
	}
	if len(c) == 0 {
		return variable{}, false
	}
	return c[g.r.Intn(len(c))], true
}

// anyVar picks a variable that may be read or written (the counter of a
// three-part loop has no name and is never picked: writing it could make
// the loop run forever).
func (g *gen) anyVar() variable {
	for {
		if v := g.vars[g.r.Intn(len(g.vars))]; v.name != "" {
			return v
		}
	}
}

func (g *gen) strConst() string { return []string{`""`, `"a"`, `"b"`}[g.r.Intn(3)] }
func (g *gen) intConst() string { return []string{"0", "1", "2"}[g.r.Intn(3)] }

// simpleCond is one comparison over one variable.
func (g *gen) simpleCond() string {
	v := g.anyVar()
	switch v.kind {
	case kString:
		switch g.r.Intn(4) {
		case 0:
			return fmt.Sprintf("%s == %s", v.name, g.strConst())
		case 1:
			return fmt.Sprintf("%s != %s", v.name, g.strConst())
		case 2:
			return fmt.Sprintf("len(%s) == 0", v.name)
		default:
			if w, ok := g.pick(kString); ok && w.name != v.name {
				return fmt.Sprintf("%s == %s", v.name, w.name)
			}
			return fmt.Sprintf("%s == %s", v.name, g.strConst())
		}
	case kInt:
		return fmt.Sprintf("%s %s %s", v.name, []string{"==", ">", "<", "!="}[g.r.Intn(4)], g.intConst())
	case kBool:
		if g.r.Intn(2) == 0 {
			return v.name
		}
		return "!" + v.name
	default: // pointer, interface
		return fmt.Sprintf("%s %s nil", v.name, []string{"==", "!="}[g.r.Intn(2)])
	}
}

func (g *gen) cond() string {
	c := g.simpleCond()
	if g.r.Intn(4) == 0 {
		op := []string{"&&", "||"}[g.r.Intn(2)]
		return fmt.Sprintf("%s %s %s", c, op, g.simpleCond())
	}
	return c
}

// block writes two to four statements.
func (g *gen) block(depth, minStmts int) {
	n := minStmts + g.r.Intn(3)
	for i := 0; i < n; i++ {
		g.stmt(depth, i)
	}
}

// action is a statement that is not a decision.
func (g *gen) action(depth int) {
	switch g.r.Intn(8) {
	case 0, 1:
		v := g.anyVar()
		switch v.kind {
		case kString:
			g.line(depth, "%s = %s", v.name, g.strConst())
		case kInt:
			g.line(depth, "%s = %s", v.name, g.intConst())
		case kBool:
			g.line(depth, "%s = %v", v.name, g.r.Intn(2) == 0)
		case kPtr:
			g.line(depth, "%s = nil", v.name)
		default:
			g.line(depth, "%s = %s", v.name, []string{"nil", `"a"`, "1"}[g.r.Intn(3)])
		}
	case 2: // a copy
		v := g.anyVar()
		if w, ok := g.pick(v.kind); ok && w.name != v.name {
			g.line(depth, "%s = %s", v.name, w.name)
		} else {
			g.line(depth, "noop()")
		}
	case 3: // an unknown call changes it
		if v, ok := g.pick(kString); ok {
			g.line(depth, "%s = normalise(%s)", v.name, v.name)
		} else {
			g.line(depth, "noop()")
		}
	case 4: // its address goes to a helper
		if v, ok := g.pick(kString); ok {
			arg := g.strConst()
			if len(g.strParam) > 0 {
				arg = g.strParam[g.r.Intn(len(g.strParam))]
			}
			g.line(depth, "touch(&%s, %s)", v.name, arg)
		} else {
			g.line(depth, "noop()")
		}
	case 5: // a closure that exists already is called
		if len(g.closures) > 0 {
			g.line(depth, "%s()", g.closures[g.r.Intn(len(g.closures))])
		} else {
			g.line(depth, "noop()")
		}
	default:
		g.line(depth, "noop()")
	}
}

// topDecl declares something new at the top level of the function.
func (g *gen) topDecl() {
	switch g.r.Intn(6) {
	case 0:
		n := g.fresh("v")
		g.line(0, "var %s string", n)
		g.addLocal(n, kString)
	case 1:
		n := g.fresh("v")
		g.line(0, "%s := %s", n, g.strConst())
		g.addLocal(n, kString)
	case 2: // a copy of a variable
		v := g.anyVar()
		n := g.fresh("c")
		g.line(0, "%s := %s", n, v.name)
		g.addLocal(n, v.kind)
	case 3: // comma-ok type assertion on an interface value
		if v, ok := g.pick(kIface); ok {
			x, okName := g.fresh("x"), g.fresh("ok")
			g.line(0, "%s, %s := %s.(string)", x, okName, v.name)
			g.addLocal(x, kString)
			g.addLocal(okName, kBool)
		} else {
			g.line(0, "noop()")
		}
	case 4: // a closure that writes a string
		if v, ok := g.pick(kString); ok {
			n := g.fresh("f")
			g.line(0, "%s := func() { %s = %s }", n, v.name, g.strConst())
			g.closures = append(g.closures, n)
		} else {
			g.line(0, "noop()")
		}
	default:
		n := g.fresh("k")
		g.line(0, "%s := %s", n, g.intConst())
		g.addLocal(n, kInt)
	}
}

func (g *gen) addLocal(name, kind string) {
	v := variable{name, kind}
	g.vars = append(g.vars, v)
	g.locals = append(g.locals, v)
}

func (g *gen) stmt(depth, i int) {
	if depth == 0 && g.r.Intn(3) == 0 {
		g.topDecl()
		return
	}
	if g.budget <= 0 || depth >= 3 {
		g.action(depth)
		return
	}
	switch g.r.Intn(10) {
	case 0, 1, 2, 3, 4: // if / else
		g.budget--
		g.line(depth, "if %s {", g.cond())
		g.block(depth+1, 1)
		if g.r.Intn(4) == 0 {
			g.line(depth+1, "return nil")
		}
		if g.r.Intn(3) == 0 {
			g.line(depth, "} else {")
			g.block(depth+1, 1)
		}
		g.line(depth, "}")
	case 5: // a type switch or a value switch
		g.budget--
		if v, ok := g.pick(kIface); ok {
			g.line(depth, "switch %s.(type) {", v.name)
			g.line(depth, "case string:")
			g.block(depth+1, 1)
			g.line(depth, "case int:")
			g.block(depth+1, 1)
			if g.r.Intn(2) == 0 {
				g.line(depth, "case float64:")
				g.block(depth+1, 1)
			}
			if g.r.Intn(2) == 0 {
				g.line(depth, "default:")
				g.block(depth+1, 1)
			}
			g.line(depth, "}")
		} else if v, ok := g.pick(kString); ok {
			g.line(depth, "switch %s {", v.name)
			g.line(depth, "case \"a\":")
			g.block(depth+1, 1)
			g.line(depth, "case \"b\":")
			g.block(depth+1, 1)
			g.line(depth, "default:")
			g.block(depth+1, 1)
			g.line(depth, "}")
		} else {
			g.line(depth, "if %s {", g.cond())
			g.block(depth+1, 1)
			g.line(depth, "}")
		}
	case 6: // a loop with a decision inside
		if g.hasLoop || depth > 1 {
			g.action(depth)
			return
		}
		g.hasLoop = true
		g.budget--
		e := g.fresh("e")
		if g.r.Intn(2) == 0 {
			g.line(depth, "for _, %s := range []string{\"a\", \"b\"} {", e)
			g.vars = append(g.vars, variable{e, kString})
		} else {
			g.line(depth, "for %s := 0; %s < 2; %s++ {", e, e, e)
			g.vars = append(g.vars, variable{"", ""}) // keeps the pop below balanced; never picked (see anyVar)
		}
		g.line(depth+1, "_ = %s", e)
		g.line(depth+1, "if %s {", g.cond())
		g.budget--
		g.block(depth+2, 1)
		g.line(depth+1, "}")
		g.block(depth+1, 1)
		g.vars = g.vars[:len(g.vars)-1]
		g.line(depth, "}")
	default:
		g.action(depth)
	}
}
