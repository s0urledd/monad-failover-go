# Changelog

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
