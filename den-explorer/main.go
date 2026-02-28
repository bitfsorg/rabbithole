package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/bitfsorg/libbitfs-go/network"
)

func main() {
	rpcURL := flag.String("rpc-url", "", "bitcoind RPC URL")
	rpcUser := flag.String("rpc-user", "", "RPC username")
	rpcPass := flag.String("rpc-pass", "", "RPC password")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	net := flag.String("network", "regtest", "Network: regtest|testnet")
	flag.Parse()

	cfg, err := network.ResolveConfig(
		&network.RPCConfig{URL: *rpcURL, User: *rpcUser, Password: *rpcPass},
		nil, *net,
	)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	rpc := network.NewRPCClient(*cfg)
	explorer := NewExplorer(rpc)

	templates, err := LoadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	srv := NewServer(explorer, templates)

	fmt.Printf("Den v0.1.0 — BitFS Blockchain Explorer\nNetwork:  %s\nRPC:      %s\nListen:   http://localhost%s\n", *net, cfg.URL, *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Routes()))
}
