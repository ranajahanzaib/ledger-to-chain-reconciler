// Package domain defines exact, JSON-safe audit records shared by all adapters.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var decimal = regexp.MustCompile(`^[0-9]+$`)
var address = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var hash = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

func Address(s string) (string, error) {
	if !address.MatchString(s) {
		return "", fmt.Errorf("invalid Ethereum address %q", s)
	}
	return strings.ToLower(s), nil
}
func Hash(s string) (string, error) {
	if !hash.MatchString(s) {
		return "", fmt.Errorf("invalid transaction/block hash %q", s)
	}
	return strings.ToLower(s), nil
}
func Amount(s string, positive bool) (string, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !decimal.MatchString(s) || !ok || n.BitLen() > 256 || (positive && n.Sign() == 0) {
		return "", fmt.Errorf("invalid uint256 base-unit amount %q", s)
	}
	return n.String(), nil
}
func ChainID(s string) (string, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !decimal.MatchString(s) || !ok || n.Sign() <= 0 || n.BitLen() > 256 {
		return "", fmt.Errorf("invalid chain ID %q", s)
	}
	return n.String(), nil
}
func Fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func EventID(chain, tx string, index uint64) string { return fmt.Sprintf("%s:%s:%d", chain, tx, index) }

type Entry struct {
	EntryID      string  `json:"entry_id"`
	AccountID    string  `json:"account_id"`
	ChainID      string  `json:"chain_id"`
	Token        string  `json:"token_address"`
	Deposit      string  `json:"deposit_address"`
	Amount       string  `json:"expected_amount_base_units"`
	Tx           string  `json:"tx_hash,omitempty"`
	LogIndex     *uint64 `json:"log_index,omitempty"`
	Status       string  `json:"credit_status"`
	CreditedAt   *uint64 `json:"credited_at_block,omitempty"`
	RecordedHash string  `json:"recorded_block_hash,omitempty"`
	Source       string  `json:"source"`
}
type Header struct {
	Number     uint64 `json:"number"`
	Hash       string `json:"hash"`
	ParentHash string `json:"parent_hash"`
}
type Transfer struct {
	ID               string `json:"event_id"`
	ChainID          string `json:"chain_id"`
	Token            string `json:"token_address"`
	Tx               string `json:"tx_hash"`
	Index            uint64 `json:"log_index"`
	Block            uint64 `json:"block_number"`
	BlockHash        string `json:"block_hash"`
	TransactionIndex uint64 `json:"transaction_index"`
	From             string `json:"from_address"`
	To               string `json:"to_address"`
	Amount           string `json:"amount_base_units"`
	ReceiptStatus    uint64 `json:"receipt_status"`
	Confirmations    uint64 `json:"confirmations"`
	Canonical        bool   `json:"canonical"`
}
type Receipt struct {
	Tx               string     `json:"tx_hash"`
	Block            uint64     `json:"block_number"`
	BlockHash        string     `json:"block_hash"`
	Status           uint64     `json:"status"`
	TransactionIndex uint64     `json:"transaction_index"`
	Transfers        []Transfer `json:"transfers"`
	Malformed        bool       `json:"malformed_transfer_event"`
}
type Observation struct {
	Receipt   *Receipt `json:"receipt,omitempty"`
	State     string   `json:"verification_state"`
	Canonical bool     `json:"canonical"`
}
type Coverage struct {
	From      uint64       `json:"from_block"`
	To        uint64       `json:"to_block"`
	Complete  bool         `json:"complete"`
	Completed []BlockRange `json:"completed_ranges"`
	Failed    []BlockRange `json:"failed_ranges"`
}
type BlockRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}
type Evidence struct {
	Head       Header                 `json:"reference_head"`
	Receipts   map[string]Observation `json:"receipts"`
	Transfers  []Transfer             `json:"transfers"`
	Headers    []Header               `json:"canonical_headers"`
	Coverage   Coverage               `json:"coverage"`
	Issues     []Finding              `json:"issues"`
	Consistent bool                   `json:"consistent"`
}
type Finding struct {
	Code          string   `json:"code"`
	Severity      string   `json:"severity"`
	State         string   `json:"verification_state"`
	Explanation   string   `json:"explanation"`
	Entries       []string `json:"ledger_entries"`
	Events        []string `json:"chain_events"`
	Expected      any      `json:"expected,omitempty"`
	Observed      any      `json:"observed,omitempty"`
	Investigation string   `json:"recommended_investigation"`
	Snapshot      string   `json:"snapshot_identity"`
}
type Summary struct {
	Entries    int            `json:"ledger_entries"`
	Verified   int            `json:"verified_entries"`
	Findings   int            `json:"findings"`
	BySeverity map[string]int `json:"by_severity"`
}
type Run struct {
	Configuration        any               `json:"configuration_snapshot"`
	ID                   string            `json:"run_id"`
	Started              time.Time         `json:"started_at"`
	Completed            time.Time         `json:"completed_at"`
	ConfigFingerprint    string            `json:"configuration_fingerprint"`
	LedgerFingerprint    string            `json:"source_ledger_fingerprint"`
	OwnershipFingerprint string            `json:"ownership_fingerprint"`
	ChainID              string            `json:"chain_id"`
	Token                string            `json:"token_address"`
	RPCSource            string            `json:"rpc_source_identifier"`
	MinConfirmations     uint64            `json:"min_confirmations"`
	Status               string            `json:"execution_status"`
	Summary              Summary           `json:"summary"`
	Findings             []Finding         `json:"findings"`
	Entries              []Entry           `json:"ledger_snapshot"`
	Owners               map[string]string `json:"ownership_snapshot"`
	Evidence             Evidence          `json:"evidence"`
}

var Severities = map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}
var Codes = map[string]string{
	"WRONG_CHAIN_ID": "high", "WRONG_TOKEN_CONTRACT": "high", "TRANSACTION_REVERTED": "critical", "TRANSACTION_NOT_OBSERVED": "low", "TRANSFER_EVENT_NOT_FOUND": "high", "NONCANONICAL_BLOCK": "critical", "RECORDED_BLOCK_MISMATCH": "high", "REORG_OBSERVED": "critical",
	"AMOUNT_MISMATCH": "high", "RECIPIENT_MISMATCH": "high", "ACCOUNT_ATTRIBUTION_MISMATCH": "high", "DUPLICATE_CREDIT": "critical", "AMBIGUOUS_TRANSFER_MATCH": "medium", "UNRECORDED_ONCHAIN_TRANSFER": "medium", "ELIGIBLE_DEPOSIT_NOT_CREDITED": "medium", "EXPECTED_DEPOSIT_NOT_OBSERVED": "info",
	"INSUFFICIENT_CONFIRMATIONS": "medium", "PREMATURE_CREDIT": "medium", "CREDIT_HEIGHT_INCONSISTENT": "medium", "HISTORICAL_CONFIRMATION_UNKNOWN": "info", "RPC_EVIDENCE_UNAVAILABLE": "low", "SCAN_INCOMPLETE": "low", "MALFORMED_EVENT_DATA": "high",
}

func NewFinding(code, state, why string, entries, events []string, expected, observed any) Finding {
	return Finding{Code: code, Severity: Codes[code], State: state, Explanation: why, Entries: entries, Events: events, Expected: expected, Observed: observed, Investigation: "Compare the referenced ledger and ownership records with receipt, event, and canonical-header evidence; investigate manually before changing any financial records."}
}
