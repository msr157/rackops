import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { api, subscribe } from './api';
import type { Node, Overview, EventItem, Alert, Service, Project, Environment, IngressConfig } from './api';

// DataProvider owns all fleet-wide state: one fetch + one SSE stream for the
// whole app, so every page renders from the same live picture.
interface Data {
  overview: Overview | null;
  nodes: Node[];
  services: Service[];
  events: EventItem[];
  alerts: Alert[];
  projects: Project[];
  envs: Environment[];
  ingress: IngressConfig | null;
  refresh: () => void;
  nodeName: (id: string) => string;
}

const DataCtx = createContext<Data | null>(null);

export function useData(): Data {
  const d = useContext(DataCtx);
  if (!d) throw new Error('useData outside DataProvider');
  return d;
}

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [events, setEvents] = useState<EventItem[]>([]);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [ingress, setIngress] = useState<IngressConfig | null>(null);

  const refresh = useCallback(() => {
    api.overview().then(setOverview).catch(() => {});
    api.nodes().then((n) => setNodes(n ?? [])).catch(() => {});
    api.services().then((s) => setServices(s ?? [])).catch(() => {});
    api.events().then((e) => setEvents((e ?? []).slice(0, 80))).catch(() => {});
    api.alerts().then((a) => setAlerts(a ?? [])).catch(() => {});
    api.ingress().then(setIngress).catch(() => {});
  }, []);

  useEffect(() => {
    refresh();
    api.projects().then(async (ps) => {
      setProjects(ps ?? []);
      if (ps?.length) setEnvs((await api.envs(ps[0].id)) ?? []);
    }).catch(() => {});

    const off = subscribe({
      node_metrics: (data) => {
        const n = data as Node;
        setNodes((prev) => {
          const i = prev.findIndex((p) => p.id === n.id);
          if (i === -1) return [...prev, n];
          const next = [...prev];
          next[i] = n;
          return next;
        });
      },
      node_joined: () => refresh(),
      node_down: () => refresh(),
      service_update: (data) => {
        const s = data as Service;
        setServices((prev) => {
          const i = prev.findIndex((p) => p.id === s.id);
          if (i === -1) return [...prev, s];
          const next = [...prev];
          next[i] = s;
          return next;
        });
        api.overview().then(setOverview).catch(() => {});
        api.events().then((e) => setEvents((e ?? []).slice(0, 80))).catch(() => {});
      },
      service_removed: (data) => {
        const { id } = data as { id: string };
        setServices((prev) => prev.filter((p) => p.id !== id));
      },
      alert: (data) => {
        const a = data as Alert;
        // Upsert by id — a re-broadcast must never duplicate a card.
        setAlerts((prev) => [a, ...prev.filter((p) => p.id !== a.id)]);
      },
      alert_resolved: (data) => {
        const a = data as Alert;
        setAlerts((prev) => prev.map((p) => (p.id === a.id ? { ...p, resolved: true } : p)));
      },
    });

    // Slow poll as SSE fallback for aggregates. Alerts are included so a
    // missed alert_resolved event can never leave a stale "needs attention"
    // card — the poll converges the UI to the server's truth.
    const t = setInterval(() => {
      api.overview().then(setOverview).catch(() => {});
      api.alerts().then((a) => setAlerts(a ?? [])).catch(() => {});
    }, 8000);
    return () => { off(); clearInterval(t); };
  }, [refresh]);

  const value = useMemo<Data>(() => ({
    overview, nodes, services, events, alerts, projects, envs, ingress, refresh,
    nodeName: (id: string) => nodes.find((n) => n.id === id)?.name ?? id,
  }), [overview, nodes, services, events, alerts, projects, envs, ingress, refresh]);

  return <DataCtx.Provider value={value}>{children}</DataCtx.Provider>;
}
