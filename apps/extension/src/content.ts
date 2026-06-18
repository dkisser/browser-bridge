interface DomCommand {
  command: string;
  tabId?: number;
  params: Record<string, unknown>;
}

function querySelector(selector: string): Element {
  const el = document.querySelector(selector);
  if (!el) throw new Error(`Element not found: ${selector}`);
  return el;
}

function querySelectorByText(text: string): Element {
  const walker = document.createTreeWalker(
    document.body,
    NodeFilter.SHOW_ELEMENT,
  );
  let node: Element | null;
  while ((node = walker.nextNode() as Element | null)) {
    if (node.textContent?.trim() === text) return node;
  }
  throw new Error(`Element with text not found: ${text}`);
}

function resolveSelector(selector: string): Element {
  try {
    return querySelector(selector);
  } catch {
    return querySelectorByText(selector);
  }
}

function executeCommand(payload: DomCommand): unknown {
  const { command, params } = payload;

  switch (command) {
    case 'click': {
      const el = resolveSelector(params.selector as string);
      (el as HTMLElement).click();
      return { clicked: params.selector };
    }

    case 'type': {
      const el = resolveSelector(params.selector as string);
      const input = el as HTMLInputElement;
      input.focus();
      input.value = params.text as string;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
      return { typed: params.text };
    }

    case 'select': {
      const el = resolveSelector(params.selector as string);
      const select = el as HTMLSelectElement;
      select.value = params.value as string;
      select.dispatchEvent(new Event('change', { bubbles: true }));
      return { selected: params.value };
    }

    case 'scroll': {
      if (params.selector === 'page' || !params.selector) {
        window.scrollBy(params.x as number, params.y as number);
      } else {
        const el = resolveSelector(params.selector as string);
        el.scrollBy(params.x as number, params.y as number);
      }
      return { scrolled: true };
    }

    case 'hover': {
      const el = resolveSelector(params.selector as string);
      el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }));
      return { hovered: params.selector };
    }

    case 'gettext': {
      const el = resolveSelector(params.selector as string);
      return { text: el.textContent };
    }

    case 'gethtml': {
      const el = resolveSelector(params.selector as string);
      return { html: el.innerHTML };
    }

    case 'wait:element': {
      const selector = params.selector as string;
      const timeout = (params.timeout as number) || 10000;

      if (document.querySelector(selector)) {
        return { found: true, selector };
      }

      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          observer.disconnect();
          reject(
            new Error(`Element not found within ${timeout}ms: ${selector}`),
          );
        }, timeout);

        const observer = new MutationObserver(() => {
          if (document.querySelector(selector)) {
            clearTimeout(timer);
            observer.disconnect();
            resolve({ found: true, selector });
          }
        });

        observer.observe(document.body, { childList: true, subtree: true });
      });
    }

    default:
      throw new Error(`Unknown DOM command: ${command}`);
  }
}

chrome.runtime.onMessage.addListener((request, _sender, sendResponse) => {
  if (request.type === 'ping') {
    sendResponse({ type: 'pong' });
    return true;
  }

  if (request.type === 'command') {
    const payload = request.payload as DomCommand;
    Promise.resolve()
      .then(() => executeCommand(payload))
      .then((data) => sendResponse({ status: 'ok', data }))
      .catch((err) => sendResponse({ status: 'error', error: String(err) }));
    return true;
  }

  return false;
});
