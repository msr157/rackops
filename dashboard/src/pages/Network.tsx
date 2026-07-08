import { useEffect, useState } from 'react';
import { Globe, Network as NetworkIcon, Plus, Trash2, Shield, Waypoints, Boxes } from 'lucide-react';
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
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  const load = () => {
    api.domains().then((d) => setDomains(d ?? [])).catch(() => {});
    api.ingress().then((i) => {
      setIngressCfg(i);
      if (i.nodeId) setIngressNode(i.nodeId);
      if (i.httpPort) setHttpPort(String(i.httpPort));
      setAcmeEmail(i.acmeEmail ?? '');
    }).catch(() => {});
    api.mesh().then(setMeshStatus).catch(() => {});
    api.cluster().then(setCluster).catch(() => {});
  };
  useEffect(load, []);

  const addDomain = async () => {
    if (!newDomain.trim()) return;
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
        httpsPort: 443,
        acmeEmail: acmeEmail.trim(),
      });
      load();
    } catch (e) { setErr(String(e)); }
    setBusy(false);
  };

  const setRoute = async (svc: Service, domain: string) => {
    const mode = !domain ? 'none' : (svc.nodeId === ingress?.nodeId ? 'static' : 'relay');
    await api.setServiceDomain(svc.id, domain, mode);
    onChanged();
  };

  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;
  const routable = services.filter((s) => s.port > 0 && s.kind !== 'compose');

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
          {meshStatus?.enabled && (
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
            </>
          )}
        </div>

        {/* Ingress config */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Shield size={15} /> Edge (Traefik)
            {ingress?.enabled
              ? <span className="badge green" style={{ marginLeft: 'auto' }}>running on {nodeName(ingress.nodeId)}</span>
              : <span className="badge gray" style={{ marginLeft: 'auto' }}>off</span>}
          </div>
          <div className="field">
            <label>Ingress node (needs public IP or LAN reachability)</label>
            <select className="input" value={ingressNode} onChange={(e) => setIngressNode(e.target.value)}>
              <option value="">— select —</option>
              {nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.ip})</option>)}
            </select>
          </div>
          <div className="grid-2">
            <div className="field">
              <label>HTTP port</label>
              <input className="input" value={httpPort} onChange={(e) => setHttpPort(e.target.value.replace(/\D/g, ''))} />
            </div>
            <div className="field">
              <label>Let's Encrypt email (optional, enables HTTPS)</label>
              <input className="input" placeholder="you@example.com" value={acmeEmail} onChange={(e) => setAcmeEmail(e.target.value)} />
            </div>
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
            <button className="btn sm danger" onClick={async () => { await api.deleteDomain(d.id); load(); }}>
              <Trash2 size={12} />
            </button>
          </div>
        ))}
        {domains.length === 0 && <div className="faint">No domains yet — add one, then pick it in the routing table below. Point its DNS A record at the ingress node's IP.</div>}
      </div>

      {/* Per-app routing */}
      <div className="section-title"><NetworkIcon size={15} /> App Routing</div>
      <div className="node-card">
        {routable.map((s) => (
          <div className="card row spread" key={s.id}>
            <div>
              <strong>{s.name}</strong>
              <span className="dim" style={{ marginLeft: 10 }}>
                {nodeName(s.nodeId)} · :{s.port}
                {s.nodeId !== ingress?.nodeId && ingress?.enabled && s.domain &&
                  <span className="badge cyan" style={{ marginLeft: 8 }}>via mesh relay</span>}
              </span>
            </div>
            <select className="input" style={{ width: 260 }} value={s.domain || ''}
              onChange={(e) => setRoute(s, e.target.value)}>
              <option value="">— no public route —</option>
              {domains.map((d) => <option key={d.id} value={d.name}>{d.name}</option>)}
            </select>
          </div>
        ))}
        {routable.length === 0 && <div className="empty">No routable services (services need a port).</div>}
      </div>

      {err && <div className="badge red" style={{ marginTop: 12 }}>{err}</div>}
    </>
  );
}
