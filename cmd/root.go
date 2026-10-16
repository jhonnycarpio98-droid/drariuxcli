// Package cmd wires the drariux cobra command tree.
package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/drariux/drariux-cli/internal/config"
	"github.com/drariux/drariux-cli/internal/rpc"
)

// Version is injected at build time via -ldflags.
var Version = "dev"

const rootLong = `drariux — a lightweight client for the Drariux chain (EVM chain id 9854).

Create local wallets, query the chain, and dry-run / publish transactions and
smart contracts against an Ethermint node. Default RPC: node.drariux.network.

Data lives in ~/.drariux (config.json + keystore/).`

var rootCmd = &cobra.Command{
	Use:           "drariux",
	Short:         "Drariux blockchain CLI",
	Long:          rootLong,
	Version:       Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(c *cobra.Command, args []string) {
		// Bare `drariux`: greet with the wordmark, then show help.
		PrintBanner()
		fmt.Println()
		_ = c.Help()
	},
}

// Execute runs the command tree.
func Execute() error {
	// Rendered lazily so every flag is registered: `drariux help` then shows the
	// exact syntax of every command instead of only their names.
	rootCmd.Long = rootLong + "\n" + syntaxSheet(rootCmd)
	return rootCmd.Execute()
}

// syntaxSheet walks the command tree and prints one line per runnable command.
func syntaxSheet(root *cobra.Command) string {
	var lines []string
	flagSheet := func(c *cobra.Command) string {
		var parts []string
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			s := "--" + f.Name
			switch f.Value.Type() {
			case "string":
				s += " <" + strings.ToUpper(f.Name) + ">"
			case "float64", "int64", "uint64", "int":
				s += " <n>"
			}
			parts = append(parts, s)
		})
		return strings.Join(parts, " ")
	}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		if c.Deprecated != "" || !c.IsAvailableCommand() {
			return
		}
		sub := path
		if c != root {
			sub = path + " " + c.Name()
		}
		if c.Runnable() && c.Parent() != nil {
			line := "  " + sub
			if u := strings.TrimSpace(strings.TrimPrefix(c.Use, c.Name())); u != "" {
				line += " " + u
			}
			if f := flagSheet(c); f != "" {
				line += "  [" + f + "]"
			}
			if len(c.Aliases) > 0 {
				line += "   · alias: " + strings.Join(c.Aliases, ", ")
			}
			lines = append(lines, line)
		}
		for _, s := range c.Commands() {
			walk(s, sub)
		}
	}
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		walk(c, root.Name())
	}
	var b strings.Builder
	b.WriteString("\nCOMMANDS · syntax\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n  drariux <command> --help   # full flag details\n")
	return b.String()
}

func init() {
	rootCmd.AddCommand(walletCmd, rpcCmd, chainCmd, txCmd, contractCmd, nodeCmd, versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the DRARIUX wordmark and CLI version",
	Run: func(c *cobra.Command, args []string) {
		PrintBanner()
		fmt.Printf("drariux %s\n", Version)
	},
}

// ---- shared helpers used across subcommands ----

// mustConfig loads config or exits.
func mustConfig() config.Config {
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	return c
}

// clientFor builds an RPC client for the configured endpoint.
func clientFor(cfg config.Config) *rpc.Client {
	return rpc.New(cfg.RPC)
}

// ctx returns a background context with a sane deadline.
func ctx() context.Context {
	c, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	_ = cancel // freed at process exit for a short-lived CLI
	return c
}

// resolveAccount picks an address from --from flag, config default, or errors.
func resolveAccount(cfg config.Config, flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if cfg.Account != "" {
		return cfg.Account
	}
	return ""
}

// parseAmount converts a human decimal amount (e.g. "1.5") into base units
// (18 decimals) as a big.Int decimal string, or returns the input if it is
// already a plain integer of wei.
func parseAmount(s string) (string, error) {
	if s == "" {
		return "0x0", nil
	}
	if strings.Contains(s, ".") {
		f, ok := new(big.Float).SetString(s)
		if !ok {
			return "", fmt.Errorf("invalid amount %q", s)
		}
		f.Mul(f, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
		i, _ := f.Int(nil)
		return "0x" + i.Text(16), nil
	}
	i, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return "", fmt.Errorf("invalid amount %q", s)
	}
	return "0x" + i.Text(16), nil
}

// fmtDRX renders a wei big.Int as a human DRX string (18 decimals).
func fmtDRX(wei *big.Int) string {
	if wei == nil {
		return "0 DRX"
	}
	div, mod := new(big.Int), new(big.Int)
	div.QuoRem(wei, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), mod)
	if mod.Sign() == 0 {
		return div.String() + " DRX"
	}
	frac := fmt.Sprintf("%018s", mod.String())
	frac = strings.TrimRight(frac, "0")
	return div.String() + "." + frac + " DRX"
}

// normalizeHexData ensures a 0x-prefixed even-length hex payload.
func normalizeHexData(s string) (string, error) {
	if s == "" {
		return "0x", nil
	}
	s = strings.TrimPrefix(s, "0x")
	if _, err := hex.DecodeString(s); err != nil {
		return "", fmt.Errorf("invalid hex data: %w", err)
	}
	if len(s)%2 != 0 {
		s = "0" + s
	}
	return "0x" + s, nil
}
