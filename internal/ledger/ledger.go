// Package ledger reads immutable source snapshots and an independent ownership registry.
package ledger

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
	_ "modernc.org/sqlite"
)

const MaxRows = 100000

var Columns = []string{"entry_id", "account_id", "chain_id", "token_address", "deposit_address", "expected_amount_base_units", "tx_hash", "log_index", "credit_status", "credited_at_block", "recorded_block_hash"}

func number(s string) (*uint64, error) {
	if s == "" {
		return nil, nil
	}
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil || fmt.Sprint(n) != s {
		return nil, fmt.Errorf("invalid unsigned block/index %q", s)
	}
	return &n, nil
}
func parse(v []string, source string) (r domain.Entry, err error) {
	for i := range v {
		v[i] = strings.TrimSpace(v[i])
	}
	r = domain.Entry{EntryID: v[0], AccountID: v[1], Status: v[8], Source: source}
	if r.EntryID == "" || r.AccountID == "" {
		return r, fmt.Errorf("entry_id and account_id required")
	}
	if r.ChainID, err = domain.ChainID(v[2]); err != nil {
		return
	}
	if r.Token, err = domain.Address(v[3]); err != nil {
		return
	}
	if r.Deposit, err = domain.Address(v[4]); err != nil {
		return
	}
	if r.Amount, err = domain.Amount(v[5], true); err != nil {
		return
	}
	if v[6] != "" {
		if r.Tx, err = domain.Hash(v[6]); err != nil {
			return
		}
	}
	if r.LogIndex, err = number(v[7]); err != nil {
		return
	}
	if r.CreditedAt, err = number(v[9]); err != nil {
		return
	}
	if v[10] != "" {
		if r.RecordedHash, err = domain.Hash(v[10]); err != nil {
			return
		}
	}
	if r.Status != "expected" && r.Status != "credited" {
		err = fmt.Errorf("unsupported credit_status %q", r.Status)
	}
	if r.Tx == "" && r.LogIndex != nil {
		err = fmt.Errorf("log_index requires tx_hash")
	}
	return
}
func ReadCSV(path string) ([]domain.Entry, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() > 64<<20 {
		return nil, fmt.Errorf("CSV exceeds 64 MiB or cannot be inspected")
	}
	r := csv.NewReader(io.LimitReader(f, 64<<20))
	head, e := r.Read()
	if e != nil {
		return nil, e
	}
	idx := map[string]int{}
	for i, k := range head {
		if _, ok := idx[k]; ok {
			return nil, fmt.Errorf("duplicate column %q", k)
		}
		idx[k] = i
	}
	for _, k := range Columns {
		if _, ok := idx[k]; !ok {
			return nil, fmt.Errorf("missing column %q", k)
		}
	}
	var out []domain.Entry
	seen := map[string]bool{}
	for row := 2; ; row++ {
		v, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("row %d: %w", row, e)
		}
		ordered := make([]string, len(Columns))
		for i, k := range Columns {
			ordered[i] = v[idx[k]]
		}
		entry, e := parse(ordered, fmt.Sprintf("%s:row:%d", path, row))
		if e != nil {
			return nil, fmt.Errorf("row %d: %w", row, e)
		}
		if seen[entry.EntryID] {
			return nil, fmt.Errorf("row %d: duplicate entry_id %q", row, entry.EntryID)
		}
		seen[entry.EntryID] = true
		out = append(out, entry)
		if len(out) > MaxRows {
			return nil, fmt.Errorf("source exceeds %d rows", MaxRows)
		}
	}
	return out, nil
}
func ReadSQLite(ctx context.Context, path string) ([]domain.Entry, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var invalid int
	if e = db.QueryRowContext(ctx, "SELECT count(*) FROM ledger_entries WHERE typeof(expected_amount_base_units) != ?", "text").Scan(&invalid); e != nil {
		return nil, e
	}
	if invalid != 0 {
		return nil, fmt.Errorf("SQLite amounts must be stored as TEXT decimal integers")
	}
	rows, e := db.QueryContext(ctx, "SELECT "+strings.Join(Columns, ",")+" FROM ledger_entries ORDER BY entry_id LIMIT ?", MaxRows+1)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []domain.Entry
	seen := map[string]bool{}
	for rows.Next() {
		v := make([]sql.NullString, len(Columns))
		dest := make([]any, len(v))
		for i := range v {
			dest[i] = &v[i]
		}
		if e = rows.Scan(dest...); e != nil {
			return nil, e
		}
		ss := make([]string, len(v))
		for i := range v {
			ss[i] = v[i].String
		}
		entry, e := parse(ss, fmt.Sprintf("%s:entry:%s", path, ss[0]))
		if e != nil {
			return nil, fmt.Errorf("entry %q: %w", ss[0], e)
		}
		if seen[entry.EntryID] {
			return nil, fmt.Errorf("duplicate entry_id %q", entry.EntryID)
		}
		seen[entry.EntryID] = true
		out = append(out, entry)
		if len(out) > MaxRows {
			return nil, fmt.Errorf("source exceeds row limit")
		}
	}
	return out, rows.Err()
}
func ReadOwners(path string) (map[string]string, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() > 16<<20 {
		return nil, fmt.Errorf("ownership CSV exceeds 16 MiB or cannot be inspected")
	}
	r := csv.NewReader(io.LimitReader(f, 16<<20))
	h, e := r.Read()
	if e != nil {
		return nil, e
	}
	if len(h) != 2 || h[0] != "account_id" || h[1] != "deposit_address" {
		return nil, fmt.Errorf("ownership header must be account_id,deposit_address")
	}
	out := map[string]string{}
	for row := 2; ; row++ {
		v, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("ownership row %d: %w", row, e)
		}
		a, e := domain.Address(strings.TrimSpace(v[1]))
		if e != nil {
			return nil, fmt.Errorf("ownership row %d: %w", row, e)
		}
		account := strings.TrimSpace(v[0])
		if account == "" {
			return nil, fmt.Errorf("ownership row %d: account_id required", row)
		}
		if prior, ok := out[a]; ok && prior != account {
			return nil, fmt.Errorf("ownership row %d: conflicting address ownership", row)
		}
		out[a] = account
		if row > MaxRows {
			return nil, fmt.Errorf("ownership exceeds row limit")
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ownership registry empty")
	}
	return out, nil
}
