package paths

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/s0urledd/monad-failover-go/internal/ui"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MONAD_HOME", "BACKUP_ROOT", "LOG_DIR", "MF_STATE_DIR", "MF_ALLOW_NONROOT",
		"FOUNDATION_DATA_BASE", "FOUNDATION_MAX_AGE", "MF_HEALTH_WAIT", "MF_SYNC_WAIT", "MF_IP_URL", "MF_UPTIME_API_BASE",
		"MF_RPC_LOCAL_URL", "MF_RPC_REFERENCE_URLS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestDefaults(t *testing.T) {
	clearEnv(t)
	p, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if p.MonadHome != "/home/monad" || p.NodeToml != "/home/monad/monad-bft/config/node.toml" ||
		p.EnvFile != "/home/monad/.env" || p.BackupRoot != "/var/lib/monad-failover/backup" || p.LogDir != "/var/lib/monad-failover/logs" ||
		p.FoundationBase != DefaultFoundationBase || p.UptimeMainnet != DefaultUptimeMainnet || p.RPCLocal != DefaultRPCLocal ||
		len(p.RPCRefsTestnet) != 2 || p.RPCRefsTestnet[0] != "https://testnet-rpc.monad.xyz" || p.RPCInterval.Seconds() != 1 ||
		p.HealthWait.Seconds() != 5 || p.SyncWait.Seconds() != 120 || p.FoundationMaxAge.Seconds() != 86400 {
		t.Errorf("defaults: %+v", p)
	}
	if p.Sandbox {
		t.Error("sandbox without MF_ALLOW_NONROOT")
	}
}

// The test-only overrides could redirect trusted state or the values the run
// shows and signs, so outside the sandbox they are refused, never ignored.
func TestTestOnlyOverridesRefusedOutsideSandbox(t *testing.T) {
	for _, k := range []string{"MF_STATE_DIR", "MF_IP_URL", "MF_UPTIME_API_BASE", "MF_RPC_LOCAL_URL", "MF_RPC_REFERENCE_URLS"} {
		clearEnv(t)
		t.Setenv(k, "/tmp/x")
		_, err := FromEnv()
		var f *ui.Fatal
		if !errors.As(err, &f) || !strings.Contains(f.Msg, k+" is only honoured by the unprivileged test suite.") {
			t.Errorf("%s: got %v", k, err)
		}
	}
}

func TestSandboxHonoursOverrides(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the sandbox is unprivileged by definition")
	}
	clearEnv(t)
	t.Setenv("MF_ALLOW_NONROOT", "1")
	t.Setenv("MF_STATE_DIR", "/tmp/state")
	t.Setenv("MF_IP_URL", "http://127.0.0.1:1/ip")
	t.Setenv("MF_UPTIME_API_BASE", "http://127.0.0.1:1/uptime")
	t.Setenv("MF_HEALTH_WAIT", "0")
	t.Setenv("MF_SYNC_WAIT", "0")
	t.Setenv("MF_RPC_LOCAL_URL", "http://127.0.0.1:1/rpc")
	t.Setenv("MF_RPC_REFERENCE_URLS", "http://127.0.0.1:1/a,http://127.0.0.1:1/b")
	p, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !p.Sandbox || p.StateDirOverride != "/tmp/state" || len(p.IPURLs) != 1 || p.IPURLs[0] != "http://127.0.0.1:1/ip" ||
		p.UptimeMainnet != "http://127.0.0.1:1/uptime" || p.UptimeTestnet != "http://127.0.0.1:1/uptime" ||
		p.HealthWait != 0 || p.SyncWait != 0 || p.RPCLocal != "http://127.0.0.1:1/rpc" ||
		len(p.RPCRefsMainnet) != 2 || p.RPCRefsMainnet[1] != "http://127.0.0.1:1/b" || p.RPCRefsTestnet[0] != "http://127.0.0.1:1/a" {
		t.Errorf("sandbox: %+v", p)
	}
}

func TestOperatorOverridesAlwaysApply(t *testing.T) {
	clearEnv(t)
	t.Setenv("MONAD_HOME", "/srv/monad")
	t.Setenv("BACKUP_ROOT", "/srv/backup")
	t.Setenv("FOUNDATION_DATA_BASE", "https://mirror.example/validator-data")
	t.Setenv("FOUNDATION_MAX_AGE", "3600")
	p, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if p.SecpKey != "/srv/monad/monad-bft/config/id-secp" || p.BackupRoot != "/srv/backup" ||
		p.FoundationBase != "https://mirror.example/validator-data" || p.FoundationMaxAge.Seconds() != 3600 {
		t.Errorf("overrides: %+v", p)
	}
	if got := strings.Join(p.Overrides, " "); got != "MONAD_HOME=/srv/monad BACKUP_ROOT=/srv/backup FOUNDATION_DATA_BASE=https://mirror.example/validator-data FOUNDATION_MAX_AGE=3600" {
		t.Errorf("overrides not reported: %q", got)
	}
}

func TestNoOverridesReportedByDefault(t *testing.T) {
	clearEnv(t)
	p, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Overrides) != 0 {
		t.Errorf("overrides reported: %v", p.Overrides)
	}
}

func TestBadDurationsAreRefused(t *testing.T) {
	for _, k := range []string{"FOUNDATION_MAX_AGE", "MF_HEALTH_WAIT", "MF_SYNC_WAIT"} {
		clearEnv(t)
		t.Setenv(k, "soon")
		if _, err := FromEnv(); err == nil {
			t.Errorf("%s=soon accepted", k)
		}
	}
}
