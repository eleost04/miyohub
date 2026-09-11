// Generate only the small compatibility fallbacks. SVG remains the primary icon.
import { readFile, writeFile } from 'node:fs/promises'
import { chromium } from '@playwright/test'

const source = await readFile(new URL('../public/favicon.svg', import.meta.url), 'utf8')
const browser = await chromium.launch({ headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 180, height: 180 }, deviceScaleFactor: 1 })
  await page.setContent('<style>body{margin:0}svg{display:block;width:100%;height:100%}</style>' + source)
  const apple = await page.locator('svg').screenshot({ omitBackground: true })
  await writeFile(new URL('../public/apple-touch-icon.png', import.meta.url), apple)
  const sizes = [16, 32], images = []
  for (const size of sizes) { await page.setViewportSize({ width: size, height: size }); images.push(await page.locator('svg').screenshot({ omitBackground: true })) }
  const header = Buffer.alloc(6 + 16 * sizes.length)
  header.writeUInt16LE(1, 2); header.writeUInt16LE(sizes.length, 4)
  let offset = header.length
  for (let n = 0; n < sizes.length; n++) {
    const at = 6 + n*16
    header[at] = sizes[n]; header[at+1] = sizes[n]; header.writeUInt16LE(1, at+4); header.writeUInt16LE(32, at+6)
    header.writeUInt32LE(images[n].length, at+8); header.writeUInt32LE(offset, at+12); offset += images[n].length
  }
  await writeFile(new URL('../public/favicon.ico', import.meta.url), Buffer.concat([header, ...images]))
} finally { await browser.close() }
