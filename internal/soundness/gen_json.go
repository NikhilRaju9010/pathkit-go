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
