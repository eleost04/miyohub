import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { brotliCompressSync, constants, gzipSync } from 'node:zlib'
import { readFile, writeFile } from 'node:fs/promises'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

export default defineConfig({
  define: { __MIYOHUB_VERSION__: JSON.stringify(readFileSync(new URL('../internal/buildinfo/VERSION', import.meta.url), 'utf8').trim()) },
  plugins: [vue(), tailwindcss(), {
    name: 'miyohub-precompressed-assets',
    apply: 'build',
    enforce: 'post',
    async writeBundle(options, bundle) {
      if (!options.dir) throw new Error('Static compression requires an output directory')
      // Vite rewrites preload links late in generateBundle. Compress the final
      // files after all bundle transforms, not the earlier in-memory chunks.
      for (const fileName of Object.keys(bundle)) {
        if (!/\.(js|css|html|svg)$/.test(fileName)) continue
        const target = resolve(options.dir, fileName)
        const source = await readFile(target)
        if (source.length < 1024) continue
        for (const [suffix, compressed] of [
          ['gz', gzipSync(source, { level: 9 })],
          ['br', brotliCompressSync(source, { params: { [constants.BROTLI_PARAM_QUALITY]: 9 } })],
        ] as const) {
          if (compressed.length < source.length) await writeFile(target + '.' + suffix, compressed)
        }
      }
    },
  }],
  server: { port: 5173, proxy: { '/api': 'http://127.0.0.1:5890' } },
})
