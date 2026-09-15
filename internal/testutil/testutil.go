// Package testutil builds the fixtures the test suite shares: Foundation
// snapshots and uptime responses shaped like the live endpoints', so every
// guard can be driven from a table.
package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// MockSecp and MockBls are the public keys the mock monad-keystore reports
// for the test suite's validator IKMs.
const (
	SecpIKM = "1111111111111111111111111111111111111111111111111111111111111111"
	BlsIKM  = "2222222222222222222222222222222222222222222222222222222222222222"
	// the mock keystore swaps each IKM digit (1 -> e, 2 -> d)
	MockSecp = "02eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	MockBls  = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	// OtherSecp and OtherBls belong to a different validator.
	OtherSecp = "03ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	OtherBls  = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

// SnapshotOpts select which guard a snapshot should trip.
type SnapshotOpts struct {
	Network  string // default testnet
	ChainID  string // default by network
	Stale    bool   // fetched far past the freshness limit
	NoPeer   bool   // validator present, no name record
	Dup      bool   // same secp listed twice
	BadBLS   bool   // entry whose BLS does not match
	NoBLS    bool   // entry without a BLS key
	Reorder  bool   // peer before secp
	Truncate bool   // cut off after the target object closes
	Seq      string // default 7
	Secp     string // default MockSecp
	Bls      string // default MockBls
}

// Snapshot renders a validator snapshot document.
func Snapshot(o SnapshotOpts) string {
	net := o.Network
	if net == "" {
		net = "testnet"
	}
	chain := o.ChainID
	if chain == "" {
		if net == "mainnet" {
			chain = "143"
		} else {
			chain = "10143"
		}
	}
	epoch := time.Now().Unix()
	if o.Stale {
		epoch -= 864000
	}
	seq := o.Seq
	if seq == "" {
		seq = "7"
	}
	secp := o.Secp
	if secp == "" {
		secp = MockSecp
	}
	bls := o.Bls
	if bls == "" {
		bls = MockBls
	}
	if o.BadBLS {
		bls = OtherBls
	}
	if o.NoBLS {
		bls = ""
	}
	peer := fmt.Sprintf(`, "peer": {"address": "198.51.100.5", "tcp_port": 8000, "udp_port": 8000, "auth_port": 8001, "record_seq_num": %s, "name_record_sig": "0xdead"}`, seq)
	if o.NoPeer {
		peer = ""
	}
	var entry string
	if o.Reorder {
		entry = fmt.Sprintf(`{"id": 1, "name": "MockVal"%s, "secp": "%s", "bls": "%s"}`, peer, secp, bls)
	} else {
		entry = fmt.Sprintf(`{"id": 1, "name": "MockVal", "secp": "%s", "bls": "%s"%s}`, secp, bls, peer)
	}
	other := `{"id": 2, "name": "Other", "secp": "` + OtherSecp + `", "bls": "` + OtherBls + `", "peer": {"record_seq_num": 3}}`
	body := entry + ", " + other
	if o.Dup {
		body = entry + ", " + entry
	}
	if o.Truncate {
		return fmt.Sprintf(`{"chain_id": "%s", "count": 2, "expected_version": "0.16.1", "fetched_at_epoch": %d, "network": "%s", "validators": [%s, {"id": 3, "secp": "02cut`,
			chain, epoch, net, entry)
	}
	return fmt.Sprintf(`{"chain_id": "%s", "count": 2, "expected_version": "0.16.1", "fetched_at_epoch": %d, "network": "%s", "validators": [%s]}`,
		chain, epoch, net, body)
}

// UptimeActive is the default healthy uptime response for secp.
func UptimeActive(secp string) string {
	return fmt.Sprintf(`{"success": true, "uptime": {"validator_name": "MockVal", "secp_address": "%s", "status": "active", "window_hours": 24, "finalized_count": 356, "timeout_count": 0, "uptime_percent": 100, "last_round": 87138952}}`, secp)
}

// RPCNode is a JSON-RPC stand-in for a Monad node: eth_chainId,
// eth_blockNumber (the head advances by Step on every reading) and
// eth_syncing (false, as the real RPC answers, unless Syncing is set).
type RPCNode struct {
	mu        sync.Mutex
	Chain     string // hex quantity, e.g. "0x279f"
	Head      uint64
	Step      uint64
	Syncing   bool // answer a sync object instead of false
	Down      bool // answer 500
	Malformed bool // answer something that is not JSON-RPC
	DownAfter int  // answer this many requests, then 500 (0: no limit)
	calls     int
}

// TestnetChain and MainnetChain are the hex chain ids the RPC reports.
const (
	TestnetChain = "0x279f"
	MainnetChain = "0x8f"
)

func (n *RPCNode) serve(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if n.Down || (n.DownAfter > 0 && n.calls > n.DownAfter) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	if n.Malformed {
		_, _ = w.Write([]byte("<html>not an rpc</html>"))
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(body, &req)
	var result string
	switch req.Method {
	case "eth_chainId":
		result = `"` + n.Chain + `"`
	case "eth_blockNumber":
		result = fmt.Sprintf(`"0x%x"`, n.Head)
		n.Head += n.Step
	case "eth_syncing":
		result = "false"
		if n.Syncing {
			result = fmt.Sprintf(`{"startingBlock":"0x0","currentBlock":"0x%x","highestBlock":"0x%x"}`, n.Head, n.Head+5000)
		}
	default:
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
		return
	}
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + result + `}`))
}

// Endpoints is one HTTP server standing in for ifconfig.me, the Foundation
// bucket, the uptime API, this node's RPC and two public RPCs. Each field
// can be changed between requests.
type Endpoints struct {
	Server *httptest.Server

	IP       string // "" answers 500
	Snapshot string // "" answers 500
	Uptime   string // "" answers 500

	Local, Ref1, Ref2 *RPCNode
}

// NewEndpoints starts the server with healthy defaults: a testnet node a
// couple of blocks behind two public RPCs, all advancing.
func NewEndpoints() *Endpoints {
	e := &Endpoints{IP: "203.0.113.7", Snapshot: Snapshot(SnapshotOpts{}), Uptime: UptimeActive(MockSecp),
		Local: &RPCNode{Chain: TestnetChain, Head: 1000, Step: 1},
		Ref1:  &RPCNode{Chain: TestnetChain, Head: 1002, Step: 1},
		Ref2:  &RPCNode{Chain: TestnetChain, Head: 1003, Step: 1}}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body string
		switch {
		case r.URL.Path == "/ip":
			body = e.IP
		case strings.HasSuffix(r.URL.Path, ".json"):
			body = e.Snapshot
		case strings.HasPrefix(r.URL.Path, "/uptime/"):
			body = e.Uptime
		case r.URL.Path == "/rpc/local":
			e.Local.serve(w, r)
			return
		case r.URL.Path == "/rpc/ref1":
			e.Ref1.serve(w, r)
			return
		case r.URL.Path == "/rpc/ref2":
			e.Ref2.serve(w, r)
			return
		}
		if body == "" {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	return e
}

// RPCLocalURL and RPCReferenceURLs are the sync-check endpoints.
func (e *Endpoints) RPCLocalURL() string { return e.Server.URL + "/rpc/local" }
func (e *Endpoints) RPCReferenceURLs() []string {
	return []string{e.Server.URL + "/rpc/ref1", e.Server.URL + "/rpc/ref2"}
}

// IPURL, FoundationBase and UptimeBase are the values for the tool's endpoint overrides.
func (e *Endpoints) IPURL() string          { return e.Server.URL + "/ip" }
func (e *Endpoints) FoundationBase() string { return e.Server.URL }
func (e *Endpoints) UptimeBase() string     { return e.Server.URL + "/uptime" }

// Close stops the server.
func (e *Endpoints) Close() { e.Server.Close() }
