# Testing and benchmark conditions

`make check` runs vet, the standard Go tests and formatting checks. `make race`
runs the same unit/RPC tests with Go's race detector. No test uses public-chain
RPCs, paid APIs or funded wallets. HTTP adapter tests need localhost sockets.

The deterministic rule suite covers correct deposits, exact large uint256 values,
amount/recipient/account/token/chain mismatches, reverted/noncanonical receipts,
duplicates, multi-event indices, unique fallbacks, ambiguity, excluded mints,
under-confirmation, premature/inconsistent/unknown historical timing, expected
and unrecorded deposits, missing identity and partial evidence. Source tests
exercise row validation, ownership conflict, CSV/SQLite parity, byte-identical
source preservation and missing-database behavior. RPC tests exercise finite
retries, cancellation, per-request timeouts, credential-safe errors, malformed
responses, response-size limits, scan failures, reference-head replacement and
receipt deduplication. History tests cover transaction rollback, idempotent
observations, changed/unknown historical blocks and refusing foreign databases.
CLI tests cover offline validation, help/version, invalid arguments and path
aliases. Report tests verify serialization, severity thresholds and exit precedence.

`make integration` builds a fresh CLI and runs the isolated Python harness. It
starts Anvil, compiles/deploys the token, emits multiple events in one transaction,
and asserts actual findings/evidence for clean, discrepant, repeated and SQLite
audits. It also performs a real snapshot/revert, mines alternative blocks and
asserts stored event canonicality changes produce `REORG_OBSERVED` while missing
receipts remain unresolved. Missing integration tooling is a failure, not a skip.
`make demo` runs the inspectable non-reorg subset and leaves generated sources and
reports in ignored `demo/`; the chain is temporary and is stopped automatically.

`make benchmark` measures the pure deterministic evaluator with 1,000 generated
credited entries and 1,000 prepared canonical Transfer/receipt observations,
explicit event identities, two owners, and no expected discrepancies. Setup is
outside the measured region. It includes matching, claim/consumption tracking,
reverse checks and sorting, without network, SQLite, CLI or file I/O. On a local
Apple M1 Max, darwin/arm64, Go 1.27.1 run, it measured approximately 0.84 ms/op,
922 KB/op and 4,091 allocations/op. This is a reproducible microbenchmark setup,
not a claim about end-to-end provider throughput. Run it on your target workload
and hardware; numbers vary by toolchain and environment.

GitHub Actions installs Go from `go.mod` and Foundry v1.4.4, then runs standard,
race, integration and demo checks. Hosted CI execution must be verified after
pushing; local success is not proof that the hosted workflow has already run.
