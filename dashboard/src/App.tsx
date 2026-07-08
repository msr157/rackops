import { useEffect, useState } from 'react';
import {
  LayoutGrid, Server, Boxes, HardDrive, Bell, Plus, Cpu, Activity, Network, Bot, Settings, LogOut,
} from 'lucide-react';
import { api, subscribe, getToken, clearToken } from './api';
import type { Node, Overview, EventItem, Alert, Service, Project, Environment } from './api';
import { OverviewPage } from './pages/Overview';
import { NodesPage } from './pages/Nodes';
import { ServicesPage } from './pages/Services';
import { StoragePage } from './pages/Storage';
import { AlertsPage } from './pages/Alerts';
import { NetworkPage } from './pages/Network';
import { CopilotPage } from './pages/Copilot';
import { DeployModal } from './pages/DeployModal';
import { OnboardModal } from './pages/OnboardModal';
import { SettingsModal } from './pages/SettingsModal';
import { AuthScreen } from './pages/Auth';

type Tab = 'overview' | 'nodes' | 'services' | 'network' | 'storage' | 'alerts' | 'copilot';

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [tab, setTab] = useState<Tab>('overview');
  const [nodes, setNodes] = useState<Node[]>([]);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [events, setEvents] = useState<EventItem[]>([]);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [showDeploy, setShowDeploy] = useState(false);
  const [showOnboard, setShowOnboard] = useState(false);
  const [showSettings, setShowSettings] = useState(false);

  // Auth gate: needsSetup or an existing session decides the screen.
  useEffect(() => {
    const onLogout = () => setAuthed(false);
    window.addEventListener('rackops-logout', onLogout);
    api.authStatus()
      .then((s) => setAuthed(s.needsSetup ? false : !!getToken()))
      .catch(() => setAuthed(!!getToken()));
    return () => window.removeEventListener('rackops-logout', onLogout);
  }, []);

  const refresh = () => {
    api.overview().then(setOverview).catch(() => {});
    api.nodes().then((n) => setNodes(n ?? [])).catch(() => {});
    api.services().then((s) => setServices(s ?? [])).catch(() => {});
    api.events().then((e) => setEvents((e ?? []).slice(0, 60))).catch(() => {});
    api.alerts().then((a) => setAlerts(a ?? [])).catch(() => {});
  };

  useEffect(() => {
    if (!authed) return;
    refresh();
    api.projects().then(async (ps) => {
      setProjects(ps ?? []);
      if (ps?.length) setEnvs((await api.envs(ps[0].id)) ?? []);
    }).catch(() => {});

    // Live updates over SSE.
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
        api.events().then((e) => setEvents((e ?? []).slice(0, 60))).catch(() => {});
      },
      service_removed: (data) => {
        const { id } = data as { id: string };
        setServices((prev) => prev.filter((p) => p.id !== id));
      },
      alert: (data) => setAlerts((prev) => [data as Alert, ...prev]),
    });

    // Slow poll as fallback for overview KPIs.
    const t = setInterval(() => api.overview().then(setOverview).catch(() => {}), 5000);
    return () => { off(); clearInterval(t); };
  }, [authed]);

  const openAlerts = alerts.filter((a) => !a.resolved);

  if (authed === null) return null;
  if (!authed) return <AuthScreen onAuthed={() => setAuthed(true)} />;

  const navItems: { key: Tab; label: string; icon: React.ReactNode; count?: number }[] = [
    { key: 'overview', label: 'Cluster Overview', icon: <LayoutGrid size={16} /> },
    { key: 'nodes', label: 'Nodes & Hardware', icon: <Cpu size={16} />, count: nodes.length },
    { key: 'services', label: 'Apps & Services', icon: <Boxes size={16} />, count: services.length },
    { key: 'network', label: 'Network & Ingress', icon: <Network size={16} /> },
    { key: 'storage', label: 'Storage', icon: <HardDrive size={16} /> },
    { key: 'alerts', label: 'Alerts', icon: <Bell size={16} />, count: openAlerts.length },
    { key: 'copilot', label: 'AI Co-Pilot', icon: <Bot size={16} /> },
  ];

  return (
    <>
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-icon">R</div>
          <div>
            <div className="brand-name">RackOps</div>
            <div className="brand-ver">LITE · v0.1.0</div>
          </div>
        </div>

        <button className="btn primary" style={{ margin: '0 4px 8px', justifyContent: 'center' }} onClick={() => setShowDeploy(true)}>
          <Plus size={15} /> Deploy
        </button>

        <div className="nav-section">Infrastructure</div>
        {navItems.map((n) => (
          <button key={n.key} className={`nav-item ${tab === n.key ? 'active' : ''}`} onClick={() => setTab(n.key)}>
            {n.icon} {n.label}
            {n.count !== undefined && n.count > 0 && <span className="count">{n.count}</span>}
          </button>
        ))}

        <div className="sidebar-footer">
          <div className="dim" style={{ marginBottom: 10 }}>
            <span className="online-dot" />
            {overview?.nodesOnline ?? 0}/{overview?.nodesTotal ?? 0} nodes online
          </div>
          <button className="btn" style={{ width: '100%', justifyContent: 'center', marginBottom: 8 }} onClick={() => setShowOnboard(true)}>
            <Server size={14} /> Add Node
          </button>
          <div className="row">
            <button className="btn sm" style={{ flex: 1, justifyContent: 'center' }} onClick={() => setShowSettings(true)} title="Settings">
              <Settings size={13} />
            </button>
            <button className="btn sm" style={{ flex: 1, justifyContent: 'center' }} title="Log out"
              onClick={() => { clearToken(); setAuthed(false); }}>
              <LogOut size={13} />
            </button>
          </div>
        </div>
      </aside>

      <main className="main">
        {tab === 'overview' && <OverviewPage overview={overview} nodes={nodes} events={events} services={services} />}
        {tab === 'nodes' && <NodesPage nodes={nodes} services={services} />}
        {tab === 'services' && (
          <ServicesPage
            services={services} nodes={nodes} projects={projects} envs={envs}
            onDeploy={() => setShowDeploy(true)} onChanged={refresh}
          />
        )}
        {tab === 'network' && <NetworkPage nodes={nodes} services={services} onChanged={refresh} />}
        {tab === 'storage' && <StoragePage nodes={nodes} />}
        {tab === 'alerts' && <AlertsPage alerts={alerts} nodes={nodes} />}
        {tab === 'copilot' && <CopilotPage overview={overview} />}
      </main>

      {showDeploy && (
        <DeployModal
          nodes={nodes} projects={projects} envs={envs}
          onClose={() => setShowDeploy(false)}
          onDeployed={() => { setShowDeploy(false); setTab('services'); refresh(); }}
        />
      )}
      {showOnboard && <OnboardModal onClose={() => setShowOnboard(false)} />}
      {showSettings && <SettingsModal onClose={() => setShowSettings(false)} />}
    </>
  );
}

export function StatusBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    running: 'green', online: 'green', building: 'yellow', pending: 'yellow',
    stopped: 'gray', offline: 'red', failed: 'red',
  };
  return (
    <span className={`badge ${map[status] ?? 'gray'}`}>
      <Activity size={11} /> {status}
    </span>
  );
}
