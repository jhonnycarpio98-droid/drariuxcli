package keyring

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func nowRFC() string { return time.Now().UTC().Format(time.RFC3339) }

// Minimal BIP-0173 bech32 encoder (no external dependency).

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// bech32CharsetIdx maps a character back to its 5-bit value.
var bech32CharsetIdx = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(bech32Charset); i++ {
		t[bech32Charset[i]] = int8(i)
	}
	return t
}()

func bech32VerifyChecksum(hrp string, data []byte) bool {
	values := append(bech32HrpExpand(hrp), data...)
	return bech32Polymod(values)^1 == 0
}

func bech32Polymod(values []byte) uint32 {
	gen := []uint32{0x3b6a57b2, 0x18c1ea30, 0x237d3f46, 0x0e4a4c72, 0x3f8640b0}
	chk := uint32(1)
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (b>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HrpExpand(hrp string) []byte {
	out := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

func bech32CreateChecksum(hrp string, data []byte) []byte {
	values := append(bech32HrpExpand(hrp), data...)
	values = append(values, 0, 0, 0, 0, 0, 0)
	mod := bech32Polymod(values) ^ 1
	out := make([]byte, 6)
	for i := 0; i < 6; i++ {
		out[i] = byte((mod >> uint(5*(5-i))) & 31)
	}
	return out
}

// convertBits regroups from-bit groups into to-bit groups.
func convertBits(data []byte, from, to uint32, pad bool) ([]byte, error) {
	var acc uint32
	var bits uint32
	var out []byte
	maxv := uint32(1)<<to - 1
	for _, v := range data {
		acc = acc<<from | uint32(v)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte(acc>>bits&maxv))
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte(acc<<(to-bits)&maxv))
		}
	} else if bits >= from || (bits > 0 && acc&((uint32(1)<<bits)-1) != 0) {
		return nil, fmt.Errorf("invalid data padding")
	}
	return out, nil
}

// Bech32Decode splits a bech32 string into its hrp and 8-bit payload.
func Bech32Decode(s string) (string, []byte, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	i := strings.LastIndex(s, "1")
	if i < 1 || i+7 > len(s) {
		return "", nil, fmt.Errorf("invalid bech32 string %q", s)
	}
	hrp, part := s[:i], s[i+1:]
	data := make([]byte, 0, len(part))
	for j := 0; j < len(part); j++ {
		v := bech32CharsetIdx[part[j]]
		if v < 0 {
			return "", nil, fmt.Errorf("invalid bech32 character %q", string(part[j]))
		}
		data = append(data, byte(v))
	}
	if !bech32VerifyChecksum(hrp, data) {
		return "", nil, fmt.Errorf("bech32 checksum mismatch for %q", s)
	}
	conv, err := convertBits(data[:len(data)-6], 5, 8, false)
	if err != nil {
		return "", nil, err
	}
	return hrp, conv, nil
}

// Bech32ToEVM maps any Drariux bech32 account address (drariux1…,
// drariuxvaloper1…, drariuxvalcons1…) to its 0x EVM representation, since on
// this chain the 20-byte payload is shared across all three forms.
func Bech32ToEVM(addr string) (string, error) {
	_, payload, err := Bech32Decode(addr)
	if err != nil {
		return "", err
	}
	if len(payload) != 20 {
		return "", fmt.Errorf("expected a 20-byte address, got %d bytes", len(payload))
	}
	return toChecksum(hex.EncodeToString(payload)), nil
}

// Bech32Encode builds a bech32 string from an 8-bit payload.
func Bech32Encode(hrp string, payload []byte) (string, error) {
	conv, err := convertBits(payload, 8, 5, true)
	if err != nil {
		return "", err
	}
	checksum := bech32CreateChecksum(strings.ToLower(hrp), conv)
	combined := append(conv, checksum...)
	var b strings.Builder
	b.WriteString(hrp)
	b.WriteByte('1')
	for _, v := range combined {
		if int(v) >= len(bech32Charset) {
			return "", fmt.Errorf("invalid data byte %d", v)
		}
		b.WriteByte(bech32Charset[v])
	}
	return b.String(), nil
}
