package config

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/domain"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Network struct {
		ChainID string `yaml:"chain_id" json:"chain_id"`
		RPCURL  string `yaml:"rpc_url" json:"rpc_url"`
	} `yaml:"network" json:"network"`
	Asset struct {
		Token    string `yaml:"token_address" json:"token_address"`
		Symbol   string `yaml:"symbol" json:"symbol"`
		Decimals int    `yaml:"decimals" json:"decimals"`
	} `yaml:"asset" json:"asset"`
	Policy struct {
		MinConfirmations uint64 `yaml:"min_confirmations" json:"min_confirmations"`
		IncludeMint      bool   `yaml:"include_mint_transfers" json:"include_mint_transfers"`
	} `yaml:"policy" json:"policy"`
	Discovery struct {
		From      uint64 `yaml:"from_block" json:"from_block"`
		To        string `yaml:"to_block" json:"to_block"`
		Chunk     uint64 `yaml:"chunk_size" json:"chunk_size"`
		MaxBlocks uint64 `yaml:"max_blocks" json:"max_blocks"`
	} `yaml:"discovery" json:"discovery"`
	RPC struct {
		Timeout     int `yaml:"timeout_seconds" json:"timeout_seconds"`
		RunTimeout  int `yaml:"run_timeout_seconds" json:"run_timeout_seconds"`
		Concurrency int `yaml:"max_concurrency" json:"max_concurrency"`
		Retries     int `yaml:"max_retries" json:"max_retries"`
	} `yaml:"rpc" json:"rpc"`
	Output struct {
		Format string `yaml:"format" json:"format"`
	} `yaml:"output" json:"output"`
}

func Load(path, override string) (Config, error) {
	var c Config
	c.Policy.MinConfirmations = 3
	c.Discovery.To = "latest"
	c.Discovery.Chunk = 500
	c.Discovery.MaxBlocks = 100000
	c.RPC.Timeout = 10
	c.RPC.RunTimeout = 120
	c.RPC.Concurrency = 8
	c.RPC.Retries = 3
	c.Output.Format = "json"
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() > 1<<20 {
		return c, fmt.Errorf("configuration exceeds 1 MiB or cannot be inspected")
	}
	d := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	d.KnownFields(true)
	if e = d.Decode(&c); e != nil {
		return c, fmt.Errorf("invalid configuration YAML")
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return c, fmt.Errorf("configuration must contain one YAML document")
	}
	if override == "" {
		override = os.Getenv("LEDGERCHAIN_RPC_URL")
	}
	if override != "" {
		c.Network.RPCURL = override
	}
	return c, c.Validate()
}
func (c *Config) Validate() error {
	var e error
	if c.Network.ChainID, e = domain.ChainID(c.Network.ChainID); e != nil {
		return e
	}
	if c.Asset.Token, e = domain.Address(c.Asset.Token); e != nil {
		return e
	}
	u, e := url.Parse(c.Network.RPCURL)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("rpc_url must be an HTTP(S) endpoint")
	}
	if c.Policy.MinConfirmations < 1 || c.Policy.MinConfirmations > 1000000 {
		return fmt.Errorf("min_confirmations must be in 1..1000000")
	}
	if c.Asset.Decimals < 0 || c.Asset.Decimals > 255 {
		return fmt.Errorf("decimals must be in 0..255")
	}
	if c.Discovery.Chunk < 1 || c.Discovery.Chunk > 10000 || c.Discovery.MaxBlocks < 1 || c.Discovery.MaxBlocks > 1000000 {
		return fmt.Errorf("discovery chunk_size must be in 1..10000; max_blocks in 1..1000000")
	}
	if c.Discovery.To != "latest" {
		var to uint64
		if _, e = fmt.Sscan(c.Discovery.To, &to); e != nil || fmt.Sprint(to) != c.Discovery.To || to < c.Discovery.From {
			return fmt.Errorf("invalid discovery to_block")
		}
		if to-c.Discovery.From >= c.Discovery.MaxBlocks {
			return fmt.Errorf("scan exceeds max_blocks")
		}
	}
	if c.RPC.Timeout < 1 || c.RPC.Timeout > 300 || c.RPC.RunTimeout < 1 || c.RPC.RunTimeout > 3600 || c.RPC.Concurrency < 1 || c.RPC.Concurrency > 64 || c.RPC.Retries < 0 || c.RPC.Retries > 5 {
		return fmt.Errorf("RPC limits out of range (timeout 1..300, run timeout 1..3600, concurrency 1..64, retries 0..5)")
	}
	if c.Output.Format != "json" && c.Output.Format != "text" {
		return fmt.Errorf("output format must be json or text")
	}
	return nil
}

// SourceID deliberately exposes no URL components, including provider API-key paths.
func SourceID(endpoint string) string {
	return "rpc:sha256:" + domain.Fingerprint(strings.TrimSpace(endpoint))
}
func (c Config) Fingerprint() string {
	c.Network.RPCURL = SourceID(c.Network.RPCURL)
	return domain.Fingerprint(c)
}

// Snapshot replaces the endpoint with a nonreversible source identifier.
func (c Config) Snapshot() Config { c.Network.RPCURL = SourceID(c.Network.RPCURL); return c }
