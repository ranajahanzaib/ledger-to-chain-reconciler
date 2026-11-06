package reconcile

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/config"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

const token = "0x1111111111111111111111111111111111111111"
const alice = "0x2222222222222222222222222222222222222222"
const bob = "0x3333333333333333333333333333333333333333"
const tx = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const bh = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func fixture() ([]domain.Entry, map[string]string, domain.Evidence, config.Config) {
	var c config.Config
	c.Network.ChainID = "31337"
	c.Asset.Token = token
	c.Policy.MinConfirmations = 3
	idx := uint64(7)
	height := uint64(12)
	entries := []domain.Entry{{EntryID: "one", AccountID: "alice", ChainID: "31337", Token: token, Deposit: alice, Amount: "100", Tx: tx, LogIndex: &idx, Status: "credited", CreditedAt: &height}}
	transfer := domain.Transfer{ID: domain.EventID("31337", tx, 7), ChainID: "31337", Token: token, Tx: tx, Index: 7, Block: 10, BlockHash: bh, From: bob, To: alice, Amount: "100", ReceiptStatus: 1, Confirmations: 3, Canonical: true}
	ev := domain.Evidence{Head: domain.Header{Number: 12, Hash: bh}, Consistent: true, Coverage: domain.Coverage{From: 0, To: 12, Complete: true}, Transfers: []domain.Transfer{transfer}, Receipts: map[string]domain.Observation{tx: {State: "observed", Canonical: true, Receipt: &domain.Receipt{Tx: tx, Block: 10, BlockHash: bh, Status: 1, Transfers: []domain.Transfer{transfer}}}}}
	return entries, map[string]string{alice: "alice", bob: "bob"}, ev, c
}
func has(r Result, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
func TestScenarios(t *testing.T) {
	tests := []struct {
		name, code string
		change     func(*[]domain.Entry, *domain.Evidence, *config.Config)
	}{
		{"correct", "", func(_ *[]domain.Entry, _ *domain.Evidence, _ *config.Config) {}},
		{"amount", "AMOUNT_MISMATCH", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].Amount = "90" }},
		{"recipient", "RECIPIENT_MISMATCH", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) {
			(*e)[0].Deposit = bob
			(*e)[0].AccountID = "bob"
		}},
		{"attribution", "ACCOUNT_ATTRIBUTION_MISMATCH", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].AccountID = "bob" }},
		{"token", "WRONG_TOKEN_CONTRACT", func(_ *[]domain.Entry, v *domain.Evidence, _ *config.Config) {
			v.Receipts[tx].Receipt.Transfers[0].Token = bob
		}},
		{"ledger token", "WRONG_TOKEN_CONTRACT", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].Token = bob }},
		{"reverted", "TRANSACTION_REVERTED", func(_ *[]domain.Entry, v *domain.Evidence, _ *config.Config) { v.Receipts[tx].Receipt.Status = 0 }},
		{"duplicate", "DUPLICATE_CREDIT", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) {
			dup := (*e)[0]
			dup.EntryID = "two"
			*e = append(*e, dup)
		}},
		{"underconfirmed", "INSUFFICIENT_CONFIRMATIONS", func(_ *[]domain.Entry, _ *domain.Evidence, c *config.Config) { c.Policy.MinConfirmations = 4 }},
		{"premature", "PREMATURE_CREDIT", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) {
			n := uint64(10)
			(*e)[0].CreditedAt = &n
		}},
		{"before inclusion", "CREDIT_HEIGHT_INCONSISTENT", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { n := uint64(9); (*e)[0].CreditedAt = &n }},
		{"unknown timing", "HISTORICAL_CONFIRMATION_UNKNOWN", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].CreditedAt = nil }},
		{"unrecorded", "UNRECORDED_ONCHAIN_TRANSFER", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { *e = nil }},
		{"expected", "ELIGIBLE_DEPOSIT_NOT_CREDITED", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].Status = "expected" }},
		{"missing identity", "EXPECTED_DEPOSIT_NOT_OBSERVED", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) {
			(*e)[0].Status = "expected"
			(*e)[0].Tx = ""
			(*e)[0].LogIndex = nil
		}},
		{"missing receipt", "TRANSACTION_NOT_OBSERVED", func(_ *[]domain.Entry, v *domain.Evidence, _ *config.Config) {
			v.Receipts[tx] = domain.Observation{State: "not_observed"}
		}},
		{"wrong chain", "WRONG_CHAIN_ID", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].ChainID = "1" }},
		{"rpc failure", "RPC_EVIDENCE_UNAVAILABLE", func(_ *[]domain.Entry, v *domain.Evidence, _ *config.Config) {
			v.Receipts[tx] = domain.Observation{State: "unavailable"}
		}},
		{"noncanonical", "NONCANONICAL_BLOCK", func(_ *[]domain.Entry, v *domain.Evidence, _ *config.Config) {
			o := v.Receipts[tx]
			o.Canonical = false
			v.Receipts[tx] = o
			v.Transfers = nil
		}},
		{"recorded hash", "RECORDED_BLOCK_MISMATCH", func(e *[]domain.Entry, _ *domain.Evidence, _ *config.Config) { (*e)[0].RecordedHash = tx }},
		{"malformed", "MALFORMED_EVENT_DATA", func(_ *[]domain.Entry, v *domain.Evidence, _ *config.Config) { v.Receipts[tx].Receipt.Malformed = true }},
		{"ambiguous", "AMBIGUOUS_TRANSFER_MATCH", func(e *[]domain.Entry, v *domain.Evidence, _ *config.Config) {
			(*e)[0].LogIndex = nil
			dup := v.Receipts[tx].Receipt.Transfers[0]
			dup.Index = 8
			dup.ID = domain.EventID("31337", tx, 8)
			v.Receipts[tx].Receipt.Transfers = append(v.Receipts[tx].Receipt.Transfers, dup)
			v.Transfers = append(v.Transfers, dup)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, o, v, c := fixture()
			tt.change(&e, &v, &c)
			r := Evaluate(e, o, v, c)
			if tt.code == "" {
				if len(r.Findings) != 0 || r.Verified != 1 || r.Incomplete {
					t.Fatalf("not clean: %+v", r)
				}
			} else if !has(r, tt.code) {
				t.Fatalf("missing %s: %+v", tt.code, r)
			}
			again := Evaluate(e, o, v, c)
			if !reflect.DeepEqual(r, again) {
				t.Fatal("nondeterministic evaluation")
			}
		})
	}
}
func TestMultiTransferAndExactUint256(t *testing.T) {
	e, o, v, c := fixture()
	large := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	e[0].Amount = large
	v.Receipts[tx].Receipt.Transfers[0].Amount = large
	v.Transfers[0].Amount = large
	dup := v.Transfers[0]
	dup.Index = 8
	dup.ID = domain.EventID("31337", tx, 8)
	v.Transfers = append(v.Transfers, dup)
	v.Receipts[tx].Receipt.Transfers = append(v.Receipts[tx].Receipt.Transfers, dup)
	second := e[0]
	second.EntryID = "two"
	idx := uint64(8)
	second.LogIndex = &idx
	e = append(e, second)
	r := Evaluate(e, o, v, c)
	if r.Verified != 2 || len(r.Findings) != 0 {
		t.Fatalf("%+v", r)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"`+large+`"`) {
		t.Fatal("lost integer precision")
	}
}
func TestIncompleteAndMint(t *testing.T) {
	e, o, v, c := fixture()
	v.Coverage.Complete = false
	if !Evaluate(e, o, v, c).Incomplete {
		t.Fatal("partial scan passed")
	}
	v.Transfers[0].From = zero
	e = nil
	if has(Evaluate(e, o, v, c), "UNRECORDED_ONCHAIN_TRANSFER") {
		t.Fatal("mint included")
	}
	c.Policy.IncludeMint = true
	if !has(Evaluate(e, o, v, c), "UNRECORDED_ONCHAIN_TRANSFER") {
		t.Fatal("mint policy ignored")
	}
}
func BenchmarkEvaluate1000(b *testing.B) {
	base, owners, ev, c := fixture()
	entries := make([]domain.Entry, 1000)
	ev.Transfers = nil
	ev.Receipts = map[string]domain.Observation{}
	for i := range entries {
		entries[i] = base[0]
		entries[i].EntryID = fmt.Sprint(i)
		entries[i].Tx = fmt.Sprintf("0x%064x", i+1)
		index := uint64(i)
		entries[i].LogIndex = &index
		transfer := domain.Transfer{ID: domain.EventID("31337", entries[i].Tx, index), ChainID: "31337", Token: token, Tx: entries[i].Tx, Index: index, Block: 10, BlockHash: bh, From: bob, To: alice, Amount: "100", ReceiptStatus: 1, Confirmations: 3, Canonical: true}
		ev.Transfers = append(ev.Transfers, transfer)
		ev.Receipts[entries[i].Tx] = domain.Observation{State: "observed", Canonical: true, Receipt: &domain.Receipt{Tx: entries[i].Tx, Block: 10, BlockHash: bh, Status: 1, Transfers: []domain.Transfer{transfer}}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := Evaluate(entries, owners, ev, c)
		if r.Verified != len(entries) || len(r.Findings) != 0 {
			b.Fatal("invalid benchmark setup")
		}
	}
}

func TestUniqueFallbackAndNoSilentAssignment(t *testing.T) {
	e, o, v, c := fixture()
	e[0].LogIndex = nil
	other := v.Receipts[tx].Receipt.Transfers[0]
	other.Index = 8
	other.ID = domain.EventID("31337", tx, 8)
	other.To = bob
	other.Amount = "200"
	v.Receipts[tx].Receipt.Transfers = append(v.Receipts[tx].Receipt.Transfers, other)
	v.Transfers = append(v.Transfers, other)
	r := Evaluate(e, o, v, c)
	if r.Verified != 1 || has(r, "AMBIGUOUS_TRANSFER_MATCH") || !has(r, "UNRECORDED_ONCHAIN_TRANSFER") {
		t.Fatal(r)
	}
	e[0].Tx = ""
	r = Evaluate(e, o, v, c)
	if r.Verified != 0 || !r.Incomplete || !has(r, "TRANSACTION_NOT_OBSERVED") {
		t.Fatal("attribute-only match verified", r)
	}
}
