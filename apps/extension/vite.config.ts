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

const LOGO_PATH = '../../docs/assets/logo.png';

function copyBuildArtifact(
  sourcePath: string,
  destinationPath: string,
  label: string,
): void {
  if (!existsSync(sourcePath)) {
    throw new Error(
      `[flatten-html] ${label} not found at ${sourcePath}. Ensure the source file exists.`,
    );
  }
  copyFileSync(sourcePath, destinationPath);
}

export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    // emptyOutDir would wipe dist/content.js on every rebuild — the
    // content vite (vite.content.config.ts) shares this directory and
    // races with us. The `build` script does an explicit `rm -rf dist`
    // before invoking vite to keep production builds clean.
    emptyOutDir: false,
    rollupOptions: {
      input: {
        sidepanel: resolve(__dirname, 'src/sidepanel.html'),
        settings: resolve(__dirname, 'src/settings.html'),
        offscreen: resolve(__dirname, 'src/offscreen.html'),
        background: resolve(__dirname, 'src/background.ts'),
      },
      output: {
        format: 'es',
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
        for (const htmlFile of [
          'sidepanel.html',
          'settings.html',
          'offscreen.html',
        ]) {
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

        copyBuildArtifact(
          resolve(__dirname, 'manifest.json'),
          resolve(distDir, 'manifest.json'),
          'manifest',
        );

        copyBuildArtifact(
          resolve(__dirname, LOGO_PATH),
          resolve(distDir, 'icon.png'),
          'logo',
        );
      },
    },
  ],
});
