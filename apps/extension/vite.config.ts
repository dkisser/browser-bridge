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
      name: 'flatten-popup-html',
      closeBundle() {
        const distDir = resolve(__dirname, 'dist');
        const nestedPopup = resolve(distDir, 'src/popup.html');
        const flatPopup = resolve(distDir, 'popup.html');

        if (existsSync(nestedPopup)) {
          const html = readFileSync(nestedPopup, 'utf-8');
          writeFileSync(flatPopup, html.replace(/\.\.\//g, './'));
          rmSync(nestedPopup);

          const srcDir = resolve(distDir, 'src');
          if (existsSync(srcDir) && readdirSync(srcDir).length === 0) {
            rmdirSync(srcDir);
          }
        }

        copyFileSync(
          resolve(__dirname, 'manifest.json'),
          resolve(distDir, 'manifest.json'),
        );
      },
    },
  ],
});
