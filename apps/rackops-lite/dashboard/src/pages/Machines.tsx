import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  Activity, Boxes, CalendarDays, Clock, Cpu, Globe, HardDrive, MemoryStick,
  Network as NetworkIcon, Package, Plus, Rocket, RotateCw, Search, Server, Terminal,
  Thermometer, TriangleAlert, Zap,
} from 'lucide-react';
import type { Drive, GPU, Node } from '../api';
import { useData } from '../data';
import { DataTable } from '../components/Table';
import type { Column } from '../components/Table';
import {
  CopyIconBtn, Crumbs, EmptyState, InfoTip, Led, RowMenu, Seg, Skeleton, SkeletonRows,
  UsageBar, formatUptime, ledClass, shortGpu, timeAgo,
} from '../components/ui';

// Severity order: ascending floats trouble to the top — offline before online.
const STATUS_RANK: Record<ReturnType<typeof ledClass>, number> = {
  crit: 0, warn: 1, off: 2, ok: 3,
};

// Status segments. Offline means "not reporting", whatever the exact status
// string: a box in any unknown state is exactly what someone filtering for
// trouble wants to see.
type NodeFilter = 'all' | 'online' | 'offline';
const FILTERS: ReadonlyArray<readonly [NodeFilter, string]> = [
  ['all', 'All'], ['online', 'Online'], ['offline', 'Offline'],
];
const FILTER_TEST: Record<NodeFilter, (n: Node) => boolean> = {
  all: () => true,
  online: (n) => n.status === 'online',
  offline: (n) => n.status !== 'online',
};

// Trims marketing noise off CPU model strings so a secondary line stays one
// line: "Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz" reads "Intel Core i5-8500T".
function shortCpu(model: string): string {
  return model.replace(/\((?:R|TM)\)/gi, '').replace(/\s*CPU\s*@.*$/i, '').replace(/\s+/g, ' ').trim();
}

// MachinesPage: every machine as a sortable row — the ranking surface for
// "which box is busy, hot, or gone quiet".
export function MachinesPage({ onAddMachine }: { onAddMachine: () => void }) {
  const { nodes, loaded, refresh } = useData();
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<NodeFilter>('all');

  const query = search.trim().toLowerCase();
  const visible = nodes.filter((n) =>
    (!query || `${n.name} ${n.ip} ${n.os} ${n.arch}`.toLowerCase().includes(query))
    && FILTER_TEST[filter](n));

  // Worst first without a status column: the Led beside each name carries the
  // word (sr-only), and rows enter pre-ranked — offline above online, ties
  // alphabetical. DataTable's stable sort keeps this order until a header is
  // clicked.
  const rows = [...visible].sort((a, b) =>
    STATUS_RANK[ledClass(a.status)] - STATUS_RANK[ledClass(b.status)] || a.name.localeCompare(b.name));

  const columns: Column<Node>[] = [
    {
      key: 'name',
      label: 'Name',
      sortValue: (n) => n.name.toLowerCase(),
      render: (n) => (
        <>
          <span className="cell-main">
            <Led status={n.status} label />
            {/* Real link: the keyboard path into the row (see DataTable). */}
            <Link to={`/machines/${n.id}`} className="t-name" style={{ color: 'inherit' }}
              onClick={(e) => e.stopPropagation()}>
              {n.name}
            </Link>
            {/* Running the control plane is an identity fact: neutral tag. */}
            {n.role === 'server' && <span className="tag">control</span>}
          </span>
          <span className="cell-sub mono">{n.ip}</span>
        </>
      ),
    },
    {
      key: 'system',
      label: 'System',
      sortValue: (n) => `${n.os}/${n.arch}`,
      render: (n) => (
        <>
          <span className="dim">{n.os}/{n.arch}</span>
          <span className="cell-sub">{shortCpu(n.hardware.cpuModel)}</span>
        </>
      ),
    },
    {
      key: 'cpu',
      label: 'CPU %',
      num: true,
      width: 80,
      sortValue: (n) => (n.status === 'online' ? n.metrics.cpuPct : -1),
      render: (n) => (n.status === 'online' ? n.metrics.cpuPct.toFixed(0) : ''),
    },
    {
      key: 'memory',
      label: 'Memory',
      num: true,
      width: 110,
      sortValue: (n) => (n.status === 'online' ? n.metrics.memPct : -1),
      render: (n) => (n.status === 'online' ? (
        <>
          {n.metrics.memPct.toFixed(0)}%
          <UsageBar pct={n.metrics.memPct} />
        </>
      ) : ''),
    },
    {
      key: 'temp',
      label: 'Temp °C',
      num: true,
      width: 90,
      sortValue: (n) => (n.status === 'online' ? n.metrics.tempC : -1),
      // A machine with no sensor reports 0; a fabricated 0°C would be a lie.
      render: (n) => (n.status === 'online' && n.metrics.tempC > 0 ? n.metrics.tempC.toFixed(0) : ''),
    },
    {
      key: 'uptime',
      label: 'Uptime',
      num: true,
      width: 90,
      sortValue: (n) => (n.status === 'online' ? n.metrics.uptimeSec : -1),
      render: (n) => (n.status === 'online' ? formatUptime(n.metrics.uptimeSec) : ''),
    },
    {
      key: 'actions',
      label: <span className="sr-only">Actions</span>,
      width: 56,
      render: (n) => (
        <RowMenu label={`Actions for ${n.name}`} items={[
          { label: 'View machine', icon: <HardDrive size={13} aria-hidden />, onSelect: () => navigate(`/machines/${n.id}`) },
        ]} />
      ),
    },
  ];

  return (
    <div className="page">
      <Crumbs items={[{ label: 'Machines', icon: <HardDrive size={13} aria-hidden /> }]} />
      {/* Count in the title once the fleet has answered; a number invented
          before `loaded` would be a guess. */}
      <div className="page-title">
        {loaded ? `${nodes.length} ${nodes.length === 1 ? 'Machine' : 'Machines'}` : 'Machines'}
      </div>
      <div className="page-sub">Every box in your rack, hardware discovered automatically.</div>

      <div className="toolbar">
        <div className="gsearch">
          <Search size={14} aria-hidden />
          <input value={search} placeholder="Search machines…" aria-label="Search machines"
            onChange={(e) => setSearch(e.target.value)} />
        </div>
        <Seg value={filter} onChange={setFilter} options={FILTERS} label="Filter by status" />
        <button type="button" className="icon-sq" onClick={() => refresh()}
          aria-label="Refresh machines" title="Refresh machines">
          <RotateCw size={14} />
        </button>
        <span className="grow" />
        <button className="btn primary" onClick={onAddMachine}>
          <Plus size={14} aria-hidden /> Add machine
        </button>
      </div>

      {/* Waiting for the first load is not the same as having no machines. */}
      {!loaded && nodes.length === 0 && <SkeletonRows count={4} height={52} />}

      {rows.length > 0 && (
        <DataTable<Node>
          columns={columns}
          rows={rows}
          rowKey={(n) => n.id}
          onRowClick={(n) => navigate(`/machines/${n.id}`)}
          caption="Machines in this rack"
        />
      )}

      {/* Active filters say what they hid — a shortened table with no note
          reads as the whole fleet. */}
      {rows.length > 0 && rows.length < nodes.length && (
        <div className="faint" style={{ padding: '10px 2px' }} role="status">
          Showing {rows.length} of {nodes.length} machines
        </div>
      )}

      {/* The add action stays in the toolbar; the empty state only points. */}
      {loaded && nodes.length === 0 && (
        <EmptyState icon={<Server size={18} />} title="No machines yet">
          <div>Old PC, Raspberry Pi, VPS: install the agent and the box shows up here in seconds, hardware and all.</div>
          <div>Works behind CGNAT; only outbound connections. <strong>Add machine</strong> above has the one-line command.</div>
        </EmptyState>
      )}

      {nodes.length > 0 && rows.length === 0 && (
        <div className="row" style={{ gap: 10, padding: '14px 2px' }}>
          <span className="faint">No machines match the current filters.</span>
          <button className="btn sm" onClick={() => { setSearch(''); setFilter('all'); }}>
            Clear filters
          </button>
        </div>
      )}
    </div>
  );
}

/* ---------- Machine detail ---------- */

// Drive health is a real status; anything unrecognized stays neutral.
const healthBadge: Record<string, string> = { ok: 'ok', warning: 'warn', critical: 'crit' };

// One stripe of the host-facts stack: icon + label left, value right (mono
// when it's data), an optional copy square for things worth pasting.
function KvRow({ icon, label, value, mono, copy }: {
  icon?: React.ReactNode; label: string; value: React.ReactNode; mono?: boolean; copy?: string;
}) {
  return (
    <div className="kv-row">
      <span className="kv-label">{icon}{label}</span>
      <span className={`kv-val ${mono ? 'mono' : ''}`}>{value}</span>
      {copy && <CopyIconBtn size={12} text={copy} label={`Copy ${label.toLowerCase()}`} />}
    </div>
  );
}

// GPU inventory: vendor is identity (neutral), temp and power are readings
// (no meter); only VRAM and utilization are fill levels.
const gpuColumns: Column<GPU>[] = [
  {
    key: 'model',
    label: 'Model',
    render: (g) => (
      <>
        <span className="cell-main">{shortGpu(g.model)}</span>
        <span className="cell-sub mono">{g.vendor.toUpperCase()}</span>
      </>
    ),
  },
  {
    key: 'vram',
    label: 'VRAM',
    num: true,
    width: 150,
    render: (g) => (
      <>
        {g.vramUsedMb}/{g.vramTotalMb} MB
        <UsageBar pct={g.vramTotalMb ? (g.vramUsedMb / g.vramTotalMb) * 100 : 0} />
      </>
    ),
  },
  {
    key: 'util',
    label: 'Util %',
    num: true,
    width: 90,
    render: (g) => (
      <>
        {g.utilPct.toFixed(0)}
        <UsageBar pct={g.utilPct} />
      </>
    ),
  },
  { key: 'temp', label: 'Temp °C', num: true, width: 90, render: (g) => g.tempC.toFixed(0) },
  { key: 'power', label: 'Power W', num: true, width: 90, render: (g) => g.powerW.toFixed(0) },
];

// Drive rows: type is hardware identity (neutral tag, never a status hue);
// the health badge is the one status word.
const driveColumns: Column<Drive>[] = [
  {
    key: 'device',
    label: 'Device',
    sortValue: (d) => d.device,
    render: (d) => (
      <>
        <span className="cell-main">
          <span className="mono">{d.device}</span>
          <span className="tag">{d.type.toUpperCase()}</span>
        </span>
        <span className="cell-sub">{d.model || 'Unknown model'}{d.mountPoint ? ` · mounted at ${d.mountPoint}` : ''}</span>
      </>
    ),
  },
  {
    key: 'health',
    label: 'Health',
    width: 140,
    render: (d) => (
      <span className="row" style={{ gap: 6 }}>
        <span className={`badge ${healthBadge[d.health] ?? 'neutral'}`}>{d.health}</span>
        {!d.mountPoint && <span className="badge warn">not mounted</span>}
      </span>
    ),
  },
  {
    key: 'used',
    label: 'Used',
    num: true,
    width: 130,
    sortValue: (d) => (d.sizeGb > 0 ? d.usedGb / d.sizeGb : -1),
    render: (d) => {
      const pct = d.sizeGb > 0 ? (d.usedGb / d.sizeGb) * 100 : 0;
      return (
        <>
          {pct.toFixed(0)}%
          <UsageBar pct={pct} />
          <span className="cell-sub mono">{d.usedGb.toFixed(0)}/{d.sizeGb.toFixed(0)} GB</span>
        </>
      );
    },
  },
  {
    key: 'io',
    label: 'R / W',
    num: true,
    width: 150,
    render: (d) => `${d.readMbps.toFixed(1)} / ${d.writeMbps.toFixed(1)} MB/s`,
  },
  {
    key: 'temp',
    label: 'Temp °C',
    num: true,
    width: 90,
    render: (d) => (d.tempC ? d.tempC.toFixed(0) : ''),
  },
];

export function MachineDetail() {
  const { id = '' } = useParams();
  const { nodes, services, loaded } = useData();
  const node = nodes.find((n) => n.id === id);

  if (!node) {
    // The fleet hasn't answered yet; this is loading, not a missing machine.
    if (!loaded) {
      return (
        <div className="page" aria-busy="true">
          <Skeleton height={13} width={160} />
          <div style={{ margin: '16px 0 10px' }}><Skeleton height={24} width={240} /></div>
          <div style={{ marginBottom: 24 }}><Skeleton height={14} width={320} /></div>
          <SkeletonRows count={4} height={46} />
          <span className="sr-only">Loading machine…</span>
        </div>
      );
    }
    return (
      <div className="page">
        <Crumbs items={[
          { label: 'Machines', to: '/machines', icon: <HardDrive size={13} aria-hidden /> },
          { label: 'Not found' },
        ]} />
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
  // No sensor, no temperature reading: a flat 0°C is noise, not telemetry.
  const hasTemp = m.tempC > 0;

  return (
    <div className="page">
      <Crumbs items={[
        { label: 'Machines', to: '/machines', icon: <HardDrive size={13} aria-hidden /> },
        { label: node.name },
      ]} />
      <div className="entity-head" style={{ alignItems: 'center' }}>
        <Led status={node.status} lg label />
        <div className="entity-name">{node.name}</div>
        {node.role === 'server' && <span className="tag">control</span>}
        <div className="entity-actions">
          <Link to={`/apps/new?node=${node.id}`} className="btn">
            <Rocket size={13} aria-hidden /> Deploy to this machine
          </Link>
        </div>
      </div>
      <div className="entity-meta">{node.os} ({node.arch}) · {node.ip}</div>

      {node.status === 'offline' && (
        <div className="notice crit">
          <TriangleAlert size={14} style={{ color: 'var(--crit)', flex: 'none' }} aria-hidden />
          <span>
            <strong>This machine stopped reporting.</strong> Check that it&rsquo;s powered on and
            connected, then make sure the agent is running:{' '}
            <code className="mono">systemctl status rackops-agent</code> (or re-run the join command).
          </span>
        </div>
      )}

      {/* Live readouts off the same telemetry the fleet stream updates.
          Temperature is a reading, not a fill level: no meter bar. */}
      <div className="section-title" style={{ marginTop: 8 }}><Activity size={15} aria-hidden /> Live usage</div>
      <div className="grid-2" style={{ maxWidth: 860, gap: '16px 32px' }}>
        <div>
          <div className="row spread">
            <span className="dim row" style={{ gap: 7 }}><Cpu size={13} aria-hidden /> CPU</span>
            <span className="mono">{m.cpuPct.toFixed(0)}%</span>
          </div>
          <UsageBar pct={m.cpuPct} />
          <div className="faint" style={{ marginTop: 5 }}>{hw.cpuCores}C/{hw.cpuThreads}T · load {m.load1.toFixed(2)}</div>
        </div>
        <div>
          <div className="row spread">
            <span className="dim row" style={{ gap: 7 }}><MemoryStick size={13} aria-hidden /> Memory</span>
            <span className="mono">{(m.memUsedMb / 1024).toFixed(1)} / {(hw.memTotalMb / 1024).toFixed(0)} GB</span>
          </div>
          <UsageBar pct={m.memPct} />
          <div className="faint" style={{ marginTop: 5 }}>{m.memPct.toFixed(0)}% used</div>
        </div>
        <div>
          <div className="row spread">
            <span className="dim row" style={{ gap: 7 }}><Thermometer size={13} aria-hidden /> Package temp</span>
            {hasTemp ? <span className="mono">{m.tempC.toFixed(0)}°C</span> : <span className="faint">no sensor</span>}
          </div>
          {hasTemp && (
            <div className="faint" style={{ marginTop: 5 }}>
              {m.tempC > 85 ? 'throttling risk' : m.tempC > 70 ? 'warm' : 'healthy'}
              <InfoTip>
                The CPU package sensor, read straight from the hardware. Above ~85°C most CPUs slow
                themselves down to cool off; an alert opens if it stays there.
              </InfoTip>
            </div>
          )}
        </div>
        <div>
          <div className="row spread">
            <span className="dim row" style={{ gap: 7 }}><Zap size={13} aria-hidden /> Workload</span>
            <span className="mono">{m.containers} containers</span>
          </div>
          <div className="faint" style={{ marginTop: 5 }}>{m.processCount} processes</div>
        </div>
      </div>

      <div className="section-title"><Server size={15} aria-hidden /> Host</div>
      <div className="kv-rows" style={{ maxWidth: 860 }}>
        <KvRow icon={<Server size={14} aria-hidden />} label="Hostname" mono value={node.hostname} copy={node.hostname} />
        <KvRow icon={<NetworkIcon size={14} aria-hidden />} label="LAN IP" mono value={node.ip} copy={node.ip} />
        {node.meshIp && (
          <KvRow icon={<NetworkIcon size={14} aria-hidden />} label="Mesh IP (WireGuard)" mono value={node.meshIp} copy={node.meshIp} />
        )}
        {node.publicIp && (
          <KvRow icon={<Globe size={14} aria-hidden />} label="Public IP" mono value={node.publicIp} copy={node.publicIp} />
        )}
        <KvRow icon={<Terminal size={14} aria-hidden />} label="Kernel" mono value={node.kernel} />
        <KvRow icon={<Cpu size={14} aria-hidden />} label="CPU"
          value={`${shortCpu(hw.cpuModel)} (${hw.cpuCores}C/${hw.cpuThreads}T)`} />
        {node.agentVersion && (
          <KvRow icon={<Package size={14} aria-hidden />} label="Agent version" mono value={node.agentVersion} />
        )}
        {/* Uptime frozen at the last report would read as still counting. */}
        {node.status === 'online' && (
          <KvRow icon={<Clock size={14} aria-hidden />} label="Uptime" value={formatUptime(m.uptimeSec)} />
        )}
        <KvRow icon={<CalendarDays size={14} aria-hidden />} label="Joined"
          value={`${new Date(node.joinedAt).toLocaleDateString()} (${timeAgo(node.joinedAt)})`} />
        <KvRow icon={<Clock size={14} aria-hidden />} label="Last seen" value={timeAgo(node.lastSeen)} />
      </div>

      <div className="section-title"><Boxes size={15} aria-hidden /> Apps on this machine</div>
      {apps.length > 0 ? (
        <div className="list">
          {apps.map((s) => (
            <div key={s.id} className="list-row">
              <Led status={s.status} label />
              <Link to={`/apps/${s.id}`} className="t-name" style={{ color: 'inherit', fontWeight: 500 }}>
                {s.name}
              </Link>
              <span className="faint mono" style={{ marginLeft: 'auto', flex: 'none' }}>
                {s.status === 'running' ? `${s.cpuPct.toFixed(0)}% cpu · ${s.memUsedMb} MB` : s.status}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <EmptyState slim title="Nothing deployed here yet">
          {/* The header's "Deploy to this machine" is the one deploy action on
              this page; the empty state points at it instead of cloning it. */}
          <div>Use <strong>Deploy to this machine</strong> above, or let auto-placement pick this box for a new app.</div>
        </EmptyState>
      )}

      {gpus.length > 0 && (
        <>
          <div className="section-title"><Zap size={15} aria-hidden /> GPUs &amp; accelerators</div>
          <DataTable<GPU>
            columns={gpuColumns}
            rows={gpus}
            rowKey={(g) => String(g.index)}
            caption={`GPUs on ${node.name}`}
          />
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
        <DataTable<Drive>
          columns={driveColumns}
          rows={drives}
          rowKey={(d) => d.device}
          caption={`Drives on ${node.name}`}
        />
      ) : (
        <EmptyState slim title="No drives discovered">
          <div>The agent hasn&rsquo;t reported any block devices on this machine yet.</div>
        </EmptyState>
      )}

      <div className="section-title"><NetworkIcon size={15} aria-hidden /> Network interfaces</div>
      {nics.length > 0 ? (
        <div className="list">
          {nics.map((nic) => (
            <div className="list-row" key={nic.name}>
              <span className="mono" style={{ fontWeight: 500 }}>{nic.name}</span>
              <span className="faint mono">{nic.mac}</span>
              <span className="dim mono" style={{ marginLeft: 'auto' }}>↓ {nic.rxMbps.toFixed(1)} MB/s</span>
              <span className="dim mono">↑ {nic.txMbps.toFixed(1)} MB/s</span>
              {nic.speedMb > 0 && <span className="faint" style={{ flex: 'none' }}>{nic.speedMb} Mb link</span>}
            </div>
          ))}
        </div>
      ) : (
        <EmptyState slim title="No interfaces reported">
          <div>The agent hasn&rsquo;t reported any network interfaces for this machine yet.</div>
        </EmptyState>
      )}
    </div>
  );
}
