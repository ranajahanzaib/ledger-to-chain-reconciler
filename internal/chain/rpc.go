// Package chain provides read-only EVM observations. It contains no signing or
// transaction broadcasting capability.
package chain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
)

var ErrMissing = errors.New("receipt not observed")
var ErrUnavailable = errors.New("RPC evidence unavailable")
var ErrMalformed = errors.New("malformed RPC evidence")
var TransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

const MaxLogs = 100000

type Reader interface {
	ChainID(context.Context) (string, error)
	Header(context.Context, *uint64) (domain.Header, error)
	Receipt(context.Context, string) (*domain.Receipt, error)
	Logs(context.Context, string, []string, uint64, uint64) ([]domain.Transfer, error)
}
type RPC struct {
	client  *ethclient.Client
	timeout time.Duration
	retries int
}

func Dial(ctx context.Context, endpoint string, timeout time.Duration, retries int) (*RPC, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: limitedTransport{transport}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("RPC redirects disabled") }}
	raw, e := gethrpc.DialOptions(ctx, endpoint, gethrpc.WithHTTPClient(client))
	if e != nil {
		return nil, ErrUnavailable
	}
	return &RPC{ethclient.NewClient(raw), timeout, retries}, nil
}
func (r *RPC) Close() { r.client.Close() }
func transient(e error) bool {
	if errors.Is(e, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(e, &ne) {
		return true
	}
	// Provider messages are inspected but never returned: they may embed credentials.
	s := strings.ToLower(e.Error())
	return strings.Contains(s, "429") || strings.Contains(s, "503") || strings.Contains(s, "502") || strings.Contains(s, "rate limit") || strings.Contains(s, "timeout") || strings.Contains(s, "temporar")
}
func request[T any](ctx context.Context, r *RPC, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt <= r.retries; attempt++ {
		sub, cancel := context.WithTimeout(ctx, r.timeout)
		v, e := fn(sub)
		cancel()
		if e == nil {
			return v, nil
		}
		if errors.Is(e, ethereum.NotFound) {
			return zero, ErrMissing
		}
		if ctx.Err() != nil {
			return zero, ErrUnavailable
		}
		if !transient(e) || attempt == r.retries {
			return zero, ErrUnavailable
		}
		delay := time.Duration(100*(1<<attempt))*time.Millisecond + time.Duration(rand.IntN(50))*time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ErrUnavailable
		case <-timer.C:
		}
	}
	return zero, ErrUnavailable
}
func (r *RPC) ChainID(ctx context.Context) (string, error) {
	n, e := request(ctx, r, r.client.ChainID)
	if e != nil {
		return "", e
	}
	if n == nil || n.Sign() <= 0 {
		return "", ErrMalformed
	}
	return n.String(), nil
}
func (r *RPC) Header(ctx context.Context, n *uint64) (domain.Header, error) {
	var num *big.Int
	if n != nil {
		num = new(big.Int).SetUint64(*n)
	}
	h, e := request(ctx, r, func(c context.Context) (*types.Header, error) { return r.client.HeaderByNumber(c, num) })
	if e != nil {
		return domain.Header{}, e
	}
	if h == nil || h.Number == nil || !h.Number.IsUint64() || (n != nil && h.Number.Uint64() != *n) {
		return domain.Header{}, ErrMalformed
	}
	return domain.Header{Number: h.Number.Uint64(), Hash: strings.ToLower(h.Hash().Hex()), ParentHash: strings.ToLower(h.ParentHash.Hex())}, nil
}
func DecodeLog(l types.Log, chainID string) (domain.Transfer, error) {
	if len(l.Topics) != 3 || l.Topics[0] != TransferTopic || len(l.Data) != 32 || l.Removed {
		return domain.Transfer{}, ErrMalformed
	}
	for _, t := range l.Topics[1:] {
		for _, b := range t[:12] {
			if b != 0 {
				return domain.Transfer{}, ErrMalformed
			}
		}
	}
	if l.TxHash == (common.Hash{}) || l.BlockHash == (common.Hash{}) {
		return domain.Transfer{}, ErrMalformed
	}
	tx := strings.ToLower(l.TxHash.Hex())
	return domain.Transfer{ID: domain.EventID(chainID, tx, uint64(l.Index)), ChainID: chainID, Token: strings.ToLower(l.Address.Hex()), Tx: tx, Index: uint64(l.Index), Block: l.BlockNumber, BlockHash: strings.ToLower(l.BlockHash.Hex()), TransactionIndex: uint64(l.TxIndex), From: strings.ToLower(common.BytesToAddress(l.Topics[1][12:]).Hex()), To: strings.ToLower(common.BytesToAddress(l.Topics[2][12:]).Hex()), Amount: new(big.Int).SetBytes(l.Data).String()}, nil
}
func (r *RPC) Receipt(ctx context.Context, tx string) (*domain.Receipt, error) {
	raw, e := request(ctx, r, func(c context.Context) (*types.Receipt, error) {
		return r.client.TransactionReceipt(c, common.HexToHash(tx))
	})
	if e != nil {
		return nil, e
	}
	if raw == nil || raw.BlockNumber == nil || !raw.BlockNumber.IsUint64() || raw.TxHash != common.HexToHash(tx) || raw.BlockHash == (common.Hash{}) || raw.Status > 1 || len(raw.Logs) > MaxLogs {
		return nil, ErrMalformed
	}
	out := &domain.Receipt{Tx: tx, Block: raw.BlockNumber.Uint64(), BlockHash: strings.ToLower(raw.BlockHash.Hex()), Status: raw.Status, TransactionIndex: uint64(raw.TransactionIndex), Transfers: []domain.Transfer{}}
	seen := map[uint]bool{}
	for _, l := range raw.Logs {
		if l == nil || l.TxHash != raw.TxHash || l.BlockHash != raw.BlockHash || l.BlockNumber != out.Block || l.TxIndex != raw.TransactionIndex || seen[l.Index] {
			return nil, ErrMalformed
		}
		seen[l.Index] = true
		if len(l.Topics) == 0 || l.Topics[0] != TransferTopic {
			continue
		}
		t, e := DecodeLog(*l, "")
		if e != nil {
			out.Malformed = true
			continue
		}
		out.Transfers = append(out.Transfers, t)
	}
	return out, nil
}
func (r *RPC) Logs(ctx context.Context, token string, recipients []string, from, to uint64) ([]domain.Transfer, error) {
	topics := make([]common.Hash, len(recipients))
	for i, a := range recipients {
		topics[i] = common.BytesToHash(common.HexToAddress(a).Bytes())
	}
	logs, e := request(ctx, r, func(c context.Context) ([]types.Log, error) {
		return r.client.FilterLogs(c, ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(from), ToBlock: new(big.Int).SetUint64(to), Addresses: []common.Address{common.HexToAddress(token)}, Topics: [][]common.Hash{{TransferTopic}, nil, topics}})
	})
	if e != nil {
		return nil, e
	}
	if len(logs) > MaxLogs {
		return nil, fmt.Errorf("log result exceeds %d events", MaxLogs)
	}
	out := make([]domain.Transfer, 0, len(logs))
	owners := map[string]bool{}
	for _, a := range recipients {
		owners[a] = true
	}
	for _, l := range logs {
		t, e := DecodeLog(l, "")
		if e != nil {
			return nil, e
		}
		if t.Token != token || !owners[t.To] || t.Block < from || t.Block > to {
			return nil, ErrMalformed
		}
		out = append(out, t)
	}
	return out, nil
}

// Limit decoded HTTP bodies, including decompressed responses, before the RPC
// JSON decoder can allocate an unbounded provider-controlled result.
const MaxResponseBytes int64 = 32 << 20

type limitedTransport struct{ base http.RoundTripper }

func (t limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, e := t.base.RoundTrip(req)
	if e != nil {
		return nil, e
	}
	if resp.ContentLength > MaxResponseBytes {
		resp.Body.Close()
		return nil, errors.New("RPC response exceeds 32 MiB")
	}
	resp.Body = &limitedBody{Reader: io.LimitReader(resp.Body, MaxResponseBytes), closer: resp.Body}
	return resp, nil
}

type limitedBody struct {
	io.Reader
	closer io.Closer
}

func (b *limitedBody) Close() error { return b.closer.Close() }
