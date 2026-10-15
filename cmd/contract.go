package cmd

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drariux/drariux-cli/internal/evmtypes"
	"github.com/drariux/drariux-cli/internal/keyring"
	"github.com/drariux/drariux-cli/internal/rpc"
)

var contractCmd = &cobra.Command{
	Use:     "contract",
	Aliases: []string{"ct", "crt"},
	Short:   "Deploy, call and inspect smart contracts",
	Long: `Developer helpers for the Drariux EVM:

  publish   deploy bytecode (runs a dry-run simulation first)
  call      eth_call a read function (free, no state change)
  code      show deployed bytecode at an address
  decode    turn raw revert data into a readable reason`,
}

var (
	cFrom         string
	cBytecode     string
	cBytecodeFile string
	cArgs         string
	cDryRun       bool
	cYes          bool
	cGas          uint64
)

// deployData returns the creation code plus optional constructor args (hex).
func deployData() (string, error) {
	code := cBytecode
	if cBytecodeFile != "" {
		b, err := os.ReadFile(cBytecodeFile)
		if err != nil {
			return "", err
		}
		code = strings.TrimSpace(string(b))
	}
	if code == "" {
		return "", fmt.Errorf("provide --bytecode 0x… or --bytecode-file ./contract.bin")
	}
	code = strings.TrimPrefix(code, "0x")
	if _, err := hex.DecodeString(code); err != nil {
		return "", fmt.Errorf("invalid bytecode hex: %w", err)
	}
	if cArgs != "" {
		args := strings.TrimPrefix(cArgs, "0x")
		if _, err := hex.DecodeString(args); err != nil {
			return "", fmt.Errorf("invalid constructor args hex: %w", err)
		}
		code += args
	}
	return "0x" + code, nil
}

var contractPublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Deploy a contract (simulates first; add --yes to broadcast)",
	Example: `  drariux contract publish --bytecode-file ./Hello.bin --dry-run
  drariux contract publish --bytecode 0x6080... --from alice --yes`,
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		data, err := deployData()
		if err != nil {
			return err
		}
		from := resolveAccount(cfg, cFrom)
		p := rpc.CallParams{Data: data}
		if from != "" {
			p.From = from
		}

		// With To empty, estimateGas/eth_call run the constructor read-only: this
		// surfaces reverts and the deployment gas cost before broadcasting.
		res, err := simulate(cfg, p)
		if err != nil {
			return err
		}
		printSim("contract deploy", res)
		if res.Reverted {
			return fmt.Errorf("deployment simulation reverted (nothing published)")
		}
		if cDryRun {
			fmt.Println("\n✓ --dry-run: deployment validated, nothing broadcast.")
			return nil
		}
		if !cYes {
			return fmt.Errorf("refusing to broadcast without --yes (or use --dry-run)")
		}
		if from == "" {
			return fmt.Errorf("--from required to sign the deploy tx")
		}
		w, err := keyring.Find(from)
		if err != nil {
			return err
		}
		client := clientFor(cfg)
		nonce, err := client.Nonce(ctx(), w.Address)
		if err != nil {
			return err
		}
		gasPrice, err := client.GasPrice(ctx())
		if err != nil {
			return err
		}
		gasLimit := res.GasEstimate
		if cGas > 0 {
			gasLimit = cGas
		} else {
			gasLimit = gasLimit + gasLimit/10
		}
		rawData, _ := hex.DecodeString(strings.TrimPrefix(data, "0x"))
		tx := &evmtypes.Tx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			GasLimit: gasLimit,
			To:       "", // creation
			Value:    new(big.Int),
			Data:     rawData,
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
		fmt.Printf("\n✓ deploy tx: %s\n", hash)
		fmt.Println("  check the contract address once the block is mined: drariux chain height")
		return nil
	},
}

var (
	callTo   string
	callData string
)

var contractCallCmd = &cobra.Command{
	Use:     "call [address]",
	Short:   "Read a contract function via eth_call (free, no state change)",
	Args:    cobra.ExactArgs(1),
	Example: `  drariux contract call 0xAbC... --data 0x18160ddd   # totalSupply()`,
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		d, err := normalizeHexData(callData)
		if err != nil {
			return err
		}
		client := clientFor(cfg)
		out, err := client.EthCall(ctx(), rpc.CallParams{To: args[0], Data: d})
		if err != nil {
			if rev, ok := err.(*rpc.RevertError); ok {
				return fmt.Errorf("call reverted: %s", decodeRevert(rev.Message, rev.Data))
			}
			return err
		}
		fmt.Printf("return: %s\n", out)
		return nil
	},
}

var contractCodeCmd = &cobra.Command{
	Use:   "code [address]",
	Short: "Show deployed bytecode at an address",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		client := clientFor(cfg)
		code, err := client.Code(ctx(), args[0])
		if err != nil {
			return err
		}
		if code == "0x" || code == "" {
			fmt.Println("(no code at this address — EOA or not yet deployed)")
			return nil
		}
		fmt.Printf("code size: %d bytes\n", (len(code)-2)/2)
		fmt.Println(code)
		return nil
	},
}

var contractDecodeCmd = &cobra.Command{
	Use:     "decode [hex-data]",
	Short:   "Decode a revert/panic reason from raw hex",
	Args:    cobra.ExactArgs(1),
	Example: `  drariux contract decode 0x08c379a00000...`,
	RunE: func(c *cobra.Command, args []string) error {
		fmt.Println(decodeRevert("", args[0]))
		return nil
	},
}

func init() {
	contractPublishCmd.Flags().StringVar(&cFrom, "from", "", "deployer wallet/address")
	contractPublishCmd.Flags().StringVar(&cBytecode, "bytecode", "", "creation code hex (0x…)")
	contractPublishCmd.Flags().StringVar(&cBytecodeFile, "bytecode-file", "", "read bytecode from file")
	contractPublishCmd.Flags().StringVar(&cArgs, "args", "", "ABI-encoded constructor args hex")
	contractPublishCmd.Flags().BoolVar(&cDryRun, "dry-run", false, "simulate only, never broadcast")
	contractPublishCmd.Flags().BoolVar(&cYes, "yes", false, "confirm broadcast")
	contractPublishCmd.Flags().Uint64Var(&cGas, "gas", 0, "override gas limit")

	contractCallCmd.Flags().StringVar(&callData, "data", "", "function selector + args hex")

	contractCmd.AddCommand(contractPublishCmd, contractCallCmd, contractCodeCmd, contractDecodeCmd)
}
