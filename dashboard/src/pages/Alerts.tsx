import { Bell, CheckCircle2 } from 'lucide-react';
import type { Alert, Node } from '../api';

export function AlertsPage({ alerts, nodes }: { alerts: Alert[]; nodes: Node[] }) {
  const open = alerts.filter((a) => !a.resolved);
  const nodeName = (id?: string) => nodes.find((n) => n.id === id)?.name ?? id ?? 'cluster';

  return (
    <>
      <div className="page-title">Alerts</div>
      <div className="page-sub">Threshold and health events raised by the alert engine</div>

      {open.length === 0 ? (
        <div className="empty">
          <CheckCircle2 size={28} style={{ marginBottom: 10, color: 'var(--green)' }} />
          <div>All clear — no open alerts.</div>
          <div className="faint" style={{ marginTop: 6 }}>
            Watching: CPU &gt; 90%, memory &gt; 90%, package temp &gt; 85°C, drives &gt; 92% full, nodes going dark.
          </div>
        </div>
      ) : (
        <div className="node-card">
          {open.map((a) => (
            <div className="card" key={a.id}>
              <div className="row spread">
                <div className="row">
                  <Bell size={14} color={a.level === 'critical' ? 'var(--red)' : 'var(--yellow)'} />
                  <strong>{a.message}</strong>
                </div>
                <span className={`badge ${a.level === 'critical' ? 'red' : 'yellow'}`}>{a.level}</span>
              </div>
              <div className="row" style={{ marginTop: 8 }}>
                <span className="tag">{a.kind}</span>
                <span className="dim">{nodeName(a.nodeId)}</span>
                <span className="faint">{new Date(a.createdAt).toLocaleString()}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  );
}
