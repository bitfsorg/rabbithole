# BitFS Dashboard

Web dashboard for monitoring and managing a BitFS daemon node. Built with React 19 + Vite 6 + TailwindCSS 4.

## Development

```bash
npm install
npm run dev          # Start dev server (http://localhost:5173/_dashboard/)
```

## Production Build

```bash
npm run build        # Build to dist/
npm run preview      # Preview production build locally
```

Or from the project root:

```bash
make dashboard       # Install deps + build
make dashboard-dev   # Install deps + start dev server
```

## Embedding in Daemon

The built `dist/` directory is embedded into the Go binary via `go:embed` (see `embed.go`). After building:

1. Run `npm run build` to generate `dist/`
2. Uncomment the `//go:embed` directive in `embed.go`
3. Rebuild the daemon: `go build ./cmd/bitfs`

The dashboard is served at `/_dashboard/*` by the daemon.

## Pages

| Route | Component | Purpose |
|-------|-----------|---------|
| `/` | DashboardHome | Node overview and stats |
| `/storage` | Storage | Content-addressed store info |
| `/network` | Network | Peer connections, SPV status |
| `/wallet` | Wallet | HD wallet, balance, UTXOs |
| `/logs` | Logs | Daemon log viewer |

## Project Structure

```
dashboard/
├── embed.go              # Go embed.FS for daemon integration
├── package.json
├── vite.config.ts
├── tsconfig.json
├── index.html
└── src/
    ├── main.tsx           # Entry point
    ├── App.tsx            # Router setup
    ├── index.css          # Tailwind imports
    ├── layouts/
    │   └── MainLayout.tsx # Sidebar + content area
    ├── pages/
    │   ├── DashboardHome.tsx
    │   ├── Storage.tsx
    │   ├── Network.tsx
    │   ├── Wallet.tsx
    │   └── Logs.tsx
    └── lib/
        └── api.ts         # Daemon HTTP client
```
