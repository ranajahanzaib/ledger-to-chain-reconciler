package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

type reader struct {
	hash string
	err  error
}

func (r reader) ChainID(context.Context) (string, error) { return "1", nil }
func (r reader) Header(context.Context, *uint64) (domain.Header, error) {
	return domain.Header{Number: 10, Hash: r.hash}, r.err
}
func (r reader) Receipt(context.Context, string) (*domain.Receipt, error) { return nil, nil }
func (r reader) Logs(context.Context, string, []string, uint64, uint64) ([]domain.Transfer, error) {
	return nil, nil
}
func TestHistoryRepeatAndReorg(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, filepath.Join(t.TempDir(), "history.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	v := domain.Transfer{ID: "1:tx:0", ChainID: "1", Token: "token", Block: 10, BlockHash: "old", To: "alice", Canonical: true, ReceiptStatus: 1}
	run := domain.Run{ID: "one", ChainID: "1", Token: "token", Status: "complete", Evidence: domain.Evidence{Consistent: true, Transfers: []domain.Transfer{v}}}
	if e = s.Save(ctx, run); e != nil {
		t.Fatal(e)
	}
	run.ID = "two"
	if e = s.Save(ctx, run); e != nil {
		t.Fatal(e)
	}
	prior, e := s.Prior(ctx, "1", "token")
	if e != nil || len(prior) != 1 {
		t.Fatal(prior, e)
	}
	owners := map[string]string{"alice": "account"}
	for _, tt := range []struct {
		hash       string
		err        error
		want       string
		incomplete bool
	}{{"old", nil, "", false}, {"new", nil, "REORG_OBSERVED", false}, {"", errors.New("unavailable"), "RPC_EVIDENCE_UNAVAILABLE", true}} {
		f, incomplete := Compare(ctx, reader{tt.hash, tt.err}, prior, domain.Header{Number: 12}, owners)
		if incomplete != tt.incomplete {
			t.Fatal("incomplete state")
		}
		if tt.want == "" {
			if len(f) != 0 {
				t.Fatal(f)
			}
		} else if len(f) != 1 || f[0].Code != tt.want {
			t.Fatal(f)
		}
	}
	if e = s.Save(ctx, run); e == nil {
		t.Fatal("duplicate run not atomic")
	}
	prior, e = s.Prior(ctx, "1", "token")
	if e != nil || len(prior) != 1 {
		t.Fatal("failed write damaged observations")
	}
}

func TestUncreatedSymlinkAliases(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual")
	if e := os.Mkdir(actual, 0700); e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(dir, "alias")
	if e := os.Symlink(actual, alias); e != nil {
		t.Fatal(e)
	}
	if !SameFile(filepath.Join(actual, "new.sqlite"), filepath.Join(alias, "new.sqlite")) {
		t.Fatal("missed nonexistent alias")
	}
}
func TestRefuseSourceMigration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "source.sqlite")
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE ledger_entries (entry_id TEXT)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	before, _ := os.ReadFile(p)
	if s, e := Open(context.Background(), p); e == nil {
		s.Close()
		t.Fatal("source migrated")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("unrecognized database modified")
	}
}
