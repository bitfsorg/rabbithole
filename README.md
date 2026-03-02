# RabbitHole

Monorepo for the BitFS + Metanet ecosystem — a decentralized encrypted file system and its incentive network.

## Products

| | BitFS | Metanet |
|---|---|---|
| Role | Decentralized encrypted file system protocol | Decentralized CDN network |
| Website | bitfs.org | metanet.org |
| CLI | `bitfs` (file owners/visitors) | `metanet` (CDN node operators) |
| Users | End users, AI agents | Content owners, node operators |
| Currency | BSV | MNT Token |
| Analogy | IPFS (protocol layer) | Filecoin (incentive layer) |

Two core value props: **Agent Friendly** + **Data can stay off-chain**.

## Repository Structure

```
RabbitHole/
├── bitfs/             — BitFS CLI + daemon (Go)
├── libbitfs-go/       — Shared core library (Go, independent repo)
├── libbitfs-ts/       — Shared core library (TypeScript, planned)
├── metanet/           — Metanet CDN node (Go)
├── den-explorer/      — Blockchain explorer (Go + htmx, independent repo)
├── git-remote-bitfs/  — Git remote helper (Go, independent repo)
├── bitfs-app/         — Desktop/mobile client (Flutter, independent repo)
├── bitfs-extension/   — Browser extension (TypeScript, independent repo)
├── websites/          — Official websites (bitfs.org + metanet.org)
├── docs/              — Design docs, whitepapers, specs, slides, VI
└── tools/             — Build tools (Mermaid renderer)
```

Sub-projects marked "independent repo" have their own `.git` and can be checked out separately.

## Tech Stack

- **Go 1.25+** — CLI, daemon, libraries, CDN node, explorer
- **BSV SDK** — `github.com/bsv-blockchain/go-sdk` (sole blockchain dependency)
- **Flutter 3.27+** — Cross-platform client
- **TypeScript** — Browser extension, planned TS library

## Documentation

| Directory | Contents |
|---|---|
| `docs/design/` | 4-layer design docs (concept → system → detail → test) |
| `docs/whitepaper/` | Academic papers (BitFS + Metanet, EN/ZH) |
| `docs/specs/` | Module specifications |
| `docs/slides/` | HTML5 presentation |
| `docs/vi/` | Visual identity system |
| `docs/references/` | Research papers |

## License

- Source code: OpenBSV License
- Websites, whitepapers, design docs: Separate license
