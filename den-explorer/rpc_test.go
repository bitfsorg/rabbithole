package main

import (
	"context"
	"os"
	"testing"

	"github.com/bitfsorg/libbitfs-go/network"
)

func newTestExplorer(t *testing.T) *Explorer {
	t.Helper()
	url := os.Getenv("DEN_RPC_URL")
	if url == "" {
		url = "http://localhost:18332"
	}
	rpc := network.NewRPCClient(network.RPCConfig{
		URL: url, User: "bitfs", Password: "bitfs",
	})
	// Quick health check
	ctx := context.Background()
	_, err := rpc.GetBestBlockHeight(ctx)
	if err != nil {
		t.Skipf("RPC not available: %v", err)
	}
	return NewExplorer(rpc)
}

func TestGetChainInfo(t *testing.T) {
	e := newTestExplorer(t)
	info, err := e.GetChainInfo(context.Background())
	if err != nil {
		t.Fatalf("GetChainInfo: %v", err)
	}
	if info.Chain != "regtest" {
		t.Errorf("expected regtest, got %s", info.Chain)
	}
	if info.Blocks < 0 {
		t.Errorf("invalid block count: %d", info.Blocks)
	}
}

func TestGetRecentBlocks(t *testing.T) {
	e := newTestExplorer(t)
	blocks, err := e.GetRecentBlocks(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetRecentBlocks: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("no blocks returned")
	}
	// Genesis or first block should exist
	if blocks[len(blocks)-1].Height < 0 {
		t.Errorf("invalid block height")
	}
}
