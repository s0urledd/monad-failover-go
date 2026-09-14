// Package promote is the migration itself: eight phases, every one of them
// resumable, with every irreversible action behind an explicit confirmation.
package promote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/s0urledd/monad-failover-go/internal/foundation"
	"github.com/s0urledd/monad-failover-go/internal/monad"
	"github.com/s0urledd/monad-failover-go/internal/netinfo"
	"github.com/s0urledd/monad-failover-go/internal/nodeconf"
	"github.com/s0urledd/monad-failover-go/internal/paths"
	"github.com/s0urledd/monad-failover-go/internal/place"
	"github.com/s0urledd/monad-failover-go/internal/state"
	"github.com/s0urledd/monad-failover-go/internal/ui"
)

// PhasesTotal is the number of banners a live run prints.
const PhasesTotal = 8

// Options are the command-line choices for a live run. Every input can be
// given as a flag; whatever is missing is asked for. Flags never skip a
// confirmation: the plan and the STOPPED gate are always asked.
type Options struct {
	Resume       bool
	KeySourceDir string // --backup-dir; "" asks, "-" means manual IKM entry
	PublicIP     string // --public-ip override
	Beneficiary  string // --beneficiary, validated by the caller
	NodeName     string // --node-name, validated by the caller
	Seq          string // --seq, validated by the caller
	Version      string
	Argv0        string // how the tool was invoked, for "re-run with" hints
}

// Run holds everything one live run knows.
type Run struct {
	c   *ui.Console
	p   paths.Paths
	d   state.Dir
	st  state.Store
	opt Options

	tools monad.Tools

	network      string
	newSeq       string
	secpPub      string
	blsPub       string
	ip           string
	selfAddress  string
	selfSig      string
	selfSeq      string
	selfAuthPort string
	beneficiary  string
	backupDir    string

	foundSeq      uint64
	foundKnown    bool
	verifyPending bool
	cachedIP      string
	detectedIP    string // what the host reports, for comparison with --public-ip
	nodeName      string

	// what the Foundation snapshot said about the imported keys, for the plan
	snapshotStatus string // found, no-record, not-listed, bls-mismatch, unavailable
	snapshotName   string
	snapshotNote   string

	// where each value came from, for the plan
	ipSource   string
	benSource  string
	seqSource  string
	nameSource string

	// injectable clocks for the test suite
	sleep func(time.Duration)
	now   func() time.Time
}

// New builds a run. The caller has already resolved paths and state.
func New(c *ui.Console, p paths.Paths, d state.Dir, opt Options) *Run {
	return &Run{
		c: c, p: p, d: d, st: d.Store(), opt: opt,
		sleep: time.Sleep, now: time.Now,
	}
}

// Prepare performs the checks every live run makes before touching state:
// the root gate, the refusal of old-layout state, the state directory
// checks, the run lock, the consistency rules, and the fresh-or-resume
// decision. It returns (false, nil) when the operator chose to stop.
func (r *Run) Prepare() (proceed bool, lock *state.RunLock, err error) {
	if r.p.EUID != 0 && !r.p.Sandbox {
		return false, nil, ui.Die("monad-failover must run as root.")
	}
	if err := state.RefuseLegacy(r.p.MonadHome, r.p.BackupRoot); err != nil {
		return false, nil, err
	}
	if err := r.d.Secure(r.p.EUID, r.p.BackupRoot); err != nil {
		return false, nil, err
	}
	// Exclusive for the whole live run, taken before any state is read or
	// written, so a second process is refused without touching anything.
	lock, err = r.d.AcquireLock()
	if err != nil {
		return false, nil, err
	}
	if err := r.st.ValidateConsistency(r.p.BackupRoot); err != nil {
		return false, lock, err
	}

	if !r.opt.Resume && r.st.Exists() {
		last := r.st.Get("last_step")
		if last != "" {
			if err := r.st.CheckLastStep(last, r.p.BackupRoot); err != nil {
				return false, lock, err
			}
			// Once a cutover has begun, "start fresh" is no longer safe: it
			// would re-snapshot a possibly half-swapped identity as this
			// node's own. The interrupted run must be finished with --resume.
			if r.st.Get("cutover_started") == "1" {
				return false, lock, ui.Die("A previous run reached cutover — starting fresh is not safe now.",
					"Finish the interrupted run instead: "+r.opt.Argv0+" --resume",
					"(Only if you have manually restored this node and are sure, delete",
					r.d.File+" to allow a fresh run.)")
			}
			r.c.Warn("Previous run stopped at step " + last)
			r.c.Println("  Run with --resume to continue, or start fresh.")
			if r.c.ConfirmYN("  Start fresh?") {
				r.st.Clear()
			} else {
				r.c.Println("  Use: " + r.opt.Argv0 + " --resume")
				return false, lock, nil
			}
		}
	}
	return true, lock, nil
}

func (r *Run) startLog() error {
	if err := place.PrivateDir(r.p.LogDir); err != nil {
		return ui.Die("Cannot use log directory "+r.p.LogDir, err.Error())
	}
	f, err := os.CreateTemp(r.p.LogDir, "failover-"+r.now().Format("20060102-150405")+"-*.log")
	if err != nil {
		return ui.Die("Could not open the run log in " + r.p.LogDir)
	}
	// Secrets never appear in the output (hidden input, no echo), so the log
	// is safe to keep; it is the operator's record of the migration.
	logPath := f.Name()
	r.c.Tee(f)
	r.c.Printf("%s(logging this run to %s)%s\n", ui.Dim, logPath, ui.Reset)
	return nil
}

// Promote runs the migration from wherever the state says it stands.
func (r *Run) Promote() error {
	if err := r.startLog(); err != nil {
		return err
	}
	r.c.Header(r.opt.Version)

	for _, cmd := range []string{"systemctl", "monad-keystore", "monad-sign-name-record"} {
		if !monad.Have(cmd) {
			return ui.Die("Missing command: " + cmd)
		}
	}
	if _, err := os.Stat(r.p.NodeToml); err != nil {
		return ui.Die("node.toml not found: " + r.p.NodeToml)
	}
	if _, err := os.Stat(r.p.EnvFile); err != nil {
		return ui.Die(".env not found: " + r.p.EnvFile)
	}
	if _, _, err := r.placementOwner(); err != nil {
		return ui.Die("Cannot resolve the monad service account; refusing to prepare a cutover.")
	}
	pw := nodeconf.LoadKeystorePassword(r.p.EnvFile)
	if len(pw) == 0 {
		return ui.Die("KEYSTORE_PASSWORD not set in " + r.p.EnvFile)
	}
	defer ui.Zero(pw)
	r.tools = monad.Tools{Password: pw, EnvFile: r.p.EnvFile}

	if r.opt.Resume {
		res, err := r.st.LoadResume(r.p.BackupRoot, netinfo.ValidIPv4)
		switch {
		case errors.Is(err, state.ErrNoPreviousRun):
			r.c.Warn("No previous run found. Starting fresh.")
			r.opt.Resume = false
		case err != nil:
			return err
		default:
			r.c.OK(fmt.Sprintf("Resuming from step %d", res.LastStep+1))
			r.network, r.newSeq = res.Network, res.NewSeq
			r.secpPub, r.blsPub = res.SecpPub, res.BlsPub
			r.ip, r.selfAddress, r.selfSig = res.IP, res.SelfAddress, res.SelfSig
			r.selfSeq, r.selfAuthPort = res.SelfSeq, res.SelfAuthPort
			r.beneficiary, r.backupDir = res.Beneficiary, res.BackupDir
			r.nodeName, r.detectedIP = res.NodeName, res.DetectedIP
			r.ipSource, r.benSource, r.seqSource, r.nameSource = res.IPSource, res.BenSource, res.SeqSource, res.NameSource
			r.snapshotStatus, r.snapshotName, r.snapshotNote = res.SnapshotStatus, res.SnapshotName, res.SnapshotNote
			if res.SnapshotSeq != "" {
				r.foundSeq, _ = strconv.ParseUint(res.SnapshotSeq, 10, 64)
				r.foundKnown = true
			}
		}
	}

	// A resume can be hours old. While the cutover has not begun, the node's
	// health is worth re-reading rather than trusting the earlier result.
	if r.opt.Resume && r.st.Get("cutover_started") != "1" {
		if err := r.checkSync(); err != nil {
			return err
		}
	}

	if r.todo(1) {
		r.c.Phase(1, PhasesTotal, "PREFLIGHT")
		if err := r.checkSync(); err != nil {
			return err
		}
		if err := r.st.Set("last_step", "1"); err != nil {
			return err
		}
	}

	if r.todo(2) {
		r.c.Phase(2, PhasesTotal, "NETWORK & HOST")
		if err := r.detectNetwork(); err != nil {
			return err
		}
		if err := r.locationGuard(); err != nil {
			return err
		}
		if err := r.st.Set("network", r.network); err != nil {
			return err
		}
		if err := r.st.Set("last_step", "2"); err != nil {
			return err
		}
	}

	if r.todo(3) {
		r.c.Phase(3, PhasesTotal, "BACKUP CURRENT CONFIG")
		if err := r.backupConfig(); err != nil {
			return err
		}
		if err := r.st.Set("last_step", "3"); err != nil {
			return err
		}
	}

	if r.todo(4) {
		if err := r.importKeys(); err != nil {
			return err
		}
	}

	if r.todo(5) {
		if err := r.configure(); err != nil {
			return err
		}
	}

	if r.todo(6) {
		if err := r.signRecord(); err != nil {
			return err
		}
	}

	if r.todo(7) {
		if err := r.cutover(); err != nil {
			return err
		}
	}

	if r.todo(8) {
		if err := r.verify(); err != nil {
			return err
		}
	}

	return r.finish()
}

// todo reports whether phase n still has to run.
func (r *Run) todo(n int) bool {
	return !r.opt.Resume || !r.st.CompletedStep(n)
}

// ── phases 1–3 ───────────────────────────────────────────────────────

func (r *Run) checkSync() error {
	r.c.Step("NODE SYNC CHECK")
	if monad.Have("monad-status") {
		status, diff, _ := monad.Status()
		if status == "in-sync" {
			if diff == "" {
				diff = "0"
			}
			r.c.OK("Node: in-sync (block difference: " + diff + ")")
			return nil
		}
		if status == "" {
			status = "unknown"
		}
		return ui.Die("Node is " + status + ". Must be fully synced before promotion.")
	}
	r.c.Warn("monad-status not installed — cannot verify sync")
	if !r.c.ConfirmYN("continue without sync check?") {
		return ui.Die("Aborted.")
	}
	return nil
}

func (r *Run) detectNetwork() error {
	r.network = ""
	if data, err := os.ReadFile(r.p.NodeToml); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "network_name") {
				parts := strings.Split(line, `"`)
				if len(parts) >= 2 {
					r.network = parts[1]
				}
				break
			}
		}
	}
	if r.network != "mainnet" && r.network != "testnet" {
		ans, err := r.c.AskRaw("Network could not be detected. Enter (mainnet/testnet): ")
		if err != nil {
			return err
		}
		r.network = ans
		if r.network != "mainnet" && r.network != "testnet" {
			return ui.Die("Invalid network")
		}
	}
	r.c.OK("Network: " + r.network)
	return nil
}

// publicIP is the --public-ip override if given, otherwise the address
// detected over HTTPS, remembered for the rest of the run. With an override
// the host's own answer is still fetched once, so the plan can show a
// mismatch; it never replaces the operator's value.
func (r *Run) publicIP() string {
	if r.cachedIP == "" {
		r.cachedIP = netinfo.DetectPublicIPv4(r.p.IPURL)
		r.detectedIP = r.cachedIP
	}
	if r.opt.PublicIP != "" {
		r.ipSource = "flag"
		return r.opt.PublicIP
	}
	r.ipSource = "detected"
	return r.cachedIP
}

// locationGuard shows where the run is happening. The confirmation comes
// with the plan, once every value is known.
func (r *Run) locationGuard() error {
	host, _ := os.Hostname()
	r.c.Blank()
	r.c.Println("  This will " + ui.Bold + "promote this full node to validator" + ui.Reset + ".")
	r.c.Println("  Hostname:  " + ui.Bold + host + ui.Reset)
	if ip := r.publicIP(); ip != "" {
		r.c.Println("  Public IP: " + ui.Bold + ip + ui.Reset)
	}
	return nil
}

// backupConfig preserves this server's own identity (encrypted keystores and
// config) so it can be restored to a full node by hand. The keystore
// password is NOT copied: colocating it with the keys would defeat the
// encryption.
func (r *Run) backupConfig() error {
	if err := place.PrivateDir(r.p.BackupRoot); err != nil {
		return err
	}
	var err error
	r.backupDir, err = os.MkdirTemp(r.p.BackupRoot, "failover-"+r.now().Format("20060102-150405")+"-")
	if err != nil {
		return err
	}
	_ = os.Chmod(r.backupDir, 0o700)
	for _, f := range []string{"node.toml", "id-secp", "id-bls"} {
		src := filepath.Join(r.p.ConfigDir, f)
		if fi, err := os.Lstat(src); err != nil || !fi.Mode().IsRegular() {
			return ui.Die("Cannot back up required identity file: " + src)
		}
		if err := place.CopyPreserve(src, filepath.Join(r.backupDir, f)); err != nil {
			return ui.Die("Could not back up " + src + " to " + r.backupDir)
		}
	}
	if _, err := os.Stat(r.p.PubkeyList); err == nil {
		if err := place.CopyPreserve(r.p.PubkeyList, filepath.Join(r.backupDir, filepath.Base(r.p.PubkeyList))); err != nil {
			return ui.Die("Could not back up " + r.p.PubkeyList + " to " + r.backupDir)
		}
	}
	r.c.OK("Config backed up to " + r.backupDir)
	return r.st.Set("backup_dir", r.backupDir)
}

// ── phase 4: validator keys into staging ─────────────────────────────

func (r *Run) importKeys() error {
	for _, f := range []string{r.d.SecpNew, r.d.BlsNew, r.d.TomlNew} {
		_ = os.Remove(f)
	}
	r.c.Phase(4, PhasesTotal, "VALIDATOR KEY IMPORT")
	r.c.Println("  Keys are imported to staging files (id-secp.new / id-bls.new).")
	r.c.Println("  Live keys remain untouched until cutover.")
	r.c.Blank()

	src := r.opt.KeySourceDir
	if src == "" {
		r.c.Println("    1) Key backup files (secp-backup / bls-backup) — " + ui.Green + "recommended" + ui.Reset)
		r.c.Println("       Works even when the old server is unreachable.")
		r.c.Println("    2) Paste IKM hex values manually (hidden input)")
		r.c.Blank()
		choice, err := r.c.Ask("select (1/2)")
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			dir, err := r.c.Ask("backup directory [" + r.p.BackupRoot + "]")
			if err != nil {
				return err
			}
			if dir == "" {
				dir = r.p.BackupRoot
			}
			src = dir
		case "2":
			src = "-"
		default:
			return ui.Die("Invalid selection")
		}
	}

	var secpIKM, blsIKM []byte
	defer func() { ui.Zero(secpIKM); ui.Zero(blsIKM) }()
	if src != "-" {
		r.c.Step("READ KEY BACKUP FILES")
		secpFile := filepath.Join(src, "secp-backup")
		blsFile := filepath.Join(src, "bls-backup")
		for _, f := range []string{secpFile, blsFile} {
			if fi, err := os.Stat(f); err != nil || !fi.Mode().IsRegular() {
				return ui.Die("Not found: " + f)
			}
		}
		var ok bool
		raw := nodeconf.ExtractIKMFromBackup(secpFile)
		secpIKM, ok = nodeconf.ValidateIKM(raw)
		ui.Zero(raw)
		if !ok {
			return ui.Die("Could not extract a valid SECP IKM from " + secpFile)
		}
		raw = nodeconf.ExtractIKMFromBackup(blsFile)
		blsIKM, ok = nodeconf.ValidateIKM(raw)
		ui.Zero(raw)
		if !ok {
			return ui.Die("Could not extract a valid BLS IKM from " + blsFile)
		}
		r.c.OK("IKM secrets extracted from backup files")
	} else {
		r.c.Println("  Paste the validator IKM hex values. Input is hidden.")
		r.c.Blank()
		raw, err := r.c.AskHidden("SECP IKM_HEX")
		if err != nil {
			return err
		}
		v, ok := nodeconf.ValidateIKM(raw)
		ui.Zero(raw)
		if !ok {
			return ui.Die("SECP IKM must be 64 hex characters")
		}
		secpIKM = v
		raw, err = r.c.AskHidden("BLS  IKM_HEX")
		if err != nil {
			return err
		}
		v, ok = nodeconf.ValidateIKM(raw)
		ui.Zero(raw)
		if !ok {
			return ui.Die("BLS IKM must be 64 hex characters")
		}
		blsIKM = v
	}

	r.c.Step("Importing SECP key (staging)")
	if err := r.tools.ImportKey(secpIKM, r.d.SecpNew); err != nil {
		return err
	}
	r.c.OK("SECP key imported to id-secp.new")
	r.c.Step("Importing BLS key (staging)")
	if err := r.tools.ImportKey(blsIKM, r.d.BlsNew); err != nil {
		return err
	}
	r.c.OK("BLS key imported to id-bls.new")
	ui.Zero(secpIKM)
	ui.Zero(blsIKM)

	r.secpPub = r.tools.RecoverPubkey(r.d.SecpNew, "secp")
	r.blsPub = r.tools.RecoverPubkey(r.d.BlsNew, "bls")
	if r.secpPub == "" {
		return ui.Die("Could not recover SECP public key")
	}
	if r.blsPub == "" {
		return ui.Die("Could not recover BLS public key")
	}

	r.c.Blank()
	r.c.Println("  SECP: " + ui.Bold + r.secpPub + ui.Reset)
	r.c.Println("  BLS:  " + ui.Bold + r.blsPub + ui.Reset)
	r.c.Blank()
	r.c.OK("Keys imported to staging; confirm them in the plan")

	// Bind the confirmed keys to their bytes now. Cutover re-checks these and
	// refuses anything that changed after this confirmation.
	for _, kv := range [][2]string{
		{"secp_pub", r.secpPub},
		{"bls_pub", r.blsPub},
		{"staged_secp_sha", place.FileSHA(r.d.SecpNew)},
		{"staged_bls_sha", place.FileSHA(r.d.BlsNew)},
		{"last_step", "4"},
	} {
		if err := r.st.Set(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// ── phase 5: beneficiary, node name, sequence, flags (staging copy) ──

// Every input here can come from a flag; whatever is missing is asked for.
// Nothing is confirmed one value at a time: the plan at cutover shows them
// all, with where each came from, and takes the one answer.

func (r *Run) configure() error {
	r.c.Phase(5, PhasesTotal, "CONFIGURE VALIDATOR")
	r.c.Println("  All changes go to a staging copy (node.toml.new).")
	r.c.Println("  The live config is untouched until cutover.")

	// Never touch the live node.toml before cutover. An abort at the plan or
	// the STOPPED gate must leave a fully unmodified full node behind.
	if err := place.CopyPreserve(r.p.NodeToml, r.d.TomlNew); err != nil {
		return ui.Die("Could not copy " + r.p.NodeToml + " to staging")
	}

	if err := r.chooseBeneficiary(); err != nil {
		return err
	}
	if err := r.chooseNodeName(); err != nil {
		return err
	}
	if err := r.chooseSeq(); err != nil {
		return err
	}

	for _, e := range []struct{ k, v, sec string }{
		{"enable_publisher", "true", "fullnode_raptorcast"},
		{"enable_client", "true", "fullnode_raptorcast"},
		{"expand_to_group", "true", "statesync"},
	} {
		if err := nodeconf.SetTomlValue(r.d.TomlNew, e.k, e.v, e.sec); err != nil {
			return err
		}
	}
	r.verifyConfigFlags(r.d.TomlNew)

	snapshotSeq := ""
	if r.foundKnown {
		snapshotSeq = strconv.FormatUint(r.foundSeq, 10)
	}
	for _, kv := range [][2]string{
		{"beneficiary", r.beneficiary},
		{"ben_source", r.benSource},
		{"node_name", r.nodeName},
		{"name_source", r.nameSource},
		{"new_seq", r.newSeq},
		{"seq_source", r.seqSource},
		{"snapshot_status", r.snapshotStatus},
		{"snapshot_name", r.snapshotName},
		{"snapshot_seq", snapshotSeq},
		{"snapshot_note", r.snapshotNote},
		{"last_step", "5"},
	} {
		if err := r.st.Set(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// readUnique returns the root-level value of key in the staged config, or
// "" when it is absent. An absent value is not an error: the operator can
// supply one. A key that appears more than once is: the staged edit could
// not be unique.
func (r *Run) readUnique(key string) (string, error) {
	cur, err := nodeconf.ReadValue(r.d.TomlNew, key, "")
	switch {
	case errors.Is(err, nodeconf.ErrMissing):
		return "", nil
	case err != nil:
		return "", ui.Die("Cannot read an unambiguous "+key+" from node.toml.",
			"It appears more than once or is not a plain string. Fix the config and re-run.")
	}
	return cur, nil
}

func orNone(v string) string {
	if v == "" {
		return "(none set)"
	}
	return v
}

func (r *Run) chooseBeneficiary() error {
	r.c.Blank()
	cur, err := r.readUnique("beneficiary")
	if err != nil {
		return err
	}
	ben := r.opt.Beneficiary
	if ben != "" {
		r.benSource = "flag"
	} else {
		r.c.Println(ui.Bold + "BENEFICIARY" + ui.Reset)
		r.c.Println("Enter the beneficiary address from the old validator's node.toml.")
		r.c.Println("Leave blank to keep the address already in this node's config:")
		r.c.Println("    " + ui.Bold + orNone(cur) + ui.Reset)
		if ben, err = r.c.Ask("beneficiary"); err != nil {
			return err
		}
		r.benSource = "entered"
	}
	if ben == "" {
		// Blank keeps what is there. Rewards go to this address, so it has
		// to exist and be an address; the plan shows it as kept.
		if cur == "" {
			return ui.Die("No beneficiary given and none set in the config.",
				"Re-run and enter the validator's beneficiary address.")
		}
		if !nodeconf.BeneficiaryRe.MatchString(cur) {
			return ui.Die("The beneficiary already in the config is not a 0x-prefixed 40-hex-character address.",
				"Re-run and enter the validator's beneficiary address.")
		}
		ben = cur
		r.benSource = "kept from node.toml"
	} else {
		if !nodeconf.BeneficiaryRe.MatchString(ben) {
			return ui.Die("beneficiary must be a 0x-prefixed 40-hex-character address")
		}
		if err := nodeconf.SetTomlValue(r.d.TomlNew, "beneficiary", `"`+ben+`"`, ""); err != nil {
			return err
		}
	}
	r.beneficiary = ben
	r.c.OK("Beneficiary: " + ben + " (" + r.benSource + ")")
	if nodeconf.ZeroAddressRe.MatchString(ben) {
		switch r.benSource {
		case "kept from node.toml":
			r.c.Warn("The address already in the config is the ZERO address.")
			r.c.Println("  Keeping it means this validator has no beneficiary set.")
		case "flag":
			r.c.Warn("--beneficiary is the ZERO address. This validator will have no beneficiary set.")
		default:
			r.c.Warn("You entered the ZERO address. This validator will have no beneficiary set.")
		}
	}
	return nil
}

func (r *Run) chooseNodeName() error {
	r.c.Blank()
	cur, err := r.readUnique("node_name")
	if err != nil {
		return err
	}
	name := r.opt.NodeName
	if name != "" {
		r.nameSource = "flag"
	} else {
		r.c.Println(ui.Bold + "NODE NAME" + ui.Reset)
		r.c.Println("Per the migration docs, this node should take over the old validator's")
		r.c.Println("node_name during migration. Leave empty to keep the current name:")
		r.c.Println("    " + ui.Bold + orNone(cur) + ui.Reset)
		if name, err = r.c.Ask("node_name"); err != nil {
			return err
		}
		r.nameSource = "entered"
	}
	if name == "" {
		// The existing name is shown, never validated: it is the node's own
		// and stays as it is. Only what is printed is made printable.
		r.nodeName = ui.Printable(cur)
		r.nameSource = "kept from node.toml"
		r.c.OK("node_name unchanged: " + orNone(r.nodeName))
		return nil
	}
	if !nodeconf.NodeNameRe.MatchString(name) {
		return ui.Die("node_name may contain only letters, digits, dot, dash, underscore (max 64)")
	}
	if err := nodeconf.SetTomlValue(r.d.TomlNew, "node_name", `"`+name+`"`, ""); err != nil {
		return err
	}
	r.nodeName = name
	r.c.OK("node_name: " + name + " (" + r.nameSource + ")")
	return nil
}

func (r *Run) chooseSeq() error {
	r.c.Blank()
	r.c.Println(ui.Bold + "SEQ NUM" + ui.Reset)
	r.c.Println("The name record's sequence number must be higher than any value this")
	r.c.Println("validator identity has used before. Gaps are harmless.")

	suggested := ""
	r.c.Step("FOUNDATION SNAPSHOT")
	body, fetchErr := foundation.Fetch(r.p.FoundationBase, r.network)
	res, note := foundation.Lookup(body, fetchErr, r.network, r.secpPub, r.blsPub, r.p.FoundationMaxAge, r.now())
	r.snapshotName = res.Name
	if note == "" {
		r.foundSeq, r.foundKnown = res.Seq, true
		r.snapshotStatus = "found"
		suggested = strconv.FormatUint(res.Seq+1, 10)
		r.c.OK(fmt.Sprintf("Last published sequence for this key: %s%d%s (%s snapshot, %dh old)",
			ui.Bold, res.Seq, ui.Reset, r.network, res.AgeHours))
		if res.Name != "" {
			r.c.Println("  The snapshot lists these keys as: " + ui.Bold + res.Name + ui.Reset)
		}
		if r.opt.Seq == "" {
			r.c.Println("  Suggested for this migration: " + ui.Bold + suggested + ui.Reset)
			r.c.Println("  Press Enter to use it, or type a higher number if you know of a later one.")
		}
	} else {
		r.c.Warn("Could not read a sequence from the Foundation snapshot (" + note + ").")
		r.noteSnapshot(note)
		if r.opt.Seq == "" {
			r.c.Println("  Enter the value yourself: one higher than the last this identity used.")
			r.c.Println("  Check your records or the old validator's node.toml.")
		}
	}

	seq := r.opt.Seq
	if seq != "" {
		r.seqSource = "flag"
	} else {
		label := "new seq_num"
		if suggested != "" {
			label += " [" + suggested + "]"
		}
		var err error
		if seq, err = r.c.Ask(label); err != nil {
			return err
		}
		r.seqSource = "entered"
		if seq == "" && suggested != "" {
			seq = suggested
			r.seqSource = "suggested by the snapshot"
		}
	}
	if !seqInputRe.MatchString(seq) {
		return ui.Die("Must be a positive number")
	}
	n, err := strconv.ParseUint(seq, 10, 64)
	if err != nil || n > foundation.SeqSaneMax {
		return ui.Die("Sequence number is unreasonably large")
	}
	// The snapshot value is a floor, never a ceiling: a stale snapshot can
	// only be behind the network, so anything at or below it would be rejected.
	if r.foundKnown && n <= r.foundSeq {
		return ui.Die(fmt.Sprintf("seq_num %s is not higher than the %d already published for this key.", seq, r.foundSeq),
			"Peers would reject the record. Use "+suggested+" or higher.")
	}
	r.newSeq = seq
	r.c.OK("seq_num for this migration: " + seq + " (" + r.seqSource + ")")
	return nil
}

// noteSnapshot classifies a snapshot note for the plan and warns when it
// says something about the imported keys. A note about the snapshot itself
// (unreachable, stale, wrong network) says nothing about the keys.
func (r *Run) noteSnapshot(note string) {
	r.snapshotNote = noteCharsRe.ReplaceAllString(note, "")
	if len(r.snapshotNote) > 120 {
		r.snapshotNote = r.snapshotNote[:120]
	}
	switch note {
	case foundation.NoteNotListed:
		r.snapshotStatus = "not-listed"
		r.c.Warn("These keys are not in the Foundation snapshot for " + r.network + ".")
		r.c.Println("  That is expected for a validator outside the active set. If this")
		r.c.Println("  validator is active, check that the backups belong to it.")
	case foundation.NoteNoRecord:
		r.snapshotStatus = "no-record"
		r.c.Println("  The snapshot lists these keys without a name record, which is")
		r.c.Println("  usual for a validator outside the active set.")
	case foundation.NoteBLSMismatch:
		r.snapshotStatus = "bls-mismatch"
		r.c.Warn("The snapshot entry for this SECP key carries a different BLS key.")
		r.c.Println("  Check that bls-backup belongs to the same validator as secp-backup.")
	default:
		r.snapshotStatus = "unavailable"
	}
}

func (r *Run) verifyConfigFlags(file string) {
	r.c.Step("VERIFY CONFIG FLAGS")
	if missing := nodeconf.MissingConfigFlags(file); len(missing) > 0 {
		r.c.Warn("Flags not set: " + strings.Join(missing, " "))
		r.c.Println("  The official migration docs require these to be true.")
		return
	}
	r.c.OK("enable_publisher, enable_client, expand_to_group all set")
}

// ── phase 6: sign the name record, patch the staged config ───────────

func (r *Run) signRecord() error {
	r.c.Phase(6, PhasesTotal, "SIGN NAME RECORD")
	if fi, err := os.Stat(r.d.TomlNew); err != nil || !fi.Mode().IsRegular() {
		return ui.Die("Staging config (node.toml.new) is missing.",
			"Start a fresh run so the configure step re-creates it.")
	}
	r.ip = r.publicIP()
	if r.ip == "" {
		return ui.Die("Could not detect a valid public IPv4 address.",
			"Retry with: "+r.opt.Argv0+" --resume --public-ip <this-server-public-IPv4>")
	}
	r.c.OK("Public IP: " + r.ip + " (" + r.ipSource + ")")
	if r.opt.PublicIP != "" && r.detectedIP != "" && r.detectedIP != r.ip {
		r.c.Warn("--public-ip " + r.ip + " differs from the address this host reports: " + r.detectedIP)
		r.c.Println("  The name record will carry " + r.ip + ". Peers must reach this node there.")
	}

	if err := nodeconf.SanitizePlaceholders(r.d.TomlNew); err != nil {
		return err
	}
	r.c.OK("Placeholders sanitized")

	r.c.Step("SIGN NAME RECORD (seq " + r.newSeq + ")")
	out, err := r.tools.SignNameRecord(r.ip, r.newSeq, r.d.SecpNew)
	if err != nil {
		return err
	}
	signed, warning, err := monad.ParseSignerOutput(out, r.newSeq)
	if err != nil {
		return err
	}
	if signed.Address != r.ip+":8000" || signed.AuthPort != "8001" {
		return ui.Die("Signer output does not match the requested IP and standard ports; refusing cutover.")
	}
	if warning != "" {
		r.c.Warn(warning)
		r.c.Println("  Safe to continue: the signature matches the emitted value.")
	}
	r.c.OK("Name record signed (seq " + signed.Seq + ")")
	r.selfAddress, r.selfAuthPort, r.selfSeq, r.selfSig = signed.Address, signed.AuthPort, signed.Seq, signed.Sig

	r.c.Step("PATCH node.toml")
	for _, e := range []struct{ k, v string }{
		{"self_address", `"` + signed.Address + `"`},
		{"self_auth_port", signed.AuthPort},
		{"self_record_seq_num", signed.Seq},
		{"self_name_record_sig", `"` + signed.Sig + `"`},
	} {
		if err := nodeconf.SetTomlValue(r.d.TomlNew, e.k, e.v, "peer_discovery"); err != nil {
			return err
		}
	}
	for _, kv := range [][2]string{{"self_address", signed.Address}, {"self_auth_port", signed.AuthPort}, {"self_record_seq_num", signed.Seq}, {"self_name_record_sig", signed.Sig}} {
		value, err := nodeconf.ReadValue(r.d.TomlNew, kv[0], "peer_discovery")
		if err != nil || value != kv[1] {
			return ui.Die("Staged config does not match signer output: " + kv[0])
		}
	}
	r.c.OK("node.toml patched and verified")

	for _, kv := range [][2]string{
		{"ip", r.ip},
		{"ip_source", r.ipSource},
		{"detected_ip", r.detectedIP},
		{"self_address", r.selfAddress},
		{"self_sig", r.selfSig},
		{"self_seq", r.selfSeq},
		{"self_auth_port", r.selfAuthPort},
		// node.toml.new is final once the record is signed and patched in.
		{"staged_toml_sha", place.FileSHA(r.d.TomlNew)},
		{"last_step", "6"},
	} {
		if err := r.st.Set(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// placementOwner never changes the live config directory or .env.
func (r *Run) placementOwner() (int, int, error) {
	if r.p.Sandbox {
		return os.Geteuid(), os.Getegid(), nil
	}
	return monad.MonadIDs()
}
