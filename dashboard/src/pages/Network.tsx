import { useEffect, useState } from 'react';
import { Globe, Network as NetworkIcon, Plus, Trash2, Shield, Waypoints, Boxes, Zap, ExternalLink, AlertTriangle, CheckCircle2, Loader } from 'lucide-react';
import { api } from '../api';
import type { Node, Service, Domain, IngressConfig, MeshStatus, ClusterStatus } from '../api';

export function NetworkPage({ nodes, services, onChanged }: {
  nodes: Node[]; services: Service[]; onChanged: () => void;
}) {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [newDomain, setNewDomain] = useState('');
  const [ingress, setIngressCfg] = useState<IngressConfig | null>(null);
  const [meshStatus, setMeshStatus] = useState<MeshStatus | null>(null);
  const [cluster, setCluster] = useState<ClusterStatus | null>(null);
  const [acmeEmail, setAcmeEmail] = useState('');
  const [ingressNode, setIngressNode] = useState('');
  const [httpPort, setHttpPort] = useState('80');
  const [httpsPort, setHttpsPort] = useState('443');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

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
  };
  useEffect(load, []);
  // Poll while the edge is settling so status/errors surface without a refresh.
  useEffect(() => {
    if (ingress?.enabled && ingress.status !== 'running') {
      const t = setInterval(() => api.ingress().then(setIngressCfg).catch(() => {}), 3000);
      return () => clearInterval(t);
    }
  }, [ingress?.enabled, ingress?.status]);

  const addDomain = async () => {
    if (!newDomain.trim()) return;
    setErr('');
    try {
      await api.createDomain(newDomain.trim());
      setNewDomain('');
      load();
    } catch (e) { setErr(String(e)); }
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
      const r = await api.exposeService(svc.id);
      onChanged();
      load();
      // Reflect immediately.
      window.open(r.url, '_blank');
    } catch (e) { setErr(String(e)); }
  };

  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;
  const routable = services.filter((s) => s.port > 0 && s.kind !== 'compose');
  const tlsOn = !!ingress?.acmeEmail;
  const scheme = tlsOn ? 'https' : 'http';

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
              HTTPS on: certs auto-issued via HTTP-01. Port {httpPort} must be publicly reachable for the challenge; HTTP redirects to HTTPS.
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
          <input className="input" placeholder="app.example.com" value={newDomain}
            onChange={(e) => setNewDomain(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && addDomain()} />
          <button className="btn primary" onClick={addDomain}><Plus size={13} /> Add</button>
        </div>
        {domains.map((d) => (
          <div className="row spread" key={d.id} style={{ padding: '7px 0', borderTop: '1px solid var(--border)' }}>
            <span className="mono">{d.name}</span>
            <button className="btn sm danger" onClick={async () => { await api.deleteDomain(d.id); load(); onChanged(); }}>
              <Trash2 size={12} />
            </button>
          </div>
        ))}
        {domains.length === 0 && <div className="faint">No custom domains yet. Add one and point its DNS A record at the ingress node's IP — or use <b>Instant URL</b> below for a zero-DNS link.</div>}
      </div>

      {/* Per-app routing */}
      <div className="section-title"><NetworkIcon size={15} /> App Routing</div>
      {!ingress?.enabled && <div className="card faint" style={{ marginBottom: 12 }}>Enable the edge above to expose apps publicly.</div>}
      <div className="node-card">
        {routable.map((s) => {
          const live = s.domain && ingress?.enabled && ingress.status === 'running';
          const url = `${scheme}://${s.domain}`;
          return (
            <div className="card row spread" key={s.id}>
              <div style={{ minWidth: 0 }}>
                <strong>{s.name}</strong>
                <span className="dim" style={{ marginLeft: 10 }}>
                  {nodeName(s.nodeId)} · :{s.port}
                  {s.nodeId !== ingress?.nodeId && ingress?.enabled && s.domain &&
                    <span className="badge cyan" style={{ marginLeft: 8 }}>via mesh relay</span>}
                </span>
                {s.domain && (
                  <div style={{ marginTop: 4 }}>
                    {live
                      ? <a className="mono" href={url} target="_blank" rel="noreferrer" style={{ color: 'var(--accent)' }}>{url} <ExternalLink size={11} /></a>
                      : <span className="mono faint">{url} <span className="faint">(edge not ready)</span></span>}
                  </div>
                )}
              </div>
              <div className="row" style={{ flexShrink: 0 }}>
                <button className="btn sm" disabled={!ingress?.enabled} title="Assign an instant nip.io URL — no DNS setup"
                  onClick={() => expose(s)}><Zap size={12} /> Instant URL</button>
                <select className="input" style={{ width: 220 }} value={s.domain || ''}
                  onChange={(e) => setRoute(s, e.target.value)}>
                  <option value="">— no public route —</option>
                  {s.domain && !domains.some((d) => d.name === s.domain) &&
                    <option value={s.domain}>{s.domain} (instant)</option>}
                  {domains.map((d) => <option key={d.id} value={d.name}>{d.name}</option>)}
                </select>
              </div>
            </div>
          );
        })}
        {routable.length === 0 && <div className="empty">No routable services (services need a port).</div>}
      </div>

      {err && <div className="badge red" style={{ marginTop: 12 }}>{err}</div>}
    </>
  );
}
