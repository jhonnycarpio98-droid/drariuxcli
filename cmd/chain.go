package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var chainCmd = &cobra.Command{
	Use:     "chain",
	Aliases: []string{"c", "network", "net"},
	Short:   "Inspect the connected chain",
}

var chainInfoCmd = &cobra.Command{
	Use:     "info",
	Aliases: []string{"status", "st"},
	Short:   "Show endpoint, chain id, latest height and gas price",
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		client := clientFor(cfg)

		id, err := client.ChainID(ctx())
		if err != nil {
			return fmt.Errorf("cannot reach node: %w", err)
		}
		height, err := client.BlockNumber(ctx())
		if err != nil {
			return err
		}
		gp, err := client.GasPrice(ctx())
		if err != nil {
			return err
		}
		box("Chain", [][2]string{
			{"endpoint", cfg.RPC},
			{"chain id", id.String()},
			{"height", fmt.Sprintf("%d", height)},
			{"gas price", fmt.Sprintf("%s wei (%s)", gp.String(), fmtDRX(gp))},
		})
		return nil
	},
}

var chainHeightCmd = &cobra.Command{
	Use:     "height",
	Aliases: []string{"h"},
	Short:   "Print only the latest block number (script-friendly)",
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		client := clientFor(cfg)
		height, err := client.BlockNumber(ctx())
		if err != nil {
			return err
		}
		fmt.Println(height)
		return nil
	},
}

func init() {
	chainCmd.AddCommand(chainInfoCmd, chainHeightCmd)
}
