import type { RenderOptions, AsciiRenderOptions } from 'beautiful-mermaid'

/** BitFS brand theme — dark background with blue accents. */
export const svg: RenderOptions = {
  bg: '#0f172a',       // slate-900
  fg: '#e2e8f0',       // slate-200
  accent: '#38bdf8',   // sky-400 (BitFS brand blue)
  line: '#475569',     // slate-600
  muted: '#94a3b8',    // slate-400
  surface: '#1e293b',  // slate-800
  border: '#334155',   // slate-700
  font: 'Inter, SF Pro Display, system-ui, sans-serif',
  padding: 48,
}

/** Bright ASCII theme for terminal preview. */
export const ascii: AsciiRenderOptions = {
  colorMode: 'truecolor',
  theme: {
    fg: '#f8fafc',         // slate-50 — bright white text
    border: '#38bdf8',     // sky-400 — bright blue borders
    line: '#a78bfa',       // violet-400 — purple edge lines
    arrow: '#facc15',      // yellow-400 — yellow arrowheads
    corner: '#a78bfa',     // violet-400 — match edge lines
    junction: '#38bdf8',   // sky-400 — match borders
  },
}
