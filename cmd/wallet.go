package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/drariux/drariux-cli/internal/config"
	"github.com/drariux/drariux-cli/internal/keyring"
)

var walletCmd = &cobra.Command{
	Use:     "wallet",
	Aliases: []string{"w", "account", "ac"},
	Short:   "Manage local wallets",
	Long:    "Create, list, inspect and fund-check local Drariux wallets (secp256k1 keypairs).",
}

var walletNewName string

var walletNewCmd = &cobra.Command{
	Use:     "new",
	Aliases: []string{"create", "add"},
	Short:   "Create a new wallet (starts with zero tokens)",
	Example: `  drariux wallet new
  drariux wallet new --name alice`,
	RunE: func(c *cobra.Command, args []string) error {
		name := walletNewName
		if name == "" && len(args) > 0 {
			name = args[0]
		}
		if name == "" {
			name = "default"
		}
		w, err := keyring.New(name)
		if err != nil {
			return err
		}
		if err := w.Save(); err != nil {
			return err
		}
		cfg := mustConfig()
		// First wallet becomes the default account automatically.
		if cfg.Account == "" {
			cfg.Account = w.Address
			_ = config.Save(cfg)
		}
		box("Wallet created · 0 DRX (unfunded)", [][2]string{
			{"name", w.Name},
			{"evm", w.Address},
			{"cosmos", w.CosmosAddr},
		})
		fmt.Println()
		fmt.Println("  Stored at ~/.drariux/keystore/ — fund it to start transacting.")
		return nil
	},
}

var walletListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List local wallets",
	RunE: func(c *cobra.Command, args []string) error {
		all, err := keyring.List()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Println("no wallets. create one: drariux wallet new")
			return nil
		}
		cfg := mustConfig()
		header := []string{"NAME", "EVM ADDRESS", "COSMOS ADDRESS", "DEFAULT"}
		var data [][]string
		for _, w := range all {
			def := ""
			if w.Address == cfg.Account {
				def = "*"
			}
			data = append(data, []string{w.Name, w.Address, w.CosmosAddr, def})
		}
		boxTable("Wallets", header, data)
		return nil
	},
}

var walletShowJSON bool

var walletShowCmd = &cobra.Command{
	Use:   "show [name|address]",
	Short: "Show a wallet's details (public info only)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		q := ""
		if len(args) > 0 {
			q = args[0]
		} else {
			q = cfg.Account
		}
		if q == "" {
			return fmt.Errorf("no wallet specified and no default set")
		}
		w, err := keyring.Find(q)
		if err != nil {
			return err
		}
		if walletShowJSON {
			b, _ := json.MarshalIndent(struct {
				Name, EVM, Cosmos, Created string
			}{w.Name, w.Address, w.CosmosAddr, w.Created}, "", "  ")
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("name   : %s\n", w.Name)
		fmt.Printf("evm    : %s\n", w.Address)
		fmt.Printf("cosmos : %s\n", w.CosmosAddr)
		fmt.Printf("created: %s\n", w.Created)
		return nil
	},
}

var walletBalanceCmd = &cobra.Command{
	Use:     "balance [name|address]",
	Aliases: []string{"bal"},
	Short:   "Query a wallet's on-chain balance via RPC",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		cfg := mustConfig()
		q := cfg.Account
		if len(args) > 0 {
			q = args[0]
		}
		addr := q
		if w, err := keyring.Find(q); err == nil {
			addr = w.Address
		}
		if addr == "" {
			return fmt.Errorf("no address given and no default wallet")
		}
		client := clientFor(cfg)
		bal, err := client.Balance(ctx(), addr)
		if err != nil {
			return fmt.Errorf("balance query failed: %w", err)
		}
		nonce, _ := client.Nonce(ctx(), addr)
		box("Balance", [][2]string{
			{"rpc", cfg.RPC},
			{"address", addr},
			{"balance", fmtDRX(bal)},
			{"nonce", fmt.Sprintf("%d", nonce)},
		})
		return nil
	},
}

var walletSetDefault string

var walletUseCmd = &cobra.Command{
	Use:     "use [name|address]",
	Aliases: []string{"set-default"},
	Short:   "Set the default account used by tx/contract commands",
	Args:    cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		w, err := keyring.Find(args[0])
		if err != nil {
			return err
		}
		cfg := mustConfig()
		cfg.Account = w.Address
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("✓ default account set to %s (%s)\n", w.Name, w.Address)
		return nil
	},
}

var walletImportPriv string

var walletImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a wallet from a 32-byte hex private key",
	RunE: func(c *cobra.Command, args []string) error {
		if walletImportPriv == "" {
			return fmt.Errorf("provide --key <hex>")
		}
		cfg := mustConfig()
		name := walletNewName
		if name == "" {
			name = "imported"
		}
		w, err := keyring.Import(name, walletImportPriv, cfg.Prefix)
		if err != nil {
			return err
		}
		if err := w.Save(); err != nil {
			return err
		}
		fmt.Printf("✓ imported %s\n  evm   : %s\n  cosmos: %s\n", w.Name, w.Address, w.CosmosAddr)
		return nil
	},
}

func init() {
	walletNewCmd.Flags().StringVar(&walletNewName, "name", "", "wallet label")
	walletShowCmd.Flags().BoolVar(&walletShowJSON, "json", false, "output JSON")
	walletUseCmd.Flags().StringVar(&walletSetDefault, "as", "", "")
	_ = walletUseCmd.Flags().MarkHidden("as")
	walletImportCmd.Flags().StringVar(&walletImportPriv, "key", "", "32-byte hex private key")
	walletImportCmd.Flags().StringVar(&walletNewName, "name", "", "wallet label")
	walletCmd.AddCommand(walletNewCmd, walletListCmd, walletShowCmd, walletBalanceCmd, walletUseCmd, walletImportCmd)
}
