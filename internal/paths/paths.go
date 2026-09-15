// Package paths resolves every file, directory and endpoint the tool uses,
// applying the environment overrides the test suite relies on.
package paths

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/s0urledd/monad-failover-go/internal/netinfo"
	"github.com/s0urledd/monad-failover-go/internal/ui"
)

// Paths is the resolved layout for one run.
type Paths struct {
	MonadHome  string
	ConfigDir  string
	NodeToml   string
	EnvFile    string
	SecpKey    string
	BlsKey     string
	PubkeyList string

	BackupRoot string
	LogDir     string

	// Sandbox is true when the process is unprivileged AND MF_ALLOW_NONROOT is
	// set: the test suite's escape hatch. Only then are test-only overrides
	// (state directory, endpoint URLs) honoured.
	Sandbox bool
	EUID    int

	StateDirOverride string // MF_STATE_DIR, sandbox only

	FoundationBase   string
	FoundationMaxAge time.Duration
	IPURL            string
	UptimeMainnet    string
	UptimeTestnet    string

	HealthWait time.Duration
	SyncWait   time.Duration

	// Sync check over RPC, used when monad-status is not installed: this
	// node's endpoint and the public endpoints of each network.
	RPCLocal       string
	RPCRefsMainnet []string
	RPCRefsTestnet []string
	RPCInterval    time.Duration // between the two head readings

	// Overrides lists the operator environment variables that changed a
	// default, as KEY=value, so a run says so before acting on them.
	Overrides []string
}

const (
	DefaultFoundationBase = "https://bucket.monadinfra.com/validator-data"
	DefaultUptimeMainnet  = "https://validator-api.huginn.tech/monad-api/validator/uptime"
	DefaultUptimeTestnet  = "https://validator-api-testnet.huginn.tech/monad-api/validator/uptime"
	DefaultRPCLocal       = "http://127.0.0.1:8080"
)

// Public RPCs of each network, run by the Foundation. Two per network so
// one being down does not leave the sync check without a comparison.
var (
	DefaultRPCRefsMainnet = []string{"https://rpc.monad.xyz", "https://rpc-mainnet.monadinfra.com"}
	DefaultRPCRefsTestnet = []string{"https://testnet-rpc.monad.xyz", "https://rpc-testnet.monadinfra.com"}
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envSeconds(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative number of seconds", key)
	}
	return time.Duration(n) * time.Second, nil
}

// FromEnv builds the layout for the current process.
func FromEnv() (Paths, error) {
	p := Paths{EUID: os.Geteuid()}
	p.Sandbox = p.EUID != 0 && os.Getenv("MF_ALLOW_NONROOT") != ""

	p.MonadHome = envOr("MONAD_HOME", "/home/monad")
	p.ConfigDir = p.MonadHome + "/monad-bft/config"
	p.NodeToml = p.ConfigDir + "/node.toml"
	p.EnvFile = p.MonadHome + "/.env"
	p.SecpKey = p.ConfigDir + "/id-secp"
	p.BlsKey = p.ConfigDir + "/id-bls"
	p.PubkeyList = p.MonadHome + "/pubkey-secp-bls"
	p.BackupRoot = envOr("BACKUP_ROOT", "/opt/monad/backup")
	p.LogDir = envOr("LOG_DIR", "/opt/monad/failover-logs")

	p.FoundationBase = envOr("FOUNDATION_DATA_BASE", DefaultFoundationBase)
	var err error
	if p.FoundationMaxAge, err = envSeconds("FOUNDATION_MAX_AGE", 86400*time.Second); err != nil {
		return p, ui.Die(err.Error())
	}
	if p.HealthWait, err = envSeconds("MF_HEALTH_WAIT", 5*time.Second); err != nil {
		return p, ui.Die(err.Error())
	}
	if p.SyncWait, err = envSeconds("MF_SYNC_WAIT", 120*time.Second); err != nil {
		return p, ui.Die(err.Error())
	}

	// Test-only overrides. MF_STATE_DIR could point the trusted state at a
	// user-writable path, and the endpoint URLs feed values the run shows and
	// signs, so a live (root) run refuses them rather than silently ignoring
	// them: a stray variable must never be assumed honoured.
	testOnly := map[string]string{
		"MF_STATE_DIR":          os.Getenv("MF_STATE_DIR"),
		"MF_IP_URL":             os.Getenv("MF_IP_URL"),
		"MF_UPTIME_API_BASE":    os.Getenv("MF_UPTIME_API_BASE"),
		"MF_RPC_LOCAL_URL":      os.Getenv("MF_RPC_LOCAL_URL"),
		"MF_RPC_REFERENCE_URLS": os.Getenv("MF_RPC_REFERENCE_URLS"),
	}
	if !p.Sandbox {
		for k, v := range testOnly {
			if v != "" {
				return p, ui.Die(
					k+" is only honoured by the unprivileged test suite.",
					"In a live (root) run this value is fixed so it cannot be redirected",
					"to a location or endpoint an unprivileged user controls.")
			}
		}
	}
	for _, k := range []string{"MONAD_HOME", "BACKUP_ROOT", "LOG_DIR", "FOUNDATION_DATA_BASE", "FOUNDATION_MAX_AGE", "MF_HEALTH_WAIT", "MF_SYNC_WAIT"} {
		if v := os.Getenv(k); v != "" {
			p.Overrides = append(p.Overrides, k+"="+v)
		}
	}
	p.StateDirOverride = testOnly["MF_STATE_DIR"]
	p.IPURL = netinfo.DefaultIPURL
	if v := testOnly["MF_IP_URL"]; v != "" {
		p.IPURL = v
	}
	p.UptimeMainnet, p.UptimeTestnet = DefaultUptimeMainnet, DefaultUptimeTestnet
	if v := testOnly["MF_UPTIME_API_BASE"]; v != "" {
		p.UptimeMainnet, p.UptimeTestnet = v, v
	}
	p.RPCLocal = DefaultRPCLocal
	if v := testOnly["MF_RPC_LOCAL_URL"]; v != "" {
		p.RPCLocal = v
	}
	p.RPCRefsMainnet, p.RPCRefsTestnet = DefaultRPCRefsMainnet, DefaultRPCRefsTestnet
	if v := testOnly["MF_RPC_REFERENCE_URLS"]; v != "" {
		refs := strings.Split(v, ",")
		p.RPCRefsMainnet, p.RPCRefsTestnet = refs, refs
	}
	p.RPCInterval = 3 * time.Second
	return p, nil
}
