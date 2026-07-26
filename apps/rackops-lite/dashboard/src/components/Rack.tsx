import { useNavigate } from 'react-router-dom';
import { Server } from 'lucide-react';
import type { Node, Service } from '../api';
import { EmptyState, Led, Skeleton, UsageBar, timeAgo } from './ui';

// Rack renders the fleet as flat hairline-separated rows: status dot (plus
// the word, for screen readers), name, one line of vitals, live meters.
// No U-numbers: slot positions were decoration, not data.
export function Rack({ nodes, services, loading, onAdd }: {
  nodes: Node[]; services: Service[]; loading?: boolean; onAdd?: () => void;
}) {
  const navigate = useNavigate();

  // An unanswered fetch must never render as an empty rack.
  if (loading && nodes.length === 0) {
    return (
      <div className="rack" style={{ padding: 8 }}>
        <div className="stack" style={{ gap: 6 }} aria-hidden>
          {[0, 1, 2].map((i) => <Skeleton key={i} height={50} radius={6} />)}
        </div>
      </div>
    );
  }

  // A truly empty rack is the start of onboarding, not a dead end: callers
  // that can open the add-machine flow get a real call to action instead of
  // prose. The EmptyState replaces the panel so borders never nest.
  if (nodes.length === 0 && onAdd) {
    return (
      <EmptyState icon={<Server size={18} />} title="The rack is empty">
        <div style={{ marginBottom: 14 }}>Add any Linux box: an old PC, a Pi, a VPS.</div>
        <button className="btn primary" onClick={onAdd}>Add your first machine</button>
      </EmptyState>
    );
  }

  return (
    <div className="rack">
      {nodes.map((n) => {
        const apps = services.filter((s) => s.nodeId === n.id);
        const up = apps.filter((s) => s.status === 'running').length;
        const offline = n.status === 'offline';
        return (
          // .rack-unit's left padding used to come from the rail column
          // (padding-left is 0 in the class), so the row supplies its own.
          <button key={n.id} className="rack-unit"
            onClick={() => navigate(`/machines/${n.id}`)}>
            {/* label: the dot is the only status cue on the row, so say the word too */}
            <Led status={n.status} lg label />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="row" style={{ gap: 8 }}>
                <span className="u-name">{n.name}</span>
                {n.role === 'server' && <span className="tag">control</span>}
              </div>
              {/* One middot per line: ip first, then readings comma-joined.
                  Temperature lives here because it is a reading, not a
                  percentage, so it never gets a meter bar. Offline rows swap
                  the live readings for when the box was last heard from:
                  stale numbers presented as live would be a lie. */}
              <div className="faint mono u-sub">
                {offline
                  ? <>{n.ip} · last seen {timeAgo(n.lastSeen)}</>
                  : <>{n.ip} · {apps.length > 0 && `${up}/${apps.length} apps up, `}{n.metrics.tempC.toFixed(0)}°C</>}
              </div>
            </div>
            <div className="u-meter">
              <span className="faint mono">CPU {n.metrics.cpuPct.toFixed(0)}%</span>
              <UsageBar pct={n.metrics.cpuPct} />
            </div>
            <div className="u-meter">
              <span className="faint mono">MEM {n.metrics.memPct.toFixed(0)}%</span>
              <UsageBar pct={n.metrics.memPct} />
            </div>
          </button>
        );
      })}
      {nodes.length === 0 && (
        <div style={{ padding: '30px 16px', textAlign: 'center' }} className="faint">
          The rack is empty. Add your first machine to bring it online.
        </div>
      )}
    </div>
  );
}
