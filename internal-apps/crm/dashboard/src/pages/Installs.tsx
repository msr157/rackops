import { useEffect, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { Server, ArrowLeft, Inbox, History, Cpu } from 'lucide-react';
import { api } from '../api';
import type { Install, InstallDetail } from '../api';
import { EmptyState, Kpi, SeverityBadge, StatusBadge, timeAgo, fmtDate, isActive } from '../components/ui';

export function InstallsPage() {
  const [installs, setInstalls] = useState<Install[] | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    let alive = true;
    const load = () => api.installs().then((r) => { if (alive) setInstalls(r); }).catch(() => {});
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, []);

  if (!installs) return null;

  return (
    <div className="page">
      <div className="page-title">Installs</div>
      <div className="page-sub">Every self-hosted RackOps Lite instance that has phoned home.</div>

      {installs.length === 0 ? (
        <EmptyState icon={<Server size={22} />} title="No installs yet">
          Instances appear when they forward a ticket or post a usage snapshot.
        </EmptyState>
      ) : (
        <div className="card" style={{ padding: 0 }}>
          <table className="table">
            <thead>
              <tr>
                <th>Name</th><th>Version</th><th>Nodes</th><th>Services</th>
                <th>Features</th><th>Tickets</th><th>Last seen</th>
              </tr>
            </thead>
            <tbody>
              {installs.map((i) => (
                <tr key={i.id} onClick={() => navigate(`/installs/${i.id}`)}>
                  <td>
                    <span className="row" style={{ gap: 8 }}>
                      <span className={`led ${isActive(i.lastSeen) ? 'ok' : 'off'}`} />
                      <span style={{ fontWeight: 500 }}>{i.name || i.id.slice(0, 8)}</span>
                    </span>
                  </td>
                  <td className="mono dim">{i.version ? `v${i.version}` : '—'}</td>
                  <td className="mono dim">{i.usage ? `${i.usage.nodesOnline}/${i.usage.nodes}` : '—'}</td>
                  <td className="mono dim">{i.usage?.services ?? '—'}</td>
                  <td>
                    <span className="row wrap" style={{ gap: 4 }}>
                      {(i.usage?.featuresUsed ?? []).slice(0, 4).map((f) => <span key={f} className="tag">{f}</span>)}
                      {(i.usage?.featuresUsed?.length ?? 0) > 4 && (
                        <span className="faint">+{i.usage!.featuresUsed!.length - 4}</span>
                      )}
                    </span>
                  </td>
                  <td>{i.openTickets > 0 ? <span className="badge red">{i.openTickets} open</span> : <span className="faint">—</span>}</td>
                  <td className="dim mono">{timeAgo(i.lastSeen)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function InstallDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [detail, setDetail] = useState<InstallDetail | null>(null);
  const [error, setError] = useState('');
  const navigate = useNavigate();

  useEffect(() => {
    if (!id) return;
    let alive = true;
    const load = () => api.install(id)
      .then((r) => { if (alive) { setDetail(r); setError(''); } })
      .catch((e) => { if (alive) setError(String(e)); });
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, [id]);

  if (error && !detail) return <EmptyState title="Install not found">{error}</EmptyState>;
  if (!detail) return null;

  const { install: i, snapshots, tickets } = detail;
  const u = i.usage;

  return (
    <div className="page">
      <Link to="/installs" className="row dim" style={{ gap: 6, marginBottom: 12 }}>
        <ArrowLeft size={14} /> All installs
      </Link>
      <div className="row" style={{ gap: 10, marginBottom: 2 }}>
        <span className={`led lg ${isActive(i.lastSeen) ? 'ok' : 'off'}`} />
        <div className="page-title" style={{ marginBottom: 0 }}>{i.name || i.id}</div>
      </div>
      <div className="page-sub mono">
        {i.id} · {i.os || '?'}/{i.arch || '?'} · first seen {fmtDate(i.firstSeen)} · last seen {timeAgo(i.lastSeen)}
      </div>

      <div className="kpi-row">
        <Kpi icon={<Cpu size={12} />} label="Version" value={i.version ? `v${i.version}` : '—'} />
        <Kpi label="Nodes" value={u ? `${u.nodesOnline}/${u.nodes}` : '—'} unit={u ? 'online' : undefined} />
        <Kpi label="Services" value={u?.services ?? '—'} />
        <Kpi label="Users" value={u?.users ?? '—'} />
        <Kpi label="Domains" value={u?.domains ?? '—'} />
      </div>

      {u && (
        <div className="card" style={{ marginBottom: 14 }}>
          <div className="section-title" style={{ marginTop: 0 }}>Capabilities</div>
          <div className="row wrap">
            {Object.entries(u.servicesByKind ?? {}).map(([k, n]) => <span key={k} className="tag">{k}: {n}</span>)}
            {u.edgeEnabled && <span className="badge amber">edge</span>}
            {u.meshEnabled && <span className="badge amber">mesh</span>}
            {u.ha && <span className="badge purple">HA</span>}
            {u.adopted > 0 && <span className="badge gray">{u.adopted} adopted</span>}
            {(u.featuresUsed ?? []).map((f) => <span key={f} className="tag">{f}</span>)}
          </div>
        </div>
      )}

      <div className="grid-2">
        <div>
          <div className="section-title"><Inbox size={14} /> Tickets ({tickets.length})</div>
          {tickets.length === 0 ? (
            <EmptyState title="No tickets from this install" />
          ) : (
            <div className="stack">
              {tickets.map((t) => (
                <div key={t.id} className="card clickable" onClick={() => navigate(`/tickets/${t.installId}/${t.id}`)}>
                  <div className="row spread">
                    <span style={{ fontWeight: 500 }}>{t.subject}</span>
                    <StatusBadge status={t.status} />
                  </div>
                  <div className="row" style={{ marginTop: 6 }}>
                    <SeverityBadge severity={t.severity} />
                    <span className="tag">{t.category}</span>
                    <span className="faint mono" style={{ marginLeft: 'auto' }}>{timeAgo(t.updatedAt)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div>
          <div className="section-title"><History size={14} /> Snapshot history</div>
          {snapshots.length === 0 ? (
            <EmptyState title="No snapshots yet" />
          ) : (
            <div className="card" style={{ padding: 0 }}>
              <table className="table">
                <thead>
                  <tr><th>When</th><th>Version</th><th>Nodes</th><th>Services</th><th>Features</th></tr>
                </thead>
                <tbody>
                  {snapshots.map((s, idx) => (
                    <tr key={idx} style={{ cursor: 'default' }}>
                      <td className="dim mono">{fmtDate(s.at)}</td>
                      <td className="mono">{s.data?.version ? `v${s.data.version}` : '—'}</td>
                      <td className="mono dim">{s.data ? `${s.data.nodesOnline}/${s.data.nodes}` : '—'}</td>
                      <td className="mono dim">{s.data?.services ?? '—'}</td>
                      <td className="dim">{(s.data?.featuresUsed ?? []).length}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
