import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { api, subscribe } from './api';
import type { Node, Overview, EventItem, Alert, Service, Project, Environment, IngressConfig, FrontDoorConfig } from './api';

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
  frontDoor: FrontDoorConfig | null;
  refresh: () => void;
  nodeName: (id: string) => string;
  /** True once the fleet has loaded at least once. Empty states must wait for it. */
  loaded: boolean;
  /** Whether the control plane answered the last time we asked. */
  reachable: boolean;
  /** When the fleet last loaded successfully — the age of what's on screen. */
  lastSync: string | null;
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
  const [frontDoor, setFrontDoor] = useState<FrontDoorConfig | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [reachable, setReachable] = useState(true);
  const [lastSync, setLastSync] = useState<string | null>(null);

  // The three calls that make up the fleet picture. When they fail together the
  // control plane is unreachable, and the UI has to say so — an unanswered
  // fetch rendered as an empty rack reads as "you have nothing", which is the
  // worst possible lie to tell an operator mid-outage.
  const refresh = useCallback(async () => {
    try {
      const [ov, ns, ss] = await Promise.all([api.overview(), api.nodes(), api.services()]);
      setOverview(ov);
      setNodes(ns ?? []);
      setServices(ss ?? []);
      setReachable(true);
      setLoaded(true);
      setLastSync(new Date().toISOString());
    } catch {
      setReachable(false);
    }
    // Secondary detail — a failure here leaves the last known values in place.
    api.events().then((e) => setEvents((e ?? []).slice(0, 80))).catch(() => {});
    api.alerts().then((a) => setAlerts(a ?? [])).catch(() => {});
    api.ingress().then(setIngress).catch(() => {});
    api.frontDoor().then(setFrontDoor).catch(() => {});
  }, []);

  // Held in refs so the subscription and poll below never re-arm on each tick.
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  const reachableRef = useRef(reachable);
  reachableRef.current = reachable;

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
      node_joined: () => refreshRef.current(),
      node_down: () => refreshRef.current(),
      services_changed: () => refreshRef.current(),
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
      // The tunnel's status changes without anyone clicking anything — a
      // reconnect, a POP failover, a quota trip. It has to arrive live or the
      // Network page would show a stale "connected" through an outage.
      frontdoor_update: (data) => setFrontDoor(data as FrontDoorConfig),
      ingress_update: (data) => setIngress(data as IngressConfig),
    });

    // Slow poll as SSE fallback for aggregates. Alerts are included so a
    // missed alert_resolved event can never leave a stale "needs attention"
    // card — the poll converges the UI to the server's truth. While the server
    // is unreachable it doubles as the reconnect probe, so recovery needs no
    // action from the operator.
    const t = setInterval(() => {
      if (!reachableRef.current) { refreshRef.current(); return; }
      api.overview()
        .then((o) => { setOverview(o); setReachable(true); setLastSync(new Date().toISOString()); })
        .catch(() => setReachable(false));
      api.alerts().then((a) => setAlerts(a ?? [])).catch(() => {});
    }, 8000);
    return () => { off(); clearInterval(t); };
  }, [refresh]);

  const value = useMemo<Data>(() => ({
    overview, nodes, services, events, alerts, projects, envs, ingress, frontDoor, refresh,
    nodeName: (id: string) => nodes.find((n) => n.id === id)?.name ?? id,
    loaded, reachable, lastSync,
  }), [overview, nodes, services, events, alerts, projects, envs, ingress, frontDoor, refresh, loaded, reachable, lastSync]);

  return <DataCtx.Provider value={value}>{children}</DataCtx.Provider>;
}
