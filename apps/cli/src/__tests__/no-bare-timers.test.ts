import { describe, expect, it } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const SRC_DIR = join(import.meta.dir, '..');
const EXCLUDED_FILES = new Set(['managedClient.ts', 'no-bare-timers.test.ts']);

function walkTsFiles(dir: string): string[] {
  const result: string[] = [];
  for (const entry of readdirSync(dir)) {
    if (entry.startsWith('__tests__')) continue;

    const fullPath = join(dir, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      result.push(...walkTsFiles(fullPath));
    } else if (entry.endsWith('.ts') && !EXCLUDED_FILES.has(entry)) {
      result.push(fullPath);
    }
  }
  return result;
}

describe('CLI source files', () => {
  const forbiddenPatterns = [
    { name: 'setTimeout', regex: /(?<!clear)setTimeout\s*\(/ },
    { name: 'setInterval', regex: /(?<!clear)setInterval\s*\(/ },
    { name: 'new WebSocket', regex: /new\s+WebSocket\s*\(/ },
  ];

  for (const file of walkTsFiles(SRC_DIR)) {
    const relativePath = file.replace(`${process.cwd()}/`, '');
    const content = readFileSync(file, 'utf-8');

    for (const { name, regex } of forbiddenPatterns) {
      it(`${relativePath} does not use bare ${name}`, () => {
        expect(content).not.toMatch(regex);
      });
    }
  }
});
