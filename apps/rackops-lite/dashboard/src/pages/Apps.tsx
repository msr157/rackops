import { useId, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Rocket, Search, TriangleAlert } from 'lucide-react';
import type { Service } from '../api';
import { useData } from '../data';
import { EmptyState, InfoTip, Led, SkeletonRows, StatusBadge, UrlChip, publicUrls } from '../components/ui';

// AppsPage: every workload, its health, and where it's reachable, with the
// full detail one click away. Rows are pure navigation: lifecycle actions live
// on the app detail header and in the command palette, so nothing on this list
// can be mis-clicked while scanning.
export function AppsPage() {
  const { services, nodes, ingress, nodeName, loaded } = useData();
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const searchId = useId();

  const filtered = services.filter((s) =>
    !search || (s.name + (s.image ?? '') + (s.domain ?? '')).toLowerCase().includes(search.toLowerCase()));

  // Stable name order, always. Failed apps sit under their own labelled group
  // instead of a live failing-first resort, so rows never jump mid-aim when a
  // status flips between telemetry ticks.
  const byName = [...filtered].sort((a, b) => a.name.localeCompare(b.name));
  const attention = byName.filter((s) => s.status === 'failed');
  const rest = byName.filter((s) => s.status !== 'failed');

  // The filter earns its place once there are 2+ apps, but its slot is always
  // reserved: appearing after the fleet loads must not shove the list down
  // mid-read. visibility keeps the exact footprint without a focusable ghost.
  const showFilter = loaded && services.length > 1;

  const renderRow = (s: Service) => {
    const urls = publicUrls(s, ingress);
    const node = nodes.find((n) => n.id === s.nodeId);
    return (
      // The app name is the real link, stretched across the card: the whole
      // surface stays clickable, and the row is reachable by Tab, openable
      // with Enter, and middle-clickable into a new tab.
      <div key={s.id} className="card row-card" style={{ padding: '13px 16px' }}>
        <div className="row" style={{ minWidth: 0 }}>
          <Link to={`/apps/${s.id}`} className="row-link"
            style={{ fontSize: 14.5, fontWeight: 600, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {s.name}
          </Link>
          <StatusBadge status={s.status} />
          {s.kind === 'compose' && <span className="tag">stack</span>}
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
          </span>
        </div>
      </div>
    );
  };

  return (
    <div className="page">
      <div className="page-title">Apps</div>
      <div className="page-sub">Everything running on your rack, and where it's reachable</div>

      <div className="search-input"
        style={{ width: 280, maxWidth: '100%', marginBottom: 16, visibility: showFilter ? 'visible' : 'hidden' }}>
        <Search size={13} aria-hidden />
        <label className="sr-only" htmlFor={searchId}>Filter apps</label>
        <input className="input" id={searchId} placeholder="Filter apps…"
          value={search} onChange={(e) => setSearch(e.target.value)} />
      </div>

      <div className="stack" style={{ gap: 8 }}>
        {attention.length > 0 && (
          <div className="eyebrow" style={{ color: 'var(--crit)' }}>
            <TriangleAlert size={12} aria-hidden /> Needs attention
          </div>
        )}
        {attention.map(renderRow)}
        {attention.length > 0 && rest.length > 0 && (
          <div className="eyebrow" style={{ marginTop: 8 }}>Everything else</div>
        )}
        {rest.map(renderRow)}

        {/* Waiting for the first load is not the same as having no apps. */}
        {!loaded && services.length === 0 && <SkeletonRows count={3} height={78} />}

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
        {services.length > 0 && filtered.length === 0 && (
          <EmptyState icon={<Search size={18} />} title={`Nothing matches "${search}"`}>
            <button className="btn sm" style={{ marginTop: 4 }} onClick={() => setSearch('')}>Clear filter</button>
          </EmptyState>
        )}
      </div>

      {/* Legend for new users: same words the status badges use, so the page
          never speaks two dialects. */}
      {services.length > 0 && (
        <div className="row wrap" style={{ marginTop: 18, gap: 16 }}>
          <span className="faint row" style={{ gap: 5 }}><Led status="running" /> running</span>
          <span className="faint row" style={{ gap: 5 }}><Led status="building" /> building</span>
          <span className="faint row" style={{ gap: 5 }}><Led status="failed" /> failed (auto-heal retries, check logs)</span>
          <span className="faint row" style={{ gap: 5 }}><Led status="stopped" /> stopped</span>
          <InfoTip wide>
            &ldquo;Building&rdquo; means a deploy or restart is in flight. &ldquo;Failed&rdquo; apps are
            retried automatically a few times; if they stay down, the logs usually say why.
            Open an app to start, stop, or restart it.
          </InfoTip>
        </div>
      )}
    </div>
  );
}
