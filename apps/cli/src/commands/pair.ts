import { LOCAL_WS_PORT } from '@browser-bridge/shared';

export interface PairOptions {
  local?: string;
}

export async function pair(options: PairOptions): Promise<void> {
  const base = options.local ?? `http://localhost:${LOCAL_WS_PORT}`;

  let res: Response;
  try {
    res = await fetch(`${base}/api/pair/start`, { method: 'POST' });
  } catch {
    console.error(`Could not reach the local proxy at ${base}.`);
    console.error('Is the service running? Start it with: bridge service up');
    process.exit(1);
    return;
  }

  if (!res.ok) {
    console.error(`Pairing request failed: HTTP ${res.status}`);
    process.exit(1);
    return;
  }

  const body = (await res.json()) as {
    success: boolean;
    data?: { code: string; expiresIn: number };
    error?: string;
  };

  if (!body.success || !body.data) {
    console.error(`Pairing request failed: ${body.error ?? 'unknown error'}`);
    process.exit(1);
    return;
  }

  console.log('');
  console.log(`  Pairing code:  ${body.data.code}`);
  console.log('');
  console.log('  Enter this code in the Browser Bridge side panel to pair ');
  console.log(
    `  The code is valid for ${Math.round(body.data.expiresIn / 60000)} minutes. Re-running this command generates a new code and invalidates the previous one.`,
  );
  console.log('');
}
