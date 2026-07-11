import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Play, Square, RotateCw, Rocket, Search } from 'lucide-react';
import { api } from '../api';
import type { Service } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { Led, StatusBadge, UrlChip, publicUrls } from '../components/ui';

// AppsPage: every workload, its health, and where it's reachable — with the
// full detail one click away.
export function AppsPage() {
  const { services, nodes, ingress, refresh, nodeName } = useData();
  const navigate = useNavigate();
  const toast = useToast();
  const [search, setSearch] = useState('');
  const [busy, setBusy] = useState('');

  const act = async (e: React.MouseEvent, s: Service, action: string) => {
    e.stopPropagation();
    setBusy(s.id);
    try {
      await api.serviceAction(s.id, action);
      toast.ok(action === 'stop' ? `${s.name} stopping.` : action === 'start' ? `${s.name} starting.` : `${s.name} restarting.`);
      refresh();
    } catch (err) { toast.err(cleanError(err)); }
    setBusy('');
  };

  const filtered = services.filter((s) =>
    !search || (s.name + (s.image ?? '') + (s.domain ?? '')).toLowerCase().includes(search.toLowerCase()));

  const failingFirst = [...filtered].sort((a, b) => {
    const rank = (s: Service) => (s.status === 'failed' ? 0 : s.status === 'running' ? 1 : 2);
    return rank(a) - rank(b) || a.name.localeCompare(b.name);
  });

  return (
    <div className="page">
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Apps</div>
        <button className="btn primary" onClick={() => navigate('/apps/new')}><Rocket size={14} /> Deploy app</button>
      </div>
      <div className="page-sub">Everything running on your rack, and where it's reachable</div>

      {services.length > 5 && (
        <div className="row" style={{ marginBottom: 16 }}>
          <div style={{ position: 'relative', width: 280 }}>
            <Search size={13} style={{ position: 'absolute', left: 11, top: 10, color: 'var(--faint)' }} />
            <input className="input" style={{ paddingLeft: 32 }} placeholder="Filter apps…"
              value={search} onChange={(e) => setSearch(e.target.value)} />
          </div>
        </div>
      )}

      <div className="stack" style={{ gap: 8 }}>
        {failingFirst.map((s) => {
          const urls = publicUrls(s, ingress);
          const node = nodes.find((n) => n.id === s.nodeId);
          return (
            <div key={s.id} className="card clickable" style={{ padding: '13px 16px' }} onClick={() => navigate(`/apps/${s.id}`)}>
              <div className="row spread">
                <div className="row" style={{ minWidth: 0 }}>
                  <strong style={{ fontFamily: 'var(--display)', fontSize: 14.5 }}>{s.name}</strong>
                  <StatusBadge status={s.status} />
                  {s.kind === 'compose' && <span className="tag">stack</span>}
                </div>
                <div className="row" onClick={(e) => e.stopPropagation()}>
                  {s.status === 'running' ? (
                    <button className="btn sm" disabled={busy === s.id} title="Stop" onClick={(e) => act(e, s, 'stop')}><Square size={12} /></button>
                  ) : (
                    <button className="btn sm" disabled={busy === s.id} title="Start" onClick={(e) => act(e, s, 'start')}><Play size={12} /></button>
                  )}
                  <button className="btn sm" disabled={busy === s.id} title="Restart" onClick={(e) => act(e, s, 'restart')}><RotateCw size={12} /></button>
                </div>
              </div>
              <div className="row wrap" style={{ marginTop: 9, gap: 10 }}>
                {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
                {urls.length === 0 && s.port > 0 && node && s.status === 'running' && (
                  <UrlChip url={`http://${node.ip}:${s.port}`} />
                )}
                {urls.length === 0 && (s.port === 0 || s.status !== 'running') && (
                  <span className="faint">no public URL yet</span>
                )}
                <span className="faint mono" style={{ marginLeft: 'auto' }}>
                  {s.image || s.gitRepo || s.kind} · {nodeName(s.nodeId)}
                  {s.status === 'running' && ` · ${s.cpuPct.toFixed(1)}% cpu · ${s.memUsedMb} MB`}
                </span>
              </div>
            </div>
          );
        })}

        {services.length === 0 && (
          <div className="empty" style={{ padding: '60px 20px' }}>
            <Rocket size={26} style={{ marginBottom: 10 }} />
            <div className="empty-title">Nothing deployed yet</div>
            <div style={{ marginBottom: 16, lineHeight: 1.6 }}>
              Pick from 45+ ready-made templates — media servers, databases, AI tools —<br />
              or bring your own image, git repo, or compose file.
            </div>
            <button className="btn primary" onClick={() => navigate('/apps/new')}>
              <Rocket size={13} /> Deploy your first app
            </button>
          </div>
        )}
        {services.length > 0 && filtered.length === 0 && (
          <div className="empty">Nothing matches "{search}".</div>
        )}
      </div>

      {/* Legend for new users: what the LEDs mean */}
      {services.length > 0 && (
        <div className="row" style={{ marginTop: 18, gap: 16 }}>
          <span className="faint row" style={{ gap: 5 }}><Led status="running" /> running</span>
          <span className="faint row" style={{ gap: 5 }}><Led status="building" /> working</span>
          <span className="faint row" style={{ gap: 5 }}><Led status="failed" /> down — auto-heal retries, check logs</span>
          <span className="faint row" style={{ gap: 5 }}><Led status="stopped" /> stopped</span>
        </div>
      )}
    </div>
  );
}
