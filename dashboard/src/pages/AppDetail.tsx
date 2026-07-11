import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import {
  ArrowLeft, Box, Cloud, Globe, History, KeyRound, Play, Plus, RotateCw, Save, ScrollText,
  Square, Trash2, TriangleAlert, Rocket, Folder, FolderOpen, Zap, Settings as SettingsIcon,
  ExternalLink, X,
} from 'lucide-react';
import { api } from '../api';
import type { Service, ServiceUpdate, Mount, EnvVar, Deployment, Domain, DirEntry, PlacementScore } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { StatusBadge, Led, Spinner, UrlChip, publicUrls, dnsLabel, timeAgo } from '../components/ui';

type Tab = 'overview' | 'networking' | 'logs' | 'deployments' | 'settings';

const TABS: { key: Tab; label: string; icon: React.ReactNode }[] = [
  { key: 'overview', label: 'Overview', icon: <Box size={14} /> },
  { key: 'networking', label: 'Networking', icon: <Globe size={14} /> },
  { key: 'logs', label: 'Logs', icon: <ScrollText size={14} /> },
  { key: 'deployments', label: 'Deployments', icon: <History size={14} /> },
  { key: 'settings', label: 'Settings', icon: <SettingsIcon size={14} /> },
];

// Draft holds the editable copy of a service; saved via PATCH.
interface Draft {
  image: string; command: string; port: string; containerPort: string; ports: string;
  env: EnvVar[]; mounts: Mount[];
  memLimitMb: string; cpuLimit: string; restartPolicy: string; gpu: boolean; nodeId: string;
}

function toDraft(s: Service): Draft {
  return {
    image: s.image ?? '', command: s.command ?? '',
    port: s.port ? String(s.port) : '', containerPort: s.containerPort ? String(s.containerPort) : '',
    ports: (s.ports ?? []).join(', '),
    env: (s.env ?? []).map((e) => ({ ...e })),
    mounts: (s.mounts ?? []).map((m) => ({ ...m })),
    memLimitMb: s.memLimitMb ? String(s.memLimitMb) : '',
    cpuLimit: s.cpuLimit ? String(s.cpuLimit) : '',
    restartPolicy: s.restartPolicy || 'unless-stopped',
    gpu: !!s.gpu, nodeId: s.nodeId,
  };
}

export function AppDetail() {
  const { id = '', tab: tabParam } = useParams();
  const tab = (TABS.some((t) => t.key === tabParam) ? tabParam : 'overview') as Tab;
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();
  const { nodes, ingress, refresh, nodeName } = useData();

  const [svc, setSvc] = useState<Service | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [baseline, setBaseline] = useState('');
  const [busy, setBusy] = useState(false);
  const [notFound, setNotFound] = useState(false);

  const load = useCallback(() => {
    api.service(id).then((s) => {
      setSvc(s);
      const d = toDraft(s);
      setDraft(d);
      setBaseline(JSON.stringify(d));
    }).catch(() => setNotFound(true));
  }, [id]);
  useEffect(load, [load]);

  // Live status refresh (never clobbers the draft being edited).
  useEffect(() => {
    const t = setInterval(() => {
      api.service(id).then((s) => setSvc(s)).catch(() => {});
    }, 4000);
    return () => clearInterval(t);
  }, [id]);

  const dirty = useMemo(() => draft && JSON.stringify(draft) !== baseline, [draft, baseline]);
  const node = nodes.find((n) => n.id === svc?.nodeId);
  const urls = svc ? publicUrls(svc, ingress) : [];

  const save = async (redeploy: boolean) => {
    if (!draft || !svc) return;
    setBusy(true);
    try {
      const upd: ServiceUpdate = {
        image: draft.image, command: draft.command,
        port: parseInt(draft.port, 10) || 0,
        containerPort: parseInt(draft.containerPort, 10) || 0,
        ports: draft.ports.split(',').map((p) => parseInt(p.trim(), 10)).filter((p) => p > 0),
        env: draft.env.filter((e) => e.key),
        mounts: draft.mounts.filter((m) => m.target),
        memLimitMb: parseInt(draft.memLimitMb, 10) || 0,
        cpuLimit: parseFloat(draft.cpuLimit) || 0,
        restartPolicy: draft.restartPolicy, gpu: draft.gpu,
        nodeId: draft.nodeId, redeploy,
      };
      const s = await api.updateService(svc.id, upd);
      setSvc(s);
      const d = toDraft(s);
      setDraft(d);
      setBaseline(JSON.stringify(d));
      refresh();
      toast.ok(redeploy ? 'Saved — redeploying now.' : 'Saved. Changes apply on the next redeploy.');
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const act = async (action: string) => {
    if (!svc) return;
    setBusy(true);
    try {
      if (action === 'delete') {
        const yes = await confirm({
          title: `Remove ${svc.name}?`,
          body: 'The container is deleted from its machine. Data in managed volumes stays on disk under rackops-volumes/.',
          confirmLabel: 'Remove app', danger: true,
        });
        if (yes) {
          await api.deleteService(svc.id);
          toast.ok(`${svc.name} removed.`);
          refresh();
          navigate('/apps');
        }
        setBusy(false);
        return;
      }
      await api.serviceAction(svc.id, action);
      toast.ok(`${svc.name}: ${action} requested.`);
      load(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  if (notFound) {
    return (
      <div className="page">
        <div className="empty">
          <div className="empty-title">This app doesn't exist anymore</div>
          <Link to="/apps" className="btn" style={{ marginTop: 10 }}>Back to apps</Link>
        </div>
      </div>
    );
  }
  if (!svc || !draft) return <div className="page"><div className="row dim"><Spinner /> loading…</div></div>;

  return (
    <div className="page">
      <div className="row spread wrap" style={{ marginBottom: 4 }}>
        <div className="row">
          <button className="btn sm ghost" onClick={() => navigate('/apps')} aria-label="Back to apps"><ArrowLeft size={14} /></button>
          <div className="page-title" style={{ margin: 0 }}>{svc.name}</div>
          <StatusBadge status={svc.status} />
          {svc.templateId && <span className="tag">{svc.templateId}</span>}
        </div>
        <div className="row">
          <button className="btn sm" disabled={busy} onClick={() => act(svc.status === 'running' ? 'stop' : 'start')}>
            {svc.status === 'running' ? <><Square size={12} /> Stop</> : <><Play size={12} /> Start</>}
          </button>
          <button className="btn sm" disabled={busy} onClick={() => act('restart')}><RotateCw size={12} /> Restart</button>
          <button className="btn sm" disabled={busy} onClick={() => act('redeploy')}><Rocket size={12} /> Redeploy</button>
        </div>
      </div>
      <div className="row wrap" style={{ marginBottom: 16, gap: 8 }}>
        <span className="dim mono">{svc.image || svc.gitRepo || svc.kind} · {nodeName(svc.nodeId)}</span>
        {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
      </div>

      {dirty && (
        <div className="card row spread" style={{ borderColor: 'var(--warn)', marginBottom: 14, padding: '10px 16px' }}>
          <span className="dim">Unsaved changes — the app keeps running until you apply them.</span>
          <div className="row">
            <button className="btn sm" disabled={busy} onClick={() => setDraft(JSON.parse(baseline))}>Discard</button>
            <button className="btn sm" disabled={busy} onClick={() => save(false)}><Save size={12} /> Save only</button>
            <button className="btn sm primary" disabled={busy} onClick={() => save(true)}><Rocket size={12} /> Save & redeploy</button>
          </div>
        </div>
      )}

      <div className="tabs">
        {TABS.map((t) => (
          <Link key={t.key} to={`/apps/${svc.id}/${t.key}`} className={`tab ${tab === t.key ? 'active' : ''}`}>
            {t.icon} {t.label}
          </Link>
        ))}
      </div>

      {tab === 'overview' && <OverviewTab svc={svc} nodeLabel={node?.name ?? svc.nodeId} urls={urls.map((u) => u.url)} />}
      {tab === 'networking' && <NetworkingTab svc={svc} draft={draft} setDraft={setDraft} reload={load} />}
      {tab === 'logs' && <LogsTab svc={svc} />}
      {tab === 'deployments' && <DeploymentsTab serviceId={svc.id} />}
      {tab === 'settings' && (
        <SettingsTab svc={svc} draft={draft} setDraft={setDraft} nodes={nodes} busy={busy} onDelete={() => act('delete')} />
      )}
    </div>
  );
}

/* ---------- Overview ---------- */

function OverviewTab({ svc, nodeLabel, urls }: { svc: Service; nodeLabel: string; urls: string[] }) {
  return (
    <>
      <div className="kpi-row">
        <div className="card"><div className="eyebrow">CPU</div><div className="kpi-value">{svc.cpuPct.toFixed(1)}<span className="unit">%</span></div></div>
        <div className="card"><div className="eyebrow">Memory</div><div className="kpi-value">{svc.memUsedMb}<span className="unit">MB{svc.memLimitMb ? ` / ${svc.memLimitMb}` : ''}</span></div></div>
        <div className="card"><div className="eyebrow">Port</div><div className="kpi-value">{svc.port || '—'}</div></div>
        <div className="card"><div className="eyebrow">Machine</div><div className="kpi-value" style={{ fontSize: 16 }}>{nodeLabel}</div></div>
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 10 }}>Details</div>
        <table style={{ borderCollapse: 'collapse', width: '100%' }}>
          <tbody>
            {[
              ['Source', svc.image || svc.gitRepo || `${svc.kind} stack`],
              ['Public URLs', urls.length ? urls.join('  ·  ') : 'none yet — set one up in Networking'],
              ['Container', svc.containerId || 'not created yet'],
              ['Restart policy', svc.restartPolicy || 'unless-stopped'],
              ['Created', `${new Date(svc.createdAt).toLocaleString()} (${timeAgo(svc.createdAt)})`],
            ].map(([k, v]) => (
              <tr key={k as string}>
                <td className="dim" style={{ padding: '7px 24px 7px 0', whiteSpace: 'nowrap', verticalAlign: 'top' }}>{k}</td>
                <td className="mono" style={{ padding: '7px 0', fontSize: 12, wordBreak: 'break-all' }}>{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

/* ---------- Networking: the make-it-public journey ---------- */

function NetworkingTab({ svc, draft, setDraft, reload }: {
  svc: Service; draft: Draft; setDraft: (d: Draft) => void; reload: () => void;
}) {
  const toast = useToast();
  const { nodes, ingress, refresh } = useData();
  const [domains, setDomains] = useState<Domain[]>([]);
  const [tunnelBusy, setTunnelBusy] = useState(false);
  const [exposeBusy, setExposeBusy] = useState(false);
  const node = nodes.find((n) => n.id === svc.nodeId);

  useEffect(() => { api.domains().then((d) => setDomains(d ?? [])).catch(() => {}); }, []);

  // While a tunnel is starting, its URL lands via the 4s status refresh.
  useEffect(() => { if (svc.quickTunnelUrl) setTunnelBusy(false); }, [svc.quickTunnelUrl]);

  const setRoute = async (host: string) => {
    try {
      await api.setServiceDomain(svc.id, host, !host ? 'none' : 'static');
      toast.ok(host ? `Routing ${host} to ${svc.name}.` : 'Public route removed.');
      reload(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
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
      toast.info('Tunnel starting — the URL appears here in ~15s (first run pulls cloudflared).');
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
  const currentIsWildcard = domains.find((d) => d.wildcard && (svc.domain || '').endsWith('.' + d.name.slice(2)));

  return (
    <>
      {/* Reachability now */}
      <div className="card" style={{ marginBottom: 14 }}>
        <div className="eyebrow" style={{ marginBottom: 10 }}>Reachable at</div>
        <div className="row wrap">
          {publicUrls(svc, ingress).map((u) => <UrlChip key={u.url} url={u.url} />)}
          {node && svc.port > 0 && <UrlChip url={`http://${node.ip}:${svc.port}`} />}
          {(!svc.port || !node) && <span className="faint">this app publishes no port{svc.kind === 'compose' ? ' — ports come from the compose file' : ''}</span>}
        </div>
      </div>

      {svc.port > 0 && (
        <div className="card" style={{ marginBottom: 14 }}>
          <div className="eyebrow" style={{ marginBottom: 12 }}>Make it public</div>
          <div className="stack" style={{ gap: 10 }}>

            {/* Option 1: instant URL */}
            <div className="row spread" style={{ alignItems: 'flex-start' }}>
              <div>
                <div className="row" style={{ gap: 7 }}><Zap size={13} style={{ color: 'var(--amber)' }} /><strong>Instant URL</strong></div>
                <div className="dim" style={{ marginTop: 3 }}>
                  A free {`<app>.<ip>.nip.io`} address through your own edge — no DNS setup.
                  {!edgeReady && ' Needs the edge proxy running (Network page).'}
                </div>
              </div>
              <button className="btn sm" disabled={exposeBusy || !edgeReady} onClick={expose}>
                {exposeBusy ? <Spinner size={12} /> : <Zap size={12} />} Get instant URL
              </button>
            </div>
            <hr className="divider" style={{ margin: '2px 0' }} />

            {/* Option 2: quick tunnel */}
            <div className="row spread" style={{ alignItems: 'flex-start' }}>
              <div>
                <div className="row" style={{ gap: 7 }}><Cloud size={13} style={{ color: 'var(--amber)' }} /><strong>Quick tunnel</strong></div>
                <div className="dim" style={{ marginTop: 3 }}>
                  Public HTTPS URL that works behind any router — no account, no port forwarding.
                  For demos and testing: the URL changes when it restarts.
                </div>
                {svc.quickTunnelUrl && (
                  <a className="mono" style={{ fontSize: 12, marginTop: 6, display: 'inline-block' }}
                    href={svc.quickTunnelUrl} target="_blank" rel="noreferrer">
                    {svc.quickTunnelUrl} <ExternalLink size={10} />
                  </a>
                )}
              </div>
              {svc.quickTunnelUrl ? (
                <button className="btn sm danger" onClick={stopTunnel}>Stop tunnel</button>
              ) : (
                <button className="btn sm" disabled={tunnelBusy} onClick={startTunnel}>
                  {tunnelBusy ? <Spinner size={12} /> : <Cloud size={12} />} {tunnelBusy ? 'Starting…' : 'Start tunnel'}
                </button>
              )}
            </div>
            <hr className="divider" style={{ margin: '2px 0' }} />

            {/* Option 3: own domain */}
            <div>
              <div className="row" style={{ gap: 7 }}><Globe size={13} style={{ color: 'var(--amber)' }} /><strong>Your own domain</strong></div>
              <div className="dim" style={{ margin: '3px 0 10px' }}>
                Route a domain you own to this app.
                {domains.length === 0 && <> First <Link to="/network">add the domain</Link> so RackOps can give you the DNS record.</>}
              </div>
              <div className="row wrap">
                <select className="input" style={{ width: 320 }}
                  value={currentIsWildcard?.name ?? (svc.domain || '')}
                  onChange={(e) => {
                    let host = e.target.value;
                    if (host.startsWith('*.')) host = `${dnsLabel(svc.name)}.${host.slice(2)}`;
                    setRoute(host);
                  }}>
                  <option value="">— no domain route —</option>
                  {svc.domain && !domains.some((d) => d.name === svc.domain || (d.wildcard && svc.domain.endsWith('.' + d.name.slice(2)))) &&
                    <option value={svc.domain}>{svc.domain} (instant)</option>}
                  {domains.map((d) => (
                    <option key={d.id} value={d.name}>
                      {d.wildcard ? `${dnsLabel(svc.name)}.${d.name.slice(2)} (via ${d.name})` : d.name}
                      {d.status !== 'verified' ? ' — unverified' : ''}
                    </option>
                  ))}
                </select>
                {svc.domain && <span className="dim mono">current: {svc.domain}</span>}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Ports */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>Ports</div>
        <div className="grid-3">
          <div className="field" style={{ marginBottom: 0 }}>
            <label>App listens on (container)</label>
            <input className="input mono" placeholder="e.g. 3000" value={draft.containerPort}
              onChange={(e) => setDraft({ ...draft, containerPort: e.target.value.replace(/\D/g, '') })} />
          </div>
          <div className="field" style={{ marginBottom: 0 }}>
            <label>Reachable on host port</label>
            <input className="input mono" value={draft.port}
              onChange={(e) => setDraft({ ...draft, port: e.target.value.replace(/\D/g, '') })} />
          </div>
          <div className="field" style={{ marginBottom: 0 }}>
            <label>Extra ports (comma-separated)</label>
            <input className="input mono" placeholder="9000, 5432" value={draft.ports}
              onChange={(e) => setDraft({ ...draft, ports: e.target.value })} />
          </div>
        </div>
        {draft.containerPort && draft.port && draft.containerPort !== draft.port && (
          <div className="faint" style={{ marginTop: 8 }}>Publishes host <span className="mono">{draft.port}</span> → container <span className="mono">{draft.containerPort}</span>.</div>
        )}
      </div>
    </>
  );
}

/* ---------- Logs ---------- */

function LogsTab({ svc }: { svc: Service }) {
  const toast = useToast();
  const [logs, setLogs] = useState('');
  const [loading, setLoading] = useState(false);
  const [follow, setFollow] = useState(true);

  const fetchLogs = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try { setLogs((await api.serviceLogs(svc.id)).logs || '(no output yet)'); }
    catch (e) { setLogs(cleanError(e)); }
    setLoading(false);
  }, [svc.id]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);
  // Follow mode: live tail every 5s while the tab is open.
  useEffect(() => {
    if (!follow) return;
    const t = setInterval(() => fetchLogs(true), 5000);
    return () => clearInterval(t);
  }, [follow, fetchLogs]);

  return (
    <div className="card">
      <div className="row spread" style={{ marginBottom: 10 }}>
        <div className="row">
          <strong>Runtime logs</strong>
          {follow && <span className="badge amber">following</span>}
        </div>
        <div className="row">
          <label className="check">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> auto-refresh
          </label>
          <button className="btn sm" disabled={loading} onClick={() => fetchLogs()}>
            {loading ? <Spinner size={12} /> : <RotateCw size={12} />} Refresh
          </button>
          <button className="btn sm danger" onClick={async () => {
            await api.clearServiceLogs(svc.id).catch((e) => toast.err(cleanError(e)));
            fetchLogs();
          }}>
            <Trash2 size={12} /> Clear old
          </button>
        </div>
      </div>
      <div className="terminal" style={{ maxHeight: 520 }}>
        {loading && !logs ? 'fetching from the machine…' : logs}
      </div>
    </div>
  );
}

/* ---------- Deployments ---------- */

function DeploymentsTab({ serviceId }: { serviceId: string }) {
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [open, setOpen] = useState('');

  const fetch = useCallback(() => {
    api.serviceDeployments(serviceId).then((d) => setDeployments(d ?? [])).catch(() => {});
  }, [serviceId]);
  useEffect(fetch, [fetch]);

  return (
    <div className="card">
      <div className="row spread" style={{ marginBottom: 10 }}>
        <strong>Deployment history</strong>
        <button className="btn sm" onClick={fetch}><RotateCw size={12} /></button>
      </div>
      {deployments.map((d) => (
        <div key={d.id} style={{ borderTop: '1px solid var(--border)', padding: '9px 0' }}>
          <div className="row spread" style={{ cursor: 'pointer' }} onClick={() => setOpen(open === d.id ? '' : d.id)}>
            <div className="row">
              <Led status={d.status} />
              <span className="mono">v{d.version}</span>
              <span className="tag">{d.trigger}</span>
              <span className="dim">{d.status}</span>
            </div>
            <span className="faint">{new Date(d.startedAt).toLocaleString()} · {(d.durationMs / 1000).toFixed(1)}s</span>
          </div>
          {open === d.id && (
            <div className="terminal" style={{ marginTop: 8, maxHeight: 420 }}>{d.log || '(no build log)'}</div>
          )}
        </div>
      ))}
      {deployments.length === 0 && <div className="faint">No deployments recorded yet.</div>}
    </div>
  );
}

/* ---------- Settings (all editable config + danger zone) ---------- */

function SettingsTab({ svc, draft, setDraft, nodes, busy, onDelete }: {
  svc: Service; draft: Draft; setDraft: (d: Draft) => void;
  nodes: ReturnType<typeof useData>['nodes']; busy: boolean; onDelete: () => void;
}) {
  const toast = useToast();
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [browse, setBrowse] = useState<{ mountIdx: number; path: string; entries: DirEntry[] } | null>(null);
  const drives = (nodes.find((n) => n.id === draft.nodeId)?.hardware.drives ?? []).filter((d) => d.mountPoint);

  useEffect(() => { api.placement({}).then(setScores).catch(() => {}); }, []);

  const setMount = (i: number, patch: Partial<Mount>) =>
    setDraft({ ...draft, mounts: draft.mounts.map((m, j) => (j === i ? { ...m, ...patch } : m)) });

  const openBrowser = async (mountIdx: number, start?: string) => {
    try {
      const r = await api.nodeFS(draft.nodeId, start ?? '/');
      setBrowse({ mountIdx, path: r.path, entries: r.entries ?? [] });
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="stack">
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>Source</div>
        <div className="grid-2">
          <div className="field"><label>Image</label>
            <input className="input mono" value={draft.image} placeholder={svc.gitRepo ? 'built from git' : 'image:tag'}
              onChange={(e) => setDraft({ ...draft, image: e.target.value })} /></div>
          <div className="field"><label>Command override</label>
            <input className="input mono" value={draft.command} placeholder="(image default)"
              onChange={(e) => setDraft({ ...draft, command: e.target.value })} /></div>
        </div>
        {svc.gitRepo && <div className="dim mono">{svc.gitRepo} @ {svc.gitBranch || 'main'}</div>}
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>Environment variables</div>
        {draft.env.map((v, i) => (
          <div className="row" key={i} style={{ marginBottom: 8 }}>
            <input className="input mono" placeholder="KEY" value={v.key}
              onChange={(e) => setDraft({ ...draft, env: draft.env.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)) })} />
            <input className="input mono" placeholder="value" type={v.secret ? 'password' : 'text'} value={v.value}
              onChange={(e) => setDraft({ ...draft, env: draft.env.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)) })} />
            <button className="btn sm" title={v.secret ? 'Shown as secret' : 'Mask as secret'}
              onClick={() => setDraft({ ...draft, env: draft.env.map((x, j) => (j === i ? { ...x, secret: !x.secret } : x)) })}>
              <KeyRound size={12} style={v.secret ? { color: 'var(--amber)' } : undefined} />
            </button>
            <button className="btn sm danger" aria-label="Remove variable"
              onClick={() => setDraft({ ...draft, env: draft.env.filter((_, j) => j !== i) })}><Trash2 size={12} /></button>
          </div>
        ))}
        <button className="btn sm" onClick={() => setDraft({ ...draft, env: [...draft.env, { key: '', value: '', secret: false }] })}>
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
          <div className="card inset" key={i} style={{ marginBottom: 10 }}>
            <div className="row" style={{ marginBottom: 8 }}>
              <select className="input" style={{ width: 150 }} value={m.type}
                onChange={(e) => setMount(i, { type: e.target.value as Mount['type'], source: '', drivePath: '' })}>
                <option value="bind">Host folder</option>
                <option value="volume">Managed volume</option>
              </select>
              {m.type === 'bind' ? (
                <>
                  <input className="input mono" placeholder="/mnt/media/movies" value={m.source}
                    onChange={(e) => setMount(i, { source: e.target.value })} />
                  <button className="btn sm" title="Browse host folders" onClick={() => openBrowser(i, m.source || '/')}>
                    <FolderOpen size={12} />
                  </button>
                </>
              ) : (
                <>
                  <input className="input mono" placeholder="volume name" value={m.source}
                    onChange={(e) => setMount(i, { source: e.target.value })} />
                  <select className="input" style={{ width: 230 }} value={m.drivePath ?? ''}
                    onChange={(e) => setMount(i, { drivePath: e.target.value })}>
                    <option value="">Default docker storage</option>
                    {drives.map((d) => (
                      <option key={d.device} value={d.mountPoint}>
                        {d.device} ({d.type.toUpperCase()}, {(d.sizeGb - d.usedGb).toFixed(0)} GB free) — {d.mountPoint}
                      </option>
                    ))}
                  </select>
                </>
              )}
            </div>
            <div className="row">
              <span className="dim">→ container path:</span>
              <input className="input mono" placeholder="/media" value={m.target}
                onChange={(e) => setMount(i, { target: e.target.value })} />
              <label className="check" style={{ whiteSpace: 'nowrap' }}>
                <input type="checkbox" checked={m.readOnly} onChange={(e) => setMount(i, { readOnly: e.target.checked })} /> read-only
              </label>
              <button className="btn sm danger" aria-label="Remove mount"
                onClick={() => setDraft({ ...draft, mounts: draft.mounts.filter((_, j) => j !== i) })}><Trash2 size={12} /></button>
            </div>
          </div>
        ))}
        <button className="btn sm" onClick={() => setDraft({ ...draft, mounts: [...draft.mounts, { type: 'bind', source: '', target: '', readOnly: false }] })}>
          <Plus size={12} /> Add mount
        </button>
        {(svc.volumes ?? []).length > 0 && (
          <div className="faint" style={{ marginTop: 12 }}>
            Template volumes (auto-managed): {(svc.volumes ?? []).join(', ')}
          </div>
        )}
      </div>

      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>Resources & machine</div>
        <div className="grid-3">
          <div className="field"><label>Memory limit (MB, 0 = none)</label>
            <input className="input mono" value={draft.memLimitMb}
              onChange={(e) => setDraft({ ...draft, memLimitMb: e.target.value.replace(/\D/g, '') })} /></div>
          <div className="field"><label>CPU limit (cores, 0 = none)</label>
            <input className="input mono" placeholder="1.5" value={draft.cpuLimit}
              onChange={(e) => setDraft({ ...draft, cpuLimit: e.target.value.replace(/[^0-9.]/g, '') })} /></div>
          <div className="field"><label>Restart policy</label>
            <select className="input" value={draft.restartPolicy} onChange={(e) => setDraft({ ...draft, restartPolicy: e.target.value })}>
              <option value="unless-stopped">unless-stopped</option>
              <option value="always">always</option>
              <option value="on-failure">on-failure</option>
              <option value="no">no</option>
            </select></div>
        </div>
        <label className="check" style={{ marginBottom: 14 }}>
          <input type="checkbox" checked={draft.gpu} onChange={(e) => setDraft({ ...draft, gpu: e.target.checked })} /> GPU access (--gpus all)
        </label>
        <div className="field" style={{ marginBottom: 0 }}>
          <label>Runs on (changing = migrate on Save & redeploy; managed volumes don't move automatically)</label>
          <select className="input" value={draft.nodeId} onChange={(e) => setDraft({ ...draft, nodeId: e.target.value })}>
            {nodes.map((n) => {
              const sc = scores.find((s) => s.nodeId === n.id);
              return <option key={n.id} value={n.id}>{n.name} {sc ? `(score ${sc.score})` : ''} {n.status !== 'online' ? '· OFFLINE' : ''}</option>;
            })}
          </select>
        </div>
      </div>

      <div className="card" style={{ borderColor: 'var(--crit)' }}>
        <div className="eyebrow" style={{ marginBottom: 8, color: 'var(--crit)' }}><TriangleAlert size={12} /> Danger zone</div>
        <div className="row spread">
          <div>
            <strong>Remove this app</strong>
            <div className="faint">Deletes the container. Managed volume data stays on disk.</div>
          </div>
          <button className="btn danger" disabled={busy} onClick={onDelete}><Trash2 size={13} /> Remove app</button>
        </div>
      </div>

      {browse && (
        <div className="modal-overlay" onClick={() => setBrowse(null)}>
          <div className="modal" style={{ width: 540 }} onClick={(e) => e.stopPropagation()}>
            <div className="row spread" style={{ marginBottom: 12 }}>
              <h2 style={{ margin: 0 }} className="row"><FolderOpen size={15} /> {browse.path}</h2>
              <button className="btn sm ghost" onClick={() => setBrowse(null)} aria-label="Close"><X size={14} /></button>
            </div>
            <div className="row" style={{ marginBottom: 10 }}>
              <button className="btn sm" disabled={browse.path === '/'}
                onClick={() => openBrowser(browse.mountIdx, browse.path.split('/').slice(0, -1).join('/') || '/')}>
                <ArrowLeft size={12} /> Up
              </button>
              <button className="btn sm primary" onClick={() => { setMount(browse.mountIdx, { source: browse.path }); setBrowse(null); }}>
                Use this folder
              </button>
            </div>
            <div style={{ maxHeight: 340, overflowY: 'auto' }}>
              {browse.entries.filter((e) => e.isDir).map((e) => (
                <button key={e.name} className="nav-item" style={{ width: '100%' }}
                  onClick={() => openBrowser(browse.mountIdx, (browse.path === '/' ? '' : browse.path) + '/' + e.name)}>
                  <Folder size={13} /> {e.name}
                </button>
              ))}
              {browse.entries.filter((e) => e.isDir).length === 0 && <div className="faint" style={{ padding: 10 }}>no subfolders</div>}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
