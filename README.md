# monad-failover

[![ci](https://github.com/s0urledd/monad-failover-go/actions/workflows/ci.yml/badge.svg)](https://github.com/s0urledd/monad-failover-go/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](go.mod)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Promotes a synced Monad full node to a validator, following the official
[node migration](https://docs.monad.xyz/node-ops/node-recovery/node-migration) procedure.
Use it for a planned migration or recovery when the old server is unavailable.
It runs on the target full node using your validator key backups, with no
connection to the old server required.

0.2.2 is under validation on testnet. Do not use it on a mainnet validator
before 1.0.

## How it works

1. With your synced full node and validator backups ready, the tool backs up
   the target's identity and imports the validator keys into protected staging.
   It takes the beneficiary, node name and sequence number from flags or
   prompts, signs the name record and checks the staged config. The full node
   keeps running.
2. Preparation and signing are complete before the switch. The tool shows the
   plan: host, public IP, both public keys, what the Foundation snapshot knows
   about them, sequence, beneficiary and node name, each with where it came
   from. Stop the old validator (or ensure it is offline), then confirm the plan
   to begin cutover. Rejecting the plan removes the staged files and the run
   state; nothing on the node has changed.
3. The tool rechecks the prepared files, masks and stops the target services,
   places the validator keys and config, verifies the placed files, and starts
   the services. It then checks service health and sync and exports fresh key
   backups. Pending sync is reported explicitly.

## What you need

- A synced full node with the standard Monad setup and `KEYSTORE_PASSWORD`
  set in `/home/monad/.env`.
- Your validator's `secp-backup` and `bls-backup`: the text backups containing
  the secret IKM, not the encrypted `id-secp` / `id-bls` keystores.
  Place them in a private directory of their own on the target (directory
  `700`, files `600`); `/opt/monad/backup` holds this node's own backups.
  These are unencrypted secrets; keep off-server copies. Hidden manual IKM
  entry is also available.
- The validator's SECP and BLS public keys to compare against the plan.
- Its beneficiary address and `node_name`, from your saved validator config.
  A blank beneficiary keeps the target's existing address; the plan shows it as kept.

The Foundation snapshot suggests a sequence when it has a matching record.
Use a number higher than every sequence this identity has used, even if that
exceeds the suggestion. Without a usable record, enter the number yourself.

## Install

On the target full node, as root. The checksum is verified before the
binary is installed:

```bash
curl -fsSLO https://github.com/s0urledd/monad-failover-go/releases/download/v0.2.2/monad-failover &&
echo "4572daf38b6fd39d137e5869b93cafc447b6d2214c91576ba4c84c0d9c613fdb  monad-failover" | sha256sum -c - &&
install -m 755 monad-failover /usr/local/bin/monad-failover
```

To build from source, install Go 1.24.7 or later and run:

```bash
git clone https://github.com/s0urledd/monad-failover-go
cd monad-failover-go && git checkout v0.2.2
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o monad-failover ./cmd/monad-failover
install -m 755 monad-failover /usr/local/bin/monad-failover
```

The binary is static. It calls `systemctl`, `monad-keystore`,
`monad-sign-name-record` and, for sync checks, `monad-status`.

## Run

```bash
# Check the node and the backup files. Nothing is changed.
monad-failover --dry-run --backup-dir /path/to/validator-backups

# Migrate. Asks where the backups are, for the beneficiary, node name and
# sequence number, then shows the plan. Stop the old validator before
# confirming the plan to begin cutover.
monad-failover
```

The answers can be given up front. The same plan confirmation is still required:

```bash
monad-failover --backup-dir /path/to/validator-backups \
  --beneficiary 0xADDRESS --node-name NAME --seq N
```

| Flag | Effect |
|---|---|
| `--dry-run` | Checks only; nothing is changed |
| `--backup-dir PATH` | Directory holding the validator's two backup files. The run asks for it if omitted |
| `--beneficiary 0xADDRESS` | Beneficiary address |
| `--node-name NAME` | `node_name` taken over from the old validator |
| `--seq N` | Name record sequence, higher than any this identity has used |
| `--public-ip IP` | Only when detection fails or is wrong: use this IPv4 in the name record |
| `--resume` | Continue an interrupted run |
| `--version` | Print the tool version |

## If a run is interrupted

Run `monad-failover --resume`, including after a partial cutover. Do not start
a fresh migration over an unfinished swap. Logs are in `/opt/monad/failover-logs/`.

The saved `/opt/monad/backup/failover-<timestamp>/` restores this server's
original full-node identity. It does not move the validator back to the old
server. See [recovery](docs/recovery.md) if resume cannot finish.

## Compatibility and operator notes

Maintained to track Monad updates. The tool targets standard P2P ports:
TCP/UDP `8000` and authenticated UDP `8001`. Custom P2P ports are not supported.

- Block public access to RPC and metrics ports (8080, 8081, 9143, etc.).
  Allow trusted sources only.
- Configure [VDP metrics](https://docs.monad.xyz/node-ops/validator-delegation-program)
  on the target and check its firewall.
- Update downstream peers with the new name record. Transfer any custom
  dedicated-full-node configuration separately; the tool edits the target's config.

[SECURITY.md](SECURITY.md) explains key handling and external requests, including
the optional monval uptime lookup operated by Huginn.

## Uninstall

After verification, `sudo rm -- /usr/local/bin/monad-failover` removes the tool.
Monad, backups and logs stay in place. Keep the backups for recovery.

[MIT licensed](LICENSE).
