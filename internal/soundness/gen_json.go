package soundness

// Values encoding/json decodes in the function (milestone M4+): the program
// writes a value as JSON and reads it back into an interface{}, with
// json.Unmarshal, with a decoder, or with a decoder switched to UseNumber.
// A number is then a float64, or a json.Number with UseNumber; claims about
// the types the value can hold must follow.

func (g *gen) jsonProbe() {
	src := `"a"`
	if v, ok := g.pick(kIface); ok {
		src = v.name
	} else if v, ok := g.pick(kString); ok {
		src = v.name
	}
	raw, out := g.fresh("jb"), g.fresh("jv")
	g.line(0, "%s, _ := json.Marshal(%s)", raw, src)
	g.line(0, "var %s interface{}", out)
	useNumber := false
	switch g.r.Intn(3) {
	case 0:
		g.line(0, "_ = json.Unmarshal(%s, &%s)", raw, out)
	case 1:
		dec := g.fresh("jd")
		g.line(0, "%s := json.NewDecoder(bytes.NewReader(%s))", dec, raw)
		g.line(0, "_ = %s.Decode(&%s)", dec, out)
	default:
		dec := g.fresh("jd")
		useNumber = true
		g.line(0, "%s := json.NewDecoder(bytes.NewReader(%s))", dec, raw)
		g.line(0, "%s.UseNumber()", dec)
		g.line(0, "_ = %s.Decode(&%s)", dec, out)
	}
	cases := []string{"string", "float64", "bool", "nil", "[]interface{}", "map[string]interface{}", "int"}
	if useNumber {
		cases = append(cases, "json.Number")
	}
	g.r.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })
	n := 2 + g.r.Intn(3)
	g.line(0, "switch %s.(type) {", out)
	for _, c := range cases[:n] {
		g.line(0, "case %s:", c)
		g.line(1, "noop()")
	}
	g.line(0, "}")
	if g.r.Intn(2) == 0 {
		g.line(0, "if _, ok := %s.(%s); ok {", out, []string{"float64", "int", "string"}[g.r.Intn(3)])
		g.line(1, "noop()")
		g.line(0, "}")
	}
	g.sinks = append(g.sinks, out)
}

// addrProbe is the shape E1 relies on (milestone M11): a typed variable filled
// through `&x` by json.Unmarshal, with decisions on it before the call, after
// it, and after a plain assignment that follows it. The variable is a plain
// string or int, so it is the value written by the call that decides the
// answer, not a type. Half the time the address is kept in a variable first,
// which must keep every note about the variable off.
func (g *gen) addrProbe() {
	if g.budget < 3 {
		g.line(0, "noop()")
		return
	}
	g.budget -= 3
	typ, zero := "string", `""`
	src, ok := g.pick(kString)
	consts := []string{`"a"`, `""`, `"b"`}
	if p, ok2 := g.pick(kInt); ok2 && (!ok || g.r.Intn(2) == 0) {
		typ, zero, src, ok = "int", "0", p, true
		consts = []string{"0", "1", "2"}
	}
	if !ok {
		g.line(0, "noop()")
		return
	}
	raw, x := g.fresh("ab"), g.fresh("av")
	g.line(0, "%s, _ := json.Marshal(%s)", raw, src.name)
	g.line(0, "var %s %s", x, typ)
	if g.r.Intn(2) == 0 { // before the call: still the zero value
		g.line(0, "if %s == %s {", x, zero)
		g.line(1, "noop()")
		g.line(0, "}")
	}
	call := "&" + x
	if g.r.Intn(2) == 0 { // the address is kept: nothing may be assumed about x
		p := g.fresh("ap")
		g.line(0, "%s := &%s", p, x)
		g.sinks = append(g.sinks, p)
		call = p
	}
	g.line(0, "_ = json.Unmarshal(%s, %s)", raw, call)
	if g.r.Intn(2) == 0 { // a plain assignment gives it a known value again
		g.line(0, "%s = %s", x, consts[g.r.Intn(len(consts))])
	}
	g.line(0, "if %s == %s {", x, consts[g.r.Intn(len(consts))])
	g.line(1, "noop()")
	g.line(0, "}")
	g.sinks = append(g.sinks, x)
}
