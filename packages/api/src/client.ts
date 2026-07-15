import type { Meta, Node } from './types';

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new Error(`${url} → HTTP ${res.status}`);
  return res.json() as Promise<T>;
}

export function fetchNodes(baseUrl = '', signal?: AbortSignal): Promise<Node[]> {
  return getJSON<Node[]>(`${baseUrl}/api/v1/nodes`, signal);
}

export function fetchMeta(baseUrl = '', signal?: AbortSignal): Promise<Meta> {
  return getJSON<Meta>(`${baseUrl}/api/v1/meta`, signal);
}
