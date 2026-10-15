package cmd

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/drariux/drariux-cli/internal/config"
	"github.com/drariux/drariux-cli/internal/rpc"
)

// Common EVM revert/panic ABI selectors.
const (
	selError = "08c379a0" // Error(string)
	selPanic = "4e487b71" // Panic(uint256)
)

// SimResult is the outcome of a dry-run simulation.
type SimResult struct {
	GasEstimate uint64
	GasPrice    *big.Int
	FeeWei      *big.Int
	FeeDRX      string
	Reverted    bool
	Reason      string
	ReturnData  string
	Notes       []string
}

// simulate runs a read-only dry-run: estimateGas + eth_call plus revert decoding.
// It changes no state and needs no private key (from is only an address).
func simulate(cfg config.Config, p rpc.CallParams) (*SimResult, error) {
	client := clientFor(cfg)
	res := &SimResult{}

	gp, err := client.GasPrice(ctx())
	if err != nil {
		gp = new(big.Int)
		if cfg.GasPrice != "" {
			gp, _ = gp.SetString(cfg.GasPrice, 10)
		}
	}
	res.GasPrice = gp

	gas, err := client.EstimateGas(ctx(), p)
	if err != nil {
		if rev, ok := err.(*rpc.RevertError); ok {
			res.Reverted = true
			res.Reason = decodeRevert(rev.Message, rev.Data)
			res.Notes = append(res.Notes, "estimateGas reverted")
		} else {
			return res, fmt.Errorf("estimateGas failed: %w", err)
		}
	}
	res.GasEstimate = gas

	if !res.Reverted {
		out, err := client.EthCall(ctx(), p)
		if err != nil {
			if rev, ok := err.(*rpc.RevertError); ok {
				res.Reverted = true
				res.Reason = decodeRevert(rev.Message, rev.Data)
			} else {
				res.Notes = append(res.Notes, "eth_call error: "+err.Error())
			}
		} else {
			res.ReturnData = out
		}
	}

	if gas > 0 && gp.Sign() > 0 {
		res.FeeWei = new(big.Int).Mul(new(big.Int).SetUint64(gas), gp)
		res.FeeDRX = fmtDRX(res.FeeWei)
	}

	if p.From != "" {
		if bal, err := client.Balance(ctx(), p.From); err == nil {
			need := new(big.Int)
			if p.Value != "" {
				v, _ := hexBig(p.Value)
				need.Add(need, v)
			}
			if res.FeeWei != nil {
				need.Add(need, res.FeeWei)
			}
			if bal.Cmp(need) < 0 {
				res.Reverted = true
				res.Reason = fmt.Sprintf("insufficient funds: have %s, need %s (value+gas)", fmtDRX(bal), fmtDRX(need))
				res.Notes = append(res.Notes, "sender balance too low for value + fee")
			}
		}
	}
	return res, nil
}

func printSim(title string, r *SimResult) {
	fmt.Printf("── %s (dry-run) ──\n", title)
	fmt.Printf("  gas estimate : %d\n", r.GasEstimate)
	if r.GasPrice != nil && r.GasPrice.Sign() > 0 {
		fmt.Printf("  gas price    : %s wei\n", r.GasPrice.String())
	}
	if r.FeeDRX != "" {
		fmt.Printf("  projected fee: %s\n", r.FeeDRX)
	}
	if r.Reverted {
		fmt.Printf("  status       : ✗ REVERTED\n")
		fmt.Printf("  reason       : %s\n", r.Reason)
	} else {
		fmt.Printf("  status       : ✓ would succeed\n")
		if r.ReturnData != "" && r.ReturnData != "0x" {
			fmt.Printf("  return data  : %s\n", r.ReturnData)
		}
	}
	for _, n := range r.Notes {
		fmt.Printf("  note         : %s\n", n)
	}
}

// decodeRevert turns a raw revert message / data into a readable string.
func decodeRevert(message, data string) string {
	raw := strings.TrimPrefix(strings.TrimSpace(data), "0x")
	if len(raw) >= 8 {
		switch strings.ToLower(raw[:8]) {
		case selError:
			if s := decodeErrorString(raw[8:]); s != "" {
				return s
			}
		case selPanic:
			if len(raw) >= 72 {
				if code, ok := new(big.Int).SetString(strings.ToLower(raw[8:72]), 16); ok {
					return fmt.Sprintf("panic code %s (%s)", code.String(), panicName(code))
				}
			}
		}
	}
	// Fall back to the RPC message, stripping common prefixes.
	m := message
	for _, p := range []string{"execution reverted: ", "execution reverted", "revert: ", "revert"} {
		m = strings.TrimPrefix(m, p)
	}
	return strings.TrimSpace(m)
}

// decodeErrorString ABI-decodes the string payload after Error(string).
func decodeErrorString(hexData string) string {
	if len(hexData) < 128 {
		return ""
	}
	// offset(32) length(32) data…
	lenHex := hexData[64:128]
	n, ok := new(big.Int).SetString(lenHex, 16)
	if !ok || n.Sign() <= 0 || int(n.Int64())*2 > len(hexData)-128 {
		return ""
	}
	body := hexData[128 : 128+n.Int64()*2]
	b, err := hex.DecodeString(body)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\x00")
}

func panicName(code *big.Int) string {
	switch code.Int64() {
	case 0x01:
		return "assertion failed"
	case 0x11:
		return "arithmetic underflow/overflow"
	case 0x12:
		return "division by zero"
	case 0x21:
		return "enum out of range"
	case 0x22:
		return "storage byte array malformed"
	case 0x31:
		return "pop on empty array"
	case 0x32:
		return "array index out of bounds"
	case 0x41:
		return "unexpected external call"
	default:
		return "unknown"
	}
}

func hexBig(s string) (*big.Int, error) {
	n := new(big.Int)
	if strings.HasPrefix(s, "0x") {
		_, ok := n.SetString(s[2:], 16)
		if !ok {
			return nil, fmt.Errorf("bad hex %q", s)
		}
		return n, nil
	}
	_, ok := n.SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("bad number %q", s)
	}
	return n, nil
}
