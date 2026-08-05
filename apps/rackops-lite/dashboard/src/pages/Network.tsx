import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Globe, Shield, Waypoints, Boxes, AlertTriangle, Plus, Trash2, RefreshCw, ChevronDown,
  ChevronRight, Lock, Rocket, Cloud, ArrowRight, ArrowLeft, Route, Network,
} from 'lucide-react';
import { api } from '../api';
import type { Domain, IngressConfig, MeshStatus, ClusterStatus, NetworkDiagnosis } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import {
  Crumbs, EmptyState, Field, InfoTip, Led, CopyBtn, CopyField, Modal, Pill, RowMenu, Skeleton,
  SkeletonRows, Spinner, Switch, UrlChip, UsageBar, formatBytes, publicUrls,
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
    case 'verified': return <Pill status="verified" />;
    case 'misconfigured': return <Pill status="misconfigured" />;
    default: return <Pill status="pending">pending DNS</Pill>;
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

// The public-access wizard's routes to a reachable landing point. Front Door
// leads: it is the only one that needs nothing from the user's network, so a
// machine behind CGNAT gets a one-click answer here instead of a homework
// assignment involving a router or a rented VPS.
type WizChoice = 'frontdoor' | 'forward' | 'vps' | 'tunnel';
const WIZ_OPTIONS: { id: WizChoice; title: string; sub: string }[] = [
  { id: 'frontdoor', title: 'RackOps Front Door', sub: 'Permanent HTTPS address, works behind CGNAT, nothing to configure' },
  { id: 'forward', title: 'Port forward your router', sub: 'Needs a real public IP' },
  { id: 'vps', title: 'VPS anchor', sub: 'Works behind CGNAT, about $4 a month' },
  { id: 'tunnel', title: 'Quick tunnel', sub: 'Testing only, URL changes on restart' },
];

// NetworkPage: fleet-level connectivity, state first and configuration last.
// The status stripes answer "is anything public and healthy?", routes and
// domains show what exists, and the edge/mesh configuration lives at the
// bottom. Per-app routing lives on each app's Networking tab.
export function NetworkPage() {
  const { nodes, services, nodeName, frontDoor, refresh, loaded } = useData();
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
  // saved config. The apply notice only appears against a live edge; on first
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
  // A machine with a real public IP is best served by port forwarding: it is
  // free and keeps traffic off anyone else's network. Everything else — CGNAT,
  // unknown, mobile — gets Front Door when this install has one available,
  // falling back to the VPS anchor when it does not.
  const recommended: WizChoice = (diagnosis?.nodes ?? []).some((n) => n.reachability === 'direct')
    ? 'forward'
    : (frontDoor?.popAddr || frontDoor?.enabled ? 'frontdoor' : 'vps');
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

  // One pill per fact, shared between the status stripes and the section
  // headers: color never travels without the word.
  const edgePill = ingressState === 'loading' ? <Skeleton width={90} height={20} radius={20} />
    : ingressState === 'error' ? <Pill status="unknown">status unknown</Pill>
    : !ingress?.enabled ? <Pill status="off" />
    : ingress.status === 'running' ? <Pill status="running">running on {nodeName(ingress.nodeId)}</Pill>
    : ingress.status === 'failed' ? <Pill status="failed" />
    : <Pill status="starting">starting…</Pill>;
  const meshPill = meshState === 'loading' ? <Skeleton width={74} height={20} radius={20} />
    : meshState === 'error' ? <Pill status="unknown">status unknown</Pill>
    : meshStatus?.enabled ? <Pill status="ok">active</Pill>
    : <Pill status="off" />;

  return (
    <div className="page">
      <Crumbs items={[{ label: 'Network', icon: <Network size={13} aria-hidden /> }]} />
      <div className="page-title">Network</div>
      <div className="page-sub">How the internet reaches your rack: public routes, domains, and the edge</div>

      {/* One-glance answer before anything else: edge, mesh, and what's live. */}
      <div className="kv-rows">
        <div className="kv-row">
          <span className="kv-label"><Shield size={14} aria-hidden /> Edge proxy</span>
          <span className="kv-val">{edgePill}</span>
        </div>
        <div className="kv-row">
          <span className="kv-label"><Waypoints size={14} aria-hidden /> WireGuard mesh</span>
          <span className="kv-val">{meshPill}</span>
        </div>
        <div className="kv-row">
          <span className="kv-label"><Route size={14} aria-hidden /> Public routes</span>
          <span className="kv-val">
            {loaded
              ? <Pill status={publicApps.length > 0 ? 'ok' : 'off'}>{publicApps.length} live</Pill>
              : <Skeleton width={70} height={20} radius={20} />}
          </span>
        </div>
        {cluster?.enabled && (
          <div className="kv-row">
            <span className="kv-label"><Boxes size={14} aria-hidden /> Control plane (HA)</span>
            <span className="kv-val">
              <Pill status="ok">{(cluster.peers ?? []).length} replica{(cluster.peers ?? []).length === 1 ? '' : 's'}</Pill>
            </span>
          </div>
        )}
      </div>

      {lanOnly && (
        <div className="notice warn" style={{ marginTop: 16, marginBottom: 0 }}>
          <AlertTriangle size={14} style={{ color: 'var(--warn)', flex: 'none' }} aria-hidden />
          <span style={{ minWidth: 0 }}>
            <strong>Your apps are LAN-only right now.</strong>{' '}
            <span className="dim">No machine here holds a public IP, so domains and instant URLs only work inside your network.</span>
          </span>
          <button className="btn" style={{ marginLeft: 'auto', flex: 'none' }} disabled={!diagnosis} onClick={openWizard}>
            <Rocket size={13} aria-hidden /> Get public access
          </button>
        </div>
      )}

      {/* Public routes: the state of the world, first. */}
      <div className="section-title"><Route size={15} aria-hidden /> Public routes</div>
      <div className="dim" style={{ marginBottom: 12 }}>
        Every app the internet can reach, and the address it answers at.
      </div>
      {routesLoading && <SkeletonRows count={2} height={44} />}
      {!routesLoading && publicApps.length > 0 && (
        <div className="list">
          {publicApps.map((s) => {
            const urls = publicUrls(s, ingress);
            const backing = domains.find((d) => (!d.wildcard && d.name === s.domain) || (d.wildcard && (s.domain || '').endsWith('.' + d.name.slice(2))));
            return (
              <div key={s.id} className="list-row">
                <span className="row wrap" style={{ minWidth: 0 }}>
                  <Led status={s.status} label />
                  <strong>{s.name}</strong>
                  {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
                  {s.quickTunnelUrl && <span className="badge neutral"><Cloud size={10} aria-hidden /> tunnel</span>}
                  {backing && backing.status !== 'verified' && <Pill status="pending">DNS not verified</Pill>}
                  {s.domain && ingress?.enabled && s.nodeId !== ingress.nodeId && <span className="badge neutral">via mesh relay</span>}
                </span>
                <Link to={`/apps/${s.id}/networking`} className="btn sm" style={{ marginLeft: 'auto', flexShrink: 0 }}>
                  Manage <ArrowRight size={11} aria-hidden />
                </Link>
              </div>
            );
          })}
        </div>
      )}
      {!routesLoading && publicApps.length === 0 && (
        <EmptyState icon={<Globe size={18} />} title="Nothing is public yet">
          <div>Open any app's <strong>Networking</strong> tab to give it an instant URL, a tunnel, or a domain.</div>
        </EmptyState>
      )}

      {/* Domains */}
      <div className="section-title">
        <Globe size={15} aria-hidden /> Domains
        <InfoTip wide>
          Add a domain you own and RackOps shows the exact DNS record to create at your registrar,
          then keeps verifying DNS, routing, and the HTTPS certificate. A wildcard
          (<code>*.example.com</code>) gives every app its own subdomain from one record.
        </InfoTip>
      </div>
      <div className="dim" style={{ marginBottom: 12 }}>
        Domains you own, pointed at the edge and verified continuously.
      </div>
      <div className="toolbar" style={{ alignItems: 'flex-end' }}>
        <Field label="Add a domain" error={domainError || undefined} style={{ marginBottom: 0, flex: 1, maxWidth: 480 }}>
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
      </div>

      {domainsState === 'loading' && <SkeletonRows count={2} height={40} />}
      {domainsState === 'error' && (
        <LoadError onRetry={() => { setDomainsState('loading'); load(); }}>
          Your domains didn&rsquo;t load.
        </LoadError>
      )}
      {domainsState === 'ready' && domains.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <caption className="sr-only">Domains with verification and certificate status</caption>
            <thead>
              <tr>
                <th>Domain</th>
                <th>Status</th>
                <th>Cert</th>
                <th style={{ width: 48 }}><span className="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              {domains.map((d) => {
                const open = expanded === d.id;
                const panelId = `domain-${d.id}`;
                return (
                  <Fragment key={d.id}>
                    <tr>
                      <td>
                        <button type="button" className="row disclosure" style={{ gap: 8 }}
                          aria-expanded={open} aria-controls={panelId}
                          onClick={() => setExpanded(open ? '' : d.id)}>
                          {open ? <ChevronDown size={13} aria-hidden /> : <ChevronRight size={13} aria-hidden />}
                          <span className="mono" style={{ fontWeight: 500 }}>{d.name}</span>
                          {d.wildcard && <span className="badge neutral">wildcard</span>}
                        </button>
                        <span className="cell-sub mono" style={{ paddingLeft: 21 }}>
                          {d.expectedIp ? `A → ${d.expectedIp}` : 'no target IP yet'}
                        </span>
                      </td>
                      <td>
                        <span className="row" style={{ gap: 8 }}>
                          <DomainBadge d={d} />
                          {checking === d.id && <Spinner size={12} />}
                        </span>
                      </td>
                      <td>
                        {d.certOk ? <Pill status="ok">TLS</Pill>
                          : !tlsOn ? <span className="faint">HTTPS off</span>
                          : d.wildcard ? <span className="dim">per subdomain</span>
                          : <span className="dim">not issued</span>}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        {removing === d.id ? <Spinner size={14} /> : (
                          <RowMenu label={`Actions for ${d.name}`} items={[
                            {
                              label: checking === d.id ? 'Checking…' : 'Verify now',
                              icon: <RefreshCw size={13} aria-hidden />,
                              disabled: checking === d.id,
                              onSelect: () => verify(d.id),
                            },
                            {
                              label: open ? 'Hide DNS setup' : 'DNS setup',
                              icon: open ? <ChevronDown size={13} aria-hidden /> : <ChevronRight size={13} aria-hidden />,
                              onSelect: () => setExpanded(open ? '' : d.id),
                            },
                            {
                              label: 'Remove domain',
                              icon: <Trash2 size={13} aria-hidden />,
                              danger: true,
                              onSelect: () => removeDomain(d),
                            },
                          ]} />
                        )}
                      </td>
                    </tr>

                    {open && (
                      <tr id={panelId}>
                        <td colSpan={4}>
                          <div className="dim" style={{ margin: '2px 0 10px' }}>
                            Create this record at your registrar (GoDaddy, Namecheap, Cloudflare DNS, …):
                          </div>
                          <div className="kv-rows" style={{ maxWidth: 560, marginBottom: 14 }}>
                            <div className="kv-row">
                              <span className="kv-label">Type</span>
                              <span className="kv-val mono">A</span>
                            </div>
                            <div className="kv-row">
                              <span className="kv-label">Host / Name</span>
                              <span className="kv-val mono">{recordHost(d.name)}</span>
                              <CopyBtn text={recordHost(d.name)} />
                            </div>
                            <div className="kv-row">
                              <span className="kv-label">Value / Points to</span>
                              <span className="kv-val mono">{d.expectedIp || '(enable the edge first)'}</span>
                              {d.expectedIp ? <CopyBtn text={d.expectedIp} /> : null}
                            </div>
                            <div className="kv-row">
                              <span className="kv-label">TTL</span>
                              <span className="kv-val mono">300</span>
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
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {domainsState === 'ready' && domains.length === 0 && (
        <EmptyState icon={<Globe size={18} />} title="No domains yet">
          <div>
            Add one above and you'll get the exact DNS record to create, or add a wildcard
            like <span className="mono">*.example.com</span> once and every app can have its
            own <span className="mono">name.example.com</span>.
          </div>
        </EmptyState>
      )}

      {/* Front Door sits above the edge form: it is the path most installs
          should take, and the edge below it is an implementation detail it
          manages on their behalf. */}
      <FrontDoorSection />

      {/* Configuration, after the state: the edge form, then mesh, then HA. */}
      <div className="section-title" id="network-config">
        <Shield size={15} aria-hidden /> Edge proxy
        <span style={{ marginLeft: 'auto' }}>{ingressState === 'error' ? null : edgePill}</span>
      </div>
      <div className="dim" style={{ marginBottom: 14 }}>
        One Traefik instance routes every domain and instant URL to the right app, across machines, over the mesh.
        <InfoTip wide>
          Pick the machine the internet can reach (a public IP or a forwarded port). Apps on
          other machines are still reachable through it: traffic rides the encrypted mesh.
          Automatic HTTPS issues Let&rsquo;s Encrypt certificates for your verified domains.
        </InfoTip>
      </div>
      <form onSubmit={submitEdge} style={{ maxWidth: 560 }}>
        {ingressState === 'loading' && <SkeletonRows count={4} height={36} />}
        {ingressState === 'error' && (
          <LoadError onRetry={() => { setIngressState('loading'); load(); }}>
            The edge configuration didn&rsquo;t load.
          </LoadError>
        )}
        {ingressState === 'ready' && (
          <>
            {ingress?.status === 'failed' && ingress.statusMsg && (
              <div className="notice crit" style={{ whiteSpace: 'pre-wrap', fontSize: 12.5, lineHeight: 1.6 }}>
                <AlertTriangle size={14} style={{ color: 'var(--crit)', flex: 'none' }} aria-hidden />
                <span style={{ minWidth: 0, flex: 1 }}>{ingress.statusMsg}</span>
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
                edits, the warn notice carries Update + Discard; otherwise
                the plain action row does. */}
            {ingress?.enabled && edgeDirty && !edgeDeploying ? (
              <div className="notice warn" style={{ margin: 0 }}>
                <span style={{ minWidth: 0 }}>Unapplied changes. The running edge still uses the last applied settings.</span>
                <span className="row" style={{ gap: 8, marginLeft: 'auto' }}>
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
      <div className="section-title">
        <Waypoints size={15} aria-hidden /> WireGuard mesh
        <InfoTip wide>
          An encrypted private network between your machines. Each machine dials out to the
          server, so it works behind CGNAT with zero port forwarding; the edge proxy uses it
          to reach apps on other machines.
        </InfoTip>
        <span style={{ marginLeft: 'auto' }}>{meshState === 'error' ? null : meshPill}</span>
      </div>
      {meshState === 'loading' && <SkeletonRows count={3} height={28} />}
      {meshState === 'error' && (
        <LoadError onRetry={() => { setMeshState('loading'); load(); }}>
          The mesh status didn&rsquo;t load.
        </LoadError>
      )}
      {meshState === 'ready' && (meshStatus?.enabled ? (
        <>
          <div className="dim" style={{ marginBottom: 12, lineHeight: 1.7 }}>
            Hub <span className="mono">{meshStatus.hubIp}</span> on UDP <span className="mono">:{meshStatus.udpPort}</span>.
            Machines dial out, so it works behind CGNAT with no port forwarding.
          </div>
          {(meshStatus.peers ?? []).length > 0 ? (
            <div className="kv-rows" style={{ maxWidth: 560 }}>
              {(meshStatus.peers ?? []).map((p) => (
                <div className="kv-row" key={p.nodeId}>
                  <span className="kv-label">{nodeName(p.nodeId)}</span>
                  <span className="kv-val mono">{p.meshIp}</span>
                </div>
              ))}
            </div>
          ) : <div className="faint">No machines have joined the mesh yet.</div>}
        </>
      ) : <div className="faint">Mesh is disabled (start the server with a mesh port).</div>)}

      {/* Control plane (HA): its own section, not a footnote of the mesh. */}
      {cluster?.enabled && (
        <>
          <div className="section-title"><Boxes size={15} aria-hidden /> Control plane (HA)</div>
          <div className="dim" style={{ marginBottom: 12 }}>
            Server replicas keeping the control plane available; one leads at a time.
          </div>
          <div className="kv-rows" style={{ maxWidth: 560 }}>
            {(cluster.peers ?? []).map((p) => (
              <div className="kv-row" key={p.id}>
                <span className="kv-label mono" style={{ fontSize: 12 }}>{p.id}</span>
                <span className="mono faint">{p.addr}</span>
                <span className="kv-val">
                  <span className={`badge ${p.role === 'leader' ? 'accent' : 'neutral'}`}>{p.role}</span>
                </span>
              </div>
            ))}
          </div>
        </>
      )}

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
                      style={{ accentColor: 'var(--accent)', marginTop: 3 }}
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
                <div className="list" style={{ marginTop: 8 }}>
                  {(diagnosis.nodes ?? []).map((n) => (
                    <div className="list-row spread" key={n.id}>
                      <span>{n.name} <span className="mono faint">{n.lanIp}{n.publicIp ? ` → ${n.publicIp}` : ''}</span></span>
                      {n.reachability === 'direct' && <Pill status="ok">publicly addressed</Pill>}
                      {n.reachability === 'nat' && <Pill status="pending">behind NAT/router</Pill>}
                      {n.reachability === 'unknown' && <Pill status="unknown">detecting…</Pill>}
                    </div>
                  ))}
                </div>
              )}
              <div className="row" style={{ marginTop: 16, justifyContent: 'flex-end' }}>
                <button className="btn primary" onClick={() => setWizStep(2)}>Next: setup steps</button>
              </div>
            </>
          ) : (
            <>
              {wizChoice === 'frontdoor' && (
                <>
                  <div className="section-title" style={{ marginTop: 0 }}>Step 2: turn Front Door on</div>
                  <div className="dim" style={{ lineHeight: 1.8 }}>
                    <ol style={{ margin: 0, paddingLeft: 18 }}>
                      <li>Turn on <b>Front Door</b> in the section below and pick a workspace name.</li>
                      <li>Open any app&rsquo;s <b>Networking</b> tab and switch Front Door on for it.</li>
                      <li>
                        You get a permanent address like{' '}
                        <span className="mono">yourapp-{frontDoor?.workspace || 'workspace'}.{frontDoor?.zone || 'link.rackops.in'}</span>{' '}
                        with real HTTPS.
                      </li>
                    </ol>
                    <div className="faint" style={{ marginTop: 8 }}>
                      Nothing to open on your router and no DNS to edit — this machine dials out.
                      Free tier includes {formatBytes(frontDoor?.quotaBytes || 100 * 1024 ** 3)} of traffic a month.
                    </div>
                  </div>
                </>
              )}
              {wizChoice === 'forward' && (
                <>
                  <div className="section-title" style={{ marginTop: 0 }}>Step 2: port forward your router</div>
                  <div className="dim" style={{ lineHeight: 1.8 }}>
                    <ol style={{ margin: 0, paddingLeft: 18 }}>
                      <li>In your router, forward TCP <b>80</b> and <b>443</b> to the edge machine's LAN IP.</li>
                      <li>In the Edge proxy section at the bottom of this page, set the HTTP/HTTPS ports to 80 and 443.</li>
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
                  <div className="dim" style={{ lineHeight: 1.8 }}>
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
                  <div className="dim" style={{ lineHeight: 1.8 }}>
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
                  {wizChoice === 'frontdoor' && (
                    <button className="btn primary"
                      onClick={() => { closeWizard(); document.getElementById('frontdoor-config')?.scrollIntoView({ behavior: 'smooth' }); }}>
                      Set up Front Door
                    </button>
                  )}
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

/* ---------- Front Door: RackOps' own tunnel ---------- */

// FrontDoorSection is the install-wide on/off for Front Door, plus the truth
// about the live tunnel.
//
// The honesty here is the point. A tunnel that says "on" while it is actually
// rejected or reconnecting sends people to check their app, their router and
// their DNS before they think to doubt the console. So the status pill reads
// from what the agent reported, the transport is shown when it fell back to
// TCP (which means something on the network blocks UDP), and usage against the
// free tier is visible before the cap bites rather than after.
function FrontDoorSection() {
  const { frontDoor, nodeName, refresh } = useData();
  const toast = useToast();
  const confirm = useConfirm();
  const [workspace, setWorkspace] = useState('');
  const [busy, setBusy] = useState(false);

  // Seed the field from the server only while it is still empty. Front Door's
  // status arrives over SSE and can land mid-edit, so overwriting a non-empty
  // field would erase what the user is typing. The guard also makes this
  // self-limiting: once workspace is set, the effect re-runs and does nothing.
  useEffect(() => {
    const saved = frontDoor?.workspace;
    if (saved && workspace === '') setWorkspace(saved);
  }, [frontDoor?.workspace, workspace]);

  // Not configured on this server: say so plainly instead of showing a switch
  // that cannot work.
  if (frontDoor && !frontDoor.enabled && !frontDoor.popAddr) {
    return (
      <>
        <div className="section-title" id="frontdoor-config">
          <Globe size={15} aria-hidden /> Front Door
        </div>
        <div className="dim" style={{ marginBottom: 22 }}>
          This install isn&rsquo;t set up for RackOps Front Door. Start the server with{' '}
          <span className="mono">--frontdoor-pop</span> to enable it, or use a domain of your own below.
        </div>
      </>
    );
  }

  const status = frontDoor?.status ?? 'off';
  const on = !!frontDoor?.enabled;
  const used = frontDoor?.usedBytes ?? 0;
  const quota = frontDoor?.quotaBytes ?? 0;
  const pct = quota > 0 ? (used / quota) * 100 : 0;

  const pill = !on
    ? <Pill status="stopped">off</Pill>
    : status === 'connected' ? <Pill status="ok">connected</Pill>
    : status === 'rejected' ? <Pill status="error">not connected</Pill>
    : <Pill status="pending">connecting</Pill>;

  const toggle = async (v: boolean) => {
    if (!v && !(await confirm({
      title: 'Turn Front Door off?',
      body: 'Every app published through it stops being reachable at its RackOps address. Apps on your own domains are unaffected.',
      confirmLabel: 'Turn it off',
      danger: true,
    }))) return;
    setBusy(true);
    try {
      await api.setFrontDoor(v, workspace.trim());
      toast.ok(v ? 'Front Door is on.' : 'Front Door is off.');
      refresh();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  return (
    <>
      <div className="section-title" id="frontdoor-config">
        <Globe size={15} aria-hidden /> Front Door
        <InfoTip wide>
          A permanent public HTTPS address for any app, served from RackOps&rsquo; own network.
          This machine dials out, so nothing needs opening on your router and it works behind
          CGNAT. Your apps and data stay on your hardware; only traffic passes through.
        </InfoTip>
        <span style={{ marginLeft: 'auto' }}>{pill}</span>
      </div>
      <div className="dim" style={{ marginBottom: 14 }}>
        Public HTTPS for your apps with no ports, no DNS and no third-party account.
      </div>

      <div className="setting toggle">
        <div style={{ minWidth: 0, flex: 1 }}>
          <div className="setting-title">Publish through RackOps Front Door</div>
          <div className="setting-desc">
            {on
              ? <>Apps appear at <span className="mono">app-{frontDoor?.workspace}.{frontDoor?.zone}</span>.</>
              : <>Pick a workspace name — it becomes part of every address.</>}
          </div>
        </div>
        <Switch checked={on} disabled={busy || (!on && workspace.trim() === '')}
          ariaLabel="RackOps Front Door" onChange={toggle} />
      </div>

      {!on && (
        <div className="row wrap" style={{ marginTop: 12, alignItems: 'flex-end' }}>
          <Field label="Workspace name" hint="Lowercase letters, numbers and dashes.">
            {(p) => (
              <input {...p} className="input" style={{ maxWidth: 240 }} value={workspace}
                placeholder="acme" onChange={(e) => setWorkspace(e.target.value)} />
            )}
          </Field>
          {workspace.trim() !== '' && (
            <span className="faint" style={{ paddingBottom: 6 }}>
              Addresses look like{' '}
              <span className="mono">immich-{workspace.trim().toLowerCase().replace(/[^a-z0-9-]/g, '-')}.{frontDoor?.zone || 'link.rackops.in'}</span>
            </span>
          )}
        </div>
      )}

      {on && (
        <div className="list" style={{ marginTop: 14 }}>
          <div className="list-row spread">
            <span className="dim">Tunnel machine</span>
            <span>{frontDoor?.nodeId ? nodeName(frontDoor.nodeId) : '—'}</span>
          </div>
          {status === 'connected' && (
            <div className="list-row spread">
              <span className="dim">Edge location</span>
              <span className="row" style={{ gap: 8 }}>
                <span className="mono">{frontDoor?.popId || '—'}</span>
                {/* Falling back to TCP is worth surfacing: it means UDP is
                    blocked here, which is the first thing to know if the
                    connection feels slow. */}
                {frontDoor?.transport === 'tcp' && (
                  <span className="badge neutral" title="This network blocks UDP, so the tunnel is using TCP.">TCP fallback</span>
                )}
                {frontDoor?.transport === 'quic' && <span className="badge neutral">QUIC</span>}
              </span>
            </div>
          )}
          {status !== 'connected' && frontDoor?.statusMsg && (
            <div className="list-row spread">
              <span className="dim">Status</span>
              <span style={{ color: status === 'rejected' ? 'var(--crit)' : undefined }}>{frontDoor.statusMsg}</span>
            </div>
          )}
          {quota > 0 && (
            <div className="list-row" style={{ display: 'block' }}>
              <div className="spread" style={{ display: 'flex' }}>
                <span className="dim">Traffic this month</span>
                <span>{formatBytes(used)} of {formatBytes(quota)}</span>
              </div>
              <div style={{ marginTop: 8 }}><UsageBar pct={pct} /></div>
              {pct > 80 && (
                <div className="faint" style={{ marginTop: 6 }}>
                  Approaching the monthly limit. Apps stop being reachable at their RackOps
                  addresses until it resets.
                </div>
              )}
            </div>
          )}
        </div>
      )}
      <div style={{ height: 22 }} />
    </>
  );
}
