// Package storage owns a separate, versioned SQLite observation database.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/chain"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func SameFile(a, b string) bool {
	aa := resolvedPath(a)
	bb := resolvedPath(b)
	if aa == bb {
		return true
	}
	sa, ea := os.Stat(a)
	sb, eb := os.Stat(b)
	return ea == nil && eb == nil && os.SameFile(sa, sb)
}

// Resolve existing ancestors too: two not-yet-created files may alias through
// different symlinked parent directories.
func resolvedPath(path string) string {
	abs, _ := filepath.Abs(path)
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		return resolved
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs
	}
	return filepath.Join(resolvedPath(parent), filepath.Base(abs))
}

func Open(ctx context.Context, path string) (*Store, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: abs}
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if _, e = db.ExecContext(ctx, "PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON"); e != nil {
		return fail(e)
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback()
	var version int
	if e = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return fail(e)
	}
	var applicationID int
	if e = tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); e != nil {
		return fail(e)
	}
	if version == 1 && applicationID != 1279477838 {
		return fail(fmt.Errorf("unrecognized history database application identity"))
	}
	if version > 1 {
		return fail(fmt.Errorf("history schema newer than supported version"))
	}
	if version == 0 {
		var tables int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); e != nil {
			return fail(e)
		}
		if tables > 0 {
			return fail(fmt.Errorf("refusing migration of nonempty unrecognized database"))
		}
		for _, q := range []string{"CREATE TABLE runs (run_id TEXT PRIMARY KEY, chain_id TEXT NOT NULL, token TEXT NOT NULL, completed_at TEXT NOT NULL, status TEXT NOT NULL, report_json TEXT NOT NULL)", "CREATE TABLE observations (chain_id TEXT NOT NULL, token TEXT NOT NULL, event_id TEXT NOT NULL, block_number TEXT NOT NULL, block_hash TEXT NOT NULL, last_run TEXT NOT NULL REFERENCES runs(run_id), transfer_json TEXT NOT NULL, PRIMARY KEY(chain_id,token,event_id,block_hash))", "PRAGMA application_id=1279477838", "PRAGMA user_version=1"} {
			if _, e = tx.ExecContext(ctx, q); e != nil {
				return fail(e)
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Prior(ctx context.Context, id, token string) ([]domain.Transfer, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT transfer_json FROM observations WHERE chain_id=? AND token=? ORDER BY event_id,block_hash LIMIT 100001", id, token)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Transfer{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var t domain.Transfer
		if e = json.Unmarshal([]byte(raw), &t); e != nil {
			return nil, e
		}
		out = append(out, t)
		if len(out) > 100000 {
			return nil, fmt.Errorf("history scope exceeds 100000 observations; rotate history database")
		}
	}
	return out, rows.Err()
}
func (s *Store) Save(ctx context.Context, r domain.Run) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT INTO runs VALUES(?,?,?,?,?,?)", r.ID, r.ChainID, r.Token, r.Completed.Format("2006-01-02T15:04:05.999999999Z07:00"), r.Status, string(b)); e != nil {
		return e
	}
	for _, t := range r.Evidence.Transfers {
		if !r.Evidence.Consistent || !t.Canonical || t.ReceiptStatus != 1 {
			continue
		}
		b, e = json.Marshal(t)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO observations VALUES(?,?,?,?,?,?,?) ON CONFLICT(chain_id,token,event_id,block_hash) DO UPDATE SET last_run=excluded.last_run,transfer_json=excluded.transfer_json", t.ChainID, t.Token, t.ID, fmt.Sprint(t.Block), t.BlockHash, r.ID, string(b)); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Compare only calls a reorganization proven by a canonical header mismatch.
// Missing historical blocks or provider errors are insufficient proof.
func Compare(ctx context.Context, r chain.Reader, prior []domain.Transfer, head domain.Header, owners map[string]string) ([]domain.Finding, bool) {
	out := []domain.Finding{}
	incomplete := false
	cache := map[uint64]domain.Header{}
	failed := map[uint64]bool{}
	for _, t := range prior {
		if owners[t.To] == "" {
			continue
		}
		h, ok := cache[t.Block]
		if !ok && !failed[t.Block] {
			var e error
			h, e = r.Header(ctx, &t.Block)
			if e != nil {
				failed[t.Block] = true
			} else {
				cache[t.Block] = h
			}
		}
		if failed[t.Block] || t.Block > head.Number {
			incomplete = true
			out = append(out, domain.NewFinding("RPC_EVIDENCE_UNAVAILABLE", "incomplete", "Stored historical event cannot currently be checked; no reorganization is inferred.", nil, []string{t.ID}, t, nil))
			continue
		}
		if h.Hash != t.BlockHash {
			out = append(out, domain.NewFinding("REORG_OBSERVED", "discrepancy", "Previously stored canonical block identity differs from the current canonical header at the same height.", nil, []string{t.ID}, t, h))
		}
	}
	for n, h := range cache {
		again, e := r.Header(ctx, &n)
		if e != nil || again.Hash != h.Hash {
			incomplete = true
			out = append(out, domain.NewFinding("RPC_EVIDENCE_UNAVAILABLE", "incomplete", "Historical canonical view changed during comparison.", nil, nil, h, nil))
		}
	}
	return out, incomplete
}
