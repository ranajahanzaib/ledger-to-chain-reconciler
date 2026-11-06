# Evidence model

Reports separate origin and conclusion:

| Origin                | Evidence or claim                                                                                                          |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Source ledger         | Entry ID, claimed account/address/amount, event reference, credit status, historical credit height and recorded block hash |
| Independent registry  | Authoritative current address-to-account mapping supplied by the operator                                                  |
| RPC receipt           | Execution status, transaction/block identity, decoded Transfer attributes                                                  |
| Canonical RPC headers | Current block hash cross-check at receipt inclusion height, plus a pinned head and end checks                              |
| Bounded RPC log scan  | Candidate inbound events within declared token/recipient/range scope                                                       |
| Reconciler history    | Prior accepted event/block assignment, run time, reference head and classifications                                        |
| Rules                 | Discrepancy, investigation, informational, unresolved or incomplete findings                                               |

Every finding carries expected/observed values when applicable, ledger/event references, an explanation, investigation guidance and a snapshot identity hashing configuration, source, ownership and reference-head fingerprints. Event references resolve into receipt transfers and the report's transfer evidence. Missing-evidence findings may have no event reference because no trustworthy event exists. Historical reorg findings include the prior transfer and current canonical header directly.

Reports include the sanitized configuration snapshot, loaded ledger/ownership snapshots, reference header, canonical headers, receipt verification states, events and scan coverage. Amounts are decimal strings even above signed-64-bit and JSON-safe-integer limits. Token metadata is not an identity proof.

`observed` describes a provider response, not a financial conclusion. `canonical=true` means its block hash matched the provider's canonical header at that height. `discrepancy` means the recorded claim disagrees with obtained evidence. `investigation` means a policy/accounting follow-up is justified without alleging fraud. `unresolved` means transaction identity/observation/ambiguity prevents a completed judgment. `incomplete` means observation or snapshot coverage failed. `informational` may describe a limit such as unknown historical timing.

A source identifier is a SHA-256 hash of the endpoint rather than a URL; no userinfo, path, query or host is disclosed. The hash is an identifier, not an encryption scheme for low-entropy credentials. Raw provider exceptions are replaced with stable safe errors.

This is provider-backed reconciliation, not cryptographic consensus verification. We do not verify receipt Merkle proofs, consensus signatures or log completeness against a locally verified chain. End-of-run checks reduce mixed-view risk; they cannot prevent a reorg immediately after observation, nor expose a coherently dishonest provider. Confirmation counts and ledger-reported historical timing must not be described as proof of consensus finality or independent historical credit time.
