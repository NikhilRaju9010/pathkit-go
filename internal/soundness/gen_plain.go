package soundness

import "fmt"

// Plain functions the engine reads (milestone M11, E2 and E3): functions that
// take no workflow context and build an interface value or a map, called from
// the program, and decisions on what they gave back. The program really runs,
// so a summary that is wrong makes a path it calls impossible happen. Some
// shapes are left unknown on purpose (the map is handed to other code, written
// under a computed key, deleted from); they must stay possible. The pointers
// stored in the values are *string: a test for string on them is a likely
// defect, which is not a claim.

func (g *gen) plainProbe() {
	if g.budget < 2 {
		g.line(0, "noop()")
		return
	}
	g.budget -= 2
	name := g.helperName()
	flag := "true"
	if v, ok := g.pick(kBool); ok && g.r.Intn(3) > 0 {
		flag = v.name
	}
	str := "s"
	if v, ok := g.pick(kString); ok {
		str = v.name
	} else {
		str = `"q"`
	}
	switch g.r.Intn(8) {
	case 6, 7: // a map handed to a helper that writes into it: facts hold until then (M11)
		g.lateEscapeProbe(name, g.r.Intn(2) == 0)
	case 0: // an interface result with two possible types, maybe nil
		last := []string{`nil`, `2.5`, `true`, `"b"`}[g.r.Intn(4)]
		g.helpers = append(g.helpers, fmt.Sprintf("func %s(flag bool, s string) interface{} {\n\tif flag {\n\t\treturn s\n\t}\n\treturn %s\n}\n", name, last))
		v := g.fresh("pv")
		g.line(0, "%s := %s(%s, %s)", v, name, flag, str)
		g.typeProbeOn(v)
	case 1: // a map built here with constant keys, one key written in a branch
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(flag bool, s string) map[string]interface{} {
	m := map[string]interface{}{"a": s}
	m["b"] = 1
	if flag {
		m["a"] = 2.5
	}
	p := &s
	m["p"] = p
	return m
}
`, name))
		m := g.fresh("pm")
		g.line(0, "%s := %s(%s, %s)", m, name, flag, str)
		g.collSinks = append(g.collSinks, m)
		key := []string{`"a"`, `"b"`, `"p"`, `"zz"`}[g.r.Intn(4)]
		g.typeProbeOn(m + "[" + key + "]")
	case 2: // the map is handed to other code first: unknown
		g.helpers = append(g.helpers, fmt.Sprintf("func %[1]sMut(m map[string]interface{}) { m[\"a\"] = 5 }\nfunc %[1]s(flag bool) map[string]interface{} {\n\tm := map[string]interface{}{\"a\": \"x\"}\n\tif flag {\n\t\t%[1]sMut(m)\n\t}\n\treturn m\n}\n", name))
		m := g.fresh("pm")
		g.line(0, "%s := %s(%s)", m, name, flag)
		g.collSinks = append(g.collSinks, m)
		g.typeProbeOn(m + `["a"]`)
	case 3: // written under a computed key: unknown
		g.helpers = append(g.helpers, fmt.Sprintf("func %s(k string) map[string]interface{} {\n\tm := map[string]interface{}{\"a\": \"x\"}\n\tm[k] = 7\n\treturn m\n}\n", name))
		m := g.fresh("pm")
		g.line(0, "%s := %s(%s)", m, name, str)
		g.collSinks = append(g.collSinks, m)
		g.typeProbeOn(m + `["a"]`)
	case 4: // a deleted key: unknown
		g.helpers = append(g.helpers, fmt.Sprintf("func %s(flag bool) map[string]interface{} {\n\tm := map[string]interface{}{\"a\": \"x\", \"b\": 1}\n\tif flag {\n\t\tdelete(m, \"a\")\n\t}\n\treturn m\n}\n", name))
		m := g.fresh("pm")
		g.line(0, "%s := %s(%s)", m, name, flag)
		g.collSinks = append(g.collSinks, m)
		g.typeProbeOn(m + `["a"]`)
	default: // a constant and a never-nil result
		g.helpers = append(g.helpers, fmt.Sprintf("func %[1]sK() string { return \"k\" }\nfunc %[1]sP() *string { s := \"v\"; return &s }\n", name))
		c, p := g.fresh("pc"), g.fresh("pp")
		g.line(0, "%s := %sK()", c, name)
		g.line(0, "if %s == %s {", c, []string{`"k"`, `"j"`}[g.r.Intn(2)])
		g.line(1, "noop()")
		g.line(0, "}")
		g.line(0, "%s := %sP()", p, name)
		g.line(0, "if %s %s nil {", p, []string{"==", "!="}[g.r.Intn(2)])
		g.line(1, "noop()")
		g.line(0, "}")
		g.sinks = append(g.sinks, c, p)
	}
}

// typeProbeOn writes a type switch, and a nil check, on an interface value.
func (g *gen) typeProbeOn(expr string) {
	cases := []string{"string", "int", "float64", "bool", "*string", "[]interface{}"}
	g.r.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })
	g.line(0, "switch %s.(type) {", expr)
	for _, c := range cases[:2+g.r.Intn(3)] {
		g.line(0, "case %s:", c)
		g.line(1, "noop()")
	}
	if g.r.Intn(2) == 0 {
		g.line(0, "default:")
		g.line(1, "noop()")
	}
	g.line(0, "}")
	if g.r.Intn(2) == 0 {
		g.line(0, "if %s %s nil {", expr, []string{"==", "!="}[g.r.Intn(2)])
		g.line(1, "noop()")
		g.line(0, "}")
	}
}

// lateEscapeProbe builds a map here, checks the type of what it holds, hands it
// to a helper that writes another type into it, and checks again; with inLoop
// the hand-over and the checks are in one loop, so the call of one round comes
// before the checks of the next.
func (g *gen) lateEscapeProbe(name string, inLoop bool) {
	g.helpers = append(g.helpers, fmt.Sprintf("func %s(m map[string]interface{}, flag bool) {\n\tif flag {\n\t\tm[\"a\"] = 5\n\t}\n}\n", name))
	flag := "true"
	if v, ok := g.pick(kBool); ok && g.r.Intn(3) > 0 {
		flag = v.name
	}
	m := g.fresh("lm")
	g.line(0, "%s := map[string]interface{}{\"a\": \"x\", \"b\": 1.5}", m)
	g.collSinks = append(g.collSinks, m)
	if inLoop {
		e := g.fresh("lk")
		g.line(0, "for %s := 0; %s < 2; %s++ {", e, e, e)
		g.line(1, "switch %s[\"a\"].(type) {", m)
		g.line(1, "case int:")
		g.line(2, "noop()")
		g.line(1, "case string:")
		g.line(2, "noop()")
		g.line(1, "}")
		g.line(1, "%s(%s, %s)", name, m, flag)
		g.line(0, "}")
		return
	}
	g.line(0, "switch %s[\"a\"].(type) {", m)
	g.line(0, "case int:")
	g.line(1, "noop()")
	g.line(0, "case string:")
	g.line(1, "noop()")
	g.line(0, "}")
	g.line(0, "%s(%s, %s)", name, m, flag)
	g.line(0, "switch %s[\"a\"].(type) {", m)
	g.line(0, "case int:")
	g.line(1, "noop()")
	g.line(0, "case string:")
	g.line(1, "noop()")
	g.line(0, "}")
}
