package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drariux/drariux-cli/internal/config"
	"github.com/drariux/drariux-cli/internal/rpc"
)

var rpcCmd = &cobra.Command{
	Use:   "rpc",
	Short: "Manage the RPC endpoint the CLI points at",
	Long: `The RPC endpoint is stored in ~/.drariux/config.json and defaults to the
live node at https://node.drariux.network. Change it with "drariux rpc set <url>"
(alias: change).`,
}

var rpcShowCmd = &cobra.Command{
	Use:     "show",
	Aliases: []string{"get"},
	Short:   "Print the current RPC endpoint and chain settings",
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		fmt.Printf("rpc        : %s\n", cfg.RPC)
		fmt.Printf("chain id   : %d\n", cfg.ChainID)
		fmt.Printf("denom      : %s (18 decimals)\n", cfg.Denom)
		fmt.Printf("bech32     : %s\n", cfg.Prefix)
		fmt.Printf("gas price  : %s %s\n", cfg.GasPrice, cfg.Denom)
		fmt.Printf("account    : %s\n", orNone(cfg.Account))
		return nil
	},
}

var rpcSetCmd = &cobra.Command{
	Use:     "set",
	Aliases: []string{"change", "use", "endpoint"},
	Args:    cobra.ExactArgs(1),
	Short:   "Point the CLI at a different RPC node",
	Example: `  drariux rpc change http://127.0.0.1:8545
  drariux rpc set https://node.drariux.network`,
	RunE: func(c *cobra.Command, args []string) error {
		endpoint := args[0]
		if err := validateEndpoint(endpoint); err != nil {
			return err
		}
		cfg := mustConfig()
		cfg.RPC = strings.TrimRight(endpoint, "/")
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("✓ RPC set to %s\n", cfg.RPC)
		fmt.Println("  run 'drariux rpc test' to verify connectivity")
		return nil
	},
}

var rpcTest bool

var rpcResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the RPC endpoint back to the default (node.drariux.network)",
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		cfg.RPC = config.DefaultRPC
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("✓ RPC reset to default: %s\n", cfg.RPC)
		return nil
	},
}

var rpcPingCmd = &cobra.Command{
	Use:     "test",
	Aliases: []string{"ping"},
	Short:   "Check connectivity to the configured RPC (no state change)",
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		client := rpc.New(cfg.RPC)
		id, err := client.ChainID(ctx())
		if err != nil {
			return fmt.Errorf("RPC unreachable or invalid: %w", err)
		}
		height, err := client.BlockNumber(ctx())
		if err != nil {
			return fmt.Errorf("connected but block query failed: %w", err)
		}
		fmt.Printf("✓ RPC OK\n")
		fmt.Printf("  endpoint  : %s\n", cfg.RPC)
		fmt.Printf("  chain id  : %s\n", id.String())
		fmt.Printf("  height    : %d\n", height)
		if int(id.Int64()) != cfg.ChainID {
			fmt.Printf("  ⚠ chain id differs from config (%d) — is this the Drariux node?\n", cfg.ChainID)
		}
		return nil
	},
}

func validateEndpoint(s string) error {
	if s == "" {
		return fmt.Errorf("empty endpoint")
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(none — run: drariux wallet new)"
	}
	return s
}

func init() {
	_ = rpcTest
	rpcCmd.AddCommand(rpcShowCmd, rpcSetCmd, rpcResetCmd, rpcPingCmd)
}
