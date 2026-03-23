#!/usr/bin/env node
/**
 * BitFS CLI Banner Generator
 * Uses figlet (ANSI Shadow) + gradient-string with brand colors.
 * Generates terminal output + HTML preview.
 */
import figlet from "figlet";
import gradient from "gradient-string";
import { writeFileSync } from "fs";

// ── Brand gradients ──
const BITFS_GRADIENTS = {
  botanical: ["#a07548", "#c9956b", "#dbb08a"],
  amberLight: ["#b47630", "#e8983e", "#f0b868"],
};

const METANET_GRADIENTS = {
  amber: ["#b47630", "#e8983e", "#f0b868"],
  botanical: ["#a07548", "#c9956b", "#dbb08a"],
};

const FONT = "ANSI Shadow";

function generateLogo(text, gradientColors) {
  const ascii = figlet.textSync(text, {
    font: FONT,
    horizontalLayout: "default",
    verticalLayout: "default",
    width: 120,
  });
  const grad = gradient(gradientColors);
  return { raw: ascii, colored: grad.multiline(ascii) };
}

// ── Generate all variants ──
const variants = [];

for (const [name, colors] of Object.entries(BITFS_GRADIENTS)) {
  const { raw, colored } = generateLogo("BitFS", colors);
  variants.push({ name: `BitFS — ${name}`, raw, colored, colors });
}

for (const [name, colors] of Object.entries(METANET_GRADIENTS)) {
  const { raw, colored } = generateLogo("MetaNet", colors);
  variants.push({ name: `MetaNet — ${name}`, raw, colored, colors });
}

// ── Terminal output ──
const dim = "\x1b[38;2;140;130;118m";
const text = "\x1b[38;2;212;205;196m";
const reset = "\x1b[0m";

for (const v of variants) {
  console.log();
  console.log(`${dim}${"━".repeat(76)}${reset}`);
  console.log(`${text}  ${v.name}${reset}  ${dim}[${v.colors.join(" → ")}]${reset}`);
  console.log(`${dim}${"━".repeat(76)}${reset}`);
  console.log();
  console.log(v.colored);
  if (v.name.startsWith("BitFS")) {
    console.log(`${text}  Decentralized Encrypted File System${reset}  ${dim}v0.1.0${reset}`);
    console.log(`${dim}  https://bitfs.org${reset}`);
  } else {
    console.log(`${text}  Decentralized CDN Network${reset}  ${dim}v0.1.0-dev [PREVIEW]${reset}`);
    console.log(`${dim}  https://metanet.org${reset}`);
  }
  console.log();
}

// ── Generate HTML preview ──
// Extract ANSI colors from the colored output and convert to HTML spans
function ansiToHtml(ansiStr) {
  // Replace ANSI color codes with HTML spans
  let html = "";
  let inSpan = false;
  const parts = ansiStr.split(/(\x1b\[[0-9;]*m)/);
  for (const part of parts) {
    const match = part.match(/\x1b\[38;2;(\d+);(\d+);(\d+)m/);
    if (match) {
      if (inSpan) html += "</span>";
      const [, r, g, b] = match;
      html += `<span style="color:rgb(${r},${g},${b})">`;
      inSpan = true;
    } else if (part === "\x1b[0m" || part === "\x1b[39m") {
      if (inSpan) { html += "</span>"; inSpan = false; }
    } else {
      html += part.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    }
  }
  if (inSpan) html += "</span>";
  return html;
}

let htmlSections = "";
for (const v of variants) {
  const coloredHtml = ansiToHtml(v.colored);
  const isMetanet = v.name.startsWith("MetaNet");
  const tagline = isMetanet
    ? `<span class="tx">Decentralized CDN Network</span>  <span class="dm">v0.1.0-dev [PREVIEW]</span>\n  <span class="dm">https://metanet.org</span>`
    : `<span class="tx">Decentralized Encrypted File System</span>  <span class="dm">v0.1.0</span>\n  <span class="dm">https://bitfs.org</span>`;

  htmlSections += `
<div class="banner-box${isMetanet ? " metanet" : ""}">
<div class="label">${v.name} <span class="colors">[${v.colors.join(" → ")}]</span></div>
<pre>
${coloredHtml}
  ${tagline}
</pre>
</div>
`;
}

const htmlContent = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>BitFS CLI Banner — Final Preview</title>
<style>
  body {
    background: #0a0908;
    color: #d4cdc4;
    font-family: 'JetBrains Mono', 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
    padding: 40px 40px 80px;
    line-height: 1.15;
    font-size: 15px;
  }
  .banner-box {
    background: #110f0d;
    border: 1px solid #2e2924;
    border-radius: 12px;
    padding: 32px 40px;
    margin-bottom: 48px;
    position: relative;
    overflow: hidden;
  }
  .banner-box::before {
    content: '';
    position: absolute;
    top: 0; left: 0; right: 0;
    height: 3px;
    background: linear-gradient(90deg, #a07548, #c9956b, #dbb08a, #c9956b, #a07548);
  }
  .banner-box.metanet::before {
    background: linear-gradient(90deg, #b07630, #e8983e, #f0b060, #e8983e, #b07630);
  }
  .label {
    color: #8a7a68;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.15em;
    margin-bottom: 24px;
    padding-bottom: 8px;
    border-bottom: 1px solid #1e1b18;
  }
  .label .colors { color: #5a5045; }
  pre {
    margin: 0;
    font-size: 15px;
    line-height: 1.15;
    font-family: inherit;
  }
  .tx { color: #d4cdc4; }
  .dm { color: #8c8076; }
  h1 { color: #c9956b; font-weight: 300; font-size: 28px; margin-bottom: 8px; }
  .subtitle { color: #6a6058; font-size: 13px; margin-bottom: 48px; }
</style>
</head>
<body>

<h1>BitFS CLI Banner — Final</h1>
<p class="subtitle">figlet ANSI Shadow + gradient-string with BitFS/Metanet brand colors. 7 gradient variants.</p>

${htmlSections}

</body>
</html>`;

const outputPath = new URL('./banner-preview.html', import.meta.url).pathname;
writeFileSync(outputPath, htmlContent);
console.log(`\n${dim}HTML preview written to tools/banner-preview.html${reset}`);
