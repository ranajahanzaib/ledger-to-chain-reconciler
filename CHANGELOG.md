# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Read-only ERC-20 ledger reconciliation CLI with CSV and SQLite input.
- Independent ownership validation, canonical receipt/event verification,
  bounded discovery, exact uint256 amounts and two-way findings.
- Evidence-linked JSON/text reports, severity thresholds and explicit incomplete exits.
- Separate versioned SQLite run history and supported historical reorg detection.
- Local Anvil fixture generation, real integration/reorg assertions and CI.
