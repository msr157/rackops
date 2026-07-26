import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  ArrowRight, ExternalLink, GitBranch, KeyRound, LifeBuoy, Network, ShieldCheck, Sparkles, Users,
} from 'lucide-react';
import { api } from '../api';

// A change line: the optional lead is the feature name the sentence already
// opens with, bolded as a scanning anchor; lead + text is the exact history.
interface ReleaseChange { lead?: string; text: string }
interface Release { version: string; date: string; title: string; changes: ReleaseChange[] }

const releases: Release[] = [
  {
    version: 'v0.4.0', date: 'July 17, 2026', title: 'Organizations done right, support, and AI-tool access',
    changes: [
      { lead: 'Organizations', text: ' rebuilt Dokploy-style: a sidebar switcher, invitation links, per-member permissions with project-level access, and true scoping — every org sees only its own machines, apps, domains, and settings.' },
      { lead: 'Profile page', text: ' with password change, API keys (expiry, last-used, shown once) that act with your role, and ready-to-paste MCP config for VS Code, Claude, and Cursor.' },
      { lead: 'Password reset', text: ' that works self-hosted: emailed from RackOps support, and always printed in the server log as the universal fallback.' },
      { lead: 'Support tickets', text: ' inside the dashboard — replies from the RackOps team land straight back in the thread.' },
      { lead: 'Interactive API docs', text: ' at /swagger, generated from the live route table and served fully offline.' },
      { lead: 'Needs Attention', text: ' now clears itself the moment an issue resolves, and action buttons stay locked for the whole deploy window.' },
      { lead: 'Info tips', text: ' (ⓘ) across every page explain anything non-obvious in plain language.' },
    ],
  },
  {
    version: 'v0.3.0', date: 'July 12, 2026', title: 'Teams, integrations, and smoother operations',
    changes: [
      { lead: 'Organizations', text: ' with member roles and project ownership.' },
      { lead: 'Revocable API keys', text: ', interactive Swagger docs, and a built-in MCP server for AI editors.' },
      { lead: 'One-click and CLI updates', text: ', account recovery polish, and local support tickets.' },
      { text: 'A clearer Needs attention view, contextual info tips, and safer action states.' },
    ],
  },
  {
    version: 'v0.2.0', date: 'July 12, 2026', title: 'Install, adopt, and deploy',
    changes: [
      { lead: 'One-line Linux/WSL installation', text: ' with Docker, Compose, and Git setup.' },
      { lead: 'Automatic adoption', text: ' and management of containers already running on a machine.' },
      { lead: 'Template-based multi-service deployments', text: ' and a rebuilt operator dashboard.' },
      { lead: 'Self-healing services', text: ', complete logs, and corrected host-to-container port mapping.' },
    ],
  },
];

// "v0.4.0" → "v0-4-0": a stable anchor so releases can be deep-linked.
const anchorId = (version: string) => version.replace(/\./g, '-');
const normVersion = (v: string) => v.replace(/^v/i, '');

// Quick paths to the v0.4.0 features — scoped under that release, because
// that is the release they shipped in.
const featureLinks = [
  { label: 'Organizations', to: '/settings?tab=org', icon: <Users size={16} aria-hidden /> },
  { label: 'API keys & MCP', to: '/profile', icon: <KeyRound size={16} aria-hidden /> },
  { label: 'Domains & edge', to: '/network', icon: <Network size={16} aria-hidden /> },
  { label: 'Support', to: '/support', icon: <LifeBuoy size={16} aria-hidden /> },
];

export function ChangelogPage() {
  // The rail dot marks the release this server is actually running, not just
  // the newest entry — an operator a version behind should see that at a
  // glance. Until (or unless) the server answers, the newest release stands in.
  const [runningVersion, setRunningVersion] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    api.versionCheck().then((v) => { if (!cancelled) setRunningVersion(v.current); }).catch(() => {});
    return () => { cancelled = true; };
  }, []);

  // React owns navigation, so a #v0-4-0 deep link needs this nudge: the
  // browser's own hash scroll fired before the releases existed.
  useEffect(() => {
    const hash = window.location.hash.slice(1);
    if (hash) document.getElementById(hash)?.scrollIntoView();
  }, []);

  const runningIdx = runningVersion === null
    ? -1
    : releases.findIndex((r) => normVersion(r.version) === normVersion(runningVersion));
  const liveIdx = runningIdx >= 0 ? runningIdx : 0;

  return (
    <div className="page">
      <div className="page-title row"><Sparkles size={20} style={{ color: 'var(--accent)' }} aria-hidden /> What&rsquo;s new</div>
      <div className="page-sub">Every user-visible RackOps change, with direct paths to the features that shipped.</div>

      <div className="release-list">
        {releases.map((release, index) => (
          // scrollMarginTop keeps a deep-linked release from landing under the
          // sticky topbar.
          <article className="release-entry" key={release.version} id={anchorId(release.version)} style={{ scrollMarginTop: 64 }}>
            <div className="release-rail" aria-hidden><span className={index === liveIdx ? 'live' : ''} /></div>
            <div className="card release-card">
              <div className="row wrap">
                <GitBranch size={14} aria-hidden />
                <strong className="mono">{release.version}</strong>
                {/* A date is metadata, not status — it wears plain faint ink. */}
                <span className="faint">{release.date}</span>
                {index === runningIdx && <span className="badge accent">running</span>}
              </div>
              <h2>{release.title}</h2>
              {release.version === 'v0.4.0' && (
                <div className="release-links" style={{ margin: '12px 0 14px' }}>
                  {featureLinks.map((f) => (
                    <Link key={f.label} to={f.to} className="card clickable">
                      {f.icon} {f.label} <ArrowRight size={12} aria-hidden />
                    </Link>
                  ))}
                  <a href="/swagger" target="_blank" rel="noreferrer" className="card clickable" title="Opens in a new tab">
                    <ShieldCheck size={16} aria-hidden /> API docs <ExternalLink size={12} aria-hidden />
                  </a>
                </div>
              )}
              <ul>
                {release.changes.map((c) => (
                  <li key={c.text}>{c.lead && <strong>{c.lead}</strong>}{c.text}</li>
                ))}
              </ul>
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}
