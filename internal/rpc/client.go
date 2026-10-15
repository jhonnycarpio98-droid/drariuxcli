package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Client talks to an EVM JSON-RPC endpoint (net/http + encoding/json only).
type Client struct {
	URL    string
	HTTP   *http.Client
	nextID int
}

func New(url string) *Client {
	return &Client{
		URL:  strings.TrimRight(url, "/"),
		HTTP: &http.Client{Timeout: 20 * time.Second},
	}
}

type rpcReq struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error"`
}

// Call performs a JSON-RPC call and unmarshals the result into out when non-nil.
func (c *Client) Call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	c.nextID++
	body, _ := json.Marshal(rpcReq{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rpc http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var r rpcResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("bad rpc response: %w (%s)", err, truncate(string(raw), 200))
	}
	if r.Error != nil {
		return &RevertError{Code: r.Error.Code, Message: r.Error.Message, Data: r.Error.Data}
	}
	if out != nil && len(r.Result) > 0 {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func (c *Client) CallRaw(ctx context.Context, method string, params []interface{}) (string, error) {
	var s string
	if err := c.Call(ctx, method, params, &s); err != nil {
		return "", err
	}
	return s, nil
}

// RevertError carries an eth_call / estimateGas failure, i.e. a contract revert
// or simulation error surfaced by the node.
type RevertError struct {
	Code    int
	Message string
	Data    string
}

func (e *RevertError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("%s (data: %s)", e.Message, e.Data)
	}
	return e.Message
}

func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	hexID, err := c.CallRaw(ctx, "eth_chainId", nil)
	if err != nil {
		return nil, err
	}
	return parseHexBig(hexID)
}

func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	h, err := c.CallRaw(ctx, "eth_blockNumber", nil)
	if err != nil {
		return 0, err
	}
	n, err := parseHexBig(h)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

func (c *Client) GasPrice(ctx context.Context) (*big.Int, error) {
	g, err := c.CallRaw(ctx, "eth_gasPrice", nil)
	if err != nil {
		return nil, err
	}
	return parseHexBig(g)
}

// Balance returns the native balance (18-dec wei) of an address.
func (c *Client) Balance(ctx context.Context, addr string) (*big.Int, error) {
	b, err := c.CallRaw(ctx, "eth_getBalance", []interface{}{addr, "latest"})
	if err != nil {
		return nil, err
	}
	return parseHexBig(b)
}

func (c *Client) Nonce(ctx context.Context, addr string) (uint64, error) {
	n, err := c.CallRaw(ctx, "eth_getTransactionCount", []interface{}{addr, "latest"})
	if err != nil {
		return 0, err
	}
	big, err := parseHexBig(n)
	if err != nil {
		return 0, err
	}
	return big.Uint64(), nil
}

func (c *Client) Code(ctx context.Context, addr string) (string, error) {
	return c.CallRaw(ctx, "eth_getCode", []interface{}{addr, "latest"})
}

// CallParams is the eth_call / eth_estimateGas transaction object.
type CallParams struct {
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Gas      string `json:"gas,omitempty"`
	GasPrice string `json:"gasPrice,omitempty"`
	Value    string `json:"value,omitempty"`
	Data     string `json:"data,omitempty"`
}

func (c *Client) EstimateGas(ctx context.Context, p CallParams) (uint64, error) {
	res, err := c.CallRaw(ctx, "eth_estimateGas", []interface{}{p, "latest"})
	if err != nil {
		return 0, err
	}
	n, err := parseHexBig(res)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

func (c *Client) EthCall(ctx context.Context, p CallParams) (string, error) {
	return c.CallRaw(ctx, "eth_call", []interface{}{p, "latest"})
}

func (c *Client) SendRawTransaction(ctx context.Context, rawHex string) (string, error) {
	return c.CallRaw(ctx, "eth_sendRawTransaction", []interface{}{rawHex})
}

func parseHexBig(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0x" {
		return new(big.Int), nil
	}
	n := new(big.Int)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		_, ok := n.SetString(s[2:], 16)
		if !ok {
			return nil, fmt.Errorf("bad hex number %q", s)
		}
		return n, nil
	}
	_, ok := n.SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("bad number %q", s)
	}
	return n, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
