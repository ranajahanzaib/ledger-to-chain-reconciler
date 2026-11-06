# Third-party notices

Original Ledger-to-Chain Reconciler code is distributed under the root
[MIT License](LICENSE). That license does not replace the licenses of dependencies.

The direct Go dependencies pinned in `go.mod` are:

| Dependency          | Version | Upstream license sources                                                                                                                                                                                                                                                        |
| ------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| go-ethereum library | v1.16.7 | [LGPL-3.0-or-later library notice](https://github.com/ethereum/go-ethereum/blob/v1.16.7/ethclient/ethclient.go), [COPYING.LESSER](https://github.com/ethereum/go-ethereum/blob/v1.16.7/COPYING.LESSER), [COPYING](https://github.com/ethereum/go-ethereum/blob/v1.16.7/COPYING) |
| modernc.org/sqlite  | v1.40.1 | [BSD-3-Clause LICENSE](https://gitlab.com/cznic/sqlite/-/blob/v1.40.1/LICENSE), [SQLite public-domain notice](https://gitlab.com/cznic/sqlite/-/blob/v1.40.1/SQLITE-LICENSE)                                                                                                    |
| gopkg.in/yaml.v3    | v3.0.1  | [Upstream file-specific MIT and Apache notices](https://github.com/go-yaml/yaml/blob/v3.0.1/LICENSE)                                                                                                                                                                            |

This is a direct-dependency summary, not an exhaustive license bundle for a
compiled distribution. The complete module graph, including transitive
dependencies, is available through `go list -m all`; versions and checksums are
recorded in `go.mod` and `go.sum`. Consult each dependency's license and notices
when preparing distributions. In particular, a binary linked with go-ethereum
includes library code under its own LGPL terms and must not be described as
containing exclusively MIT-licensed components.

Foundry and Solidity are external development/test tools rather than vendored
project code. The test contract declares `SPDX-License-Identifier: MIT`.
