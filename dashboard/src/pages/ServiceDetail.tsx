import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowLeft, Box, Cloud, Cpu, ExternalLink, Globe, HardDrive, History, KeyRound, Play, Plus,
  RotateCw, Save, ScrollText, Square, Trash2, TriangleAlert, X, Folder, FolderOpen, Rocket,
} from 'lucide-react';
import { api } from '../api';
import type { Service, ServiceUpdate, Node, Mount, EnvVar, Deployment, Domain, DirEntry, PlacementScore } from '../api';
import { StatusBadge } from '../App';

type DetailTab = 'overview' | 'environment' | 'storage' | 'network' | 'resources' | 'deployments' | 'logs' | 'danger';

const TABS: { key: DetailTab; label: string; icon: React.ReactNode }[] = [
  { key: 'overview', label: 'Overview', icon: <Box size={14} /> },
  { key: 'environment', label: 'Environment', icon: <KeyRound size={14} /> },
  { key: 'storage', label: 'Storage & Mounts', icon: <HardDrive size={14} /> },
  { key: 'network', label: 'Network', icon: <Globe size={14} /> },
  { key: 'resources', label: 'Resources & Node', icon: <Cpu size={14} /> },
  { key: 'deployments', label: 'Deployments', icon: <History size={14} /> },
  { key: 'logs', label: 'Logs', icon: <ScrollText size={14} /> },
  { key: 'danger', label: 'Danger Zone', icon: <TriangleAlert size={14} /> },
];

// Draft holds the editable copy; saved via PATCH with redeploy flag.
interface Draft {
  image: string; command: string; port: string; ports: string;
  env: EnvVar[]; mounts: Mount[];
  memLimitMb: string; cpuLimit: string; restartPolicy: string; gpu: boolean; nodeId: string;
}

function toDraft(s: Service): Draft {
  return {
    image: s.image ?? '', command: s.command ?? '',
    port: s.port ? String(s.port) : '', ports: (s.ports ?? []).join(', '),
    env: (s.env ?? []).map((e) => ({ ...e })),
    mounts: (s.mounts ?? []).map((m) => ({ ...m })),
    memLimitMb: s.memLimitMb ? String(s.memLimitMb) : '',
    cpuLimit: s.cpuLimit ? String(s.cpuLimit) : '',
    restartPolicy: s.restartPolicy || 'unless-stopped',
    gpu: !!s.gpu, nodeId: s.nodeId,
  };
}

export function ServiceDetail({ serviceId, nodes, onBack, onChanged }: {
  serviceId: string; nodes: Node[]; onBack: () => void; onChanged: () => void;
}) {
  const [svc, setSvc] = useState<Service | null>(null);
  const [tab, setTab] = useState<DetailTab>('overview');
  const [draft, setDraft] = useState<Draft | null>(null);
  const [baseline, setBaseline] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [logs, setLogs] = useState('');
  const [logsLoading, setLogsLoading] = useState(false);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [openDeploy, setOpenDeploy] = useState('');
  const [domains, setDomains] = useState<Domain[]>([]);
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [browse, setBrowse] = useState<{ mountIdx: number; path: string; entries: DirEntry[] } | null>(null);
  const [tunnelBusy, setTunnelBusy] = useState(false);

  const load = useCallback(() => {
    api.service(serviceId).then((s) => {
      setSvc(s);
      const d = toDraft(s);
      setDraft(d);
      setBaseline(JSON.stringify(d));
    }).catch((e) => setErr(String(e)));
    api.domains().then((d) => setDomains(d ?? [])).catch(() => {});
    api.placement({}).then(setScores).catch(() => {});
  }, [serviceId]);
  useEffect(load, [load]);

  // Refresh runtime status (not the draft) while viewing.
  useEffect(() => {
    const t = setInterval(() => {
      api.service(serviceId).then((s) => setSvc((prev) => prev ? { ...s, mounts: prev.mounts } : s)).catch(() => {});
    }, 4000);
    return () => clearInterval(t);
  }, [serviceId]);

  const dirty = useMemo(() => draft && JSON.stringify(draft) !== baseline, [draft, baseline]);
  const node = nodes.find((n) => n.id === svc?.nodeId);
  const drives = (nodes.find((n) => n.id === draft?.nodeId)?.hardware.drives ?? []).filter((d) => d.mountPoint);

  const save = async (redeploy: boolean) => {
    if (!draft || !svc) return;
    setBusy(true); setErr('');
    try {
      const upd: ServiceUpdate = {
        image: draft.image, command: draft.command,
        port: parseInt(draft.port, 10) || 0,
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
      onChanged();
    } catch (e) { setErr(String(e)); }
    setBusy(false);
  };

  const act = async (action: string) => {
    if (!svc) return;
    setBusy(true);
    try {
      if (action === 'delete') { await api.deleteService(svc.id); onChanged(); onBack(); return; }
      await api.serviceAction(svc.id, action);
      load(); onChanged();
    } catch (e) { setErr(String(e)); }
    setBusy(false);
  };

  const fetchLogs = async () => {
    if (!svc) return;
    setLogsLoading(true);
    try { setLogs((await api.serviceLogs(svc.id)).logs || '(no output)'); }
    catch (e) { setLogs(String(e)); }
    setLogsLoading(false);
  };

  const fetchDeployments = useCallback(() => {
    api.serviceDeployments(serviceId).then((d) => setDeployments(d ?? [])).catch(() => {});
  }, [serviceId]);

  useEffect(() => { if (tab === 'logs') fetchLogs(); if (tab === 'deployments') fetchDeployments(); }, [tab]); // eslint-disable-line

  const openBrowser = async (mountIdx: number, start?: string) => {
    if (!draft) return;
    const path = start ?? '/';
    try {
      const r = await api.nodeFS(draft.nodeId, path);
      setBrowse({ mountIdx, path: r.path, entries: r.entries ?? [] });
    } catch (e) { setErr(String(e)); }
  };

  if (!svc || !draft) return <div className="empty">{err || 'loading…'}</div>;

  const setMount = (i: number, patch: Partial<Mount>) =>
    setDraft({ ...draft, mounts: draft.mounts.map((m, j) => (j === i ? { ...m, ...patch } : m)) });

  return (
    <>
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="row">
          <button className="btn sm" onClick={onBack}><ArrowLeft size={13} /></button>
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
      <div className="page-sub">
        {svc.image || svc.gitRepo || svc.kind} · running on <strong>{node?.name ?? svc.nodeId}</strong>
        {svc.containerId && <> · <span className="mono">{svc.containerId}</span></>}
      </div>

      {dirty && (
        <div className="card row spread" style={{ borderColor: 'var(--yellow)', marginBottom: 14, padding: '10px 16px' }}>
          <span className="dim">Unsaved configuration changes — the container keeps running until you apply.</span>
          <div className="row">
            <button className="btn sm" disabled={busy} onClick={() => { const d = JSON.parse(baseline); setDraft(d); }}>Discard</button>
            <button className="btn sm" disabled={busy} onClick={() => save(false)}><Save size={12} /> Save only</button>
            <button className="btn sm primary" disabled={busy} onClick={() => save(true)}><Rocket size={12} /> Save & Redeploy</button>
          </div>
        </div>
      )}
      {err && <div className="badge red" style={{ marginBottom: 12 }}>{err}</div>}

      <div style={{ display: 'grid', gridTemplateColumns: '190px 1fr', gap: 18, alignItems: 'start' }}>
        <div className="card" style={{ padding: 8 }}>
          {TABS.map((t) => (
            <button key={t.key} className={`nav-item ${tab === t.key ? 'active' : ''}`} onClick={() => setTab(t.key)}>
              {t.icon} {t.label}
            </button>
          ))}
        </div>

        <div>
          {tab === 'overview' && (
            <>
              <div className="kpi-row">
                <div className="card"><div className="kpi-label">CPU</div><div className="kpi-value">{svc.cpuPct.toFixed(1)}<span className="unit">%</span></div></div>
                <div className="card"><div className="kpi-label">Memory</div><div className="kpi-value">{svc.memUsedMb}<span className="unit">MB{svc.memLimitMb ? ` / ${svc.memLimitMb}` : ''}</span></div></div>
                <div className="card"><div className="kpi-label">Port</div><div className="kpi-value">{svc.port || '—'}</div></div>
                <div className="card"><div className="kpi-label">Mounts</div><div className="kpi-value">{(svc.mounts ?? []).length + (svc.volumes ?? []).length}</div></div>
              </div>
              <div className="card">
                <div className="section-title" style={{ marginTop: 0 }}>Source</div>
                <div className="grid-2">
                  <div className="field"><label>Image</label>
                    <input className="input mono" value={draft.image} onChange={(e) => setDraft({ ...draft, image: e.target.value })} placeholder={svc.gitRepo ? 'built from git' : 'image:tag'} /></div>
                  <div className="field"><label>Command override</label>
                    <input className="input mono" value={draft.command} onChange={(e) => setDraft({ ...draft, command: e.target.value })} placeholder="(image default)" /></div>
                </div>
                {svc.gitRepo && <div className="dim mono">{svc.gitRepo} @ {svc.gitBranch || 'main'}</div>}
              </div>
            </>
          )}

          {tab === 'environment' && (
            <div className="card">
              <div className="section-title" style={{ marginTop: 0 }}>Environment Variables</div>
              {draft.env.map((v, i) => (
                <div className="row" key={i} style={{ marginBottom: 8 }}>
                  <input className="input mono" placeholder="KEY" value={v.key}
                    onChange={(e) => setDraft({ ...draft, env: draft.env.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)) })} />
                  <input className="input mono" placeholder="value" type={v.secret ? 'password' : 'text'} value={v.value}
                    onChange={(e) => setDraft({ ...draft, env: draft.env.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)) })} />
                  <button className="btn sm" title={v.secret ? 'Reveal' : 'Mask as secret'}
                    onClick={() => setDraft({ ...draft, env: draft.env.map((x, j) => (j === i ? { ...x, secret: !x.secret } : x)) })}>
                    <KeyRound size={12} />
                  </button>
                  <button className="btn sm danger" onClick={() => setDraft({ ...draft, env: draft.env.filter((_, j) => j !== i) })}><Trash2 size={12} /></button>
                </div>
              ))}
              <button className="btn sm" onClick={() => setDraft({ ...draft, env: [...draft.env, { key: '', value: '', secret: false }] })}>
                <Plus size={12} /> Add variable
              </button>
            </div>
          )}

          {tab === 'storage' && (
            <div className="card">
              <div className="section-title" style={{ marginTop: 0 }}>Storage & Mounts</div>
              <div className="dim" style={{ marginBottom: 14 }}>
                Bind mounts attach real host folders (e.g. your media library). Managed volumes are created by
                RackOps — optionally placed on a specific drive. Changes apply on Save & Redeploy.
              </div>
              {draft.mounts.map((m, i) => (
                <div className="card" key={i} style={{ marginBottom: 10, background: 'var(--bg-2)' }}>
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
                    <label className="row dim" style={{ cursor: 'pointer', whiteSpace: 'nowrap' }}>
                      <input type="checkbox" checked={m.readOnly} onChange={(e) => setMount(i, { readOnly: e.target.checked })} /> read-only
                    </label>
                    <button className="btn sm danger" onClick={() => setDraft({ ...draft, mounts: draft.mounts.filter((_, j) => j !== i) })}><Trash2 size={12} /></button>
                  </div>
                </div>
              ))}
              <button className="btn sm" onClick={() => setDraft({ ...draft, mounts: [...draft.mounts, { type: 'bind', source: '', target: '', readOnly: false }] })}>
                <Plus size={12} /> Add mount
              </button>
              {(svc.volumes ?? []).length > 0 && (
                <div className="faint" style={{ marginTop: 12 }}>
                  Legacy template volumes (auto-managed): {(svc.volumes ?? []).join(', ')}
                </div>
              )}
            </div>
          )}

          {tab === 'network' && (
            <div className="card">
              <div className="section-title" style={{ marginTop: 0 }}>Ports</div>
              <div className="grid-2">
                <div className="field"><label>Primary port (published on the node)</label>
                  <input className="input mono" value={draft.port} onChange={(e) => setDraft({ ...draft, port: e.target.value.replace(/\D/g, '') })} /></div>
                <div className="field"><label>Extra ports (comma-separated)</label>
                  <input className="input mono" placeholder="9000, 5432" value={draft.ports} onChange={(e) => setDraft({ ...draft, ports: e.target.value })} /></div>
              </div>
              <div className="section-title">Public Domain</div>
              <div className="row">
                <select className="input" style={{ width: 300 }}
                  value={domains.find((d) => d.wildcard && (svc.domain || '').endsWith('.' + d.name.slice(2)))?.name ?? (svc.domain || '')}
                  onChange={async (e) => {
                    let host = e.target.value;
                    if (host.startsWith('*.')) {
                      // Wildcard picked: this service gets <name>.<base>.
                      const label = svc.name.toLowerCase().replace(/[ ._]/g, '-').replace(/[^a-z0-9-]/g, '') || 'app';
                      host = `${label}.${host.slice(2)}`;
                    }
                    await api.setServiceDomain(svc.id, host, !host ? 'none' : 'static');
                    load(); onChanged();
                  }}>
                  <option value="">— no public route —</option>
                  {svc.domain && !domains.some((d) => d.name === svc.domain || (d.wildcard && svc.domain.endsWith('.' + d.name.slice(2)))) &&
                    <option value={svc.domain}>{svc.domain} (instant)</option>}
                  {domains.map((d) => (
                    <option key={d.id} value={d.name}>
                      {d.name}{d.status !== 'verified' ? ' ⚠ unverified' : ''}
                    </option>
                  ))}
                </select>
                <span className="faint">applies immediately via Traefik (verify DNS on the Network page)</span>
              </div>
              {svc.domain && <div className="mono dim" style={{ marginTop: 6 }}>current: {svc.domain}</div>}

              <div className="section-title">Quick Tunnel <span className="faint">(Cloudflare, testing only)</span></div>
              {svc.quickTunnelUrl ? (
                <div className="row">
                  <a className="mono" href={svc.quickTunnelUrl} target="_blank" rel="noreferrer" style={{ color: 'var(--accent)' }}>
                    {svc.quickTunnelUrl} <ExternalLink size={11} />
                  </a>
                  <span className="badge yellow"><Cloud size={10} /> URL changes on restart</span>
                  <button className="btn sm danger" disabled={tunnelBusy}
                    onClick={async () => { setTunnelBusy(true); try { await api.stopQuickTunnel(svc.id); load(); } catch (e) { setErr(String(e)); } setTunnelBusy(false); }}>
                    Stop tunnel
                  </button>
                </div>
              ) : (
                <div className="row">
                  <button className="btn sm" disabled={tunnelBusy || !svc.port}
                    onClick={async () => {
                      setTunnelBusy(true); setErr('');
                      try { await api.startQuickTunnel(svc.id); } catch (e) { setErr(String(e)); }
                      setTimeout(() => setTunnelBusy(false), 20000); // URL lands via the 4s status refresh
                    }}>
                    <Cloud size={12} /> {tunnelBusy ? 'Starting… (first run pulls cloudflared)' : 'Get free public URL'}
                  </button>
                  <span className="faint">no account, no DNS, works behind CGNAT/hotspots — for demos & testing</span>
                </div>
              )}
            </div>
          )}

          {tab === 'resources' && (
            <>
              <div className="card" style={{ marginBottom: 14 }}>
                <div className="section-title" style={{ marginTop: 0 }}>Limits & Policy</div>
                <div className="grid-3">
                  <div className="field"><label>Memory limit (MB, 0 = none)</label>
                    <input className="input mono" value={draft.memLimitMb} onChange={(e) => setDraft({ ...draft, memLimitMb: e.target.value.replace(/\D/g, '') })} /></div>
                  <div className="field"><label>CPU limit (cores, 0 = none)</label>
                    <input className="input mono" placeholder="1.5" value={draft.cpuLimit} onChange={(e) => setDraft({ ...draft, cpuLimit: e.target.value.replace(/[^0-9.]/g, '') })} /></div>
                  <div className="field"><label>Restart policy</label>
                    <select className="input" value={draft.restartPolicy} onChange={(e) => setDraft({ ...draft, restartPolicy: e.target.value })}>
                      <option value="unless-stopped">unless-stopped</option>
                      <option value="always">always</option>
                      <option value="on-failure">on-failure</option>
                      <option value="no">no</option>
                    </select></div>
                </div>
                <label className="row dim" style={{ cursor: 'pointer' }}>
                  <input type="checkbox" checked={draft.gpu} onChange={(e) => setDraft({ ...draft, gpu: e.target.checked })} /> GPU access (--gpus all)
                </label>
              </div>
              <div className="card">
                <div className="section-title" style={{ marginTop: 0 }}>Node Placement</div>
                <div className="field"><label>Run on node (changing = migrate on Save & Redeploy; managed volumes do not move automatically)</label>
                  <select className="input" value={draft.nodeId} onChange={(e) => setDraft({ ...draft, nodeId: e.target.value })}>
                    {nodes.map((n) => {
                      const sc = scores.find((s) => s.nodeId === n.id);
                      return <option key={n.id} value={n.id}>{n.name} {sc ? `(score ${sc.score})` : ''} {n.status !== 'online' ? '· OFFLINE' : ''}</option>;
                    })}
                  </select></div>
                {scores.find((s) => s.viable) && (
                  <div className="faint">Scheduler recommendation: {nodes.find((n) => n.id === scores.find((s) => s.viable)!.nodeId)?.name} — {scores.find((s) => s.viable)!.reasons.join(' · ')}</div>
                )}
              </div>
            </>
          )}

          {tab === 'deployments' && (
            <div className="card">
              <div className="row spread" style={{ marginBottom: 10 }}>
                <div className="section-title" style={{ margin: 0 }}>Deployment History</div>
                <button className="btn sm" onClick={fetchDeployments}><RotateCw size={12} /></button>
              </div>
              {deployments.map((d) => (
                <div key={d.id} style={{ borderTop: '1px solid var(--border)', padding: '8px 0' }}>
                  <div className="row spread" style={{ cursor: 'pointer' }} onClick={() => setOpenDeploy(openDeploy === d.id ? '' : d.id)}>
                    <div className="row">
                      <span className="mono">v{d.version}</span>
                      <StatusBadge status={d.status} />
                      <span className="tag">{d.trigger}</span>
                    </div>
                    <span className="faint">{new Date(d.startedAt).toLocaleString()} · {(d.durationMs / 1000).toFixed(1)}s</span>
                  </div>
                  {openDeploy === d.id && (
                    <div className="terminal" style={{ marginTop: 8, whiteSpace: 'pre-wrap' }}>{d.log || '(no build log)'}</div>
                  )}
                </div>
              ))}
              {deployments.length === 0 && <div className="faint">No deployments recorded yet.</div>}
            </div>
          )}

          {tab === 'logs' && (
            <div className="card">
              <div className="row spread" style={{ marginBottom: 10 }}>
                <div className="section-title" style={{ margin: 0 }}>Runtime Logs</div>
                <div className="row">
                  <button className="btn sm" disabled={logsLoading} onClick={fetchLogs}><RotateCw size={12} /> Refresh</button>
                  <button className="btn sm danger" onClick={async () => { await api.clearServiceLogs(svc.id); fetchLogs(); }}>
                    <Trash2 size={12} /> Clear old logs
                  </button>
                </div>
              </div>
              <div className="terminal" style={{ maxHeight: 460, whiteSpace: 'pre-wrap' }}>
                {logsLoading ? 'fetching from agent…' : logs || 'no logs yet'}
              </div>
            </div>
          )}

          {tab === 'danger' && (
            <div className="card" style={{ borderColor: 'var(--red)' }}>
              <div className="section-title" style={{ marginTop: 0 }}><TriangleAlert size={14} /> Danger Zone</div>
              <div className="row spread" style={{ padding: '10px 0', borderTop: '1px solid var(--border)' }}>
                <div><strong>Stop service</strong><div className="faint">Container stops; data and config stay.</div></div>
                <button className="btn" disabled={busy || svc.status !== 'running'} onClick={() => act('stop')}><Square size={13} /> Stop</button>
              </div>
              <div className="row spread" style={{ padding: '10px 0', borderTop: '1px solid var(--border)' }}>
                <div><strong>Remove service</strong><div className="faint">Deletes the container. Managed volume data remains on disk under rackops-volumes/.</div></div>
                <button className="btn danger" disabled={busy} onClick={() => { if (confirm(`Remove ${svc.name}? The container will be deleted.`)) act('delete'); }}>
                  <Trash2 size={13} /> Remove
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {browse && (
        <div className="modal-overlay" onClick={() => setBrowse(null)}>
          <div className="modal" style={{ width: 540 }} onClick={(e) => e.stopPropagation()}>
            <div className="row spread" style={{ marginBottom: 12 }}>
              <h2 style={{ margin: 0 }}><FolderOpen size={15} style={{ verticalAlign: -2 }} /> {browse.path}</h2>
              <button className="btn sm" onClick={() => setBrowse(null)}><X size={13} /></button>
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
    </>
  );
}
