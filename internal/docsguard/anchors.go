// SPDX-License-Identifier: MPL-2.0

package docsguard

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type (
	// artifactIndex is the executable-artifact anchor registry: declared Make
	// targets, soundness manifest identifiers, and Go identifiers occurring in
	// the tools/goplint sources.
	artifactIndex struct {
		root                string
		makeTargets         map[string]struct{}
		manifestIdentifiers map[string]struct{}
		goIdentifiers       map[string]struct{}
	}

	// gateManifest is the minimal local shape of soundness-gate.v1.json.
	gateManifest struct {
		Profiles []gateProfile `json:"profiles"`
		Subgates []gateSubgate `json:"subgates"`
	}

	// gateProfile is one aggregate profile with its bound subgates.
	gateProfile struct {
		ID         string   `json:"id"`
		SubgateIDs []string `json:"subgate_ids"`
	}

	// gateSubgate is one blocking subgate identifier.
	gateSubgate struct {
		ID string `json:"id"`
	}

	// evidenceRegistry is the minimal local shape of semantic-evidence.v2.json.
	evidenceRegistry struct {
		Registrations []evidenceRegistration `json:"registrations"`
	}

	// evidenceRegistration is one typed category/layer evidence registration.
	evidenceRegistration struct {
		ID         string              `json:"id"`
		Category   string              `json:"category"`
		Layer      string              `json:"layer"`
		FeatureID  string              `json:"feature_id"`
		ProducerID string              `json:"producer_id"`
		TestID     string              `json:"test_id"`
		Boundary   string              `json:"boundary"`
		Expected   evidenceExpectation `json:"expected"`
	}

	// evidenceExpectation carries the observation identifiers a registration
	// requires from its executing test.
	evidenceExpectation struct {
		Outcome            string   `json:"outcome"`
		RequiredStages     []string `json:"required_stages"`
		RequiredProperties []string `json:"required_properties"`
		RequiredDimensions []string `json:"required_dimensions"`
	}
)

// newArtifactIndex loads every anchor source once: the repository Makefile,
// the soundness-gate manifest, the semantic evidence registry, and one plain
// text identifier scan over the tools/goplint Go sources.
func newArtifactIndex(root string) (*artifactIndex, error) {
	makeTargets, err := loadMakeTargets(root)
	if err != nil {
		return nil, err
	}
	manifestIdentifiers := map[string]struct{}{}
	if err := loadGateIdentifiers(filepath.Join(root, filepath.FromSlash(gateManifestRelPath)), manifestIdentifiers); err != nil {
		return nil, err
	}
	if err := loadEvidenceIdentifiers(filepath.Join(root, filepath.FromSlash(evidenceRegistryRelPath)), manifestIdentifiers); err != nil {
		return nil, err
	}
	goIdentifiers, err := loadGoIdentifiers(filepath.Join(root, filepath.FromSlash(goplintModuleDir)))
	if err != nil {
		return nil, err
	}
	return &artifactIndex{
		root:                root,
		makeTargets:         makeTargets,
		manifestIdentifiers: manifestIdentifiers,
		goIdentifiers:       goIdentifiers,
	}, nil
}

// hasMakeTarget reports whether the repository Makefile declares the target.
func (index *artifactIndex) hasMakeTarget(target string) bool {
	_, ok := index.makeTargets[target]
	return ok
}

// hasManifestIdentifier reports whether the reference is a known soundness
// profile, subgate, registration, test, or observation identifier.
func (index *artifactIndex) hasManifestIdentifier(reference string) bool {
	_, ok := index.manifestIdentifiers[reference]
	return ok
}

// hasGoIdentifierPath reports whether every dot-separated segment of the
// reference is identifier-shaped and occurs as a whole word in the tools/goplint
// Go sources (for example `protocoloracle.Program` or `semanticCatalog`).
func (index *artifactIndex) hasGoIdentifierPath(reference string) bool {
	segments := strings.Split(reference, ".")
	for _, segment := range segments {
		if !wholeIdentifierPattern.MatchString(segment) {
			return false
		}
		if _, ok := index.goIdentifiers[segment]; !ok {
			return false
		}
	}
	return len(segments) > 0
}

// hasGoIdentifierWord reports whether a single identifier-shaped word occurs
// in the tools/goplint Go sources.
func (index *artifactIndex) hasGoIdentifierWord(word string) bool {
	_, ok := index.goIdentifiers[word]
	return ok
}

// resolveRepoPath reports whether the normalized reference exists on disk,
// resolving against the repository root, the tools/goplint module directory,
// and the referencing document's own directory.
func (index *artifactIndex) resolveRepoPath(docDir, normalized string) bool {
	return index.existsUnderAny(normalized, ".", goplintModuleDir, docDir)
}

// resolveClaimPath reports whether a claim-row reference exists on disk. Claim
// rows additionally resolve slash-less manifest file names against the spec
// directory.
func (index *artifactIndex) resolveClaimPath(docDir, normalized string) bool {
	return index.existsUnderAny(normalized, ".", goplintModuleDir, specDirRelPath, docDir)
}

// isPackageSymbolReference reports whether an unresolved path-shaped reference is
// really a Go package-qualified symbol such as `pkg/types.FilesystemPath`:
// stripping a trailing `.ExportedName` leaves an existing directory.
func (index *artifactIndex) isPackageSymbolReference(docDir, normalized string) bool {
	trimmed := packageSymbolPattern.ReplaceAllString(normalized, "")
	if trimmed == normalized {
		return false
	}
	return index.existsUnderAny(trimmed, ".", goplintModuleDir, docDir)
}

// existsUnderAny reports whether the slash-separated relative reference exists
// under any of the given repository-relative base directories.
func (index *artifactIndex) existsUnderAny(reference string, bases ...string) bool {
	for _, base := range bases {
		candidate := filepath.Join(index.root, filepath.FromSlash(base), filepath.FromSlash(reference))
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

// loadMakeTargets parses declared targets from the repository root Makefile.
// A declaration is a line-leading `name:` that is not a `:=` variable
// assignment; `.PHONY` lines never declare targets.
func loadMakeTargets(root string) (map[string]struct{}, error) {
	makefilePath := filepath.Join(root, "Makefile")
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		return nil, fmt.Errorf("read repository Makefile %s: %w", makefilePath, err)
	}
	targets := map[string]struct{}{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if match := makeDeclarationPattern.FindStringSubmatch(line); match != nil {
			targets[match[1]] = struct{}{}
		}
	}
	return targets, nil
}

// loadGateIdentifiers collects profile and subgate identifiers from the
// aggregate soundness-gate manifest.
func loadGateIdentifiers(manifestPath string, identifiers map[string]struct{}) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read soundness-gate manifest %s: %w", manifestPath, err)
	}
	manifest := gateManifest{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse soundness-gate manifest %s: %w", manifestPath, err)
	}
	for _, profile := range manifest.Profiles {
		addIdentifier(identifiers, profile.ID)
		for _, subgateID := range profile.SubgateIDs {
			addIdentifier(identifiers, subgateID)
		}
	}
	for _, subgate := range manifest.Subgates {
		addIdentifier(identifiers, subgate.ID)
	}
	return nil
}

// loadEvidenceIdentifiers collects registration, test, and observation
// identifiers from the semantic evidence registry.
func loadEvidenceIdentifiers(registryPath string, identifiers map[string]struct{}) error {
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return fmt.Errorf("read semantic evidence registry %s: %w", registryPath, err)
	}
	registry := evidenceRegistry{}
	if err := json.Unmarshal(data, &registry); err != nil {
		return fmt.Errorf("parse semantic evidence registry %s: %w", registryPath, err)
	}
	for _, registration := range registry.Registrations {
		addIdentifier(identifiers, registration.ID)
		addIdentifier(identifiers, registration.Category)
		addIdentifier(identifiers, registration.Layer)
		addIdentifier(identifiers, registration.FeatureID)
		addIdentifier(identifiers, registration.ProducerID)
		addIdentifier(identifiers, registration.TestID)
		addIdentifier(identifiers, registration.Boundary)
		addIdentifier(identifiers, registration.Expected.Outcome)
		for _, stage := range registration.Expected.RequiredStages {
			addIdentifier(identifiers, stage)
		}
		for _, property := range registration.Expected.RequiredProperties {
			addIdentifier(identifiers, property)
		}
		for _, dimension := range registration.Expected.RequiredDimensions {
			addIdentifier(identifiers, dimension)
		}
	}
	return nil
}

// loadGoIdentifiers scans every *.go file under the module directory once and
// collects identifier-shaped words as plain text; no packages are loaded.
func loadGoIdentifiers(moduleDir string) (map[string]struct{}, error) {
	identifiers := map[string]struct{}{}
	walkErr := filepath.WalkDir(moduleDir, func(sourcePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", sourcePath, err)
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		data, readErr := os.ReadFile(sourcePath) // #nosec G122 -- reads reviewed repository-tracked Go sources under the validated root.
		if readErr != nil {
			return fmt.Errorf("read Go source %s: %w", sourcePath, readErr)
		}
		for _, word := range goIdentifierPattern.FindAllString(string(data), -1) {
			identifiers[word] = struct{}{}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan Go identifiers under %s: %w", moduleDir, walkErr)
	}
	return identifiers, nil
}

// addIdentifier records one non-empty identifier.
func addIdentifier(identifiers map[string]struct{}, identifier string) {
	if identifier != "" {
		identifiers[identifier] = struct{}{}
	}
}
