package soundness

import (
	"fmt"
	"strings"
)

// Interface values, slices and maps (milestone M3): collections built in the
// function and filled with values of known types, collections that come in
// as inputs (decoded by Temporal's JSON converter), type switches and
// assertions on their elements, typed nil pointers, delete, a map handed to
// a helper, and range loops over built slices.

const (
	kImap   = "map[string]interface{}"
	kIslice = "[]interface{}"
)

// ifaceValue is an expression that can be stored in an interface{}.
func (g *gen) ifaceValue() string {
	var opts []string
	opts = append(opts, g.strConst(), g.intConst(), "2.5", "true", "nil")
	for _, v := range g.vars {
		switch v.kind {
		case kString, kInt, kBool, kPtr, kIface:
			if v.name != "" {
				opts = append(opts, v.name)
			}
		}
	}
	return opts[g.r.Intn(len(opts))]
}

// caseTypes writes the cases of a type switch (some of them, in a random order).
func (g *gen) caseList() []string {
	all := []string{"string", "int", "float64", "bool", "*string", "nil", "[]interface{}", "map[string]interface{}"}
	g.r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	return all[:1+g.r.Intn(4)]
}

// element picks an element of a collection: its expression, and a guard
// that must hold for the index to be in range ("" when none is needed).
func (g *gen) element() (expr, guard string, ok bool) {
	var maps, slices []variable
	for _, v := range g.vars {
		switch v.kind {
		case kImap:
			maps = append(maps, v)
		case kIslice:
			slices = append(slices, v)
		}
	}
	if len(maps)+len(slices) == 0 {
		return "", "", false
	}
	if len(maps) > 0 && (len(slices) == 0 || g.r.Intn(2) == 0) {
		m := maps[g.r.Intn(len(maps))]
		key := []string{`"a"`, `"b"`}[g.r.Intn(2)]
		if len(g.strParam) > 0 && g.r.Intn(3) == 0 {
			key = g.strParam[g.r.Intn(len(g.strParam))]
		}
		return fmt.Sprintf("%s[%s]", m.name, key), "", true
	}
	s := slices[g.r.Intn(len(slices))]
	if g.minLen[s.name] > 0 {
		return s.name + "[0]", "", true
	}
	return s.name + "[0]", fmt.Sprintf("len(%s) > 0", s.name), true
}

func (g *gen) wrapGuard(depth int, guard string, body func(depth int)) {
	if guard == "" {
		body(depth)
		return
	}
	g.budget--
	g.line(depth, "if %s {", guard)
	body(depth + 1)
	g.line(depth, "}")
}

// typeStmt writes one statement about the collections.
func (g *gen) typeStmt(depth int) {
	switch g.r.Intn(13) {
	case 9: // a slice literal with indexes: the slots before them are nil
		if g.budget < 1 {
			g.action(depth)
			return
		}
		n := g.fresh("sx")
		g.line(depth, "%s := []interface{}{2: %s}", n, g.ifaceValue())
		g.budget--
		g.line(depth, "if %s[%d] == nil {", n, g.r.Intn(3))
		g.line(depth+1, "noop()")
		g.line(depth, "}")
	case 10: // a tuple assignment into an element
		var maps []variable
		for _, v := range g.vars {
			if v.kind == kImap {
				maps = append(maps, v)
			}
		}
		if len(maps) == 0 || len(g.strParam) == 0 {
			g.action(depth)
			return
		}
		m := maps[g.r.Intn(len(maps))]
		e := g.fresh("er")
		g.line(depth, "if %s == nil { %s = map[string]interface{}{} }", m.name, m.name)
		g.line(depth, "var %s error", e)
		g.line(depth, "%s[\"a\"], %s = pair(%s)", m.name, e, g.strParam[g.r.Intn(len(g.strParam))])
		g.line(depth, "_ = %s", e)
	case 11: // a map with interface keys: 1, 1.0 and int64(1) are three keys
		if g.budget < 1 {
			g.action(depth)
			return
		}
		n := g.fresh("mk")
		g.line(depth, "%s := map[interface{}]interface{}{1: %s}", n, g.ifaceValue())
		g.budget--
		g.line(depth, "if %s[%s] == nil {", n, []string{"1", "1.0", "int64(1)", `"a"`}[g.r.Intn(4)])
		g.line(depth+1, "noop()")
		g.line(depth, "}")
	case 12: // a container taken out of an interface, maybe changed by a call that got the interface
		var ifs []variable
		for _, v := range g.vars {
			if v.kind == kIface {
				ifs = append(ifs, v)
			}
		}
		if len(ifs) == 0 || g.budget < 1 {
			g.action(depth)
			return
		}
		src := ifs[g.r.Intn(len(ifs))]
		c, ok := g.fresh("cm"), g.fresh("okc")
		g.line(depth, "%s, %s := %s.(map[string]interface{})", c, ok, src.name)
		g.line(depth, "_ = %s", ok)
		if g.r.Intn(2) == 0 {
			g.line(depth, "mutateAny(%s)", src.name)
		}
		g.budget--
		g.line(depth, "switch %s[\"a\"].(type) {", c)
		for _, cs := range g.caseList() {
			g.line(depth, "case %s:", cs)
			g.line(depth+1, "noop()")
		}
		g.line(depth, "}")
	case 0: // a type switch on an element
		e, guard, ok := g.element()
		if !ok || g.budget < 1 {
			g.action(depth)
			return
		}
		g.budget--
		g.wrapGuard(depth, guard, func(d int) {
			g.line(d, "switch %s.(type) {", e)
			for _, c := range g.caseList() {
				g.line(d, "case %s:", c)
				g.line(d+1, "noop()")
			}
			if g.r.Intn(2) == 0 {
				g.line(d, "default:")
				g.line(d+1, "noop()")
			}
			g.line(d, "}")
		})
	case 1: // a comma-ok assertion on an element
		e, guard, ok := g.element()
		if !ok || g.budget < 1 {
			g.action(depth)
			return
		}
		g.budget--
		typ := []string{"string", "int", "float64", "bool", "*string", "[]interface{}", "map[string]interface{}"}[g.r.Intn(7)]
		n := g.fresh("ok")
		g.wrapGuard(depth, guard, func(d int) {
			g.line(d, "_, %s := %s.(%s)", n, e, typ)
			g.line(d, "if %s {", []string{n, "!" + n}[g.r.Intn(2)])
			g.line(d+1, "noop()")
			g.line(d, "}")
		})
	case 2: // a range loop over a slice, with a type switch inside
		var slices []variable
		for _, v := range g.vars {
			if v.kind == kIslice {
				slices = append(slices, v)
			}
		}
		if len(slices) == 0 || g.hasLoop || depth > 1 || g.budget < 2 {
			g.action(depth)
			return
		}
		g.hasLoop = true
		g.budget -= 2
		s := slices[g.r.Intn(len(slices))]
		e := g.fresh("el")
		g.line(depth, "for _, %s := range %s {", e, s.name)
		g.line(depth+1, "switch %s.(type) {", e)
		for _, c := range g.caseList() {
			g.line(depth+1, "case %s:", c)
			g.line(depth+2, "noop()")
		}
		g.line(depth+1, "}")
		g.line(depth, "}")
	case 3: // a nil check of an element
		e, guard, ok := g.element()
		if !ok || g.budget < 1 {
			g.action(depth)
			return
		}
		g.budget--
		g.wrapGuard(depth, guard, func(d int) {
			g.line(d, "if %s %s nil {", e, []string{"==", "!="}[g.r.Intn(2)])
			g.line(d+1, "noop()")
			g.line(d, "}")
		})
	case 4: // lengths
		var colls []variable
		for _, v := range g.vars {
			if v.kind == kImap || v.kind == kIslice {
				colls = append(colls, v)
			}
		}
		if len(colls) == 0 || g.budget < 1 {
			g.action(depth)
			return
		}
		g.budget--
		c := colls[g.r.Intn(len(colls))]
		g.line(depth, "if len(%s) %s %s {", c.name, []string{"==", ">", "!="}[g.r.Intn(3)], []string{"0", "1"}[g.r.Intn(2)])
		g.line(depth+1, "noop()")
		g.line(depth, "}")
	case 5: // a typed nil pointer in an interface
		if g.budget < 1 {
			g.action(depth)
			return
		}
		g.budget--
		q := g.fresh("q")
		g.line(depth, "var %s interface{}", q)
		if p, ok := g.pick(kPtr); ok && g.r.Intn(2) == 0 {
			g.line(depth, "if %s {", g.cond())
			g.line(depth+1, "%s = %s", q, p.name)
			g.line(depth, "}")
			g.budget--
		} else {
			g.line(depth, "var np%s *string", q)
			g.line(depth, "%s = np%s", q, q)
		}
		g.line(depth, "if %s == nil {", q)
		g.line(depth+1, "noop()")
		g.line(depth, "}")
		g.line(depth, "_ = %s", q)
	case 6: // store into a map or slice, delete, or hand the map to a helper
		var maps, slices []variable
		for _, v := range g.vars {
			switch v.kind {
			case kImap:
				maps = append(maps, v)
			case kIslice:
				slices = append(slices, v)
			}
		}
		switch g.r.Intn(4) {
		case 0:
			if len(maps) > 0 {
				m := maps[g.r.Intn(len(maps))]
				g.line(depth, "if %s == nil { %s = map[string]interface{}{} }", m.name, m.name)
				g.line(depth, "%s[%s] = %s", m.name, []string{`"a"`, `"b"`}[g.r.Intn(2)], g.ifaceValue())
				return
			}
		case 1:
			if len(maps) > 0 {
				m := maps[g.r.Intn(len(maps))]
				g.line(depth, "delete(%s, %s)", m.name, []string{`"a"`, `"b"`}[g.r.Intn(2)])
				return
			}
		case 2:
			if len(maps) > 0 {
				g.line(depth, "mutate(%s)", maps[g.r.Intn(len(maps))].name)
				return
			}
		default:
			if len(slices) > 0 {
				s := slices[g.r.Intn(len(slices))]
				if g.r.Intn(2) == 0 {
					g.line(depth, "mutateS(%s)", s.name)
				} else {
					g.line(depth, "%s = append(%s, %s)", s.name, s.name, g.ifaceValue())
					if depth == 0 { // inside an if or a switch case it may not run
						g.minLen[s.name]++
					}
				}
				return
			}
		}
		g.line(depth, "noop()")
	default:
		g.action(depth)
	}
}

// declCollection declares a map or slice at the top level of the function.
func (g *gen) declCollection() {
	if g.r.Intn(2) == 0 {
		n := g.fresh("mp")
		switch g.r.Intn(3) {
		case 0:
			g.line(0, "%s := map[string]interface{}{}", n)
		case 1:
			g.line(0, "%s := map[string]interface{}{\"a\": %s}", n, g.ifaceValue())
		default:
			g.line(0, "%s := map[string]interface{}{\"a\": %s, \"b\": %s}", n, g.ifaceValue(), g.ifaceValue())
		}
		g.addLocal(n, kImap)
		return
	}
	n := g.fresh("sl")
	count := g.r.Intn(3)
	var elems []string
	for i := 0; i < count; i++ {
		elems = append(elems, g.ifaceValue())
	}
	if count == 0 && g.r.Intn(2) == 0 {
		g.line(0, "var %s []interface{}", n)
	} else {
		g.line(0, "%s := []interface{}{%s}", n, strings.Join(elems, ", "))
	}
	g.minLen[n] = count
	g.addLocal(n, kIslice)
}
