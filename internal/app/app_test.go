package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCLI(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"help"}, {"reconcile", "--help"}} {
		var out bytes.Buffer
		if code := Main(context.Background(), args, &out, &out); code != 0 || out.Len() == 0 {
			t.Fatal(args, code, out.String())
		}
	}
	for _, args := range [][]string{{"unknown"}, {"reconcile"}, {"validate", "--bad-flag"}} {
		var out bytes.Buffer
		if code := Main(context.Background(), args, &out, &out); code != 2 {
			t.Fatal(args, code)
		}
	}
}
func TestOfflineAndPathIsolation(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.yaml")
	ledger := filepath.Join(dir, "ledger.csv")
	accounts := filepath.Join(dir, "accounts.csv")
	os.WriteFile(config, []byte("network:\n  chain_id: 31337\n  rpc_url: http://127.0.0.1:1\nasset:\n  token_address: 0x1111111111111111111111111111111111111111\n"), 0600)
	os.WriteFile(accounts, []byte("account_id,deposit_address\nalice,0x2222222222222222222222222222222222222222\n"), 0600)
	os.WriteFile(ledger, []byte("entry_id,account_id,chain_id,token_address,deposit_address,expected_amount_base_units,tx_hash,log_index,credit_status,credited_at_block,recorded_block_hash\na,alice,31337,0x1111111111111111111111111111111111111111,0x2222222222222222222222222222222222222222,100,,,expected,,\n"), 0600)
	args := []string{"validate", "--config", config, "--ledger", ledger, "--accounts", accounts}
	var out bytes.Buffer
	if code := Main(context.Background(), args, &out, &out); code != 0 {
		t.Fatal(code, out.String())
	}
	alias := filepath.Join(dir, "alias")
	if e := os.Link(ledger, alias); e != nil {
		t.Fatal(e)
	}
	for _, extra := range [][]string{{"--output", ledger}, {"--state-db", ledger}, {"--output", alias}, {"--fail-on", "invalid"}} {
		out.Reset()
		full := append(append([]string{}, args...), extra...)
		if code := Main(context.Background(), full, &out, &out); code != 2 {
			t.Fatal("unsafe path accepted", full, code)
		}
	}
}
