# drariux

A lightweight CLI for the **Drariux** chain (EVM chain id `9854`). Create local
wallets, query the node, and **dry-run / publish** transactions and smart
contracts against an Ethermint node — defaulting to `https://node.drariux.network`.

Single static Go binary (~8 MB), no runtime dependencies.

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/jhonnycarpio98-droid/drariuxcli/main/install.sh | sh
```

Pinned version / custom repo:

```sh
DRARIUX_VERSION=v0.1.0 curl -fsSL .../install.sh | sh
```

Or from source:

```sh
make build   # produces ./drariux
# or: go build -ldflags="-s -w" -o drariux .
```

---

## Quick start

```sh
drariux wallet new --name alice   # create a local, unfunded wallet
drariux chain info                # endpoint, chain id, height, gas price
drariux wallet balance alice      # on-chain balance + nonce (read-only)
```

New wallets start with **zero tokens**. Fund them (airdrop / bridge / transfer)
before sending real transactions.

---

## Commands

### Wallet
| Command | Description |
|---|---|
| `drariux wallet new [--name <label>]` | Create a secp256k1 wallet (EVM + Cosmos addresses) |
| `drariux wallet list` (`ls`) | List local wallets |
| `drariux wallet show [name\|addr]` | Show public details |
| `drariux wallet balance [name\|addr]` (`bal`) | Query on-chain balance + nonce |
| `drariux wallet use <name\|addr>` | Set the default account for tx/contract |
| `drariux wallet import --key <hex>` | Import a 32-byte hex private key |

### RPC endpoint
| Command | Description |
|---|---|
| `drariux rpc show` | Print current endpoint + chain settings |
| `drariux rpc set <url>` (`change`, `use`) | Point the CLI at a different node |
| `drariux rpc reset` | Back to default `node.drariux.network` |
| `drariux rpc test` (`ping`) | Verify connectivity (no state change) |

```sh
drariux rpc change http://127.0.0.1:8545   # e.g. a local node over SSH tunnel
drariux rpc test                            # -> chain id, height
```

### Chain
| Command | Description |
|---|---|
| `drariux chain info` (`status`) | endpoint, chain id, latest height, gas price |
| `drariux chain height` | print latest block number only (script-friendly) |

### Transactions
| Command | Description |
|---|---|
| `drariux tx dryrun --to <addr> [--value <drx>] [--data <hex>] [--from <acct>]` | Simulate a transfer/call: gas, projected fee, revert reason. **Read-only.** |
| `drariux tx send --to <addr> --value <drx> [--from <acct>] [--dry-run] [--yes]` | Sign + broadcast. Always simulates first; needs `--yes` to actually send. |

```sh
# estimate the cost of a 10 DRX transfer (no broadcast)
drariux tx dryrun --from alice --to 0xAbC... --value 10

# after reviewing the simulation, broadcast it
drariux tx send --from alice --to 0xAbC... --value 10 --yes
```

### Contracts
| Command | Description |
|---|---|
| `drariux contract publish --bytecode <hex>` **or** `--bytecode-file <path>` `[--args <hex>] [--from <acct>] [--dry-run] [--yes]` | Deploy. Simulates the constructor first (deployment gas + revert), broadcasts only with `--yes`. |
| `drariux contract call <addr> --data <selector+args>` | `eth_call` a read function (free) |
| `drariux contract code <addr>` | Show deployed bytecode |
| `drariux contract decode <hex>` | Decode a revert / panic reason from raw data |

```sh
# validate a deployment without spending anything
drariux contract publish --bytecode-file ./Hello.bin --from alice --dry-run

# read a contract: totalSupply() selector 0x18160ddd
drariux contract call 0xAbC... --data 0x18160ddd

# translate a revert payload
drariux contract decode 0x08c379a0...   # -> Error(string) reason
```

---

## Dry-run / gas simulation

`tx dryrun` and `contract publish --dry-run` use the same Ethermint read paths
(`eth_estimateGas` + `eth_call`) the node uses internally. They:

- report the **gas estimate** and **projected fee** (`gas × gasPrice` in DRX),
- flag **insufficient-funds** before you waste a broadcast,
- surface **revert / panic reasons** decoded from the ABI
  (`Error(string)` and `Panic(uint256)` codes).

The simulation never signs and never mutates state — the `--from` is only used
as an address for balance/nonce checks. This is the intended workflow for
contract developers to catch errors and measure gas before publishing.

---

## Files & configuration

| Path | Purpose |
|---|---|
| `~/.drariux/config.json` | endpoint, chain id, denom, default account |
| `~/.drariux/keystore/<evm>.json` | one file per wallet (private key hex, mode 0600) |

Defaults: RPC `https://node.drariux.network`, chain id `9854`, denom `draxiux`
(18 decimals), bech32 prefix `drariux`.

### Security notes
- Keystore files store the **private key in hex** (0600). Back them up and keep
  them out of version control. Add OS-keychain / encrypt-keyfile storage before
  using this with mainnet value.
- The 12-word string shown by `wallet new` is informational only — it is **not**
  a BIP-39 recovery phrase. The recoverable secret is the keystore file.
- The first wallet created becomes the default account automatically.

---

## Build & release

```sh
make dist      # cross-compile linux/darwin × amd64/arm64 into dist/
make release   # gh release create + upload artifacts
```

Pushing a `v*` tag also triggers `.github/workflows/release.yaml`, which builds
every target and attaches them to the GitHub Release that `install.sh` reads.

## License

MIT
