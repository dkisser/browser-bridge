import {
  copyFileSync,
  existsSync,
  readdirSync,
  readFileSync,
  rmdirSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { resolve } from 'node:path';
import { defineConfig } from 'vite';

export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        popup: resolve(__dirname, 'src/popup.html'),
        offscreen: resolve(__dirname, 'src/offscreen.html'),
        background: resolve(__dirname, 'src/background.ts'),
        content: resolve(__dirname, 'src/content.ts'),
      },
      output: {
        entryFileNames: '[name].js',
        chunkFileNames: '[name].js',
        assetFileNames: (assetInfo) => {
          const name = assetInfo.name ?? '';
          return name.replace(/^src\//, '');
        },
      },
    },
  },
  plugins: [
    {
      name: 'flatten-html',
      closeBundle() {
        const distDir = resolve(__dirname, 'dist');

        // Flatten nested HTML files (Vite puts them under src/)
        for (const htmlFile of ['popup.html', 'offscreen.html']) {
          const nested = resolve(distDir, 'src', htmlFile);
          const flat = resolve(distDir, htmlFile);

          if (existsSync(nested)) {
            const html = readFileSync(nested, 'utf-8');
            writeFileSync(flat, html.replace(/\.\.\//g, './'));
            rmSync(nested);
          }
        }

        const srcDir = resolve(distDir, 'src');
        if (existsSync(srcDir) && readdirSync(srcDir).length === 0) {
          rmdirSync(srcDir);
        }

        copyFileSync(
          resolve(__dirname, 'manifest.json'),
          resolve(distDir, 'manifest.json'),
        );
      },
    },
  ],
});
