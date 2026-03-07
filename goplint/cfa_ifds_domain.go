// SPDX-License-Identifier: MPL-2.0

package goplint

import "strings"

type ifdsFactFamily string

const (
	ifdsFactFamilyZero                    ifdsFactFamily = "zero"
	ifdsFactFamilyCastNeedsValidate       ifdsFactFamily = "cast-needs-validate"
	ifdsFactFamilyUBVNeedsValidateBefore  ifdsFactFamily = "ubv-needs-validate-before-use"
	ifdsFactFamilyCtorReturnNeedsValidate ifdsFactFamily = "ctor-return-needs-validate"
)

type ifdsCastNeedsValidateFact struct {
	OriginKey string
	TargetKey string
	TypeKey   string
}

func (f ifdsCastNeedsValidateFact) Family() ifdsFactFamily {
	return ifdsFactFamilyCastNeedsValidate
}

func (f ifdsCastNeedsValidateFact) Key() string {
	return joinIFDSFactKey(string(f.Family()), f.OriginKey, f.TargetKey, f.TypeKey)
}

type ifdsUBVNeedsValidateBeforeUseFact struct {
	OriginKey string
	TargetKey string
	TypeKey   string
	Mode      string
}

func (f ifdsUBVNeedsValidateBeforeUseFact) Family() ifdsFactFamily {
	return ifdsFactFamilyUBVNeedsValidateBefore
}

func (f ifdsUBVNeedsValidateBeforeUseFact) Key() string {
	return joinIFDSFactKey(string(f.Family()), f.OriginKey, f.TargetKey, f.TypeKey, f.Mode)
}

type ifdsCtorReturnNeedsValidateFact struct {
	ConstructorKey string
	ReturnTypeKey  string
}

func (f ifdsCtorReturnNeedsValidateFact) Family() ifdsFactFamily {
	return ifdsFactFamilyCtorReturnNeedsValidate
}

func (f ifdsCtorReturnNeedsValidateFact) Key() string {
	return joinIFDSFactKey(string(f.Family()), f.ConstructorKey, f.ReturnTypeKey)
}

func joinIFDSFactKey(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		return string(ifdsFactFamilyZero)
	}
	return strings.Join(cleaned, "|")
}
