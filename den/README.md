# Den — BitFS Blockchain Explorer

A web-based BSV blockchain explorer with BitFS/Metanet protocol verification.
Built for development and debugging on regtest and testnet.

## Quick Start

```bash
# Start regtest node (requires Docker)
cd ../bitfs/e2e && docker compose up -d

# Build and run
cd ../../den && go build . && ./den

# Open http://localhost:8080
```

## Features

**Standard Explorer:**
- Block and transaction browsing
- Address UTXO lookup
- Search by txid, block hash, height, or address

**BitFS Protocol Verification:**
- Metanet OP_RETURN decoding (MetaFlag, P_node, TLV payload)
- Metanet DAG tree visualization (node types, access levels, children)
- Method 42 encryption analysis (ECDH key derivation chain)
- SPV Merkle proof verification (transaction inclusion + directory MerkleRoot)

## Configuration

```
den [flags]
  -rpc-url     bitcoind RPC URL      (default from network preset)
  -rpc-user    RPC username           (default "bitfs")
  -rpc-pass    RPC password           (default "bitfs")
  -addr        HTTP listen address    (default ":8080")
  -network     regtest|testnet        (default "regtest")
```

## Tech Stack

Go + htmx + libbitfs. Single binary, no build tools, no JavaScript framework.
