const DEFAULT_BASE = '/api';

function resolveBase(): string {
  const fromEnv = (import.meta as ImportMeta & { env?: Record<string, string> }).env?.VITE_API_BASE;
  return fromEnv && fromEnv.length > 0 ? fromEnv : DEFAULT_BASE;
}

export async function postEvent(
  eventName: string,
  payload: Record<string, unknown>,
): Promise<void> {
  const base = resolveBase();
  try {
    await fetch(`${base}/events`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ event: eventName, payload, ts: Date.now() }),
      keepalive: true,
    });
  } catch (err) {
    // eslint-disable-next-line no-console
    console.warn('[api] postEvent failed', eventName, err);
  }
}
