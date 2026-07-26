import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
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
import {
  EmptyState, Field, InfoTip, Led, CopyBtn, CopyField, Modal, Skeleton, SkeletonRows,
  Spinner, UrlChip, publicUrls,
} from '../components/ui';

// recordHost: what goes in the registrar's "Host/Name" field.
function recordHost(domain: string): string {
  const wildcard = domain.startsWith('*.');
  const base = wildcard ? domain.slice(2) : domain;
  const parts = base.split('.');
  if (parts.length <= 2) return wildcard ? '*' : '@';
  const host = parts.slice(0, -2).join('.');
  return wildcard ? `*.${host}` : host;
}

// Shape check only — the server does the real validation. This exists so a
// pasted URL or a stray space fails softly under the field instead of as a
// round-trip error toast.
const DOMAIN_SHAPE = /^(\*\.)?[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$/;

function DomainBadge({ d }: { d: Domain }) {
  switch (d.status) {
    case 'verified': return <span className="badge ok"><Led status="verified" /> verified</span>;
    case 'misconfigured': return <span className="badge crit"><AlertTriangle size={11} aria-hidden /> misconfigured</span>;
    default: return <span className="badge warn"><Led status="pending" /> pending DNS</span>;
  }
}

// A section whose fetch failed says so, quietly, with a way back — a failed
// load must never wear the same face as "you have none".
function LoadError({ onRetry, children }: { onRetry: () => void; children: React.ReactNode }) {
  return (
    <div className="row" style={{ padding: '8px 0' }}>
      <AlertTriangle size={13} style={{ color: 'var(--warn)', flex: 'none' }} aria-hidden />
      <span className="dim">{children}</span>
      <button className="btn sm" style={{ marginLeft: 'auto' }} onClick={onRetry}>Retry</button>
    </div>
  );
}

type LoadState = 'loading' | 'ready' | 'error';

// NetworkPage: fleet-level connectivity — the edge, your domains, and how
// the internet reaches your rack. Per-app routing lives on each app's
// Networking tab; this page shows the overview.
export function NetworkPage() {
  const { nodes, services, nodeName, refresh, loaded } = useData();
  const toast = useToast();
  const confirm = useConfirm();

  const [domains, setDomains] = useState<Domain[]>([]);
  const [newDomain, setNewDomain] = useState('');
  const [domainError, setDomainError] = useState('');
  const [expanded, setExpanded] = useState('');
  const [checking, setChecking] = useState('');
  const [removing, setRemoving] = useState('');
  const [ingress, setIngressCfg] = useState<IngressConfig | null>(null);
  const [meshStatus, setMeshStatus] = useState<MeshStatus | null>(null);
  const [cluster, setCluster] = useState<ClusterStatus | null>(null);
  const [diagnosis, setDiagnosis] = useState<NetworkDiagnosis | null>(null);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [acmeEmail, setAcmeEmail] = useState('');
  const [ingressNode, setIngressNode] = useState('');
  const [httpPort, setHttpPort] = useState('80');
  const [httpsPort, setHttpsPort] = useState('443');
  const [busy, setBusy] = useState<false | 'apply' | 'disable'>(false);

  const [domainsState, setDomainsState] = useState<LoadState>('loading');
  const [ingressState, setIngressState] = useState<LoadState>('loading');
  const [meshState, setMeshState] = useState<LoadState>('loading');

  // The edge form belongs to the user the moment they touch it: background
  // reloads (settling poll, SSE-triggered refreshes) must not stomp on
  // half-typed edits. Cleared after a successful apply, when the server's
  // copy becomes the truth again.
  const formDirty = useRef(false);

  const load = useCallback(() => {
    api.domains()
      .then((d) => { setDomains(d ?? []); setDomainsState('ready'); })
      .catch(() => setDomainsState((s) => (s === 'ready' ? s : 'error')));
    api.ingress()
      .then((i) => {
        setIngressCfg(i);
        setIngressState('ready');
        if (!formDirty.current) {
          if (i.nodeId) setIngressNode(i.nodeId);
          if (i.httpPort) setHttpPort(String(i.httpPort));
          if (i.httpsPort) setHttpsPort(String(i.httpsPort));
          setAcmeEmail(i.acmeEmail ?? '');
        }
      })
      .catch(() => setIngressState((s) => (s === 'ready' ? s : 'error')));
    api.mesh()
      .then((m) => { setMeshStatus(m); setMeshState('ready'); })
      .catch(() => setMeshState((s) => (s === 'ready' ? s : 'error')));
    api.cluster().then(setCluster).catch(() => {});
    api.networkDiagnosis().then(setDiagnosis).catch(() => {});
  }, []);
  useEffect(load, [load]);

  // Poll while anything is settling: edge starting, or domains not green yet.
  const settling = (ingress?.enabled && ingress.status !== 'running') || domains.some((d) => d.status !== 'verified');
  useEffect(() => {
    if (!settling) return;
    const t = setInterval(() => {
      api.ingress().then((i) => { setIngressCfg(i); setIngressState('ready'); }).catch(() => {});
      api.domains().then((d) => { setDomains(d ?? []); setDomainsState('ready'); }).catch(() => {});
    }, 5000);
    return () => clearInterval(t);
  }, [settling]);

  const [addingDomain, setAddingDomain] = useState(false);
  const addDomain = async () => {
    const name = newDomain.trim();
    if (!name || addingDomain) return;
    if (!DOMAIN_SHAPE.test(name)) {
      setDomainError('A domain can only contain letters, digits, dots, and hyphens — like app.example.com or *.example.com.');
      return;
    }
    setAddingDomain(true);
    try {
      const d = await api.createDomain(name);
      setNewDomain('');
      setExpanded(d.id); // open the setup instructions right away
      toast.ok(`${d.name} added — create the DNS record shown below.`);
      load();
    } catch (e) { toast.err(cleanError(e)); }
    setAddingDomain(false);
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
    setRemoving(d.id);
    try {
      await api.deleteDomain(d.id);
      toast.ok(`${d.name} removed.`);
      load(); refresh();
    } catch (e) { toast.err(cleanError(e)); }
    setRemoving('');
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
    setBusy(enabled ? 'apply' : 'disable');
    try {
      await api.setIngress({
        enabled,
        nodeId: ingressNode || nodes[0]?.id || '',
        httpPort: parseInt(httpPort, 10) || 80,
        httpsPort: parseInt(httpsPort, 10) || 443,
        acmeEmail: acmeEmail.trim(),
      });
      toast.ok(enabled ? 'Edge deploying — status updates below.' : 'Edge disabled.');
      formDirty.current = false;
      load();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  // Disabling the edge is not an app lifecycle action — it takes every
  // public route down with it, so it pauses for an explicit decision.
  const disableEdge = async () => {
    const routed = services.filter((s) => !!s.domain);
    const yes = await confirm({
      title: 'Disable the edge?',
      body: routed.length > 0
        ? `This kills every public route: ${routed.length} app${routed.length > 1 ? 's' : ''} stop${routed.length > 1 ? '' : 's'} answering at ${routed.length > 1 ? 'their domains' : 'its domain'} the moment the edge goes down. The apps keep running on your rack — they just become unreachable from the internet until you enable the edge again.`
        : 'This kills every public route through the edge. Nothing is routed through it right now, so nothing goes dark today — but domains will not work until you enable it again.',
      confirmLabel: 'Disable edge', danger: true,
    });
    if (yes) applyIngress(false);
  };

  const tlsOn = !!acmeEmail.trim();
  const edgeReady = ingress?.enabled && ingress.status === 'running';
  // A deploy is in flight until the edge lands on running or failed — the
  // settling poll above keeps `ingress` fresh, so this self-clears.
  const edgeDeploying = !!ingress?.enabled && ingress.status !== 'running' && ingress.status !== 'failed';
  const lanOnly = diagnosis?.lanOnly ?? false;
  const publicApps = services.filter((s) => s.domain || s.quickTunnelUrl);
  const routesLoading = !loaded || ingressState === 'loading';

  return (
    <div className="page">
      <div className="page-title">Network</div>
      <div className="page-sub">How the internet reaches your rack — edge, domains, and public routes</div>

      {lanOnly && (
        <div className="card warn row spread" style={{ marginBottom: 16 }}>
          <div>
            <strong className="row" style={{ gap: 7 }}><AlertTriangle size={13} style={{ color: 'var(--warn)' }} aria-hidden /> Your apps are LAN-only right now</strong>
            <div className="dim" style={{ marginTop: 4 }}>
              No machine here holds a public IP — domains and instant URLs only work inside your network.
            </div>
          </div>
          <button className="btn primary" onClick={() => setWizardOpen(true)}><Rocket size={13} aria-hidden /> Get public access</button>
        </div>
      )}

      <div className="grid-2">
        {/* Edge config */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Shield size={15} aria-hidden /> Edge proxy
            <span style={{ marginLeft: 'auto' }}>
              {ingressState === 'loading' ? <Skeleton width={64} height={20} radius={20} />
                : ingressState === 'error' ? null
                : !ingress?.enabled ? <span className="badge neutral">off</span>
                : ingress.status === 'running' ? <span className="badge ok"><Led status="running" /> running on {nodeName(ingress.nodeId)}</span>
                : ingress.status === 'failed' ? <span className="badge crit"><AlertTriangle size={11} aria-hidden /> failed</span>
                : <span className="badge warn"><Led status="pending" /> starting…</span>}
            </span>
          </div>
          <div className="dim" style={{ marginBottom: 12 }}>
            One Traefik instance routes every domain and instant URL to the right app — across machines, over the mesh.
            <InfoTip wide>
              Pick the machine the internet can reach (a public IP or a forwarded port). Apps on
              other machines are still reachable through it — traffic rides the encrypted mesh.
              Adding a Let&rsquo;s Encrypt email turns on automatic HTTPS certificates.
            </InfoTip>
          </div>
          {ingressState === 'loading' && <SkeletonRows count={4} height={36} />}
          {ingressState === 'error' && (
            <LoadError onRetry={() => { setIngressState('loading'); load(); }}>
              The edge configuration didn&rsquo;t load.
            </LoadError>
          )}
          {ingressState === 'ready' && (
            <>
              {ingress?.status === 'failed' && ingress.statusMsg && (
                <div className="card inset crit" style={{ marginBottom: 12, whiteSpace: 'pre-wrap', fontSize: 12.5, lineHeight: 1.6 }}>
                  {ingress.statusMsg}
                </div>
              )}
              <Field label="Run the edge on">
                {(p) => (
                  <select className="input" {...p} value={ingressNode}
                    onChange={(e) => { formDirty.current = true; setIngressNode(e.target.value); }}>
                    <option value="">— select a machine —</option>
                    {nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.publicIp || n.ip})</option>)}
                  </select>
                )}
              </Field>
              <div className="grid-2">
                <Field label="HTTP port">
                  {(p) => (
                    <input className="input mono" {...p} inputMode="numeric" value={httpPort}
                      onChange={(e) => { formDirty.current = true; setHttpPort(e.target.value.replace(/\D/g, '')); }} />
                  )}
                </Field>
                <Field label="HTTPS port">
                  {(p) => (
                    <input className="input mono" {...p} inputMode="numeric" value={httpsPort}
                      onChange={(e) => { formDirty.current = true; setHttpsPort(e.target.value.replace(/\D/g, '')); }} />
                  )}
                </Field>
              </div>
              <Field
                label={`Let's Encrypt email ${tlsOn ? '' : '(optional — enables automatic HTTPS)'}`}
                hint={tlsOn
                  ? `HTTPS on: certificates issue automatically. Port ${httpPort} must be reachable from the internet; HTTP redirects to HTTPS.`
                  : undefined}>
                {(p) => (
                  <input className="input" {...p} type="email" placeholder="you@example.com" value={acmeEmail}
                    onChange={(e) => { formDirty.current = true; setAcmeEmail(e.target.value); }} />
                )}
              </Field>
              <div className="row">
                <button className="btn primary" disabled={!!busy || edgeDeploying || !ingressNode} onClick={() => applyIngress(true)}>
                  {busy === 'apply' || edgeDeploying ? <Spinner size={13} /> : null}
                  {edgeDeploying ? 'Deploying…' : ingress?.enabled ? 'Update edge' : 'Enable edge'}
                </button>
                {ingress?.enabled && (
                  <button className="btn danger" disabled={!!busy || edgeDeploying} onClick={disableEdge}>
                    {busy === 'disable' && <Spinner size={13} />} Disable
                  </button>
                )}
              </div>
            </>
          )}
        </div>

        {/* Mesh */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Waypoints size={15} aria-hidden /> WireGuard mesh
            <InfoTip wide>
              An encrypted private network between your machines. Each machine dials out to the
              server, so it works behind CGNAT with zero port forwarding — the edge proxy uses it
              to reach apps on other machines.
            </InfoTip>
            <span style={{ marginLeft: 'auto' }}>
              {meshState === 'loading' ? <Skeleton width={60} height={20} radius={20} />
                : meshState === 'error' ? null
                : meshStatus?.enabled ? <span className="badge ok">active</span>
                : <span className="badge neutral">disabled</span>}
            </span>
          </div>
          {meshState === 'loading' && <SkeletonRows count={3} height={28} />}
          {meshState === 'error' && (
            <LoadError onRetry={() => { setMeshState('loading'); load(); }}>
              The mesh status didn&rsquo;t load.
            </LoadError>
          )}
          {meshState === 'ready' && (meshStatus?.enabled ? (
            <>
              <div className="dim" style={{ marginBottom: 10, lineHeight: 1.7 }}>
                Hub <span className="mono">{meshStatus.hubIp}</span> on UDP <span className="mono">:{meshStatus.udpPort}</span>.
                Machines dial out — works behind CGNAT, no port forwarding.
              </div>
              <div className="list">
                {(meshStatus.peers ?? []).map((p) => (
                  <div className="list-row spread" key={p.nodeId}>
                    <span>{nodeName(p.nodeId)}</span>
                    <span className="mono dim">{p.meshIp}</span>
                  </div>
                ))}
              </div>
              {(meshStatus.peers ?? []).length === 0 && <div className="faint">No machines have joined the mesh yet.</div>}
            </>
          ) : <div className="faint">Mesh is disabled (start the server with a mesh port).</div>)}

          {cluster?.enabled && (
            <>
              <div className="section-title" style={{ marginBottom: 8 }}><Boxes size={14} aria-hidden /> Control plane (HA)</div>
              <div className="list">
                {(cluster.peers ?? []).map((p) => (
                  <div className="list-row spread" key={p.id}>
                    <span className="mono" style={{ fontSize: 12 }}>{p.id} <span className="faint">{p.addr}</span></span>
                    <span className={`badge ${p.role === 'leader' ? 'accent' : 'neutral'}`}>{p.role}</span>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      </div>

      {/* Domains */}
      <div className="section-title">
        <Globe size={15} aria-hidden /> Your domains
        <InfoTip wide>
          Add a domain you own and RackOps shows the exact DNS record to create at your registrar,
          then keeps verifying DNS, routing, and the HTTPS certificate. A wildcard
          (<code>*.example.com</code>) gives every app its own subdomain from one record.
        </InfoTip>
      </div>
      <div className="card">
        <Field label="Add a domain" error={domainError || undefined}>
          {(p) => (
            <div className="row">
              <input className="input" {...p}
                placeholder="app.example.com — or *.example.com to give every app its own subdomain"
                value={newDomain}
                onChange={(e) => { setNewDomain(e.target.value); if (domainError) setDomainError(''); }}
                onKeyDown={(e) => e.key === 'Enter' && addDomain()} />
              <button className="btn primary" disabled={addingDomain} onClick={addDomain}>
                {addingDomain ? <Spinner size={13} /> : <Plus size={13} aria-hidden />} Add
              </button>
            </div>
          )}
        </Field>

        {domainsState === 'loading' && <SkeletonRows count={2} height={40} />}
        {domainsState === 'error' && (
          <LoadError onRetry={() => { setDomainsState('loading'); load(); }}>
            Your domains didn&rsquo;t load.
          </LoadError>
        )}
        {domainsState === 'ready' && domains.length > 0 && (
          <div className="list">
            {domains.map((d) => {
              const open = expanded === d.id;
              const panelId = `domain-${d.id}`;
              return (
                <Fragment key={d.id}>
                  <div className="list-row">
                    <button type="button" className="row disclosure" style={{ gap: 8 }}
                      aria-expanded={open} aria-controls={panelId}
                      onClick={() => setExpanded(open ? '' : d.id)}>
                      {open ? <ChevronDown size={13} aria-hidden /> : <ChevronRight size={13} aria-hidden />}
                      <span className="mono">{d.name}</span>
                      {d.wildcard && <span className="badge neutral">wildcard</span>}
                      <DomainBadge d={d} />
                      {d.certOk && <span className="badge ok"><Lock size={10} aria-hidden /> TLS</span>}
                    </button>
                    <span className="row">
                      {/* Fixed width so the label swap to "Checking…" doesn't jitter the row. */}
                      <button className="btn sm" style={{ minWidth: 104 }} disabled={checking === d.id}
                        aria-label={`Verify ${d.name} now`} onClick={() => verify(d.id)}>
                        <RefreshCw size={11} className={checking === d.id ? 'spin' : ''} aria-hidden />
                        {checking === d.id ? 'Checking…' : 'Verify now'}
                      </button>
                      <button className="btn sm danger" disabled={removing === d.id}
                        aria-label={`Remove ${d.name}`} title="Remove" onClick={() => removeDomain(d)}>
                        {removing === d.id ? <Spinner size={12} /> : <Trash2 size={12} aria-hidden />}
                      </button>
                    </span>
                  </div>

                  {open && (
                    <div className="card inset" id={panelId} style={{ margin: '2px 0 12px 21px' }}>
                      <div className="dim" style={{ marginBottom: 10 }}>
                        Create this record at your registrar (GoDaddy, Namecheap, Cloudflare DNS, …):
                      </div>
                      <dl className="kv" style={{ marginBottom: 12 }}>
                        <dt>Type</dt>
                        <dd>A</dd>
                        <dt>Host / Name</dt>
                        <dd>
                          <span className="row" style={{ gap: 8 }}>
                            {recordHost(d.name)}
                            <CopyBtn text={recordHost(d.name)} />
                          </span>
                        </dd>
                        <dt>Value / Points to</dt>
                        <dd>
                          <span className="row" style={{ gap: 8 }}>
                            {d.expectedIp || '(enable the edge first)'}
                            {d.expectedIp ? <CopyBtn text={d.expectedIp} /> : null}
                          </span>
                        </dd>
                        <dt>TTL</dt>
                        <dd>300</dd>
                      </dl>

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
                          <div className="faint"><Lock size={10} aria-hidden /> Certificates issue per subdomain automatically as apps are routed.</div>
                        )}
                        {d.lastChecked && <div className="faint">last checked {new Date(d.lastChecked).toLocaleTimeString()} — rechecks every 30s until verified, then hourly</div>}
                      </div>
                    </div>
                  )}
                </Fragment>
              );
            })}
          </div>
        )}
        {domainsState === 'ready' && domains.length === 0 && (
          <div className="faint" style={{ lineHeight: 1.7 }}>
            No domains yet. Add one and you'll get the exact DNS record to create — or add a wildcard
            like <span className="mono">*.example.com</span> once and every app can have its
            own <span className="mono">name.example.com</span>.
          </div>
        )}
      </div>

      {/* Public routes overview */}
      <div className="section-title"><Globe size={15} aria-hidden /> Public routes</div>
      <div className="stack" style={{ gap: 8 }}>
        {routesLoading && <SkeletonRows count={2} height={44} />}
        {!routesLoading && publicApps.map((s) => {
          const urls = publicUrls(s, ingress);
          const backing = domains.find((d) => (!d.wildcard && d.name === s.domain) || (d.wildcard && (s.domain || '').endsWith('.' + d.name.slice(2))));
          return (
            <div key={s.id} className="card row spread" style={{ padding: '11px 14px' }}>
              <div className="row wrap" style={{ minWidth: 0 }}>
                <Led status={s.status} label />
                <strong>{s.name}</strong>
                {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
                {s.quickTunnelUrl && <span className="badge neutral"><Cloud size={10} aria-hidden /> tunnel</span>}
                {backing && backing.status !== 'verified' && <span className="badge warn"><AlertTriangle size={10} aria-hidden /> DNS not verified</span>}
                {s.domain && ingress?.enabled && s.nodeId !== ingress.nodeId && <span className="badge neutral">via mesh relay</span>}
              </div>
              <Link to={`/apps/${s.id}/networking`} className="btn sm" style={{ flexShrink: 0 }}>
                Manage <ArrowRight size={11} aria-hidden />
              </Link>
            </div>
          );
        })}
        {!routesLoading && publicApps.length === 0 && (
          <EmptyState icon={<Globe size={18} />} title="Nothing is public yet">
            <div>Open any app's <strong>Networking</strong> tab to give it an instant URL, a tunnel, or a domain.</div>
          </EmptyState>
        )}
      </div>

      {/* Public access wizard */}
      {wizardOpen && diagnosis && (
        <Modal title="Get public access" icon={<Rocket size={15} aria-hidden />} width={680} onClose={() => setWizardOpen(false)}>
          <div className="dim" style={{ lineHeight: 1.7, marginBottom: 14 }}>
            For the internet to reach your apps, traffic needs <b>one publicly reachable landing point</b>.
            From there, the WireGuard mesh tunnels to every machine automatically — even behind CGNAT.
          </div>

          <div className="section-title" style={{ marginTop: 0 }}>Your machines right now</div>
          <div className="card inset" style={{ marginBottom: 14 }}>
            <div className="list">
              {(diagnosis.nodes ?? []).map((n) => (
                <div className="list-row spread" key={n.id}>
                  <span>{n.name} <span className="mono faint">{n.lanIp}{n.publicIp ? ` → ${n.publicIp}` : ''}</span></span>
                  {n.reachability === 'direct' && <span className="badge ok">publicly addressed</span>}
                  {n.reachability === 'nat' && <span className="badge warn">behind NAT/router</span>}
                  {n.reachability === 'unknown' && <span className="badge neutral">detecting…</span>}
                </div>
              ))}
            </div>
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
            On any app's <b>Networking</b> tab, hit <b><Cloud size={11} aria-hidden /> Start tunnel</b> — you get a free
            public <span className="mono">trycloudflare.com</span> URL in seconds, even behind CGNAT or a
            mobile hotspot. <span className="faint">The URL changes on every restart — great for demos, not production.</span>
          </div>
        </Modal>
      )}
    </div>
  );
}
