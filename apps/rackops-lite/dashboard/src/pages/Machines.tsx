import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  Activity, ArrowLeft, Boxes, Clock, Cpu, HardDrive, MemoryStick, Network as NetworkIcon, Plus, Rocket, Server, Thermometer, Zap,
} from 'lucide-react';
import { api } from '../api';
import type { MetricPoint, Node } from '../api';
import { useData } from '../data';
import { DataTable } from '../components/Table';
import type { Column } from '../components/Table';
import { Seg, TrendChart, trendDelta } from '../components/charts';
import {
  Delta, EmptyState, InfoTip, Kpi, Led, Pill, Skeleton, SkeletonRows, StatusBadge, UsageBar,
  formatUptime, ledClass, shortGpu, timeAgo,
} from '../components/ui';

// The metrics the Activity chart can plot; MetricPoint carries all three.
type MetricKey = 'cpuPct' | 'memUsedMb' | 'tempC';

// MachinesPage: fleet-wide capacity up top, then every machine as a sortable
// row — the ranking surface for "which box is busy, full, or gone quiet".
export function MachinesPage({ onAddMachine }: { onAddMachine: () => void }) {
  const { nodes, services, overview, loaded } = useData();
  const navigate = useNavigate();
  const [fleetPoints, setFleetPoints] = useState<MetricPoint[]>([]);
  // One add-machine action at a time: while the empty state carries the CTA,
  // the header button steps aside so the intent never appears twice.
  const fleetEmpty = loaded && nodes.length === 0;

  // One 24h fleet series feeds the KPI deltas. Fetched once: the comparison
  // averages whole hours, so refetching per tick would change nothing.
  useEffect(() => {
    let alive = true;
    api.metricsHistory('fleet', undefined, '24h')
      .then((h) => { if (alive) setFleetPoints(h.points); })
      .catch(() => { /* no history yet: the deltas simply stay hidden */ });
    return () => { alive = false; };
  }, []);

  const cpuDelta = trendDelta(fleetPoints, 'cpuPct');
  const memDelta = trendDelta(fleetPoints, 'memUsedMb');

  const columns = useMemo<Column<Node>[]>(() => {
    const appCounts = new Map<string, { up: number; total: number }>();
    for (const s of services) {
      const c = appCounts.get(s.nodeId) ?? { up: 0, total: 0 };
      c.total += 1;
      if (s.status === 'running') c.up += 1;
      appCounts.set(s.nodeId, c);
    }
    // Fill across every discovered drive; null when the agent hasn't
    // reported block devices yet, so the cell stays empty instead of "0%".
    const diskPct = (n: Node): number | null => {
      const drives = n.hardware.drives ?? [];
      const sizeGb = drives.reduce((sum, d) => sum + d.sizeGb, 0);
      if (sizeGb <= 0) return null;
      return (drives.reduce((sum, d) => sum + d.usedGb, 0) / sizeGb) * 100;
    };
    // Ascending status sort floats trouble to the top: offline before online.
    const statusRank: Record<'ok' | 'warn' | 'crit' | 'off', number> = { crit: 0, warn: 1, off: 2, ok: 3 };
    return [
      {
        key: 'name',
        label: 'Machine',
        sortValue: (n) => n.name.toLowerCase(),
        render: (n) => (
          <span className="row" style={{ gap: 8 }}>
            {/* Real link: the keyboard path into the row (see DataTable). */}
            <Link to={`/machines/${n.id}`} className="t-name" style={{ fontWeight: 600 }} onClick={(e) => e.stopPropagation()}>
              {n.name}
            </Link>
            {/* Running the control plane is an identity fact: neutral tag. */}
            {n.role === 'server' && <span className="tag">control</span>}
          </span>
        ),
      },
      {
        key: 'status',
        label: 'Status',
        sortValue: (n) => statusRank[ledClass(n.status)],
        render: (n) => <Pill status={n.status} />,
      },
      {
        key: 'ip',
        label: 'IP',
        render: (n) => <span className="dim mono">{n.ip}</span>,
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
        sortValue: (n) => (n.status === 'online' ? n.metrics.memUsedMb : -1),
        render: (n) => (n.status === 'online'
          ? `${(n.metrics.memUsedMb / 1024).toFixed(1)}/${(n.hardware.memTotalMb / 1024).toFixed(0)} GB`
          : ''),
      },
      {
        key: 'disk',
        label: 'Disk',
        num: true,
        sortValue: (n) => diskPct(n) ?? -1,
        render: (n) => {
          const p = diskPct(n);
          return p !== null ? `${p.toFixed(0)}%` : '';
        },
      },
      {
        key: 'apps',
        label: 'Apps',
        num: true,
        sortValue: (n) => appCounts.get(n.id)?.up ?? -1,
        render: (n) => {
          const c = appCounts.get(n.id);
          return c ? `${c.up}/${c.total}` : '';
        },
      },
      {
        key: 'uptime',
        label: 'Uptime',
        num: true,
        sortValue: (n) => (n.status === 'online' ? n.metrics.uptimeSec : -1),
        render: (n) => (n.status === 'online' ? formatUptime(n.metrics.uptimeSec) : ''),
      },
      {
        // Online rows say nothing here: the status pill already says it.
        key: 'lastSeen',
        label: 'Last seen',
        sortValue: (n) => Date.parse(n.lastSeen) || 0,
        render: (n) => (n.status === 'offline' ? <span className="faint">{timeAgo(n.lastSeen)}</span> : null),
      },
    ];
  }, [services]);

  return (
    <div className="page">
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Machines</div>
        {!fleetEmpty && (
          <button className="btn primary" onClick={onAddMachine}><Plus size={14} aria-hidden /> Add machine</button>
        )}
      </div>
      <div className="page-sub">Every box in your rack, hardware discovered automatically</div>

      {/* Fleet KPIs: skeletons hold the row's shape until the overview arrives,
          so numbers land without a layout jump. An empty (loaded) fleet has no
          capacity to summarize, so the row disappears rather than showing zeros. */}
      {fleetEmpty ? null : overview ? (
        <div className="kpi-row">
          <Kpi icon={<HardDrive size={12} aria-hidden />} label="Machines" value={`${overview.nodesOnline}/${overview.nodesTotal}`} unit="online" />
          <Kpi icon={<Cpu size={12} aria-hidden />} label="Fleet CPU" value={overview.avgCpuPct.toFixed(0)} unit="%">
            {cpuDelta !== null && (
              <div className="kpi-delta"><Delta pct={cpuDelta} label="vs 24 hours ago" /></div>
            )}
          </Kpi>
          <Kpi icon={<MemoryStick size={12} aria-hidden />} label="Memory"
            value={(overview.usedMemMb / 1024).toFixed(1)} unit={`/ ${(overview.totalMemMb / 1024).toFixed(0)} GB`}>
            {memDelta !== null && (
              <div className="kpi-delta"><Delta pct={memDelta} label="vs 24 hours ago" /></div>
            )}
          </Kpi>
          <Kpi icon={<HardDrive size={12} aria-hidden />} label="Storage"
            value={overview.usedStorageGb.toFixed(0)} unit={`/ ${overview.totalStorageGb.toFixed(0)} GB`} />
        </div>
      ) : (
        <div className="kpi-row" aria-hidden>
          {[0, 1, 2, 3].map((i) => <Skeleton key={i} height={90} radius="var(--r-lg)" />)}
        </div>
      )}

      {/* The comparison surface: one sortable row per machine. Skeletons hold
          the space while the fleet loads; once it is confirmed empty, the
          single empty state below takes over. */}
      {!fleetEmpty && (loaded ? (
        <DataTable
          columns={columns}
          rows={nodes}
          rowKey={(n) => n.id}
          onRowClick={(n) => navigate(`/machines/${n.id}`)}
          defaultSort={{ key: 'status', dir: 'asc' }}
          caption="Machines in this rack"
        />
      ) : (
        <SkeletonRows count={4} height={52} />
      ))}

      {fleetEmpty && (
        <EmptyState icon={<Server size={18} />} title="One command turns any Linux box into part of your rack">
          <div style={{ marginBottom: 14, lineHeight: 1.6 }}>
            Old PC, Raspberry Pi, VPS: install the agent and it shows up here in seconds,<br />
            hardware and all. Works behind CGNAT; only outbound connections.
          </div>
          <button className="btn primary" onClick={onAddMachine}><Plus size={13} aria-hidden /> Add machine</button>
        </EmptyState>
      )}
    </div>
  );
}

// Drive health is a real status; anything unrecognized stays neutral.
const healthBadge: Record<string, string> = { ok: 'ok', warning: 'warn', critical: 'crit' };

export function MachineDetail() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const { nodes, services, loaded } = useData();
  const node = nodes.find((n) => n.id === id);

  const [metric, setMetric] = useState<MetricKey>('cpuPct');
  const [win, setWin] = useState<'24h' | '7d'>('24h');
  const [points, setPoints] = useState<MetricPoint[]>([]);
  const [histErr, setHistErr] = useState(false);
  const exists = !!node;

  // Switching machines must not show the previous box's history while the
  // new fetch is in flight.
  useEffect(() => { setPoints([]); setHistErr(false); }, [id]);

  // Server-side history: refetched on window change and once a minute, so a
  // detail page left open on a shelf screen keeps tracking without a reload.
  useEffect(() => {
    if (!exists) return;
    let alive = true;
    const load = () => {
      api.metricsHistory('node', id, win)
        .then((h) => { if (alive) { setPoints(h.points); setHistErr(false); } })
        .catch(() => { if (alive) setHistErr(true); }); // a dead read must not wear "collecting"
    };
    load();
    const t = window.setInterval(load, 60_000);
    return () => { alive = false; window.clearInterval(t); };
  }, [id, win, exists]);

  if (!node) {
    // The fleet hasn't answered yet; this is loading, not a missing machine.
    if (!loaded) {
      return (
        <div className="page" aria-busy="true">
          <Skeleton height={24} width={240} />
          <div style={{ marginTop: 10 }}><Skeleton height={14} width={340} /></div>
          <div className="kpi-row" style={{ marginTop: 22 }} aria-hidden>
            {[0, 1, 2, 3].map((i) => <Skeleton key={i} height={140} radius="var(--r-lg)" />)}
          </div>
          <SkeletonRows count={3} height={76} />
        </div>
      );
    }
    return (
      <div className="page">
        <EmptyState title="Machine not found">
          <div style={{ marginBottom: 12 }}>It may have been removed from the rack, or the link is stale.</div>
          <Link to="/machines" className="btn">Back to machines</Link>
        </EmptyState>
      </div>
    );
  }

  const m = node.metrics;
  const hw = node.hardware;
  const apps = services.filter((s) => s.nodeId === node.id);
  const gpus = hw.gpus ?? [];
  const drives = hw.drives ?? [];
  const nics = hw.nics ?? [];

  // No sensor, no Temperature tab: a flat 0°C line is noise, not telemetry.
  const hasTemp = m.tempC > 0 || points.some((p) => (p.tempC ?? 0) > 0);
  const activeMetric: MetricKey = metric === 'tempC' && !hasTemp ? 'cpuPct' : metric;
  const metricDefs: Record<MetricKey, { label: string; current: number; format: (v: number) => string }> = {
    cpuPct: { label: 'CPU usage', current: m.cpuPct, format: (v) => `${v.toFixed(0)}%` },
    memUsedMb: { label: 'Memory used', current: m.memUsedMb, format: (v) => `${(v / 1024).toFixed(1)} GB` },
    tempC: { label: 'Package temperature', current: m.tempC, format: (v) => `${v.toFixed(0)}°C` },
  };
  const chart = metricDefs[activeMetric];
  const metricOptions: { value: MetricKey; label: string }[] = [
    { value: 'cpuPct', label: 'CPU' },
    { value: 'memUsedMb', label: 'Memory' },
  ];
  if (hasTemp) metricOptions.push({ value: 'tempC', label: 'Temperature' });

  return (
    <div className="page">
      <div className="row" style={{ marginBottom: 4 }}>
        <button className="btn sm ghost" onClick={() => navigate('/machines')}
          aria-label="Back to machines" title="Back to machines"><ArrowLeft size={14} aria-hidden /></button>
        <div className="page-title" style={{ margin: 0, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.name}</div>
        <StatusBadge status={node.status} />
        <Link to={`/apps/new?node=${node.id}`} className="btn" style={{ marginLeft: 'auto', flex: 'none' }}>
          <Rocket size={13} aria-hidden /> Deploy to this machine
        </Link>
      </div>
      <div className="row wrap page-sub" style={{ gap: 8 }}>
        <span className="tag">{node.os}</span>
        <span className="tag">{node.arch}</span>
        <span className="tag">kernel {node.kernel}</span>
        <span className="tag">{hw.cpuModel}</span>
        <span className="dim mono">{node.ip}</span>
        {node.publicIp && <span className="dim mono">public {node.publicIp}</span>}
        <span className="dim row" style={{ gap: 5 }}><Clock size={12} aria-hidden /> up {formatUptime(m.uptimeSec)}</span>
      </div>

      {node.status === 'offline' && (
        <div className="card crit" style={{ marginBottom: 16 }}>
          <strong style={{ color: 'var(--crit)' }}>This machine stopped reporting.</strong>
          <div className="dim" style={{ marginTop: 4 }}>
            Check that it's powered on and connected, then make sure the agent is running:
            <code className="mono" style={{ marginLeft: 6 }}>systemctl status rackops-agent</code> (or re-run the join command).
          </div>
        </div>
      )}

      <div className="kpi-row">
        <Kpi icon={<Cpu size={12} aria-hidden />} label="CPU" value={m.cpuPct.toFixed(0)} unit="%">
          <div className="faint">{hw.cpuCores}C/{hw.cpuThreads}T · load {m.load1.toFixed(2)}</div>
          <UsageBar pct={m.cpuPct} />
        </Kpi>
        <Kpi icon={<MemoryStick size={12} aria-hidden />} label="Memory"
          value={(m.memUsedMb / 1024).toFixed(1)} unit={`/ ${(hw.memTotalMb / 1024).toFixed(0)} GB`}>
          <div className="faint">{m.memPct.toFixed(0)}% used</div>
          <UsageBar pct={m.memPct} />
        </Kpi>
        {/* Temperature is a reading, not a percentage; the number stands alone,
            no meter bar. */}
        <Kpi icon={<Thermometer size={12} aria-hidden />} label="Package temp" value={m.tempC.toFixed(0)} unit="°C">
          <div className="faint">
            {m.tempC > 85 ? 'throttling risk' : m.tempC > 70 ? 'warm' : 'healthy'}
            <InfoTip>
              The CPU package sensor, read straight from the hardware. Above ~85°C most CPUs slow
              themselves down to cool off; an alert opens if it stays there.
            </InfoTip>
          </div>
        </Kpi>
        <Kpi icon={<Zap size={12} aria-hidden />} label="Workload" value={m.containers} unit="containers">
          <div className="faint">{m.processCount} processes</div>
        </Kpi>
      </div>

      {/* Activity: server-side history for this machine. One metric at a
          time, switched with a Seg; the KPI tiles above stay live-now. */}
      <div className="card" style={{ marginBottom: 16 }}>
        <div className="row spread wrap" style={{ gap: 10 }}>
          <div className="eyebrow"><Activity size={12} aria-hidden /> Activity</div>
          <div className="row wrap" style={{ gap: 8 }}>
            <Seg value={activeMetric} onChange={setMetric} options={metricOptions} label="Chart metric" />
            <Seg value={win} onChange={setWin} label="Time window"
              options={[{ value: '24h', label: '24h' }, { value: '7d', label: '7d' }]} />
          </div>
        </div>
        <div className="row" style={{ gap: 10, margin: '12px 0 4px', alignItems: 'baseline' }}>
          <span className="mono" style={{ fontSize: 20, fontWeight: 600 }}>{chart.format(chart.current)}</span>
          <Delta pct={trendDelta(points, activeMetric)} label="vs 24 hours ago" />
        </div>
        <TrendChart points={points} metric={activeMetric} height={170}
          yMax={activeMetric === 'cpuPct' ? 100 : undefined}
          format={chart.format} label={chart.label} unavailable={histErr}
          drawKey={`${activeMetric}:${win}`} />
      </div>

      <div className="section-title" style={{ marginTop: 4 }}><Boxes size={15} aria-hidden /> Apps on this machine</div>
      {apps.length > 0 ? (
        <div className="stack" style={{ gap: 8 }}>
          {apps.map((s) => (
            <Link key={s.id} to={`/apps/${s.id}`} className="card clickable row" style={{ padding: '10px 14px', color: 'var(--text)' }}>
              <Led status={s.status} label />
              <strong style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.name}</strong>
              <span className="faint mono" style={{ marginLeft: 'auto', flex: 'none' }}>
                {s.status === 'running' ? `${s.cpuPct.toFixed(0)}% cpu · ${s.memUsedMb} MB` : s.status}
              </span>
            </Link>
          ))}
        </div>
      ) : (
        <EmptyState icon={<Boxes size={18} />} title="Nothing deployed here yet">
          {/* The header's "Deploy to this machine" is the one deploy action on
              this page; the empty state points at it instead of cloning it. */}
          <div>Use <strong>Deploy to this machine</strong> above, or let auto-placement pick this box for a new app.</div>
        </EmptyState>
      )}

      {gpus.length > 0 && (
        <>
          <div className="section-title">GPUs & accelerators</div>
          <div className="grid-2">
            {gpus.map((g) => (
              <div className="card" key={g.index}>
                <div className="row spread">
                  <strong>{shortGpu(g.model)}</strong>
                  {/* Vendor is an identity fact, not a status; neutral tag. */}
                  <span className="tag">{g.vendor.toUpperCase()}</span>
                </div>
                <div className="grid-3" style={{ marginTop: 12 }}>
                  <div>
                    <div className="row spread dim"><span>VRAM</span><span className="mono">{g.vramUsedMb}/{g.vramTotalMb} MB</span></div>
                    <UsageBar pct={g.vramTotalMb ? (g.vramUsedMb / g.vramTotalMb) * 100 : 0} />
                  </div>
                  <div>
                    <div className="row spread dim"><span>Util</span><span className="mono">{g.utilPct.toFixed(0)}%</span></div>
                    <UsageBar pct={g.utilPct} />
                  </div>
                  <div>
                    {/* Temp and power are readings, not fill levels; no bar. */}
                    <div className="row spread dim"><span>Temp</span><span className="mono">{g.tempC.toFixed(0)}°C</span></div>
                    <div className="row spread dim"><span>Power</span><span className="mono">{g.powerW.toFixed(0)}W</span></div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      <div className="section-title">
        <HardDrive size={15} aria-hidden /> Drives
        <InfoTip>
          Live read/write throughput and fill level per drive. The health badge warns at 85% full
          and turns critical at 95%; full disks are the top cause of failed deploys.
        </InfoTip>
      </div>
      {drives.length > 0 ? (
        <div className="grid-2">
          {drives.map((d) => {
            const pct = d.sizeGb > 0 ? (d.usedGb / d.sizeGb) * 100 : 0;
            return (
              <div className="card" key={d.device}>
                <div className="row spread">
                  <div className="row wrap">
                    <strong className="mono">{d.device}</strong>
                    {/* Drive type is hardware identity; neutral tag, never a status hue. */}
                    <span className="tag">{d.type.toUpperCase()}</span>
                    <span className={`badge ${healthBadge[d.health] ?? 'neutral'}`}>{d.health}</span>
                    {!d.mountPoint && <span className="badge warn">not mounted</span>}
                  </div>
                  {d.tempC ? (
                    <span className="dim mono row" style={{ gap: 4 }}>
                      <Thermometer size={12} aria-hidden />{d.tempC.toFixed(0)}°C
                    </span>
                  ) : null}
                </div>
                <div className="faint" style={{ margin: '6px 0 10px' }}>
                  {d.model || 'Unknown model'}{d.mountPoint && ` · mounted at ${d.mountPoint}`}
                </div>
                <div className="row spread dim">
                  <span>{d.usedGb.toFixed(0)} GB used</span>
                  <span className="mono">{d.sizeGb.toFixed(0)} GB · {pct.toFixed(0)}%</span>
                </div>
                <UsageBar pct={pct} thick />
                <div className="row" style={{ marginTop: 10, gap: 16 }}>
                  <span className="dim mono">R {d.readMbps.toFixed(1)} MB/s</span>
                  <span className="dim mono">W {d.writeMbps.toFixed(1)} MB/s</span>
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <EmptyState slim title="No drives discovered">
          <div>The agent hasn't reported any block devices on this machine yet.</div>
        </EmptyState>
      )}

      <div className="section-title"><NetworkIcon size={15} aria-hidden /> Network interfaces</div>
      {nics.length > 0 ? (
        <div className="grid-3">
          {nics.map((nic) => (
            <div className="card" key={nic.name}>
              <div className="row spread">
                <strong className="mono">{nic.name}</strong>
                <span className="faint mono">{nic.mac}</span>
              </div>
              <div className="row" style={{ marginTop: 8, gap: 16 }}>
                <span className="dim mono">↓ {nic.rxMbps.toFixed(1)} MB/s</span>
                <span className="dim mono">↑ {nic.txMbps.toFixed(1)} MB/s</span>
                {nic.speedMb > 0 && <span className="faint">{nic.speedMb} Mb link</span>}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <EmptyState slim title="No interfaces reported">
          <div>The agent hasn't reported any network interfaces for this machine yet.</div>
        </EmptyState>
      )}
    </div>
  );
}
