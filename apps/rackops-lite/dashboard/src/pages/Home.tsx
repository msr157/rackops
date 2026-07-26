import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  Boxes, HardDrive, Radio, Bell, Rocket, Globe, Check, ScrollText,
} from 'lucide-react';
import { useData } from '../data';
import { Rack } from '../components/Rack';
import { EmptyState, InfoTip, Led, SkeletonRows, UrlChip, publicUrls, timeAgo } from '../components/ui';

// Home answers one question at a glance: is everything okay?
// Anything that needs attention comes first; healthy detail follows.
export function HomePage({ onAddMachine }: { onAddMachine: () => void }) {
  const { overview, nodes, services, events, alerts, ingress, loaded, reachable } = useData();
  const navigate = useNavigate();

  const failing = services.filter((s) => s.status === 'failed');
  const openAlerts = alerts.filter((a) => !a.resolved);
  const nodesOffline = nodes.filter((n) => n.status === 'offline');
  // An offline machine counts toward the headline: the banner must never
  // say "all systems operational" over a red attention list.
  const attention = failing.length + openAlerts.length + nodesOffline.length;
  const troubled = loaded && attention > 0;

  const running = services.filter((s) => s.status === 'running').length;

  return (
    <div className="page">
      {/* Status banner: the one-glance answer. It must never claim an empty
          rack while the fleet is still loading or the server is unreachable,
          because that reads as data loss at the exact moment it would panic
          someone. */}
      <div className={`status-banner ${troubled ? 'crit' : ''}`}>
        <Led lg label
          status={!loaded ? 'pending'
            : troubled ? 'failed'
            : nodes.length === 0 ? 'stopped' : 'running'} />
        <div style={{ flex: 1 }}>
          <div className="status-banner-title">
            {!loaded ? (reachable ? 'Reading the rack…' : 'Waiting for the server')
              : nodes.length === 0 ? 'Your rack is waiting for its first machine'
              : attention > 0 ? `${attention} thing${attention > 1 ? 's' : ''} need${attention > 1 ? '' : 's'} your attention`
              : 'All systems operational'}
          </div>
          <div className="dim" style={{ marginTop: 2 }}>
            {!loaded
              ? (reachable ? 'connecting…' : 'the control plane is not answering yet')
              : overview
                ? `${overview.nodesOnline}/${overview.nodesTotal} machines online, ${running}/${services.length} apps running · fleet CPU ${overview.avgCpuPct}%`
                : 'connecting…'}
          </div>
        </div>
        {/* The banner action follows the state it announces: incidents jump
            to the attention list, an empty rack starts onboarding, and
            all-clear needs no button at all. */}
        {troubled && (
          <a href="#attention" className="btn">Review {attention} issue{attention > 1 ? 's' : ''}</a>
        )}
        {loaded && !troubled && nodes.length === 0 && (
          <button className="btn primary" onClick={onAddMachine}>Add machine</button>
        )}
      </div>

      {/* Needs attention: failures and alerts, each one click from its fix */}
      {(failing.length > 0 || openAlerts.length > 0 || nodesOffline.length > 0) && (
        <>
          <div className="section-title" id="attention">
            <Bell size={15} style={{ color: 'var(--crit)' }} aria-hidden /> Needs attention
            <InfoTip wide>
              Only real, ongoing problems appear here: an app that is down, a machine that stopped
              reporting, or an alert that has persisted for several readings. Each entry clears
              itself automatically the moment the problem is fixed.
            </InfoTip>
          </div>
          <div className="stack" style={{ marginBottom: 8 }}>
            {failing.map((s) => (
              <div key={s.id} className="card crit row spread" style={{ padding: '12px 16px' }}>
                <div className="row">
                  <Led status="failed" />
                  <strong>{s.name}</strong>
                  <span className="dim">app is down, auto-heal is retrying</span>
                </div>
                <button className="btn sm primary" onClick={() => navigate(`/apps/${s.id}/logs`)}>
                  <ScrollText size={12} /> View logs
                </button>
              </div>
            ))}
            {nodesOffline.map((n) => (
              <div key={n.id} className="card crit row spread" style={{ padding: '12px 16px' }}>
                <div className="row">
                  <Led status="offline" />
                  <strong>{n.name}</strong>
                  <span className="dim">machine stopped reporting: check power and network, or restart its agent</span>
                </div>
                <button className="btn sm" onClick={() => navigate(`/machines/${n.id}`)}>View machine</button>
              </div>
            ))}
            {openAlerts.map((a) => (
              <div key={a.id} className={`card row spread ${a.level === 'critical' ? 'crit' : 'warn'}`}
                style={{ padding: '12px 16px' }}>
                <div className="row">
                  <Led status={a.level === 'critical' ? 'failed' : 'pending'} />
                  <span>{a.message}</span>
                  <span className="faint">{timeAgo(a.createdAt)}</span>
                </div>
                {a.nodeId && (
                  <button className="btn sm" onClick={() => navigate(`/machines/${a.nodeId}`)}>View machine</button>
                )}
              </div>
            ))}
          </div>
        </>
      )}

      {/* Onboarding never interrupts incidents: the checklist waits below the
          attention list and only renders when nothing needs attention. */}
      {attention === 0 && <SetupChecklist onAddMachine={onAddMachine} />}

      <div className="grid-2" style={{ alignItems: 'start', marginTop: 10 }}>
        <div>
          <div className="section-title" style={{ marginTop: 12 }}>
            <Boxes size={15} aria-hidden /> Your apps
            {services.length > 0 && <Link to="/apps" className="faint" style={{ marginLeft: 'auto', fontWeight: 400 }}>View all →</Link>}
          </div>
          <div className="stack" style={{ gap: 8 }}>
            {services.slice(0, 6).map((s) => {
              const urls = publicUrls(s, ingress);
              return (
                <div key={s.id} className="card row-card row" style={{ padding: '11px 14px' }}>
                  <Led status={s.status} label />
                  <Link to={`/apps/${s.id}`} className="row-link"
                    style={{ fontWeight: 600, minWidth: 90, maxWidth: 190, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {s.name}
                  </Link>
                  <span className="row-actions row" style={{ minWidth: 0 }}>
                    {urls.length > 0
                      ? <UrlChip url={urls[0].url} />
                      : <span className="faint">not public yet</span>}
                  </span>
                  <span className="faint mono" style={{ marginLeft: 'auto' }}>{s.status === 'running' ? `${s.cpuPct.toFixed(0)}% cpu` : s.status}</span>
                </div>
              );
            })}
            {!loaded && services.length === 0 && <SkeletonRows count={3} height={44} />}
            {loaded && services.length === 0 && nodes.length > 0 && (
              <EmptyState icon={<Rocket size={18} />} title="No apps yet">
                <div style={{ marginBottom: 14 }}>Deploy anything: Jellyfin, Postgres, your own code, in under a minute.</div>
                <button className="btn primary" onClick={() => navigate('/apps/new')}><Rocket size={13} /> Deploy your first app</button>
              </EmptyState>
            )}
          </div>

          <div className="section-title">
            <Radio size={15} aria-hidden /> Live activity
            <InfoTip>
              A running log of everything the fleet does: deploys, self-healing, machines joining,
              alerts raising and resolving. It updates in real time.
            </InfoTip>
            {/* The badge tracks the stream, not the section: claiming LIVE
                while the server is unreachable is the same lie in miniature. */}
            <span className={`badge ${reachable ? 'accent' : 'neutral'}`} style={{ marginLeft: 'auto' }}>
              {reachable ? 'live' : 'stalled'}
            </span>
          </div>
          <div className="terminal" style={{ maxHeight: 260 }}>
            {events.map((e) => (
              <div key={e.id}>
                <span className="t-time">{new Date(e.createdAt).toLocaleTimeString()}</span>
                <span className="t-kind">[{e.kind}]</span>
                {e.message}
              </div>
            ))}
            {events.length === 0 && (
              <span className="faint">
                {loaded
                  ? 'quiet so far: activity from deploys, heals, and machines appears here'
                  : 'waiting for the server…'}
              </span>
            )}
          </div>
        </div>

        <div>
          <div className="section-title" style={{ marginTop: 12 }}>
            <HardDrive size={15} aria-hidden /> The rack
            <InfoTip>
              Every machine in this organization, one slot per machine. The dot shows each
              machine&rsquo;s health; click through for full hardware detail.
            </InfoTip>
            <Link to="/machines" className="faint" style={{ marginLeft: 'auto', fontWeight: 400 }}>View all →</Link>
          </div>
          {/* No onAdd here: the status banner already carries the page's
              Add-machine primary; the rack panel stays quiet. */}
          <Rack nodes={nodes} services={services} loading={!loaded} />

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

// SetupChecklist guides the first-run journey: machine, then app, then public
// URL. It ticks off live via SSE updates and disappears once complete (or skipped).
function SetupChecklist({ onAddMachine }: { onAddMachine: () => void }) {
  const { nodes, services, ingress, loaded } = useData();
  const navigate = useNavigate();
  const [dismissed, setDismissed] = useState(() => localStorage.getItem('rackops-setup-done') === '1');

  const hasMachine = nodes.length > 0;
  const hasApp = services.length > 0;
  const hasPublic = services.some((s) => publicUrls(s, ingress).length > 0);
  const allDone = hasMachine && hasApp && hasPublic;

  // Never greet an established operator with first-run steps because the
  // fleet hasn't arrived yet.
  if (!loaded || dismissed || allDone) return null;

  const firstApp = services[0];
  const steps = [
    {
      done: hasMachine, title: 'Add a machine',
      desc: 'Run one command on any Linux box: an old PC, a Pi, a VPS.',
      action: 'Show command', run: onAddMachine,
    },
    {
      done: hasApp, title: 'Deploy your first app',
      desc: 'Pick from 45+ templates, or bring an image, git repo, or compose file.',
      action: 'Deploy', run: () => navigate('/apps/new'),
    },
    {
      done: hasPublic, title: 'Make it public',
      desc: 'Get a URL you can open from anywhere: instant, tunnel, or your own domain.',
      action: firstApp ? 'Choose a URL' : undefined,
      run: firstApp ? () => navigate(`/apps/${firstApp.id}/networking`) : undefined,
    },
  ];

  return (
    <div className="card" style={{ marginBottom: 20 }}>
      <div className="row spread" style={{ marginBottom: 12 }}>
        <div style={{ fontWeight: 600, fontSize: 14.5 }}>Get your rack online</div>
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
