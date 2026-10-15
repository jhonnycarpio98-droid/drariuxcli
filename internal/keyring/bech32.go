package keyring

import (
	"fmt"
	"strings"
	"time"
)

func nowRFC() string { return time.Now().UTC().Format(time.RFC3339) }

// Minimal BIP-0173 bech32 encoder (no external dependency).

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

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
