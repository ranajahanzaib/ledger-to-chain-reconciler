package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndRedact(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := `network:
  chain_id: 31337
  rpc_url: https://user:password@example.org/key-secret?token=secret
asset:
  token_address: 0x1111111111111111111111111111111111111111
`
	if e := os.WriteFile(p, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	c, e := Load(p, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{SourceID(c.Network.RPCURL), c.Fingerprint()} {
		if strings.Contains(s, "secret") || strings.Contains(s, "password") || strings.Contains(s, "example") {
			t.Fatal("credentials exposed")
		}
	}
	c.Policy.MinConfirmations = 0
	if c.Validate() == nil {
		t.Fatal("zero confirmations accepted")
	}
	os.WriteFile(p, []byte(body+"unknown: true\n"), 0600)
	if _, e = Load(p, ""); e == nil {
		t.Fatal("unknown setting ignored")
	}
}
