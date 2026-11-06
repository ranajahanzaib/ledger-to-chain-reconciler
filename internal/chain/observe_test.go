package chain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/config"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

const testToken = "0x1111111111111111111111111111111111111111"
const recipient = "0x2222222222222222222222222222222222222222"
const testTx = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testHash = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type mock struct {
	network      string
	receipt      *domain.Receipt
	receiptErr   error
	scanErr      bool
	noncanonical bool
	changing     bool
	calls        atomic.Int32
	headerCalls  map[uint64]int
}

func (m *mock) ChainID(context.Context) (string, error) { return m.network, nil }
func (m *mock) Header(_ context.Context, n *uint64) (domain.Header, error) {
	height := uint64(12)
	if n != nil {
		height = *n
	}
	if m.headerCalls == nil {
		m.headerCalls = map[uint64]int{}
	}
	m.headerCalls[height]++
	h := testHash
	if m.noncanonical && height == 10 {
		h = testTx
	}
	if m.changing && height == 12 && m.headerCalls[height] > 1 {
		h = testTx
	}
	return domain.Header{Number: height, Hash: h}, nil
}
func (m *mock) Receipt(context.Context, string) (*domain.Receipt, error) {
	m.calls.Add(1)
	return m.receipt, m.receiptErr
}
func (m *mock) Logs(_ context.Context, _ string, _ []string, from, to uint64) ([]domain.Transfer, error) {
	if m.scanErr && from == 5 {
		return nil, errors.New("failed chunk")
	}
	if m.receipt != nil && from <= 10 && to >= 10 {
		return m.receipt.Transfers, nil
	}
	return nil, nil
}
func observationFixture() (*mock, config.Config, []domain.Entry, map[string]string) {
	var c config.Config
	c.Network.ChainID = "31337"
	c.Asset.Token = testToken
	c.Policy.MinConfirmations = 3
	c.Discovery.To = "latest"
	c.Discovery.Chunk = 5
	c.Discovery.MaxBlocks = 100
	c.RPC.Concurrency = 2
	t := domain.Transfer{Token: testToken, Tx: testTx, Index: 7, Block: 10, BlockHash: testHash, From: testToken, To: recipient, Amount: "100"}
	m := &mock{network: "31337", receipt: &domain.Receipt{Tx: testTx, Block: 10, BlockHash: testHash, Status: 1, Transfers: []domain.Transfer{t}}}
	entries := []domain.Entry{{Tx: testTx, ChainID: "31337"}, {Tx: testTx, ChainID: "31337"}}
	return m, c, entries, map[string]string{recipient: "alice"}
}
func TestObserve(t *testing.T) {
	for _, name := range []string{"clean", "wrong network", "failed chunk", "noncanonical", "changing head", "missing", "unavailable"} {
		t.Run(name, func(t *testing.T) {
			m, c, entries, owners := observationFixture()
			switch name {
			case "wrong network":
				m.network = "1"
			case "failed chunk":
				m.scanErr = true
			case "noncanonical":
				m.noncanonical = true
			case "changing head":
				m.changing = true
			case "missing":
				m.receipt = nil
				m.receiptErr = ErrMissing
			case "unavailable":
				m.receipt = nil
				m.receiptErr = ErrUnavailable
			}
			ev, e := Observe(context.Background(), m, c, entries, owners)
			if name == "wrong network" {
				if e == nil {
					t.Fatal("wrong network accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if m.calls.Load() != 1 {
				t.Fatal("receipt requests not deduplicated")
			}
			switch name {
			case "clean":
				if !ev.Coverage.Complete || !ev.Consistent || len(ev.Transfers) != 1 || ev.Transfers[0].Confirmations != 3 {
					t.Fatalf("%+v", ev)
				}
			case "failed chunk":
				if ev.Coverage.Complete || len(ev.Coverage.Failed) != 1 {
					t.Fatal("failed chunk hidden")
				}
			case "noncanonical":
				if ev.Receipts[testTx].Canonical || ev.Coverage.Complete {
					t.Fatal("noncanonical accepted")
				}
			case "changing head":
				if ev.Consistent {
					t.Fatal("head replacement ignored")
				}
			case "missing":
				if ev.Receipts[testTx].State != "not_observed" {
					t.Fatal("missing != unavailable")
				}
			case "unavailable":
				if ev.Receipts[testTx].State != "unavailable" {
					t.Fatal("error != missing")
				}
			}
		})
	}
}
func TestDecodeLog(t *testing.T) {
	l := types.Log{Address: common.HexToAddress(testToken), Topics: []common.Hash{TransferTopic, common.BytesToHash(common.HexToAddress(testToken).Bytes()), common.BytesToHash(common.HexToAddress(recipient).Bytes())}, Data: common.LeftPadBytes(new(big.Int).Lsh(big.NewInt(1), 255).Bytes(), 32), TxHash: common.HexToHash(testTx), BlockHash: common.HexToHash(testHash), BlockNumber: 10, Index: 7}
	v, e := DecodeLog(l, "31337")
	if e != nil || v.Index != 7 || len(v.Amount) < 70 {
		t.Fatal(v, e)
	}
	for _, kind := range []string{"short data", "topics", "padding", "removed", "identity"} {
		bad := l
		bad.Topics = append([]common.Hash{}, l.Topics...)
		switch kind {
		case "short data":
			bad.Data = l.Data[:31]
		case "topics":
			bad.Topics = l.Topics[:2]
		case "padding":
			bad.Topics[1][0] = 1
		case "removed":
			bad.Removed = true
		case "identity":
			bad.TxHash = common.Hash{}
		}
		if _, e = DecodeLog(bad, "31337"); e == nil {
			t.Fatal("accepted", kind)
		}
	}
}
func TestRPCBoundedRetriesAndRedaction(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
		fmt.Fprint(w, "secret-token")
	}))
	defer s.Close()
	rpc, e := Dial(context.Background(), s.URL+"/secret-token", time.Second, 2)
	if e != nil {
		t.Fatal(e)
	}
	defer rpc.Close()
	_, e = rpc.ChainID(context.Background())
	if e == nil || calls.Load() != 3 || strings.Contains(e.Error(), "secret") {
		t.Fatal(calls.Load(), e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = rpc.ChainID(ctx)
	if e == nil {
		t.Fatal("cancel ignored")
	}
}
func TestRPCMalformedAndMissing(t *testing.T) {
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"result":null}`, `{"jsonrpc":"2.0","id":1,"result":{"status":"0x1"}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		r, e := Dial(context.Background(), s.URL, time.Second, 0)
		if e != nil {
			t.Fatal(e)
		}
		_, e = r.Receipt(context.Background(), testTx)
		if e == nil {
			t.Fatal("malformed response accepted")
		}
		r.Close()
		s.Close()
	}
}

func TestRPCPerRequestTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer s.Close()
	rpc, e := Dial(context.Background(), s.URL, 20*time.Millisecond, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer rpc.Close()
	start := time.Now()
	if _, e = rpc.ChainID(context.Background()); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("per-request timeout ignored")
	}
}
func TestRPCResponseSizeLimit(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(MaxResponseBytes+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()
	rpc, e := Dial(context.Background(), s.URL, time.Second, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer rpc.Close()
	if _, e = rpc.ChainID(context.Background()); !errors.Is(e, ErrUnavailable) {
		t.Fatal("oversized response accepted", e)
	}
}
func TestMalformedReceiptCannotCreateEligibleDiscovery(t *testing.T) {
	m, c, e, o := observationFixture()
	m.receipt.Malformed = true
	ev, err := Observe(context.Background(), m, c, e, o)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Coverage.Complete {
		t.Fatal("malformed receipt accepted as discovery proof")
	}
	for _, v := range ev.Transfers {
		if v.Canonical {
			t.Fatal("malformed evidence retained as canonical transfer")
		}
	}
}
