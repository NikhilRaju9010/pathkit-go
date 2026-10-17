package facts

import (
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// key names a tracked value: a variable, or a field path under a variable
// that holds a struct by value ("in" + ".A.B").
type key struct {
	v    *types.Var
	path string
}

func (k key) String() string { return k.v.Name() + k.path }

type origin int

const (
	oDecl       origin = iota // var x T: the zero value
	oConst                    // x = <constant>
	oBranch                   // a decision on the path
	oCommaOk                  // v, ok := ...; ok is false, so v is the zero value
	oCopy                     // y := x
	oNonNil                   // &T{}, new, make, composite literal
	oNil                      // = nil
	oStore                    // a value of a known type is put in an interface, slice or map
	oJSON                     // a value decoded by Temporal's default JSON converter
	oStdJSON                  // a value decoded by encoding/json in this function
	oTypeAssert               // x.(T) is decided by the types x can hold
	oElem                     // a value read from a slice or map
	oLen                      // the length of a slice, map or string
)

// why is where a fact came from.
type why struct {
	seq    int
	origin origin
	step   int // path step (1-based) for oBranch and oCommaOk
	pos    token.Pos
	// text builds the sentence; it is only called when a proof is written,
	// so the many facts that never lead to a claim cost no formatting.
	text    func() string
	key     key  // the value the fact is about
	early   bool // oBranch: learned from an if whose other side leaves (return, ...)
	parents []*why
	atStep  int // path steps done when the fact was created
	stage   int // the stage it was created in (see state.stage)
}

type dop int

const (
	dEq dop = iota
	dNe
	dNil
	dNonNil
	dTypes      // an interface value holds one of ts (see typeSet)
	dElem       // the elements of a slice or map hold ts
	dLenZero    // a slice or map is empty
	dLenNonZero // a slice or map has an element
	dKeys       // these constant keys are in the map
)

// datum is one thing known about one value.
type datum struct {
	op   dop
	val  constant.Value
	ts   *typeSet         // dTypes, dElem
	keys []constant.Value // dKeys
	why  *why
	// soft is "" while the fact is trusted. When PathKit has to forget the
	// value (a loop may change it, its address is taken, ...) the fact
	// stays here with the reason, only for the possible-correlation check;
	// it never supports a claim.
	soft string
}

type link struct {
	v  key
	ty types.Type
	by *why
}

type span struct{ from, to token.Pos }

// state is the notebook while one path is walked.
type state struct {
	a         *Analyzer
	d         map[key][]*datum
	links     map[key]link // "ok" value -> the value it guards (comma-ok forms)
	skips     []span
	seq       int
	stepsDone int
	// stage counts the stages the walk has been handed on through (Rebase);
	// a fact from an earlier stage has no step number to show.
	stage    int
	suspect  *Suspect
	unknown  *Suspect
	seenLoop map[*model.Junction]bool
	// taken records the decisions already walked, for proofs that mention
	// code a decision skipped.
	taken []takenStep
}

// takenStep is a decision the path took, and the code on the side it did not take.
type takenStep struct {
	info    stepInfo
	skipped ast.Node // the body or else of the if that this path does not run (nil if none)
}

func newState(a *Analyzer) *state {
	return &state{a: a, d: map[key][]*datum{}, links: map[key]link{}, seenLoop: map[*model.Junction]bool{}}
}

func (s *state) newWhy(o origin, pos token.Pos, k key, text func() string, parents ...*why) *why {
	s.seq++
	return &why{seq: s.seq, origin: o, key: k, pos: pos, text: text, parents: parents, atStep: s.stepsDone, stage: s.stage}
}

// where returns the file (base name) and line the reason points at.
func (a *Analyzer) where(w *why) (string, int) {
	if w.pos == token.NoPos {
		return "", 0
	}
	return a.baseFile(w.pos)
}

func lit(text string) func() string { return func() string { return text } }

func (s *state) inSkip(pos token.Pos) bool {
	for _, sp := range s.skips {
		if pos >= sp.from && pos < sp.to {
			return true
		}
	}
	return false
}

// killKey forgets everything about k and the fields under it, for good:
// the value was assigned something else.
func (s *state) killKey(k key) {
	for kk := range s.d {
		if kk.v == k.v && underPath(kk.path, k.path) {
			delete(s.d, kk)
		}
	}
	for ok, l := range s.links {
		// A write to the guarded value, to a field inside it, or to the whole
		// struct it is a field of, ends the link: v is no longer what the
		// comma-ok form left it.
		touches := func(p string) bool { return underPath(p, k.path) || underPath(k.path, p) }
		if (ok.v == k.v && touches(ok.path)) || (l.v.v == k.v && touches(l.v.path)) {
			delete(s.links, ok)
		}
	}
}

// forgetVar stops trusting what is known about v (but remembers it, with
// the reason, for the possible-correlation check): v may have changed.
func (s *state) forgetVar(v *types.Var, reason string) {
	for kk, ds := range s.d {
		if kk.v != v {
			continue
		}
		for _, d := range ds {
			if d.soft == "" {
				d.soft = reason
			}
		}
	}
	for ok, l := range s.links {
		if ok.v == v || l.v.v == v {
			delete(s.links, ok)
		}
	}
}

// underPath: is p the path q or a field path below it?
func underPath(p, q string) bool { return p == q || strings.HasPrefix(p, q+".") }

// add records a fact about k. Facts about a value the engine never trusts
// (see Analyzer.trackable) are born soft.
func (s *state) add(k key, d *datum) {
	switch d.op {
	case dElem, dLenZero, dLenNonZero, dKeys:
		if s.a.escapes[k.v] != "" {
			return // something else can reach its contents
		}
	}
	if !s.a.trackable(k.v) {
		reason := s.a.untracked[k.v]
		if reason == "" {
			reason = "PathKit does not follow this value"
		}
		d.soft = reason
	}
	switch d.op {
	case dEq: // an exact value replaces everything else known
		s.d[k] = []*datum{d}
		return
	case dNil, dNonNil:
		keep := s.d[k][:0:0]
		for _, o := range s.d[k] {
			if o.op != dNil && o.op != dNonNil {
				keep = append(keep, o)
			}
		}
		s.d[k] = append(keep, d)
		return
	case dTypes, dElem, dKeys:
		s.d[k] = append(withoutOp(s.d[k], d.op), d)
		return
	case dLenZero, dLenNonZero:
		s.d[k] = append(withoutOp(withoutOp(s.d[k], dLenZero), dLenNonZero), d)
		return
	case dNe:
		active := 0
		for _, o := range s.d[k] {
			if o.op == dNe {
				if o.soft == d.soft && cmpEq(o.val, d.val) {
					return
				}
				active++
			}
		}
		if active >= 8 {
			return
		}
	}
	s.d[k] = append(s.d[k], d)
}

// datums returns what is known about k; with shadow, soft facts count too.
func (s *state) datums(k key, shadow bool) []*datum {
	var out []*datum
	for _, d := range s.d[k] {
		if d.soft == "" || shadow {
			out = append(out, d)
		}
	}
	return out
}

// cmpEq: do two constants have the same value (and a comparable kind)?
func cmpEq(a, b constant.Value) bool {
	r, ok := cmp(a, token.EQL, b)
	return ok && r
}

// cmp compares two constants when their kinds can be compared.
func cmp(a constant.Value, op token.Token, b constant.Value) (res bool, ok bool) {
	if a == nil || b == nil {
		return false, false
	}
	ka, kb := a.Kind(), b.Kind()
	numeric := func(k constant.Kind) bool { return k == constant.Int || k == constant.Float }
	switch {
	case ka == constant.String && kb == constant.String:
	case ka == constant.Bool && kb == constant.Bool:
		if op != token.EQL && op != token.NEQ {
			return false, false
		}
	case numeric(ka) && numeric(kb):
	default:
		return false, false
	}
	return constant.Compare(a, op, b), true
}

// typeKind sorts a type for the engine.
type typeKind int

const (
	tkOther typeKind = iota
	tkBool
	tkString
	tkNumber
	tkNilable // pointer, interface, map, slice, chan, func
	tkStruct
)

func kindOf(t types.Type) typeKind {
	if t == nil {
		return tkOther
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return tkBool
		case u.Info()&types.IsString != 0:
			return tkString
		case u.Info()&(types.IsInteger|types.IsFloat) != 0:
			return tkNumber
		}
	case *types.Pointer, *types.Interface, *types.Map, *types.Slice, *types.Chan, *types.Signature:
		if _, isTypeParam := t.(*types.TypeParam); isTypeParam {
			return tkOther
		}
		return tkNilable
	case *types.Struct:
		return tkStruct
	}
	return tkOther
}

func zeroConst(t types.Type) constant.Value {
	switch kindOf(t) {
	case tkBool:
		return constant.MakeBool(false)
	case tkString:
		return constant.MakeString("")
	case tkNumber:
		return constant.MakeInt64(0)
	}
	return nil
}

func exprText(e ast.Expr) string { return types.ExprString(e) }
