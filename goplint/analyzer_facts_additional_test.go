// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestValidatesTypeFactAFact(t *testing.T) {
	t.Parallel()

	var fact analysis.Fact = &ValidatesTypeFact{TypeName: "Server"}
	fact.(*ValidatesTypeFact).AFact()
}

func TestNonZeroFactAFact(t *testing.T) {
	t.Parallel()

	var fact analysis.Fact = &NonZeroFact{}
	fact.(*NonZeroFact).AFact()
}
