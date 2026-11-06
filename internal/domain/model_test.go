package domain

import (
	"strings"
	"testing"
)

func TestAmount(t *testing.T) {
	for _, s := range []string{"-1", "0", "1.0", "1e6", "0x10", "", "+1", strings.Repeat("9", 79)} {
		if _, e := Amount(s, true); e == nil {
			t.Errorf("accepted %q", s)
		}
	}
	for _, s := range []string{"1", "000123", "18446744073709551616"} {
		if _, e := Amount(s, true); e != nil {
			t.Fatal(e)
		}
	}
	if s, e := Amount("0", false); e != nil || s != "0" {
		t.Fatal("zero event rejected")
	}
}
func TestIdentities(t *testing.T) {
	if a, e := Address("0x" + strings.Repeat("A", 40)); e != nil || a != "0x"+strings.Repeat("a", 40) {
		t.Fatal(a, e)
	}
	for _, s := range []string{"0x12", strings.Repeat("a", 40), "0x" + strings.Repeat("g", 40)} {
		if _, e := Address(s); e == nil {
			t.Fatal(s)
		}
	}
	if _, e := Hash("0x" + strings.Repeat("a", 63)); e == nil {
		t.Fatal("short hash accepted")
	}
	if _, e := ChainID("0"); e == nil {
		t.Fatal("zero chain accepted")
	}
}
