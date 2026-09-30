import { LOCAL_HOST, LOCAL_WS_PORT } from '@browser-bridge/shared';
import { requestPolicyOp } from './policy-ops';

// The settings tab displays this same address, so both read the one
// declaration in shared/constants rather than each spelling it out.
export const API_BASE = `http://${LOCAL_HOST}:${LOCAL_WS_PORT}`;

export interface StatusResponse {
  success: boolean;
  data?: {
    browserId: string;
    paired: boolean;
  };
  error?: string;
}

interface PairConfirmResponse {
  success: boolean;
  data?: { token: string };
  error?: string;
  attemptsRemaining?: number;
}

interface PongResponse {
  connected?: boolean;
}

export async function fetchStatus(): Promise<StatusResponse> {
  try {
    const response = await fetch(`${API_BASE}/api/status`);
    return (await response.json()) as StatusResponse;
  } catch {
    return { success: false, error: 'Bridge unreachable' };
  }
}

export async function queryBrowserConnection(): Promise<boolean> {
  try {
    const response = (await chrome.runtime.sendMessage({
      type: 'ping',
    })) as PongResponse | undefined;
    return response?.connected ?? false;
  } catch {
    return false;
  }
}

// Confirms a pairing code against bridge-core. Returns an error string for
// the pairing drawer, or null on success (token persisted, reconnect sent).
export async function confirmPairingCode(code: string): Promise<string | null> {
  try {
    const response = await fetch(`${API_BASE}/api/pair/confirm`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    });
    const result = (await response.json()) as PairConfirmResponse;
    if (result.success && result.data?.token) {
      await requestPolicyOp({
        op: 'set_pairing_token',
        token: result.data.token,
      });
      void chrome.runtime.sendMessage({ type: 'connect' }).catch(() => {});
      return null;
    }
    const attempts =
      result.attemptsRemaining !== undefined
        ? ` (${result.attemptsRemaining} attempts remaining)`
        : '';
    return `Pairing failed: ${result.error ?? 'unknown'}${attempts}`;
  } catch {
    return 'Bridge unreachable';
  }
}
