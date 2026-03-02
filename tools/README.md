# Tools

Build tools for the RabbitHole workspace.

## Mermaid Renderer

TypeScript CLI that converts `.mmd` Mermaid diagram files to SVG.

### Prerequisites

- [Bun](https://bun.sh) runtime

### Usage

```bash
bun tools/mermaid/cli.ts <directory> [--theme bitfs] [--no-ascii]
```

Processes all `.mmd` files in the given directory and outputs `.svg` files alongside them.
