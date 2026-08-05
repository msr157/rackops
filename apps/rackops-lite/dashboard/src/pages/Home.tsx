import { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  AlertCircle, AlertTriangle, Check, Globe, HardDrive, Plus, Rocket,
} from 'lucide-react';
import type { Node } from '../api';
import { useData } from '../data';
import { DataTable } from '../components/Table';
import type { Column } from '../components/Table';
import {
  EmptyState, Led, Pill, SkeletonRows, UsageBar, ledClass, publicUrls, timeAgo,
} from '../components/ui';

// Home answers one question in one line: is everything okay? Anything that
// needs attention follows as plain hairline rows, then quiet summaries of
// machines and apps. No charts, no KPI grid — the sub line carries the
// numbers and the lists carry the detail. History lives on /activity.
export function HomePage({ onAddMachine }: { onAddMachine: () => void }) {
  const { overview, nodes, services, alerts, nodeName, loaded } = useData();
  const navigate = useNavigate();
  const [showAllAttn, setShowAllAttn] = useState(false);

  const failing = services.filter((s) => s.status === 'failed');
  const openAlerts = alerts.filter((a) => !a.resolved);
  const nodesOffline = nodes.filter((n) => n.status === 'offline');
  // An offline machine counts toward the headline: the sub line must never
  // say "all systems normal" above a red attention list.
  const attention = failing.length + openAlerts.length + nodesOffline.length;
  const fleetEmpty = loaded && nodes.length === 0;

  const running = services.filter((s) => s.status === 'running').length;

  // A 20-incident night must not bury the rest of the page under a wall of
  // red: worst first (down apps, dark machines), alerts fill the rest of an
  // 8-row budget, and one button discloses the full list.
  const ATTN_CAP = 8;
  const failingShown = showAllAttn ? failing : failing.slice(0, ATTN_CAP);
  const offlineShown = showAllAttn ? nodesOffline
    : nodesOffline.slice(0, Math.max(0, ATTN_CAP - failingShown.length));
  const alertsShown = showAllAttn ? openAlerts
    : openAlerts.slice(0, Math.max(0, ATTN_CAP - failingShown.length - offlineShown.length));
  const hiddenAttn = attention - failingShown.length - offlineShown.length - alertsShown.length;

  // The one-line truthful answer. While the fleet is still loading it says
  // so — an unanswered fetch narrated as an empty rack reads as data loss.
  const sub = !loaded || !overview
    ? 'Checking your rack…'
    : fleetEmpty
      ? 'No machines yet. Join your first machine to bring the rack online.'
      : attention > 0
        ? `${attention} issue${attention === 1 ? '' : 's'} need${attention === 1 ? 's' : ''} your attention.`
        : `All systems normal. ${overview.nodesOnline}/${overview.nodesTotal} machine${overview.nodesTotal === 1 ? '' : 's'} online, ${
          services.length === 0 ? 'no apps deployed yet'
            : running === services.length ? `${running} app${running === 1 ? '' : 's'} running`
            : `${running} of ${services.length} apps running`}.`;

  // Machines summary: one glanceable row per machine. UsageBar is the only
  // quantitative graphic in the product now.
  const machineCols = useMemo<Column<Node>[]>(() => [
    {
      key: 'name',
      label: 'Machine',
      sortValue: (n) => n.name.toLowerCase(),
      render: (n) => (
        <span className="cell-main">
          <Led status={n.status} label />
          {/* Real link: the keyboard path into the row (see DataTable). */}
          <Link to={`/machines/${n.id}`} className="t-name" style={{ fontWeight: 600 }}
            onClick={(e) => e.stopPropagation()}>
            {n.name}
          </Link>
        </span>
      ),
    },
    {
      key: 'cpu',
      label: 'CPU',
      num: true,
      sortValue: (n) => (n.status === 'online' ? n.metrics.cpuPct : -1),
      render: (n) => (n.status === 'online' ? `${n.metrics.cpuPct.toFixed(0)}%` : ''),
    },
    {
      key: 'memory',
      label: 'Memory',
      num: true,
      sortValue: (n) => (n.status === 'online' ? n.metrics.memPct : -1),
      render: (n) => (n.status === 'online' ? (
        <span style={{ display: 'inline-block', minWidth: 96 }}>
          {n.metrics.memPct.toFixed(0)}%
          <UsageBar pct={n.metrics.memPct} />
        </span>
      ) : ''),
    },
    {
      key: 'temp',
      label: 'Temp',
      num: true,
      sortValue: (n) => (n.status === 'online' ? n.metrics.tempC : -1),
      render: (n) => (n.status === 'online' && n.metrics.tempC > 0
        ? `${n.metrics.tempC.toFixed(0)}°C` : ''),
    },
  ], []);

  // Apps summary, worst first: a failed app must never hide below the fold
  // of the six-row preview. Stable within each severity band.
  const sevRank: Record<'ok' | 'warn' | 'crit' | 'off', number> = { crit: 0, warn: 1, off: 2, ok: 3 };
  const appsPreview = services
    .map((s, i) => ({ s, i }))
    .sort((a, b) => (sevRank[ledClass(a.s.status)] - sevRank[ledClass(b.s.status)]) || a.i - b.i)
    .slice(0, 6)
    .map((x) => x.s);

  return (
    <div className="page">
      {/* Title row carries the page-owned quick actions: Deploy app lives
          here (and in Apps), never in the global chrome. On an empty fleet
          both yield to the join-command notice — one label per intent. */}
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title" style={{ marginBottom: 0 }}>Home</div>
        {!fleetEmpty && (
          <div className="row" style={{ gap: 8 }}>
            <button className="btn" onClick={onAddMachine}><Plus size={13} /> Add machine</button>
            <Link to="/apps/new" className="btn primary"><Rocket size={13} /> Deploy app</Link>
          </div>
        )}
      </div>
      <div className="page-sub">{sub}</div>

      {/* Empty fleet: onboarding owns the page — join command first, then
          the checklist. No machines/apps sections to render as hollow. */}
      {fleetEmpty && (
        <div className="notice accent">
          <HardDrive size={15} aria-hidden />
          <span style={{ flex: 1, minWidth: 220 }}>
            Your rack is waiting for its first machine. Any Linux box can join with one command.
          </span>
          <button className="btn primary" onClick={onAddMachine}>Show join command</button>
        </div>
      )}

      {/* Needs attention: worst first, each row one click from its fix. */}
      {attention > 0 && (
        <>
          <div className="section-title">Needs attention</div>
          <div className="list">
            {failingShown.map((s) => (
              <div key={`svc-${s.id}`} className="list-row" style={{ cursor: 'pointer' }}
                onClick={() => navigate(`/apps/${s.id}/logs`)}>
                <AlertCircle size={15} style={{ color: 'var(--crit)', flex: 'none' }} aria-hidden />
                <Link to={`/apps/${s.id}/logs`} style={{ color: 'inherit', fontWeight: 600 }}
                  onClick={(e) => e.stopPropagation()}>
                  {s.name}
                </Link>
                <span className="dim">app is down, auto-heal is retrying</span>
                <span className="feed-time">{timeAgo(s.updatedAt ?? s.createdAt)}</span>
              </div>
            ))}
            {offlineShown.map((n) => (
              <div key={`node-${n.id}`} className="list-row" style={{ cursor: 'pointer' }}
                onClick={() => navigate(`/machines/${n.id}`)}>
                <AlertCircle size={15} style={{ color: 'var(--crit)', flex: 'none' }} aria-hidden />
                <Link to={`/machines/${n.id}`} style={{ color: 'inherit', fontWeight: 600 }}
                  onClick={(e) => e.stopPropagation()}>
                  {n.name}
                </Link>
                <span className="dim">machine stopped reporting, check power and network</span>
                <span className="feed-time">{timeAgo(n.lastSeen)}</span>
              </div>
            ))}
            {alertsShown.map((a) => (
              <div key={`alert-${a.id}`} className="list-row"
                style={a.nodeId ? { cursor: 'pointer' } : undefined}
                onClick={a.nodeId ? () => navigate(`/machines/${a.nodeId}`) : undefined}>
                {a.level === 'critical'
                  ? <AlertCircle size={15} style={{ color: 'var(--crit)', flex: 'none' }} aria-hidden />
                  : <AlertTriangle size={15} style={{ color: 'var(--warn)', flex: 'none' }} aria-hidden />}
                {a.nodeId ? (
                  <Link to={`/machines/${a.nodeId}`} style={{ color: 'inherit' }}
                    onClick={(e) => e.stopPropagation()}>
                    {a.message}
                  </Link>
                ) : (
                  <span>{a.message}</span>
                )}
                <span className="feed-time">{timeAgo(a.createdAt)}</span>
              </div>
            ))}
          </div>
          {hiddenAttn > 0 && (
            <button className="btn sm" style={{ marginTop: 10 }} onClick={() => setShowAllAttn(true)}>
              Show all {attention} issues
            </button>
          )}
        </>
      )}

      {/* Onboarding never interrupts incidents: the checklist waits below the
          attention list and only renders when nothing needs attention. */}
      {attention === 0 && <SetupChecklist onAddMachine={onAddMachine} />}

      {/* Summaries render while loading (as skeletons) and on a live fleet;
          an empty fleet keeps the page to the notice + checklist above. */}
      {(!loaded || nodes.length > 0) && (
        <>
          <div className="section-title">
            Machines
            <Link to="/machines" className="faint section-link">View all →</Link>
          </div>
          {loaded ? (
            <DataTable<Node>
              caption="Machines: status, CPU, memory, and temperature per machine"
              columns={machineCols}
              rows={nodes}
              rowKey={(n) => n.id}
              onRowClick={(n) => navigate(`/machines/${n.id}`)}
            />
          ) : (
            <SkeletonRows count={3} height={44} />
          )}

          <div className="section-title">
            Apps
            {services.length > 0 && (
              <Link to="/apps" className="faint section-link">View all →</Link>
            )}
          </div>
          {!loaded ? (
            <SkeletonRows count={3} height={44} />
          ) : services.length === 0 ? (
            <EmptyState slim title="No apps yet">
              <span className="dim">Deploy anything with the button above: Jellyfin, Postgres, your own code.</span>
            </EmptyState>
          ) : (
            <div className="list">
              {appsPreview.map((s) => (
                <div key={s.id} className="list-row" style={{ cursor: 'pointer' }}
                  onClick={() => navigate(`/apps/${s.id}`)}>
                  <Led status={s.status} />
                  <Link to={`/apps/${s.id}`} onClick={(e) => e.stopPropagation()}
                    style={{
                      color: 'inherit', fontWeight: 600, maxWidth: 220,
                      overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                    }}>
                    {s.name}
                  </Link>
                  <Pill status={s.status} />
                  <span className="faint" style={{ marginLeft: 'auto' }}>{nodeName(s.nodeId)}</span>
                </div>
              ))}
            </div>
          )}

        </>
      )}
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
    <>
      <div className="section-title">
        Get your rack online
        <button className="btn sm ghost" style={{ marginLeft: 'auto' }}
          onClick={() => { localStorage.setItem('rackops-setup-done', '1'); setDismissed(true); }}>
          Skip setup
        </button>
      </div>
      <div>
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
    </>
  );
}
