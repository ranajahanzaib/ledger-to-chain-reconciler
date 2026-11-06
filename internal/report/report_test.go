package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

func TestReportAndExit(t *testing.T) {
	r := domain.Run{ID: "test", Status: "complete", Findings: []domain.Finding{}, Summary: domain.Summary{}}
	if ExitCode(r, "medium") != 0 {
		t.Fatal("clean fails")
	}
	r.Findings = append(r.Findings, domain.NewFinding("AMOUNT_MISMATCH", "discrepancy", "exact amounts differ", []string{"a"}, []string{"event"}, "100", "90"))
	if ExitCode(r, "medium") != 1 || ExitCode(r, "critical") != 0 {
		t.Fatal("threshold ignored")
	}
	r.Status = "incomplete"
	if ExitCode(r, "critical") != 3 {
		t.Fatal("incomplete passed")
	}
	var b bytes.Buffer
	if e := JSON(&b, r); e != nil {
		t.Fatal(e)
	}
	var decoded domain.Run
	if e := json.Unmarshal(b.Bytes(), &decoded); e != nil || decoded.Findings[0].Observed != "90" {
		t.Fatal("lossy report", e)
	}
	b.Reset()
	if e := Text(&b, r); e != nil || !bytes.Contains(b.Bytes(), []byte("AMOUNT_MISMATCH")) {
		t.Fatal("text report", e)
	}
}
