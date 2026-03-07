// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/token"
	"maps"

	"golang.org/x/tools/go/analysis"
)

func useBeforeValidateMessage(targetName, typeName string, crossBlock bool) string {
	if crossBlock {
		return fmt.Sprintf("variable %s of type %s used before Validate() across blocks", targetName, typeName)
	}
	return fmt.Sprintf("variable %s of type %s used before Validate() in same block", targetName, typeName)
}

func unvalidatedCastMessage(typeName string) string {
	return fmt.Sprintf("type conversion to %s from non-constant without Validate() check", typeName)
}

func unvalidatedCastInconclusiveMessage(typeName string) string {
	return fmt.Sprintf("type conversion to %s from non-constant has inconclusive Validate() path analysis", typeName)
}

func useBeforeValidateInconclusiveMessage(targetName, typeName string) string {
	return fmt.Sprintf("variable %s of type %s has inconclusive use-before-validate path analysis", targetName, typeName)
}

func constructorValidateInconclusiveMessage(qualCtorName, returnTypePkg, returnTypeName string) string {
	return fmt.Sprintf(
		"constructor %s returns %s.%s with inconclusive Validate() path analysis",
		qualCtorName, returnTypePkg, returnTypeName)
}

func reportFindingIfNotBaselined(
	pass *analysis.Pass,
	bl *BaselineConfig,
	pos token.Pos,
	category, findingID, message string,
) {
	reportFindingWithMetaIfNotBaselined(pass, bl, pos, category, findingID, message, nil)
}

func reportFindingWithMetaIfNotBaselined(
	pass *analysis.Pass,
	bl *BaselineConfig,
	pos token.Pos,
	category, findingID, message string,
	meta map[string]string,
) {
	if bl.ContainsFinding(category, findingID, message) {
		return
	}
	reportDiagnosticWithMeta(pass, pos, category, findingID, message, meta)
}

func reportInconclusiveFindingWithMetaIfNotBaselined(
	pass *analysis.Pass,
	bl *BaselineConfig,
	inconclusivePolicy string,
	pos token.Pos,
	category, findingID, message string,
	meta map[string]string,
) {
	if inconclusivePolicy == cfgInconclusivePolicyOff {
		return
	}
	effectiveMeta := copyFindingMeta(meta)
	if effectiveMeta == nil {
		effectiveMeta = make(map[string]string)
	}
	effectiveMeta["cfg_inconclusive_policy"] = inconclusivePolicy
	if inconclusivePolicy == cfgInconclusivePolicyWarn {
		effectiveMeta["cfg_inconclusive_severity"] = "warning"
	}
	reportFindingWithMetaIfNotBaselined(pass, bl, pos, category, findingID, message, effectiveMeta)
}

func copyFindingMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	maps.Copy(out, meta)
	return out
}
