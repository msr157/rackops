import { useEffect, useState } from 'react';
import { fetchNodes } from './client';
import type { Node } from './types';

export interface UseNodesResult {
  nodes: Node[];
  error: string | null;
  lastUpdated: Date | null;
}

export function useNodes(baseUrl = '', intervalMs = 5000): UseNodesResult {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    const poll = async () => {
      try {
        const data = await fetchNodes(baseUrl, controller.signal);
        setNodes(data);
        setError(null);
        setLastUpdated(new Date());
      } catch (err) {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      }
    };

    void poll();
    const timer = setInterval(poll, intervalMs);

    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [baseUrl, intervalMs]);

  return { nodes, error, lastUpdated };
}
