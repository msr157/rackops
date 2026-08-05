import { useState } from 'react';
import { Activity as ActivityIcon, AlertTriangle, HardDrive, Radio, Rocket } from 'lucide-react';
import { useData } from '../data';
import { Crumbs, EmptyState, Seg, SkeletonRows, timeAgo } from '../components/ui';

// Feed icons are neutral wayfinding, not status: color never decorates, and
// anything that truly needs attention lives on Home's attention list.
function feedIcon(kind: string) {
  if (kind.includes('alert')) return <AlertTriangle size={14} />;
  if (kind.includes('node') || kind.includes('machine')) return <HardDrive size={14} />;
  if (kind.includes('deploy') || kind.includes('service')) return <Rocket size={14} />;
  if (kind.includes('heal')) return <ActivityIcon size={14} />;
  return <Radio size={14} />;
}

type Filter = 'all' | 'apps' | 'machines' | 'alerts' | 'workspace';

// Kinds group by what the operator was doing, not by which handler emitted
// them: everything about running software is "apps", everything about the
// fleet is "machines", people-and-settings events are "workspace".
const APP_KINDS = ['deploy', 'service', 'tunnel', 'ingress', 'domain', 'migrate', 'heal'];
const MACHINE_KINDS = ['node', 'machine', 'cluster'];

function matches(filter: Filter, kind: string): boolean {
  switch (filter) {
    case 'all': return true;
    case 'apps': return APP_KINDS.some((k) => kind.includes(k));
    case 'machines': return MACHINE_KINDS.some((k) => kind.includes(k));
    case 'alerts': return kind.includes('alert');
    case 'workspace':
      return !APP_KINDS.some((k) => kind.includes(k))
        && !MACHINE_KINDS.some((k) => kind.includes(k))
        && !kind.includes('alert');
  }
}

// ActivityPage: the full event stream, moved off Home so the front page stays
// a status answer and history lives one click away. Holds everything the
// server keeps (the most recent 80 events), filterable by area.
export function ActivityPage() {
  const { events, loaded } = useData();
  const [filter, setFilter] = useState<Filter>('all');

  const shown = events.filter((e) => matches(filter, e.kind));

  return (
    <div className="page" style={{ maxWidth: 850 }}>
      <Crumbs items={[{ label: 'Activity', icon: <ActivityIcon size={13} aria-hidden /> }]} />
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title" style={{ marginBottom: 0 }}>Activity</div>
        <Seg<Filter> label="Filter activity" value={filter} onChange={setFilter}
          options={[['all', 'All'], ['apps', 'Apps'], ['machines', 'Machines'], ['alerts', 'Alerts'], ['workspace', 'Workspace']]} />
      </div>
      <div className="page-sub">
        Everything that happened across your rack: deploys, heals, machines joining, alerts firing and clearing.
      </div>

      {!loaded ? (
        <SkeletonRows count={6} height={36} />
      ) : events.length === 0 ? (
        <EmptyState title="Quiet so far">
          <span className="dim">Activity from deploys, heals, and machines appears here as it happens.</span>
        </EmptyState>
      ) : shown.length === 0 ? (
        <EmptyState slim title="Nothing in this filter yet">
          <span className="dim">Recent activity is under another filter.</span>
        </EmptyState>
      ) : (
        <div className="feed">
          {shown.map((e) => (
            <div key={e.id} className="feed-item">
              <span style={{ color: 'var(--faint)', flex: 'none', paddingTop: 1 }} aria-hidden>
                {feedIcon(e.kind)}
              </span>
              <span style={{ flex: 1 }}>{e.message}</span>
              <span className="feed-time">{timeAgo(e.createdAt)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
