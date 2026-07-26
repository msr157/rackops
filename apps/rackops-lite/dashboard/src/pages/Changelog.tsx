import { Link } from 'react-router-dom';
import { ArrowRight, GitBranch, KeyRound, LifeBuoy, Network, ShieldCheck, Sparkles, Users } from 'lucide-react';

const releases = [
  {
    version: 'v0.4.0', date: 'July 17, 2026', title: 'Organizations done right, support, and AI-tool access',
    changes: [
      'Organizations rebuilt Dokploy-style: a sidebar switcher, invitation links, per-member permissions with project-level access, and true scoping — every org sees only its own machines, apps, domains, and settings.',
      'Profile page with password change, API keys (expiry, last-used, shown once) that act with your role, and ready-to-paste MCP config for VS Code, Claude, and Cursor.',
      'Password reset that works self-hosted: emailed from RackOps support, and always printed in the server log as the universal fallback.',
      'Support tickets inside the dashboard — replies from the RackOps team land straight back in the thread.',
      'Interactive API docs at /swagger, generated from the live route table and served fully offline.',
      'Needs Attention now clears itself the moment an issue resolves, and action buttons stay locked for the whole deploy window.',
      'Info tips (ⓘ) across every page explain anything non-obvious in plain language.',
    ],
  },
  {
    version: 'v0.3.0', date: 'July 12, 2026', title: 'Teams, integrations, and smoother operations',
    changes: [
      'Organizations with member roles and project ownership.',
      'Revocable API keys, interactive Swagger docs, and a built-in MCP server for AI editors.',
      'One-click and CLI updates, account recovery polish, and local support tickets.',
      'A clearer Needs attention view, contextual info tips, and safer action states.',
    ],
  },
  {
    version: 'v0.2.0', date: 'July 12, 2026', title: 'Install, adopt, and deploy',
    changes: [
      'One-line Linux/WSL installation with Docker, Compose, and Git setup.',
      'Automatic adoption and management of containers already running on a machine.',
      'Template-based multi-service deployments and a rebuilt operator dashboard.',
      'Self-healing services, complete logs, and corrected host-to-container port mapping.',
    ],
  },
];

export function ChangelogPage() {
  return (
    <div className="page changelog-page">
      <div className="page-title row"><Sparkles size={20} style={{ color: 'var(--amber)' }} /> What&rsquo;s new</div>
      <div className="page-sub">Every user-visible RackOps change, with direct paths to the features that shipped.</div>

      <div className="release-links">
        <Link to="/settings?tab=org" className="card clickable"><Users size={16} /> Organizations <ArrowRight size={12} /></Link>
        <Link to="/profile" className="card clickable"><KeyRound size={16} /> API keys &amp; MCP <ArrowRight size={12} /></Link>
        <Link to="/network" className="card clickable"><Network size={16} /> Domains &amp; edge <ArrowRight size={12} /></Link>
        <Link to="/support" className="card clickable"><LifeBuoy size={16} /> Support <ArrowRight size={12} /></Link>
        <a href="/swagger" target="_blank" rel="noreferrer" className="card clickable"><ShieldCheck size={16} /> API docs <ArrowRight size={12} /></a>
      </div>

      <div className="release-list">
        {releases.map((release, index) => (
          <article className="release-entry" key={release.version}>
            <div className="release-rail"><span className={index === 0 ? 'live' : ''} /></div>
            <div className="card release-card">
              <div className="row spread wrap">
                {/* A date is metadata, not status — amber is reserved for the LED language. */}
                <div className="row"><GitBranch size={14} aria-hidden /><strong>{release.version}</strong><span className="tag">{release.date}</span></div>
              </div>
              <h2>{release.title}</h2>
              <ul>{release.changes.map((change) => <li key={change}>{change}</li>)}</ul>
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}
