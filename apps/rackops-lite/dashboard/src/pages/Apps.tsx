import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Boxes, Cloud, Play, Rocket, RotateCw, ScrollText, Search, Square } from 'lucide-react';
import { api } from '../api';
import type { Service } from '../api';
import { useData } from '../data';
import { useConfirm } from '../components/confirm';
import { cleanError, useToast } from '../components/toast';
import { DataTable } from '../components/Table';
import type { Column } from '../components/Table';
import {
  Crumbs, EmptyState, Led, Pill, RowMenu, Seg, SkeletonRows, UrlChip, ledClass, publicUrls,
} from '../components/ui';
import type { RowMenuItem } from '../components/ui';
import { MigrationWizard } from '../components/MigrationWizard';

// AppsPage: the fleet's comparison surface. Every workload in one sortable
// table — status, machine, live resource use, and where it's reachable —
// with lifecycle verbs one kebab away and the full detail one click away.

// Severity order for the status column: ascending puts problems on top
// (failed, then in-flight, then stopped, then running).
const STATUS_RANK: Record<ReturnType<typeof ledClass>, number> = {
  crit: 0, warn: 1, off: 2, ok: 3,
};

// Status segments. Bucketing goes through ledClass so every backend status
// lands in exactly one segment: building/pending ride with Running (they are
// on their way up), crit reads as Failed, off as Stopped.
type StatusFilter = 'all' | 'running' | 'stopped' | 'failed';
const FILTERS: ReadonlyArray<readonly [StatusFilter, string]> = [
  ['all', 'All'], ['running', 'Running'], ['stopped', 'Stopped'], ['failed', 'Failed'],
];
const FILTER_TEST: Record<StatusFilter, (s: Service) => boolean> = {
  all: () => true,
  running: (s) => { const c = ledClass(s.status); return c === 'ok' || c === 'warn'; },
  stopped: (s) => ledClass(s.status) === 'off',
  failed: (s) => ledClass(s.status) === 'crit',
};

function formatMem(mb: number): string {
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${Math.round(mb)} MB`;
}

// What the app is built from, most human name first: template, then image,
// then git repo; compose stacks carry their kind.
function source(s: Service): string {
  return s.templateId || s.image || s.gitRepo || `${s.kind} stack`;
}

export function AppsPage() {
  const { services, ingress, nodeName, loaded, refresh } = useData();
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<StatusFilter>('all');
  const [showMigrationWizard, setShowMigrationWizard] = useState(false);
  const [selectedMigrationApp, setSelectedMigrationApp] = useState<string | undefined>();

  const query = search.trim().toLowerCase();
  const visible = services.filter((s) =>
    (!query || `${s.name} ${s.image ?? ''} ${s.gitRepo ?? ''} ${s.templateId ?? ''}`.toLowerCase().includes(query))
    && FILTER_TEST[filter](s));

  // Rows enter the table in name order; DataTable's sort is stable, so ties
  // inside the failing-first default always resolve alphabetically and rows
  // never shuffle between telemetry ticks.
  const rows = [...visible].sort((a, b) => a.name.localeCompare(b.name));

  // Same call, same confirm, same copy as the command palette's lifecycle
  // verbs: stop pauses for a routine confirm (Enter accepts); start and
  // restart never confirm.
  const act = async (s: Service, action: 'start' | 'stop' | 'restart') => {
    if (action === 'stop') {
      const ok = await confirm({
        title: `Stop ${s.name}?`,
        body: `${s.name} stops serving until you start it again.`,
        confirmLabel: 'Stop app',
      });
      if (!ok) return;
    }
    try {
      await api.serviceAction(s.id, action);
      toast.ok(action === 'stop' ? `${s.name} stopping.` : action === 'start' ? `${s.name} starting.` : `${s.name} restarting.`);
      refresh();
    } catch (e) { toast.err(cleanError(e)); }
  };

  // The row kebab. No lifecycle verbs while a deploy is in flight (the server
  // would refuse them anyway); Logs is always reachable.
  const menuFor = (s: Service): RowMenuItem[] => {
    const working = s.status === 'building' || s.status === 'pending';
    const items: RowMenuItem[] = [];
    if (!working) {
      items.push(s.status === 'running'
        ? { label: 'Stop', icon: <Square size={13} aria-hidden />, onSelect: () => { void act(s, 'stop'); } }
        : { label: 'Start', icon: <Play size={13} aria-hidden />, onSelect: () => { void act(s, 'start'); } });
      items.push({ label: 'Restart', icon: <RotateCw size={13} aria-hidden />, onSelect: () => { void act(s, 'restart'); } });
    }
    items.push({ label: 'Logs', icon: <ScrollText size={13} aria-hidden />, onSelect: () => navigate(`/apps/${s.id}/logs`) });
    items.push({
      label: 'Migrate to Cloud',
      icon: <Cloud size={13} aria-hidden />,
      onSelect: () => {
        setSelectedMigrationApp(s.id);
        setShowMigrationWizard(true);
      },
    });
    return items;
  };

  const columns: Column<Service>[] = [
    {
      key: 'name',
      label: 'Name',
      sortValue: (s) => s.name.toLowerCase(),
      render: (s) => (
        <>
          <span className="cell-main">
            <Led status={s.status} label />
            {/* A real link inside the clickable row: reachable by Tab, openable
                with Enter, middle-clickable into a new tab. */}
            <Link to={`/apps/${s.id}`} className="t-name" style={{ color: 'inherit' }}
              onClick={(e) => e.stopPropagation()}>
              {s.name}
            </Link>
            {s.kind === 'compose' && <span className="tag">stack</span>}
          </span>
          <span className="cell-sub mono">{source(s)}</span>
        </>
      ),
    },
    {
      key: 'where',
      label: 'Where',
      sortValue: (s) => nodeName(s.nodeId).toLowerCase(),
      render: (s) => (
        <>
          <span className="dim">{nodeName(s.nodeId)}</span>
          {s.port > 0 && <span className="cell-sub mono">:{s.port}</span>}
        </>
      ),
    },
    {
      key: 'access',
      label: 'Access',
      render: (s) => {
        const urls = publicUrls(s, ingress);
        return urls.length > 0 ? <UrlChip url={urls[0].url} /> : <span className="dim">private</span>;
      },
    },
    {
      key: 'status',
      label: 'Status',
      width: 120,
      sortValue: (s) => STATUS_RANK[ledClass(s.status)],
      render: (s) => <Pill status={s.status} />,
    },
    {
      key: 'cpu',
      label: 'CPU %',
      num: true,
      width: 80,
      sortValue: (s) => (s.status === 'running' ? s.cpuPct : -1),
      render: (s) => (s.status === 'running' ? s.cpuPct.toFixed(0) : ''),
    },
    {
      key: 'mem',
      label: 'Memory',
      num: true,
      width: 100,
      sortValue: (s) => (s.status === 'running' ? s.memUsedMb : -1),
      render: (s) => (s.status === 'running' ? formatMem(s.memUsedMb) : ''),
    },
    {
      key: 'actions',
      label: <span className="sr-only">Actions</span>,
      width: 56,
      render: (s) => <RowMenu label={`Actions for ${s.name}`} items={menuFor(s)} />,
    },
  ];

  return (
    <div className="page">
      <Crumbs items={[{ label: 'Apps', icon: <Boxes size={13} aria-hidden /> }]} />
      {/* Count in the title once the fleet has answered; a number invented
          before `loaded` would be a guess. */}
      <div className="page-title">
        {loaded ? `${services.length} ${services.length === 1 ? 'App' : 'Apps'}` : 'Apps'}
      </div>
      <div className="page-sub">Everything deployed on your rack, healthiest last.</div>

      <div className="toolbar">
        <div className="gsearch">
          <Search size={14} aria-hidden />
          <input value={search} placeholder="Search apps…" aria-label="Search apps"
            onChange={(e) => setSearch(e.target.value)} />
        </div>
        <Seg value={filter} onChange={setFilter} options={FILTERS} label="Filter by status" />
        <button type="button" className="icon-sq" onClick={() => refresh()}
          aria-label="Refresh apps" title="Refresh apps">
          <RotateCw size={14} />
        </button>
        <span className="grow" />
        <button className="btn" onClick={() => { setSelectedMigrationApp(undefined); setShowMigrationWizard(true); }}>
          <Cloud size={14} aria-hidden /> Move to Cloud
        </button>
        <button className="btn primary" onClick={() => navigate('/apps/new')}>
          <Rocket size={14} aria-hidden /> Deploy app
        </button>
      </div>

      {/* Waiting for the first load is not the same as having no apps. */}
      {!loaded && services.length === 0 && <SkeletonRows count={4} height={52} />}

      {rows.length > 0 && (
        <DataTable<Service>
          columns={columns}
          rows={rows}
          rowKey={(s) => s.id}
          onRowClick={(s) => navigate(`/apps/${s.id}`)}
          defaultSort={{ key: 'status', dir: 'asc' }}
          caption="Apps with status, machine, resource use, and where they're reachable"
        />
      )}

      {/* Active filters say what they hid — a shortened table with no note
          reads as the whole fleet. */}
      {rows.length > 0 && rows.length < services.length && (
        <div className="faint" style={{ padding: '10px 2px' }} role="status">
          Showing {rows.length} of {services.length} apps
        </div>
      )}

      {/* The deploy action stays in the toolbar; the empty state only points. */}
      {loaded && services.length === 0 && (
        <EmptyState icon={<Boxes size={18} />} title="No apps yet">
          <div>Pick from 45+ ready-made templates, or bring your own image, git repo, or compose file.</div>
          <div><strong>Deploy app</strong> in the toolbar starts the flow.</div>
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
      {showMigrationWizard && (
        <MigrationWizard
          preselectedServiceId={selectedMigrationApp}
          onClose={() => setShowMigrationWizard(false)}
        />
      )}
    </div>
  );
}
