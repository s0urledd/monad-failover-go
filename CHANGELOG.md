# Changelog

## 0.4.0 — 2026-09-15

The tool no longer writes under `/opt/monad`.

- The identity backup taken before cutover and the key backups re-exported
  after it go to `/var/lib/monad-failover/backup/`, the run log to
  `/var/log/monad-failover/`. Both are root-only directories of the tool's
  own, so the permissions of `/opt/monad` no longer matter and this node's
  existing backups are never renamed. `BACKUP_ROOT` and `LOG_DIR` still
  override the locations.
- The validator's backups are read only from where the operator points:
  `--backup-dir`, or the directory asked for in the run. There is no
  default; the only directory the tool could have offered held this node's
  own keys. The dry run says so when no `--backup-dir` is given instead of
  checking the wrong files.
- The RPC sync check waits one second between its two head readings
  instead of three, and validates each public reference once.
- A run of 0.3.x interrupted after cutover records its backup under the
  old location; resume it with `BACKUP_ROOT=/opt/monad/backup`.

## 0.3.2 — 2026-09-15

- The dry run checks the backup and log roots the way the live run does:
  a symlink, a foreign owner or a directory other users can write to
  anywhere above them is a blocking finding, reported with the directory,
  its mode and the fix. The live run's refusal names the same fix and the
  environment variable that points elsewhere. On a standard install this
  is `/opt/monad` left group- or world-writable.

## 0.3.1 — 2026-09-15

Two adjustments to the RPC sync check, released so the README on main
describes a tagged build again.

- The second reading takes the public references first and the local head
  last, so the time the reference calls take counts in the node's favour.
  With that, the lag allowed is 5 blocks instead of 50; a local head more
  than 50 blocks past every reference still means the references are
  stale, not that the node is ahead.
- An in-sync node prints the same short line whatever the source:
  `Node: in-sync (block difference: N)`.

## 0.3.0 — 2026-09-15

Sync is judged over RPC when `monad-status` is not installed. The manual
"continue without sync check?" prompt is gone.

- Without `monad-status`, the node's own RPC (`127.0.0.1:8080`) is compared
  with the Foundation's public RPCs of the network: the chain id must match,
  the local head, read after the references, must be within 5 blocks of the
  network head, and it must advance between two readings three seconds
  apart. `eth_syncing` is read
  but never trusted on its own: the Monad RPC answers `false` for it
  unconditionally. A node that cannot be compared (RPC not answering,
  public RPCs unreachable) is "unverified" and the run stops before
  cutover rather than assuming sync.
- After cutover the same check runs inside the sync window; an unverified
  reading is retried and ends as "verification pending", as it did with
  `monad-status`, never as success.
- The dry run reports one sync result instead of two warnings about the
  missing tool, and says only "valid IKM format" for the backup files,
  with a note that the derived public keys are compared in the plan.
- Test doubles include JSON-RPC nodes; the flow tests cover the RPC path
  before and after cutover, a node behind, stalled or on another chain,
  and unreachable endpoints.

## 0.2.2 — 2026-09-14

- Use one plan confirmation for both interactive and flag-based migrations.
  The instruction to stop the old validator appears directly above it;
  confirming starts cutover without a separate STOPPED prompt.
- Keep the default answer as no. Interrupted input preserves the prepared
  run for resume; rejecting a fresh plan clears its staging and state.
- Update the README, reboot procedure and flow regression tests.

## 0.2.1 — 2026-09-14

Fixes from a pre-testnet audit. The cutover sequence is unchanged.

- Preflight and the dry run check that systemd knows the three units and
  that their unit files can be masked. A unit file under
  `/etc/systemd/system`, which `systemctl mask` cannot override, is refused
  before anything is stopped instead of at cutover with the old validator
  already down.
- `systemctl stop` has its own 15-minute deadline; execution can take
  minutes to flush on shutdown. A stop that still outlives it leaves the
  run resumable with nothing swapped.
- Ctrl-C and SIGTERM restore the terminal (hidden input turns echo off) and
  print the resume hint. Every step records its progress before it acts, so
  an interrupt anywhere is safe.
- A resume names the command-line inputs it does not use because their step
  already ran.
- `chain_id` in node.toml must agree with `network_name` (10143 for
  testnet, 143 for mainnet); a config edited by hand is refused.
- A `--public-ip` in a private, loopback or reserved range is warned about
  at signing and in the plan.
- Operator environment overrides in effect (`MONAD_HOME`, `BACKUP_ROOT`,
  `LOG_DIR`, `FOUNDATION_DATA_BASE` and the timing variables) are announced
  at start.
- The snapshot lookup compares the SECP key the same way as the BLS key,
  ignoring case and a `0x` prefix.
- Test doubles answer in the real tools' shapes: 66-hex SECP and 96-hex BLS
  keys without a prefix, and `systemctl show` honours the property asked
  for. The Foundation's published mainnet and testnet node.toml are
  fixtures; the migration runs over both and must leave every line it does
  not own unchanged.

## 0.2.0 — 2026-09-14

Plan and apply. The per-step questions are gone; one plan is shown and
confirmed before the switch.

- `--beneficiary`, `--node-name` and `--seq` give the inputs on the command
  line; whatever is missing is asked for. Flags are validated before the run
  starts and never skip a confirmation.
- Phase 7 shows the plan: hostname, network, public IP, address and ports,
  both public keys in full, what the Foundation snapshot knows about them,
  sequence, beneficiary and node name, each with where it came from (flag,
  entered, kept from node.toml, suggested by the snapshot). Then
  `proceed with this plan?` and `STOPPED`. The separate key, beneficiary and
  `proceed with cutover?` questions are removed.
- Rejecting the plan removes the staged files and the run state; the node is
  untouched and the next run starts clean. Once a cutover has begun, a
  rejection changes nothing and points at `--resume` and the identity backup.
- `--resume` shows the plan again, from the recorded state.
- The plan names the validator the snapshot lists for these keys. Keys the
  snapshot does not list, or a BLS key that differs from the listed entry,
  are warned about as a likely wrong backup. Snapshot names are stripped of
  control characters before they are printed.
- A `--public-ip` that differs from the address the host reports is warned
  about at signing and in the plan. On a resume after signing, a new
  `--public-ip` is reported as ignored.
- Tests for each item; the flow tests follow the new prompt order.

## 0.1.1 — 2026-09-14

Fixes from a code review before testnet validation. No new prompts or flags.

- A failed `systemctl stop` or a failed state query stops the run before any
  file is swapped; an unknown service state is never read as stopped.
- `monad-status` that exits non-zero is not evidence of sync, whatever it
  printed; subprocess calls carry deadlines and the post-cutover sync window
  is measured in real time.
- Key backups are written through uniquely named private temporary files
  with fsync and rename; backup and log directories are refused when they
  are symlinks, foreign-owned or reachable through a writable ancestor.
  Earlier exports are kept under unique `.bak` names.
- Preparation no longer touches the ownership or mode of the live config
  directory, `.env` or key files. Placement sets and checks ownership and
  mode on the three placed files, and checks them again before unmasking.
- Config values are read with TOML quoting and comment rules and must be
  unique in their table; a kept beneficiary is validated as an address before
  it is recorded. The operator can still type a beneficiary when the config
  has none.
- Signer output must carry the requested address and ports, a 130-hex-digit
  signature and a sequence within range, once each; the staged config is
  read back and compared before "patched and verified".
- Terminal input goes through a fixed buffer this code owns; consumed lines
  are zeroed from it. Hardening measures that did not apply are printed at
  start instead of being silently ignored.
- Regression tests for each item, including root-only ownership checks.


## 0.1.0 — 2026-09-14

First release.

- Single static linux/amd64 binary built from the Go standard library only.
  The build is reproducible; the README carries the release checksum and CI
  fails when the two drift apart.
- Eight-phase migration: preflight, host confirmation, backup of the target's
  identity, validator key import into protected staging, configuration on a
  staging copy, name record signing, cutover behind two typed confirmations,
  verification with fresh key backups. Every phase is resumable.
- Safeguards: units masked before the swap so a reboot cannot start a
  half-swapped node; checksums recorded at confirmation and never refreshed
  from disk; each file placed through a single `O_EXCL` open, verified before
  and after the rename; root-owned state with every field validated before
  use; an exclusive lock for the whole run; a started cutover refuses a fresh
  run.
- Run-time requirements: `systemctl`, `monad-keystore`,
  `monad-sign-name-record` and, for sync checks, `monad-status`.
- Test suite of 104 functions, including in-process flow tests that drive the
  full migration against mock Monad binaries and inject faults between file
  placements. Runs without root, network or systemd.
- Secrets are held as bytes and zeroed after use; free memory is returned to
  the kernel after every command that carried a secret; the process cannot
  dump core, is not dumpable and, as root, is never paged out to swap.
- Not yet used on a live network.
