#!/usr/bin/env node
const fs = require('fs')
const path = require('path')
let seed = Number(process.env.SMATRIX_BRAND_SEED || Date.now()) >>> 0
function rand() {
  seed = (seed * 1664525 + 1013904223) >>> 0
  return seed / 0xffffffff
}
function point(cx, cy, radius, angle) {
  return [cx + Math.cos(angle) * radius, cy + Math.sin(angle) * radius]
}
const count = 3 + Math.floor(rand() * 3)
const nodes = []
for (let i = 0; i < count; i++) {
  const angle = (Math.PI * 2 * i) / count + rand() * 0.55
  const radius = 22 + rand() * 22
  nodes.push({ x: 50 + Math.cos(angle) * radius, y: 50 + Math.sin(angle) * radius, sides: 3 + Math.floor(rand() * 4), r: 5 + rand() * 7 })
}
const lines = []
for (let i = 0; i < nodes.length; i++) {
  const a = nodes[i], b = nodes[(i + 1) % nodes.length]
  lines.push(`<path d="M${a.x.toFixed(1)} ${a.y.toFixed(1)} L${b.x.toFixed(1)} ${b.y.toFixed(1)}" stroke="#10b981" stroke-width="2.2" stroke-linecap="round" opacity="0.72" filter="url(#glow)"/>`)
}
if (nodes.length > 3) {
  const a = nodes[0], b = nodes[Math.floor(nodes.length / 2)]
  lines.push(`<path d="M${a.x.toFixed(1)} ${a.y.toFixed(1)} L${b.x.toFixed(1)} ${b.y.toFixed(1)}" stroke="#38bdf8" stroke-width="1.6" stroke-dasharray="3 4" opacity="0.68"/>`)
}
const polys = nodes.map((n, idx) => {
  const pts = []
  for (let k = 0; k < n.sides; k++) {
    const [x, y] = point(n.x, n.y, n.r, -Math.PI / 2 + (Math.PI * 2 * k) / n.sides + rand() * 0.22)
    pts.push(`${x.toFixed(1)},${y.toFixed(1)}`)
  }
  const fill = idx === 0 ? '#10b981' : idx % 2 ? '#0ea5e9' : '#8b5cf6'
  return `<polygon points="${pts.join(' ')}" fill="${fill}" stroke="#d1fae5" stroke-width="0.9" opacity="0.92" filter="url(#glow)"/>`
}).join('')
const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><radialGradient id="bg" cx="50%" cy="50%" r="65%"><stop offset="0" stop-color="#12342e"/><stop offset="1" stop-color="#0f172a"/></radialGradient><filter id="glow"><feGaussianBlur stdDeviation="2.4" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs><rect width="100" height="100" rx="22" fill="url(#bg)"/><circle cx="50" cy="50" r="42" fill="none" stroke="#10b981" stroke-width="1" opacity="0.25"/>${lines.join('')}${polys}<text x="50" y="88" text-anchor="middle" font-size="8" font-family="monospace" fill="#a7f3d0" opacity="0.8">SMX</text></svg>`
for (const target of ['s-matrix-frontend/public/favicon.svg', 'frontend/public/favicon.svg']) {
  const file = path.join(process.cwd(), target)
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, svg)
}
console.log(`brand generated seed=${seed}`)
