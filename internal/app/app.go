// Package app coordinates adapters; accounting rules remain in reconcile.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/chain"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/config"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/ledger"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/reconcile"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/report"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/storage"
)

var Version = "dev"
var Commit = "unknown"
var BuildDate = "unknown"

const usage = `ledgerchain — read-only ERC-20 ledger reconciliation

Commands:
  version     Show build metadata
  validate    Validate configuration, ledger and independent ownership (offline)
  reconcile   Observe RPC evidence, reconcile both directions, and report

Examples:
  ledgerchain validate --config examples/config.yaml --ledger examples/ledger.csv --accounts examples/accounts.csv
  ledgerchain reconcile --config demo/config.yaml --ledger demo/ledger.csv --accounts demo/accounts.csv --output demo/report.json
  ledgerchain reconcile --config config.yaml --ledger ledger.sqlite --ledger-type sqlite --accounts accounts.csv --state-db history.sqlite

Exit codes: 0 complete/pass; 1 complete/threshold findings; 2 invalid input or execution failure; 3 incomplete or unresolved.
`

func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if args[0] == "version" {
		fmt.Fprintf(stdout, "ledgerchain %s commit=%s built=%s\n", Version, Commit, BuildDate)
		return 0
	}
	if args[0] != "validate" && args[0] != "reconcile" {
		fmt.Fprintln(stderr, "unknown command; use ledgerchain help")
		return 2
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(stderr)
	cfg := f.String("config", "", "YAML configuration path")
	input := f.String("ledger", "", "source ledger path")
	kind := f.String("ledger-type", "csv", "csv or sqlite")
	accounts := f.String("accounts", "", "independent ownership CSV path")
	output := f.String("output", "", "report path (default stdout)")
	state := f.String("state-db", "", "separate optional history SQLite path")
	endpoint := f.String("rpc-url", "", "RPC URL override (prefer LEDGERCHAIN_RPC_URL to keep credentials out of process arguments)")
	threshold := f.String("fail-on", "medium", "minimum severity to fail: info, low, medium, high, critical")
	format := f.String("format", "", "json or text (overrides configuration)")
	verbose := f.Bool("verbose", false, "print evidence counts and snapshot fingerprint")
	if e := f.Parse(args[1:]); e != nil {
		if e == flag.ErrHelp {
			return 0
		}
		return 2
	}
	fail := func(e error) int { fmt.Fprintln(stderr, e); return 2 }
	if f.NArg() != 0 || *cfg == "" || *input == "" || *accounts == "" {
		return fail(fmt.Errorf("--config, --ledger and --accounts are required; no positional arguments accepted"))
	}
	if *kind != "csv" && *kind != "sqlite" {
		return fail(fmt.Errorf("ledger-type must be csv or sqlite"))
	}
	if _, ok := domain.Severities[*threshold]; !ok {
		return fail(fmt.Errorf("invalid fail-on severity"))
	}
	for _, w := range []string{*state, *output} {
		if w == "" {
			continue
		}
		for _, src := range []string{*cfg, *input, *accounts} {
			if storage.SameFile(w, src) {
				return fail(fmt.Errorf("output/history path must differ from all input files"))
			}
		}
	}
	if *state != "" && *output != "" && storage.SameFile(*state, *output) {
		return fail(fmt.Errorf("report and history paths must differ"))
	}
	c, e := config.Load(*cfg, *endpoint)
	if e != nil {
		return fail(e)
	}
	if *format != "" {
		c.Output.Format = *format
		if e = c.Validate(); e != nil {
			return fail(e)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.RPC.RunTimeout)*time.Second)
	defer cancel()
	owners, e := ledger.ReadOwners(*accounts)
	if e != nil {
		return fail(e)
	}
	var entries []domain.Entry
	if *kind == "csv" {
		entries, e = ledger.ReadCSV(*input)
	} else {
		entries, e = ledger.ReadSQLite(ctx, *input)
	}
	if e != nil {
		return fail(e)
	}
	if args[0] == "validate" {
		for _, v := range entries {
			if v.ChainID != c.Network.ChainID || v.Token != c.Asset.Token || owners[v.Deposit] != v.AccountID {
				return fail(fmt.Errorf("entry %q: chain, token or independent ownership conflicts with configuration", v.EntryID))
			}
		}
		fmt.Fprintf(stdout, "Validated %d ledger entries and %d independently owned addresses (offline).\n", len(entries), len(owners))
		return 0
	}
	started := time.Now().UTC()
	rpc, e := chain.Dial(ctx, c.Network.RPCURL, time.Duration(c.RPC.Timeout)*time.Second, c.RPC.Retries)
	if e != nil {
		return fail(e)
	}
	defer rpc.Close()
	ev, e := chain.Observe(ctx, rpc, c, entries, owners)
	if e != nil {
		return fail(e)
	}
	result := reconcile.Evaluate(entries, owners, ev, c)
	var history *storage.Store
	if *state != "" {
		history, e = storage.Open(ctx, *state)
		if e != nil {
			return fail(e)
		}
		defer history.Close()
		prior, e := history.Prior(ctx, c.Network.ChainID, c.Asset.Token)
		if e != nil {
			return fail(e)
		}
		findings, incomplete := storage.Compare(ctx, rpc, prior, ev.Head, owners)
		result.Findings = append(result.Findings, findings...)
		result.Incomplete = result.Incomplete || incomplete
		// History reads may take time. Recheck the original reference anchor afterwards.
		h, e := rpc.Header(ctx, &ev.Head.Number)
		if e != nil || h.Hash != ev.Head.Hash {
			result.Incomplete = true
			ev.Consistent = false
			result.Findings = append(result.Findings, domain.NewFinding("RPC_EVIDENCE_UNAVAILABLE", "incomplete", "Reference head changed or became unavailable during history comparison.", nil, nil, ev.Head, h))
		}
	}
	reconcile.Sort(result.Findings)
	id := make([]byte, 16)
	if _, e = rand.Read(id); e != nil {
		return fail(e)
	}
	r := domain.Run{Configuration: c.Snapshot(), ID: hex.EncodeToString(id), Started: started, Completed: time.Now().UTC(), ConfigFingerprint: c.Fingerprint(), LedgerFingerprint: domain.Fingerprint(entries), OwnershipFingerprint: domain.Fingerprint(owners), ChainID: c.Network.ChainID, Token: c.Asset.Token, RPCSource: config.SourceID(c.Network.RPCURL), MinConfirmations: c.Policy.MinConfirmations, Status: "complete", Findings: result.Findings, Entries: entries, Owners: owners, Evidence: ev}
	if result.Incomplete {
		r.Status = "incomplete"
	}
	r.Summary = domain.Summary{Entries: len(entries), Verified: result.Verified, Findings: len(r.Findings), BySeverity: map[string]int{}}
	snapshot := domain.Fingerprint(struct{ Config, Ledger, Owners, Head string }{r.ConfigFingerprint, r.LedgerFingerprint, r.OwnershipFingerprint, ev.Head.Hash})
	for i := range r.Findings {
		r.Findings[i].Snapshot = snapshot
		r.Summary.BySeverity[r.Findings[i].Severity]++
	}
	if history != nil {
		if e = history.Save(ctx, r); e != nil {
			return fail(e)
		}
	}
	write := func(w io.Writer) error {
		if c.Output.Format == "text" {
			return report.Text(w, r)
		}
		return report.JSON(w, r)
	}
	if *output == "" {
		e = write(stdout)
	} else {
		e = atomicWrite(*output, write)
	}
	if e != nil {
		return fail(e)
	}
	fmt.Fprintf(stderr, "%s: %d entries, %d verified, %d findings; discovery %d..%d complete=%t\n", r.Status, r.Summary.Entries, r.Summary.Verified, r.Summary.Findings, ev.Coverage.From, ev.Coverage.To, ev.Coverage.Complete)
	if *verbose {
		fmt.Fprintf(stderr, "Snapshot %s; receipts=%d events=%d headers=%d\n", snapshot, len(ev.Receipts), len(ev.Transfers), len(ev.Headers))
	}
	return report.ExitCode(r, *threshold)
}
func atomicWrite(path string, write func(io.Writer) error) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".ledgerchain-report-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = write(f); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(name, path)
}
