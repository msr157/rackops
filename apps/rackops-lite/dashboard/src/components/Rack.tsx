import { useNavigate } from 'react-router-dom';
import type { Node, Service } from '../api';
import { Led, Skeleton, UsageBar } from './ui';

// Rack renders the fleet as stacked rack units — the signature element.
// Each slat carries the machine's LED, name, live meters, and app count.
export function Rack({ nodes, services, loading }: { nodes: Node[]; services: Service[]; loading?: boolean }) {
  const navigate = useNavigate();

  // An unanswered fetch must never render as an empty rack.
  if (loading && nodes.length === 0) {
    return (
      <div className="rack">
        <div className="stack" style={{ gap: 6 }} aria-hidden>
          {[0, 1, 2].map((i) => <Skeleton key={i} height={54} radius={6} />)}
        </div>
      </div>
    );
  }

  return (
    <div className="rack">
      {nodes.map((n, i) => {
        const apps = services.filter((s) => s.nodeId === n.id);
        const up = apps.filter((s) => s.status === 'running').length;
        return (
          <button key={n.id} className="rack-unit" onClick={() => navigate(`/machines/${n.id}`)}>
            <span className="u-label">U{nodes.length - i}</span>
            {/* label: the LED is the only status cue on a slat, so say the word too */}
            <Led status={n.status} lg label />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="row" style={{ gap: 8 }}>
                <span className="u-name">{n.name}</span>
                {n.role === 'server' && <span className="tag">control</span>}
              </div>
              <div className="faint mono u-sub">{n.ip}{apps.length > 0 && ` · ${up}/${apps.length} apps up`}</div>
            </div>
            <div className="u-meter">
              <span className="faint mono">CPU {n.metrics.cpuPct.toFixed(0)}%</span>
              <UsageBar pct={n.metrics.cpuPct} />
            </div>
            <div className="u-meter">
              <span className="faint mono">MEM {n.metrics.memPct.toFixed(0)}%</span>
              <UsageBar pct={n.metrics.memPct} />
            </div>
            <div className="u-meter">
              <span className="faint mono">{n.metrics.tempC.toFixed(0)}°C</span>
              <UsageBar pct={n.metrics.tempC} />
            </div>
          </button>
        );
      })}
      {nodes.length === 0 && (
        <div style={{ padding: '30px 16px', textAlign: 'center' }} className="faint">
          The rack is empty — add your first machine to bring it online.
        </div>
      )}
    </div>
  );
}
