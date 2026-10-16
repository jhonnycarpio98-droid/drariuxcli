package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/drariux/drariux-cli/internal/config"
	"github.com/drariux/drariux-cli/internal/keyring"
	"github.com/drariux/drariux-cli/internal/rpc"
)

// Node defaults. The release files (stripped drariuxd.gz + genesis.json) are
// served from a plain directory over HTTP by IP — no DNS involved.
const (
	DefaultNodeBase   = "http://16.16.47.157:8788"
	DefaultSeedPeer   = "deded3a56be267839c6940be0c13f0e0f4d55204@51.21.239.184:26656"
	DefaultChainIDStr = "drariux_9854-1"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Run a Drariux full node / validator from this device",
	Long: `Join the live Drariux chain as an additional node.

This only reads the public chain and runs the node binary — it never modifies
the chain or any deployed contract. PoS bonding uses the chain's native staking
module (x/staking), so a new validator is created with a normal transaction.

  drariux node start --moniker mynode --bond 500   # download + sync + (opt) validator
  drariux node status                               # height, sync %, peers, chain match
  drariux node fund                                 # show how to receive funds (never sends/withdraws)
  drariux node bond                                 # lock the chosen DRX for PoS once synced
  drariux node stop`,
}

// ---- shared flags ----
var (
	nodeBase   string
	nodeHome   string
	nodeMonik  string
	nodePeers  string
	nodeBond   string
	nodeYes    bool
	nodeWallet string
)

// nodeDirs is the layout for one node instance.
type nodeDirs struct {
	Root    string // ~/.drariux/nodes/<moniker>
	Home    string // drariuxd --home
	Bin     string
	LogFile string
	Meta    string
}

type nodeMeta struct {
	Moniker   string `json:"moniker"`
	ChainID   string `json:"chain_id"`
	Home      string `json:"home"`
	Bin       string `json:"bin"`
	NodeID    string `json:"node_id"`
	P2P       string `json:"p2p"`
	RPC       string `json:"rpc"`
	EVMRPC    string `json:"evm_rpc"`
	P2PPort   int    `json:"p2p_port,omitempty"`
	RPCPort   int    `json:"rpc_port,omitempty"`
	EVMPort   int    `json:"evm_port,omitempty"`
	Operator  string `json:"operator_address,omitempty"`
	ValEVM    string `json:"validator_evm,omitempty"`
	BondDRX   string `json:"bond_drx,omitempty"`
	Bonded    bool   `json:"bonded"`
	FundedDRX string `json:"funded_drx,omitempty"`
	Created   string `json:"created"`
}

func nodeBaseURL() string {
	if v := os.Getenv("DRARIUX_NODE_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return nodeBase
}

func dirsFor(moniker string) (*nodeDirs, error) {
	h, err := config.Home()
	if err != nil {
		return nil, err
	}
	if moniker == "" {
		moniker = "node"
	}
	// The moniker comes from user input and becomes a path element: keep it
	// inside the nodes directory (no separators, no dot-only names).
	if strings.ContainsAny(moniker, "/\\") || moniker == "." || moniker == ".." {
		return nil, fmt.Errorf("invalid --moniker %q: must not contain path separators", moniker)
	}
	root := filepath.Join(h, "nodes", moniker)
	if base := filepath.Join(h, "nodes"); !strings.HasPrefix(root, base+string(os.PathSeparator)) {
		return nil, fmt.Errorf("invalid --moniker %q", moniker)
	}
	return &nodeDirs{
		Root:    root,
		Home:    filepath.Join(root, "data"),
		Bin:     filepath.Join(root, "drariuxd"),
		LogFile: filepath.Join(root, "node.log"),
		Meta:    filepath.Join(root, "node.json"),
	}, nil
}

func loadMeta(d *nodeDirs) (*nodeMeta, error) {
	b, err := os.ReadFile(d.Meta)
	if err != nil {
		return nil, err
	}
	m := &nodeMeta{}
	return m, json.Unmarshal(b, m)
}

func saveMeta(d *nodeDirs, m *nodeMeta) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(d.Meta, b, 0o600)
}

// httpGet downloads url into dst with a hard deadline (never hangs forever).
func httpGet(url, dst string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// runExec executes a command with a deadline and returns its output.
func runExec(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// rpcPort picks free-ish default ports for a secondary node.
var nodeStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Download the chain binary and start syncing a new node",
	Example: `  drariux node start --moniker mi-nodo
  drariux node start --moniker val1 --bond 500 --yes`,
	RunE: func(c *cobra.Command, args []string) error {
		moniker := nodeMonik
		if moniker == "" {
			moniker = "drariux-" + strconv.FormatInt(time.Now().Unix(), 10)
		}
		d, err := dirsFor(moniker)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(d.Root, 0o755); err != nil {
			return err
		}

		// 1. binary (gunzip when the release ships .gz). The release folder carries
		// per-arch static builds (drariuxd_linux_amd64.gz / _arm64.gz) so the same
		// command works on x86_64 servers and 64-bit Termux; fall back to the
		// single drariuxd.gz if only one asset is published.
		if _, err := os.Stat(d.Bin); err != nil {
			base := nodeBaseURL()
			fmt.Printf(">> downloading drariuxd from %s for %s/%s (this is ~40 MB)\n", base, "linux", runtime.GOARCH)
			assets := []string{"drariuxd_linux_" + runtime.GOARCH + ".gz"}
			// The generic drariuxd.gz is an amd64 build; only fall back to it on
			// amd64 so another arch never downloads an incompatible binary.
			if runtime.GOARCH == "amd64" {
				assets = append(assets, "drariuxd.gz")
			}
			var derr error
			got := false
			for _, a := range assets {
				derr = httpGet(base+"/"+a, d.Bin+".gz", 15*time.Minute)
				if derr == nil {
					got = true
					break
				}
			}
			if !got {
				return fmt.Errorf("no drariuxd build published for linux/%s at %s (looked for: %s). Run 'drariux node start' on a supported device, or ask for a %s build to be hosted.", runtime.GOARCH, base, strings.Join(assets, ", "), runtime.GOARCH)
			}
			out, err := runExec(5*time.Minute, "gzip", "-d", "-f", d.Bin+".gz")
			if err != nil {
				return fmt.Errorf("gunzip failed: %v %s", err, out)
			}
			if err := os.Chmod(d.Bin, 0o755); err != nil {
				return err
			}
		} else {
			fmt.Printf(">> binary already present at %s\n", d.Bin)
		}

		// 2. init home (idempotent) + fetch genesis
		genesis := filepath.Join(d.Home, "config", "genesis.json")
		if _, err := os.Stat(genesis); err != nil {
			fmt.Println(">> initializing node home")
			out, err := runExec(time.Minute, d.Bin, "init", moniker,
				"--home", d.Home, "--chain-id", DefaultChainIDStr)
			if err != nil {
				return fmt.Errorf("init failed: %v\n%s", err, trimOut(out))
			}
			fmt.Println(">> fetching genesis")
			if err := httpGet(nodeBaseURL()+"/genesis.json", genesis, 2*time.Minute); err != nil {
				return fmt.Errorf("genesis download failed: %w", err)
			}
		}

		// 3. config: pick free ports (this box may already run an ssh tunnel or
		// another node) + persistent peers
		peers := nodePeers
		if peers == "" {
			peers = DefaultSeedPeer
		}
		ports := pickPorts()
		cfgPath := filepath.Join(d.Home, "config", "config.toml")
		patches := []tomlSet{
			{Section: "p2p", Key: "laddr", Value: fmt.Sprintf("tcp://0.0.0.0:%d", ports.P2P)},
			{Section: "p2p", Key: "persistent_peers", Value: peers},
			{Section: "rpc", Key: "laddr", Value: fmt.Sprintf("tcp://127.0.0.1:%d", ports.RPC)},
			{Section: "json-rpc", Key: "address", Value: fmt.Sprintf("127.0.0.1:%d", ports.EVM)},
			{Section: "json-rpc", Key: "ws-address", Value: fmt.Sprintf("127.0.0.1:%d", ports.WS)},
			{Section: "api", Key: "address", Value: fmt.Sprintf("tcp://127.0.0.1:%d", ports.API)},
		}
		if err := patchToml(cfgPath, patches); err != nil {
			return err
		}

		// 4. validator key + bond selection (native x/staking, no contract changes)
		m, _ := loadMeta(d)
		if m == nil {
			m = &nodeMeta{Moniker: moniker, ChainID: DefaultChainIDStr, Created: time.Now().UTC().Format(time.RFC3339)}
		}
		if nodeBond != "" && nodeBond != "0" && m.Operator == "" {
			out, err := runExec(time.Minute, d.Bin, "keys", "add", "validator",
				"--keyring-backend", "test", "--home", d.Home, "--output", "json")
			if err != nil {
				return fmt.Errorf("validator key creation failed: %v\n%s", err, trimOut(out))
			}
			var kp struct {
				Address string `json:"address"`
				Name    string `json:"name"`
			}
			if e := json.Unmarshal([]byte(extractJSON(out)), &kp); e != nil || kp.Address == "" {
				return fmt.Errorf("could not parse validator address from: %s", trimOut(out))
			}
			evmAddr, err := keyring.Bech32ToEVM(kp.Address)
			if err != nil {
				return fmt.Errorf("validator address %q is not a Drariux bech32 account: %w", kp.Address, err)
			}
			m.Operator = kp.Address
			m.ValEVM = evmAddr
			m.BondDRX = nodeBond
		}
		m.Home, m.Bin = d.Home, d.Bin
		m.P2PPort, m.RPCPort, m.EVMPort = ports.P2P, ports.RPC, ports.EVM
		m.P2P = fmt.Sprintf("tcp://0.0.0.0:%d", ports.P2P)
		m.RPC = fmt.Sprintf("tcp://127.0.0.1:%d", ports.RPC)
		m.EVMRPC = fmt.Sprintf("http://127.0.0.1:%d", ports.EVM)
		m.Moniker, m.ChainID = moniker, DefaultChainIDStr
		target := seedHeight()

		// 5. launch detached (never blocks the CLI)
		if pid := nodePID(d); pid > 0 {
			fmt.Printf(">> node already running (pid %d)\n", pid)
		} else {
			if err := startDaemon(d, ports); err != nil {
				return err
			}
		}
		if err := saveMeta(d, m); err != nil {
			return err
		}

		rows := [][2]string{
			{"moniker", m.Moniker},
			{"chain id", m.ChainID},
			{"home", m.Home},
			{"binary", m.Bin},
			{"p2p", m.P2P},
			{"rpc", m.RPC},
			{"evm rpc", m.EVMRPC},
			{"peers", peers},
			{"sync target", fmt.Sprintf("%d", target)},
			{"log", d.LogFile},
		}
		if m.Operator != "" {
			rows = append(rows,
				[2]string{"validator op", m.Operator},
				[2]string{"validator evm", m.ValEVM},
				[2]string{"poS bond", m.BondDRX + " DRX (run: drariux node bond --yes)"},
			)
		} else {
			rows = append(rows, [2]string{"role", "full node (no bond; add --bond <DRX> to validate)"})
		}
		box("Node starting · "+m.Moniker, rows)
		fmt.Println()
		fmt.Println("  follow with:  drariux node status   |   drariux node logs")
		if m.Operator != "" {
			fmt.Printf("  to fund: have a third party send DRX to %s, then: drariux node bond --yes\n", m.ValEVM)
		}
		return nil
	},
}

// patchToml rewrites `key = "value"` inside a specific [section] of a TOML
// file. Section-aware because config.toml has several keys named `laddr`.
type tomlSet struct {
	Section string
	Key     string
	Value   string
}

func patchToml(path string, sets []tomlSet) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	section := ""
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			section = strings.Trim(t, "[]")
			continue
		}
		for _, s := range sets {
			if s.Section != section {
				continue
			}
			if strings.HasPrefix(t, s.Key+" ") || strings.HasPrefix(t, s.Key+"=") {
				lines[i] = fmt.Sprintf("%s = %q", s.Key, s.Value)
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// nodePorts are the local listen ports for one node instance.
type nodePorts struct{ P2P, RPC, EVM, WS, API int }

// portFree checks whether a TCP port can be bound right now.
func portFree(p int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// pickPorts keeps the chain defaults (26656/26657/8545/8546/1317) when they are
// free and otherwise shifts the whole set by +100, so starting a node on a box
// that already tunnels or runs one never fails with "address already in use".
func pickPorts() nodePorts {
	for _, off := range []int{0, 100, 200, 300} {
		p := nodePorts{26656 + off, 26657 + off, 8545 + off, 8546 + off, 1317 + off}
		if portFree(p.P2P) && portFree(p.RPC) && portFree(p.EVM) && portFree(p.WS) && portFree(p.API) {
			return p
		}
	}
	return nodePorts{26656, 26657, 8545, 8546, 1317}
}

func startDaemon(d *nodeDirs, ports nodePorts) error {
	lf, err := os.OpenFile(d.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(d.Bin, "start", "--home", d.Home, "--chain-id", DefaultChainIDStr,
		"--p2p.laddr", fmt.Sprintf("tcp://0.0.0.0:%d", ports.P2P),
		"--rpc.laddr", fmt.Sprintf("tcp://127.0.0.1:%d", ports.RPC),
		"--json-rpc.address", fmt.Sprintf("127.0.0.1:%d", ports.EVM),
		"--json-rpc.ws-address", fmt.Sprintf("127.0.0.1:%d", ports.WS))
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("could not start drariuxd: %w", err)
	}
	_ = os.WriteFile(filepath.Join(d.Root, "node.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	lf.Close()
	go func() { _ = cmd.Wait() }()
	return nil
}

func nodePID(d *nodeDirs) int {
	b, err := os.ReadFile(filepath.Join(d.Root, "node.pid"))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		return 0
	}
	if p, err := os.FindProcess(pid); err == nil {
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return 0
		}
	}
	return pid
}

// statusRPC asks the *local* node for its CometBFT status.
type cometStatus struct {
	NodeInfo struct {
		Moniker string `json:"moniker"`
		Net     string `json:"network"`
	} `json:"node_info"`
	SyncInfo struct {
		LatestBlockHeight string `json:"latest_block_height"`
		Catching          bool   `json:"catching_up"`
	} `json:"sync_info"`
	ValidatorInfo struct {
		VotingPower string `json:"voting_power"`
	} `json:"validator_info"`
}

func localStatus(timeout time.Duration) (*cometStatus, error) {
	port := 26657
	if m, _, err := currentMeta(); err == nil && m.RPCPort > 0 {
		port = m.RPCPort
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/status", port), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var wrap struct {
		Result cometStatus `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		return nil, err
	}
	return &wrap.Result, nil
}

// seedHeight reads the canonical chain height from the public RPC (used to
// validate that this node is on the same chain and to compute sync %).
func seedHeight() uint64 {
	cfg, err := config.Load()
	if err != nil {
		return 0
	}
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	n, err := rpc.New(cfg.RPC).BlockNumber(c)
	if err != nil {
		return 0
	}
	return n
}

func seedChainMatches(localNet string) (bool, string) {
	cfg, err := config.Load()
	if err != nil {
		return false, "config unavailable"
	}
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	id, err := rpc.New(cfg.RPC).ChainID(c)
	if err != nil {
		return false, "public RPC unreachable: " + err.Error()
	}
	want := strconv.FormatInt(int64(cfg.ChainID), 10)
	got := id.String()
	if got != want {
		return false, fmt.Sprintf("public RPC reports chain id %s, config says %s", got, want)
	}
	// Comet network name is drariux_9854-1; the EVM chain id is 9854.
	if localNet != "" && !strings.Contains(localNet, want) {
		return false, fmt.Sprintf("local network %q does not carry chain id %s", localNet, want)
	}
	return true, "chain id " + want + " (public RPC + local node agree)"
}

var nodeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local node height, sync progress, peers and chain match",
	RunE: func(c *cobra.Command, args []string) error {
		st, err := localStatus(8 * time.Second)
		if err != nil {
			return fmt.Errorf("local node not answering: %w\n  (start it: drariux node start)", err)
		}
		h, _ := strconv.ParseUint(st.SyncInfo.LatestBlockHeight, 10, 64)
		target := seedHeight()
		pct := 0.0
		if target > 0 {
			pct = float64(h) / float64(target) * 100
			if pct > 100 {
				pct = 100
			}
		}
		ok, why := seedChainMatches(st.NodeInfo.Net)
		state := "syncing"
		if !st.SyncInfo.Catching {
			state = "in sync ✓"
		}
		box("Node status", [][2]string{
			{"moniker", st.NodeInfo.Moniker},
			{"network", st.NodeInfo.Net},
			{"height", fmt.Sprintf("%d", h)},
			{"canonical height", fmt.Sprintf("%d", target)},
			{"synced", fmt.Sprintf("%.2f%%", pct)},
			{"state", state},
			{"voting power", st.ValidatorInfo.VotingPower},
			{"chain match", fmt.Sprintf("%v (%s)", ok, why)},
		})
		return nil
	},
}

var nodeStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local node",
	RunE: func(c *cobra.Command, args []string) error {
		d, err := resolveDirs()
		if err != nil {
			return err
		}
		pid := nodePID(d)
		if pid == 0 {
			fmt.Println("no running node found for this moniker")
			return nil
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		if err := p.Signal(syscall.SIGTERM); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(d.Root, "node.pid"))
		fmt.Printf("✓ sent SIGTERM to drariuxd (pid %d)\n", pid)
		return nil
	},
}

var nodeLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail the node log",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		d, err := resolveDirs()
		if err != nil {
			return err
		}
		out, err := runExec(15*time.Second, "tail", "-n", "25", d.LogFile)
		fmt.Print(out)
		return err
	},
}

var nodeFundCmd = &cobra.Command{
	Use:   "fund",
	Short: "Show how to top up the node's validator account (receive-only: never sends or withdraws)",
	Long: `Security model: the CLI never moves money on your behalf.

The validator account is a RECEIVE-ONLY address from the tool's point of view:
  • Funds must arrive from a third party through a NORMAL transfer, exactly like
    any other address (that wallet runs: drariux tx send --to <validator> ...).
  • This command does NOT withdraw from the node wallet and does NOT use any
    contract. There is deliberately no programmatic funding, to avoid exploits.

Once the account holds DRX, anchor it to PoS with:  drariux node bond --yes`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		m, _, err := currentMeta()
		if err != nil {
			return err
		}
		if m.ValEVM == "" {
			return fmt.Errorf("this node has no validator account (start with --bond <DRX>)")
		}
		box("Fondear el nodo · recibir", [][2]string{
			{"validator evm", m.ValEVM},
			{"operator", m.Operator},
			{"cómo fondear", "que un tercero envíe DRX normalmente a la dirección de arriba"},
			{"desde esa wallet", fmt.Sprintf("drariux tx send --to %s --value <DRX> --yes", m.ValEVM)},
			{"luego anclar", "drariux node bond --yes"},
			{"nota", "el CLI nunca retira de la wallet del nodo ni usa contratos"},
		})
		return nil
	},
}

var nodeBondCmd = &cobra.Command{
	Use:   "bond",
	Short: "Lock DRX for PoS using the chain's native staking module",
	Long: `Creates a validator with the bond amount chosen at 'node start --bond <DRX>'.

This is a normal x/staking MsgCreateValidator transaction broadcast through the
local node — it does not deploy or modify any contract, and it respects the
chain's existing parameters (bond denom draxiux, max validators 100).`,
	RunE: func(c *cobra.Command, args []string) error {
		m, d, err := currentMeta()
		if err != nil {
			return err
		}
		if m.Operator == "" {
			return fmt.Errorf("no validator key: re-run drariux node start --bond <DRX>")
		}
		if m.Bonded {
			fmt.Printf("✓ already bonded (%s DRX)\n", m.BondDRX)
			return nil
		}
		st, err := localStatus(8 * time.Second)
		if err != nil {
			return err
		}
		if st.SyncInfo.Catching {
			return fmt.Errorf("node still syncing at height %s — wait for 'drariux node status' to show in sync", st.SyncInfo.LatestBlockHeight)
		}
		if !nodeYes {
			return fmt.Errorf("this locks %s DRX for PoS; re-run with --yes to confirm", m.BondDRX)
		}
		pubOut, err := runExec(time.Minute, m.Bin, "comet", "show-validator", "--home", m.Home)
		if err != nil {
			return fmt.Errorf("show-validator failed: %v %s", err, trimOut(pubOut))
		}
		pubKey := strings.TrimSpace(pubOut)
		bondAmt := strings.TrimSpace(m.BondDRX)
		if bondAmt == "" {
			bondAmt = "1"
		}
		out, err := runExec(2*time.Minute, m.Bin, "tx", "staking", "create-validator",
			"--home", m.Home,
			"--from", "validator",
			"--keyring-backend", "test",
			"--chain-id", m.ChainID,
			"--moniker", m.Moniker,
			"--pubkey", pubKey,
			"--amount", strings.ReplaceAll(bondAmt, ".", "")+"draxiux",
			"--gas", "auto",
			"--gas-prices", "2000000000draxiux",
			"--gas-adjustment", "1.6",
			"--commission-rate", "0.05",
			"--commission-max-rate", "0.20",
			"--commission-max-change-rate", "0.01",
			"--min-self-delegation", "1",
			"--broadcast-mode", "sync",
			"-y",
		)
		if err != nil {
			return fmt.Errorf("create-validator failed: %v\n%s", err, trimOut(out))
		}
		m.Bonded = true
		_ = saveMeta(d, m)
		box("Validator created ✓", [][2]string{
			{"operator", m.Operator},
			{"bonded", fmtDRX_must(m.BondDRX)},
			{"moniker", m.Moniker},
			{"note", "bonded via native x/staking; no contract was touched"},
		})
		return nil
	},
}

func currentMeta() (*nodeMeta, *nodeDirs, error) {
	moniker, err := resolveMoniker()
	if err != nil {
		return nil, nil, err
	}
	d, err := dirsFor(moniker)
	if err != nil {
		return nil, nil, err
	}
	m, err := loadMeta(d)
	if err != nil {
		return nil, nil, fmt.Errorf("no metadata for %q: run drariux node start first", moniker)
	}
	return m, d, nil
}

// resolveMoniker returns --moniker when set; otherwise, if exactly one node
// exists, its name. This lets status/logs/stop work without repeating the
// moniker once a single node is present.
func resolveMoniker() (string, error) {
	if nodeMonik != "" {
		return nodeMonik, nil
	}
	h, err := config.Home()
	if err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(filepath.Join(h, "nodes"))
	switch len(entries) {
	case 0:
		return "", fmt.Errorf("no nodes yet: drariux node start --moniker <name>")
	case 1:
		return entries[0].Name(), nil
	default:
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return "", fmt.Errorf("multiple nodes, pass --moniker: %s", strings.Join(names, ", "))
	}
}

// resolveDirs is the moniker-aware directories lookup used by logs/stop, so
// they behave like status when a single node is present.
func resolveDirs() (*nodeDirs, error) {
	moniker, err := resolveMoniker()
	if err != nil {
		return nil, err
	}
	return dirsFor(moniker)
}

func trimOut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

func extractJSON(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return "{}"
	}
	return s[i : j+1]
}

func fmtDRX_must(whole string) string {
	n, ok := new(big.Int).SetString(strings.ReplaceAll(whole, ".", ""), 10)
	if !ok {
		return whole + " DRX"
	}
	return fmtDRX(n)
}

func init() {
	for _, c := range []*cobra.Command{nodeStartCmd, nodeStopCmd, nodeLogsCmd, nodeFundCmd, nodeBondCmd, nodeStatusCmd} {
		c.Flags().StringVar(&nodeBase, "base-url", DefaultNodeBase, "release files base URL (dir listing, no DNS)")
		c.Flags().StringVar(&nodeMonik, "moniker", "", "node name (instance id)")
	}
	nodeStartCmd.Flags().StringVar(&nodePeers, "peers", "", "comma-separated p2p peers (default: the live seed node)")
	nodeStartCmd.Flags().StringVar(&nodeBond, "bond", "", "DRX to lock for PoS via native staking (empty = full node only)")
	nodeBondCmd.Flags().BoolVar(&nodeYes, "yes", false, "confirm the PoS bond")
	nodeCmd.AddCommand(nodeStartCmd, nodeStatusCmd, nodeStopCmd, nodeLogsCmd, nodeFundCmd, nodeBondCmd)
}
