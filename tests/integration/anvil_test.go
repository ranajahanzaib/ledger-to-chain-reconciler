//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAnvilEndToEnd(t *testing.T) {
	for _, tool := range []string{"anvil", "forge", "cast", "python3"} {
		if _, e := exec.LookPath(tool); e != nil {
			t.Fatalf("required integration tool %s missing: %v", tool, e)
		}
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(t.TempDir(), "ledgerchain")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ledgerchain")
	build.Dir = root
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v\n%s", e, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "script/demo.py", "--integration", "--output-dir", t.TempDir())
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "LEDGERCHAIN_BIN="+binary)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("Anvil integration: %v\n%s", e, out)
	} else {
		t.Log(string(out))
	}
}
