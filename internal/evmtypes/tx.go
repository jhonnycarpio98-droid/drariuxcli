package evmtypes

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Tx is a legacy (pre-EIP-2718) transaction.
type Tx struct {
	Nonce    uint64
	GasPrice *big.Int
	GasLimit uint64
	To       string // empty = contract creation
	Value    *big.Int
	Data     []byte
	ChainID  *big.Int
}

func (t *Tx) signingHash() []byte {
	fields := []rlpItem{
		uintItem(t.Nonce),
		bigItem(t.GasPrice),
		uintItem(t.GasLimit),
		addrItem(t.To),
		bigItem(orZero(t.Value)),
		bytesItem(t.Data),
		uintItem(uint64(t.ChainID.Int64())),
		uintItem(0),
		uintItem(0),
	}
	h := sha3.NewLegacyKeccak256()
	h.Write(encodeList(fields))
	return h.Sum(nil)
}

// Raw returns the EIP-155 signed transaction as 0x-hex.
func (t *Tx) Raw(privHex string) (string, error) {
	privHex = strings.TrimPrefix(privHex, "0x")
	pb, err := hex.DecodeString(privHex)
	if err != nil || len(pb) != 32 {
		return "", fmt.Errorf("invalid private key")
	}
	key := secp256k1.PrivKeyFromBytes(pb)
	sig := ecdsa.SignCompact(key, t.signingHash(), false) // [v(1), r(32), s(32)]
	if len(sig) != 65 {
		return "", fmt.Errorf("unexpected signature length %d", len(sig))
	}
	recoveryID := sig[0] - 27
	r := new(big.Int).SetBytes(sig[1:33])
	s := new(big.Int).SetBytes(sig[33:65])

	v := new(big.Int).Add(
		new(big.Int).Mul(t.ChainID, big.NewInt(2)),
		big.NewInt(int64(35+int(recoveryID))),
	)

	fields := []rlpItem{
		uintItem(t.Nonce),
		bigItem(t.GasPrice),
		uintItem(t.GasLimit),
		addrItem(t.To),
		bigItem(orZero(t.Value)),
		bytesItem(t.Data),
		bigItem(v),
		bigItem(r),
		bigItem(s),
	}
	return "0x" + hex.EncodeToString(encodeList(fields)), nil
}

// TxHash is keccak256 of the raw signed transaction.
func TxHash(rawHex string) (string, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(rawHex, "0x"))
	if err != nil {
		return "", err
	}
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	return "0x" + hex.EncodeToString(h.Sum(nil)), nil
}

func orZero(x *big.Int) *big.Int {
	if x == nil {
		return new(big.Int)
	}
	return x
}

// ---- minimal RLP ----

type rlpItem struct {
	bytes  []byte
	isList bool
	list   []rlpItem
}

func uintItem(n uint64) rlpItem { return bigItem(new(big.Int).SetUint64(n)) }

func bigItem(x *big.Int) rlpItem {
	if x == nil || x.Sign() == 0 {
		return rlpItem{bytes: nil}
	}
	return rlpItem{bytes: trimLeadingZero(x.Bytes())}
}

func addrItem(addr string) rlpItem {
	if addr == "" {
		return rlpItem{bytes: nil}
	}
	a, _ := hex.DecodeString(strings.TrimPrefix(addr, "0x"))
	return rlpItem{bytes: a}
}

func bytesItem(b []byte) rlpItem { return rlpItem{bytes: b} }

func encodeList(items []rlpItem) []byte {
	var payload []byte
	for _, it := range items {
		if it.isList {
			payload = append(payload, encodeList(it.list)...)
		} else {
			payload = append(payload, encodeString(it.bytes)...)
		}
	}
	return append(encodeLength(len(payload), 0xc0), payload...)
}

func encodeString(b []byte) []byte {
	if len(b) == 1 && b[0] <= 0x7f {
		return []byte{b[0]}
	}
	return append(encodeLength(len(b), 0x80), b...)
}

func encodeLength(length int, offset byte) []byte {
	if length <= 55 {
		return []byte{offset + byte(length)}
	}
	var lenBytes []byte
	l := length
	for l > 0 {
		lenBytes = append([]byte{byte(l & 0xff)}, lenBytes...)
		l >>= 8
	}
	return append([]byte{offset + 55 + byte(len(lenBytes))}, lenBytes...)
}

func trimLeadingZero(b []byte) []byte {
	i := 0
	for i < len(b) && b[i] == 0 {
		i++
	}
	return b[i:]
}
