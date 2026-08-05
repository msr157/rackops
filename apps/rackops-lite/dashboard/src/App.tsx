import { useEffect, useRef, useState } from 'react';
import { Routes, Route, NavLink, Navigate, useNavigate, useLocation } from 'react-router-dom';
import {
  LayoutGrid, Boxes, HardDrive, Network as NetworkIcon, Settings, LogOut, Bot,
  UserRound, LifeBuoy, CloudOff, RotateCw, Bell, Sparkles, Activity,
  AlertTriangle, AlertCircle, PanelLeft, CircleHelp, X,
} from 'lucide-react';
import { api, getToken, clearToken, getActiveOrg } from './api';
import type { VersionInfo, User } from './api';
import { DataProvider, useData } from './data';
import { ToastProvider } from './components/toast';
import { ConfirmProvider } from './components/confirm';
import { CopilotProvider, useCopilot } from './components/CopilotDrawer';
import { CommandPalette } from './components/CommandPalette';
import { AddMachineModal } from './components/AddMachineModal';
import { OrgSwitcher } from './components/OrgSwitcher';
import { BrandMark, Led, UsageBar, timeAgo } from './components/ui';
import { AuthScreen } from './pages/Auth';
import { InvitationPage } from './pages/Invitation';
import { HomePage } from './pages/Home';
import { AppsPage } from './pages/Apps';
import { AppDetail } from './pages/AppDetail';
import { DeployFlow } from './pages/DeployFlow';
import { MachinesPage, MachineDetail } from './pages/Machines';
import { NetworkPage } from './pages/Network';
import { CopilotPage } from './pages/Copilot';
import { SettingsPage } from './pages/SettingsPage';
import { ProfilePage } from './pages/Profile';
import { SupportPage } from './pages/Support';
import { ChangelogPage } from './pages/Changelog';
import { ActivityPage } from './pages/Activity';

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null);
  // Remount the whole authed shell when the active org changes: every provider
  // refetches through the new X-Rackops-Org header. No stale cross-org state.
  const [orgKey, setOrgKey] = useState(() => getActiveOrg());
  const navigate = useNavigate();

  // Auth gate: first-run setup or an existing session decides the screen.
  useEffect(() => {
    const onLogout = () => setAuthed(false);
    // Route reset on switch: a deep link like /apps/:id points at a resource
    // that does not exist in the new org, so land on Home instead.
    const onOrg = () => { setOrgKey(getActiveOrg()); navigate('/', { replace: true }); };
    window.addEventListener('rackops-logout', onLogout);
    window.addEventListener('rackops-org-changed', onOrg);
    api.authStatus()
      .then((s) => setAuthed(s.needsSetup ? false : !!getToken()))
      .catch(() => setAuthed(!!getToken()));
    return () => {
      window.removeEventListener('rackops-logout', onLogout);
      window.removeEventListener('rackops-org-changed', onOrg);
    };
  }, [navigate]);

  // Invitation links work signed-out or signed-in — before the auth gate.
  if (window.location.pathname === '/invitation') {
    return <ToastProvider><InvitationPage /></ToastProvider>;
  }

  if (authed === null) return <BootScreen />;
  if (!authed) return <AuthScreen onAuthed={() => setAuthed(true)} />;

  return (
    <ToastProvider>
      <ConfirmProvider>
        <DataProvider key={orgKey}>
          <CopilotProvider>
            <Shell onLogout={() => { clearToken(); setAuthed(false); }} />
          </CopilotProvider>
        </DataProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

// BootScreen fills the gap between load and the auth answer — the blank
// frame it replaced said nothing at all.
function BootScreen() {
  return (
    <div className="boot" role="status">
      <div className="brand-mark" style={{ width: 34, height: 34 }}><BrandMark size={18} /></div>
      <div className="boot-name">RackOps</div>
      <div className="faint">Starting up…</div>
    </div>
  );
}

// ConnectionBanner is the difference between "you have nothing" and "we can't
// see your rack right now". A full-width strip directly under the header;
// it outranks everything and stays until the poll gets an answer back.
function ConnectionBanner() {
  const { reachable, lastSync, refresh } = useData();
  if (reachable) return null;
  return (
    <div className="offline-strip" role="alert">
      <CloudOff size={16} style={{ color: 'var(--crit)', flexShrink: 0 }} aria-hidden />
      <span style={{ flex: 1 }}>
        <strong>Can&rsquo;t reach the RackOps server</strong>
        <span className="dim">
          {'. Retrying every few seconds. '}
          {lastSync
            ? `Showing the last known state from ${timeAgo(lastSync)}; your apps keep running.`
            : 'Nothing has loaded yet, so this page may be blank.'}
        </span>
      </span>
      <button className="btn sm" onClick={refresh}><RotateCw size={12} /> Retry now</button>
    </div>
  );
}

// UpdateStrip: the one green band in the chrome. Dismissing it (per version,
// per session) moves the signal into the sidebar fleet card.
function UpdateStrip({ version, onDismiss }: { version: VersionInfo; onDismiss: () => void }) {
  const latest = version.latest.replace(/^v/, '');
  return (
    <div className="update-strip">
      <span className="strip-pill">NEW</span>
      <span>RackOps v{latest} is available.</span>
      <NavLink to="/changelog">See what&rsquo;s new</NavLink>
      <button className="strip-close" onClick={onDismiss} aria-label="Dismiss update notice">
        <X size={14} />
      </button>
    </div>
  );
}

function stripKey(v: VersionInfo) { return `rackops-update-strip:${v.latest}`; }

function Shell({ onLogout }: { onLogout: () => void }) {
  const { overview, services, nodes, alerts, loaded } = useData();
  const { setOpen: setCopilotOpen } = useCopilot();
  const { pathname } = useLocation();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [addMachine, setAddMachine] = useState(false);
  const [railHidden, setRailHidden] = useState(false);
  const [version, setVersion] = useState<VersionInfo | null>(null);
  const [me, setMe] = useState<User | null>(null);
  const [stripDismissed, setStripDismissed] = useState(false);

  useEffect(() => {
    api.versionCheck().then((v) => {
      setVersion(v);
      try { setStripDismissed(!!v && sessionStorage.getItem(stripKey(v)) === '1'); } catch { /* private mode */ }
    }).catch(() => {});
    api.me().then(setMe).catch(() => {});
  }, []);

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
  const machinesOffline = nodes.filter((n) => n.status === 'offline').length;
  const attention = failing + openAlerts + machinesOffline;

  const updateAvailable = !!version?.updateAvailable;
  const showStrip = updateAvailable && !stripDismissed;
  const dismissStrip = () => {
    if (version) { try { sessionStorage.setItem(stripKey(version), '1'); } catch { /* private mode */ } }
    setStripDismissed(true);
  };

  // Only attention is badged: a number in the rail always means "needs your
  // attention", never an inventory total the page itself already shows.
  type NavItem = { to: string; label: string; icon: React.ReactNode; count?: number; attn?: boolean; end?: boolean };
  const navOperate: NavItem[] = [
    { to: '/', label: 'Home', icon: <LayoutGrid size={16} />, count: attention, attn: true, end: true },
    { to: '/apps', label: 'Apps', icon: <Boxes size={16} /> },
    { to: '/machines', label: 'Machines', icon: <HardDrive size={16} /> },
    { to: '/network', label: 'Network', icon: <NetworkIcon size={16} /> },
    { to: '/activity', label: 'Activity', icon: <Activity size={16} /> },
    { to: '/copilot', label: 'Copilot', icon: <Bot size={16} /> },
  ];
  const navWorkspace: NavItem[] = [
    { to: '/settings', label: 'Settings', icon: <Settings size={16} /> },
    { to: '/support', label: 'Support', icon: <LifeBuoy size={16} /> },
    { to: '/changelog', label: "What's new", icon: <Sparkles size={16} /> },
  ];
  const renderNav = (items: NavItem[]) => items.map((n) => (
    <NavLink key={n.to} to={n.to} end={n.end} aria-label={n.label}
      className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
      {n.icon} <span className="nav-label">{n.label}</span>
      {n.count !== undefined && n.count > 0 && (
        <span className={`count${n.attn ? ' attn' : ''}`}>{n.count}</span>
      )}
    </NavLink>
  ));

  const memPct = overview && overview.totalMemMb > 0
    ? (overview.usedMemMb / overview.totalMemMb) * 100 : 0;

  return (
    <>
      {showStrip && version && <UpdateStrip version={version} onDismiss={dismissStrip} />}

      <header className="topnav">
        <NavLink to="/" className="topnav-brand" aria-label="RackOps home">
          <span className="brand-mark" aria-hidden><BrandMark /></span>
          <span className="brand-name">RackOps</span>
          <span className="lite-tag">LITE</span>
        </NavLink>
        <button className="icon-sq quiet" onClick={() => setRailHidden((v) => !v)}
          aria-label={railHidden ? 'Show sidebar' : 'Hide sidebar'}
          title={railHidden ? 'Show sidebar' : 'Hide sidebar'}
          aria-pressed={railHidden}>
          <PanelLeft size={15} />
        </button>
        <div className="topnav-right">
          <NavLink to="/support" className="icon-sq quiet" aria-label="Support" title="Support">
            <CircleHelp size={16} />
          </NavLink>
          <NotificationBell />
          <AccountMenu me={me} onLogout={onLogout} />
        </div>
      </header>

      <ConnectionBanner />

      <div className="app-body">
        {!railHidden && (
          <aside className="sidebar">
            <OrgSwitcher me={me} />

            {/* aria-label carries the name at narrow widths, where the visible
                label is hidden and the item would otherwise be an unnamed icon. */}
            <div className="nav-section">Operate</div>
            {renderNav(navOperate)}
            <div className="nav-section">Workspace</div>
            {renderNav(navWorkspace)}

            <div className="sidebar-footer">
              {/* Before anything has loaded there is no count to report — "0/0"
                  would be an assertion the dashboard cannot yet make. */}
              <div className="fleet-card">
                <div className="fleet-line">
                  <Led status={!loaded ? 'pending'
                    : overview && overview.nodesOnline === overview.nodesTotal && overview.nodesTotal > 0 ? 'running'
                    : overview?.nodesOnline ? 'building' : 'stopped'} />
                  <span>
                    {loaded && overview ? `${overview.nodesOnline}/${overview.nodesTotal} machines online` : 'checking machines…'}
                  </span>
                </div>
                {loaded && overview && overview.totalMemMb > 0 && (
                  <div>
                    <span className="faint">
                      {(overview.usedMemMb / 1024).toFixed(0)}/{(overview.totalMemMb / 1024).toFixed(0)} GB memory in use
                    </span>
                    <UsageBar pct={memPct} />
                  </div>
                )}
                <span className="fleet-ver">{version ? `v${version.current}` : ''}</span>
                {updateAvailable && stripDismissed && (
                  <NavLink to="/changelog" className="btn sm primary" style={{ justifyContent: 'center' }}>
                    Update available
                  </NavLink>
                )}
              </div>
            </div>
          </aside>
        )}

        <main className="main">
          <Routes>
            <Route path="/" element={<HomePage onAddMachine={() => setAddMachine(true)} />} />
            <Route path="/apps" element={<AppsPage />} />
            <Route path="/apps/new" element={<DeployFlow />} />
            <Route path="/apps/new/launch/:serviceId" element={<DeployFlow />} />
            <Route path="/apps/:id/:tab?" element={<AppDetail />} />
            <Route path="/machines" element={<MachinesPage onAddMachine={() => setAddMachine(true)} />} />
            <Route path="/machines/:id" element={<MachineDetail />} />
            <Route path="/network" element={<NetworkPage />} />
            <Route path="/activity" element={<ActivityPage />} />
            <Route path="/copilot" element={<CopilotPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="/profile" element={<ProfilePage />} />
            <Route path="/support" element={<SupportPage />} />
            <Route path="/changelog" element={<ChangelogPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>

      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)}
        onAddMachine={() => setAddMachine(true)}
        onCopilot={() => { if (pathname !== '/copilot') setCopilotOpen(true); }} />
      {addMachine && <AddMachineModal onClose={() => setAddMachine(false)} />}
    </>
  );
}

// NotificationBell: open alerts plus the recent activity stream, one click
// from the fix. The badge counts only open alerts — it never counts routine
// activity, so a number on the bell always means something is wrong.
function NotificationBell() {
  const { alerts, events } = useData();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { setOpen(false); triggerRef.current?.focus(); }
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const openAlerts = alerts.filter((a) => !a.resolved);
  const recent = events.slice(0, 8);

  return (
    <div className="bell" ref={boxRef}>
      <button ref={triggerRef} className="bell-btn" onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog" aria-expanded={open} title="Notifications"
        aria-label={openAlerts.length > 0 ? `Notifications: ${openAlerts.length} open alert${openAlerts.length > 1 ? 's' : ''}` : 'Notifications'}>
        <Bell size={16} aria-hidden />
        {openAlerts.length > 0 && <span className="bell-count" aria-hidden>{openAlerts.length > 9 ? '9+' : openAlerts.length}</span>}
      </button>
      {open && (
        <div className="bell-pop" role="dialog" aria-label="Notifications">
          <div className="bell-head">
            Notifications
            {openAlerts.length > 0 && <span className="pill crit">{openAlerts.length} open</span>}
          </div>
          <div className="bell-list">
            {openAlerts.map((a) => (
              <button key={a.id} className="bell-item"
                onClick={() => { setOpen(false); if (a.nodeId) navigate(`/machines/${a.nodeId}`); else navigate('/'); }}>
                {a.level === 'critical'
                  ? <AlertCircle size={14} style={{ color: 'var(--crit)', flexShrink: 0, marginTop: 2 }} aria-hidden />
                  : <AlertTriangle size={14} style={{ color: 'var(--warn)', flexShrink: 0, marginTop: 2 }} aria-hidden />}
                <span style={{ flex: 1 }}>
                  {a.message}
                  <span className="faint">{timeAgo(a.createdAt)}</span>
                </span>
              </button>
            ))}
            {openAlerts.length === 0 && recent.length === 0 && (
              <div className="bell-empty">Nothing needs your attention.</div>
            )}
            {recent.length > 0 && (
              <>
                <div className="org-menu-label">Recent activity</div>
                {recent.map((e) => (
                  <div key={e.id} className="bell-item static">
                    {/* neutral dot: activity is information, not status */}
                    <span className="led off" style={{ marginTop: 5 }} aria-hidden />
                    <span style={{ flex: 1 }}>
                      {e.message}
                      <span className="faint">{timeAgo(e.createdAt)}</span>
                    </span>
                  </div>
                ))}
                <button className="bell-item" onClick={() => { setOpen(false); navigate('/activity'); }}>
                  <Activity size={14} style={{ flexShrink: 0, marginTop: 2 }} aria-hidden />
                  <span style={{ flex: 1 }}>View all activity</span>
                </button>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// AccountMenu: the avatar at the header's right edge. Log out lives inside
// the menu, so ending the session is a deliberate two-step, never a misclick.
function AccountMenu({ me, onLogout }: { me: User | null; onLogout: () => void }) {
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { setOpen(false); triggerRef.current?.focus(); }
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const name = me?.fullName || me?.email || 'Account';
  const initial = (me?.fullName || me?.email || '?').trim().charAt(0).toUpperCase();

  return (
    <div className="account" ref={boxRef}>
      <button ref={triggerRef} className="avatar-btn" onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu" aria-expanded={open} aria-label={`Account: ${name}`} title={name}>
        {initial}
      </button>
      {open && (
        <div className="org-menu right" role="menu">
          <div className="org-menu-label">
            {name}
            {!!me?.fullName && !!me?.email && <span style={{ display: 'block' }} className="faint">{me.email}</span>}
          </div>
          <NavLink to="/profile" role="menuitem" className="org-item" onClick={() => setOpen(false)}>
            <UserRound size={14} /> Profile
          </NavLink>
          <div className="org-menu-sep" />
          <button role="menuitem" className="org-item" onClick={onLogout}>
            <LogOut size={14} /> Log out
          </button>
        </div>
      )}
    </div>
  );
}
