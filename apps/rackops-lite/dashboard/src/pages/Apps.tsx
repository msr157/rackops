import { useId, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Play, Square, RotateCw, Rocket, Search } from 'lucide-react';
import { api } from '../api';
import type { Service } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { InfoTip, Led, SkeletonRows, Spinner, StatusBadge, UrlChip, publicUrls } from '../components/ui';

// AppsPage: every workload, its health, and where it's reachable — with the
// full detail one click away.
export function AppsPage() {
  const { services, nodes, ingress, refresh, nodeName, loaded } = useData();
  const navigate = useNavigate();
  const toast = useToast();
  const [search, setSearch] = useState('');
  const searchId = useId();
  // busy = "serviceId:action" — disables every action button on that service
  // while one is running, and spins the one that's running.
  const [busy, setBusy] = useState('');

  const act = async (e: React.MouseEvent, s: Service, action: string) => {
    e.stopPropagation();
    setBusy(`${s.id}:${action}`);
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

      {services.length > 1 && (
        <div className="row" style={{ marginBottom: 16 }}>
          <div style={{ position: 'relative', width: 280 }}>
            <Search size={13} style={{ position: 'absolute', left: 11, top: 10, color: 'var(--faint)' }} aria-hidden />
            <label className="sr-only" htmlFor={searchId}>Filter apps</label>
            <input className="input" id={searchId} style={{ paddingLeft: 32 }} placeholder="Filter apps…"
              value={search} onChange={(e) => setSearch(e.target.value)} />
          </div>
        </div>
      )}

      <div className="stack" style={{ gap: 8 }}>
        {failingFirst.map((s) => {
          const urls = publicUrls(s, ingress);
          const node = nodes.find((n) => n.id === s.nodeId);
          // Lock actions for the whole deploy window, not just our own HTTP
          // call — svc.status arrives live over SSE, so this releases the
          // moment the machine reports the container running (or failed).
          const working = s.status === 'building' || s.status === 'pending';
          const locked = busy.startsWith(`${s.id}:`) || working;
          return (
            // The app name is the real link, stretched across the card: the
            // whole surface stays clickable, and the row is reachable by Tab,
            // openable with Enter, and middle-clickable into a new tab.
            <div key={s.id} className="card row-card" style={{ padding: '13px 16px' }}>
              <div className="row spread">
                <div className="row" style={{ minWidth: 0 }}>
                  <Link to={`/apps/${s.id}`} className="row-link"
                    style={{ fontFamily: 'var(--display)', fontSize: 14.5, fontWeight: 700 }}>
                    {s.name}
                  </Link>
                  <StatusBadge status={s.status} />
                  {s.kind === 'compose' && <span className="tag">stack</span>}
                </div>
                <div className="row row-actions">
                  {s.status === 'running' ? (
                    <button className="btn sm" disabled={locked} title="Stop" aria-label={`Stop ${s.name}`} onClick={(e) => act(e, s, 'stop')}>
                      {busy === `${s.id}:stop` ? <Spinner size={12} /> : <Square size={12} />}
                    </button>
                  ) : (
                    <button className="btn sm" disabled={locked}
                      title={working ? 'Working — hang tight' : 'Start'}
                      aria-label={working ? `${s.name} is working` : `Start ${s.name}`}
                      onClick={(e) => act(e, s, 'start')}>
                      {busy === `${s.id}:start` || working ? <Spinner size={12} /> : <Play size={12} />}
                    </button>
                  )}
                  <button className="btn sm" disabled={locked} title="Restart" aria-label={`Restart ${s.name}`} onClick={(e) => act(e, s, 'restart')}>
                    {busy === `${s.id}:restart` ? <Spinner size={12} /> : <RotateCw size={12} />}
                  </button>
                </div>
              </div>
              {/* row-actions: keeps the live URLs clickable above the stretched link */}
              <div className="row wrap row-actions" style={{ marginTop: 9, gap: 10 }}>
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

        {/* Waiting for the first load is not the same as having no apps. */}
        {!loaded && services.length === 0 && <SkeletonRows count={3} height={78} />}

        {loaded && services.length === 0 && (
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
          <InfoTip wide>
            &ldquo;Working&rdquo; means a deploy or restart is in flight — its buttons unlock when it
            finishes. &ldquo;Down&rdquo; apps are retried automatically a few times; if they stay
            down, the logs usually say why.
          </InfoTip>
        </div>
      )}
    </div>
  );
}
