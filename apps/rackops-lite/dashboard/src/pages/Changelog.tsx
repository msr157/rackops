import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle, ArrowRight, ArrowUpCircle, ExternalLink, GitBranch, KeyRound, LifeBuoy, Network,
  RotateCw, ShieldCheck, Sparkles, Users,
} from 'lucide-react';
import { api } from '../api';
import type { VersionInfo } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { Crumbs, Spinner } from '../components/ui';

// A change line: the optional lead is the feature name the sentence already
// opens with, bolded as a scanning anchor; lead + text is the exact history.
interface ReleaseChange { lead?: string; text: string }
interface Release { version: string; date: string; title: string; changes: ReleaseChange[] }

const releases: Release[] = [
  {
    version: 'v0.7.0', date: 'October 13, 2026', title: 'Multi-cloud migration, Front Door tunnels, and hardened tenant isolation',
    changes: [
      { lead: 'Multi-Cloud Migration', text: ' — launch and cut over workloads directly to AWS, Azure, or GCP with live VM and storage pricing quotes, and real-time downtime calculation.' },
      { lead: '9-Stage Zero-Loss Engine', text: ' — deterministic cutover pipeline: quiesce, encrypt, snapshot, transfer, restore, boot, validate, and atomically cut over traffic with automatic rollback protection.' },
      { lead: 'Migration Wizard', text: ' — interactive 5-step React wizard with live SSE progress tracking and phase status visualization.' },
      { lead: 'Front Door edge tunnels', text: ' — self-hosted multiplexed TCP/QUIC edge ingress service exposing internal dashboards and apps through CGNAT with automated Let’s Encrypt TLS.' },
      { lead: 'AI Tenant Isolation', text: ' — strict organizational context boundaries in the AI Co-Pilot, offline fallback heuristics, and sandbox execution constraints.' },
      { lead: 'AES-GCM 256 Backups', text: ' — authenticated local and cloud volume snapshot replication targeting AWS S3, Azure Blob, Google Cloud Storage, or MinIO.' },
    ],
  },
  {
    version: 'v0.6.0', date: 'August 6, 2026', title: 'One-paste machines and remote access',
    changes: [
      { lead: 'One-paste Add machine', text: ' — the join dialog now hands you a single installer command that sets up Docker, installs RackOps, and joins the machine to your rack as a boot-time service (Linux, macOS, WSL2).' },
      { lead: 'Dashboard quick tunnel', text: ' — a switch under Network → Remote access gives this dashboard a public URL in seconds: no port forwarding, works behind CGNAT, heals itself if the tunnel dies.' },
      { lead: 'Dashboard domain', text: ' — installs with a public edge can serve the dashboard at a real domain through Traefik, with HTTPS once Let’s Encrypt is on.' },
      { lead: 'Honest recommendations', text: ' — on racks with no public IP, "Recommended" moved from the instant URL (LAN-only there) to the quick tunnel, and the VPS-anchor wizard now walks the path that actually works.' },
      { lead: 'Copy buttons', text: ' work everywhere now, including dashboards served over plain http:// on a LAN or VPS IP.' },
      { lead: 'Activity page', text: ' — the event feed moved off Home into its own filterable page; Home stays a one-glance status answer. The real RackOps logo landed in the tab and the chrome, too.' },
      { lead: 'Steadier multi-machine routing', text: ' — cross-node domain routes no longer reset every couple of seconds on fleets with more than one machine.' },
    ],
  },
  {
    version: 'v0.5.0', date: 'August 6, 2026', title: 'macOS, live logs, and hardened sessions',
    changes: [
      { lead: 'macOS support', text: ' — the same install one-liner now works on Apple Silicon and Intel Macs: launchd service, in-place self-update, Docker via Docker Desktop or colima.' },
      { lead: 'Live logs', text: ' — the Logs tab tails your app in real time, and build/deploy output streams line-by-line as it runs.' },
      { lead: 'Hardened sessions', text: ' — login tokens are signed with a dedicated per-cluster secret. Everyone is signed out once on upgrade; just log in again.' },
      { lead: 'Restart-proof operations', text: ' — queued deploys and mesh IPs survive control-plane restarts, and agents release their mesh interface and relay ports cleanly.' },
      { lead: 'Plain-HTTP dashboards', text: ' fixed: app pages no longer crash on LAN dashboards served over http://, and DNS copy buttons work there too.' },
      { lead: 'Server restart control', text: ' — owners and admins can restart the server from the dashboard, with automatic reconnection.' },
    ],
  },
  {
    version: 'v0.4.0', date: 'July 17, 2026', title: 'Organizations done right, support, and AI-tool access',
    changes: [
      { lead: 'Organizations', text: ' rebuilt Dokploy-style: a sidebar switcher, invitation links, per-member permissions with project-level access, and true scoping. Every org sees only its own machines, apps, domains, and settings.' },
      { lead: 'Profile page', text: ' with password change, API keys (expiry, last-used, shown once) that act with your role, and ready-to-paste MCP config for VS Code, Claude, and Cursor.' },
      { lead: 'Password reset', text: ' that works self-hosted: emailed from RackOps support, and always printed in the server log as the universal fallback.' },
      { lead: 'Support tickets', text: ' inside the dashboard. Replies from the RackOps team land straight back in the thread.' },
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
  { label: 'Organizations', to: '/settings?tab=organization', icon: <Users size={16} aria-hidden /> },
  { label: 'API keys & MCP', to: '/profile', icon: <KeyRound size={16} aria-hidden /> },
  { label: 'Domains & edge', to: '/network', icon: <Network size={16} aria-hidden /> },
  { label: 'Support', to: '/support', icon: <LifeBuoy size={16} aria-hidden /> },
];

export function ChangelogPage() {
  const toast = useToast();
  const confirm = useConfirm();
  // The rail dot marks the release this server is actually running, not just
  // the newest entry — an operator a version behind should see that at a
  // glance. Until (or unless) the server answers, the newest release stands in.
  const [info, setInfo] = useState<VersionInfo | null>(null);
  const [updating, setUpdating] = useState(false);
  // The update never came back in time: the poll gave up and the page says
  // so inline, where it survives being missed for a minute.
  const [stalled, setStalled] = useState(false);
  const pollRef = useRef<number | undefined>(undefined);
  const giveUpRef = useRef<number | undefined>(undefined);
  const runningVersion = info?.current ?? null;

  useEffect(() => {
    let cancelled = false;
    api.versionCheck().then((v) => { if (!cancelled) setInfo(v); }).catch(() => {});
    return () => {
      cancelled = true;
      window.clearInterval(pollRef.current);
      window.clearTimeout(giveUpRef.current);
    };
  }, []);

  // The one-click update, hosted here since the changelog is where the
  // sidebar's "vX available" chip lands.
  const update = async () => {
    if (!info || updating) return;
    const yes = await confirm({
      title: `Update RackOps to ${info.latest}?`,
      body: 'The server downloads the new release and restarts itself. Your apps keep running; the dashboard reconnects in a few seconds.',
      confirmLabel: 'Update now',
    });
    if (!yes) return;
    setUpdating(true);
    setStalled(false);
    try {
      await api.applyUpdate();
      toast.info('Updating. The server restarts shortly; this page will reconnect.');
      // Poll until the new version answers, then reload onto fresh assets. If
      // it never answers, say so inline instead of reloading onto an unchanged
      // server and leaving the operator to guess whether the update took.
      const target = info.latest.replace(/^v/, '');
      pollRef.current = window.setInterval(async () => {
        try {
          const v = await api.versionCheck();
          if (v.current === target) { window.clearInterval(pollRef.current); window.location.reload(); }
        } catch { /* server restarting */ }
      }, 2000);
      giveUpRef.current = window.setTimeout(() => {
        window.clearInterval(pollRef.current);
        setUpdating(false);
        setStalled(true);
      }, 60000);
    } catch (e) {
      toast.err(cleanError(e));
      setUpdating(false);
    }
  };

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
    <div className="page" style={{ maxWidth: 850 }}>
      <Crumbs items={[{ label: 'What’s new', icon: <Sparkles size={13} aria-hidden /> }]} />
      <div className="page-title">What&rsquo;s new</div>
      <div className="page-sub">Every user-visible RackOps change, with direct paths to the features that shipped.</div>

      {stalled && info && (
        <div className="notice warn" role="alert">
          <AlertTriangle size={16} style={{ color: 'var(--warn)', flexShrink: 0 }} aria-hidden />
          <span style={{ flex: 1, minWidth: 240 }}>
            The server has not come back on {info.latest} yet. It may still be restarting.
            Check the server log (<code className="mono">journalctl -u rackops</code>), then reload.
          </span>
          <button className="btn sm" onClick={() => window.location.reload()}>
            <RotateCw size={12} aria-hidden /> Reload page
          </button>
        </div>
      )}

      {info?.updateAvailable && (
        <div className="notice accent">
          <ArrowUpCircle size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} aria-hidden />
          <span style={{ flex: 1, minWidth: 240 }}>
            <strong>RackOps {info.latest} is available.</strong>{' '}
            <span className="dim">You are running v{info.current}.</span>{' '}
            {info.releaseUrl && (
              <a href={info.releaseUrl} target="_blank" rel="noreferrer">
                Release notes on GitHub <ExternalLink size={11} aria-hidden />
              </a>
            )}
          </span>
          <button className="btn primary" disabled={updating} onClick={update}>
            {updating
              ? <><Spinner size={13} /> Updating…</>
              : <><ArrowUpCircle size={13} aria-hidden /> Update now</>}
          </button>
        </div>
      )}

      <div className="release-list">
        {releases.map((release, index) => (
          // scrollMarginTop keeps a deep-linked release from landing under the
          // sticky topbar.
          <article className="release-entry" key={release.version} id={anchorId(release.version)} style={{ scrollMarginTop: 64 }}>
            <div className="release-rail" aria-hidden><span className={index === liveIdx ? 'live' : ''} /></div>
            {/* Releases sit on the canvas; the rail and whitespace carry the
                structure, not a border. Padding keeps the rail dot on the
                version line's center. */}
            <div className="release-card" style={{ paddingTop: 12, paddingLeft: 8 }}>
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
