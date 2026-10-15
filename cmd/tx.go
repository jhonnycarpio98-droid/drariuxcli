package cmd

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drariux/drariux-cli/internal/evmtypes"
	"github.com/drariux/drariux-cli/internal/keyring"
	"github.com/drariux/drariux-cli/internal/rpc"
)

var txCmd = &cobra.Command{
	Use:     "tx",
	Aliases: []string{"t"},
	Short:   "Send or simulate value transfers / calldata",
}

// shared flags across tx subcommands
var (
	txFrom   string
	txTo     string
	txValue  string
	txData   string
	txGas    uint64
	txDryRun bool
	txYes    bool
)

func buildCallParams(from, to, value, data string) (rpc.CallParams, error) {
	p := rpc.CallParams{}
	if from != "" {
		p.From = from
	}
	if to != "" {
		p.To = to
	}
	if value != "" {
		v, err := parseAmount(value)
		if err != nil {
			return p, err
		}
		p.Value = v
	}
	if data != "" {
		d, err := normalizeHexData(data)
		if err != nil {
			return p, err
		}
		p.Data = d
	}
	return p, nil
}

var txDryRunCmd = &cobra.Command{
	Use:     "dryrun",
	Aliases: []string{"simulate", "sim", "estimate"},
	Short:   "Simulate a tx without touching the chain (gas + fee + revert reason)",
	Example: `  drariux tx dryrun --to 0xAbC... --value 1.5
  drariux tx dryrun --from 0x funded addr --to 0xDef --data 0x...`,
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		from := resolveAccount(cfg, txFrom)
		p, err := buildCallParams(from, txTo, txValue, txData)
		if err != nil {
			return err
		}
		if p.To == "" && p.Data == "" {
			return fmt.Errorf("provide --to and/or --data")
		}
		res, err := simulate(cfg, p)
		if err != nil {
			return err
		}
		printSim("transfer/call", res)
		return nil
	},
}

var txSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Sign and broadcast a transfer (use --dry-run to test first)",
	Example: `  drariux tx send --to 0xAbC... --value 1 --dry-run
  drariux tx send --from alice --to 0xAbC... --value 2`,
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		from := resolveAccount(cfg, txFrom)
		if from == "" {
			return fmt.Errorf("no --from and no default account (drariux wallet use <name>)")
		}
		if txTo == "" {
			return fmt.Errorf("--to is required")
		}
		w, err := keyring.Find(from)
		if err != nil {
			return fmt.Errorf("sender %q must exist in keystore to sign: %w", from, err)
		}
		client := clientFor(cfg)

		valueWei, err := parseAmountBig(txValue)
		if err != nil {
			return err
		}
		var dataBytes []byte
		if txData != "" {
			d, err := normalizeHexData(txData)
			if err != nil {
				return err
			}
			dataBytes, _ = decodeHex(d)
		}

		p := rpc.CallParams{From: w.Address, To: txTo, Value: "0x" + valueWei.Text(16), Data: txData}

		// Dry-run first; only broadcast on explicit --yes.
		res, err := simulate(cfg, p)
		if err != nil {
			return err
		}
		printSim("transfer/call", res)
		if res.Reverted {
			return fmt.Errorf("aborted: simulation reverted (no tx sent)")
		}
		if txDryRun {
			fmt.Println("\n✓ --dry-run: nothing broadcast.")
			return nil
		}
		if !txYes {
			return fmt.Errorf("refusing to broadcast without --yes (re-run with --yes to confirm)")
		}

		nonce, err := client.Nonce(ctx(), w.Address)
		if err != nil {
			return err
		}
		gasPrice, err := client.GasPrice(ctx())
		if err != nil {
			return err
		}
		gasLimit := res.GasEstimate
		if txGas > 0 {
			gasLimit = txGas
		} else if gasLimit > 0 {
			gasLimit = gasLimit + gasLimit/10 // headroom
		}
		tx := &evmtypes.Tx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			GasLimit: gasLimit,
			To:       txTo,
			Value:    valueWei,
			Data:     dataBytes,
			ChainID:  new(big.Int).SetInt64(int64(cfg.ChainID)),
		}
		raw, err := tx.Raw(w.PrivHex)
		if err != nil {
			return fmt.Errorf("signing failed: %w", err)
		}
		hash, err := client.SendRawTransaction(ctx(), raw)
		if err != nil {
			return fmt.Errorf("broadcast failed: %w", err)
		}
		fmt.Printf("\n✓ broadcast tx: %s\n", hash)
		return nil
	},
}

func parseAmountBig(s string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	if strings.Contains(s, ".") {
		f, ok := new(big.Float).SetString(s)
		if !ok {
			return nil, fmt.Errorf("invalid amount %q", s)
		}
		f.Mul(f, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
		i, _ := f.Int(nil)
		return i, nil
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount %q", s)
	}
	return n, nil
}

func decodeHex(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

func init() {
	for _, c := range []*cobra.Command{txDryRunCmd, txSendCmd} {
		c.Flags().StringVar(&txFrom, "from", "", "sender address or wallet name")
		c.Flags().StringVar(&txTo, "to", "", "recipient address")
		c.Flags().StringVar(&txValue, "value", "0", "amount in DRX (decimal allowed)")
		c.Flags().StringVar(&txData, "data", "", "hex calldata")
	}
	txSendCmd.Flags().BoolVar(&txDryRun, "dry-run", false, "simulate only, never broadcast")
	txSendCmd.Flags().BoolVar(&txYes, "yes", false, "confirm broadcast")
	txSendCmd.Flags().Uint64Var(&txGas, "gas", 0, "override gas limit")
	txCmd.AddCommand(txDryRunCmd, txSendCmd)
}
