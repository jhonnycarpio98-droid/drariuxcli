package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Chain defaults for the live Drariux network.
const (
	DefaultRPC      = "https://node.drariux.network"
	DefaultChainID  = 9854
	DefaultDenom    = "draxiux"
	DefaultBech32   = "drariux"
	DefaultGasPrice = "2000000000"
)

// Config is the on-disk settings file at ~/.drariux/config.json.
type Config struct {
	RPC      string `json:"rpc"`
	ChainID  int    `json:"chain_id"`
	Denom    string `json:"denom"`
	Prefix   string `json:"bech32_prefix"`
	GasPrice string `json:"gas_price"`
	Account  string `json:"default_account,omitempty"`
}

func Home() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(h, ".drariux")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func KeystoreDir() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(h, "keystore")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func configPath() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "config.json"), nil
}

func Default() Config {
	return Config{
		RPC:      DefaultRPC,
		ChainID:  DefaultChainID,
		Denom:    DefaultDenom,
		Prefix:   DefaultBech32,
		GasPrice: DefaultGasPrice,
	}
}

// Load reads config.json, writing defaults when it does not exist yet.
func Load() (Config, error) {
	p, err := configPath()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			c := Default()
			_ = save(p, c)
			return c, nil
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if c.RPC == "" {
		c.RPC = DefaultRPC
	}
	if c.ChainID == 0 {
		c.ChainID = DefaultChainID
	}
	if c.Denom == "" {
		c.Denom = DefaultDenom
	}
	if c.Prefix == "" {
		c.Prefix = DefaultBech32
	}
	if c.GasPrice == "" {
		c.GasPrice = DefaultGasPrice
	}
	return c, nil
}

func Save(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	return save(p, c)
}

func save(p string, c Config) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}
