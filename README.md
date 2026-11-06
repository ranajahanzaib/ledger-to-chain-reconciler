# Ledger-to-Chain Reconciler

`ledgerchain` independently compares a deposit ledger with canonical ERC-20 Transfer events on one EVM network. It detects unsupported credits and discovers eligible inbound events that lack valid credits, using a separately supplied address ownership registry.

The tool reads CSV or SQLite, checks receipts and canonical block headers through go-ethereum, and produces evidence-linked JSON or terminal reports. Amounts remain exact uint256 decimal strings. No private keys, hosted database, Docker, or commercial API are required.

## Quick start

Prerequisites: Go 1.27.1 or newer. The local blockchain demo also needs Python 3.9+ and [Foundry](https://getfoundry.sh/) (`anvil`, `forge`, `cast`; tested with v1.4.4). Forge downloads Solidity 0.8.30 on its first build.

```sh
git clone https://github.com/ranajahanzaib/ledger-to-chain-reconciler.git
cd ledger-to-chain-reconciler
make build
./bin/ledgerchain version
./bin/ledgerchain validate --config examples/config.yaml \
  --ledger examples/ledger.csv --accounts examples/accounts.csv
make test
make demo
```

`make demo` starts a temporary **localhost-only** Anvil chain, verifies chain ID 31337, deploys the fixture token, emits three transfers in one transaction, and generates real ledger identities. It runs and asserts clean, deliberately inconsistent, SQLite, and repeated audits. It stops Anvil automatically. Inspect `demo/clean-report.json` and `demo/discrepancy-report.json`; generated RPC URLs only work while that temporary process exists. Re-run the demo to regenerate them. This workflow uses Anvil's unlocked development accounts; it contains no production keys.

Example terminal summaries:

```text
complete: 3 entries, 3 verified, 0 findings; discovery 0..6 complete=true
complete: 3 entries, 0 verified, 6 findings; discovery 0..6 complete=true
```

`make integration` adds an actual Anvil snapshot/revert and alternative block history, asserting `REORG_OBSERVED` against stored evidence. Integration tests fail if Foundry is missing; they do not silently skip.

## Audit your ledger

Start from [examples/config.yaml](examples/config.yaml) and replace the chain ID, token contract, RPC URL and explicit discovery bounds. Example addresses are placeholders for **offline validation**, not deployed contracts.

```sh
export LEDGERCHAIN_RPC_URL='https://your-provider.example/your-credential'
./bin/ledgerchain reconcile --config config.yaml \
  --ledger ledger.csv --accounts accounts.csv \
  --state-db history.sqlite --output report.json --fail-on high

./bin/ledgerchain reconcile --config config.yaml \
  --ledger source.sqlite --ledger-type sqlite --accounts accounts.csv \
  --format text --output report.txt
```

RPC precedence: `--rpc-url`, then `LEDGERCHAIN_RPC_URL`, then YAML. Environment variables keep credentials out of process arguments. Reports include a SHA-256 source identifier instead of any endpoint URL components. Provider error text is never forwarded. `--verbose` prints only snapshot/evidence counts. Protect source files and reports: they contain account identities and financial records.

The independent ownership file looks like:

```csv
account_id,deposit_address
customer_001,0x2222222222222222222222222222222222222222
customer_002,0x3333333333333333333333333333333333333333
```

An address can belong to only one account; an account may own several addresses. Never generate this registry solely from the ledger being audited.

A ledger CSV has these columns, including blank optional fields:

```csv
entry_id,account_id,chain_id,token_address,deposit_address,expected_amount_base_units,tx_hash,log_index,credit_status,credited_at_block,recorded_block_hash
expected_001,customer_001,31337,0x1111111111111111111111111111111111111111,0x2222222222222222222222222222222222222222,12500000,,,expected,,
```

For SQLite, supply `ledger_entries` as a table or compatible view; see [examples/schema.sql](examples/schema.sql). Amount storage must be TEXT decimal integers. The demo generates `demo/ledger.sqlite` automatically. The source connection uses `mode=ro`, performs SELECTs only, and never creates schemas, migrations or indexes.

## Behavior and automation

The default deposit model is a positive, successful, canonical Transfer emitted by the configured token to an independently registered address, meeting the configured confirmation count. Zero-value events are decoded but cannot satisfy a positive ledger deposit. Mints are excluded unless explicitly enabled. Chain ID plus token address establishes asset identity; symbols and decimals are display metadata.

Event identity is `chain_id:tx_hash:log_index`, using the RPC's **block-global** log index. When the index is absent, attributes must identify a unique event. Missing transaction identity only produces candidates, never a verified credit. A canonical receipt without an appropriate Transfer is insufficient.

Supported findings include wrong chain/token, reverts, missing or malformed evidence, amount/recipient/account mismatches, duplicates, ambiguous events, current and historical confirmation problems, noncanonical blocks, recorded block mismatches, historical reorgs, unrecorded inbound transfers, and eligible deposits still marked expected. [All codes and severity levels](docs/findings.md) are documented.

Exit status precedence:

| Code | Meaning                                                                                             |
| ---- | --------------------------------------------------------------------------------------------------- |
| 0    | Complete run; no finding at or above `--fail-on`                                                    |
| 1    | Complete run; at least one finding at/above threshold (default `medium`)                            |
| 2    | Invalid input/configuration, wrong RPC network, initial snapshot failure, or output/storage failure |
| 3    | Incomplete or unresolved reconciliation, regardless of severity threshold                           |

An unobserved expected deposit is unresolved and exits 3, without alleging failure or fraud. A known under-confirmed event is a policy finding, not proof of consensus finality. Missing historical credit height is informational; it does not prevent verification of **current** credit eligibility.

JSON output contains ledger and ownership snapshots, sanitized configuration, receipt/event/header evidence, covered and failed scan ranges, expected/observed values, investigation guidance, and configuration/source/ownership fingerprints. Report file replacement is atomic. Without `--output`, the report goes to stdout and the summary to stderr. Use `--format text` for a detailed terminal report.

Optional `--state-db` stores report archives and idempotent event/block observations in its own versioned SQLite database. Use a separate database exclusively for this tool. Previous block assignments are checked again; only a canonical header mismatch supports `REORG_OBSERVED`. A missing receipt alone never proves a reorg. History is local evidence, not an indexer; retention/rotation is operator-managed.

## Configuration

| Setting                               | Default   | Meaning                                          |
| ------------------------------------- | --------- | ------------------------------------------------ |
| `network.chain_id`, `network.rpc_url` | required  | Positive decimal chain ID; HTTP(S) endpoint      |
| `asset.token_address`                 | required  | Exact ERC-20 contract identity                   |
| `asset.symbol`, `asset.decimals`      | empty / 0 | Display metadata; decimals 0–255                 |
| `policy.min_confirmations`            | 3         | 1–1,000,000; `head - inclusion + 1`              |
| `policy.include_mint_transfers`       | false     | Include positive zero-sender events              |
| `discovery.from_block`                | 0         | Inclusive lower bound                            |
| `discovery.to_block`                  | latest    | Inclusive height or `latest` pinned at run start |
| `discovery.chunk_size`                | 500       | Block query size, 1–10,000                       |
| `discovery.max_blocks`                | 100,000   | Inclusive range cap, 1–1,000,000                 |
| `rpc.timeout_seconds`                 | 10        | Each request attempt, 1–300                      |
| `rpc.run_timeout_seconds`             | 120       | Overall deadline, 1–3,600                        |
| `rpc.max_concurrency`                 | 8         | Receipt workers, 1–64                            |
| `rpc.max_retries`                     | 3         | Additional attempts for transient errors, 0–5    |
| `output.format`                       | json      | `json` or `text`                                 |

Receipt requests are deduplicated, historical headers cached, recipient filters batched in groups of 100, and discovery ranges chunked. Retries use bounded exponential backoff with jitter. Workload caps are 100,000 ledger rows, ownership rows, discovered logs, unique receipts, and loaded historical observations per scope; CSV limits are 64 MiB for ledger and 16 MiB for ownership. Exceeding limits fails explicitly. HTTP response bodies are limited to 32 MiB before JSON decoding. These limits are guardrails, not a throughput guarantee.

## Architecture and validation

Packages separate validated domain data, source adapters, read-only RPC, deterministic rules, reporting, history, and CLI orchestration. Broadcasting exists only in `script/demo.py`. See [architecture](docs/architecture.md), [input format](docs/ledger-format.md), [rules](docs/reconciliation-rules.md), and [evidence model](docs/evidence-model.md).

```sh
make check         # vet, unit/RPC tests, formatting
make race          # race-enabled tests
make integration   # real local chain, two-way audit and reorg assertions
make demo          # generates example sources and inspectable reports
make benchmark     # pure rules benchmark; no RPC
```

GitHub Actions runs formatting, vet, normal/race tests, CLI build, pinned Foundry integration and the demo, and uploads demo reports. No secrets are needed. `go.mod`/`go.sum` pin dependency versions. The HTTP RPC tests use local test servers; tests need permission to bind localhost sockets.

## Limits and security

This is a developer audit tool with a bounded event-accounting model, not a certified financial audit or consensus-verifying Ethereum node. It trusts the configured RPC's responses, cross-checks canonical headers, and checks the reference view again at the end. It cannot prove that a provider did not omit logs. Findings need human investigation before any correction.

Native ETH, NFTs, rebasing, transfer-fee/share tokens, bridge settlement, shared custodial deposit addresses, and custom allocation workflows are unsupported. Historical timing comes from optional ledger data and is labelled accordingly. Confirmation counts are not `safe`/`finalized` consensus tags. Reconciliation never signs, broadcasts, changes balances, or modifies the source ledger. See [limitations](docs/limitations.md).

## Contributing

Contributions are welcome. See the [contributing guide](CONTRIBUTING.md) for development and testing instructions, and review the [Code of Conduct](CODE_OF_CONDUCT.md) before participating.

## License

[MIT](LICENSE) © Rana Jahanzaib. Third-party dependencies are covered by their respective licenses, listed in [Third-Party Notices](THIRD_PARTY_NOTICES.md).
