import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Globe, Shield, Waypoints, Boxes, AlertTriangle, Plus, Trash2, RefreshCw, ChevronDown,
  ChevronRight, Lock, Rocket, Cloud, ArrowRight,
} from 'lucide-react';
import { api } from '../api';
import type { Domain, IngressConfig, MeshStatus, ClusterStatus, NetworkDiagnosis } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { Led, CopyBtn, CopyField, Modal, Spinner, UrlChip, publicUrls } from '../components/ui';

// recordHost: what goes in the registrar's "Host/Name" field.
function recordHost(domain: string): string {
  const wildcard = domain.startsWith('*.');
  const base = wildcard ? domain.slice(2) : domain;
  const parts = base.split('.');
  if (parts.length <= 2) return wildcard ? '*' : '@';
  const host = parts.slice(0, -2).join('.');
  return wildcard ? `*.${host}` : host;
}

function DomainBadge({ d }: { d: Domain }) {
  switch (d.status) {
    case 'verified': return <span className="badge green"><Led status="verified" /> verified</span>;
    case 'misconfigured': return <span className="badge red"><AlertTriangle size={11} /> misconfigured</span>;
    default: return <span className="badge yellow"><Led status="pending" /> pending DNS</span>;
  }
}

// NetworkPage: fleet-level connectivity — the edge, your domains, and how
// the internet reaches your rack. Per-app routing lives on each app's
// Networking tab; this page shows the overview.
export function NetworkPage() {
  const { nodes, services, nodeName, refresh } = useData();
  const toast = useToast();
  const confirm = useConfirm();

  const [domains, setDomains] = useState<Domain[]>([]);
  const [newDomain, setNewDomain] = useState('');
  const [expanded, setExpanded] = useState('');
  const [checking, setChecking] = useState('');
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

  const load = useCallback(() => {
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
  }, []);
  useEffect(load, [load]);

  // Poll while anything is settling: edge starting, or domains not green yet.
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
    try {
      const d = await api.createDomain(newDomain.trim());
      setNewDomain('');
      setExpanded(d.id); // open the setup instructions right away
      toast.ok(`${d.name} added — create the DNS record shown below.`);
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const removeDomain = async (d: Domain) => {
    const routed = services.filter((s) => s.domain === d.name || (d.wildcard && (s.domain || '').endsWith('.' + d.name.slice(2))));
    const yes = await confirm({
      title: `Remove ${d.name}?`,
      body: routed.length > 0
        ? `${routed.length} app${routed.length > 1 ? 's' : ''} routed through it will lose their public URL.`
        : 'The domain and its verification state are removed. Your DNS records are untouched.',
      confirmLabel: 'Remove domain', danger: true,
    });
    if (!yes) return;
    try {
      await api.deleteDomain(d.id);
      toast.ok(`${d.name} removed.`);
      load(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const verify = async (id: string) => {
    setChecking(id);
    try {
      const d = await api.verifyDomain(id);
      setDomains((ds) => ds.map((x) => (x.id === d.id ? d : x)));
    } catch (e) { toast.err(cleanError(e)); }
    setChecking('');
  };

  const applyIngress = async (enabled: boolean) => {
    setBusy(true);
    try {
      await api.setIngress({
        enabled,
        nodeId: ingressNode || nodes[0]?.id || '',
        httpPort: parseInt(httpPort, 10) || 80,
        httpsPort: parseInt(httpsPort, 10) || 443,
        acmeEmail: acmeEmail.trim(),
      });
      toast.ok(enabled ? 'Edge deploying — status updates below.' : 'Edge disabled.');
      load();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const tlsOn = !!acmeEmail.trim();
  const edgeReady = ingress?.enabled && ingress.status === 'running';
  const lanOnly = diagnosis?.lanOnly ?? false;
  const publicApps = services.filter((s) => s.domain || s.quickTunnelUrl);

  return (
    <div className="page">
      <div className="page-title">Network</div>
      <div className="page-sub">How the internet reaches your rack — edge, domains, and public routes</div>

      {lanOnly && (
        <div className="card row spread" style={{ borderColor: 'var(--warn)', marginBottom: 16 }}>
          <div>
            <strong className="row" style={{ gap: 7 }}><AlertTriangle size={13} style={{ color: 'var(--warn)' }} /> Your apps are LAN-only right now</strong>
            <div className="dim" style={{ marginTop: 4 }}>
              No machine here holds a public IP — domains and instant URLs only work inside your network.
            </div>
          </div>
          <button className="btn primary" onClick={() => setWizardOpen(true)}><Rocket size={13} /> Get public access</button>
        </div>
      )}

      <div className="grid-2">
        {/* Edge config */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Shield size={15} /> Edge proxy
            <span style={{ marginLeft: 'auto' }}>
              {!ingress?.enabled ? <span className="badge gray">off</span>
                : ingress.status === 'running' ? <span className="badge green"><Led status="running" /> running on {nodeName(ingress.nodeId)}</span>
                : ingress.status === 'failed' ? <span className="badge red"><AlertTriangle size={11} /> failed</span>
                : <span className="badge yellow"><Led status="pending" /> starting…</span>}
            </span>
          </div>
          <div className="dim" style={{ marginBottom: 12 }}>
            One Traefik instance routes every domain and instant URL to the right app — across machines, over the mesh.
          </div>
          {ingress?.status === 'failed' && ingress.statusMsg && (
            <div className="card inset" style={{ borderColor: 'var(--crit)', marginBottom: 12, whiteSpace: 'pre-wrap', fontSize: 12.5, lineHeight: 1.6 }}>
              {ingress.statusMsg}
            </div>
          )}
          <div className="field">
            <label>Run the edge on</label>
            <select className="input" value={ingressNode} onChange={(e) => setIngressNode(e.target.value)}>
              <option value="">— select a machine —</option>
              {nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.publicIp || n.ip})</option>)}
            </select>
          </div>
          <div className="grid-2">
            <div className="field">
              <label>HTTP port</label>
              <input className="input mono" value={httpPort} onChange={(e) => setHttpPort(e.target.value.replace(/\D/g, ''))} />
            </div>
            <div className="field">
              <label>HTTPS port</label>
              <input className="input mono" value={httpsPort} onChange={(e) => setHttpsPort(e.target.value.replace(/\D/g, ''))} />
            </div>
          </div>
          <div className="field">
            <label>Let's Encrypt email {tlsOn ? '' : '(optional — enables automatic HTTPS)'}</label>
            <input className="input" placeholder="you@example.com" value={acmeEmail} onChange={(e) => setAcmeEmail(e.target.value)} />
            {tlsOn && <span className="faint">
              HTTPS on: certificates issue automatically. Port {httpPort} must be reachable from the internet; HTTP redirects to HTTPS.
            </span>}
          </div>
          <div className="row">
            <button className="btn primary" disabled={busy || !ingressNode} onClick={() => applyIngress(true)}>
              {busy ? <Spinner size={13} /> : null} {ingress?.enabled ? 'Update edge' : 'Enable edge'}
            </button>
            {ingress?.enabled && (
              <button className="btn danger" disabled={busy} onClick={() => applyIngress(false)}>Disable</button>
            )}
          </div>
        </div>

        {/* Mesh */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Waypoints size={15} /> WireGuard mesh
            {meshStatus?.enabled
              ? <span className="badge green" style={{ marginLeft: 'auto' }}>active</span>
              : <span className="badge gray" style={{ marginLeft: 'auto' }}>disabled</span>}
          </div>
          {meshStatus?.enabled ? (
            <>
              <div className="dim" style={{ marginBottom: 10, lineHeight: 1.7 }}>
                Hub <span className="mono">{meshStatus.hubIp}</span> on UDP <span className="mono">:{meshStatus.udpPort}</span>.
                Machines dial out — works behind CGNAT, no port forwarding.
              </div>
              {(meshStatus.peers ?? []).map((p) => (
                <div className="row spread" key={p.nodeId} style={{ padding: '6px 0', borderTop: '1px solid var(--border)' }}>
                  <span>{nodeName(p.nodeId)}</span>
                  <span className="mono dim">{p.meshIp}</span>
                </div>
              ))}
              {(meshStatus.peers ?? []).length === 0 && <div className="faint">No machines have joined the mesh yet.</div>}
            </>
          ) : <div className="faint">Mesh is disabled (start the server with a mesh port).</div>}

          {cluster?.enabled && (
            <>
              <div className="section-title" style={{ marginBottom: 8 }}><Boxes size={14} /> Control plane (HA)</div>
              {(cluster.peers ?? []).map((p) => (
                <div className="row spread" key={p.id} style={{ padding: '5px 0', borderTop: '1px solid var(--border)' }}>
                  <span className="mono" style={{ fontSize: 12 }}>{p.id} <span className="faint">{p.addr}</span></span>
                  <span className={`badge ${p.role === 'leader' ? 'green' : 'gray'}`}>{p.role}</span>
                </div>
              ))}
            </>
          )}
        </div>
      </div>

      {/* Domains */}
      <div className="section-title"><Globe size={15} /> Your domains</div>
      <div className="card">
        <div className="row" style={{ marginBottom: 12 }}>
          <input className="input" placeholder="app.example.com — or *.example.com to give every app its own subdomain"
            value={newDomain} onChange={(e) => setNewDomain(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && addDomain()} />
          <button className="btn primary" onClick={addDomain}><Plus size={13} /> Add</button>
        </div>

        {domains.map((d) => {
          const open = expanded === d.id;
          return (
            <div key={d.id} style={{ borderTop: '1px solid var(--border)' }}>
              <div className="row spread" style={{ padding: '9px 0', cursor: 'pointer' }} onClick={() => setExpanded(open ? '' : d.id)}>
                <span className="row" style={{ gap: 8 }}>
                  {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                  <span className="mono">{d.name}</span>
                  {d.wildcard && <span className="badge purple">wildcard</span>}
                  <DomainBadge d={d} />
                  {d.certOk && <span className="badge green"><Lock size={10} /> TLS</span>}
                </span>
                <span className="row" onClick={(e) => e.stopPropagation()}>
                  <button className="btn sm" disabled={checking === d.id} onClick={() => verify(d.id)}>
                    <RefreshCw size={11} className={checking === d.id ? 'spin' : ''} /> {checking === d.id ? 'Checking…' : 'Verify now'}
                  </button>
                  <button className="btn sm danger" aria-label={`Remove ${d.name}`} onClick={() => removeDomain(d)}>
                    <Trash2 size={12} />
                  </button>
                </span>
              </div>

              {open && (
                <div className="card inset" style={{ margin: '0 0 12px 21px' }}>
                  <div className="dim" style={{ marginBottom: 8 }}>
                    Create this record at your registrar (GoDaddy, Namecheap, Cloudflare DNS, …):
                  </div>
                  <div className="row wrap" style={{ gap: 8, alignItems: 'stretch', marginBottom: 10 }}>
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

                  <div style={{ lineHeight: 2 }}>
                    <div className="row" style={{ gap: 8 }}>
                      <Led status={d.status === 'verified' ? 'ok' : d.status === 'misconfigured' ? 'error' : 'pending'} />
                      <strong>DNS</strong>
                      <span className="dim">
                        {d.status === 'verified'
                          ? <>resolves to <span className="mono">{d.expectedIp}</span></>
                          : (d.message || 'not checked yet')}
                      </span>
                    </div>
                    <div className="row" style={{ gap: 8 }}>
                      <Led status={d.edgeOk ? 'ok' : 'pending'} />
                      <strong>Edge</strong>
                      <span className="dim">{d.edgeOk
                        ? 'your edge answers at that address — traffic will route'
                        : edgeReady ? 'edge did not answer there (port forwarding / firewall / another server on the port)' : 'enable the edge to check'}</span>
                    </div>
                    {tlsOn && !d.wildcard && (
                      <div className="row" style={{ gap: 8 }}>
                        <Led status={d.certOk ? 'ok' : 'pending'} />
                        <strong>Certificate</strong>
                        <span className="dim">{d.certOk
                          ? `issued by ${d.certIssuer}${d.certExpiry ? `, expires ${new Date(d.certExpiry).toLocaleDateString()}` : ''}`
                          : 'not issued yet — needs DNS verified + port 80 reachable from the internet, then an app routed to it'}</span>
                      </div>
                    )}
                    {tlsOn && d.wildcard && (
                      <div className="faint"><Lock size={10} /> Certificates issue per subdomain automatically as apps are routed.</div>
                    )}
                    {d.lastChecked && <div className="faint">last checked {new Date(d.lastChecked).toLocaleTimeString()} — rechecks every 30s until verified, then hourly</div>}
                  </div>
                </div>
              )}
            </div>
          );
        })}
        {domains.length === 0 && (
          <div className="faint" style={{ lineHeight: 1.7 }}>
            No domains yet. Add one and you'll get the exact DNS record to create — or add a wildcard
            like <span className="mono">*.example.com</span> once and every app can have its
            own <span className="mono">name.example.com</span>.
          </div>
        )}
      </div>

      {/* Public routes overview */}
      <div className="section-title"><Globe size={15} /> Public routes</div>
      <div className="stack" style={{ gap: 8 }}>
        {publicApps.map((s) => {
          const urls = publicUrls(s, ingress);
          const backing = domains.find((d) => (!d.wildcard && d.name === s.domain) || (d.wildcard && (s.domain || '').endsWith('.' + d.name.slice(2))));
          return (
            <div key={s.id} className="card row spread" style={{ padding: '11px 14px' }}>
              <div className="row wrap" style={{ minWidth: 0 }}>
                <Led status={s.status} />
                <strong>{s.name}</strong>
                {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
                {s.quickTunnelUrl && <span className="badge yellow"><Cloud size={10} /> tunnel</span>}
                {backing && backing.status !== 'verified' && <span className="badge yellow"><AlertTriangle size={10} /> DNS not verified</span>}
                {s.domain && ingress?.enabled && s.nodeId !== ingress.nodeId && <span className="badge amber">via mesh relay</span>}
              </div>
              <Link to={`/apps/${s.id}/networking`} className="btn sm" style={{ flexShrink: 0 }}>
                Manage <ArrowRight size={11} />
              </Link>
            </div>
          );
        })}
        {publicApps.length === 0 && (
          <div className="empty">
            <div className="empty-title">Nothing is public yet</div>
            <div>Open any app's <strong>Networking</strong> tab to give it an instant URL, a tunnel, or a domain.</div>
          </div>
        )}
      </div>

      {/* Public access wizard */}
      {wizardOpen && diagnosis && (
        <Modal title="Get public access" icon={<Rocket size={15} />} width={680} onClose={() => setWizardOpen(false)}>
          <div className="dim" style={{ lineHeight: 1.7, marginBottom: 14 }}>
            For the internet to reach your apps, traffic needs <b>one publicly reachable landing point</b>.
            From there, the WireGuard mesh tunnels to every machine automatically — even behind CGNAT.
          </div>

          <div className="section-title" style={{ marginTop: 0 }}>Your machines right now</div>
          <div className="card inset" style={{ marginBottom: 14 }}>
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
          <div className="card inset dim" style={{ lineHeight: 1.8, marginBottom: 14 }}>
            1. In your router, forward TCP <b>80</b> and <b>443</b> to the edge machine's LAN IP.<br />
            2. Set the edge's HTTP/HTTPS ports to 80/443 on this page.<br />
            3. Domains then point at your public IP; instant URLs use it automatically.<br />
            <span className="faint">Doesn't work behind CGNAT — if your router's WAN IP differs from the public IP shown above, use Option B.</span>
          </div>

          <div className="section-title">Option B — VPS anchor <span className="faint">(works everywhere, ~$4/mo)</span></div>
          <div className="card inset dim" style={{ lineHeight: 1.8, marginBottom: 14 }}>
            1. Get any small VPS with a public IP (Hetzner, DigitalOcean, Oracle free tier…).<br />
            2. Run the control plane there:
            <div style={{ margin: '6px 0' }}><CopyField text="rackops server --listen :8080" /></div>
            3. Join this machine to it (dials out — no port forwarding needed):
            <div style={{ margin: '6px 0' }}><CopyField text={diagnosis.joinCommand} /></div>
            4. On the VPS dashboard, enable the <b>edge on the VPS machine</b> (ports 80/443).<br />
            5. Your apps stay home — traffic flows VPS → encrypted mesh → here, automatically.
          </div>

          <div className="section-title">Option C — quick tunnel <span className="faint">(zero setup, testing only)</span></div>
          <div className="card inset dim" style={{ lineHeight: 1.8 }}>
            On any app's <b>Networking</b> tab, hit <b><Cloud size={11} /> Start tunnel</b> — you get a free
            public <span className="mono">trycloudflare.com</span> URL in seconds, even behind CGNAT or a
            mobile hotspot. <span className="faint">The URL changes on every restart — great for demos, not production.</span>
          </div>
        </Modal>
      )}
    </div>
  );
}
