// Package reconcile contains deterministic rules over prepared evidence, without I/O.
package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/config"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

const zero = "0x0000000000000000000000000000000000000000"

type Result struct {
	Findings   []domain.Finding
	Verified   int
	Incomplete bool
}

func Eligible(t domain.Transfer, c config.Config, owners map[string]string) bool {
	return t.ChainID == c.Network.ChainID && t.Token == c.Asset.Token && t.Canonical && t.ReceiptStatus == 1 && t.Confirmations >= c.Policy.MinConfirmations && t.Amount != "0" && owners[t.To] != "" && (c.Policy.IncludeMint || t.From != zero)
}

type evaluator struct {
	result   Result
	c        config.Config
	owners   map[string]string
	evidence domain.Evidence
	claims   map[string][]string
	consumed map[string]bool
	verified map[string]bool
}

func Evaluate(entries []domain.Entry, owners map[string]string, ev domain.Evidence, c config.Config) Result {
	x := evaluator{c: c, owners: owners, evidence: ev, claims: map[string][]string{}, consumed: map[string]bool{}, verified: map[string]bool{}}
	x.result.Findings = append([]domain.Finding{}, ev.Issues...)
	x.result.Incomplete = !ev.Consistent || !ev.Coverage.Complete
	sorted := append([]domain.Entry{}, entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].EntryID < sorted[j].EntryID })
	for _, e := range sorted {
		x.entry(e)
	}
	for id, claims := range x.claims {
		if len(claims) > 1 {
			sort.Strings(claims)
			x.add("DUPLICATE_CREDIT", "discrepancy", "One event is claimed by multiple credited ledger entries.", claims, []string{id}, 1, len(claims))
			for _, entry := range claims {
				delete(x.verified, entry)
			}
		}
	}
	seen := map[string]bool{}
	for _, t := range ev.Transfers {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		if t.Block < ev.Coverage.From || t.Block > ev.Coverage.To || !Eligible(t, c, owners) || x.consumed[t.ID] {
			continue
		}
		x.add("UNRECORDED_ONCHAIN_TRANSFER", "investigation", "An eligible inbound event has no uniquely matched valid ledger credit in the bounded scan.", nil, []string{t.ID}, map[string]string{"account_id": owners[t.To]}, t)
	}
	x.result.Verified = len(x.verified)
	Sort(x.result.Findings)
	return x.result
}
func (x *evaluator) add(code, state, why string, entries, events []string, expected, observed any) {
	x.result.Findings = append(x.result.Findings, domain.NewFinding(code, state, why, entries, events, expected, observed))
	if state == "incomplete" || state == "unresolved" {
		x.result.Incomplete = true
	}
}
func (x *evaluator) entry(e domain.Entry) {
	refs := []string{e.EntryID}
	before := len(x.result.Findings)
	add := func(code, state, why string, expected, observed any) {
		x.add(code, state, why, refs, nil, expected, observed)
	}
	if e.ChainID != x.c.Network.ChainID {
		add("WRONG_CHAIN_ID", "discrepancy", "Ledger asset chain differs from configured chain.", x.c.Network.ChainID, e.ChainID)
		return
	}
	if e.Token != x.c.Asset.Token {
		add("WRONG_TOKEN_CONTRACT", "discrepancy", "Ledger token differs from configured asset.", x.c.Asset.Token, e.Token)
		return
	}
	owner, known := x.owners[e.Deposit]
	if !known || owner != e.AccountID {
		add("ACCOUNT_ATTRIBUTION_MISMATCH", "discrepancy", "Ledger address is not independently registered to the claimed account.", e.AccountID, owner)
	}
	if e.Tx == "" {
		candidates := []string{}
		for _, t := range x.evidence.Transfers {
			if t.Canonical && t.Token == e.Token && t.To == e.Deposit && t.Amount == e.Amount {
				candidates = append(candidates, t.ID)
			}
		}
		sort.Strings(candidates)
		code := "EXPECTED_DEPOSIT_NOT_OBSERVED"
		if e.Status == "credited" {
			code = "TRANSACTION_NOT_OBSERVED"
		}
		x.add(code, "unresolved", "Transaction identity is absent. Attribute-only discovery candidates do not establish a verified event identity.", refs, candidates, e, nil)
		return
	}
	obs, ok := x.evidence.Receipts[e.Tx]
	if !ok || obs.State == "unavailable" || obs.State == "after_reference_head" {
		add("RPC_EVIDENCE_UNAVAILABLE", "incomplete", "Receipt or canonical-header evidence could not be obtained at the reference snapshot.", e.Tx, obs.State)
		return
	}
	if obs.State == "not_observed" || obs.Receipt == nil {
		code := "TRANSACTION_NOT_OBSERVED"
		if e.Status == "expected" {
			code = "EXPECTED_DEPOSIT_NOT_OBSERVED"
		}
		add(code, "unresolved", "The provider did not observe the transaction; absence does not prove failure or reorganization.", e.Tx, nil)
		return
	}
	r := obs.Receipt
	if !obs.Canonical {
		add("NONCANONICAL_BLOCK", "discrepancy", "Receipt block hash disagrees with the canonical header.", "canonical block hash", r.BlockHash)
		return
	}
	if r.Status != 1 {
		add("TRANSACTION_REVERTED", "discrepancy", "Canonical receipt reports execution failure; no logs justify this deposit.", 1, r.Status)
		return
	}
	if r.Malformed {
		add("MALFORMED_EVENT_DATA", "incomplete", "Receipt contains malformed Transfer data or inconsistent log metadata.", "strict ERC-20 event", r)
		return
	}
	if e.RecordedHash != "" && e.RecordedHash != r.BlockHash {
		add("RECORDED_BLOCK_MISMATCH", "discrepancy", "Ledger-supplied block hash differs from current canonical inclusion.", e.RecordedHash, r.BlockHash)
	}
	var candidates []domain.Transfer
	wrongToken := false
	for _, t := range r.Transfers {
		if e.LogIndex != nil && t.Index != *e.LogIndex {
			continue
		}
		if t.Token != x.c.Asset.Token {
			wrongToken = true
			continue
		}
		if t.Canonical {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		code := "TRANSFER_EVENT_NOT_FOUND"
		if wrongToken {
			code = "WRONG_TOKEN_CONTRACT"
		}
		add(code, "discrepancy", "Successful receipt does not contain the requested configured-token Transfer event.", e, r)
		return
	}
	candidates = selectCandidates(candidates, e)
	if len(candidates) != 1 {
		events := []string{}
		for _, t := range candidates {
			events = append(events, t.ID)
		}
		sort.Strings(events)
		x.add("AMBIGUOUS_TRANSFER_MATCH", "unresolved", "Available event attributes do not uniquely identify one Transfer; supply its block-global log_index.", refs, events, e, nil)
		return
	}
	t := candidates[0]
	events := []string{t.ID}
	eventAdd := func(code, state, why string, expected, observed any) {
		x.add(code, state, why, refs, events, expected, observed)
	}
	if e.Status == "credited" {
		x.claims[t.ID] = append(x.claims[t.ID], e.EntryID)
	}
	if t.To != e.Deposit {
		eventAdd("RECIPIENT_MISMATCH", "discrepancy", "Observed recipient differs from ledger deposit address.", e.Deposit, t.To)
	}
	if x.owners[t.To] != e.AccountID {
		eventAdd("ACCOUNT_ATTRIBUTION_MISMATCH", "discrepancy", "Observed recipient does not belong to the claimed account in the independent registry.", e.AccountID, x.owners[t.To])
	}
	if t.Amount != e.Amount {
		eventAdd("AMOUNT_MISMATCH", "discrepancy", "Exact uint256 base-unit amounts differ.", e.Amount, t.Amount)
	}
	if !x.c.Policy.IncludeMint && t.From == zero {
		eventAdd("TRANSFER_EVENT_NOT_FOUND", "discrepancy", "Mint events are excluded by the configured deposit policy.", "non-mint deposit", t.From)
	}
	if t.Confirmations < x.c.Policy.MinConfirmations {
		state := "investigation"
		if e.Status == "credited" {
			state = "discrepancy"
		}
		eventAdd("INSUFFICIENT_CONFIRMATIONS", state, "Canonical event is below the current confirmation-count policy.", x.c.Policy.MinConfirmations, t.Confirmations)
	}
	attributeValid := len(x.result.Findings) == before
	if e.Status == "credited" {
		if e.CreditedAt == nil {
			eventAdd("HISTORICAL_CONFIRMATION_UNKNOWN", "informational", "Current eligibility can be verified, but historical credit timing is unavailable.", "ledger-supplied credited_at_block", nil)
		} else {
			if *e.CreditedAt < t.Block {
				eventAdd("CREDIT_HEIGHT_INCONSISTENT", "discrepancy", "Ledger-supplied credit height precedes canonical inclusion.", fmt.Sprintf(">= %d", t.Block), *e.CreditedAt)
				eventAdd("PREMATURE_CREDIT", "discrepancy", "Ledger-supplied timing reports credit before inclusion.", x.c.Policy.MinConfirmations, 0)
			} else if *e.CreditedAt > x.evidence.Head.Number {
				eventAdd("CREDIT_HEIGHT_INCONSISTENT", "unresolved", "Ledger-supplied credit height is after the reference head.", x.evidence.Head.Number, *e.CreditedAt)
			} else if *e.CreditedAt-t.Block+1 < x.c.Policy.MinConfirmations {
				eventAdd("PREMATURE_CREDIT", "discrepancy", "Ledger-supplied credit timing is below policy; this is not independent proof of historical timing.", x.c.Policy.MinConfirmations, *e.CreditedAt-t.Block+1)
			}
		}
		if attributeValid && Eligible(t, x.c, x.owners) {
			x.consumed[t.ID] = true
			valid := true
			for _, f := range x.result.Findings[before:] {
				if f.State != "informational" {
					valid = false
				}
			}
			if valid {
				x.verified[e.EntryID] = true
			}
		}
	} else if attributeValid && Eligible(t, x.c, x.owners) {
		eventAdd("ELIGIBLE_DEPOSIT_NOT_CREDITED", "investigation", "A uniquely identified eligible deposit remains expected rather than credited.", "credited", e.Status)
		x.consumed[t.ID] = true
	}
}
func selectCandidates(in []domain.Transfer, e domain.Entry) []domain.Transfer {
	if e.LogIndex != nil || len(in) == 1 {
		return in
	}
	var exact, recipient, amount []domain.Transfer
	for _, t := range in {
		if t.To == e.Deposit {
			recipient = append(recipient, t)
			if t.Amount == e.Amount {
				exact = append(exact, t)
			}
		}
		if t.Amount == e.Amount {
			amount = append(amount, t)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	if len(recipient) > 0 {
		return recipient
	}
	if len(amount) > 0 {
		return amount
	}
	return in
}
func Sort(f []domain.Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		a, b := f[i], f[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		ak, bk := strings.Join(a.Entries, "|"), strings.Join(b.Entries, "|")
		if ak != bk {
			return ak < bk
		}
		return strings.Join(a.Events, "|") < strings.Join(b.Events, "|")
	})
}
