//go:build e2e

// Package testutil provides test utilities for e2e tests against a BSV regtest node.
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// RPCClient is a thin JSON-RPC 1.0 client for a BSV node.
// It uses stdlib net/http with basic auth and sequential request IDs.
type RPCClient struct {
	url      string
	user     string
	password string
	client   *http.Client
	nextID   atomic.Int64
}

// rpcRequest is the JSON-RPC 1.0 request envelope.
type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// rpcResponse is the JSON-RPC 1.0 response envelope.
type rpcResponse struct {
	ID     int64            `json:"id"`
	Result json.RawMessage  `json:"result"`
	Error  *rpcResponseError `json:"error"`
}

// rpcResponseError represents a JSON-RPC error object.
type rpcResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcResponseError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// NewRPCClient creates a new JSON-RPC client targeting the given URL with
// basic auth credentials.
func NewRPCClient(url, user, password string) *RPCClient {
	return &RPCClient{
		url:      url,
		user:     user,
		password: password,
		client:   &http.Client{},
	}
}

// Call performs a JSON-RPC call. The result (if non-nil) is JSON-unmarshalled
// into the value pointed to by result. If the RPC returns an error object,
// it is returned as *rpcResponseError which satisfies the error interface.
func (c *RPCClient) Call(ctx context.Context, method string, params []interface{}, result interface{}) error {
	id := c.nextID.Add(1)

	if params == nil {
		params = []interface{}{}
	}

	reqBody := rpcRequest{
		JSONRPC: "1.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal RPC request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(c.user, c.password)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request to %s: %w", c.url, err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read RPC response body: %w", err)
	}

	// Bitcoin node returns 500 for RPC-level errors but still sends valid JSON.
	// Only treat non-200/non-500 as transport errors.
	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusInternalServerError {
		return fmt.Errorf("HTTP %d from %s: %s", httpResp.StatusCode, c.url, string(respBody))
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return fmt.Errorf("unmarshal RPC response: %w (body: %s)", err, string(respBody))
	}

	if rpcResp.Error != nil {
		return rpcResp.Error
	}

	if result != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("unmarshal RPC result into %T: %w", result, err)
		}
	}

	return nil
}
