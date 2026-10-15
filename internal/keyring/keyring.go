package keyring

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck
	"golang.org/x/crypto/sha3"
)

// Wallet is a secp256k1 account. One key yields both address forms the chain
// understands: an EVM hex address and a Cosmos bech32 address.
type Wallet struct {
	Name       string `json:"name"`
	PrivHex    string `json:"priv"`
	Address    string `json:"address"`
	CosmosAddr string `json:"cosmos"`
	Created    string `json:"created"`
}

func New(name string) (*Wallet, error) {
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	return fromKey(key, name)
}

func Import(name, privHex string, prefix string) (*Wallet, error) {
	privHex = strings.TrimPrefix(privHex, "0x")
	b, err := hex.DecodeString(privHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(b))
	}
	key := secp256k1.PrivKeyFromBytes(b)
	w, err := fromKey(key, name)
	if err != nil {
		return nil, err
	}
	if prefix != "" {
		w.CosmosAddr = cosmosAddr(key, prefix)
	}
	return w, nil
}

// RandSeedPhrase returns a random 12-word string for the user to note down. It
// is display only; the recoverable secret is the key file in the keystore.
func RandSeedPhrase() string {
	const words = "alpha bravo charlie delta echo foxtrot golf hotel india juliett kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu"
	list := strings.Fields(words)
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	var out []string
	for i := 0; i < 12; i++ {
		out = append(out, list[int(b[i])%len(list)])
	}
	return strings.Join(out, " ")
}

func fromKey(key *secp256k1.PrivateKey, name string) (*Wallet, error) {
	uncompressed := key.PubKey().SerializeUncompressed()
	h := sha3.NewLegacyKeccak256()
	h.Write(uncompressed[1:])
	evm := h.Sum(nil)[12:]

	return &Wallet{
		Name:       name,
		PrivHex:    hex.EncodeToString(key.Serialize()),
		Address:    toChecksum(hex.EncodeToString(evm)),
		CosmosAddr: cosmosAddr(key, "drariux"),
		Created:    nowRFC(),
	}, nil
}

func cosmosAddr(key *secp256k1.PrivateKey, prefix string) string {
	raw := sha256.Sum256(key.PubKey().SerializeCompressed())
	r := ripemd160.New()
	r.Write(raw[:])
	addr20 := r.Sum(nil)
	s, err := Bech32Encode(prefix, addr20)
	if err != nil {
		return ""
	}
	return s
}

func save(w *Wallet) error {
	dir, err := keystoreDir()
	if err != nil {
		return err
	}
	p := filepath.Join(dir, strings.TrimPrefix(strings.ToLower(w.Address), "0x")+".json")
	b, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

func (w *Wallet) Save() error { return save(w) }

func List() ([]*Wallet, error) {
	dir, err := keystoreDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*Wallet
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var w Wallet
		if json.Unmarshal(b, &w) == nil && w.Address != "" {
			out = append(out, &w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out, nil
}

// Find locates a wallet by name or either address form.
func Find(query string) (*Wallet, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	for _, w := range all {
		if strings.ToLower(w.Name) == q ||
			strings.ToLower(w.Address) == q ||
			strings.ToLower(w.CosmosAddr) == strings.ToLower(query) {
			return w, nil
		}
	}
	return nil, fmt.Errorf("wallet %q not found in keystore", query)
}

func keystoreDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(h, ".drariux", "keystore")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func toChecksum(addrHex string) string {
	lower := strings.ToLower(addrHex)
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(lower))
	sum := h.Sum(nil)
	out := []byte("0x" + lower)
	for i := 2; i < len(out); i++ {
		c := out[i]
		if c >= 'a' && c <= 'f' {
			nibble := sum[(i-2)/2]
			var v byte
			if (i-2)%2 == 0 {
				v = nibble >> 4
			} else {
				v = nibble & 0x0f
			}
			if v >= 8 {
				out[i] = c - 32
			}
		}
	}
	return string(out)
}
