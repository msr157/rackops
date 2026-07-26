import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Globe, Shield, Waypoints, Boxes, AlertTriangle, Plus, Trash2, RefreshCw, ChevronDown,
  ChevronRight, Lock, Rocket, Cloud, ArrowRight, ArrowLeft, Route, SlidersHorizontal,
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

// Shape check only; the server does the real validation. This exists so a
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

// A section whose fetch failed says so, quietly, with a way back; a failed
// load must never wear the same face as "you have none".
function LoadError({ onRetry, children }: { onRetry: () => void; children: React.ReactNode }) {
  return (
    <div className="row" style={{ padding: '8px 0' }}>
      <AlertTriangle size={13} style={{ color: 'var(--warn)', flex: 'none' }} aria-hidden />
      <span className="dim">{children}</span>
      {/* type="button": this row can render inside the edge <form>. */}
      <button type="button" className="btn sm" style={{ marginLeft: 'auto' }} onClick={onRetry}>Retry</button>
    </div>
  );
}

type LoadState = 'loading' | 'ready' | 'error';

// The public-access wizard's three routes to a reachable landing point.
type WizChoice = 'forward' | 'vps' | 'tunnel';
const WIZ_OPTIONS: { id: WizChoice; title: string; sub: string }[] = [
  { id: 'forward', title: 'Port forward your router', sub: 'Needs a real public IP' },
  { id: 'vps', title: 'VPS anchor', sub: 'Works behind CGNAT, about $4 a month' },
  { id: 'tunnel', title: 'Quick tunnel', sub: 'Testing only, URL changes on restart' },
];

// NetworkPage: fleet-level connectivity, state first and configuration last.
// The strip answers "is anything public and healthy?", routes and domains show
// what exists, and the edge/mesh configuration lives at the bottom. Per-app
// routing lives on each app's Networking tab.
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
  const [acmeEmail, setAcmeEmail] = useState('');
  const [autoTls, setAutoTls] = useState(false);
  const [acmeError, setAcmeError] = useState('');
  const [ingressNode, setIngressNode] = useState('');
  const [httpPort, setHttpPort] = useState('80');
  const [httpsPort, setHttpsPort] = useState('443');
  const [busy, setBusy] = useState<false | 'apply' | 'disable'>(false);

  const [wizardOpen, setWizardOpen] = useState(false);
  const [wizStep, setWizStep] = useState<1 | 2>(1);
  const [wizChoice, setWizChoice] = useState<WizChoice>('vps');
  const [whyOpen, setWhyOpen] = useState(false);

  const [domainsState, setDomainsState] = useState<LoadState>('loading');
  const [ingressState, setIngressState] = useState<LoadState>('loading');
  const [meshState, setMeshState] = useState<LoadState>('loading');

  // The edge form belongs to the user the moment they touch any field (node,
  // ports, the HTTPS checkbox, the email): background reloads (settling poll,
  // SSE-triggered refreshes) must not stomp on half-typed edits. Cleared after
  // a successful apply or an explicit discard, when the server's copy becomes
  // the truth again. The ref mirrors the state so load() (stable callback)
  // reads the current value.
  const [touched, setTouched] = useState(false);
  const touchedRef = useRef(false);
  const markTouched = () => { touchedRef.current = true; setTouched(true); };
  const clearTouched = () => { touchedRef.current = false; setTouched(false); };

  const load = useCallback(() => {
    api.domains()
      .then((d) => { setDomains(d ?? []); setDomainsState('ready'); })
      .catch(() => setDomainsState((s) => (s === 'ready' ? s : 'error')));
    api.ingress()
      .then((i) => {
        setIngressCfg(i);
        setIngressState('ready');
        if (!touchedRef.current) {
          if (i.nodeId) setIngressNode(i.nodeId);
          if (i.httpPort) setHttpPort(String(i.httpPort));
          if (i.httpsPort) setHttpsPort(String(i.httpsPort));
          setAcmeEmail(i.acmeEmail ?? '');
          setAutoTls(!!i.acmeEmail);
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

  // With no saved choice, default the edge to the one machine the internet
  // can already reach; a default is not an edit, so it never marks the form
  // touched.
  useEffect(() => {
    if (touchedRef.current || ingressNode) return;
    if (ingressState !== 'ready' || ingress?.nodeId) return;
    const direct = (diagnosis?.nodes ?? []).filter((n) => n.reachability === 'direct');
    if (direct.length === 1) setIngressNode(direct[0].id);
  }, [diagnosis, ingress, ingressState, ingressNode]);

  const [addingDomain, setAddingDomain] = useState(false);
  const addDomain = async () => {
    const name = newDomain.trim();
    if (!name || addingDomain) return;
    if (!DOMAIN_SHAPE.test(name)) {
      setDomainError('A domain can only contain letters, digits, dots, and hyphens, like app.example.com or *.example.com.');
      return;
    }
    setAddingDomain(true);
    try {
      const d = await api.createDomain(name);
      setNewDomain('');
      setExpanded(d.id); // open the setup instructions right away
      toast.ok(`${d.name} added. Create the DNS record shown below.`);
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
        acmeEmail: autoTls ? acmeEmail.trim() : '',
      });
      toast.ok(enabled ? 'Edge deploying. Status updates live on this page.' : 'Edge disabled.');
      clearTouched();
      load();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  // Submitting the form is the only way changes reach the edge; Enter in any
  // field lands here, and the HTTPS checkbox refuses a silent empty email.
  const submitEdge = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (autoTls && !acmeEmail.trim()) {
      setAcmeError("Enter the email Let's Encrypt should send certificate notices to, or turn off Automatic HTTPS.");
      return;
    }
    applyIngress(true);
  };

  // Disabling the edge is not an app lifecycle action; it takes every
  // public route down with it, so it pauses for an explicit decision.
  const disableEdge = async () => {
    const routed = services.filter((s) => !!s.domain);
    const yes = await confirm({
      title: 'Disable the edge?',
      body: routed.length > 0
        ? `This kills every public route: ${routed.length} app${routed.length > 1 ? 's' : ''} stop${routed.length > 1 ? '' : 's'} answering at ${routed.length > 1 ? 'their domains' : 'its domain'} the moment the edge goes down. The apps keep running on your rack; they just become unreachable from the internet until you enable the edge again.`
        : 'This kills every public route through the edge. Nothing is routed through it right now, so nothing goes dark today, but domains will not work until you enable it again.',
      confirmLabel: 'Disable edge', danger: true,
    });
    if (yes) applyIngress(false);
  };

  // Discard puts the form back to the server's copy, exactly as load() would.
  const discardEdits = () => {
    setIngressNode(ingress?.nodeId ?? '');
    setHttpPort(String(ingress?.httpPort || 80));
    setHttpsPort(String(ingress?.httpsPort || 443));
    setAcmeEmail(ingress?.acmeEmail ?? '');
    setAutoTls(!!ingress?.acmeEmail);
    setAcmeError('');
    clearTouched();
  };

  // TLS state the domain checks report against: what the server has applied,
  // not what's half-edited in the form.
  const tlsOn = !!ingress?.acmeEmail;
  const edgeReady = ingress?.enabled && ingress.status === 'running';
  // A deploy is in flight until the edge lands on running or failed; the
  // settling poll above keeps `ingress` fresh, so this self-clears.
  const edgeDeploying = !!ingress?.enabled && ingress.status !== 'running' && ingress.status !== 'failed';
  const lanOnly = diagnosis?.lanOnly ?? false;
  const publicApps = services.filter((s) => s.domain || s.quickTunnelUrl);
  const routesLoading = !loaded || ingressState === 'loading';

  // Dirty means: the user touched the form AND its values differ from the
  // saved config. The banner only appears against a live edge; on first
  // enable the form is simply new, not drifted.
  const savedAcme = ingress?.acmeEmail ?? '';
  const effAcme = autoTls ? acmeEmail.trim() : '';
  const edgeDirty = touched && (
    ingressNode !== (ingress?.nodeId ?? '')
    || httpPort !== String(ingress?.httpPort || 80)
    || httpsPort !== String(ingress?.httpsPort || 443)
    || effAcme !== savedAcme
  );

  // Wizard preselection: any directly reachable machine means port forwarding
  // will work; otherwise a VPS anchor is the reliable path.
  const recommended: WizChoice = (diagnosis?.nodes ?? []).some((n) => n.reachability === 'direct') ? 'forward' : 'vps';
  const openWizard = () => {
    setWizChoice(recommended);
    setWizStep(1);
    setWhyOpen(false);
    setWizardOpen(true);
  };
  const closeWizard = () => {
    setWizardOpen(false);
    setWizStep(1);
    setWhyOpen(false);
  };
  const firstApp = services[0];

  // How each machine is reachable, for annotating the edge's node picker.
  const reachability = new Map((diagnosis?.nodes ?? []).map((n) => [n.id, n.reachability] as const));

  return (
    <div className="page">
      <div className="page-title">Network</div>
      <div className="page-sub">How the internet reaches your rack: public routes, domains, and the edge</div>

      {/* One-line answer before anything else: edge, mesh, and what's live. */}
      <div className="card row wrap" style={{ padding: '10px 14px', marginBottom: 16, gap: 10 }}>
        {ingressState === 'loading' ? <Skeleton width={90} height={20} radius={20} />
          : ingressState === 'error' ? <span className="badge neutral">edge status unknown</span>
          : !ingress?.enabled ? <span className="badge neutral">edge off</span>
          : ingress.status === 'running' ? <span className="badge ok"><Led status="running" /> edge running on {nodeName(ingress.nodeId)}</span>
          : ingress.status === 'failed' ? <span className="badge crit"><AlertTriangle size={11} aria-hidden /> edge failed</span>
          : <span className="badge warn"><Led status="pending" /> edge starting…</span>}
        {meshState === 'loading' ? <Skeleton width={74} height={20} radius={20} />
          : meshState === 'error' ? <span className="badge neutral">mesh status unknown</span>
          : meshStatus?.enabled ? <span className="badge ok">mesh active</span>
          : <span className="badge neutral">mesh off</span>}
        {loaded
          ? <span className="dim">{publicApps.length} route{publicApps.length === 1 ? '' : 's'} live</span>
          : <Skeleton width={70} height={14} />}
      </div>

      {lanOnly && (
        <div className="card warn row spread" style={{ marginBottom: 16 }}>
          <div>
            <strong className="row" style={{ gap: 7 }}><AlertTriangle size={13} style={{ color: 'var(--warn)' }} aria-hidden /> Your apps are LAN-only right now</strong>
            <div className="dim" style={{ marginTop: 4 }}>
              No machine here holds a public IP, so domains and instant URLs only work inside your network.
            </div>
          </div>
          <button className="btn" style={{ flex: 'none' }} disabled={!diagnosis} onClick={openWizard}>
            <Rocket size={13} aria-hidden /> Get public access
          </button>
        </div>
      )}

      {/* Public routes: the state of the world, first. */}
      <div className="section-title" style={{ marginTop: 20 }}><Route size={15} aria-hidden /> Public routes</div>
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
                placeholder="app.example.com (or *.example.com for per-app subdomains)"
                value={newDomain}
                onChange={(e) => { setNewDomain(e.target.value); if (domainError) setDomainError(''); }}
                onKeyDown={(e) => e.key === 'Enter' && addDomain()} />
              <button className="btn" disabled={addingDomain} onClick={addDomain}>
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
                    {/* Fixed width so the label swap to "Checking…" doesn't jitter the row. */}
                    <button className="btn sm" style={{ minWidth: 104, flex: 'none' }} disabled={checking === d.id}
                      aria-label={`Verify ${d.name} now`} onClick={() => verify(d.id)}>
                      <RefreshCw size={11} className={checking === d.id ? 'spin' : ''} aria-hidden />
                      {checking === d.id ? 'Checking…' : 'Verify now'}
                    </button>
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
                            ? 'your edge answers at that address, traffic will route'
                            : edgeReady ? 'edge did not answer there (port forwarding / firewall / another server on the port)' : 'enable the edge to check'}</span>
                        </div>
                        {tlsOn && !d.wildcard && (
                          <div className="row" style={{ gap: 8 }}>
                            <Led status={d.certOk ? 'ok' : 'pending'} />
                            <strong>Certificate</strong>
                            <span className="dim">{d.certOk
                              ? `issued by ${d.certIssuer}${d.certExpiry ? `, expires ${new Date(d.certExpiry).toLocaleDateString()}` : ''}`
                              : 'not issued yet: needs DNS verified, port 80 reachable from the internet, then an app routed to it'}</span>
                          </div>
                        )}
                        {tlsOn && d.wildcard && (
                          <div className="faint"><Lock size={10} aria-hidden /> Certificates issue per subdomain automatically as apps are routed.</div>
                        )}
                        {d.lastChecked && <div className="faint">last checked {new Date(d.lastChecked).toLocaleTimeString()} (rechecks every 30s until verified, then hourly)</div>}
                      </div>

                      {/* The one destructive action lives at the bottom of the
                          expanded panel, labeled, away from Verify. */}
                      <hr className="divider" />
                      <div className="row">
                        <button className="btn sm danger" style={{ marginLeft: 'auto' }} disabled={removing === d.id}
                          onClick={() => removeDomain(d)}>
                          {removing === d.id ? <Spinner size={12} /> : <Trash2 size={12} aria-hidden />} Remove domain
                        </button>
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
            No domains yet. Add one and you'll get the exact DNS record to create, or add a wildcard
            like <span className="mono">*.example.com</span> once and every app can have its
            own <span className="mono">name.example.com</span>.
          </div>
        )}
      </div>

      {/* Configuration: the edge form, mesh, and control plane, after the state. */}
      <div className="section-title" id="network-config"><SlidersHorizontal size={15} aria-hidden /> Configuration</div>
      <div className="grid-2">
        {/* Edge config */}
        <form className="card" onSubmit={submitEdge}>
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
            One Traefik instance routes every domain and instant URL to the right app, across machines, over the mesh.
            <InfoTip wide>
              Pick the machine the internet can reach (a public IP or a forwarded port). Apps on
              other machines are still reachable through it: traffic rides the encrypted mesh.
              Automatic HTTPS issues Let&rsquo;s Encrypt certificates for your verified domains.
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
                    onChange={(e) => { markTouched(); setIngressNode(e.target.value); }}>
                    <option value="">Select a machine</option>
                    {nodes.map((n) => {
                      const r = reachability.get(n.id);
                      const note = r === 'direct' ? 'public IP, recommended'
                        : r === 'nat' ? 'behind NAT'
                        : (n.publicIp || n.ip);
                      return <option key={n.id} value={n.id}>{n.name} ({note})</option>;
                    })}
                  </select>
                )}
              </Field>
              <div className="grid-2">
                <Field label="HTTP port">
                  {(p) => (
                    <input className="input mono" {...p} inputMode="numeric" value={httpPort}
                      onChange={(e) => { markTouched(); setHttpPort(e.target.value.replace(/\D/g, '')); }} />
                  )}
                </Field>
                <Field label="HTTPS port">
                  {(p) => (
                    <input className="input mono" {...p} inputMode="numeric" value={httpsPort}
                      onChange={(e) => { markTouched(); setHttpsPort(e.target.value.replace(/\D/g, '')); }} />
                  )}
                </Field>
              </div>
              <label className="check" style={{ marginBottom: autoTls ? 10 : 14 }}>
                <input type="checkbox" checked={autoTls}
                  onChange={(e) => { markTouched(); setAutoTls(e.target.checked); if (!e.target.checked) setAcmeError(''); }} />
                Automatic HTTPS (Let's Encrypt)
              </label>
              {autoTls && (
                <Field label="Certificate email" error={acmeError || undefined}
                  hint={acmeError ? undefined
                    : `Certificates issue automatically. Port ${httpPort || '80'} must be reachable from the internet; HTTP redirects to HTTPS.`}>
                  {(p) => (
                    <input className="input" {...p} type="email" placeholder="you@example.com" value={acmeEmail}
                      onChange={(e) => { markTouched(); setAcmeEmail(e.target.value); if (acmeError) setAcmeError(''); }} />
                  )}
                </Field>
              )}

              {/* One apply intent at a time: while a live edge has unapplied
                  edits, the dirty banner carries Update + Discard; otherwise
                  the plain action row does. */}
              {ingress?.enabled && edgeDirty && !edgeDeploying ? (
                <div className="dirty-banner" style={{ margin: 0 }}>
                  <span>Unapplied changes. The running edge still uses the last applied settings.</span>
                  <span className="row" style={{ gap: 8 }}>
                    <button type="button" className="btn" disabled={!!busy} onClick={discardEdits}>Discard</button>
                    <button type="submit" className="btn primary" disabled={!!busy || !ingressNode}>
                      {busy === 'apply' && <Spinner size={13} />} Update edge
                    </button>
                  </span>
                </div>
              ) : (
                <div className="row">
                  <button type="submit" className="btn primary"
                    disabled={!!busy || edgeDeploying || !ingressNode || (!!ingress?.enabled && !edgeDirty)}
                    title={ingress?.enabled && !edgeDirty && !edgeDeploying ? 'No changes to apply yet' : undefined}>
                    {busy === 'apply' || edgeDeploying ? <Spinner size={13} /> : null}
                    {edgeDeploying ? 'Deploying…' : ingress?.enabled ? 'Update edge' : 'Enable edge'}
                  </button>
                </div>
              )}

              {ingress?.enabled && (
                <>
                  <hr className="divider" />
                  <div className="row">
                    <span className="faint">Disabling takes every public route down; apps keep running.</span>
                    <button type="button" className="btn danger" style={{ marginLeft: 'auto', flex: 'none' }}
                      disabled={!!busy || edgeDeploying} onClick={disableEdge}>
                      {busy === 'disable' && <Spinner size={13} />} Disable edge
                    </button>
                  </div>
                </>
              )}
            </>
          )}
        </form>

        {/* Mesh */}
        <div className="card">
          <div className="section-title" style={{ margin: '0 0 12px' }}>
            <Waypoints size={15} aria-hidden /> WireGuard mesh
            <InfoTip wide>
              An encrypted private network between your machines. Each machine dials out to the
              server, so it works behind CGNAT with zero port forwarding; the edge proxy uses it
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
                Machines dial out, so it works behind CGNAT with no port forwarding.
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
        </div>

        {/* Control plane (HA): its own card, not a footnote of the mesh. */}
        {cluster?.enabled && (
          <div className="card">
            <div className="section-title" style={{ margin: '0 0 12px' }}>
              <Boxes size={15} aria-hidden /> Control plane (HA)
            </div>
            <div className="dim" style={{ marginBottom: 10 }}>
              Server replicas keeping the control plane available; one leads at a time.
            </div>
            <div className="list">
              {(cluster.peers ?? []).map((p) => (
                <div className="list-row spread" key={p.id}>
                  <span className="mono" style={{ fontSize: 12 }}>{p.id} <span className="faint">{p.addr}</span></span>
                  <span className={`badge ${p.role === 'leader' ? 'accent' : 'neutral'}`}>{p.role}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Public access wizard: pick a route, then follow only that procedure. */}
      {wizardOpen && diagnosis && (
        <Modal title="Get public access" icon={<Rocket size={15} aria-hidden />} width={640} onClose={closeWizard}>
          {wizStep === 1 ? (
            <>
              <div className="dim" style={{ lineHeight: 1.7, marginBottom: 14 }}>
                For the internet to reach your apps, traffic needs <b>one publicly reachable landing point</b>.
                From there, the WireGuard mesh tunnels to every machine automatically, even behind CGNAT.
              </div>
              <div role="radiogroup" aria-label="How traffic reaches your rack" className="stack" style={{ gap: 8 }}>
                {WIZ_OPTIONS.map((o) => (
                  <label key={o.id} className={`card clickable ${wizChoice === o.id ? 'accent' : ''}`}
                    style={{
                      display: 'flex', alignItems: 'flex-start', gap: 10, padding: '12px 14px', cursor: 'pointer',
                      background: wizChoice === o.id ? 'var(--accent-dim)' : undefined,
                    }}>
                    <input type="radio" name="public-access" checked={wizChoice === o.id}
                      style={{ accentColor: 'var(--accent-strong)', marginTop: 3 }}
                      onChange={() => setWizChoice(o.id)} />
                    <span style={{ minWidth: 0 }}>
                      <span className="row wrap" style={{ gap: 8 }}>
                        <strong>{o.title}</strong>
                        {recommended === o.id && <span className="badge accent">Recommended for your network</span>}
                      </span>
                      <span className="faint" style={{ display: 'block', marginTop: 2 }}>{o.sub}</span>
                    </span>
                  </label>
                ))}
              </div>
              <button type="button" className="btn sm ghost" style={{ marginTop: 10 }}
                aria-expanded={whyOpen} aria-label="Why this recommendation"
                onClick={() => setWhyOpen((v) => !v)}>
                {whyOpen ? <ChevronDown size={13} aria-hidden /> : <ChevronRight size={13} aria-hidden />} Why?
              </button>
              {whyOpen && (
                <div className="card inset" style={{ marginTop: 8 }}>
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
              )}
              <div className="row" style={{ marginTop: 16, justifyContent: 'flex-end' }}>
                <button className="btn primary" onClick={() => setWizStep(2)}>Next: setup steps</button>
              </div>
            </>
          ) : (
            <>
              {wizChoice === 'forward' && (
                <>
                  <div className="section-title" style={{ marginTop: 0 }}>Step 2: port forward your router</div>
                  <div className="card inset dim" style={{ lineHeight: 1.8 }}>
                    <ol style={{ margin: 0, paddingLeft: 18 }}>
                      <li>In your router, forward TCP <b>80</b> and <b>443</b> to the edge machine's LAN IP.</li>
                      <li>In the Configuration section at the bottom of this page, set the edge's HTTP/HTTPS ports to 80 and 443.</li>
                      <li>Domains then point at your public IP; instant URLs use it automatically.</li>
                    </ol>
                    <div className="faint" style={{ marginTop: 8 }}>
                      Does not work behind CGNAT. If your router's WAN IP differs from the detected public IP, go back and pick the VPS anchor.
                    </div>
                  </div>
                </>
              )}
              {wizChoice === 'vps' && (
                <>
                  <div className="section-title" style={{ marginTop: 0 }}>Step 2: set up a VPS anchor</div>
                  <div className="card inset dim" style={{ lineHeight: 1.8 }}>
                    <ol style={{ margin: 0, paddingLeft: 18 }}>
                      <li>Get any small VPS with a public IP (Hetzner, DigitalOcean, Oracle free tier).</li>
                      <li>
                        Run the control plane there:
                        <div style={{ margin: '6px 0' }}><CopyField text="rackops server --listen :8080" /></div>
                      </li>
                      <li>
                        Join this machine to it (dials out, no port forwarding needed):
                        <div style={{ margin: '6px 0' }}><CopyField text={diagnosis.joinCommand} /></div>
                      </li>
                      <li>On the VPS dashboard, enable the <b>edge on the VPS machine</b> (ports 80/443).</li>
                      <li>Your apps stay home; traffic flows VPS → encrypted mesh → here, automatically.</li>
                    </ol>
                  </div>
                </>
              )}
              {wizChoice === 'tunnel' && (
                <>
                  <div className="section-title" style={{ marginTop: 0 }}>Step 2: start a quick tunnel</div>
                  <div className="card inset dim" style={{ lineHeight: 1.8 }}>
                    <ol style={{ margin: 0, paddingLeft: 18 }}>
                      <li>Open any app's <b>Networking</b> tab.</li>
                      <li>
                        Hit <b><Cloud size={11} aria-hidden /> Start tunnel</b>: a free
                        public <span className="mono">trycloudflare.com</span> URL appears in seconds, even behind
                        CGNAT or a mobile hotspot.
                      </li>
                    </ol>
                    <div className="faint" style={{ marginTop: 8 }}>The URL changes on every restart: great for demos, not production.</div>
                  </div>
                </>
              )}
              <div className="row" style={{ marginTop: 16 }}>
                <button className="btn" onClick={() => setWizStep(1)}><ArrowLeft size={13} aria-hidden /> Back</button>
                <span style={{ marginLeft: 'auto' }}>
                  {wizChoice === 'forward' && (
                    <button className="btn primary"
                      onClick={() => { closeWizard(); document.getElementById('network-config')?.scrollIntoView(); }}>
                      Open edge settings
                    </button>
                  )}
                  {wizChoice === 'vps' && (
                    <button className="btn primary" onClick={closeWizard}>Done</button>
                  )}
                  {wizChoice === 'tunnel' && (firstApp ? (
                    <Link className="btn primary" to={`/apps/${firstApp.id}/networking`} onClick={closeWizard}>
                      Open {firstApp.name}'s Networking tab
                    </Link>
                  ) : (
                    <button className="btn primary" onClick={closeWizard}>Done</button>
                  ))}
                </span>
              </div>
            </>
          )}
        </Modal>
      )}
    </div>
  );
}
