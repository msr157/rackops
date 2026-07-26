import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import {
  ArrowLeft, Box, Cloud, Globe, History, KeyRound, Play, Plus, RotateCw, Save, ScrollText,
  Square, Trash2, TriangleAlert, Rocket, Folder, FolderOpen, Zap, Settings as SettingsIcon,
  ExternalLink,
} from 'lucide-react';
import { api, subscribeLogChunks } from '../api';
import type { Service, ServiceUpdate, Mount, EnvVar, Deployment, Domain, DirEntry, PlacementScore, LogChunk } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import {
  EmptyState, Field, InfoTip, Kpi, Led, Modal, Skeleton, SkeletonRows, Spinner, StatusBadge,
  UrlChip, dnsLabel, publicUrls, timeAgo,
} from '../components/ui';

type Tab = 'overview' | 'networking' | 'logs' | 'deployments' | 'settings';

const TABS: { key: Tab; label: string; icon: React.ReactNode }[] = [
  { key: 'overview', label: 'Overview', icon: <Box size={14} /> },
  { key: 'networking', label: 'Networking', icon: <Globe size={14} /> },
  { key: 'logs', label: 'Logs', icon: <ScrollText size={14} /> },
  { key: 'deployments', label: 'Deployments', icon: <History size={14} /> },
  { key: 'settings', label: 'Settings', icon: <SettingsIcon size={14} /> },
];

// Draft holds the editable copy of a service; saved via PATCH. Env and mount
// rows carry a client-only `rid` so React keys stay stable while rows are
// added and removed — the rid is stripped before anything is sent to the API.
interface DraftEnv extends EnvVar { rid: string }
interface DraftMount extends Mount { rid: string }
interface Draft {
  image: string; command: string; port: string; containerPort: string; ports: string;
  env: DraftEnv[]; mounts: DraftMount[];
  memLimitMb: string; cpuLimit: string; restartPolicy: string; gpu: boolean; nodeId: string;
}

function toDraft(s: Service): Draft {
  return {
    image: s.image ?? '', command: s.command ?? '',
    port: s.port ? String(s.port) : '', containerPort: s.containerPort ? String(s.containerPort) : '',
    ports: (s.ports ?? []).join(', '),
    env: (s.env ?? []).map((e) => ({ ...e, rid: crypto.randomUUID() })),
    mounts: (s.mounts ?? []).map((m) => ({ ...m, rid: crypto.randomUUID() })),
    memLimitMb: s.memLimitMb ? String(s.memLimitMb) : '',
    cpuLimit: s.cpuLimit ? String(s.cpuLimit) : '',
    restartPolicy: s.restartPolicy || 'unless-stopped',
    gpu: !!s.gpu, nodeId: s.nodeId,
  };
}

// Client-side validation that runs before a save leaves the browser. It exists
// so bad input fails loudly next to the field that caused it — never silently
// dropped (the old behavior for a valued env row with no key).
interface DraftErrors {
  port?: string;
  containerPort?: string;
  ports?: string;
  /** rid → message, for env rows whose value would be lost on save. */
  env: Record<string, string>;
}

const NO_ERRORS: DraftErrors = { env: {} };

function validPort(v: string): boolean {
  const n = parseInt(v, 10);
  return Number.isFinite(n) && n >= 1 && n <= 65535;
}

function validateDraft(d: Draft): DraftErrors {
  const errs: DraftErrors = { env: {} };
  if (d.containerPort && !validPort(d.containerPort)) errs.containerPort = 'Ports run from 1 to 65535.';
  if (d.port && !validPort(d.port)) errs.port = 'Ports run from 1 to 65535.';
  if (d.ports.trim()) {
    const parts = d.ports.split(',').map((p) => p.trim()).filter(Boolean);
    if (parts.length === 0 || parts.some((p) => !/^\d+$/.test(p) || !validPort(p))) {
      errs.ports = 'Comma-separated port numbers, each between 1 and 65535.';
    }
  }
  for (const row of d.env) {
    // A fully blank row is dropped harmlessly on save; a valued row with no
    // key would lose data, so it blocks the save instead of vanishing.
    if (!row.key.trim() && row.value !== '') {
      errs.env[row.rid] = 'This value has no name. Add a KEY or remove the row.';
    }
  }
  return errs;
}

function hasDraftErrors(e: DraftErrors): boolean {
  return !!(e.port || e.containerPort || e.ports) || Object.keys(e.env).length > 0;
}

export function AppDetail() {
  const { id = '', tab: tabParam } = useParams();
  const tab = (TABS.some((t) => t.key === tabParam) ? tabParam : 'overview') as Tab;
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();
  const { nodes, services, ingress, refresh, nodeName } = useData();

  const [svc, setSvc] = useState<Service | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [baseline, setBaseline] = useState('');
  // busyAction: which action ('start' | 'stop' | 'restart' | 'redeploy' |
  // 'delete' | 'save' | 'saveRedeploy') is in flight — all action buttons
  // disable, only the active one spins. Empty string = idle.
  const [busyAction, setBusyAction] = useState('');
  // The raw error string from a failed load. Kept raw (not cleaned) so the
  // render can tell a 404 (the app is gone) from a transient read failure.
  const [loadFailed, setLoadFailed] = useState<string | null>(null);
  // Validation errors stay invisible until a save is attempted, then update
  // live as the user types (they derive from the draft).
  const [showErrors, setShowErrors] = useState(false);
  // Actions stay locked through the whole deploy window: from the moment a
  // request is accepted until the machine reports the container running (or
  // failed). svc.status refreshes on a 4s poll, so the lock releases itself.
  const working = svc?.status === 'building' || svc?.status === 'pending';
  const busy = busyAction !== '' || working;

  const hydrate = useCallback((s: Service) => {
    setSvc(s);
    setLoadFailed(null);
    const d = toDraft(s);
    setDraft(d);
    setBaseline(JSON.stringify(d));
  }, []);

  const load = useCallback(() => {
    api.service(id).then(hydrate).catch((e) => setLoadFailed(String(e)));
  }, [id, hydrate]);

  // Reset everything when the route points at a different app, so the old
  // app's data never flashes under the new one's URL.
  useEffect(() => {
    setSvc(null); setDraft(null); setBaseline('');
    setLoadFailed(null); setShowErrors(false); setBusyAction('');
    load();
  }, [load]);

  // Live status refresh (never clobbers the draft being edited). If the first
  // load failed, the poll doubles as automatic recovery and seeds the page.
  const draftRef = useRef<Draft | null>(null);
  draftRef.current = draft;
  useEffect(() => {
    const t = setInterval(() => {
      api.service(id).then((s) => {
        if (draftRef.current) { setSvc(s); setLoadFailed(null); } else { hydrate(s); }
      }).catch(() => {});
    }, 4000);
    return () => clearInterval(t);
  }, [id, hydrate]);

  const dirty = useMemo(() => draft && JSON.stringify(draft) !== baseline, [draft, baseline]);
  const node = nodes.find((n) => n.id === svc?.nodeId);
  const urls = svc ? publicUrls(svc, ingress) : [];

  const draftErrors = useMemo(() => (draft ? validateDraft(draft) : NO_ERRORS), [draft]);
  const visibleErrors = showErrors ? draftErrors : NO_ERRORS;

  const save = async (redeploy: boolean) => {
    if (!draft || !svc) return;
    if (hasDraftErrors(draftErrors)) {
      setShowErrors(true);
      toast.err('Nothing was saved. Fix the highlighted fields on the Settings tab first.');
      return;
    }
    setBusyAction(redeploy ? 'saveRedeploy' : 'save');
    try {
      const upd: ServiceUpdate = {
        image: draft.image, command: draft.command,
        port: parseInt(draft.port, 10) || 0,
        containerPort: parseInt(draft.containerPort, 10) || 0,
        ports: draft.ports.split(',').map((p) => parseInt(p.trim(), 10)).filter((p) => p > 0),
        env: draft.env.filter((e) => e.key).map((e) => ({ key: e.key, value: e.value, secret: e.secret })),
        mounts: draft.mounts.filter((m) => m.target)
          .map((m) => ({ type: m.type, source: m.source, target: m.target, readOnly: m.readOnly, drivePath: m.drivePath })),
        memLimitMb: parseInt(draft.memLimitMb, 10) || 0,
        cpuLimit: parseFloat(draft.cpuLimit) || 0,
        restartPolicy: draft.restartPolicy, gpu: draft.gpu,
        nodeId: draft.nodeId, redeploy,
      };
      const s = await api.updateService(svc.id, upd);
      hydrate(s);
      setShowErrors(false);
      refresh();
      toast.ok(redeploy ? 'Saved. Redeploying now.' : 'Saved. Changes apply on the next redeploy.');
    } catch (e) { toast.err(cleanError(e)); }
    setBusyAction('');
  };

  const act = async (action: string) => {
    if (!svc) return;
    setBusyAction(action);
    try {
      if (action === 'delete') {
        const yes = await confirm({
          title: `Remove ${svc.name}?`,
          body: 'The container is deleted from its machine. Data in managed volumes stays on disk under rackops-volumes/.',
          confirmLabel: 'Remove app', danger: true, expect: svc.name,
        });
        if (yes) {
          await api.deleteService(svc.id);
          toast.ok(`${svc.name} removed.`);
          refresh();
          navigate('/apps');
        }
        setBusyAction('');
        return;
      }
      await api.serviceAction(svc.id, action);
      toast.ok(`${svc.name}: ${action} requested.`);
      load(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
    setBusyAction('');
  };

  // Load failure before anything rendered. A 404 for an app the fleet no
  // longer knows means it's really gone; everything else (network failure,
  // 5xx, or a 404 the fleet contradicts) is a read problem worth retrying —
  // telling someone their app "doesn't exist anymore" over a flaky connection
  // is the one message this page must never get wrong.
  if ((!svc || !draft) && loadFailed) {
    const gone = /^Error:\s*404\b/.test(loadFailed) && !services.some((s) => s.id === id);
    if (gone) {
      return (
        <div className="page">
          <EmptyState icon={<Box size={18} />} title="This app doesn't exist anymore">
            <div style={{ marginBottom: 14 }}>It was removed in another session, or by someone else in this org.</div>
            <Link to="/apps" className="btn">Back to apps</Link>
          </EmptyState>
        </div>
      );
    }
    return (
      <div className="page">
        <div className="card crit" style={{ maxWidth: 520 }}>
          <div className="row" style={{ marginBottom: 8 }}>
            <TriangleAlert size={15} style={{ color: 'var(--crit)' }} aria-hidden />
            <strong>Couldn&rsquo;t load this app</strong>
          </div>
          <div className="dim" style={{ marginBottom: 6 }}>{cleanError(loadFailed)}</div>
          <div className="dim" style={{ marginBottom: 14 }}>
            The app itself is probably fine; this page just couldn&rsquo;t read its record.
            It also retries on its own every few seconds.
          </div>
          <div className="row">
            <button className="btn primary" onClick={() => { setLoadFailed(null); load(); }}>Retry now</button>
            <Link to="/apps" className="btn ghost">Back to apps</Link>
          </div>
        </div>
      </div>
    );
  }

  if (!svc || !draft) {
    return (
      <div className="page">
        <div className="row" style={{ marginBottom: 18 }} aria-hidden>
          <Skeleton height={24} width={240} radius="var(--r-md)" />
          <Skeleton height={18} width={84} radius={20} />
        </div>
        <SkeletonRows count={4} height={64} />
        <span className="sr-only">Loading app…</span>
      </div>
    );
  }

  return (
    <div className="page">
      <div className="row spread wrap" style={{ marginBottom: 4 }}>
        <div className="row">
          <button className="btn sm ghost" onClick={() => navigate('/apps')}
            aria-label="Back to apps" title="Back to apps"><ArrowLeft size={14} /></button>
          <div className="page-title" style={{ margin: 0 }}>{svc.name}</div>
          <StatusBadge status={svc.status} />
          {svc.adopted && (
            <span className="tag">
              adopted
              <InfoTip wide>
                This container was already running on the machine, so RackOps adopted it for you to
                manage here. Start, stop, and remove work; its build configuration stays read-only.
              </InfoTip>
            </span>
          )}
          {svc.templateId && <span className="tag">{svc.templateId}</span>}
        </div>
        <div className="row">
          <button className="btn sm" disabled={busy} onClick={() => act(svc.status === 'running' ? 'stop' : 'start')}>
            {busyAction === 'stop' || busyAction === 'start' ? <Spinner size={12} />
              : svc.status === 'running' ? <Square size={12} /> : <Play size={12} />}
            {svc.status === 'running' ? 'Stop' : 'Start'}
          </button>
          <button className="btn sm" disabled={busy} onClick={() => act('restart')}>
            {busyAction === 'restart' ? <Spinner size={12} /> : <RotateCw size={12} />} Restart
          </button>
          {!svc.adopted && (
            // While edits are unsaved, Redeploy must not silently ship the old
            // config: Save & redeploy in the bottom bar is the one redeploy
            // verb for that state.
            <button className="btn sm" disabled={busy || !!dirty}
              title={dirty ? 'You have unsaved changes. Use Save & redeploy below.' : undefined}
              onClick={() => act('redeploy')}>
              {busyAction === 'redeploy' ? <Spinner size={12} /> : <Rocket size={12} aria-hidden />} Redeploy
            </button>
          )}
        </div>
      </div>
      <div className="row wrap" style={{ marginBottom: 16, gap: 8 }}>
        <span className="dim mono">{svc.image || svc.gitRepo || svc.kind} · {nodeName(svc.nodeId)}</span>
        {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
      </div>

      <div className="tabs">
        {TABS.map((t) => (
          <Link key={t.key} to={`/apps/${svc.id}/${t.key}`} className={`tab ${tab === t.key ? 'active' : ''}`}>
            {t.icon} {t.label}
          </Link>
        ))}
      </div>

      {tab === 'overview' && <OverviewTab svc={svc} nodeLabel={node?.name ?? svc.nodeId} urls={urls.map((u) => u.url)} />}
      {tab === 'networking' && <NetworkingTab svc={svc} reload={load} />}
      {tab === 'logs' && <LogsTab svc={svc} />}
      {tab === 'deployments' && <DeploymentsTab serviceId={svc.id} />}
      {tab === 'settings' && (
        <SettingsTab svc={svc} draft={draft} setDraft={setDraft} nodes={nodes} busy={busy}
          deleting={busyAction === 'delete'} errors={visibleErrors} onDelete={() => act('delete')} />
      )}

      {/* Pinned save bar: rides the bottom of the viewport while the draft is
          dirty, so the actions stay in reach of whichever field is being
          edited. Adopted apps are read-only everywhere, so they never get
          dirty and never see this bar. */}
      {dirty && !svc.adopted && (
        <div className="dirty-banner pinned" role="status">
          <span className="row" style={{ flex: 1, minWidth: 220, gap: 8 }}>
            <TriangleAlert size={14} style={{ color: 'var(--warn)', flex: 'none' }} aria-hidden />
            <span>Unsaved changes. The app keeps running until you apply them.</span>
          </span>
          <div className="row">
            <button className="btn sm" disabled={busy} onClick={() => setDraft(JSON.parse(baseline))}>Discard</button>
            <button className="btn sm" disabled={busy} onClick={() => save(false)}>
              {busyAction === 'save' ? <Spinner size={12} /> : <Save size={12} aria-hidden />} Save
            </button>
            <button className="btn sm primary" disabled={busy} onClick={() => save(true)}>
              {busyAction === 'saveRedeploy' ? <Spinner size={12} /> : <Rocket size={12} aria-hidden />} Save &amp; redeploy
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/* ---------- Overview ---------- */

function OverviewTab({ svc, nodeLabel, urls }: { svc: Service; nodeLabel: string; urls: string[] }) {
  return (
    <>
      <div className="kpi-row">
        <Kpi label="CPU" value={svc.cpuPct.toFixed(1)} unit="%" />
        <Kpi label="Memory" value={svc.memUsedMb} unit={`MB${svc.memLimitMb ? ` / ${svc.memLimitMb}` : ''}`} />
        <Kpi label="Port" value={svc.port || 'none'} />
        <Kpi label="Machine" value={nodeLabel} />
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 10 }}>Details</div>
        <dl className="kv">
          <dt>Source</dt>
          <dd>{svc.image || svc.gitRepo || `${svc.kind} stack`}</dd>
          <dt>Public URLs</dt>
          <dd>{urls.length ? urls.join(' · ') : 'none yet. Set one up in Networking.'}</dd>
          <dt>Container</dt>
          <dd>{svc.containerId || 'not created yet'}</dd>
          <dt>Restart policy</dt>
          <dd>{svc.restartPolicy || 'unless-stopped'}</dd>
          <dt>Created</dt>
          <dd>{new Date(svc.createdAt).toLocaleString()} ({timeAgo(svc.createdAt)})</dd>
        </dl>
      </div>
    </>
  );
}

/* ---------- Networking: the make-it-public journey, all instant actions ---------- */

function NetworkingTab({ svc, reload }: { svc: Service; reload: () => void }) {
  const toast = useToast();
  const { nodes, ingress, refresh } = useData();
  const [domains, setDomains] = useState<Domain[]>([]);
  const [domainsLoaded, setDomainsLoaded] = useState(false);
  const [tunnelBusy, setTunnelBusy] = useState(false);
  const [exposeBusy, setExposeBusy] = useState(false);
  // Which route call is in flight. Picking in the select is local-only;
  // nothing touches the API until Apply route or Remove route.
  const [routeBusy, setRouteBusy] = useState<'' | 'apply' | 'remove'>('');
  // null = nothing staged; a string is a staged select choice awaiting Apply.
  const [routeChoice, setRouteChoice] = useState<string | null>(null);
  const node = nodes.find((n) => n.id === svc.nodeId);

  useEffect(() => {
    api.domains().then((d) => { setDomains(d ?? []); setDomainsLoaded(true); }).catch(() => {});
  }, []);

  // While a tunnel is starting, its URL lands via the 4s status refresh.
  useEffect(() => { if (svc.quickTunnelUrl) setTunnelBusy(false); }, [svc.quickTunnelUrl]);

  // A staged choice only means something against the route it was staged
  // over: when the live route changes (applied here, or from another
  // session), the stage resets to reality.
  useEffect(() => { setRouteChoice(null); }, [svc.domain]);

  const setRoute = async (host: string, op: 'apply' | 'remove') => {
    setRouteBusy(op);
    try {
      await api.setServiceDomain(svc.id, host, !host ? 'none' : 'static');
      toast.ok(host ? `Routing ${host} to ${svc.name}.` : 'Public route removed.');
      setRouteChoice(null);
      reload(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
    setRouteBusy('');
  };

  const expose = async () => {
    setExposeBusy(true);
    try {
      const r = await api.exposeService(svc.id);
      toast.ok(`Instant URL ready: ${r.domain}`);
      reload(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
    setExposeBusy(false);
  };

  const startTunnel = async () => {
    setTunnelBusy(true);
    try {
      await api.startQuickTunnel(svc.id);
      toast.info('Tunnel starting. The URL appears here in ~15s (first run pulls cloudflared).');
    } catch (e) { toast.err(cleanError(e)); setTunnelBusy(false); }
  };

  const stopTunnel = async () => {
    try {
      await api.stopQuickTunnel(svc.id);
      toast.ok('Tunnel stopped.');
      reload();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const edgeReady = ingress?.enabled && ingress.status === 'running';
  // The select speaks in bare hostnames. A wildcard route is represented by
  // its wildcard parent so re-selecting the current route stays a no-op.
  const currentIsWildcard = domains.find((d) => d.wildcard && (svc.domain || '').endsWith('.' + d.name.slice(2)));
  const currentValue = currentIsWildcard?.name ?? (svc.domain || '');
  const selectValue = routeChoice ?? currentValue;
  const routeChanged = selectValue !== currentValue;
  // The domain the select points at right now (staged or applied), so an
  // unverified pick gets a visible warning instead of a route that silently
  // never works.
  const chosen = domains.find((d) => d.name === selectValue);

  const applyRoute = () => {
    let host = selectValue;
    if (host.startsWith('*.')) host = `${dnsLabel(svc.name)}.${host.slice(2)}`;
    setRoute(host, 'apply');
  };

  return (
    <>
      {/* Reachability now */}
      <div className="card" style={{ marginBottom: 14 }}>
        <div className="eyebrow" style={{ marginBottom: 10 }}>Reachable at</div>
        <div className="row wrap">
          {publicUrls(svc, ingress).map((u) => <UrlChip key={u.url} url={u.url} />)}
          {node && svc.port > 0 && <UrlChip url={`http://${node.ip}:${svc.port}`} />}
          {(!svc.port || !node) && <span className="faint">this app publishes no port{svc.kind === 'compose' ? ' (ports come from the compose file)' : ''}</span>}
        </div>
      </div>

      {svc.port > 0 && (
        <div className="card">
          <div className="eyebrow" style={{ marginBottom: 12 }}>Make it public</div>
          <div className="stack" style={{ gap: 10 }}>

            {/* Option 1: instant URL */}
            <div className="row spread" style={{ alignItems: 'flex-start', gap: 14 }}>
              <div style={{ minWidth: 0 }}>
                <div className="row" style={{ gap: 7 }}>
                  <Zap size={13} style={{ color: 'var(--accent)' }} aria-hidden />
                  <strong>Instant URL</strong>
                  <span className="badge accent">Recommended</span>
                  <InfoTip wide>
                    Publishes this app at a free {`<app>.<ip>.nip.io`} address through your own
                    edge proxy. No DNS setup and no third party; the address keeps working for
                    as long as the edge runs.
                  </InfoTip>
                </div>
                <div className="dim" style={{ marginTop: 3 }}>Free nip.io address through your own edge.</div>
              </div>
              <div style={{ flex: 'none', textAlign: 'right' }}>
                <button className="btn sm" disabled={exposeBusy || !edgeReady} onClick={expose}>
                  {exposeBusy ? <Spinner size={12} /> : <Zap size={12} aria-hidden />} Get instant URL
                </button>
                {!edgeReady && (
                  <div className="faint" style={{ marginTop: 5, maxWidth: 220 }}>
                    Needs the edge proxy running. <Link to="/network">Start it on the Network page</Link>.
                  </div>
                )}
              </div>
            </div>
            <hr className="divider" style={{ margin: '2px 0' }} />

            {/* Option 2: quick tunnel */}
            <div className="row spread" style={{ alignItems: 'flex-start', gap: 14 }}>
              <div style={{ minWidth: 0 }}>
                <div className="row" style={{ gap: 7 }}>
                  <Cloud size={13} style={{ color: 'var(--accent)' }} aria-hidden />
                  <strong>Quick tunnel</strong>
                  <InfoTip wide>
                    A Cloudflare tunnel with a public HTTPS URL. No account, no port forwarding,
                    works behind any router. The URL changes when the tunnel restarts, so treat
                    it as disposable (best for demos and testing).
                  </InfoTip>
                </div>
                <div className="dim" style={{ marginTop: 3 }}>Temporary public HTTPS URL, works behind any router.</div>
                {svc.quickTunnelUrl && (
                  <a className="mono" style={{ fontSize: 12, marginTop: 6, display: 'inline-block' }}
                    href={svc.quickTunnelUrl} target="_blank" rel="noreferrer">
                    {svc.quickTunnelUrl} <ExternalLink size={10} />
                  </a>
                )}
              </div>
              {svc.quickTunnelUrl ? (
                <button className="btn sm danger" style={{ flex: 'none' }} onClick={stopTunnel}>Stop tunnel</button>
              ) : (
                <button className="btn sm" style={{ flex: 'none' }} disabled={tunnelBusy} onClick={startTunnel}>
                  {tunnelBusy ? <Spinner size={12} /> : <Cloud size={12} aria-hidden />} {tunnelBusy ? 'Starting…' : 'Start tunnel'}
                </button>
              )}
            </div>
            <hr className="divider" style={{ margin: '2px 0' }} />

            {/* Option 3: own domain. Selecting only stages the choice; Apply
                route is the single commit point. */}
            <div>
              <div className="row" style={{ gap: 7 }}>
                <Globe size={13} style={{ color: 'var(--accent)' }} aria-hidden />
                <strong>Your own domain</strong>
                <InfoTip wide>
                  Routes a domain you own to this app through the edge proxy. Needs one DNS
                  record; RackOps shows you exactly which on the Network page. Picking a domain
                  here changes nothing until you apply it.
                </InfoTip>
              </div>
              <div className="dim" style={{ margin: '3px 0 10px' }}>
                Permanent address on a domain you own.
                {domainsLoaded && domains.length === 0 && <> First <Link to="/network">add the domain</Link> so RackOps can give you the DNS record.</>}
              </div>
              <div className="row wrap">
                <select className="input" style={{ flex: '1 1 240px', maxWidth: 380 }}
                  aria-label={`Domain route for ${svc.name}`}
                  value={selectValue} disabled={routeBusy !== ''}
                  onChange={(e) => setRouteChoice(e.target.value)}>
                  <option value="" disabled>Choose a domain…</option>
                  {svc.domain && !domains.some((d) => d.name === svc.domain || (d.wildcard && svc.domain.endsWith('.' + d.name.slice(2)))) &&
                    <option value={svc.domain}>{svc.domain}</option>}
                  {domains.map((d) => (
                    <option key={d.id} value={d.name}>
                      {d.wildcard ? `${dnsLabel(svc.name)}.${d.name.slice(2)}` : d.name}
                    </option>
                  ))}
                </select>
                {chosen && chosen.status !== 'verified' && <span className="badge warn">unverified</span>}
                <button className="btn sm primary" disabled={!routeChanged || routeBusy !== ''} onClick={applyRoute}
                  aria-label={`Apply domain route for ${svc.name}`}>
                  {routeBusy === 'apply' && <Spinner size={12} />} Apply route
                </button>
                {svc.domain && (
                  <button className="btn sm" disabled={routeBusy !== ''} onClick={() => setRoute('', 'remove')}
                    aria-label={`Remove domain route for ${svc.name}`}>
                    {routeBusy === 'remove' && <Spinner size={12} />} Remove route
                  </button>
                )}
                {svc.domain && routeChanged && <span className="dim mono">current: {svc.domain}</span>}
              </div>
              {chosen && chosen.status !== 'verified' && (
                <div style={{ color: 'var(--warn)', fontSize: 12, marginTop: 6 }}>
                  This domain hasn&rsquo;t verified yet, so routing won&rsquo;t work until it does.{' '}
                  <Link to="/network">Check its DNS on the Network page</Link>.
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}

/* ---------- Logs ---------- */

// A short random stream id correlating our follow request with its SSE chunks.
function newStreamId() {
  return Math.random().toString(36).slice(2) + Date.now().toString(36);
}

function LogsTab({ svc }: { svc: Service }) {
  const toast = useToast();
  const [logs, setLogs] = useState('');
  const [live, setLive] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [follow, setFollow] = useState(true);
  const termRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true); // keep scrolling to the bottom unless the user scrolled up

  // Live tail: start a server-side `docker logs -f` follow and append chunks as
  // they arrive over SSE. The agent caps a follow at 15 min, so we re-arm well
  // before that to keep a long-open tab streaming.
  useEffect(() => {
    if (!follow) return;
    let stopped = false;
    let streamId = '';
    let rearm: ReturnType<typeof setTimeout> | undefined;

    const unsub = subscribeLogChunks(
      (c: LogChunk) => c.stream === streamId,
      (c: LogChunk) => {
        if (c.text) setLogs((prev) => (prev + c.text).slice(-200_000));
        if (c.end) setLive(false);
      },
    );

    const arm = async () => {
      if (stopped) return;
      streamId = newStreamId();
      try {
        await api.startLogStream(svc.id, streamId);
        if (!stopped) setLive(true);
      } catch (e) {
        if (!stopped) { setLogs((p) => p + `\n(couldn't start live logs: ${cleanError(e)})\n`); setLive(false); }
      }
      rearm = setTimeout(arm, 10 * 60 * 1000);
    };
    setLogs('');
    arm();

    return () => {
      stopped = true;
      if (rearm) clearTimeout(rearm);
      unsub();
      if (streamId) api.stopLogStream(svc.id, streamId).catch(() => {});
    };
  }, [follow, svc.id]);

  // One-shot fetch when not following (snapshot of recent logs).
  const fetchOnce = useCallback(async () => {
    try { setLogs((await api.serviceLogs(svc.id)).logs || '(no output yet)'); }
    catch (e) { setLogs(cleanError(e)); }
  }, [svc.id]);
  useEffect(() => { if (!follow) fetchOnce(); }, [follow, fetchOnce]);

  // Auto-scroll to the newest line while pinned to the bottom.
  useEffect(() => {
    const el = termRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [logs]);

  const onScroll = () => {
    const el = termRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

  return (
    <div className="card">
      <div className="row spread" style={{ marginBottom: 10 }}>
        <div className="row">
          <strong>Runtime logs</strong>
          <InfoTip wide>
            Live tail streams new output as it happens (like <code>docker logs -f</code>).
            Untick it for a one-shot snapshot. &ldquo;Clear old&rdquo; only hides output before
            this moment in the dashboard; nothing is deleted from the machine.
          </InfoTip>
          {follow && live && <span className="badge accent">live</span>}
          {follow && !live && <span className="badge neutral">connecting…</span>}
        </div>
        <div className="row">
          <label className="check">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> live tail
          </label>
          {!follow && (
            <button className="btn sm" onClick={() => fetchOnce()}>
              <RotateCw size={12} /> Refresh
            </button>
          )}
          <button className="btn sm danger" disabled={clearing} onClick={async () => {
            setClearing(true);
            await api.clearServiceLogs(svc.id).catch((e) => toast.err(cleanError(e)));
            if (follow) { setFollow(false); setTimeout(() => setFollow(true), 50); } else { await fetchOnce(); }
            setClearing(false);
          }}>
            {clearing ? <Spinner size={12} /> : <Trash2 size={12} />} Clear old
          </button>
        </div>
      </div>
      <div className="terminal" ref={termRef} onScroll={onScroll} style={{ maxHeight: 520 }}>
        {logs || (follow ? 'streaming from the machine…' : 'fetching from the machine…')}
      </div>
    </div>
  );
}

/* ---------- Deployments ---------- */

function DeploymentsTab({ serviceId }: { serviceId: string }) {
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [open, setOpen] = useState('');
  // Live build output per deployment id, streamed while a deploy is in flight.
  const [liveLogs, setLiveLogs] = useState<Record<string, string>>({});

  const fetchList = useCallback(async () => {
    try {
      const d = await api.serviceDeployments(serviceId);
      setDeployments(d ?? []);
      setLoaded(true);
      setError('');
    } catch (e) {
      // Keep whatever list we already have; the error line + refresh button
      // say why it may be stale. Never shown as an empty history.
      setError(cleanError(e));
    }
  }, [serviceId]);
  useEffect(() => { fetchList(); }, [fetchList]);

  // Append deploy build output as it streams (stream id == deployment id).
  useEffect(() => {
    const unsub = subscribeLogChunks(
      (c: LogChunk) => c.kind === 'deploy' && c.serviceId === serviceId,
      (c: LogChunk) => {
        if (c.text) setLiveLogs((prev) => ({ ...prev, [c.stream]: (prev[c.stream] || '') + c.text }));
        if (c.end) fetchList(); // pull the finalized record + status
      },
    );
    return unsub;
  }, [serviceId, fetchList]);

  const onRefresh = async () => {
    setRefreshing(true);
    await fetchList();
    setRefreshing(false);
  };

  return (
    <div className="card">
      <div className="row spread" style={{ marginBottom: 10 }}>
        <div className="row" style={{ gap: 4 }}>
          <strong>Deployment history</strong>
          <InfoTip>
            Every build and deploy of this app, newest first, with its full output. A deploy in
            progress streams its log live. Click any row to expand it.
          </InfoTip>
        </div>
        <button className="btn sm" disabled={refreshing} onClick={onRefresh}
          aria-label="Refresh deployment history" title="Refresh deployment history">
          {refreshing ? <Spinner size={12} /> : <RotateCw size={12} />}
        </button>
      </div>
      {error && (
        <div className="field-error" style={{ marginBottom: 8 }}>
          couldn&rsquo;t {loaded ? 'refresh' : 'load'} deployments: {error}
        </div>
      )}
      {!loaded && !error && <SkeletonRows count={3} height={38} />}
      {loaded && deployments.length > 0 && (
        <div className="list">
          {deployments.map((d) => {
            const live = liveLogs[d.id];
            const building = d.status === 'building' || d.status === 'pending';
            const expanded = open === d.id || building;
            return (
              <div key={d.id} className="list-row" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
                <button type="button" className="row spread disclosure"
                  aria-expanded={expanded} aria-controls={`deploy-${d.id}`}
                  onClick={() => setOpen(open === d.id ? '' : d.id)}>
                  <div className="row">
                    <Led status={d.status} />
                    <span className="mono">v{d.version}</span>
                    <span className="tag">{d.trigger}</span>
                    <span className="dim">{d.status}</span>
                    {building && live && <span className="badge accent">live</span>}
                  </div>
                  <span className="faint">{new Date(d.startedAt).toLocaleString()} · {(d.durationMs / 1000).toFixed(1)}s</span>
                </button>
                {expanded && (
                  <div className="terminal" id={`deploy-${d.id}`} style={{ maxHeight: 420 }}>
                    {live || d.log || (building ? 'waiting for the machine to pick up the job…' : '(no build log)')}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      {loaded && deployments.length === 0 && <div className="faint">No deployments recorded yet.</div>}
    </div>
  );
}

/* ---------- Settings (all editable config + danger zone) ---------- */

function SettingsTab({ svc, draft, setDraft, nodes, busy, deleting, errors, onDelete }: {
  svc: Service; draft: Draft; setDraft: (d: Draft) => void;
  nodes: ReturnType<typeof useData>['nodes']; busy: boolean; deleting: boolean;
  errors: DraftErrors; onDelete: () => void;
}) {
  const toast = useToast();
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [browse, setBrowse] = useState<{ mountRid: string; path: string; entries: DirEntry[] } | null>(null);
  const drives = (nodes.find((n) => n.id === draft.nodeId)?.hardware.drives ?? []).filter((d) => d.mountPoint);
  // Adopted containers are managed where they were created: their config is
  // shown read-only up front instead of accepting edits that could never
  // apply. Lifecycle (start/stop/remove) still works from the header.
  const readOnly = !!svc.adopted;

  useEffect(() => { api.placement({}).then(setScores).catch(() => {}); }, []);

  const setEnv = (rid: string, patch: Partial<EnvVar>) =>
    setDraft({ ...draft, env: draft.env.map((v) => (v.rid === rid ? { ...v, ...patch } : v)) });

  const setMount = (rid: string, patch: Partial<Mount>) =>
    setDraft({ ...draft, mounts: draft.mounts.map((m) => (m.rid === rid ? { ...m, ...patch } : m)) });

  const openBrowser = async (mountRid: string, start?: string) => {
    try {
      const r = await api.nodeFS(draft.nodeId, start ?? '/');
      setBrowse({ mountRid, path: r.path, entries: r.entries ?? [] });
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="stack">
      {readOnly && (
        <div className="card warn row" style={{ gap: 10, padding: '10px 14px' }}>
          <TriangleAlert size={14} style={{ color: 'var(--warn)', flex: 'none' }} aria-hidden />
          <span className="dim">
            This container was adopted: it already existed on the machine. Start, stop, and remove
            work here, but its configuration lives where it was created, so these settings are read-only.
          </span>
        </div>
      )}

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>Source</div>
        <div className="grid-2">
          <Field label="Image">
            {(p) => (
              <input className="input mono" {...p} value={draft.image} readOnly={readOnly}
                placeholder={svc.gitRepo ? 'built from git' : 'image:tag'}
                onChange={(e) => setDraft({ ...draft, image: e.target.value })} />
            )}
          </Field>
          <Field label="Command override">
            {(p) => (
              <input className="input mono" {...p} value={draft.command} readOnly={readOnly} placeholder="(image default)"
                onChange={(e) => setDraft({ ...draft, command: e.target.value })} />
            )}
          </Field>
        </div>
        {svc.gitRepo && <div className="dim mono">{svc.gitRepo} @ {svc.gitBranch || 'main'}</div>}
      </div>

      {/* Ports are draft-based like everything else on this tab: they save
          and apply with the rest of the config, so they live here rather than
          among Networking's instant actions. */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>
          Ports
          <InfoTip wide>
            Two different ports: the one your app listens on <em>inside</em> its container, and
            the host port it&rsquo;s published on (what URLs and routing use). If your preferred
            host port is taken, RackOps bumps to the next free one. Changes apply on
            Save &amp; redeploy.
          </InfoTip>
        </div>
        <div className="grid-3">
          <Field label="App listens on (container)" style={{ marginBottom: 0 }} error={errors.containerPort}>
            {(p) => (
              <input className="input mono" {...p} inputMode="numeric" placeholder="e.g. 3000"
                value={draft.containerPort} readOnly={readOnly}
                onChange={(e) => setDraft({ ...draft, containerPort: e.target.value.replace(/\D/g, '') })} />
            )}
          </Field>
          <Field label="Reachable on host port" style={{ marginBottom: 0 }} error={errors.port}>
            {(p) => (
              <input className="input mono" {...p} inputMode="numeric" value={draft.port} readOnly={readOnly}
                onChange={(e) => setDraft({ ...draft, port: e.target.value.replace(/\D/g, '') })} />
            )}
          </Field>
          <Field label="Extra ports (comma-separated)" style={{ marginBottom: 0 }} error={errors.ports}>
            {(p) => (
              <input className="input mono" {...p} placeholder="9000, 5432" value={draft.ports} readOnly={readOnly}
                onChange={(e) => setDraft({ ...draft, ports: e.target.value })} />
            )}
          </Field>
        </div>
        {draft.containerPort && draft.port && draft.containerPort !== draft.port && (
          <div className="faint" style={{ marginTop: 8 }}>Publishes host <span className="mono">{draft.port}</span> → container <span className="mono">{draft.containerPort}</span>.</div>
        )}
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>
          Environment variables
          <InfoTip wide>
            Passed to the container at start. The key icon marks a value as a secret: masked
            here and redacted from API and AI-tool output. Changes apply on Save &amp; redeploy.
          </InfoTip>
        </div>
        {draft.env.map((v, i) => (
          <div key={v.rid} style={{ marginBottom: 8 }}>
            <div className="row">
              <input className="input mono" placeholder="KEY" value={v.key} readOnly={readOnly}
                aria-label={`Variable ${i + 1} name`}
                aria-invalid={!!errors.env[v.rid]}
                aria-describedby={errors.env[v.rid] ? `env-err-${v.rid}` : undefined}
                onChange={(e) => setEnv(v.rid, { key: e.target.value })} />
              <input className="input mono" placeholder="value" type={v.secret ? 'password' : 'text'}
                value={v.value} readOnly={readOnly}
                aria-label={v.key ? `Value for ${v.key}` : `Variable ${i + 1} value`}
                onChange={(e) => setEnv(v.rid, { value: e.target.value })} />
              <button className="btn sm" aria-pressed={v.secret} disabled={readOnly}
                title={v.secret ? 'Shown as secret' : 'Mask as secret'}
                aria-label={v.secret ? `Stop masking ${v.key || 'this variable'}` : `Mask ${v.key || 'this variable'} as secret`}
                onClick={() => setEnv(v.rid, { secret: !v.secret })}>
                <KeyRound size={12} style={v.secret ? { color: 'var(--accent)' } : undefined} />
              </button>
              <button className="btn sm danger" disabled={readOnly}
                aria-label={`Remove ${v.key || `variable ${i + 1}`}`}
                onClick={() => setDraft({ ...draft, env: draft.env.filter((x) => x.rid !== v.rid) })}><Trash2 size={12} /></button>
            </div>
            {errors.env[v.rid] && (
              <div className="field-error" id={`env-err-${v.rid}`} style={{ marginTop: 4 }}>{errors.env[v.rid]}</div>
            )}
          </div>
        ))}
        <button className="btn sm" disabled={readOnly}
          onClick={() => setDraft({ ...draft, env: [...draft.env, { rid: crypto.randomUUID(), key: '', value: '', secret: false }] })}>
          <Plus size={12} /> Add variable
        </button>
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 6 }}>Storage & mounts</div>
        <div className="dim" style={{ marginBottom: 14 }}>
          Bind mounts attach real host folders (like a media library). Managed volumes are created by
          RackOps, optionally on a specific drive. Changes apply on Save & redeploy.
        </div>
        {draft.mounts.map((m, i) => (
          <div className="card inset" key={m.rid} style={{ marginBottom: 10 }}>
            <div className="row" style={{ marginBottom: 8 }}>
              <select className="input" style={{ flex: '0 1 150px', minWidth: 120 }} value={m.type}
                aria-label={`Mount ${i + 1} type`} disabled={readOnly}
                onChange={(e) => setMount(m.rid, { type: e.target.value as Mount['type'], source: '', drivePath: '' })}>
                <option value="bind">Host folder</option>
                <option value="volume">Managed volume</option>
              </select>
              {m.type === 'bind' ? (
                <>
                  <input className="input mono" placeholder="/mnt/media/movies" value={m.source} readOnly={readOnly}
                    aria-label={`Mount ${i + 1} host folder`}
                    onChange={(e) => setMount(m.rid, { source: e.target.value })} />
                  <button className="btn sm" title="Browse host folders" disabled={readOnly}
                    aria-label={`Browse host folders for mount ${i + 1}`} onClick={() => openBrowser(m.rid, m.source || '/')}>
                    <FolderOpen size={12} />
                  </button>
                </>
              ) : (
                <>
                  <input className="input mono" placeholder="volume name" value={m.source} readOnly={readOnly}
                    aria-label={`Mount ${i + 1} volume name`}
                    onChange={(e) => setMount(m.rid, { source: e.target.value })} />
                  <select className="input" style={{ flex: '1 1 200px', minWidth: 140 }} value={m.drivePath ?? ''}
                    aria-label={`Mount ${i + 1} drive`} disabled={readOnly}
                    onChange={(e) => setMount(m.rid, { drivePath: e.target.value })}>
                    <option value="">Default docker storage</option>
                    {drives.map((d) => (
                      <option key={d.device} value={d.mountPoint}>
                        {d.device} ({d.type.toUpperCase()}, {(d.sizeGb - d.usedGb).toFixed(0)} GB free) at {d.mountPoint}
                      </option>
                    ))}
                  </select>
                </>
              )}
            </div>
            <div className="row">
              <span className="dim" aria-hidden>→ container path:</span>
              <input className="input mono" placeholder="/media" value={m.target} readOnly={readOnly}
                aria-label={`Mount ${i + 1} container path`}
                onChange={(e) => setMount(m.rid, { target: e.target.value })} />
              <label className="check" style={{ whiteSpace: 'nowrap' }}>
                <input type="checkbox" checked={m.readOnly} disabled={readOnly}
                  onChange={(e) => setMount(m.rid, { readOnly: e.target.checked })} /> read-only
              </label>
              <button className="btn sm danger" disabled={readOnly} aria-label={`Remove mount ${i + 1}`}
                onClick={() => setDraft({ ...draft, mounts: draft.mounts.filter((x) => x.rid !== m.rid) })}><Trash2 size={12} /></button>
            </div>
          </div>
        ))}
        <button className="btn sm" disabled={readOnly}
          onClick={() => setDraft({ ...draft, mounts: [...draft.mounts, { rid: crypto.randomUUID(), type: 'bind', source: '', target: '', readOnly: false }] })}>
          <Plus size={12} /> Add mount
        </button>
        {(svc.volumes ?? []).length > 0 && (
          <div className="faint" style={{ marginTop: 12 }}>
            Template volumes (auto-managed): {(svc.volumes ?? []).join(', ')}
          </div>
        )}
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>
          Resources &amp; machine
          <InfoTip wide>
            Limits cap what the container may use; 0 means unlimited. Choosing a different
            machine migrates the app on Save &amp; redeploy: the container moves, but data in
            managed volumes stays on the old machine&rsquo;s disk.
          </InfoTip>
        </div>
        <div className="grid-3">
          <Field label="Memory limit (MB, 0 = none)">
            {(p) => (
              <input className="input mono" {...p} inputMode="numeric" value={draft.memLimitMb} readOnly={readOnly}
                onChange={(e) => setDraft({ ...draft, memLimitMb: e.target.value.replace(/\D/g, '') })} />
            )}
          </Field>
          <Field label="CPU limit (cores, 0 = none)">
            {(p) => (
              <input className="input mono" {...p} inputMode="decimal" placeholder="1.5" value={draft.cpuLimit} readOnly={readOnly}
                onChange={(e) => setDraft({ ...draft, cpuLimit: e.target.value.replace(/[^0-9.]/g, '') })} />
            )}
          </Field>
          <Field label="Restart policy">
            {(p) => (
              <select className="input" {...p} value={draft.restartPolicy} disabled={readOnly}
                onChange={(e) => setDraft({ ...draft, restartPolicy: e.target.value })}>
                <option value="unless-stopped">unless-stopped</option>
                <option value="always">always</option>
                <option value="on-failure">on-failure</option>
                <option value="no">no</option>
              </select>
            )}
          </Field>
        </div>
        <label className="check" style={{ marginBottom: 14 }}>
          <input type="checkbox" checked={draft.gpu} disabled={readOnly}
            onChange={(e) => setDraft({ ...draft, gpu: e.target.checked })} /> GPU access (--gpus all)
        </label>
        <Field label="Runs on"
          hint="Changing this migrates the app on Save &amp; redeploy; managed volumes don't move automatically."
          style={{ marginBottom: 0 }}>
          {(p) => (
            <select className="input" {...p} value={draft.nodeId} disabled={readOnly}
              onChange={(e) => setDraft({ ...draft, nodeId: e.target.value })}>
              {nodes.map((n) => {
                const sc = scores.find((s) => s.nodeId === n.id);
                return <option key={n.id} value={n.id}>{n.name} {sc ? `(score ${sc.score})` : ''} {n.status !== 'online' ? '· OFFLINE' : ''}</option>;
              })}
            </select>
          )}
        </Field>
      </div>

      <div className="card crit">
        <div className="eyebrow" style={{ marginBottom: 8, color: 'var(--crit)' }}>
          <TriangleAlert size={12} aria-hidden /> Danger zone
        </div>
        <div className="row spread wrap">
          <div>
            <strong>Remove this app</strong>
            <div className="faint">Deletes the container. Managed volume data stays on disk.</div>
          </div>
          <button className="btn danger" disabled={busy} onClick={onDelete}>
            {deleting ? <Spinner size={13} /> : <Trash2 size={13} />} Remove app
          </button>
        </div>
      </div>

      {/* Shared Modal, so the folder browser gets Escape, a focus trap, and
          focus return like every other dialog in the product. */}
      {browse && (
        <Modal title={browse.path} icon={<FolderOpen size={15} />} width={540} onClose={() => setBrowse(null)}>
          <div className="row" style={{ marginBottom: 10 }}>
            <button className="btn sm" disabled={browse.path === '/'}
              onClick={() => openBrowser(browse.mountRid, browse.path.split('/').slice(0, -1).join('/') || '/')}>
              <ArrowLeft size={12} /> Up
            </button>
            <button className="btn sm primary" onClick={() => { setMount(browse.mountRid, { source: browse.path }); setBrowse(null); }}>
              Use this folder
            </button>
          </div>
          <div style={{ maxHeight: 340, overflowY: 'auto' }}>
            {browse.entries.filter((e) => e.isDir).map((e) => (
              <button key={e.name} className="nav-item" style={{ width: '100%' }}
                onClick={() => openBrowser(browse.mountRid, (browse.path === '/' ? '' : browse.path) + '/' + e.name)}>
                <Folder size={13} aria-hidden /> {e.name}
              </button>
            ))}
            {browse.entries.filter((e) => e.isDir).length === 0 && <div className="faint" style={{ padding: 10 }}>no subfolders</div>}
          </div>
        </Modal>
      )}
    </div>
  );
}
