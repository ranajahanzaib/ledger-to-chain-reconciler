package ledger

import (
	"context"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if e := os.WriteFile(p, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func validRow() string {
	return "entry,alice,31337,0x1111111111111111111111111111111111111111,0x2222222222222222222222222222222222222222,18446744073709551616,,,expected,,\n"
}
func TestSourceValidation(t *testing.T) {
	head := strings.Join(Columns, ",") + "\n"
	for _, tt := range []struct{ name, body string }{{"valid", validRow()}, {"negative", strings.Replace(validRow(), "18446744073709551616", "-1", 1)}, {"fraction", strings.Replace(validRow(), "18446744073709551616", "1.5", 1)}, {"hash", strings.Replace(validRow(), ",,,expected", ",0x12,,expected", 1)}, {"status", strings.Replace(validRow(), "expected", "pending", 1)}, {"duplicate", validRow() + validRow()}, {"address", strings.Replace(validRow(), "0x2222222222222222222222222222222222222222", "bad", 1)}} {
		t.Run(tt.name, func(t *testing.T) {
			_, e := ReadCSV(write(t, "ledger.csv", head+tt.body))
			if (e == nil) != (tt.name == "valid") {
				t.Fatal(e)
			}
			if e != nil && !strings.Contains(e.Error(), "row") {
				t.Fatal("not row specific", e)
			}
		})
	}
	if _, e := ReadCSV(write(t, "bad.csv", "entry_id\none\n")); e == nil {
		t.Fatal("missing columns accepted")
	}
}
func TestSQLiteParityAndIsolation(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "source.sqlite")
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	defs := []string{}
	for _, c := range Columns {
		defs = append(defs, c+" TEXT")
	}
	if _, e = db.Exec("CREATE TABLE ledger_entries (" + strings.Join(defs, ",") + ")"); e != nil {
		t.Fatal(e)
	}
	v, _ := csv.NewReader(strings.NewReader(validRow())).Read()
	args := make([]any, len(v))
	for i := range v {
		args[i] = v[i]
	}
	if _, e = db.Exec("INSERT INTO ledger_entries VALUES (?,?,?,?,?,?,?,?,?,?,?)", args...); e != nil {
		t.Fatal(e)
	}
	db.Close()
	before, _ := os.ReadFile(p)
	sq, e := ReadSQLite(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	cs, e := ReadCSV(write(t, "ledger.csv", strings.Join(Columns, ",")+"\n"+validRow()))
	if e != nil {
		t.Fatal(e)
	}
	sq[0].Source = ""
	cs[0].Source = ""
	if !reflect.DeepEqual(sq, cs) {
		t.Fatal(sq, cs)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("source mutated")
	}
	if _, e = ReadSQLite(ctx, p+"missing"); e == nil {
		t.Fatal("missing db accepted")
	}
	if _, e = os.Stat(p + "missing"); !os.IsNotExist(e) {
		t.Fatal("read created database")
	}
}
func TestOwnership(t *testing.T) {
	head := "account_id,deposit_address\n"
	row := "alice,0x2222222222222222222222222222222222222222\n"
	if _, e := ReadOwners(write(t, "accounts.csv", head+row)); e != nil {
		t.Fatal(e)
	}
	for _, body := range []string{head + row + strings.Replace(row, "alice", "bob", 1), head + "alice,bad\n", head} {
		if _, e := ReadOwners(write(t, "accounts.csv", body)); e == nil {
			t.Fatal("invalid registry accepted")
		}
	}
}
