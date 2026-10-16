package facts

// wantClaims is every claim the engine makes on the fixture, by workflow:
// the path (its key) and the label. A workflow not listed makes none.
var wantClaims = map[string]map[string]Kind{
	"ImpossibleAndOr": {
		"J1.false J4.false J5.true|completed":                   Contradictory,
		"J1.true J2.false J3.false J4.false J5.false|completed": Contradictory,
		"J1.true J2.false J3.false J4.false J5.true|completed":  Contradictory,
		"J1.true J2.false J3.true J4.false J5.false|completed":  Contradictory,
		"J1.true J2.false J3.true J4.false J5.true|completed":   Contradictory,
		"J1.true J2.false J3.true J4.true|completed":            Contradictory,
		"J1.true J2.true J3.false J4.false J5.false|completed":  Contradictory,
		"J1.true J2.true J3.false J4.false J5.true|completed":   Contradictory,
		"J1.true J2.true J3.false J4.true|completed":            Contradictory,
		"J1.true J2.true J3.true J4.false J5.false|completed":   Contradictory,
		"J1.true J2.true J3.true J4.false J5.true|completed":    Contradictory,
		"J1.true J2.true J3.true J4.true|completed":             Contradictory,
	},
	"ImpossibleBranchContradiction": {
		"J1.true J2.true|completed": Contradictory,
	},
	"ImpossibleCommaOk": {
		"J1.false J3.true J4.true|completed":         ValueFlow,
		"J1.true J2.false J3.true J4.true|completed": ValueFlow,
		"J1.true J2.true J3.false|completed":         ValueFlow,
		"J1.true J2.true J3.true J4.false|completed": ValueFlow,
		"J1.true J2.true J3.true J4.true|completed":  ValueFlow,
	},
	"ImpossibleCommaOkStructZero": {
		"J1.true J2.true|": ValueFlow,
	},
	"ImpossibleConstAssigned": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleConstReassigned": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleCopy": {
		"J1.true|completed": ValueFlow,
	},
	"ImpossibleDeclaredOthers": {
		"J1.false J2.false J3.true|completed": ValueFlow,
		"J1.false J2.true J3.false|completed": ValueFlow,
		"J1.false J2.true J3.true|completed":  ValueFlow,
		"J1.true J2.false J3.false|completed": ConstantCondition,
		"J1.true J2.false J3.true|completed":  ConstantCondition,
		"J1.true J2.true J3.false|completed":  ConstantCondition,
		"J1.true J2.true J3.true|completed":   ConstantCondition,
	},
	"ImpossibleDeclaredString": {
		"J1.false|completed": ConstantCondition,
	},
	"ImpossibleDeclaredStruct": {
		"J1.false J2.true|completed": ValueFlow,
		"J1.true J2.false|completed": ConstantCondition,
		"J1.true J2.true|completed":  ConstantCondition,
	},
	"ImpossibleEarlyReturn": {
		"J1.false J2.true|completed": ValueFlow,
	},
	"ImpossibleFieldAssigned": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleLenForms": {
		"J1.false J2.false J3.false J4.false|completed": ValueFlow,
		"J1.false J2.false J3.true J4.false|completed":  ValueFlow,
		"J1.false J2.false J3.true J4.true|completed":   ValueFlow,
		"J1.false J2.true J3.false J4.false|completed":  ValueFlow,
		"J1.false J2.true J3.false J4.true|completed":   ValueFlow,
		"J1.false J2.true J3.true J4.false|completed":   ValueFlow,
		"J1.false J2.true J3.true J4.true|completed":    ValueFlow,
	},
	"ImpossibleLoopKeepsOthers": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleNamedBool": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleNonNil": {
		"J1.false J2.false J3.true|completed": ValueFlow,
		"J1.false J2.true J3.false|completed": ValueFlow,
		"J1.false J2.true J3.true|completed":  ValueFlow,
		"J1.true J2.false J3.false|completed": ConstantCondition,
		"J1.true J2.false J3.true|completed":  ConstantCondition,
		"J1.true J2.true J3.false|completed":  ConstantCondition,
		"J1.true J2.true J3.true|completed":   ConstantCondition,
	},
	"ImpossibleNotEqualFalse": {
		"J1.false J2.true|completed": Contradictory,
	},
	"ImpossibleNotEqualSet": {
		"J1.true J2.true J3.true|completed": Contradictory,
	},
	"ImpossiblePointerNilness": {
		"J1.false J2.true|completed": ValueFlow,
	},
	"ImpossibleSameValueTwice": {
		"J1.false J2.true|completed": Contradictory,
		"J1.true J2.false|completed": Contradictory,
	},
	"ImpossibleStructCopy": {
		"J1.true|completed": ValueFlow,
	},
	"ImpossibleSwap": {
		"J1.false J2.true|completed": ValueFlow,
		"J1.true J2.false|completed": ValueFlow,
		"J1.true J2.true|completed":  ValueFlow,
	},
	"ImpossibleUnknownAnd": {
		"J1.true J2.false|completed": ConstantCondition,
		"J1.true J2.true|completed":  ConstantCondition,
	},
	"MixedCommaOkFieldWritten": {
		"J1.true J2.true|completed": ValueFlow,
	},
	"MixedCommaOkNestedFieldWritten": {
		"J1.true J2.true|completed": ValueFlow,
	},
}

// wantSuspects are the paths the engine sends to possible-correlation.
var wantSuspects = map[string][]string{
	"SuspectLoopBetween": {"J1.true J2.true|completed"},
}

// wantOff are the workflows the engine switches itself off for, and why.
var wantOff = map[string]string{
	"PossibleRecover": "it calls recover",
	"PossibleUnsafe":  "it uses the unsafe package",
}
