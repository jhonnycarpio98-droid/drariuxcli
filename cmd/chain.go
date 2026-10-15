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
		fmt.Printf("endpoint : %s\n", cfg.RPC)

		id, err := client.ChainID(ctx())
		if err != nil {
			return fmt.Errorf("cannot reach node: %w", err)
		}
		fmt.Printf("chain id : %s\n", id.String())

		height, err := client.BlockNumber(ctx())
		if err != nil {
			return err
		}
		fmt.Printf("height   : %d\n", height)

		gp, err := client.GasPrice(ctx())
		if err != nil {
			return err
		}
		fmt.Printf("gas price: %s wei (%s)\n", gp.String(), fmtDRX(gp))
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
