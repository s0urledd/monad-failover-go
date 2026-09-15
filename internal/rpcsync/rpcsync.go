// Package rpcsync judges whether this node is in sync from its JSON-RPC
// endpoint, for hosts without monad-status.
//
// eth_syncing is not the answer: the Monad RPC returns false for it
// unconditionally, so a node still catching up looks synced. What can be
// established over RPC is that the node answers on the right chain, that
// its head is close to the heads the public Foundation RPCs report, and
// that its head moves between two readings. All three must hold. A node
// that cannot be reached, or a network that cannot be compared against,
// is "unverified", never "in sync".
//
// The comparison reads the references first and the local head last, so
// the time the reference calls take counts in the node's favour: a node at
// the tip reads level with or ahead of what the references showed, and a
// difference of more than a few blocks is lag, not measurement.
package rpcsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/s0urledd/monad-failover-go/internal/netinfo"
)

// Verdict is the outcome of one check.
type Verdict int

const (
	Unverified Verdict = iota // could not compare: RPC or references unreachable, malformed
	NotInSync                 // compared, and the node is behind, stalled or on another chain
	InSync                    // compared, and the node keeps up with the network
)

// Result carries the verdict and the numbers behind it.
type Result struct {
	Verdict   Verdict
	Detail    string // one line, for the operator
	LocalHead uint64
	NetHead   uint64
}

// Config is one check.
type Config struct {
	Local      string   // this node's JSON-RPC endpoint
	References []string // public endpoints of the same network
	ChainID    uint64   // expected eth_chainId
	MaxBehind  uint64   // blocks the local head may trail the network head
	MaxAhead   uint64   // blocks the local head may lead every reference before the comparison is distrusted
	Interval   time.Duration
	Timeout    time.Duration // per request
	Sleep      func(time.Duration)
}

// DefaultMaxBehind is a couple of seconds of blocks: the local head is
// read after the references, so a node at the tip shows 0 or 1 behind and
// anything past this is real lag. State sync leaves thousands to catch up.
const DefaultMaxBehind = 5

// DefaultMaxAhead: a local head this far past every public RPC means the
// references are stale, not that the node is ahead of the network.
const DefaultMaxAhead = 50

// Verify runs the check: chain, two readings of every head, comparison.
func Verify(c Config) Result {
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if c.MaxBehind == 0 {
		c.MaxBehind = DefaultMaxBehind
	}
	if c.MaxAhead == 0 {
		c.MaxAhead = DefaultMaxAhead
	}
	cl := &client{http: netinfo.Client(c.Timeout)}

	chain, err := cl.chainID(c.Local)
	if err != nil {
		return Result{Verdict: Unverified, Detail: "local RPC did not answer eth_chainId (" + err.Error() + "); the RPC answers only once state sync is done"}
	}
	if chain != c.ChainID {
		return Result{Verdict: NotInSync, Detail: fmt.Sprintf("local RPC is on chain %d, expected %d", chain, c.ChainID)}
	}
	if syncing, err := cl.syncing(c.Local); err == nil && syncing {
		return Result{Verdict: NotInSync, Detail: "local RPC reports eth_syncing in progress"}
	}

	first, err := cl.head(c.Local)
	if err != nil {
		return Result{Verdict: Unverified, Detail: "local RPC did not answer eth_blockNumber (" + err.Error() + ")"}
	}
	refs := cl.referenceHeads(c.References, c.ChainID)
	c.Sleep(c.Interval)
	// References first, the local head last: see the package comment.
	if refs2 := cl.referenceHeads(c.References, c.ChainID); len(refs2) > 0 {
		refs = refs2
	}
	second, err := cl.head(c.Local)
	if err != nil {
		return Result{Verdict: Unverified, Detail: "local RPC stopped answering eth_blockNumber (" + err.Error() + ")"}
	}
	if len(refs) == 0 {
		return Result{Verdict: Unverified, LocalHead: second,
			Detail: "no public RPC of this network could be reached to compare against (" + strings.Join(c.References, ", ") + ")"}
	}
	net := uint64(0)
	for _, h := range refs {
		if h > net {
			net = h
		}
	}
	res := Result{LocalHead: second, NetHead: net}
	switch {
	case second <= first:
		res.Verdict = NotInSync
		res.Detail = fmt.Sprintf("local head did not advance between two readings (%d, %d); network head %d", first, second, net)
	case second+c.MaxBehind < net:
		res.Verdict = NotInSync
		res.Detail = fmt.Sprintf("local head %d is %d blocks behind the network head %d", second, net-second, net)
	case second > net+c.MaxAhead:
		res.Verdict = Unverified
		res.Detail = fmt.Sprintf("local head %d is ahead of every public RPC (%d); cannot compare", second, net)
	default:
		res.Verdict = InSync
		switch {
		case second >= net:
			res.Detail = fmt.Sprintf("local head %d, at the network head (%d), advancing", second, net)
		default:
			res.Detail = fmt.Sprintf("local head %d, network head %d, %d behind, advancing", second, net, net-second)
		}
	}
	return res
}

type client struct{ http *http.Client }

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// call performs one JSON-RPC request and returns the raw result.
func (c *client) call(url, method string) (json.RawMessage, error) {
	body := `{"jsonrpc":"2.0","method":"` + method + `","params":[],"id":1}`
	resp, err := c.http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	var r rpcResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("not a JSON-RPC response")
	}
	if r.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", r.Error.Message)
	}
	if len(r.Result) == 0 {
		return nil, fmt.Errorf("empty result")
	}
	return r.Result, nil
}

func (c *client) quantity(url, method string) (uint64, error) {
	raw, err := c.call(url, method)
	if err != nil {
		return 0, err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("%s: result is not a string", method)
	}
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if s == "" || len(s) > 16 {
		return 0, fmt.Errorf("%s: result is not a quantity", method)
	}
	n, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: result is not a quantity", method)
	}
	return n, nil
}

func (c *client) chainID(url string) (uint64, error) { return c.quantity(url, "eth_chainId") }
func (c *client) head(url string) (uint64, error)    { return c.quantity(url, "eth_blockNumber") }

// syncing is true only for a sync object; false, an error or a malformed
// answer proves nothing and is reported as not syncing.
func (c *client) syncing(url string) (bool, error) {
	raw, err := c.call(url, "eth_syncing")
	if err != nil {
		return false, err
	}
	return bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")), nil
}

// referenceHeads reads the head of every reachable reference on the
// expected chain. One that answers for another chain is left out.
func (c *client) referenceHeads(refs []string, chain uint64) []uint64 {
	var heads []uint64
	for _, u := range refs {
		if id, err := c.chainID(u); err != nil || id != chain {
			continue
		}
		if h, err := c.head(u); err == nil {
			heads = append(heads, h)
		}
	}
	return heads
}

// ChainID is the chain id of a network name, 0 when unknown.
func ChainID(network string) uint64 {
	switch network {
	case "mainnet":
		return 143
	case "testnet":
		return 10143
	}
	return 0
}
