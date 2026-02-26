#!/usr/bin/env bun
/**
 * Mermaid diagram renderer — converts .mmd files to SVG.
 *
 * Usage:
 *   bun tools/mermaid/cli.ts <dir> [--theme bitfs] [--no-ascii]
 *
 * Arguments:
 *   <dir>         Directory containing .mmd files (SVGs go to <dir>/svg/)
 *   --theme NAME  Theme name from tools/mermaid/themes/ (default: bitfs)
 *   --no-ascii    Skip ASCII terminal preview
 */
import { renderMermaidSVG, renderMermaidASCII, THEMES } from 'beautiful-mermaid'
import type { RenderOptions, AsciiRenderOptions } from 'beautiful-mermaid'
import { readdir, readFile, writeFile, mkdir } from 'node:fs/promises'
import { join, basename, resolve } from 'node:path'

interface ThemeModule {
  svg: RenderOptions
  ascii?: AsciiRenderOptions
}

function parseArgs(args: string[]) {
  let dir = ''
  let themeName = 'bitfs'
  let ascii = true

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--theme' && args[i + 1]) {
      themeName = args[++i]
    } else if (args[i] === '--no-ascii') {
      ascii = false
    } else if (!args[i].startsWith('-')) {
      dir = args[i]
    }
  }

  if (!dir) {
    console.error('Usage: bun tools/mermaid/cli.ts <dir> [--theme bitfs] [--no-ascii]')
    process.exit(1)
  }

  return { dir: resolve(dir), themeName, ascii }
}

async function loadTheme(name: string): Promise<ThemeModule> {
  // Try custom theme from themes/ directory
  const themePath = join(import.meta.dirname, 'themes', `${name}.ts`)
  try {
    return await import(themePath) as ThemeModule
  } catch {
    // Fall back to built-in beautiful-mermaid theme
    if (name in THEMES) {
      return { svg: THEMES[name as keyof typeof THEMES] }
    }
    console.error(`Theme "${name}" not found in themes/ or beautiful-mermaid built-ins.`)
    console.error(`Available built-ins: ${Object.keys(THEMES).join(', ')}`)
    process.exit(1)
  }
}

async function main() {
  const { dir, themeName, ascii } = parseArgs(process.argv.slice(2))
  const theme = await loadTheme(themeName)
  const svgDir = join(dir, 'svg')

  await mkdir(svgDir, { recursive: true })

  const files = (await readdir(dir))
    .filter(f => f.endsWith('.mmd'))
    .sort()

  if (files.length === 0) {
    console.log(`No .mmd files found in ${dir}`)
    return
  }

  console.log(`Rendering ${files.length} diagram(s) with theme "${themeName}":\n`)

  for (const file of files) {
    const name = basename(file, '.mmd')
    const source = await readFile(join(dir, file), 'utf-8')

    // Render SVG
    const svg = renderMermaidSVG(source, theme.svg)
    const svgPath = join(svgDir, `${name}.svg`)
    await writeFile(svgPath, svg)

    // ASCII preview
    console.log(`--- ${name} ---`)
    if (ascii) {
      try {
        const asciiOut = renderMermaidASCII(source, theme.ascii ?? {})
        console.log(asciiOut)
      } catch {
        console.log('  (ASCII preview skipped — diagram too complex)')
      }
    }
    console.log(`  -> ${svgPath}\n`)
  }

  console.log(`Done. ${files.length} SVG(s) written to ${svgDir}`)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
