package rpcsync

import (
	"strings"
	"testing"
	"time"

	"github.com/s0urledd/monad-failover-go/internal/testutil"
)

func check(t *testing.T, e *testutil.Endpoints) Result {
	t.Helper()
	return Verify(Config{
		Local: e.RPCLocalURL(), References: e.RPCReferenceURLs(), ChainID: 10143,
		Interval: 0, Timeout: 2 * time.Second, Sleep: func(time.Duration) {},
	})
}

func TestHealthyNodeIsInSync(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	res := check(t, e)
	if res.Verdict != InSync || !strings.Contains(res.Detail, "advancing") {
		t.Fatalf("%+v", res)
	}
}

// A node at the tip reads level with the references, whatever their lead
// at the first reading, because the local head is read last.
func TestNodeAtTheTipReadsLevel(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Head, e.Ref1.Head, e.Ref2.Head = 1000, 1000, 1000
	res := check(t, e)
	if res.Verdict != InSync || !strings.Contains(res.Detail, "at the network head") {
		t.Fatalf("%+v", res)
	}
}

// Five blocks is the line: a node six behind after the references were
// read is lagging, not mis-measured.
func TestSixBlocksBehindIsLag(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Head, e.Ref1.Head, e.Ref2.Head = 1000, 1006, 1006
	// each head advances by one per reading: the second round reads the
	// references at 1007 and then the local head at 1001, six behind
	res := check(t, e)
	if res.Verdict != NotInSync || !strings.Contains(res.Detail, "6 blocks behind") {
		t.Fatalf("%+v", res)
	}
	e2 := testutil.NewEndpoints()
	defer e2.Close()
	e2.Local.Head, e2.Ref1.Head, e2.Ref2.Head = 1000, 1005, 1005
	if res := check(t, e2); res.Verdict != InSync || !strings.Contains(res.Detail, "5 behind") {
		t.Fatalf("%+v", res)
	}
}

// eth_syncing = false proves nothing: a node whose head does not move is
// not in sync, whatever it says.
func TestStalledHeadIsNotInSyncDespiteSyncingFalse(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Step = 0
	res := check(t, e)
	if res.Verdict != NotInSync || !strings.Contains(res.Detail, "did not advance") {
		t.Fatalf("%+v", res)
	}
}

func TestNodeBehindTheNetworkIsNotInSync(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Head = 100
	res := check(t, e)
	if res.Verdict != NotInSync || !strings.Contains(res.Detail, "blocks behind the network head") {
		t.Fatalf("%+v", res)
	}
}

func TestWrongChainIsNotInSync(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Chain = testutil.MainnetChain
	res := check(t, e)
	if res.Verdict != NotInSync || !strings.Contains(res.Detail, "chain 143, expected 10143") {
		t.Fatalf("%+v", res)
	}
}

func TestSyncObjectIsNotInSync(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Syncing = true
	res := check(t, e)
	if res.Verdict != NotInSync || !strings.Contains(res.Detail, "eth_syncing in progress") {
		t.Fatalf("%+v", res)
	}
}

func TestLocalRPCDownIsUnverified(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Down = true
	res := check(t, e)
	if res.Verdict != Unverified || !strings.Contains(res.Detail, "local RPC did not answer eth_chainId") {
		t.Fatalf("%+v", res)
	}
}

func TestMalformedLocalAnswerIsUnverified(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Malformed = true
	res := check(t, e)
	if res.Verdict != Unverified || !strings.Contains(res.Detail, "not a JSON-RPC response") {
		t.Fatalf("%+v", res)
	}
}

func TestOneReferenceDownStillCompares(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Ref1.Down = true
	if res := check(t, e); res.Verdict != InSync {
		t.Fatalf("%+v", res)
	}
}

func TestReferenceOnAnotherChainIsIgnored(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Ref1.Chain = testutil.MainnetChain
	e.Ref1.Head = 999999
	if res := check(t, e); res.Verdict != InSync {
		t.Fatalf("%+v", res)
	}
}

func TestNoReferenceReachableIsUnverified(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Ref1.Down, e.Ref2.Malformed = true, true
	res := check(t, e)
	if res.Verdict != Unverified || !strings.Contains(res.Detail, "no public RPC of this network could be reached") {
		t.Fatalf("%+v", res)
	}
}

func TestLocalFarAheadOfReferencesIsUnverified(t *testing.T) {
	e := testutil.NewEndpoints()
	defer e.Close()
	e.Local.Head = 5000
	res := check(t, e)
	if res.Verdict != Unverified || !strings.Contains(res.Detail, "ahead of every public RPC") {
		t.Fatalf("%+v", res)
	}
}

func TestChainIDs(t *testing.T) {
	if ChainID("mainnet") != 143 || ChainID("testnet") != 10143 || ChainID("devnet") != 0 {
		t.Fatal("chain ids")
	}
}
