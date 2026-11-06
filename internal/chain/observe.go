package chain

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/config"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

// Observe uses one fixed reference head; advancing heads are harmless, replacing
// the reference head or any checked historical block makes the run incomplete.
func Observe(ctx context.Context, r Reader, c config.Config, entries []domain.Entry, owners map[string]string) (domain.Evidence, error) {
	ev := domain.Evidence{Receipts: map[string]domain.Observation{}, Transfers: []domain.Transfer{}, Headers: []domain.Header{}, Issues: []domain.Finding{}, Consistent: true}
	id, e := r.ChainID(ctx)
	if e != nil {
		return ev, fmt.Errorf("chain ID evidence unavailable")
	}
	if id != c.Network.ChainID {
		return ev, fmt.Errorf("WRONG_CHAIN_ID: RPC network differs from configured chain")
	}
	ev.Head, e = r.Header(ctx, nil)
	if e != nil {
		return ev, fmt.Errorf("reference head unavailable")
	}
	to := ev.Head.Number
	if c.Discovery.To != "latest" {
		to, _ = strconv.ParseUint(c.Discovery.To, 10, 64)
	}
	if to > ev.Head.Number || to < c.Discovery.From || to-c.Discovery.From >= c.Discovery.MaxBlocks {
		return ev, fmt.Errorf("discovery range outside reference head or max_blocks")
	}
	ev.Coverage = domain.Coverage{From: c.Discovery.From, To: to, Complete: true, Completed: []domain.BlockRange{}, Failed: []domain.BlockRange{}}
	issue := func(code, why string) {
		ev.Issues = append(ev.Issues, domain.NewFinding(code, "incomplete", why, nil, nil, nil, nil))
		ev.Consistent = false
	}
	wanted := map[string]bool{}
	for _, v := range entries {
		if v.ChainID == id && v.Tx != "" {
			wanted[v.Tx] = true
		}
	}
	recipients := make([]string, 0, len(owners))
	for a := range owners {
		recipients = append(recipients, a)
	}
	sort.Strings(recipients)
	var discovered []domain.Transfer
	// Bound recipient OR clauses as well as block ranges.
	for start := c.Discovery.From; ; {
		end := to
		if to-start >= c.Discovery.Chunk {
			end = start + c.Discovery.Chunk - 1
		}
		ok := true
		for off := 0; off < len(recipients); off += 100 {
			limit := off + 100
			if limit > len(recipients) {
				limit = len(recipients)
			}
			logs, err := r.Logs(ctx, c.Asset.Token, recipients[off:limit], start, end)
			if err != nil {
				if errors.Is(err, ErrMalformed) {
					issue("MALFORMED_EVENT_DATA", "Discovery returned malformed ERC-20 event evidence.")
				}
				ok = false
				continue
			}
			if len(discovered)+len(logs) > MaxLogs {
				ok = false
				continue
			}
			discovered = append(discovered, logs...)
		}
		span := domain.BlockRange{From: start, To: end}
		if ok {
			ev.Coverage.Completed = append(ev.Coverage.Completed, span)
		} else {
			ev.Coverage.Complete = false
			ev.Coverage.Failed = append(ev.Coverage.Failed, span)
		}
		if end == to {
			break
		}
		start = end + 1
	}
	if !ev.Coverage.Complete {
		issue("SCAN_INCOMPLETE", "One or more bounded discovery chunks could not be verified; coverage is partial.")
	}
	for _, t := range discovered {
		wanted[t.Tx] = true
	}
	if len(wanted) > MaxLogs {
		return ev, fmt.Errorf("receipt workload exceeds limit")
	}
	txs := make([]string, 0, len(wanted))
	for tx := range wanted {
		txs = append(txs, tx)
	}
	sort.Strings(txs)
	type result struct {
		tx      string
		receipt *domain.Receipt
		err     error
	}
	jobs := make(chan string)
	results := make(chan result, c.RPC.Concurrency)
	var wg sync.WaitGroup
	for i := 0; i < c.RPC.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tx := range jobs {
				receipt, err := r.Receipt(ctx, tx)
				results <- result{tx, receipt, err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, tx := range txs {
			jobs <- tx
		}
	}()
	go func() { wg.Wait(); close(results) }()
	// Header access is serialized and cached: every receipt block is checked once.
	headers := map[uint64]domain.Header{ev.Head.Number: ev.Head}
	headerErrors := map[uint64]bool{}
	for res := range results {
		obs := domain.Observation{Receipt: res.receipt, State: "observed"}
		if res.err != nil || res.receipt == nil {
			obs.Receipt = nil
			if errors.Is(res.err, ErrMalformed) {
				issue("MALFORMED_EVENT_DATA", "Receipt identity or log metadata is malformed.")
			}
			if errors.Is(res.err, ErrMissing) {
				obs.State = "not_observed"
			} else {
				obs.State = "unavailable"
			}
			ev.Receipts[res.tx] = obs
			continue
		}
		receipt := res.receipt
		if receipt.Tx != res.tx || receipt.Status > 1 || receipt.BlockHash == "" {
			obs.State = "unavailable"
			ev.Receipts[res.tx] = obs
			continue
		}
		if receipt.Block > ev.Head.Number {
			obs.State = "after_reference_head"
			ev.Receipts[res.tx] = obs
			continue
		}
		h, ok := headers[receipt.Block]
		if !ok && !headerErrors[receipt.Block] {
			h, e = r.Header(ctx, &receipt.Block)
			if e != nil {
				headerErrors[receipt.Block] = true
			} else {
				headers[receipt.Block] = h
			}
		}
		if headerErrors[receipt.Block] {
			obs.State = "unavailable"
			ev.Receipts[res.tx] = obs
			continue
		}
		obs.Canonical = h.Hash == receipt.BlockHash
		if !obs.Canonical {
			obs.State = "noncanonical"
		}
		for i := range receipt.Transfers {
			t := &receipt.Transfers[i]
			t.ChainID = id
			t.ID = domain.EventID(id, t.Tx, t.Index)
			t.Canonical = obs.Canonical
			t.ReceiptStatus = receipt.Status
			if t.Tx != res.tx || t.Block != receipt.Block || t.BlockHash != receipt.BlockHash || t.TransactionIndex != receipt.TransactionIndex {
				receipt.Malformed = true
				t.Canonical = false
			}
			if t.Canonical {
				t.Confirmations = ev.Head.Number - t.Block + 1
			}
			if t.Token == c.Asset.Token {
				ev.Transfers = append(ev.Transfers, *t)
			}
		}
		if receipt.Malformed {
			for i := range receipt.Transfers {
				receipt.Transfers[i].Canonical = false
			}
			for i := range ev.Transfers {
				if ev.Transfers[i].Tx == res.tx {
					ev.Transfers[i].Canonical = false
				}
			}
		}
		ev.Receipts[res.tx] = obs
	}
	// Scan responses are only discovery hints. Every returned event must also be
	// present byte-for-byte in its independently retrieved, canonical receipt.
	for _, t := range discovered {
		obs := ev.Receipts[t.Tx]
		found := false
		if obs.Receipt != nil && obs.Canonical && obs.Receipt.Status == 1 && !obs.Receipt.Malformed {
			for _, rt := range obs.Receipt.Transfers {
				if sameEvent(t, rt) {
					found = true
					break
				}
			}
		}
		if !found {
			ev.Coverage.Complete = false
			issue("SCAN_INCOMPLETE", "A discovered event lacks matching canonical successful receipt evidence.")
		}
	}
	// Anchor both scan boundaries, including empty-log ranges, to the chain view.
	for _, n := range []uint64{c.Discovery.From, to} {
		if _, ok := headers[n]; !ok {
			h, err := r.Header(ctx, &n)
			if err != nil {
				issue("RPC_EVIDENCE_UNAVAILABLE", "Scan boundary header unavailable.")
			} else {
				headers[n] = h
			}
		}
	}
	nums := make([]uint64, 0, len(headers))
	for n := range headers {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	for _, n := range nums {
		h := headers[n]
		ev.Headers = append(ev.Headers, h)
		again, err := r.Header(ctx, &n)
		if err != nil || again.Hash != h.Hash {
			issue("RPC_EVIDENCE_UNAVAILABLE", "Canonical block identities changed or became unavailable during the run.")
		}
	}
	sort.Slice(ev.Transfers, func(i, j int) bool {
		a, b := ev.Transfers[i], ev.Transfers[j]
		if a.Block != b.Block {
			return a.Block < b.Block
		}
		if a.TransactionIndex != b.TransactionIndex {
			return a.TransactionIndex < b.TransactionIndex
		}
		return a.Index < b.Index
	})
	return ev, nil
}
func sameEvent(a, b domain.Transfer) bool {
	return a.Tx == b.Tx && a.Index == b.Index && a.Token == b.Token && a.From == b.From && a.To == b.To && a.Amount == b.Amount && a.Block == b.Block && a.BlockHash == b.BlockHash && a.TransactionIndex == b.TransactionIndex
}
