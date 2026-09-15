// monad-failover promotes a synced Monad full node to validator, following
// the official node migration procedure.
//
//	monad-failover [--backup-dir PATH] [--beneficiary ADDR] [--node-name NAME]
//	               [--seq N] [--public-ip IP] [--resume]
//	monad-failover --dry-run
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/s0urledd/monad-failover-go/internal/harden"
	"github.com/s0urledd/monad-failover-go/internal/monad"
	"github.com/s0urledd/monad-failover-go/internal/netinfo"
	"github.com/s0urledd/monad-failover-go/internal/nodeconf"
	"github.com/s0urledd/monad-failover-go/internal/paths"
	"github.com/s0urledd/monad-failover-go/internal/promote"
	"github.com/s0urledd/monad-failover-go/internal/state"
	"github.com/s0urledd/monad-failover-go/internal/ui"
)

// Version is the tool version printed by --version and in the banner.
const Version = "0.4.3"

func usage(argv0 string) {
	fmt.Printf("monad-failover v%s — promote a synced Monad full node to validator\n", Version)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s [--backup-dir PATH] [--beneficiary ADDR] [--node-name NAME] [--seq N]\n", argv0)
	fmt.Printf("  %s --resume\n", argv0)
	fmt.Printf("  %s --dry-run\n", argv0)
	fmt.Println()
	fmt.Println("Every input can be given as a flag; whatever is missing is asked for.")
	fmt.Println("The plan is shown and confirmed before anything on the node changes.")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --dry-run      Read-only preflight: run every check, change nothing")
	fmt.Println("  --backup-dir   Directory containing secp-backup / bls-backup key files")
	fmt.Println("  --beneficiary  Beneficiary address (0x + 40 hex) for the validator")
	fmt.Println("  --node-name    node_name to take over from the old validator")
	fmt.Println("  --seq          Name record sequence number, higher than any used before")
	fmt.Println("  --public-ip    Use this IPv4 in the name record instead of auto-detection")
	fmt.Println("  --resume       Continue from the last completed step")
	fmt.Println("  --version      Print version and exit")
}

func main() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	// Secrets (key backups, state) must never be created world-readable,
	// not even for the instant between open() and chmod.
	syscall.Umask(0o077)
	// No core dump, not dumpable, and as root no paging out: a secret this
	// process holds should not leave it through those routes. Each measure
	// is best effort; what did not apply is said before the run starts.
	hardening := harden.Apply(os.Geteuid())

	c := ui.New(os.Stdout, os.Stderr, os.Stdin)
	argv0 := args[0]
	opt := promote.Options{Version: Version, Argv0: argv0}
	dryRun := false

	fail := func(err error) int {
		c.Report(err)
		return 1
	}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--resume":
			opt.Resume = true
		case "--backup-dir":
			i++
			if i >= len(args) || args[i] == "" {
				return fail(ui.Die("--backup-dir requires a value"))
			}
			opt.KeySourceDir = args[i]
		case "--public-ip":
			i++
			if i >= len(args) || !netinfo.ValidIPv4(args[i]) {
				return fail(ui.Die("--public-ip must be a valid IPv4 address"))
			}
			opt.PublicIP = args[i]
		case "--beneficiary":
			i++
			if i >= len(args) || !nodeconf.BeneficiaryRe.MatchString(args[i]) {
				return fail(ui.Die("--beneficiary must be a 0x-prefixed 40-hex-character address"))
			}
			opt.Beneficiary = args[i]
		case "--node-name":
			i++
			if i >= len(args) || !nodeconf.NodeNameRe.MatchString(args[i]) {
				return fail(ui.Die("--node-name may contain only letters, digits, dot, dash, underscore (max 64)"))
			}
			opt.NodeName = args[i]
		case "--seq":
			i++
			if i >= len(args) || !promote.ValidSeq(args[i]) {
				return fail(ui.Die("--seq must be a positive number without a leading zero"))
			}
			opt.Seq = args[i]
		case "--version":
			fmt.Printf("monad-failover v%s\n", Version)
			return 0
		case "-h", "--help", "help":
			usage(argv0)
			return 0
		default:
			return fail(ui.Die("Unknown argument: " + args[i] + ". Run with --help for usage."))
		}
	}

	p, err := paths.FromEnv()
	if err != nil {
		return fail(err)
	}
	for _, problem := range hardening.Problems() {
		c.Warn("Hardening: " + problem + "; continuing.")
	}
	for _, o := range p.Overrides {
		c.Warn("Environment override in effect: " + o)
	}

	// An interrupt restores the terminal (hidden input turns echo off) and
	// leaves the run to --resume; every step records its progress before it
	// acts, so stopping at any point is safe.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		c.RestoreTerminal()
		fmt.Fprintf(os.Stderr, "\n%s✗%s Interrupted (%s).\n   If a run was in progress, continue it with: %s --resume\n", ui.Red, ui.Reset, sig, argv0)
		if sig == syscall.SIGTERM {
			os.Exit(143)
		}
		os.Exit(130)
	}()

	if dryRun {
		return promote.DryRun(c, p, opt.KeySourceDir, Version)
	}

	d := state.Resolve(p.Sandbox, p.StateDirOverride)
	r := promote.New(c, p, d, opt)
	proceed, lock, err := r.Prepare()
	if lock != nil {
		defer lock.Release()
	}
	if err != nil {
		return fail(err)
	}
	if !proceed {
		return 0
	}
	monad.Clear(os.Stdout)
	if err := r.Promote(); err != nil {
		return fail(err)
	}
	return 0
}
