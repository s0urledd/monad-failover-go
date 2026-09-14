# Changelog

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
