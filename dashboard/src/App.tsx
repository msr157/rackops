import { useEffect, useState } from 'react';
import { Routes, Route, NavLink, Navigate, useNavigate } from 'react-router-dom';
import {
  LayoutGrid, Boxes, HardDrive, Network as NetworkIcon, Settings, LogOut, Rocket, Bot, Search, Plus,
} from 'lucide-react';
import { api, getToken, clearToken } from './api';
import { DataProvider, useData } from './data';
import { ToastProvider } from './components/toast';
import { ConfirmProvider } from './components/confirm';
import { CopilotProvider, useCopilot } from './components/CopilotDrawer';
import { CommandPalette } from './components/CommandPalette';
import { AddMachineModal } from './components/AddMachineModal';
import { Led } from './components/ui';
import { AuthScreen } from './pages/Auth';
import { HomePage } from './pages/Home';
import { AppsPage } from './pages/Apps';
import { AppDetail } from './pages/AppDetail';
import { DeployFlow } from './pages/DeployFlow';
import { MachinesPage, MachineDetail } from './pages/Machines';
import { NetworkPage } from './pages/Network';
import { SettingsPage } from './pages/SettingsPage';

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null);

  // Auth gate: first-run setup or an existing session decides the screen.
  useEffect(() => {
    const onLogout = () => setAuthed(false);
    window.addEventListener('rackops-logout', onLogout);
    api.authStatus()
      .then((s) => setAuthed(s.needsSetup ? false : !!getToken()))
      .catch(() => setAuthed(!!getToken()));
    return () => window.removeEventListener('rackops-logout', onLogout);
  }, []);

  if (authed === null) return null;
  if (!authed) return <AuthScreen onAuthed={() => setAuthed(true)} />;

  return (
    <ToastProvider>
      <ConfirmProvider>
        <DataProvider>
          <CopilotProvider>
            <Shell onLogout={() => { clearToken(); setAuthed(false); }} />
          </CopilotProvider>
        </DataProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function Shell({ onLogout }: { onLogout: () => void }) {
  const { overview, services, nodes, alerts } = useData();
  const { setOpen: setCopilotOpen } = useCopilot();
  const navigate = useNavigate();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [addMachine, setAddMachine] = useState(false);

  // ⌘K / Ctrl+K opens the palette from anywhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const failing = services.filter((s) => s.status === 'failed').length;
  const openAlerts = alerts.filter((a) => !a.resolved).length;
  const attention = failing + openAlerts;
  const fleetStatus = nodes.some((n) => n.status === 'offline') || failing > 0 ? 'failed'
    : nodes.length === 0 ? 'stopped' : 'running';

  const nav = [
    { to: '/', label: 'Home', icon: <LayoutGrid size={16} />, count: attention, end: true },
    { to: '/apps', label: 'Apps', icon: <Boxes size={16} />, count: services.length },
    { to: '/machines', label: 'Machines', icon: <HardDrive size={16} />, count: nodes.length },
    { to: '/network', label: 'Network', icon: <NetworkIcon size={16} /> },
  ];

  return (
    <>
      <aside className="sidebar">
        <NavLink to="/" className="brand">
          <div className="brand-unit"><Led status={fleetStatus} /></div>
          <div>
            <div className="brand-name">RackOps</div>
            <div className="brand-ver">LITE · v0.1.0</div>
          </div>
        </NavLink>

        <div className="nav-section">Your rack</div>
        {nav.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end}
            className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
            {n.icon} <span className="nav-label">{n.label}</span>
            {n.count !== undefined && n.count > 0 && <span className="count">{n.count}</span>}
          </NavLink>
        ))}

        <div className="sidebar-footer">
          <div className="dim row" style={{ gap: 8, padding: '0 6px' }}>
            <Led status={overview && overview.nodesOnline === overview.nodesTotal && overview.nodesTotal > 0 ? 'running' : overview?.nodesOnline ? 'building' : 'stopped'} />
            <span className="nav-label">{overview?.nodesOnline ?? 0}/{overview?.nodesTotal ?? 0} machines online</span>
          </div>
          <button className="btn" onClick={() => setAddMachine(true)}>
            <Plus size={14} /> <span className="nav-label">Add machine</span>
          </button>
          <div className="row">
            <NavLink to="/settings" className="btn sm" style={{ flex: 1 }} title="Settings">
              <Settings size={13} />
            </NavLink>
            <button className="btn sm" style={{ flex: 1 }} title="Log out" onClick={onLogout}>
              <LogOut size={13} />
            </button>
          </div>
        </div>
      </aside>

      <div className="shell-right">
        <header className="topbar">
          <button className="search-trigger" onClick={() => setPaletteOpen(true)}>
            <Search size={14} /> Jump to app, machine, action…
            <kbd>⌘K</kbd>
          </button>
          <div style={{ marginLeft: 'auto' }} className="row">
            <button className="btn" onClick={() => setCopilotOpen(true)}>
              <Bot size={14} /> Copilot
            </button>
            <button className="btn primary" onClick={() => navigate('/apps/new')}>
              <Rocket size={14} /> Deploy app
            </button>
          </div>
        </header>

        <main className="main">
          <Routes>
            <Route path="/" element={<HomePage onAddMachine={() => setAddMachine(true)} />} />
            <Route path="/apps" element={<AppsPage />} />
            <Route path="/apps/new" element={<DeployFlow />} />
            <Route path="/apps/:id/:tab?" element={<AppDetail />} />
            <Route path="/machines" element={<MachinesPage onAddMachine={() => setAddMachine(true)} />} />
            <Route path="/machines/:id" element={<MachineDetail />} />
            <Route path="/network" element={<NetworkPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>

      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)}
        onAddMachine={() => setAddMachine(true)} onCopilot={() => setCopilotOpen(true)} />
      {addMachine && <AddMachineModal onClose={() => setAddMachine(false)} />}
    </>
  );
}
