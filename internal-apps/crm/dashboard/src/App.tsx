import { useEffect, useState } from 'react';
import { Routes, Route, NavLink, Navigate } from 'react-router-dom';
import { LayoutGrid, Server, Inbox, Globe, LogOut } from 'lucide-react';
import { api, getToken, clearToken } from './api';
import type { Overview } from './api';
import { AuthScreen } from './pages/Auth';
import { OverviewPage } from './pages/Overview';
import { InstallsPage, InstallDetailPage } from './pages/Installs';
import { TicketsPage, TicketDetailPage } from './pages/Tickets';
import { FrontDoorPage } from './pages/FrontDoor';

export default function App() {
  const [authed, setAuthed] = useState<boolean>(() => !!getToken());

  useEffect(() => {
    const onLogout = () => setAuthed(false);
    window.addEventListener('crm-logout', onLogout);
    return () => window.removeEventListener('crm-logout', onLogout);
  }, []);

  if (!authed) return <AuthScreen onAuthed={() => setAuthed(true)} />;
  return <Shell onLogout={() => { clearToken(); setAuthed(false); }} />;
}

function Shell({ onLogout }: { onLogout: () => void }) {
  const [overview, setOverview] = useState<Overview | null>(null);

  // Keep the sidebar counters live alongside the 10s ticket polling.
  useEffect(() => {
    let alive = true;
    const load = () => api.overview().then((o) => { if (alive) setOverview(o); }).catch(() => {});
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, []);

  const nav = [
    { to: '/', label: 'Overview', icon: <LayoutGrid size={16} />, end: true },
    { to: '/installs', label: 'Installs', icon: <Server size={16} />, count: overview?.installs },
    { to: '/tickets', label: 'Tickets', icon: <Inbox size={16} />, count: overview?.openTickets },
    { to: '/frontdoor', label: 'Front Door', icon: <Globe size={16} /> },
  ];

  return (
    <>
      <aside className="sidebar">
        <NavLink to="/" className="brand">
          <div className="brand-unit"><Led ok={(overview?.openTickets ?? 0) === 0} /></div>
          <div>
            <div className="brand-name">RackOps Admin</div>
            <div className="brand-ver">support · installs · tunnel</div>
          </div>
        </NavLink>

        <div className="nav-section">Operations</div>
        {nav.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end}
            className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
            {n.icon} <span className="nav-label">{n.label}</span>
            {n.count !== undefined && n.count > 0 && <span className="count">{n.count}</span>}
          </NavLink>
        ))}

        <div className="sidebar-footer">
          <div className="dim row" style={{ gap: 8, padding: '0 6px' }}>
            <span className={`led ${(overview?.activeWeek ?? 0) > 0 ? 'ok' : 'off'}`} />
            <span className="nav-label">{overview?.activeWeek ?? 0} active this week</span>
          </div>
          <button className="btn sm" onClick={onLogout}>
            <LogOut size={13} /> <span className="nav-label">Sign out</span>
          </button>
        </div>
      </aside>

      <div className="shell-right">
        <main className="main">
          <Routes>
            <Route path="/" element={<OverviewPage />} />
            <Route path="/installs" element={<InstallsPage />} />
            <Route path="/installs/:id" element={<InstallDetailPage />} />
            <Route path="/tickets" element={<TicketsPage />} />
            <Route path="/tickets/:installId/:id" element={<TicketDetailPage />} />
            <Route path="/frontdoor" element={<FrontDoorPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>
    </>
  );
}

function Led({ ok }: { ok: boolean }) {
  return <span className={`led ${ok ? 'ok' : 'warn'}`} />;
}
