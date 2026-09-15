import { describe, expect, test } from 'bun:test';
import type { CloudClient } from '../src/cloud-client';
import type { LocalServer } from '../src/local-server';
import { Router } from '../src/router';
import type { StateManager } from '../src/state';

function setup(status: 'online' | 'idle_wait') {
  const reported: string[] = [];
  const state = { status } as unknown as StateManager;
  const cloud = {
    reportStatus: (s: string) => {
      reported.push(s);
    },
  } as unknown as CloudClient;
  const local = {} as unknown as LocalServer;
  const router = new Router(state, cloud, local);
  return { router, reported };
}

describe('Router cloud reconnect', () => {
  test('re-reports online when the extension is still connected', () => {
    const { router, reported } = setup('online');
    router.handleCloudConnect();
    expect(reported).toEqual(['online']);
  });

  test('does not report status when the extension is not connected', () => {
    const { router, reported } = setup('idle_wait');
    router.handleCloudConnect();
    expect(reported).toEqual([]);
  });
});
