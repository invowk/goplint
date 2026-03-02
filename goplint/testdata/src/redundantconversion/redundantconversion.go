// SPDX-License-Identifier: MPL-2.0

package redundantconversion

// --- Named string types ---

type TokenA string
type TokenB string

// --- Named int types ---

type CountA int
type CountB int

// --- Type with different underlying type ---

type Label string
type Code int

// --- Should be flagged ---

func stringHop(a TokenA) TokenB {
	return TokenB(string(a)) // want `redundant intermediate conversion to string in TokenB\(string\(\.\.\.\)\); use TokenB\(\.\.\.\) directly`
}

func intHop(a CountA) CountB {
	return CountB(int(a)) // want `redundant intermediate conversion to int in CountB\(int\(\.\.\.\)\); use CountB\(\.\.\.\) directly`
}

func multipleInOneFunc(a TokenA, c CountA) {
	_ = TokenB(string(a)) // want `redundant intermediate conversion to string in TokenB\(string\(\.\.\.\)\); use TokenB\(\.\.\.\) directly`
	_ = CountB(int(c))    // want `redundant intermediate conversion to int in CountB\(int\(\.\.\.\)\); use CountB\(\.\.\.\) directly`
}

func sameType(a TokenA) TokenA {
	return TokenA(string(a)) // want `redundant intermediate conversion to string in TokenA\(string\(\.\.\.\)\); use TokenA\(\.\.\.\) directly`
}

// methodReceiver: tests the receiver method path for qualFuncName construction.
func (a TokenA) ToB() TokenB {
	return TokenB(string(a)) // want `redundant intermediate conversion to string in TokenB\(string\(\.\.\.\)\); use TokenB\(\.\.\.\) directly`
}

// --- Should NOT be flagged (for redundant-conversion) ---

// directConversion: no intermediate hop — already the correct form.
func directConversion(a TokenA) TokenB {
	return TokenB(a)
}

// fromLiteral: inner arg is an untyped constant, not a named type.
func fromLiteral() TokenB {
	return TokenB(string("hello"))
}

// fromBareString: inner arg is raw string, not a named type.
func fromBareString(s string) TokenB { // want `parameter "s" of redundantconversion\.fromBareString uses primitive type string`
	return TokenB(string(s))
}

// toBasic: outer target is a basic type, not a named type.
func toBasic(a TokenA) string { // want `return value of redundantconversion\.toBasic uses primitive type string`
	return string(a)
}

// namedIntermediate: intermediate is a named type (not basic), out of scope.
func namedIntermediate(a TokenA) TokenB {
	return TokenB(Label(a))
}

// differentUnderlying: outer and inner arg have different underlying types.
func differentUnderlying(a Label) Code {
	_ = a
	return Code(0)
}

// functionCallNotConversion: inner call is a function, not a type conversion.
func trim(s string) string { return s } // want `parameter "s" of redundantconversion\.trim uses primitive type string` `return value of redundantconversion\.trim uses primitive type string`

func functionCallNotConversion(s string) TokenB { // want `parameter "s" of redundantconversion\.functionCallNotConversion uses primitive type string`
	return TokenB(trim(s))
}

// outerFunctionNotConversion: outer call is a function, not a type conversion.
func makeToken(s string) string { return s } // want `parameter "s" of redundantconversion\.makeToken uses primitive type string` `return value of redundantconversion\.makeToken uses primitive type string`

func outerFunctionNotConversion(a TokenA) string { // want `return value of redundantconversion\.outerFunctionNotConversion uses primitive type string`
	return makeToken(string(a))
}
