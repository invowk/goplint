# goplint Semantic Oracles (Phase A)

This index maps CFA-backed categories to fixture symbols that serve as property oracles.

Conventions:

- `must-report`: symbol is expected to produce a finding for the category.
- `must-not-report`: symbol is expected not to produce that category.

## unvalidated-cast

- must-report:
  - `cfa_castvalidation.ValidateInDeadBranch`
  - `cfa_castvalidation.ValidateOnOneBranch`
- must-not-report:
  - `cfa_castvalidation.ValidateOnAllBranches`
  - `cfa_castvalidation.ValidateBeforeUse`

## unvalidated-cast-inconclusive

- must-report:
  - `cfa_cast_inconclusive.Inconclusive`
- must-not-report:
  - `cfa_castvalidation.ValidateBeforeUse`

## use-before-validate-same-block

- must-report:
  - `use_before_validate.UseBeforeValidate`
  - `use_before_validate.UseInFuncArgBeforeValidate`
- must-not-report:
  - `use_before_validate.ValidateBeforeUse`
  - `use_before_validate.NoUseAtAll`

## use-before-validate-cross-block

- must-report:
  - `use_before_validate_cross.CrossBlockUseBeforeValidate`
  - `use_before_validate_cross.CrossBlockUseOnOnePath`
- must-not-report:
  - `use_before_validate_cross.CrossBlockValidateFirst`
  - `use_before_validate_cross.CrossBlockNoUse`

## use-before-validate-inconclusive

- must-report:
  - `use_before_validate_escape.RecursiveCycleConservative`
- must-not-report:
  - `use_before_validate_escape.DelegatedValidationCoversPath`

## missing-constructor-validate

- must-report:
  - `constructorvalidates.NewServer`
  - `constructorvalidates.NewWidget`
- must-not-report:
  - `constructorvalidates.NewConfig`
  - `constructorvalidates.NewSession`

## missing-constructor-validate-inconclusive

- must-report:
  - `constructorvalidates_inconclusive.NewServer`
- must-not-report:
  - `constructorvalidates.NewConfig`

## Historical Miss Fixtures (replay required)

- Historical replay expectations are defined in `semantic-rules.v1.json` under
  `historical_miss_oracles` and are enforced with symbol-level checks
  (`must_report`/`must_not_report`) in addition to category-level checks.
- `castvalidation_nocfa_dead_branch`
- `castvalidation_nocfa_dotimport_compare`
- `castvalidation_nocfa_errors_noncomparison`
- `castvalidation_nocfa_suppression`
- `castvalidation_nocfa_validate_before_cast`
- `constructorvalidates_nocfa_ast`
