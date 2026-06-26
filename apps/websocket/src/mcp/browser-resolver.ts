import type { BrowserConnection } from '@browser-bridge/shared';

export type BrowserResolutionSuccess = { success: true; browserId: string };
export type BrowserResolutionFailure = {
  success: false;
  message: string;
  availableBrowsers?: BrowserConnection[];
};
export type BrowserResolutionResult =
  | BrowserResolutionSuccess
  | BrowserResolutionFailure;

function formatBrowserList(browsers: BrowserConnection[]): string {
  return browsers.map((b) => `- ${b.browserId} (${b.status})`).join('\n');
}

export function resolveBrowser(
  explicitBrowserId: string | undefined,
  browsers: BrowserConnection[],
): BrowserResolutionResult {
  if (explicitBrowserId) {
    const match = browsers.find((b) => b.browserId === explicitBrowserId);
    if (!match) {
      return {
        success: false,
        message: `Browser "${explicitBrowserId}" is not connected.`,
        availableBrowsers: browsers,
      };
    }
    if (match.status !== 'online') {
      return {
        success: false,
        message: `Browser "${explicitBrowserId}" is not online (status: ${match.status}).`,
        availableBrowsers: browsers,
      };
    }
    return { success: true, browserId: explicitBrowserId };
  }

  const online = browsers.filter((b) => b.status === 'online');

  if (online.length === 0) {
    return {
      success: false,
      message: 'No browser connected. Start the extension/local-proxy first.',
      availableBrowsers: browsers,
    };
  }

  if (online.length === 1) {
    return { success: true, browserId: online[0].browserId };
  }

  return {
    success: false,
    message: `Multiple browsers are online. Call set_browser with one of:\n${formatBrowserList(online)}`,
    availableBrowsers: online,
  };
}
