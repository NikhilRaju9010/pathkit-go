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
	"ImpossibleLengthBeforePlainWriter": {
		"J1.true|completed": ValueFlow,
	},
	"ImpossibleSliceElementBeforeWriter": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleMapPassedToActivity": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleMapInsideActivityInput": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleMapPassedToChildWorkflow": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleMapSignalled": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleHelperLocal": {
		"J1.true|": ConstantCondition,
	},
	"ImpossibleKnownAcrossHelper": {
		"J1.true J3.false J2.success J4.true|": Contradictory,
		"J1.true J3.true J2.success J4.true|":  Contradictory,
	},
	"ImpossibleBeforeMapPassedOn": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleBeforeAddressTaken": {
		"J1.false|completed": ConstantCondition,
	},
	"ImpossibleGetThenAssigned": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleUnmarshalThenAssigned": {
		"J1.true|completed": ConstantCondition,
	},
	"LikelyDefectOtherCasesStayImpossible": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossiblePointerToOtherType": {
		"J1.true|completed": ImpossibleType,
	},
	"ImpossibleStdDecoder": {
		"J1.true|": ImpossibleType,
	},
	"ImpossibleStdUnmarshal": {
		"J1.case int|": ImpossibleType,
	},
	"ImpossibleStdUnmarshalDespiteOtherUseNumber": {
		"J1.true|": ImpossibleType,
	},
	"ImpossibleAppendedSlice": {
		"J1.case float64|completed": ImpossibleType,
		"J1.default|completed":      ImpossibleType,
	},
	"ImpossibleAssertionFails": {
		"J1.true|completed": ImpossibleType,
	},
	"ImpossibleAssertionSucceeds": {
		"J1.true|completed": ImpossibleType,
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
	"ImpossibleDefaultCase": {
		"J1.default|completed": ImpossibleType,
	},
	"ImpossibleEarlyReturn": {
		"J1.false J2.true|completed": ValueFlow,
	},
	"ImpossibleFieldAssigned": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleForCondition": {
		"J1.exit|completed": LoopExit,
	},
	"ImpossibleInterfaceCase": {
		"J1.case interface{Len() int}|completed": ImpossibleType,
		"J1.default|completed":                   ImpossibleType,
	},
	"ImpossibleJSONGet": {
		"J1.case uint8|completed": ImpossibleType,
	},
	"ImpossibleJSONMapParam": {
		"J1.case int64|completed": ImpossibleType,
	},
	"ImpossibleJSONNested": {
		"J1.iterate J2.case int J1.retry J1.exit|completed": ImpossibleType,
	},
	"ImpossibleJSONParam": {
		"J1.case int|completed": ImpossibleType,
	},
	"ImpossibleJSONReceive": {
		"J1.case uint8|completed": ImpossibleType,
	},
	"ImpossibleJSONReceiveFromVariable": {
		"J1.case uint8|completed": ImpossibleType,
	},
	"ImpossibleJSONSideEffect": {
		"J1.case uint8|completed": ImpossibleType,
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
	"ImpossibleLengthAfterAppend": {
		"J1.true|completed": ValueFlow,
	},
	"ImpossibleLengthAfterStore": {
		"J1.true|completed": ValueFlow,
	},
	"ImpossibleLengthOfLiteral": {
		"J1.false J2.true|completed": ValueFlow,
		"J1.true J2.false|completed": ValueFlow,
		"J1.true J2.true|completed":  ValueFlow,
	},
	"ImpossibleLengthOfMake": {
		"J1.true|completed": ValueFlow,
	},
	"ImpossibleLoopKeepsOthers": {
		"J1.true|completed": ConstantCondition,
	},
	"ImpossibleMakeSliceHasNils": {
		"J1.false|completed": ValueFlow,
	},
	"ImpossibleMapUnion": {
		"J1.case int|completed": ImpossibleType,
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
	"ImpossiblePlainKeyIntConst": {
		"J1.true|completed": ImpossibleNil,
	},
	"ImpossiblePointerNilness": {
		"J1.false J2.true|completed": ValueFlow,
	},
	"ImpossibleProvenKey": {
		"J1.case nil|completed": ImpossibleType,
		"J1.default|completed":  ImpossibleType,
	},
	"ImpossibleRangeNonEmpty": {
		"J1.exit|completed": LoopExit,
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
	"ImpossibleTypeSwitchOnStored": {
		"J1.case int|completed": ImpossibleType,
		"J1.default|completed":  ImpossibleType,
	},
	"ImpossibleTypedNil": {
		"J1.true|completed": ImpossibleNil,
	},
	"ImpossibleTypedNilInMap": {
		"J1.true|completed": ImpossibleNil,
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
	"MixedInterfaceCase": {
		"J1.default|completed": ImpossibleType,
	},
	"MixedTypedParam": {
		"J1.default|completed": ImpossibleType,
	},
}

// wantSuspects are the paths the engine sends to possible-correlation.
var wantSuspects = map[string][]string{
	"SuspectLoopBetween": {"J1.true J2.true|completed"},
}

// wantUnknown are the paths that take a type switch case on a value whose origin PathKit cannot follow.
// wantDefects is every path the engine calls a likely defect (E3): the case or
// comparison is impossible only because the value is a pointer. The paths are
// not impossible, so they are not in wantClaims.
var wantDefects = map[string][]string{
	"LikelyDefectCaseString":               {"J1.case string|completed"},
	"LikelyDefectComparesToEmpty":          {"J1.true|completed"},
	"LikelyDefectOtherCasesStayImpossible": {"J1.case string|completed"},
}

var wantUnknown = map[string][]string{
	"PossibleSliceElementAfterWriter":       {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapInPointerStructAfterWriter": {"J1.case int|completed", "J1.default|completed"},
	"PossibleChannelHandedToGo":             {"J1.case int|completed", "J1.default|completed"},
	"PossibleAfterMapPassedOn":              {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapAliasedBeforeTheCheck":      {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapGivenToGo":                  {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapPassedOnInALoop":            {"J1.iterate J2.case int J1.retry J1.exit|completed", "J1.iterate J2.default J1.retry J1.exit|completed"},
	"PossibleAppendSpread":                  {"J1.case int|completed", "J1.default|completed"},
	"PossibleContainerAliasPassedOn":        {"J1.case int|completed", "J1.default|completed"},
	"PossibleContainerChangedViaInterface":  {"J1.case int|completed", "J1.default|completed"},
	"PossibleJSONGetIntoPointer":            {"J1.case *string|completed", "J1.case int|completed", "J1.default|completed"},
	"PossibleJSONMapWritten":                {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapAddressTaken":               {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapAliasWrites":                {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapInsideKeptInput":            {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapPassedToActivityAndCall":    {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapPassedToLocalActivity":      {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapPassedToCall":               {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapReassigned":                 {"J1.case int|completed", "J1.default|completed"},
	"PossibleMapWrittenInClosure":           {"J1.case int|completed", "J1.default|completed"},
	"PossibleOwnChannelReceive":             {"J1.case int|completed", "J1.default|completed"},
	"PossibleRangeIntoElement":              {"J1.iterate J1.retry J1.exit J2.case int|completed", "J1.iterate J1.retry J1.exit J2.default|completed", "J1.exit J2.case int|completed", "J1.exit J2.default|completed"},
	"PossibleSettableFuture":                {"J1.case int|completed", "J1.default|completed"},
	"PossibleSliceAliasWrites":              {"J1.case int|completed", "J1.default|completed"},
	"PossibleSparseSliceLiteral":            {"J1.true J2.case string|completed", "J1.true J2.default|completed", "J1.false J2.case string|completed", "J1.false J2.default|completed"},
	"PossibleStoreInAssumedBranch":          {"J1.case int|completed", "J1.default|completed"},
	"PossibleStoreInLoop":                   {"J1.iterate J1.retry J1.exit J2.case int|completed", "J1.iterate J1.retry J1.exit J2.default|completed", "J1.exit J2.case int|completed", "J1.exit J2.default|completed"},
	"PossibleStoreOfUnknownType":            {"J1.case int|completed", "J1.default|completed"},
	"PossibleTupleStoreIntoElement":         {"J1.case string|completed", "J1.case int|completed", "J1.default|completed"},
}

// wantOff are the workflows the engine switches itself off for, and why.
var wantOff = map[string]string{
	"PossibleRecover": "it calls recover",
	"PossibleUnsafe":  "it uses the unsafe package",
}
