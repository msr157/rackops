import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Activity, Inbox, Layers, Sparkles, GitBranch } from 'lucide-react';
import { api } from '../api';
import type { Overview } from '../api';
import { Kpi, DistBar, SeverityBadge, StatusBadge, EmptyState, timeAgo } from '../components/ui';

export function OverviewPage() {
  const [ov, setOv] = useState<Overview | null>(null);
  const [error, setError] = useState('');
  const navigate = useNavigate();

  useEffect(() => {
    let alive = true;
    const load = () => api.overview()
      .then((o) => { if (alive) { setOv(o); setError(''); } })
      .catch((e) => { if (alive) setError(String(e)); });
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, []);

  if (error && !ov) return <EmptyState title="Couldn't load overview">{error}</EmptyState>;
  if (!ov) return null;

  const kinds = Object.entries(ov.byKindTotals ?? {}).sort((a, b) => b[1] - a[1]);
  const kindTotal = kinds.reduce((n, [, c]) => n + c, 0);
  const recent = ov.recentTickets ?? [];

  return (
    <div className="page">
      <div className="page-title">Overview</div>
      <div className="page-sub">The customer fleet at a glance — installs, activity, and what needs an answer.</div>

      <div className="kpi-row">
        <Kpi icon={<Server size={12} />} label="Installs" value={ov.installs} />
        <Kpi icon={<Activity size={12} />} label="Active this week" value={ov.activeWeek}>
          <div className="bar"><div className="bar-fill ok" style={{ width: `${ov.installs ? (ov.activeWeek / ov.installs) * 100 : 0}%` }} /></div>
        </Kpi>
        <Kpi icon={<Inbox size={12} />} label="Open tickets" value={ov.openTickets}>
          {ov.openTickets > 0 && <div className="faint" style={{ marginTop: 4 }}>waiting on an agent</div>}
        </Kpi>
        <Kpi icon={<Layers size={12} />} label="Services deployed" value={kindTotal} />
      </div>

      <div className="grid-2">
        <div className="card">
          <div className="section-title" style={{ marginTop: 0 }}><GitBranch size={14} /> Version distribution</div>
          {ov.versionDist.length === 0
            ? <div className="faint">No versions reported yet.</div>
            : ov.versionDist.map((v) => (
              <DistBar key={v.version} label={`v${v.version}`} count={v.count} total={ov.installs} suffix=" installs" />
            ))}
        </div>

        <div className="card">
          <div className="section-title" style={{ marginTop: 0 }}><Sparkles size={14} /> Hero features</div>
          <div className="faint" style={{ marginBottom: 10 }}>Share of installs using each feature.</div>
          {ov.featureUsage.length === 0
            ? <div className="faint">No feature telemetry yet.</div>
            : ov.featureUsage.slice(0, 10).map((f) => (
              <DistBar key={f.feature} label={f.feature} count={f.count} total={ov.installs} suffix=" installs" />
            ))}
        </div>
      </div>

      {kinds.length > 0 && (
        <div className="card" style={{ marginTop: 14 }}>
          <div className="section-title" style={{ marginTop: 0 }}><Layers size={14} /> Services by kind</div>
          <div className="row wrap">
            {kinds.map(([kind, count]) => (
              <span key={kind} className="tag">{kind}: {count}</span>
            ))}
          </div>
        </div>
      )}

      <div className="section-title"><Inbox size={14} /> Recent tickets</div>
      {recent.length === 0 ? (
        <EmptyState icon={<Inbox size={22} />} title="No tickets yet">
          Tickets forwarded by customer instances land here.
        </EmptyState>
      ) : (
        <div className="card" style={{ padding: 0 }}>
          <table className="table">
            <thead>
              <tr><th>Subject</th><th>Install</th><th>Severity</th><th>Status</th><th>Updated</th></tr>
            </thead>
            <tbody>
              {recent.map((t) => (
                <tr key={`${t.installId}/${t.id}`} onClick={() => navigate(`/tickets/${t.installId}/${t.id}`)}>
                  <td style={{ fontWeight: 500 }}>{t.subject}</td>
                  <td className="dim">{t.installName || t.installId.slice(0, 8)}</td>
                  <td><SeverityBadge severity={t.severity} /></td>
                  <td><StatusBadge status={t.status} /></td>
                  <td className="dim mono">{timeAgo(t.updatedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
