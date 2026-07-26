import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  Boxes, HardDrive, Radio, Bell, Rocket, Globe, Check, ScrollText,
  Cpu, MemoryStick, Database, Plus, UserPlus, AlertTriangle, Activity,
} from 'lucide-react';
import { api, type MetricPoint, type User } from '../api';
import { useData } from '../data';
import { Rack } from '../components/Rack';
import { TrendChart, Seg, trendDelta } from '../components/charts';
import {
  Delta, EmptyState, IconChip, InfoTip, Led, SkeletonRows, UrlChip, UsageBar,
  publicUrls, timeAgo,
} from '../components/ui';

// Home answers one question at a glance: is everything okay? Anything that
// needs attention comes first; context (KPIs, the fleet chart, activity)
// follows in reference-register form: big numbers, deltas, one hero chart.
export function HomePage({ onAddMachine }: { onAddMachine: () => void }) {
  const { overview, nodes, services, events, alerts, ingress, loaded, reachable } = useData();
  const navigate = useNavigate();
  const [me, setMe] = useState<User | null>(null);
  const [hist24, setHist24] = useState<MetricPoint[]>([]);
  const [hist7, setHist7] = useState<MetricPoint[]>([]);
  const [win, setWin] = useState<'24h' | '7d'>('24h');
  const [metric, setMetric] = useState<'cpuPct' | 'memUsedMb'>('cpuPct');
  const [histErr, setHistErr] = useState(false);

  useEffect(() => { api.me().then(setMe).catch(() => {}); }, []);

  // The 24h fleet series backs both the hero chart and every KPI delta;
  // it refreshes on the sampler's own cadence, not the 2s live stream.
  useEffect(() => {
    let alive = true;
    const load = () => {
      api.metricsHistory('fleet', undefined, '24h')
        .then((h) => { if (alive) { setHist24(h.points); setHistErr(false); } })
        .catch(() => { if (alive) setHistErr(true); });
    };
    load();
    const t = setInterval(load, 60_000);
    return () => { alive = false; clearInterval(t); };
  }, []);

  useEffect(() => {
    if (win !== '7d') return;
    let alive = true;
    const load = () => {
      api.metricsHistory('fleet', undefined, '7d')
        .then((h) => { if (alive) { setHist7(h.points); setHistErr(false); } })
        .catch(() => { if (alive) setHistErr(true); });
    };
    load();
    const t = setInterval(load, 120_000);
    return () => { alive = false; clearInterval(t); };
  }, [win]);

  const failing = services.filter((s) => s.status === 'failed');
  const openAlerts = alerts.filter((a) => !a.resolved);
  const nodesOffline = nodes.filter((n) => n.status === 'offline');
  // An offline machine counts toward the headline: the banner must never
  // say "all systems operational" over a red attention list.
  const attention = failing.length + openAlerts.length + nodesOffline.length;
  const troubled = loaded && attention > 0;

  const running = services.filter((s) => s.status === 'running').length;

  const hour = new Date().getHours();
  const daypart = hour < 12 ? 'morning' : hour < 18 ? 'afternoon' : 'evening';
  const firstName = me?.fullName?.trim().split(/\s+/)[0];

  const points = win === '24h' ? hist24 : hist7;
  const heroValue = metric === 'cpuPct'
    ? `${overview?.avgCpuPct ?? 0}%`
    : `${((overview?.usedMemMb ?? 0) / 1024).toFixed(1)} GB`;
  const fmtCpu = (v: number) => `${v.toFixed(0)}%`;
  const fmtMem = (v: number) => `${(v / 1024).toFixed(1)} GB`;

  return (
    <div className="page">
      <div className="greet-row">
        <div>
          <div className="greet-date">
            {new Date().toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })}
          </div>
          <div className="greet">Good {daypart}{firstName ? `, ${firstName}` : ''}</div>
        </div>
        {/* Quick actions carry verbs the chrome doesn't already own (the
            topbar has Deploy). Add machine yields to the banner primary on
            an empty rack — one label per intent per surface. */}
        <div className="quick-actions">
          {nodes.length > 0 && (
            <button className="btn" onClick={onAddMachine}><Plus size={13} /> Add machine</button>
          )}
          {services.length > 0 && (
            <Link to="/network" className="btn"><Globe size={13} /> Make an app public</Link>
          )}
          <Link to="/settings" className="btn"><UserPlus size={13} /> Invite a teammate</Link>
        </div>
      </div>

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
          {/* Counts live in the KPI tiles right below — the banner only
              narrates while there is nothing else on screen yet. */}
          {!loaded && (
            <div className="dim" style={{ marginTop: 2 }}>
              {reachable ? 'connecting…' : 'the control plane is not answering yet'}
            </div>
          )}
        </div>
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

      {loaded && nodes.length > 0 && overview && (
        <>
          <div className="kpi-row" style={{ marginTop: 4 }}>
            <StatTile label="Machines online" tone={nodesOffline.length > 0 ? 'crit' : 'ok'}
              icon={<HardDrive size={15} />}
              value={overview.nodesOnline} unit={`/ ${overview.nodesTotal}`} />
            <StatTile label="Apps running" tone={failing.length > 0 ? 'crit' : services.length > 0 ? 'ok' : undefined}
              icon={<Boxes size={15} />}
              value={running} unit={`/ ${services.length}`} />
            <StatTile label="Fleet CPU" icon={<Cpu size={15} />}
              value={overview.avgCpuPct} unit="%"
              delta={<Delta pct={trendDelta(hist24, 'cpuPct')} />} />
            <StatTile label="Fleet memory" icon={<MemoryStick size={15} />}
              value={(overview.usedMemMb / 1024).toFixed(1)} unit={`/ ${(overview.totalMemMb / 1024).toFixed(0)} GB`}
              delta={<Delta pct={trendDelta(hist24, 'memUsedMb')} />} />
          </div>

          {/* The hero: how the fleet has been doing, not just how it is now */}
          <div className="card" style={{ marginBottom: 20 }}>
            <div className="row spread wrap" style={{ marginBottom: 10 }}>
              <div className="row" style={{ gap: 8 }}>
                <span style={{ fontWeight: 600, fontSize: 14 }}>Fleet activity</span>
                <InfoTip>
                  Averaged across every online machine in this organization, recorded once a
                  minute for 24 hours and every 15 minutes for a week. Gaps mean a machine was
                  offline, never zero.
                </InfoTip>
              </div>
              <div className="row" style={{ gap: 8 }}>
                <Seg value={metric} onChange={setMetric} label="Metric"
                  options={[{ value: 'cpuPct', label: 'CPU' }, { value: 'memUsedMb', label: 'Memory' }]} />
                <Seg value={win} onChange={setWin} label="Time window"
                  options={[{ value: '24h', label: '24h' }, { value: '7d', label: '7 days' }]} />
              </div>
            </div>
            <div className="row" style={{ gap: 10, marginBottom: 8 }}>
              <span className="mono" style={{ fontSize: 24, fontWeight: 600, letterSpacing: '-0.02em' }}>{heroValue}</span>
              <Delta pct={trendDelta(win === '24h' ? hist24 : hist7, metric)} />
            </div>
            <TrendChart points={points} metric={metric} height={190}
              yMax={metric === 'cpuPct' ? 100 : undefined}
              format={metric === 'cpuPct' ? fmtCpu : fmtMem}
              label={metric === 'cpuPct' ? 'Fleet CPU usage' : 'Fleet memory usage'}
              unavailable={histErr} />
          </div>
        </>
      )}

      <div className="grid-2" style={{ alignItems: 'start' }}>
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
          <div className="card" style={{ padding: '4px 16px' }}>
            <div className="feed">
              {events.slice(0, 12).map((e) => (
                <div key={e.id} className="feed-item">
                  <IconChip sm tone={feedTone(e.kind)}>{feedIcon(e.kind)}</IconChip>
                  <span style={{ flex: 1, paddingTop: 4 }}>{e.message}</span>
                  <span className="feed-time">{timeAgo(e.createdAt)}</span>
                </div>
              ))}
              {events.length === 0 && (
                <div className="feed-item">
                  <span className="faint" style={{ padding: '6px 0' }}>
                    {loaded
                      ? 'quiet so far: activity from deploys, heals, and machines appears here'
                      : 'waiting for the server…'}
                  </span>
                </div>
              )}
            </div>
          </div>
        </div>

        <div>
          <div className="section-title" style={{ marginTop: 12 }}>
            <HardDrive size={15} aria-hidden /> The rack
            <InfoTip>
              Every machine in this organization, one row per machine. The dot shows each
              machine&rsquo;s health; click through for full hardware detail.
            </InfoTip>
            <Link to="/machines" className="faint" style={{ marginLeft: 'auto', fontWeight: 400 }}>View all →</Link>
          </div>
          {/* No onAdd here: the greeting row already carries Add machine;
              the rack panel stays quiet. */}
          <Rack nodes={nodes} services={services} loading={!loaded} />

          {overview && nodes.length > 0 && overview.totalStorageGb > 0 && (
            <div className="card" style={{ marginTop: 14 }}>
              <div className="row spread">
                <span className="eyebrow">Pooled storage</span>
                <IconChip sm><Database size={14} /></IconChip>
              </div>
              <div className="kpi-value">{overview.usedStorageGb.toFixed(0)} <span className="unit">/ {overview.totalStorageGb.toFixed(0)} GB</span></div>
              <div className="row" style={{ gap: 8, marginTop: 8 }}>
                <UsageBar pct={(overview.usedStorageGb / overview.totalStorageGb) * 100} thick />
                <span className="faint mono">{((overview.usedStorageGb / overview.totalStorageGb) * 100).toFixed(0)}%</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// StatTile: eyebrow + icon chip, big mono value, optional delta line.
function StatTile({ label, icon, tone, value, unit, delta }: {
  label: string; icon: React.ReactNode; tone?: 'ok' | 'warn' | 'crit' | 'accent';
  value: React.ReactNode; unit?: string; delta?: React.ReactNode;
}) {
  return (
    <div className="card">
      <div className="row spread">
        <span className="eyebrow">{label}</span>
        <IconChip sm tone={tone}>{icon}</IconChip>
      </div>
      <div className="kpi-value">{value} {unit && <span className="unit">{unit}</span>}</div>
      {delta && <div className="kpi-delta">{delta}</div>}
    </div>
  );
}

// Feed icon + tone by event kind. Tone is meaning: alerts warn, a machine
// going dark is critical, everything routine stays neutral.
function feedTone(kind: string): 'warn' | 'crit' | undefined {
  if (kind.includes('down')) return 'crit';
  if (kind.includes('alert')) return 'warn';
  return undefined;
}
function feedIcon(kind: string) {
  if (kind.includes('alert')) return <AlertTriangle size={13} />;
  if (kind.includes('node') || kind.includes('machine')) return <HardDrive size={13} />;
  if (kind.includes('deploy') || kind.includes('service')) return <Rocket size={13} />;
  if (kind.includes('heal')) return <Activity size={13} />;
  return <Radio size={13} />;
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
