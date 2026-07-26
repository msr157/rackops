import { useId, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Rocket, Search } from 'lucide-react';
import type { Service } from '../api';
import { useData } from '../data';
import { DataTable } from '../components/Table';
import type { Column } from '../components/Table';
import { EmptyState, Pill, SkeletonRows, UrlChip, ledClass, publicUrls } from '../components/ui';

// AppsPage: the fleet's comparison surface. Every workload in one sortable
// table — status, machine, live resource use, and where it's reachable —
// with the full detail one click away. Rows are pure navigation: lifecycle
// actions live on the app detail header and in the command palette, so
// nothing on this list can be mis-clicked while scanning.

// Severity order for the status column: ascending puts problems on top
// (failed, then in-flight, then stopped, then running).
const STATUS_RANK: Record<ReturnType<typeof ledClass>, number> = {
  crit: 0, warn: 1, off: 2, ok: 3,
};

// Status filter groups. Grouping goes through ledClass so every backend
// status lands in exactly one chip: ok/warn (running + building/pending)
// read as "Running", crit as "Needs attention", off as "Stopped".
type StatusFilter = 'all' | 'running' | 'attention' | 'stopped';
const STATUS_GROUPS: { id: StatusFilter; label: string; test: (s: Service) => boolean }[] = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'running', label: 'Running', test: (s) => { const c = ledClass(s.status); return c === 'ok' || c === 'warn'; } },
  { id: 'attention', label: 'Needs attention', test: (s) => ledClass(s.status) === 'crit' },
  { id: 'stopped', label: 'Stopped', test: (s) => ledClass(s.status) === 'off' },
];

function formatMem(mb: number): string {
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${Math.round(mb)} MB`;
}

export function AppsPage() {
  const { services, ingress, nodes, nodeName, loaded } = useData();
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<StatusFilter>('all');
  const searchId = useId();

  const group = STATUS_GROUPS.find((g) => g.id === filter) ?? STATUS_GROUPS[0];
  const query = search.trim().toLowerCase();
  const visible = services.filter((s) =>
    (!query || `${s.name} ${s.image ?? ''} ${s.domain ?? ''}`.toLowerCase().includes(query))
    && group.test(s));

  // Rows enter the table in name order; DataTable's sort is stable, so ties
  // inside any sorted column always resolve alphabetically and rows never
  // shuffle arbitrarily between telemetry ticks.
  const rows = [...visible].sort((a, b) => a.name.localeCompare(b.name));

  // The filter row earns its place once there are 2+ apps, but its slot is
  // always reserved: appearing after the fleet loads must not shove the table
  // down mid-read. visibility keeps the footprint without focusable ghosts.
  const showFilter = loaded && services.length > 1;

  const columns: Column<Service>[] = [
    {
      key: 'name',
      label: 'App',
      sortValue: (s) => s.name.toLowerCase(),
      render: (s) => (
        <span className="row" style={{ gap: 8, minWidth: 0 }}>
          {/* A real link inside the clickable row: reachable by Tab, openable
              with Enter, middle-clickable into a new tab. */}
          <Link to={`/apps/${s.id}`} className="t-name" style={{ color: 'inherit', fontWeight: 600 }}
            onClick={(e) => e.stopPropagation()}>
            {s.name}
          </Link>
          {s.kind === 'compose' && <span className="tag">stack</span>}
        </span>
      ),
    },
    {
      key: 'status',
      label: 'Status',
      width: 130,
      sortValue: (s) => STATUS_RANK[ledClass(s.status)],
      render: (s) => <Pill status={s.status} />,
    },
    {
      key: 'machine',
      label: 'Machine',
      sortValue: (s) => nodeName(s.nodeId).toLowerCase(),
      render: (s) => <span className="dim">{nodeName(s.nodeId)}</span>,
    },
    {
      key: 'cpu',
      label: 'CPU',
      num: true,
      width: 70,
      sortValue: (s) => (s.status === 'running' ? s.cpuPct : -1),
      render: (s) => (s.status === 'running' ? `${s.cpuPct.toFixed(0)}%` : ''),
    },
    {
      key: 'mem',
      label: 'Memory',
      num: true,
      width: 90,
      sortValue: (s) => (s.status === 'running' ? s.memUsedMb : -1),
      render: (s) => (s.status === 'running' ? formatMem(s.memUsedMb) : ''),
    },
    {
      key: 'url',
      label: 'URL',
      render: (s) => {
        const urls = publicUrls(s, ingress);
        if (urls.length > 0) return <UrlChip url={urls[0].url} />;
        // No public URL, but on a homelab the LAN address IS the address.
        const ip = nodes.find((n) => n.id === s.nodeId)?.ip;
        if (s.status === 'running' && s.port > 0 && ip) {
          return <UrlChip url={`http://${ip}:${s.port}`} />;
        }
        return <span className="faint">not public yet</span>;
      },
    },
  ];

  return (
    <div className="page">
      <div className="page-title">Apps</div>
      <div className="page-sub">Everything running on your rack, and where it's reachable</div>

      <div className="row wrap" style={{ gap: 8, marginBottom: 16, visibility: showFilter ? 'visible' : 'hidden' }}>
        <div className="search-input" style={{ width: 260, maxWidth: '100%' }}>
          <Search size={13} aria-hidden />
          <label className="sr-only" htmlFor={searchId}>Filter apps by name</label>
          <input className="input" id={searchId} placeholder="Filter apps…"
            value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        <div className="row" role="group" aria-label="Filter by status" style={{ gap: 6 }}>
          {STATUS_GROUPS.map((g) => (
            <button key={g.id} type="button"
              className={`btn sm ${filter === g.id ? 'chip-on' : ''}`}
              aria-pressed={filter === g.id}
              onClick={() => setFilter(g.id)}>
              {g.label}
            </button>
          ))}
        </div>
      </div>

      {/* Waiting for the first load is not the same as having no apps. */}
      {!loaded && services.length === 0 && <SkeletonRows count={4} height={46} />}

      {rows.length > 0 && (
        <DataTable<Service>
          columns={columns}
          rows={rows}
          rowKey={(s) => s.id}
          onRowClick={(s) => navigate(`/apps/${s.id}`)}
          defaultSort={{ key: 'status', dir: 'asc' }}
          caption="Apps with status, machine, resource use, and public URL"
        />
      )}

      {loaded && services.length === 0 && (
        <EmptyState icon={<Rocket size={18} />} title="Nothing deployed yet">
          <div style={{ maxWidth: 460, margin: '0 auto 16px', lineHeight: 1.6 }}>
            Pick from 45+ ready-made templates (media servers, databases, AI tools)
            or bring your own image, git repo, or compose file.
          </div>
          <button className="btn primary" onClick={() => navigate('/apps/new')}>
            <Rocket size={13} aria-hidden /> Deploy your first app
          </button>
        </EmptyState>
      )}

      {services.length > 0 && rows.length === 0 && (
        <div className="row" style={{ gap: 10, padding: '14px 2px' }}>
          <span className="faint">No apps match the current filters.</span>
          <button className="btn sm" onClick={() => { setSearch(''); setFilter('all'); }}>
            Clear filters
          </button>
        </div>
      )}
    </div>
  );
}
