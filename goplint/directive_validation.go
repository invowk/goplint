// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

type directiveAttachment string

const (
	directiveAttachmentFile        directiveAttachment = "file"
	directiveAttachmentDeclaration directiveAttachment = "declaration"
	directiveAttachmentType        directiveAttachment = "type"
	directiveAttachmentField       directiveAttachment = "field"
	directiveAttachmentFunction    directiveAttachment = "function"
	directiveAttachmentMethod      directiveAttachment = "method"
	directiveAttachmentParameter   directiveAttachment = "parameter"
	directiveAttachmentStatement   directiveAttachment = "statement"
)

type directiveDefinition struct {
	requiresValue bool
	validateValue func(string) bool
	attachments   []directiveAttachment
}

type directiveOccurrence struct {
	key      string
	value    string
	hasValue bool
	valid    bool
	comment  *ast.Comment
}

type directiveProblem struct {
	code    string
	key     string
	related string
	message string
}

type directiveSite struct {
	attachment directiveAttachment
	name       string
	groups     []*ast.CommentGroup
	anchor     ast.Node
}

type directiveDiagnostic struct {
	pos       token.Pos
	findingID string
	message   string
}

func directiveDefinitionFor(key string) (directiveDefinition, bool) {
	typeAttachments := []directiveAttachment{directiveAttachmentType}
	switch key {
	case "ignore":
		return directiveDefinition{attachments: []directiveAttachment{
			directiveAttachmentDeclaration,
			directiveAttachmentType,
			directiveAttachmentField,
			directiveAttachmentFunction,
			directiveAttachmentMethod,
			directiveAttachmentParameter,
			directiveAttachmentStatement,
		}}, true
	case "internal":
		return directiveDefinition{attachments: []directiveAttachment{directiveAttachmentField}}, true
	case "render":
		return directiveDefinition{attachments: []directiveAttachment{
			directiveAttachmentField,
			directiveAttachmentFunction,
			directiveAttachmentMethod,
		}}, true
	case "nonzero":
		return directiveDefinition{attachments: []directiveAttachment{
			directiveAttachmentType,
			directiveAttachmentMethod,
		}}, true
	case "validate-all", "constant-only", "mutable", "cue-fed-path":
		attachments := typeAttachments
		if key == "validate-all" {
			attachments = []directiveAttachment{directiveAttachmentType, directiveAttachmentMethod}
		}
		return directiveDefinition{attachments: attachments}, true
	case "no-delegate":
		return directiveDefinition{attachments: []directiveAttachment{directiveAttachmentField}}, true
	case "enum-cue":
		return directiveDefinition{
			requiresValue: true,
			validateValue: validCUEDefinitionPath,
			attachments:   typeAttachments,
		}, true
	case "trusted-boundary":
		return directiveDefinition{attachments: []directiveAttachment{
			directiveAttachmentFunction,
			directiveAttachmentMethod,
		}}, true
	case "path-domain":
		return directiveDefinition{
			requiresValue: true,
			validateValue: isPathDomainName,
			attachments:   typeAttachments,
		}, true
	default:
		return directiveDefinition{}, false
	}
}

func validCUEDefinitionPath(value string) bool {
	if len(value) < 2 || value[0] != '#' {
		return false
	}
	for idx, r := range value[1:] {
		if idx == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// hasIgnoreDirective checks whether a field, function, method, parameter, or
// statement has a valid ignore directive.
func hasIgnoreDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "ignore")
}

// hasInternalDirective checks whether a struct field is explicitly internal.
func hasInternalDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "internal")
}

// hasRenderDirective checks whether a function, method, or field is rendered
// presentation text rather than a domain value.
func hasRenderDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "render")
}

// hasMutableDirective checks whether a type is intentionally mutable.
func hasMutableDirective(genDoc *ast.CommentGroup, specDoc *ast.CommentGroup) bool {
	return hasDirectiveKey(genDoc, specDoc, "mutable")
}

// hasNoDelegateDirective checks whether a struct field is deliberately not
// delegated by its containing type's Validate method.
func hasNoDelegateDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "no-delegate")
}

// hasTrustedBoundaryDirective checks whether a function or method is an
// intentional validation-boundary exception.
func hasTrustedBoundaryDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "trusted-boundary")
}

// hasMethodDirective resolves both the method's ordinary documentation and a
// separately grouped directive immediately preceding it. The central site
// collector is the attachment authority, so validation and consumers cannot
// disagree about whether a loose directive belongs to a method.
func hasMethodDirective(pass *analysis.Pass, method *ast.FuncDecl, key string) bool {
	if pass == nil || method == nil || method.Recv == nil {
		return false
	}
	for _, file := range pass.Files {
		if file == nil || method.Pos() < file.Pos() || method.End() > file.End() {
			continue
		}
		for _, site := range collectDirectiveSites(pass, file) {
			if site.attachment == directiveAttachmentMethod && site.anchor == method {
				return hasDirectiveKeyFromGroups(site.groups, key)
			}
		}
	}
	return false
}

func hasDirectiveKeyFromGroups(groups []*ast.CommentGroup, key string) bool {
	occurrences, problems := parseDirectiveGroups(groups)
	if len(problems) != 0 {
		return false
	}
	for _, occurrence := range occurrences {
		if occurrence.valid && occurrence.key == key {
			return true
		}
	}
	return false
}

// hasDirectiveKey returns true only when the complete directive set is valid.
// An unknown, malformed, duplicate, or conflicting peer prevents every member
// of the set from influencing analyzer behavior.
func hasDirectiveKey(doc *ast.CommentGroup, lineComment *ast.CommentGroup, key string) bool {
	occurrences, problems := parseDirectiveGroups([]*ast.CommentGroup{doc, lineComment})
	if len(problems) != 0 {
		return false
	}
	for _, occurrence := range occurrences {
		if occurrence.valid && occurrence.key == key {
			return true
		}
	}
	return false
}

// directiveValue returns a parameterized directive value only when the entire
// directive set is valid. This prevents a partially recognized configuration
// from reaching a consumer.
func directiveValue(groups []*ast.CommentGroup, key string) (string, bool) {
	occurrences, problems := parseDirectiveGroups(groups)
	if len(problems) != 0 {
		return "", false
	}
	for _, occurrence := range occurrences {
		if occurrence.valid && occurrence.key == key && occurrence.hasValue {
			return occurrence.value, true
		}
	}
	return "", false
}

// parseDirectiveKeys retains the small parser oracle used by focused tests.
// Production consumers use parseDirectiveGroups so malformed peer directives
// cannot be ignored.
func parseDirectiveKeys(text string) (keys []string, unknown []string) {
	comment := &ast.Comment{Text: text}
	occurrences, problems, _ := parseDirectiveComment(comment)
	for _, occurrence := range occurrences {
		keys = append(keys, occurrence.key)
	}
	for _, problem := range problems {
		if problem.code == "unknown" {
			unknown = append(unknown, problem.key)
		}
	}
	return keys, unknown
}

func parseDirectiveGroups(groups []*ast.CommentGroup) ([]directiveOccurrence, []directiveProblem) {
	var occurrences []directiveOccurrence
	var problems []directiveProblem
	for _, group := range groups {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			parsed, parsedProblems, _ := parseDirectiveComment(comment)
			occurrences = append(occurrences, parsed...)
			problems = append(problems, parsedProblems...)
		}
	}

	counts := make(map[string]int)
	for _, occurrence := range occurrences {
		if occurrence.valid {
			counts[occurrence.key]++
		}
	}
	for key, count := range counts {
		if count > 1 {
			problems = append(problems, directiveProblem{
				code:    "duplicate",
				key:     key,
				message: fmt.Sprintf("duplicate goplint directive %q", key),
			})
		}
	}
	if counts["constant-only"] > 0 && counts["mutable"] > 0 {
		problems = append(problems, directiveProblem{
			code:    "conflict",
			key:     "constant-only",
			related: "mutable",
			message: `conflicting goplint directives "constant-only" and "mutable"`,
		})
	}
	return occurrences, normalizeDirectiveProblems(problems)
}

func parseDirectiveComment(comment *ast.Comment) ([]directiveOccurrence, []directiveProblem, bool) {
	if comment == nil {
		return nil, nil, false
	}
	content := directiveCommentContent(comment.Text)
	if rest, ok := strings.CutPrefix(content, "nolint:"); ok {
		rest = trimDirectiveReason(rest)
		var occurrences []directiveOccurrence
		for linter := range strings.SplitSeq(rest, ",") {
			if strings.TrimSpace(linter) == "goplint" {
				occurrences = append(occurrences, directiveOccurrence{
					key:     "ignore",
					valid:   true,
					comment: comment,
				})
			}
		}
		return occurrences, nil, len(occurrences) > 0
	}

	valueText, matched := directivePayload(content)
	if !matched {
		return nil, nil, false
	}
	valueText = trimDirectiveReason(valueText)
	if strings.TrimSpace(valueText) == "" {
		return nil, []directiveProblem{{
			code:    "empty",
			message: "goplint directive must name at least one directive key",
		}}, true
	}

	var occurrences []directiveOccurrence
	var problems []directiveProblem
	for part := range strings.SplitSeq(valueText, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			problems = append(problems, directiveProblem{
				code:    "empty-entry",
				message: "goplint directive contains an empty directive entry",
			})
			continue
		}
		key, value, hasValue := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		definition, known := directiveDefinitionFor(key)
		if !known {
			problems = append(problems, directiveProblem{
				code:    "unknown",
				key:     key,
				message: fmt.Sprintf("unknown directive key %q in goplint directive", key),
			})
			continue
		}

		occurrence := directiveOccurrence{
			key:      key,
			value:    value,
			hasValue: hasValue,
			valid:    true,
			comment:  comment,
		}
		switch {
		case definition.requiresValue && (!hasValue || value == ""):
			occurrence.valid = false
			problems = append(problems, directiveProblem{
				code:    "missing-value",
				key:     key,
				message: fmt.Sprintf("directive %q requires a non-empty value", key),
			})
		case definition.requiresValue && !definition.validateValue(value):
			occurrence.valid = false
			problems = append(problems, directiveProblem{
				code:    "invalid-value",
				key:     key,
				related: value,
				message: fmt.Sprintf("directive %q has invalid value %q", key, value),
			})
		case !definition.requiresValue && hasValue:
			occurrence.valid = false
			problems = append(problems, directiveProblem{
				code:    "unexpected-value",
				key:     key,
				related: value,
				message: fmt.Sprintf("directive %q does not accept a value", key),
			})
		}
		occurrences = append(occurrences, occurrence)
	}
	return occurrences, problems, true
}

func directiveCommentContent(text string) string {
	content := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(content, "//"):
		content = strings.TrimPrefix(content, "//")
	case strings.HasPrefix(content, "/*") && strings.HasSuffix(content, "*/"):
		content = strings.TrimPrefix(content, "/*")
		content = strings.TrimSuffix(content, "*/")
	}
	return strings.TrimSpace(content)
}

func directivePayload(content string) (string, bool) {
	for _, prefix := range []string{"goplint:", "plint:"} {
		if payload, ok := strings.CutPrefix(content, prefix); ok {
			return payload, true
		}
	}
	return "", false
}

func trimDirectiveReason(value string) string {
	if value, _, found := strings.Cut(value, " --"); found {
		return value
	}
	return value
}

func normalizeDirectiveProblems(problems []directiveProblem) []directiveProblem {
	unique := make(map[string]directiveProblem, len(problems))
	for _, problem := range problems {
		identity := strings.Join([]string{problem.code, problem.key, problem.related}, "\x00")
		unique[identity] = problem
	}
	result := make([]directiveProblem, 0, len(unique))
	for _, problem := range unique {
		result = append(result, problem)
	}
	sort.Slice(result, func(left, right int) bool {
		leftKey := strings.Join([]string{result[left].code, result[left].key, result[left].related}, "\x00")
		rightKey := strings.Join([]string{result[right].code, result[right].key, result[right].related}, "\x00")
		return leftKey < rightKey
	})
	return result
}

func inspectDirectivesInFile(pass *analysis.Pass, file *ast.File) {
	if pass == nil || file == nil {
		return
	}
	sites := collectDirectiveSites(pass, file)
	diagnostics := make([]directiveDiagnostic, 0)
	seen := make(map[string]struct{})
	for _, site := range sites {
		occurrences, problems := parseDirectiveGroups(site.groups)
		for _, occurrence := range occurrences {
			if !occurrence.valid {
				continue
			}
			definition, known := directiveDefinitionFor(occurrence.key)
			if !known {
				continue
			}
			if !directiveAllowedAt(definition, site.attachment) {
				problems = append(problems, directiveProblem{
					code:    "misplaced",
					key:     occurrence.key,
					related: string(site.attachment),
					message: fmt.Sprintf(
						"directive %q is not allowed on %s documentation",
						occurrence.key,
						site.attachment,
					),
				})
				continue
			}
			if occurrence.key == "nonzero" && site.attachment == directiveAttachmentMethod && site.name != validateMethodName {
				problems = append(problems, directiveProblem{
					code:    "misplaced-method",
					key:     occurrence.key,
					related: site.name,
					message: `directive "nonzero" is only allowed on a method named Validate`,
				})
			}
			if occurrence.key == "validate-all" && site.attachment == directiveAttachmentMethod && site.name != "Validate" {
				problems = append(problems, directiveProblem{
					code:    "misplaced-method",
					key:     occurrence.key,
					related: site.name,
					message: `directive "validate-all" is only allowed on a method named Validate`,
				})
			}
		}
		problems = normalizeDirectiveProblems(problems)
		if len(problems) == 0 {
			continue
		}

		pos := directiveSitePosition(site)
		message := directiveSiteProblemMessage(site, problems)
		dedupeKey := fmt.Sprintf("%d\x00%s", pos, message)
		if _, duplicate := seen[dedupeKey]; duplicate {
			continue
		}
		seen[dedupeKey] = struct{}{}
		identities := make([]string, 0, len(problems))
		for _, problem := range problems {
			identities = append(identities, strings.Join([]string{problem.code, problem.key, problem.related}, ":"))
		}
		diagnostics = append(diagnostics, directiveDiagnostic{
			pos: pos,
			findingID: PackageScopedFindingID(
				pass,
				CategoryUnknownDirective,
				"directive",
				directiveSiteSemanticKey(pass, site),
				strings.Join(identities, ","),
			),
			message: message,
		})
	}

	sort.Slice(diagnostics, func(left, right int) bool {
		if diagnostics[left].pos != diagnostics[right].pos {
			return diagnostics[left].pos < diagnostics[right].pos
		}
		return diagnostics[left].message < diagnostics[right].message
	})
	for _, diagnostic := range diagnostics {
		reportDiagnostic(pass, diagnostic.pos, CategoryUnknownDirective, diagnostic.findingID, diagnostic.message)
	}
}

func directiveAllowedAt(definition directiveDefinition, attachment directiveAttachment) bool {
	return slices.Contains(definition.attachments, attachment)
}

func directiveSitePosition(site directiveSite) token.Pos {
	var result token.Pos
	for _, group := range site.groups {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			_, _, matched := parseDirectiveComment(comment)
			if !matched {
				continue
			}
			if !result.IsValid() || comment.Pos() < result {
				result = comment.Pos()
			}
		}
	}
	if !result.IsValid() && site.anchor != nil {
		return site.anchor.Pos()
	}
	return result
}

func directiveSiteProblemMessage(site directiveSite, problems []directiveProblem) string {
	if len(problems) == 1 {
		return problems[0].message
	}
	messages := make([]string, 0, len(problems))
	for _, problem := range problems {
		messages = append(messages, problem.message)
	}
	return fmt.Sprintf(
		"invalid goplint directives on %s documentation: %s",
		site.attachment,
		strings.Join(messages, "; "),
	)
}

func directiveSiteSemanticKey(pass *analysis.Pass, site directiveSite) string {
	if site.anchor == nil {
		return "directive-site:" + string(site.attachment)
	}
	if file, ok := site.anchor.(*ast.File); ok {
		return fileDirectiveSiteSemanticKey(pass, file, site)
	}
	return semanticNodeKey(pass, site.anchor.Pos())
}

func fileDirectiveSiteSemanticKey(pass *analysis.Pass, file *ast.File, site directiveSite) string {
	filename := "unknown.go"
	if pass != nil && pass.Fset != nil {
		filename = filepath.Base(pass.Fset.Position(file.Pos()).Filename)
	}
	text := directiveSiteText(site.groups)
	digest := sha256.Sum256([]byte(text))
	ordinal := 0
	if len(site.groups) > 0 {
		target := site.groups[0]
		for _, group := range file.Comments {
			if group == target {
				break
			}
			if directiveSiteText([]*ast.CommentGroup{group}) == text {
				ordinal++
			}
		}
	}
	return strings.Join([]string{
		"directive-site",
		string(site.attachment),
		filename,
		hex.EncodeToString(digest[:]),
		strconv.Itoa(ordinal),
	}, ":")
}

func directiveSiteText(groups []*ast.CommentGroup) string {
	var values []string
	for _, group := range groups {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			if _, _, matched := parseDirectiveComment(comment); matched {
				values = append(values, directiveCommentContent(comment.Text))
			}
		}
	}
	return strings.Join(values, "\n")
}

func collectDirectiveSites(pass *analysis.Pass, file *ast.File) []directiveSite {
	parents := buildParentMap(file)
	assigned := make(map[*ast.CommentGroup]struct{})
	var sites []directiveSite
	addSite := func(attachment directiveAttachment, name string, anchor ast.Node, groups ...*ast.CommentGroup) {
		filtered := make([]*ast.CommentGroup, 0, len(groups))
		seenGroups := make(map[*ast.CommentGroup]struct{}, len(groups))
		for _, group := range groups {
			if group == nil {
				continue
			}
			if _, duplicate := seenGroups[group]; duplicate {
				continue
			}
			seenGroups[group] = struct{}{}
			filtered = append(filtered, group)
			assigned[group] = struct{}{}
		}
		if len(filtered) == 0 {
			return
		}
		for index := range sites {
			if sites[index].attachment != attachment || sites[index].anchor != anchor {
				continue
			}
			for _, group := range filtered {
				if !slices.Contains(sites[index].groups, group) {
					sites[index].groups = append(sites[index].groups, group)
				}
			}
			return
		}
		sites = append(sites, directiveSite{
			attachment: attachment,
			name:       name,
			groups:     filtered,
			anchor:     anchor,
		})
	}

	addSite(directiveAttachmentFile, "", file, file.Doc)
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.GenDecl:
			if typed.Tok == token.TYPE {
				for _, spec := range typed.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					addSite(directiveAttachmentType, typeSpec.Name.Name, typeSpec, typed.Doc, typeSpec.Doc)
					addSite(directiveAttachmentDeclaration, typeSpec.Name.Name, typeSpec, typeSpec.Comment)
				}
				return true
			}
			for _, spec := range typed.Specs {
				switch declaration := spec.(type) {
				case *ast.ValueSpec:
					addSite(directiveAttachmentDeclaration, "", declaration, typed.Doc, declaration.Doc, declaration.Comment)
				case *ast.ImportSpec:
					addSite(directiveAttachmentDeclaration, "", declaration, typed.Doc, declaration.Doc, declaration.Comment)
				}
			}
		case *ast.FuncDecl:
			attachment := directiveAttachmentFunction
			if typed.Recv != nil {
				attachment = directiveAttachmentMethod
			}
			addSite(attachment, typed.Name.Name, typed, typed.Doc)
		case *ast.Field:
			attachment := directiveFieldAttachment(parents, typed)
			addSite(attachment, directiveFieldName(typed), typed, typed.Doc, typed.Comment)
		}
		return true
	})

	for _, group := range file.Comments {
		if _, known := assigned[group]; known {
			continue
		}
		attachment, anchor := looseDirectiveAttachment(pass, file, group)
		addSite(attachment, directiveAnchorName(anchor), anchor, group)
	}
	return sites
}

func directiveAnchorName(anchor ast.Node) string {
	switch typed := anchor.(type) {
	case *ast.FuncDecl:
		return typed.Name.Name
	case *ast.TypeSpec:
		return typed.Name.Name
	case *ast.Field:
		return directiveFieldName(typed)
	default:
		return ""
	}
}

func directiveFieldAttachment(parents map[ast.Node]ast.Node, field *ast.Field) directiveAttachment {
	fieldList, ok := parents[field].(*ast.FieldList)
	if !ok {
		return directiveAttachmentField
	}
	if _, ok := parents[fieldList].(*ast.FuncType); ok {
		return directiveAttachmentParameter
	}
	return directiveAttachmentField
}

func directiveFieldName(field *ast.Field) string {
	if field == nil || len(field.Names) == 0 {
		return ""
	}
	return field.Names[0].Name
}

func looseDirectiveAttachment(
	pass *analysis.Pass,
	file *ast.File,
	group *ast.CommentGroup,
) (directiveAttachment, ast.Node) {
	var signatureFunction *ast.FuncDecl
	var enclosingFunction *ast.FuncDecl
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok {
			return true
		}
		signatureEnd := function.End()
		if function.Body != nil {
			signatureEnd = function.Body.Lbrace
		}
		if group.Pos() >= function.Type.Pos() && group.End() < signatureEnd {
			signatureFunction = function
			return false
		}
		if function.Body == nil {
			return true
		}
		if group.Pos() > function.Body.Lbrace && group.End() < function.Body.Rbrace {
			enclosingFunction = function
			return false
		}
		return true
	})
	if signatureFunction != nil {
		if field := nearestDirectiveField(signatureFunction.Type, group); field != nil {
			return directiveAttachmentParameter, field
		}
		return directiveAttachmentParameter, signatureFunction
	}
	if enclosingFunction == nil {
		if group.Pos() < file.Package {
			return directiveAttachmentFile, file
		}
		if attachment, anchor, ok := followingLooseDirectiveSite(pass, file, group); ok {
			return attachment, anchor
		}
		return directiveAttachmentFile, file
	}
	if statement := nearestDirectiveStatement(pass, enclosingFunction.Body, group); statement != nil {
		return directiveAttachmentStatement, statement
	}
	return directiveAttachmentStatement, enclosingFunction
}

func followingLooseDirectiveSite(
	pass *analysis.Pass,
	file *ast.File,
	group *ast.CommentGroup,
) (directiveAttachment, ast.Node, bool) {
	if pass == nil || pass.Fset == nil || file == nil || group == nil {
		return "", nil, false
	}
	commentLine := pass.Fset.Position(group.End()).Line
	var bestAttachment directiveAttachment
	var bestAnchor ast.Node
	bestDistance := int(^uint(0) >> 1)
	consider := func(attachment directiveAttachment, anchor ast.Node) {
		if anchor == nil || anchor.Pos() <= group.End() {
			return
		}
		line := pass.Fset.Position(anchor.Pos()).Line
		if line < commentLine || line-commentLine > 8 {
			return
		}
		distance := int(anchor.Pos() - group.End())
		if distance < bestDistance {
			bestAttachment = attachment
			bestAnchor = anchor
			bestDistance = distance
		}
	}
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			attachment := directiveAttachmentFunction
			if typed.Recv != nil {
				attachment = directiveAttachmentMethod
			}
			consider(attachment, typed)
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch typedSpec := spec.(type) {
				case *ast.TypeSpec:
					consider(directiveAttachmentType, typedSpec)
				case *ast.ValueSpec:
					consider(directiveAttachmentDeclaration, typedSpec)
				case *ast.ImportSpec:
					consider(directiveAttachmentDeclaration, typedSpec)
				}
			}
		}
	}
	return bestAttachment, bestAnchor, bestAnchor != nil
}

func nearestDirectiveField(functionType *ast.FuncType, group *ast.CommentGroup) *ast.Field {
	if functionType == nil || group == nil {
		return nil
	}
	var nearest *ast.Field
	nearestDistance := int(^uint(0) >> 1)
	ast.Inspect(functionType, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if !ok || field.Pos() < group.End() {
			return true
		}
		distance := int(field.Pos() - group.End())
		if distance < nearestDistance {
			nearest = field
			nearestDistance = distance
		}
		return true
	})
	return nearest
}

func nearestDirectiveStatement(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	group *ast.CommentGroup,
) ast.Stmt {
	if pass == nil || pass.Fset == nil || body == nil || group == nil {
		return nil
	}
	commentLine := pass.Fset.Position(group.End()).Line
	var containing ast.Stmt
	containingSpan := int(^uint(0) >> 1)
	var following ast.Stmt
	followingDistance := int(^uint(0) >> 1)
	var preceding ast.Stmt
	precedingDistance := int(^uint(0) >> 1)
	ast.Inspect(body, func(node ast.Node) bool {
		statement, ok := node.(ast.Stmt)
		if !ok {
			return true
		}
		_, isBlock := statement.(*ast.BlockStmt)
		if !isBlock && group.Pos() >= statement.Pos() && group.End() <= statement.End() {
			span := int(statement.End() - statement.Pos())
			if span < containingSpan {
				containing = statement
				containingSpan = span
			}
		}
		statementLine := pass.Fset.Position(statement.Pos()).Line
		if statementLine == commentLine || statementLine == commentLine+1 {
			distance := int(statement.Pos() - group.End())
			if distance >= 0 && distance < followingDistance {
				following = statement
				followingDistance = distance
			}
		}
		statementEndLine := pass.Fset.Position(statement.End()).Line
		if !isBlock && statementEndLine == commentLine {
			distance := int(group.Pos() - statement.End())
			if distance >= 0 && distance < precedingDistance {
				preceding = statement
				precedingDistance = distance
			}
		}
		return true
	})
	if following != nil {
		return following
	}
	if preceding != nil {
		return preceding
	}
	return containing
}
