# Goplint soundness-gate migration performance

This report records migration evidence for the resource-aware goplint
soundness gate. Times are wall-clock unless stated otherwise. A run is
comparable only within the same runner class, toolchain, cache policy, profile,
and topology. Failed or canceled legacy semantic runs remain useful for a
lower-bound critical-path baseline, but are not successful assurance verdicts.

## Measurement identity

| Field | Legacy CI | Legacy local | Optimized local |
|---|---|---|---|
| Commit | Per-run SHA below | `5b43ee24e2bd48dd352e35f09e013c18cd6f09a1` | Parent commit `5b43ee24e2bd48dd352e35f09e013c18cd6f09a1`, bound dirty-tree digest per artifact |
| Go toolchain | `go1.27.0` from `go.mod` via `actions/setup-go@v6` | `go1.27.0 linux/amd64` | `go1.27.0 linux/amd64` |
| Runner class | GitHub `ubuntu-latest`, reviewed `github-ubuntu-x64-4cpu` policy | Fedora 44, Intel Core Ultra 9 285K, 24 effective CPUs, 93 GiB RAM | Same local class, reviewed as `fedora-44-x86_64-24cpu-reviewed` |
| Cache policy | `actions/setup-go` cache enabled; warm-cache comparisons reported separately | Existing local Go build/module caches, no cache clearing between comparable runs | Existing local Go build/module caches; each certification sample reruns analyzer work |
| Legacy topology | Four independent jobs: semantic core, full scan, baseline, exceptions | Serial aggregate runner | Immutable plan, resource-aware local scheduler, shared audit, split algorithm/full-scan certification, weighted build-once race/repeat |

## Legacy GitHub Actions baseline

The three most comparable hosted semantic attempts use the same legacy job
shape and reviewed runner class. They were unable to produce a successful
semantic verdict before the migration, so the durations are conservative lower
bounds for the old critical path.

| Run | Commit | Semantic job | Full scan | Baseline | Exception governance | Outcome |
|---|---|---:|---:|---:|---:|---|
| `29694846559` | `5b43ee24e2bd48dd352e35f09e013c18cd6f09a1` | 86m56s | 5m06s | 5m42s | 9m27s | semantic job failed |
| `29690974616` | `3f15c14e1b8f3479eaac6b791633daa8b8fb8fa5` | 84m37s | 5m50s | 5m13s | canceled at 10m09s | semantic job failed; run canceled |
| `29688476593` | `a2343ba10fe0632b6e501f5909bc759f2849ccc6` | 75m15s | 5m20s | 5m28s | 9m32s | semantic job canceled |
| Median | — | **84m37s** | **5m20s** | **5m28s** | **at least 9m32s** | no successful semantic verdict |

The legacy workflow used `cache: true` for every `actions/setup-go@v6` job.
Separate baseline, full-scan, and exception jobs therefore repeated package
loading and analyzer traversal despite sharing the same checkout identity.

## Legacy local baseline

The frozen legacy worktree is `/tmp/invowk-goplint-baseline-worktree`. Its three
sequential successful retained runs are:

| Run ID | Retained telemetry | Wall / critical path | CPU time | Peak RSS | Process swaps | Outcome |
|---|---|---:|---:|---:|---:|---|
| `run-7cacf2c8c46de8ba3cd8f1043db5259c` | `/tmp/invowk-goplint-legacy-local.4VmFEC/run-1-detached.telemetry.json` | 55m30.126s | 16,218.173s | 4.041 GiB | 0 | passed |
| `run-23032b33aa2b90839ce0e571569488fb` | `/tmp/invowk-goplint-legacy-local-2.8cxjRf/telemetry.json` | 53m06.693s | 15,778.408s | 3.979 GiB | 0 | passed |
| `run-1bb4ca5d131406ca468e38fdbed2fa32` | `/tmp/invowk-goplint-legacy-local-3.yR4NsD/telemetry.json` | 53m16.011s | 15,893.594s | 4.203 GiB | 0 | passed |
| Median | — | **53m16.011s** | **15,893.594s** | **4.041 GiB** | **0** | passed |

The median slowest serial subgates were race/repeat 27m03.624s, certification
9m20.301s, targeted mutation 8m50.630s, baseline 1m39.244s, and full scan
1m39.227s. Independent `/usr/bin/time -v` wall measurements were 55m31.30s,
53m06.84s, and 53m16.16s; all recorded zero process swaps.

## Optimized component evidence

The retained weighted race/repeat run at `/tmp/tmp.b0QMAlDayS` binds workspace
digest `sha256:00b3e05d6f17a4766025aad070e934438fa9349fbb9e2500124344292aadfb03`
and plan ID
`sha256:1c0e476a4edad9957606749d00cfa12af2ee7735ce79a4b1a3160cc68f1996f2`.
It completed in 18m38.54s with 4.976 GiB process peak RSS, zero process swaps,
and exact structured counts:

- 64 of 64 work units passed;
- 491 of 491 census members ran and passed once under race;
- 1,473 of 1,473 normal executions ran and passed, exactly three per member;
- no fail, skip, duplicate, extra, or missing terminal record was accepted;
- the normal and race binaries were each built once and digest-bound.

The consumer smoke run completed in 1m40.42s. Its one fresh full scan measured
99.610s and 4.010 GiB against catastrophic limits of 480s and 8 GiB. Solver and
recursive-tabulation smoke measured 112.643 µs and 81.792 µs. This is smoke,
not certification.

The standalone five-sample certification completed in 9m18.97s under runner
policy `fedora-44-x86_64-24cpu-reviewed`. The optimized plan preserves the same
policy and five samples while scheduling its short algorithmic and long
full-scan phases independently. Selected medians were:

| Workload | Median | Reviewed limit |
|---|---:|---:|
| Canonical solver | 46.423 µs/op | 60 µs/op |
| Recursive tabulation | 61.322 µs/op | 100 µs/op |
| Generated analyzer | 134.177 ms/op | 175 ms/op |
| Repository full scan | 99.670s | 300s |
| Repository full-scan peak RSS | 3.871 GiB | 6 GiB |

The first successful resource-aware semantic run after certification splitting
is retained at `/tmp/invowk-goplint-optimized.EoT4JJ`. It passed all 21 work
units in 29m39.77s, used the full 24-CPU reservation, recorded a 46 GiB maximum
concurrent reservation, reported 6.042 GiB process peak RSS for race/repeat,
and recorded zero process swaps. Its 29m39.243s telemetry wall time is a 46.6
percent reduction from the 55m30.126s serial sample, which is below the required
50 percent threshold. Telemetry isolated the remaining issue: the combined
race/repeat unit queued for 3m11s and then ran for 25m27s, including a 6m24s
supporting-package prefix before the balanced analyzer executor. The final
topology therefore plans supporting-package and analyzer race/repeat work
independently and admits larger reservations first. The accepted measurements
below validate that final topology.

The first passing checkpoint with that split topology is retained at
`/tmp/invowk-goplint-optimized-final.dgBYqe`. It passed all 22 semantic work
units in 22m11.42s (22m10.987s telemetry wall, 21m02.202s critical path), used
the full 24-CPU reservation, recorded a 28 GiB maximum concurrent reservation,
reported 5.943 GiB race/repeat peak RSS, and recorded zero process swaps. This
checkpoint is 60.0 percent faster than the first serial sample. It is not a
final acceptance sample: review immediately afterward found missing
normal/race binary-census comparison, an unbounded worker override,
nondeterministic concurrent-failure selection, and incomplete distributed
artifact/telemetry binding. Acceptance measurements restart after those
fail-closed fixes so all compared reports bind the final exact tree.

## Accepted optimized local measurements

Three sequential accepted runs bind workspace digest
`sha256:cf2391dff63fb3e064a2057b83bfc1177a12248863a7ffb926920be74dec5cc9`
and normalized-report digest
`sha256:9cce6641414a984dde7584e6e4663f5c00a54ba605c8336197f05da0e22fe3a9`.
The comparator confirmed byte-identical normalized findings, observations,
populations, and verdicts across all three runs.

| Run ID | Retained directory | Telemetry wall | Critical path | CPU time | Peak subgate RSS | External wall / RSS | Process swaps |
|---|---|---:|---:|---:|---:|---:|---:|
| `run-5537ed9d698efa4a38704203322b1043` | `/tmp/invowk-goplint-optimized-accepted-1.jtRkVW` | 22m55.626s | 21m40.586s | 17,174.178s | 5.875 GiB | 22m56.03s / 5.875 GiB | 0 |
| `run-20a12fb001c609469642173db0cc38a6` | `/tmp/invowk-goplint-optimized-accepted-2.T3b1Xt` | 22m50.977s | 21m28.513s | 17,223.704s | 5.756 GiB | 22m51.41s / 5.756 GiB | 0 |
| `run-f2a89f214b005b518a1ae727dea68330` | `/tmp/invowk-goplint-optimized-accepted-3.AESA0a` | 22m14.701s | 21m08.564s | 16,574.863s | 4.829 GiB | 22m15.12s / 4.829 GiB | 0 |
| Median | — | **22m50.977s** | **21m28.513s** | **17,174.178s** | **5.756 GiB** | **22m51.41s / 5.756 GiB** | **0** |

Every accepted run discovered and reserved all 24 effective CPUs, reached a
maximum concurrent memory reservation of 28 GiB, passed all 22 semantic work
units, retained exactly 491 race and 1,473 repeat executions, and recorded no
timeout. The external median is 57.09 percent below the 53m16.16s legacy
external median, a 2.33x speedup.

## Exact-tree serial and optimized parity

A final one-pass parity comparison bound both executors to workspace digest
`sha256:7fde66a764c6955eae88d2aa5b3a8785c27222ad84362a2270f2d10e8388d6c3`.
The serial reference retained at
`/tmp/invowk-goplint-exact-parity-serial-final.eVXVHc` passed in 48m56.69s.
The optimized run retained at
`/tmp/invowk-goplint-exact-parity-parallel.0lacrq` passed in 22m17.25s
externally (22m16.828s telemetry wall, 21m05.355s critical path), used
16,725.420s CPU time, reported 4.727 GiB peak subgate RSS, reserved all 24
effective CPUs and at most 28 GiB, and recorded zero process swaps, failures,
or timeouts across all 22 work units.

`soundness-report-compare` produced normalized-report digest
`sha256:ad33854d99b0af8c1f331cd62e739a9799c021741391b1c0100c2786b04231e9`
and confirmed byte-identical normalized findings, observations, populations,
and verdicts. Raw report envelopes intentionally retain distinct run IDs and
therefore have different whole-file digests.

## Current shard balance

The checked timing manifest has 491 entries with three fresh samples each and
129 nested families. The machine check reports zero cases where a nested-case
maximum exceeds its top-level scheduling weight. On the retained 16-shard
plan:

| Allocation | Minimum shard weight | Maximum shard weight | Max/min ratio |
|---|---:|---:|---:|
| Deterministic LPT | 200.690s | 206.970s | 1.031 |
| Legacy modulo | 116.400s | 363.260s | 3.121 |

LPT reduces maximum estimated shard weight by 43.02 percent and removes the
hidden nested-family underweighting found during live migration testing.

## Hosted side-by-side semantic runs

Published-workflow semantic runs execute the optimized plan/matrix/aggregate
lanes and the retained legacy serial reference on the same exact tree, then
compare normalized reports byte for byte. Wall times measure run creation to
optimized-aggregate success for the optimized lane and job start to success
for the serial lane; both lanes use the reviewed warm-cache 4-vCPU
`github-hosted-ubuntu-x64-4cpu-reviewed` class.

| Run | Commit | Optimized lane | Serial reference | Parity | Versus same-run serial | Versus recorded 84m37s lower bound |
|---|---|---:|---:|---|---:|---:|
| `29984291493` | `66da5eac` | 33m36s | failed at 2h13m45s (pre-fix legacy executor ran consumers before the audit producer) | not run | — | 60.3 percent |
| `30031336155` | `c9ce1ac4` | 30m15s | **3h16m41s passed** | **byte-identical** | **84.6 percent** | 64.2 percent |

Run `30031336155` is the first successful hosted serial semantic execution
ever recorded for this gate, so the historical 84m37s failed-run median is
confirmed as a deep lower bound. Six deterministic analyzer race/repeat
groups (15m41s to 28m10s each), the supporting population, targeted mutation,
and both certification phases all fit independent four-CPU workers. The
`soundness-report-compare` parity job confirmed byte-identical normalized
findings, observations, populations, and verdicts between the distributed
lane and the serial reference on the hosted class.

## Consumer feedback objective

The consumer profile executes `repository-audit`, `baseline`, `exceptions`,
`full-scan`, and `performance-smoke`. Every constituent was measured on the
reviewed warm-cache 4-vCPU class within run `30031336155`:

| Constituent | Measured |
|---|---:|
| Plan, routing, and shared repository audit (one job) | 5m19s |
| `baseline` verdict worker (checkout, setup, audit reuse) | 58s |
| `exceptions` verdict worker | 51s |
| `full-scan` verdict worker | 57s |
| Strict no-gap aggregate job | 1m08s |

`performance-smoke` reuses the bound shared-audit wall/RSS measurements
instead of rescanning and adds only microsecond-scale solver and tabulation
samples (1m40s locally on faster hardware, bounded by roughly two to three
minutes on the hosted class including worker setup). Workers run in one
parallel wave after the plan job, so end-to-end consumer feedback is
approximately 8 to 9 minutes, within the 10-minute objective. The maintainer
closed migration measurement after the recorded runs above; no dedicated
consumer-routed run was executed, and the first natural consumer change on
the default branch will confirm the derived figure.

## Acceptance summary

- Byte-for-byte normalized evidence parity between the optimized executor and
  the serial reference is proven locally on one exact tree and on the hosted
  runner class in run `30031336155`.
- Optimized hosted semantic wall times (33m36s, 30m15s) are 60.3 and 64.2
  percent below the recorded 84m37s serial lower bound, and 84.6 percent
  below the only successful hosted serial semantic execution (3h16m41s, same
  exact tree).
- Local optimized medians (22m50.977s telemetry) are 57.09 percent below the
  53m16.16s local serial median.
- The maintainer accepted this evidence and closed side-by-side collection;
  the migration-era legacy serial reference lane, its parity job, and the
  `legacy-serial` executor switch were removed. The `plan-serial` executor
  remains the permanent serial reference and rollback path, and
  `cmd/soundness-report-compare` remains for parity diagnosis.

## Four-tier routed timings (2026-07-24, local 16-thread developer machine)

The `rescope-goplint-soundness-triggers` change split routing into four
reviewed change classes. Each tier was executed once end to end on the same
tree that shipped the change, with the resource-aware parallel executor and
default local resource discovery:

| Tier | Command | Wall time | Scope |
|------|---------|-----------|-------|
| documentation | `make check-goplint-docs` | 0.4s | static docs-guard anchor validation only |
| consumer | `make check-goplint-soundness-consumer` | 2m19s | one shared repository audit plus verdict consumers and smoke |
| harness | `make check-goplint-soundness-harness` | 6m16s | consumer tier plus the goplint module test suite and serial-versus-parallel fixture parity |
| semantic | `make check-goplint-soundness-semantic` | 43m58s | every causal analyzer soundness population (29 subgates, 88 observations) |

The `harness-parity` subgate alone completes in under one second warm and is
bounded by its one-minute fixture budget. Prose-only completion-record drift
is repaired by `make rebind-goplint-clean-tree-evidence` in seconds instead
of the former full regeneration, which re-executed the semantic profile
inside the synthetic tree.
