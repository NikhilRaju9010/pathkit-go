package soundness

import (
	"fmt"
	"strings"
)

// Helper functions (milestone M4): functions of the package that call the
// Temporal SDK, which PathKit follows into, one copy per call. The probes
// cover helpers with and without an error result, with branches and loops,
// reused with other arguments, that change their own parameters, that are
// closures writing the caller's variables, that take a pointer, that are
// methods, and that call other helpers up to three deep. Every helper's own
// errors are swallowed by the caller, so a program never fails.

// helperName makes a name that no other program in the batch uses.
func (g *gen) helperName() string {
	return fmt.Sprintf("h%s_%d", g.name, g.fresh2())
}

func (g *gen) fresh2() int {
	g.next++
	return g.next
}

// strArg is a string a helper can be called with.
func (g *gen) strArg() string {
	if v, ok := g.pick(kString); ok && g.r.Intn(3) > 0 {
		return v.name
	}
	return g.strConst()
}

// helperProbe declares a helper and writes calls of it in the function.
func (g *gen) helperProbe() {
	name := g.helperName()
	switch g.r.Intn(16) {
	case 0: // an error of its own, swallowed by the caller
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, s string) error {
	_ = workflow.GetInfo(ctx)
	if s == "a" {
		return errors.New("a")
	}
	return nil
}
`, name))
		err := g.fresh("er")
		g.line(0, "if %s := %s(ctx, %s); %s != nil {", err, name, g.strArg(), err)
		g.line(1, "noop()")
		g.line(0, "}")
	case 1: // the same helper twice, which changes its own parameter
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, n int) error {
	if n == 2 {
		return workflow.Sleep(ctx, time.Nanosecond)
	}
	n = 2
	_ = n
	return nil
}
`, name))
		g.line(0, "_ = %s(ctx, %s)", name, g.intConst())
		g.line(0, "_ = %s(ctx, %s)", name, g.intConst())
		g.helperCalls = append(g.helperCalls, fmt.Sprintf("_ = %s(ctx, %s)", name, g.intConst()))
	case 2: // a constant inside the helper
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context) error {
	m := "a"
	if m == "b" {
		return workflow.Sleep(ctx, time.Nanosecond)
	}
	if m == "a" {
		_ = workflow.GetInfo(ctx)
	}
	return nil
}
`, name))
		g.line(0, "_ = %s(ctx)", name)
		g.helperCalls = append(g.helperCalls, fmt.Sprintf("_ = %s(ctx)", name))
	case 3: // up to three helpers deep, a bool handed down
		depth := 1 + g.r.Intn(3)
		names := make([]string, depth)
		for i := range names {
			names[i] = fmt.Sprintf("%s_%d", name, i+1)
		}
		for i := depth - 1; i >= 0; i-- {
			var body string
			if i == depth-1 {
				body = "\t_ = workflow.GetInfo(ctx)\n\tif b {\n\t\treturn errors.New(\"b\")\n\t}\n\treturn nil\n"
			} else {
				arg := "b"
				if g.r.Intn(2) == 0 {
					arg = "!b"
				}
				body = fmt.Sprintf("\tif err := %s(ctx, %s); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n", names[i+1], arg)
			}
			g.helpers = append(g.helpers, fmt.Sprintf("func %s(ctx workflow.Context, b bool) error {\n%s}\n", names[i], body))
		}
		arg := "true"
		if v, ok := g.pick(kBool); ok {
			arg = v.name
		}
		err := g.fresh("er")
		g.line(0, "if %s := %s(ctx, %s); %s != nil {", err, names[0], arg, err)
		g.line(1, "noop()")
		g.line(0, "}")
		g.helperCalls = append(g.helperCalls, fmt.Sprintf("_ = %s(ctx, true)", names[0]))
	case 4: // a closure that writes the caller's variable
		flag, set := g.fresh("fg"), g.fresh("set")
		g.line(0, "%s := %s", flag, g.boolConst())
		g.line(0, "%s := func(ctx workflow.Context) error {", set)
		g.line(1, "%s = %s", flag, g.boolConst())
		g.line(1, "return workflow.Sleep(ctx, time.Nanosecond)")
		g.line(0, "}")
		g.line(0, "_ = %s(ctx)", set)
		g.line(0, "if %s {", flag)
		g.line(1, "noop()")
		g.line(0, "}")
		g.addLocal(flag, kBool)
	case 5: // a pointer to the caller's variable
		if g.next%4 != 0 {
			g.writeProbe(name, g.next%4-1+1)
			break
		}
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, p *string) error {
	*p = "z"
	return workflow.Sleep(ctx, time.Nanosecond)
}
`, name))
		v := g.fresh("pv")
		g.line(0, "%s := %s", v, g.strConst())
		g.line(0, "_ = %s(ctx, &%s)", name, v)
		g.line(0, "if %s == \"\" {", v)
		g.line(1, "noop()")
		g.line(0, "}")
		g.addLocal(v, kString)
	case 6: // a loop inside the helper
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, s string) error {
	for _, e := range []string{"a", "b"} {
		if e == s {
			return workflow.Sleep(ctx, time.Nanosecond)
		}
	}
	return nil
}
`, name))
		g.line(0, "_ = %s(ctx, %s)", name, g.strArg())
		g.helperCalls = append(g.helperCalls, fmt.Sprintf("_ = %s(ctx, %s)", name, g.strConst()))
	case 7: // a value and an error
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, s string) (string, error) {
	if s == "" {
		return "d", nil
	}
	if err := workflow.Sleep(ctx, time.Nanosecond); err != nil {
		return "", err
	}
	return s, nil
}
`, name))
		r, err := g.fresh("rv"), g.fresh("er")
		g.line(0, "%s, %s := %s(ctx, %s)", r, err, name, g.strArg())
		g.line(0, "if %s != nil {", err)
		g.line(1, "noop()")
		g.line(0, "}")
		g.addLocal(r, kString)
	case 8: // a method of a struct
		if g.next%3 == 0 {
			g.writeProbe(name, 0)
			break
		}
		if r, ok := g.pick(kStruct); ok {
			g.helpers = append(g.helpers, fmt.Sprintf(`func (r *Rec) %s(ctx workflow.Context) error {
	_ = workflow.GetInfo(ctx)
	if r.A == "" {
		return errors.New("e")
	}
	return nil
}
`, name))
			err := g.fresh("er")
			g.line(0, "if %s := %s.%s(ctx); %s != nil {", err, r.name, name, err)
			g.line(1, "noop()")
			g.line(0, "}")
		} else {
			g.line(0, "noop()")
		}
	case 10: // an error check PathKit only assumes passes
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, n int) error {
	_ = workflow.GetInfo(ctx)
	if err := pair2(n); err != nil {
		return err
	}
	return nil
}
`, name))
		err := g.fresh("er")
		g.line(0, "if %s := %s(ctx, %s); %s != nil {", err, name, g.intConst(), err)
		g.line(1, "noop()")
		g.line(0, "}")
	case 11: // a deferred function clears the named result
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, b bool) (err error) {
	defer func() {
		if b {
			err = nil
		}
	}()
	_ = workflow.GetInfo(ctx)
	return errors.New("x")
}
`, name))
		arg := "true"
		if v, ok := g.pick(kBool); ok {
			arg = v.name
		}
		err := g.fresh("er")
		g.line(0, "if %s := %s(ctx, %s); %s != nil {", err, name, arg, err)
		g.line(1, "noop()")
		g.line(0, "}")
	case 12, 13: // a map handed to activities and local activities
		g.mapToCallProbe()
	case 14, 15: // values set early, asked about in a later stage
		g.stageProbe()
	default: // returns the helper's result: the helper's end is the workflow's
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, b bool) error {
	if b {
		return errors.New("b")
	}
	return workflow.Sleep(ctx, time.Nanosecond)
}
`, name))
		arg := "true"
		if v, ok := g.pick(kBool); ok {
			arg = v.name
		}
		g.line(0, "if %s {", g.cond())
		g.line(1, "_ = %s(ctx, %s)", name, arg)
		g.line(0, "}")
	}
}

func (g *gen) boolConst() string { return []string{"false", "true"}[g.r.Intn(2)] }

// helperSource is the helpers declared by the program, then nothing else.
func (g *gen) helperSource() string { return strings.Join(g.helpers, "\n") }

// mapToCallProbe hands a map to an activity (the SDK encodes it at the call,
// so the activity cannot change it), to a local activity (which gets the very
// map and does), or to both in some order, directly or inside a struct, and
// then looks at a value of the map. A claim about that value must follow.
func (g *gen) mapToCallProbe() {
	m, v := g.fresh("am"), g.fresh("av")
	actx := g.fresh("ac")
	g.line(0, "%s := map[string]interface{}{\"k\": \"x\"}", m)
	g.line(0, "%s := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})", actx)
	lctx := g.fresh("lc")
	g.line(0, "%s := workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{StartToCloseTimeout: time.Minute})", lctx)
	g.line(0, "_, _ = %s, %s", actx, lctx)
	arg := m
	if g.r.Intn(3) == 0 {
		arg = "&actInput{Data: " + m + "}"
	}
	steps := []string{
		fmt.Sprintf("_ = workflow.ExecuteActivity(%s, ActEcho, %s).Get(%s, nil)", actx, arg, actx),
		fmt.Sprintf("_ = workflow.ExecuteLocalActivity(%s, LocalSet, %s).Get(%s, nil)", lctx, m, lctx),
		fmt.Sprintf("_ = workflow.ExecuteActivity(%s, ActEcho, %s).Get(%s, nil)", actx, arg, actx),
	}
	n := 1 + g.r.Intn(3)
	start := g.r.Intn(len(steps))
	for i := 0; i < n; i++ {
		g.line(0, "%s", steps[(start+i)%len(steps)])
	}
	if g.r.Intn(3) == 0 {
		g.line(0, "if %s {", g.cond())
		g.line(1, "_ = workflow.ExecuteLocalActivity(%s, LocalSet, %s).Get(%s, nil)", lctx, m, lctx)
		g.line(0, "}")
	}
	g.line(0, "%s := %s[\"k\"]", v, m)
	cases := []string{"string", "int", "float64", "nil"}
	g.r.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })
	g.line(0, "switch %s.(type) {", v)
	for _, c := range cases[:2+g.r.Intn(2)] {
		g.line(0, "case %s:", c)
		g.line(1, "noop()")
	}
	g.line(0, "}")
	g.sinks = append(g.sinks, v)
}

// stageProbe sets values in the first decisions of the function, then asks
// about them after more decisions: with small stages the asking is in a later
// stage than the setting, so the answer depends on the states the earlier
// stages leave behind.
func (g *gen) stageProbe() {
	vars := []string{g.fresh("sv"), g.fresh("sv")}
	for _, v := range vars {
		g.line(0, "%s := %s", v, g.strConst())
	}
	for _, v := range vars {
		if g.r.Intn(3) > 0 {
			g.line(0, "if %s {", g.cond())
			g.line(1, "%s = %s", v, g.strConst())
			g.line(0, "}")
		}
	}
	for i, n := 0, 2+g.r.Intn(4); i < n; i++ {
		g.line(0, "if %s {", g.cond())
		g.line(1, "noop()")
		g.line(0, "}")
	}
	for _, v := range vars {
		g.line(0, "if %s == %s {", v, g.strConst())
		g.line(1, "noop()")
		g.line(0, "}")
	}
	g.sinks = append(g.sinks, vars...)
}

// temporalProbe writes a Temporal shape whose steps are decisions the map must
// agree with the recorder about: a timer read later, a wait, a wait with a
// timeout, and a check of the kind of an error (milestone M8). None can fail in
// the test environment, so the failure branches are paths no run takes.
func (g *gen) temporalProbe() {
	switch g.r.Intn(4) {
	case 0: // a timer, read later
		tm, er := g.fresh("tm"), g.fresh("er")
		g.line(0, "%s := workflow.NewTimer(ctx, time.Nanosecond)", tm)
		g.line(0, "noop()")
		g.line(0, "if %s := %s.Get(ctx, nil); %s != nil {", er, tm, er)
		g.line(1, "noop()")
		g.line(0, "}")
	case 1: // a wait that is already over
		er := g.fresh("er")
		g.line(0, "if %s := workflow.Await(ctx, func() bool { return true }); %s != nil {", er, er)
		g.line(1, "noop()")
		g.line(0, "}")
	case 2: // a wait with a timeout that is not needed
		ok, er := g.fresh("ok"), g.fresh("er")
		g.line(0, "%s, %s := workflow.AwaitWithTimeout(ctx, time.Second, func() bool { return true })", ok, er)
		g.line(0, "if %s != nil {", er)
		g.line(1, "noop()")
		g.line(0, "}")
		g.line(0, "if !%s {", ok)
		g.line(1, "noop()")
		g.line(0, "}")
	default: // the kind of an error
		er := g.fresh("er")
		g.line(0, "if %s := workflow.Sleep(ctx, time.Nanosecond); %s != nil {", er, er)
		g.line(1, "if temporal.IsCanceledError(%s) {", er)
		g.line(2, "noop()")
		g.line(1, "} else if temporal.IsTimeoutError(%s) {", er)
		g.line(2, "noop()")
		g.line(1, "}")
		g.line(0, "}")
	}
}

// writeProbe declares a helper that writes to a value it was given (through a
// pointer receiver, a map, a slice or a pointer to a struct) and reads the
// value in the caller's next decision: the caller must forget what it knew.
// It is picked inside the pointer and method probes without drawing more random
// numbers, so the programs of the other probes stay as they were.
func (g *gen) writeProbe(name string, variant int) {
	switch variant {
	case 0: // a pointer receiver writes a field the caller reads next
		if r, ok := g.pick(kStruct); ok {
			g.helpers = append(g.helpers, fmt.Sprintf(`func (r *Rec) %s(ctx workflow.Context) error {
	r.A = "z"
	return workflow.Sleep(ctx, time.Nanosecond)
}
`, name))
			g.line(0, "_ = %s.%s(ctx)", r.name, name)
			g.line(0, "if %s.A == \"\" {", r.name)
			g.line(1, "noop()")
			g.line(0, "}")
		} else {
			g.line(0, "noop()")
		}
	case 1: // a map argument is written by the helper
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, m map[string]string) error {
	m["k"] = "z"
	return workflow.Sleep(ctx, time.Nanosecond)
}
`, name))
		m := g.fresh("mv")
		g.line(0, "%s := map[string]string{\"k\": %s}", m, g.strConst())
		g.line(0, "_ = %s(ctx, %s)", name, m)
		g.line(0, "if %s[\"k\"] == \"\" {", m)
		g.line(1, "noop()")
		g.line(0, "}")
	case 2: // a slice argument is written by the helper
		g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, s []int) error {
	s[0] = 9
	return workflow.Sleep(ctx, time.Nanosecond)
}
`, name))
		sl := g.fresh("sv")
		g.line(0, "%s := []int{%s}", sl, g.intConst())
		g.line(0, "_ = %s(ctx, %s)", name, sl)
		g.line(0, "if %s[0] == 0 {", sl)
		g.line(1, "noop()")
		g.line(0, "}")
	default: // a pointer to a struct is written through, field by field
		if r, ok := g.pick(kStruct); ok {
			g.helpers = append(g.helpers, fmt.Sprintf(`func %s(ctx workflow.Context, p *Rec) error {
	p.B = 7
	p.C = true
	return workflow.Sleep(ctx, time.Nanosecond)
}
`, name))
			g.line(0, "_ = %s(ctx, &%s)", name, r.name)
			g.line(0, "if %s.B == 0 || !%s.C {", r.name, r.name)
			g.line(1, "noop()")
			g.line(0, "}")
		} else {
			g.line(0, "noop()")
		}
	}
}
