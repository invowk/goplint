# Bootstrap the standalone goplint repository

Reviewed ledger for the extraction of goplint from `invowk/invowk` into this
repository. Bound by the retained clean-tree completion record.

## 1. Extraction

- [x] 1.1 Extract `tools/goplint` history with `git filter-repo` and rename the module to `github.com/invowk/goplint`.
- [x] 1.2 Re-root every command, script, manifest, and test onto the repository root.

## 2. Bindings

- [x] 2.1 Author the repository Makefile, pre-commit hooks, and CI workflows (lint executor, module tests, fuzz, CodeQL, dependabot).
- [x] 2.2 Author the goplint-repo ownership manifest rules and regenerate the soundness-gate manifest with repository-root working directories.
- [x] 2.3 Re-anchor docs-guard and the governed documentation set to this repository.
- [x] 2.4 Generate the self-scan (dogfood) baseline and exception configuration.

## 3. Analyzer fixes surfaced by dogfooding

- [x] 3.1 Constructor-validates: consult the caller-local proof before delegating a helper-originated returned value; add helper-origin regression fixtures and oracle entries.
- [x] 3.2 Deduplicate function-local type names that collide in stable finding identities.
