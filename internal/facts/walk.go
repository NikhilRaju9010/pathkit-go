package facts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// A Walk is the engine part way along a path: the notebook as it is after the
// steps taken so far. Step moves it one decision on and says whether that
// decision cannot be taken. A Walk can be copied, so one prefix can be
// followed in several directions (pruning), and it can be handed on to the
// next stage with Rebase.
type Walk struct {
	s    *state
	dead bool
}

// NewWalk starts a walk at the beginning of the workflow.
func (a *Analyzer) NewWalk() *Walk {
	s := newState(a)
	if a.off == "" {
		s.initParams()
		s.run(a.g.Start.Nodes)
	}
	return &Walk{s: s}
}

// Off reports that the engine does nothing for this workflow.
func (w *Walk) Off() bool { return w.s.a.off != "" }

// Step takes one decision of the path. A claim means the decision cannot be
// taken there (and the walk is finished). Without the engine nothing is ever
// claimed.
func (w *Walk) Step(st model.Step) *Claim {
	if w.dead || w.s.a.off != "" {
		return nil
	}
	s := w.s
	step := s.stepsDone + 1
	s.cur = st.Junction.Stmt.Pos()
	var c *Claim
	switch st.Junction.Kind {
	case model.PlainIf:
		c = s.ifStep(step, st.Junction, st.Exit)
	case model.Loop:
		c = s.loopStep(step, st.Junction, st.Exit)
	case model.Switch:
		c = s.switchStep(step, st.Junction, st.Exit)
	}
	if c != nil {
		w.dead = true
		return c
	}
	s.stepsDone = step
	s.run(st.Exit.To.Nodes)
	return nil
}

// Result is what the walk found so far besides claims: a suspect or an
// unknown-input-type.
func (w *Walk) Result() Result {
	return Result{Suspect: w.s.suspect, Unknown: w.s.unknown, Defect: w.s.defect}
}

// Clone copies the walk.
func (w *Walk) Clone() *Walk {
	o := *w.s
	o.d = make(map[key][]*datum, len(w.s.d))
	for k, ds := range w.s.d {
		cp := make([]*datum, len(ds))
		for i, d := range ds {
			c := *d
			if d.ts != nil {
				c.ts = d.ts.clone()
			}
			c.keys = append(c.keys[:0:0], d.keys...)
			cp[i] = &c
		}
		o.d[k] = cp
	}
	o.links = make(map[key]link, len(w.s.links))
	for k, l := range w.s.links {
		o.links[k] = l
	}
	o.skips = append(o.skips[:0:0], w.s.skips...)
	o.taken = append(o.taken[:0:0], w.s.taken...)
	o.seenLoop = make(map[*model.Junction]bool, len(w.s.seenLoop))
	for k, v := range w.s.seenLoop {
		o.seenLoop[k] = v
	}
	return &Walk{s: &o, dead: w.dead}
}

// Rebase makes the walk the start of a new stage: steps are counted from 0
// again, and what the walk found before (a suspect, an unknown-input-type) is
// not carried on. The facts stay; proofs name the steps of the new stage only.
func (w *Walk) Rebase() {
	s := w.s
	s.stage++
	s.stepsDone = 0
	s.taken = nil
	s.suspect, s.unknown, s.defect = nil, nil, nil
	s.seenLoop = map[*model.Junction]bool{}
}

// Signature is what the notebook says, with the reasons left out: two walks
// with the same signature settle every later decision the same way. Facts the
// engine has forgotten but keeps for possible-correlation count too.
func (w *Walk) Signature() string {
	var keys []string
	by := map[string][]*datum{}
	for k, ds := range w.s.d {
		name := fmt.Sprintf("%p%s", k.v, k.path)
		keys = append(keys, name)
		by[name] = ds
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, name := range keys {
		var parts []string
		for _, d := range by[name] {
			p := fmt.Sprintf("%d", d.op)
			if d.val != nil {
				p += "=" + d.val.ExactString()
			}
			if d.ts != nil {
				var ts []string
				for _, e := range d.ts.ents {
					ts = append(ts, e.t.String())
				}
				sort.Strings(ts)
				p += fmt.Sprintf("[%s nil=%v json=%v]", strings.Join(ts, ","), d.ts.mayNil, d.ts.json)
			}
			for _, kv := range d.keys {
				p += "," + kv.ExactString()
			}
			if d.perKey != nil {
				var ks []string
				for k, set := range d.perKey {
					var ts []string
					for _, e := range set.ents {
						ts = append(ts, e.t.String())
					}
					sort.Strings(ts)
					ks = append(ks, fmt.Sprintf("%q:[%s nil=%v]", k, strings.Join(ts, ","), set.mayNil))
				}
				sort.Strings(ks)
				p += "{" + strings.Join(ks, ";") + "}"
			}
			if d.soft != "" {
				p += "~"
			}
			parts = append(parts, p)
		}
		sort.Strings(parts)
		fmt.Fprintf(&b, "%s:%s;", name, strings.Join(parts, "|"))
	}
	var ls []string
	for k, l := range w.s.links {
		ls = append(ls, fmt.Sprintf("%p%s->%p%s", k.v, k.path, l.v.v, l.v.path))
	}
	sort.Strings(ls)
	b.WriteString("L:" + strings.Join(ls, ","))
	return b.String()
}
