package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

func JSON(w io.Writer, r domain.Run) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	e.SetEscapeHTML(false)
	return e.Encode(r)
}
func Text(w io.Writer, r domain.Run) error {
	if _, e := fmt.Fprintf(w, "Ledger-to-Chain Reconciler | %s\nRun: %s\nChain %s | Token %s\nReference head: %d %s\nScan: %d..%d (complete=%t) | confirmations >= %d\nEntries: %d | verified: %d | findings: %d\n", r.Status, r.ID, r.ChainID, r.Token, r.Evidence.Head.Number, r.Evidence.Head.Hash, r.Evidence.Coverage.From, r.Evidence.Coverage.To, r.Evidence.Coverage.Complete, r.MinConfirmations, r.Summary.Entries, r.Summary.Verified, r.Summary.Findings); e != nil {
		return e
	}
	for _, f := range r.Findings {
		if _, e := fmt.Fprintf(w, "[%s] %s (%s) entries=%v events=%v: %s\n", f.Severity, f.Code, f.State, f.Entries, f.Events, f.Explanation); e != nil {
			return e
		}
	}
	return nil
}
func ExitCode(r domain.Run, threshold string) int {
	if r.Status != "complete" {
		return 3
	}
	rank := domain.Severities[threshold]
	for _, f := range r.Findings {
		if domain.Severities[f.Severity] >= rank {
			return 1
		}
	}
	return 0
}
