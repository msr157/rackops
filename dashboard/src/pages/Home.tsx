import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  Boxes, HardDrive, Radio, Bell, Rocket, Globe, ArrowRight, Check, ScrollText, Server,
} from 'lucide-react';
import { useData } from '../data';
import { Rack } from '../components/Rack';
import { InfoTip, Led, UrlChip, publicUrls, timeAgo } from '../components/ui';

// Home answers one question at a glance: is everything okay?
// Anything that needs attention comes first; healthy detail follows.
export function HomePage({ onAddMachine }: { onAddMachine: () => void }) {
  const { overview, nodes, services, events, alerts, ingress } = useData();
  const navigate = useNavigate();

  const failing = services.filter((s) => s.status === 'failed');
  const openAlerts = alerts.filter((a) => !a.resolved);
  const attention = failing.length + openAlerts.length;
  const nodesOffline = nodes.filter((n) => n.status === 'offline');

  const running = services.filter((s) => s.status === 'running').length;

  return (
    <div className="page">
      {/* Status banner: the one-glance answer */}
      <div className="hero-banner">
        <Led lg status={attention > 0 || nodesOffline.length > 0 ? 'failed' : nodes.length === 0 ? 'stopped' : 'running'} />
        <div style={{ flex: 1 }}>
          <div style={{ fontFamily: 'var(--display)', fontWeight: 700, fontSize: 17, letterSpacing: '-0.01em' }}>
            {nodes.length === 0 ? 'Your rack is waiting for its first machine'
              : attention > 0 ? `${attention} thing${attention > 1 ? 's' : ''} need${attention > 1 ? '' : 's'} your attention`
              : 'All systems operational'}
          </div>
          <div className="dim" style={{ marginTop: 2 }}>
            {overview
              ? `${overview.nodesOnline}/${overview.nodesTotal} machines online · ${running}/${services.length} apps running · fleet CPU ${overview.avgCpuPct}%`
              : 'connecting…'}
          </div>
        </div>
        {services.length > 0 && (
          <Link to="/apps" className="btn">Open apps <ArrowRight size={13} /></Link>
        )}
      </div>

      <SetupChecklist onAddMachine={onAddMachine} />

      {/* Needs attention: failures and alerts, each one click from its fix */}
      {(failing.length > 0 || openAlerts.length > 0 || nodesOffline.length > 0) && (
        <>
          <div className="section-title">
            <Bell size={15} style={{ color: 'var(--crit)' }} /> Needs attention
            <InfoTip text="Only live problems appear here — a failing app, an offline machine, or a sustained alert. Entries disappear automatically the moment the issue clears; nothing needs manual dismissing." wide />
          </div>
          <div className="stack" style={{ marginBottom: 8 }}>
            {failing.map((s) => (
              <div key={s.id} className="card row spread" style={{ borderColor: 'var(--crit)', padding: '12px 16px' }}>
                <div className="row">
                  <Led status="failed" />
                  <strong>{s.name}</strong>
                  <span className="dim">app is down{s.status === 'failed' ? ' — auto-heal is retrying' : ''}</span>
                </div>
                <div className="row">
                  <button className="btn sm" onClick={() => navigate(`/apps/${s.id}/logs`)}><ScrollText size={12} /> View logs</button>
                  <button className="btn sm" onClick={() => navigate(`/apps/${s.id}/deployments`)}>Deployments</button>
                </div>
              </div>
            ))}
            {nodesOffline.map((n) => (
              <div key={n.id} className="card row spread" style={{ borderColor: 'var(--crit)', padding: '12px 16px' }}>
                <div className="row">
                  <Led status="offline" />
                  <strong>{n.name}</strong>
                  <span className="dim">machine stopped reporting — check power and network, or restart its agent</span>
                </div>
                <button className="btn sm" onClick={() => navigate(`/machines/${n.id}`)}>Inspect</button>
              </div>
            ))}
            {openAlerts.map((a) => (
              <div key={a.id} className="card row spread" style={{ borderColor: a.level === 'critical' ? 'var(--crit)' : 'var(--warn)', padding: '12px 16px' }}>
                <div className="row">
                  <Led status={a.level === 'critical' ? 'failed' : 'pending'} />
                  <span>{a.message}</span>
                </div>
                <span className="faint">{timeAgo(a.createdAt)}</span>
              </div>
            ))}
          </div>
        </>
      )}

      <div className="grid-2" style={{ alignItems: 'start', marginTop: 10 }}>
        <div>
          <div className="section-title" style={{ marginTop: 12 }}>
            <Boxes size={15} /> Your apps
            {services.length > 0 && <Link to="/apps" className="faint" style={{ marginLeft: 'auto', fontWeight: 400 }}>all {services.length} →</Link>}
          </div>
          <div className="stack" style={{ gap: 8 }}>
            {services.slice(0, 6).map((s) => {
              const urls = publicUrls(s, ingress);
              return (
                <div key={s.id} className="card clickable row" style={{ padding: '11px 14px' }} onClick={() => navigate(`/apps/${s.id}`)}>
                  <Led status={s.status} />
                  <strong style={{ minWidth: 90 }}>{s.name}</strong>
                  {urls.length > 0
                    ? <UrlChip url={urls[0].url} />
                    : <span className="faint">not public yet</span>}
                  <span className="faint mono" style={{ marginLeft: 'auto' }}>{s.status === 'running' ? `${s.cpuPct.toFixed(0)}% cpu` : s.status}</span>
                </div>
              );
            })}
            {services.length === 0 && nodes.length > 0 && (
              <div className="empty">
                <Rocket size={22} style={{ marginBottom: 8 }} />
                <div className="empty-title">No apps yet</div>
                <div style={{ marginBottom: 14 }}>Deploy anything — Jellyfin, Postgres, your own code — in under a minute.</div>
                <button className="btn primary" onClick={() => navigate('/apps/new')}><Rocket size={13} /> Deploy your first app</button>
              </div>
            )}
            {nodes.length === 0 && (
              <div className="empty">
                <Server size={22} style={{ marginBottom: 8 }} />
                <div className="empty-title">Apps run on machines</div>
                <div>Add a machine first — then deploy onto it.</div>
              </div>
            )}
          </div>

          <div className="section-title"><Radio size={15} /> Live activity <span className="badge amber" style={{ marginLeft: 'auto' }}>LIVE</span></div>
          <div className="terminal" style={{ maxHeight: 260 }}>
            {events.map((e) => (
              <div key={e.id}>
                <span className="t-time">{new Date(e.createdAt).toLocaleTimeString()}</span>
                <span className="t-kind">[{e.kind}]</span>
                {e.message}
              </div>
            ))}
            {events.length === 0 && <span className="faint">quiet so far — activity from deploys, heals, and machines appears here</span>}
          </div>
        </div>

        <div>
          <div className="section-title" style={{ marginTop: 12 }}>
            <HardDrive size={15} /> The rack
            <InfoTip text="Every machine in your fleet, drawn as a rack. Green LED = online and reporting; the bars show live CPU and memory." />
            <Link to="/machines" className="faint" style={{ marginLeft: 'auto', fontWeight: 400 }}>details →</Link>
          </div>
          <Rack nodes={nodes} services={services} />

          {overview && nodes.length > 0 && (
            <div className="kpi-row" style={{ marginTop: 14 }}>
              <div className="card">
                <div className="eyebrow">Fleet memory</div>
                <div className="kpi-value">{((overview.usedMemMb) / 1024).toFixed(1)} <span className="unit">/ {(overview.totalMemMb / 1024).toFixed(0)} GB</span></div>
              </div>
              <div className="card">
                <div className="eyebrow">Pooled storage</div>
                <div className="kpi-value">{overview.usedStorageGb.toFixed(0)} <span className="unit">/ {overview.totalStorageGb.toFixed(0)} GB</span></div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// SetupChecklist guides the first-run journey: machine → app → public URL.
// It ticks off live via SSE updates and disappears once complete (or skipped).
function SetupChecklist({ onAddMachine }: { onAddMachine: () => void }) {
  const { nodes, services, ingress } = useData();
  const navigate = useNavigate();
  const [dismissed, setDismissed] = useState(() => localStorage.getItem('rackops-setup-done') === '1');

  const hasMachine = nodes.length > 0;
  const hasApp = services.length > 0;
  const hasPublic = services.some((s) => publicUrls(s, ingress).length > 0);
  const allDone = hasMachine && hasApp && hasPublic;

  if (dismissed || allDone) return null;

  const firstApp = services[0];
  const steps = [
    {
      done: hasMachine, title: 'Add a machine',
      desc: 'Run one command on any Linux box — an old PC, a Pi, a VPS.',
      action: 'Show command', run: onAddMachine,
    },
    {
      done: hasApp, title: 'Deploy your first app',
      desc: 'Pick from 45+ templates, or bring an image, git repo, or compose file.',
      action: 'Deploy', run: () => navigate('/apps/new'),
    },
    {
      done: hasPublic, title: 'Make it public',
      desc: 'Get a URL you can open from anywhere — instant, tunnel, or your own domain.',
      action: firstApp ? 'Choose a URL' : undefined,
      run: firstApp ? () => navigate(`/apps/${firstApp.id}/networking`) : undefined,
    },
  ];

  return (
    <div className="card" style={{ marginBottom: 20 }}>
      <div className="row spread" style={{ marginBottom: 12 }}>
        <div style={{ fontFamily: 'var(--display)', fontWeight: 600, fontSize: 15 }}>Get your rack online</div>
        <button className="btn sm ghost" onClick={() => { localStorage.setItem('rackops-setup-done', '1'); setDismissed(true); }}>
          Skip setup
        </button>
      </div>
      <div className="stack" style={{ gap: 8 }}>
        {steps.map((s, i) => (
          <div key={i} className={`setup-step ${s.done ? 'done' : ''}`}>
            <div className="setup-n">{s.done ? <Check size={13} /> : i + 1}</div>
            <div style={{ flex: 1 }}>
              <div style={{ fontWeight: 600 }}>{s.title}</div>
              <div className="dim">{s.desc}</div>
            </div>
            {!s.done && s.action && s.run && (
              <button className={`btn sm ${i === steps.findIndex((x) => !x.done) ? 'primary' : ''}`} onClick={s.run}>
                {s.action === 'Choose a URL' && <Globe size={12} />}
                {s.action}
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
