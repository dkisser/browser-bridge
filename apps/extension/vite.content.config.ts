// content.js — built as an IIFE so Chrome MV3 can load it as a classic
// script under `content_scripts`. The main app config (`vite.config.ts`)
// builds everything else as ESM; this one only emits content.js.

import { resolve } from 'node:path';
import { defineConfig } from 'vite';

export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    rollupOptions: {
      input: {
        content: resolve(__dirname, 'src/content.ts'),
      },
      output: {
        // IIFE wraps everything in `(function(){ ... })()` — no top-level
        // imports or exports. Chrome's content_scripts has no
        // `type:"module"` knob, so a real ESM bundle would be rejected as
        // "Cannot use import statement outside a module" the moment it
        // gets injected into a page.
        format: 'iife',
        entryFileNames: '[name].js',
        // inlineDynamicImports defaults to true under IIFE; explicit so
        // any future contributor sees the constraint.
        inlineDynamicImports: true,
      },
    },
  },
});
