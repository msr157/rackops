import { useEffect, useMemo, useState } from 'react';
import { Globe, Network as NetworkIcon, Plus, Trash2, Shield, Waypoints, Boxes, Zap, ExternalLink, AlertTriangle, CheckCircle2, Loader, Copy, RefreshCw, ChevronDown, ChevronRight, Lock, Rocket, X, Cloud } from 'lucide-react';
import { api } from '../api';
import type { Node, Service, Domain, IngressConfig, MeshStatus, ClusterStatus, NetworkDiagnosis } from '../api';

// dnsLabel mirrors the backend's sanitizer: DNS-safe service names.
function dnsLabel(s: string): string {
  const out = s.toLowerCase().replace(/[ ._]/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '');
  return out || 'app';
}

// recordHost: what goes in the registrar's "Host/Name" field.
function recordHost(domain: string): string {
  const wildcard = domain.startsWith('*.');
  const base = wildcard ? domain.slice(2) : domain;
  const parts = base.split('.');
  if (parts.length <= 2) return wildcard ? '*' : '@';
  const host = parts.slice(0, -2).join('.');
  return wildcard ? `*.${host}` : host;
}

function CopyBtn({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button className="btn sm" title="Copy" onClick={() => {
      navigator.clipboard.writeText(text);
      setDone(true); setTimeout(() => setDone(false), 1200);
    }}>{done ? '✓' : <Copy size={11} />}</button>
  );
}

function DomainBadge({ d }: { d: Domain }) {
  switch (d.status) {
    case 'verified':
      return <span className="badge green"><CheckCircle2 size={11} /> verified</span>;
    case 'misconfigured':
      return <span className="badge red"><AlertTriangle size={11} /> misconfigured</span>;
    default:
      return <span className="badge yellow"><Loader size={11} /> pending DNS</span>;
  }
}

export function NetworkPage({ nodes, services, onChanged }: {
  nodes: Node[]; services: Service[]; onChanged: () => void;
}) {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [newDomain, setNewDomain] = useState('');
  const [expanded, setExpanded] = useState<string>(''); // domain id with open setup card
  const [checking, setChecking] = useState<string>('');
  const [ingress, setIngressCfg] = useState<IngressConfig | null>(null);
  const [meshStatus, setMeshStatus] = useState<MeshStatus | null>(null);
  const [cluster, setCluster] = useState<ClusterStatus | null>(null);
  const [diagnosis, setDiagnosis] = useState<NetworkDiagnosis | null>(null);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [acmeEmail, setAcmeEmail] = useState('');
  const [ingressNode, setIngressNode] = useState('');
  const [httpPort, setHttpPort] = useState('80');
  const [httpsPort, setHttpsPort] = useState('443');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [subEdit, setSubEdit] = useState<{ svcId: string; sub: string; base: string } | null>(null);
  const [tunnelStarting, setTunnelStarting] = useState<Record<string, boolean>>({});

  const load = () => {
    api.domains().then((d) => setDomains(d ?? [])).catch(() => {});
    api.ingress().then((i) => {
      setIngressCfg(i);
      if (i.nodeId) setIngressNode(i.nodeId);
      if (i.httpPort) setHttpPort(String(i.httpPort));
      if (i.httpsPort) setHttpsPort(String(i.httpsPort));
      setAcmeEmail(i.acmeEmail ?? '');
    }).catch(() => {});
    api.mesh().then(setMeshStatus).catch(() => {});
    api.cluster().then(setCluster).catch(() => {});
    api.networkDiagnosis().then(setDiagnosis).catch(() => {});
  };
  useEffect(load, []);
  // Poll while anything is settling: edge starting, or domains not yet green.
  const settling = (ingress?.enabled && ingress.status !== 'running') || domains.some((d) => d.status !== 'verified');
  useEffect(() => {
    if (!settling) return;
    const t = setInterval(() => {
      api.ingress().then(setIngressCfg).catch(() => {});
      api.domains().then((d) => setDomains(d ?? [])).catch(() => {});
    }, 5000);
    return () => clearInterval(t);
  }, [settling]);

  const addDomain = async () => {
    if (!newDomain.trim()) return;
    setErr('');
    try {
      const d = await api.createDomain(newDomain.trim());
      setNewDomain('');
      setExpanded(d.id); // open the setup instructions right away
      load();
    } catch (e) { setErr(String(e)); }
  };

  const verify = async (id: string) => {
    setChecking(id);
    try {
      const d = await api.verifyDomain(id);
      setDomains((ds) => ds.map((x) => x.id === d.id ? d : x));
    } catch (e) { setErr(String(e)); }
    setChecking('');
  };

  const applyIngress = async (enabled: boolean) => {
    setBusy(true); setErr('');
    try {
      await api.setIngress({
        enabled,
        nodeId: ingressNode || nodes[0]?.id || '',
        httpPort: parseInt(httpPort, 10) || 80,
        httpsPort: parseInt(httpsPort, 10) || 443,
        acmeEmail: acmeEmail.trim(),
      });
      load();
    } catch (e) { setErr(String(e)); }
    setBusy(false);
  };

  const setRoute = async (svc: Service, domain: string) => {
    setErr('');
    try {
      const mode = !domain ? 'none' : (svc.nodeId === ingress?.nodeId ? 'static' : 'relay');
      await api.setServiceDomain(svc.id, domain, mode);
      onChanged();
    } catch (e) { setErr(String(e)); }
  };

  const expose = async (svc: Service) => {
    setErr('');
    try {
      await api.exposeService(svc.id);
      onChanged();
      load();
    } catch (e) { setErr(String(e)); }
  };

  const startTunnel = async (svc: Service) => {
    setErr('');
    setTunnelStarting((t) => ({ ...t, [svc.id]: true }));
    try {
      await api.startQuickTunnel(svc.id);
      // The URL arrives async (first run pulls the cloudflared image);
      // poll the service list until it lands or we give up.
      const until = Date.now() + 75_000;
      const tick = async () => {
        onChanged();
        const fresh = await api.service(svc.id).catch(() => null);
        if (fresh?.quickTunnelUrl || Date.now() > until) {
          setTunnelStarting((t) => ({ ...t, [svc.id]: false }));
          onChanged();
        } else setTimeout(tick, 3000);
      };
      setTimeout(tick, 3000);
    } catch (e) {
      setErr(String(e));
      setTunnelStarting((t) => ({ ...t, [svc.id]: false }));
    }
  };

  const stopTunnel = async (svc: Service) => {
    setErr('');
    try { await api.stopQuickTunnel(svc.id); onChanged(); } catch (e) { setErr(String(e)); }
  };

  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;
  const routable = services.filter((s) => s.port > 0 && s.kind !== 'compose');
  const tlsOn = !!ingress?.acmeEmail;

  // Find the Domain entry backing a service's hostname (exact or wildcard).
  const domainFor = (host: string): Domain | undefined => {
    if (!host) return undefined;
    const exact = domains.find((d) => !d.wildcard && d.name === host);
    if (exact) return exact;
    return domains.find((d) => d.wildcard && host.endsWith('.' + d.name.slice(2)));
  };

  const edgeReady = ingress?.enabled && ingress.status === 'running';
  const lanOnly = useMemo(() => diagnosis?.lanOnly ?? false, [diagnosis]);

  const StatusBadge = () => {
    if (!ingress?.enabled) return <span className="badge gray" style={{ marginLeft: 'auto' }}>off</span>;
    switch (ingress.status) {
      case 'running':
        return <span className="badge green" style={{ marginLeft: 'auto' }}><CheckCircle2 size={11} /> running on {nodeName(ingress.nodeId)}</span>;
      case 'failed':
        return <span className="badge red" style={{ marginLeft: 'auto' }}><AlertTriangle size={11} /> failed</span>;
      default:
        return <span className="badge yellow" style={{ marginLeft: 'auto' }}><Loader size={11} /> starting…</span>;
    }
  };

  return (
    <>
      <div className="page-title">Network & Ingress</div>
      <div className="page-sub">Secure mesh, public domains, and per-app routing</div>

      {/* LAN-only banner → wizard */}
      {lanOnly && (
        <div className="card row spread" style={{ borderColor: 'var(--yellow)', marginBottom: 16 }}>
          <div>
            <strong><AlertTriangle size={13} style={{ verticalAlign: -2, color: 'var(--yellow)' }} /> Your apps are LAN-only right now</strong>
            <div className="dim" style={{ marginTop: 4 }}>
              No node in this fleet holds a public IP — domains and instant URLs will only work inside your network.
            </div>
          </div>
          <button className="btn primary" onClick={() => setWizardOpen(true)}><Rocket size={13} /> Get public access</button>
        </div>
      )}

      <div className="grid-2">
        {/* Mesh status */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Waypoints size={15} /> WireGuard Mesh
            {meshStatus?.enabled
              ? <span className="badge green" style={{ marginLeft: 'auto' }}>active</span>
              : <span className="badge gray" style={{ marginLeft: 'auto' }}>disabled</span>}
          </div>
          {meshStatus?.enabled ? (
            <>
              <div className="dim" style={{ marginBottom: 10, lineHeight: 1.7 }}>
                Hub <span className="mono">{meshStatus.hubIp}</span> on UDP <span className="mono">:{meshStatus.udpPort}</span>.
                Agents dial out — works behind CGNAT, no port forwarding.
              </div>
              {(meshStatus.peers ?? []).map((p) => (
                <div className="row spread" key={p.nodeId} style={{ padding: '6px 0', borderTop: '1px solid var(--border)' }}>
                  <span>{nodeName(p.nodeId)}</span>
                  <span className="mono dim">{p.meshIp}</span>
                </div>
              ))}
              {(meshStatus.peers ?? []).length === 0 && <div className="faint">No nodes have joined the mesh yet.</div>}
            </>
          ) : <div className="faint">Mesh is disabled (start the server with a mesh port).</div>}
        </div>

        {/* Ingress config */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Shield size={15} /> Edge (Traefik)
            <StatusBadge />
          </div>
          {ingress?.status === 'failed' && ingress.statusMsg && (
            <div className="badge red" style={{ display: 'block', whiteSpace: 'pre-wrap', padding: 10, marginBottom: 12, lineHeight: 1.5 }}>
              <AlertTriangle size={12} /> {ingress.statusMsg}
            </div>
          )}
          <div className="field">
            <label>Ingress node (needs public IP or LAN reachability)</label>
            <select className="input" value={ingressNode} onChange={(e) => setIngressNode(e.target.value)}>
              <option value="">— select —</option>
              {nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.publicIp || n.ip})</option>)}
            </select>
          </div>
          <div className="grid-2">
            <div className="field">
              <label>HTTP port</label>
              <input className="input" value={httpPort} onChange={(e) => setHttpPort(e.target.value.replace(/\D/g, ''))} />
            </div>
            <div className="field">
              <label>HTTPS port</label>
              <input className="input" value={httpsPort} onChange={(e) => setHttpsPort(e.target.value.replace(/\D/g, ''))} />
            </div>
          </div>
          <div className="field">
            <label>Let's Encrypt email {tlsOn ? '' : '(optional — enables automatic HTTPS)'}</label>
            <input className="input" placeholder="you@example.com" value={acmeEmail} onChange={(e) => setAcmeEmail(e.target.value)} />
            {tlsOn && <div className="faint" style={{ marginTop: 4 }}>
              HTTPS on: certs auto-issue via HTTP-01. Port {httpPort} must be reachable from the internet; HTTP redirects to HTTPS.
            </div>}
          </div>
          <div className="row">
            <button className="btn primary" disabled={busy || !ingressNode} onClick={() => applyIngress(true)}>
              {ingress?.enabled ? 'Update edge' : 'Enable edge'}
            </button>
            {ingress?.enabled && (
              <button className="btn danger" disabled={busy} onClick={() => applyIngress(false)}>Disable</button>
            )}
          </div>
        </div>
      </div>

      {/* HA cluster */}
      {cluster?.enabled && (
        <>
          <div className="section-title"><Boxes size={15} /> Control Plane Cluster (HA)</div>
          <div className="card">
            <div className="dim" style={{ marginBottom: 10 }}>
              Raft-replicated control plane — state survives node failure, leader re-elected automatically.
            </div>
            {(cluster.peers ?? []).map((p) => (
              <div className="row spread" key={p.id} style={{ padding: '6px 0', borderTop: '1px solid var(--border)' }}>
                <span className="mono">{p.id} <span className="faint">{p.addr}</span></span>
                <span className={`badge ${p.role === 'leader' ? 'green' : 'gray'}`}>{p.role}</span>
              </div>
            ))}
          </div>
        </>
      )}

      {/* Domains */}
      <div className="section-title"><Globe size={15} /> Domains</div>
      <div className="card">
        <div className="row" style={{ marginBottom: 12 }}>
          <input className="input" placeholder="app.example.com — or *.example.com for every app at once" value={newDomain}
            onChange={(e) => setNewDomain(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && addDomain()} />
          <button className="btn primary" onClick={addDomain}><Plus size={13} /> Add</button>
        </div>

        {domains.map((d) => {
          const open = expanded === d.id;
          return (
            <div key={d.id} style={{ borderTop: '1px solid var(--border)' }}>
              <div className="row spread" style={{ padding: '8px 0', cursor: 'pointer' }} onClick={() => setExpanded(open ? '' : d.id)}>
                <span className="row" style={{ gap: 8 }}>
                  {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                  <span className="mono">{d.name}</span>
                  {d.wildcard && <span className="badge purple">wildcard</span>}
                  <DomainBadge d={d} />
                  {d.certOk && <span className="badge green"><Lock size={10} /> TLS</span>}
                </span>
                <span className="row" onClick={(e) => e.stopPropagation()}>
                  <button className="btn sm" disabled={checking === d.id} onClick={() => verify(d.id)}>
                    <RefreshCw size={11} className={checking === d.id ? 'spin' : ''} /> {checking === d.id ? 'Checking…' : 'Verify'}
                  </button>
                  <button className="btn sm danger" onClick={async () => { await api.deleteDomain(d.id); load(); onChanged(); }}>
                    <Trash2 size={12} />
                  </button>
                </span>
              </div>

              {open && (
                <div className="card" style={{ margin: '0 0 12px 21px', background: 'var(--bg-2)' }}>
                  {/* Setup instructions */}
                  <div className="dim" style={{ marginBottom: 8 }}>
                    Create this record at your registrar (GoDaddy, Namecheap, Cloudflare DNS, …):
                  </div>
                  <div className="row" style={{ gap: 8, alignItems: 'stretch', flexWrap: 'wrap', marginBottom: 10 }}>
                    <div className="card" style={{ padding: '6px 12px' }}>
                      <div className="faint">Type</div><strong className="mono">A</strong>
                    </div>
                    <div className="card" style={{ padding: '6px 12px' }}>
                      <div className="faint">Host / Name</div>
                      <span className="row" style={{ gap: 6 }}><strong className="mono">{recordHost(d.name)}</strong><CopyBtn text={recordHost(d.name)} /></span>
                    </div>
                    <div className="card" style={{ padding: '6px 12px' }}>
                      <div className="faint">Value / Points to</div>
                      <span className="row" style={{ gap: 6 }}>
                        <strong className="mono">{d.expectedIp || '(enable the edge first)'}</strong>
                        {d.expectedIp && <CopyBtn text={d.expectedIp} />}
                      </span>
                    </div>
                    <div className="card" style={{ padding: '6px 12px' }}>
                      <div className="faint">TTL</div><strong className="mono">300</strong>
                    </div>
                  </div>

                  {/* Live check results */}
                  <div style={{ lineHeight: 2 }}>
                    <div>
                      {d.status === 'verified' ? '✅' : d.status === 'misconfigured' ? '❌' : '⏳'} <strong>DNS</strong>{' '}
                      <span className="dim">
                        {d.status === 'verified' && <>resolves to <span className="mono">{d.expectedIp}</span></>}
                        {d.status !== 'verified' && (d.message || 'not checked yet')}
                      </span>
                    </div>
                    <div>
                      {d.edgeOk ? '✅' : '⚠️'} <strong>Edge</strong>{' '}
                      <span className="dim">{d.edgeOk
                        ? 'our Traefik answers on that IP — traffic will route'
                        : edgeReady ? 'edge did not answer there (port forwarding / firewall / another server on the port)' : 'enable the edge to check'}</span>
                    </div>
                    {tlsOn && !d.wildcard && (
                      <div>
                        {d.certOk ? '✅' : '⏳'} <strong>Certificate</strong>{' '}
                        <span className="dim">{d.certOk
                          ? `issued by ${d.certIssuer}${d.certExpiry ? `, expires ${new Date(d.certExpiry).toLocaleDateString()}` : ''}`
                          : 'not issued yet — needs DNS verified + port 80 reachable from the internet, then route a service to it'}</span>
                      </div>
                    )}
                    {tlsOn && d.wildcard && (
                      <div className="faint"><Lock size={10} /> Certificates are issued per subdomain automatically when each app is routed.</div>
                    )}
                    {d.lastChecked && <div className="faint">last checked {new Date(d.lastChecked).toLocaleTimeString()} — rechecks every 30s until verified, then hourly</div>}
                  </div>
                </div>
              )}
            </div>
          );
        })}
        {domains.length === 0 && (
          <div className="faint">
            No custom domains yet. Add one (you'll get exact DNS instructions), or add a wildcard like <span className="mono">*.example.com</span> once
            to give every app its own <span className="mono">name.example.com</span>. For a zero-DNS link, use <b>Instant URL</b> below.
          </div>
        )}
      </div>

      {/* Per-app routing */}
      <div className="section-title"><NetworkIcon size={15} /> App Routing</div>
      {!ingress?.enabled && <div className="card faint" style={{ marginBottom: 12 }}>Enable the edge above to expose apps publicly.</div>}
      <div className="node-card">
        {routable.map((s) => {
          const backing = domainFor(s.domain);
          const live = s.domain && edgeReady && (!backing || backing.status === 'verified');
          const scheme = backing?.certOk || (tlsOn && backing?.status === 'verified') ? 'https' : 'http';
          const url = `${scheme}://${s.domain}`;
          const wildcardBase = backing?.wildcard ? backing.name.slice(2) : '';
          const editing = subEdit?.svcId === s.id;
          return (
            <div className="card row spread" key={s.id}>
              <div style={{ minWidth: 0 }}>
                <strong>{s.name}</strong>
                <span className="dim" style={{ marginLeft: 10 }}>
                  {nodeName(s.nodeId)} · :{s.port}
                  {s.nodeId !== ingress?.nodeId && ingress?.enabled && s.domain &&
                    <span className="badge cyan" style={{ marginLeft: 8 }}>via mesh relay</span>}
                  {backing && backing.status !== 'verified' && s.domain &&
                    <span className="badge yellow" style={{ marginLeft: 8 }}><AlertTriangle size={10} /> DNS not verified</span>}
                </span>
                {s.quickTunnelUrl && (
                  <div style={{ marginTop: 4 }} className="row">
                    <a className="mono" href={s.quickTunnelUrl} target="_blank" rel="noreferrer" style={{ color: 'var(--accent)' }}>
                      {s.quickTunnelUrl} <ExternalLink size={11} />
                    </a>
                    <span className="badge yellow"><Cloud size={10} /> quick tunnel — testing, URL changes on restart</span>
                    <button className="btn sm danger" title="Stop tunnel" onClick={() => stopTunnel(s)}><X size={11} /></button>
                  </div>
                )}
                {s.domain && (
                  <div style={{ marginTop: 4 }} className="row">
                    {live
                      ? <a className="mono" href={url} target="_blank" rel="noreferrer" style={{ color: 'var(--accent)' }}>{url} <ExternalLink size={11} /></a>
                      : <span className="mono faint">{url}</span>}
                    {wildcardBase && !editing && (
                      <button className="btn sm" title="Edit subdomain"
                        onClick={() => setSubEdit({ svcId: s.id, sub: s.domain.slice(0, s.domain.length - wildcardBase.length - 1), base: wildcardBase })}>✎</button>
                    )}
                    {editing && (
                      <span className="row" style={{ gap: 4 }}>
                        <input className="input" style={{ width: 140 }} value={subEdit.sub} autoFocus
                          onChange={(e) => setSubEdit({ ...subEdit, sub: dnsLabel(e.target.value) })}
                          onKeyDown={async (e) => {
                            if (e.key === 'Enter') { await setRoute(s, `${dnsLabel(subEdit.sub)}.${subEdit.base}`); setSubEdit(null); }
                            if (e.key === 'Escape') setSubEdit(null);
                          }} />
                        <span className="mono dim">.{subEdit.base}</span>
                        <button className="btn sm primary" onClick={async () => { await setRoute(s, `${dnsLabel(subEdit.sub)}.${subEdit.base}`); setSubEdit(null); }}>OK</button>
                      </span>
                    )}
                  </div>
                )}
              </div>
              <div className="row" style={{ flexShrink: 0 }}>
                {!s.quickTunnelUrl && (
                  <button className="btn sm" disabled={!!tunnelStarting[s.id]}
                    title="Free public URL via Cloudflare quick tunnel — no account, no DNS, testing only"
                    onClick={() => startTunnel(s)}>
                    <Cloud size={12} /> {tunnelStarting[s.id] ? 'Starting…' : 'CF tunnel'}
                  </button>
                )}
                <button className="btn sm" disabled={!ingress?.enabled} title="Assign an instant nip.io URL — no DNS setup"
                  onClick={() => expose(s)}><Zap size={12} /> Instant URL</button>
                <select className="input" style={{ width: 230 }}
                  value={backing ? backing.name : (s.domain || '')}
                  onChange={(e) => {
                    const v = e.target.value;
                    if (!v) return setRoute(s, '');
                    if (v.startsWith('*.')) return setRoute(s, `${dnsLabel(s.name)}.${v.slice(2)}`);
                    setRoute(s, v);
                  }}>
                  <option value="">— no public route —</option>
                  {s.domain && !backing && <option value={s.domain}>{s.domain} (instant)</option>}
                  {domains.map((d) => (
                    <option key={d.id} value={d.name}>
                      {d.wildcard ? `${dnsLabel(s.name)}.${d.name.slice(2)} (wildcard)` : d.name}{d.status !== 'verified' ? ' ⚠' : ''}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          );
        })}
        {routable.length === 0 && <div className="empty">No routable services (services need a port).</div>}
      </div>

      {err && <div className="badge red" style={{ marginTop: 12 }}>{err}</div>}

      {/* Public access wizard */}
      {wizardOpen && diagnosis && (
        <div className="modal-overlay" onClick={() => setWizardOpen(false)}>
          <div className="modal" style={{ width: 680, maxHeight: '85vh', overflowY: 'auto' }} onClick={(e) => e.stopPropagation()}>
            <div className="row spread" style={{ marginBottom: 14 }}>
              <h2 style={{ margin: 0 }}><Rocket size={15} style={{ verticalAlign: -2 }} /> Get Public Access</h2>
              <button className="btn sm" onClick={() => setWizardOpen(false)}><X size={13} /></button>
            </div>

            <div className="dim" style={{ lineHeight: 1.7, marginBottom: 14 }}>
              For the internet to reach your apps, traffic needs <b>one publicly reachable landing point</b>.
              From there, RackOps' WireGuard mesh tunnels to every machine automatically — even behind CGNAT.
            </div>

            <div className="section-title" style={{ marginTop: 0 }}>Your fleet right now</div>
            <div className="card" style={{ marginBottom: 14 }}>
              {(diagnosis.nodes ?? []).map((n) => (
                <div className="row spread" key={n.id} style={{ padding: '5px 0' }}>
                  <span>{n.name} <span className="mono faint">{n.lanIp}{n.publicIp ? ` → ${n.publicIp}` : ''}</span></span>
                  {n.reachability === 'direct' && <span className="badge green">publicly addressed</span>}
                  {n.reachability === 'nat' && <span className="badge yellow">behind NAT/router</span>}
                  {n.reachability === 'unknown' && <span className="badge gray">detecting…</span>}
                </div>
              ))}
            </div>

            <div className="section-title">Option A — port forwarding <span className="faint">(needs a real public IP from your ISP)</span></div>
            <div className="card dim" style={{ lineHeight: 1.8, marginBottom: 14 }}>
              1. In your router, forward TCP <b>80</b> and <b>443</b> to this machine's LAN IP.<br />
              2. Set the edge's HTTP/HTTPS ports to 80/443 above.<br />
              3. Domains then point at your public IP; Instant URLs use it automatically.<br />
              <span className="faint">⚠ Doesn't work behind CGNAT (if your router's WAN IP differs from the IP shown above, you're CGNAT'd — use Option B).</span>
            </div>

            <div className="section-title">Option B — VPS anchor <span className="faint">(works everywhere, ~$4/mo)</span></div>
            <div className="card dim" style={{ lineHeight: 1.8 }}>
              1. Get any small VPS with a public IP (Hetzner, DigitalOcean, Oracle free tier…).<br />
              2. Run the control plane there:
              <div className="row" style={{ margin: '6px 0' }}>
                <code className="code-block" style={{ flex: 1 }}>rackops server --listen :8080</code>
                <CopyBtn text="rackops server --listen :8080" />
              </div>
              3. Join this machine to it (dials out — no port forwarding needed):
              <div className="row" style={{ margin: '6px 0' }}>
                <code className="code-block" style={{ flex: 1 }}>{diagnosis.joinCommand}</code>
                <CopyBtn text={diagnosis.joinCommand} />
              </div>
              4. On the VPS dashboard: enable the <b>edge on the VPS node</b> (ports 80/443).<br />
              5. Your apps stay on this machine — traffic flows VPS → encrypted mesh → here, automatically.
            </div>

            <div className="section-title">Option C — quick tunnel <span className="faint">(zero setup, testing only)</span></div>
            <div className="card dim" style={{ lineHeight: 1.8 }}>
              Hit <b><Cloud size={11} /> CF tunnel</b> on any app in the routing table — you get a free public
              <span className="mono"> trycloudflare.com</span> URL in seconds, even behind CGNAT or a mobile hotspot.
              No account, no DNS. <span className="faint">⚠ URL changes on every restart and rides Cloudflare's free tier — great for demos/testing, not production.</span>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
