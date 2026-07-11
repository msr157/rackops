import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Rocket, Package, Container, GitBranch, Layers, Sparkles, Plus, Trash2, ArrowLeft, ArrowRight,
  Globe, CheckCircle2, ScrollText, Bot, Server, Home, Wrench, Gamepad2, Database,
} from 'lucide-react';
import { api } from '../api';
import type { Template, EnvVar, PlacementScore, Service, Deployment } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import { Led, Spinner, UrlChip, publicUrls } from '../components/ui';

type Source = 'template' | 'image' | 'git' | 'compose';

const CATEGORIES = [
  { key: '', label: 'All', icon: <Layers size={12} /> },
  { key: 'infra', label: 'Infra', icon: <Server size={12} /> },
  { key: 'homelab', label: 'Homelab', icon: <Home size={12} /> },
  { key: 'ai', label: 'AI', icon: <Bot size={12} /> },
  { key: 'web', label: 'Web', icon: <Globe size={12} /> },
  { key: 'tools', label: 'Tools', icon: <Wrench size={12} /> },
  { key: 'games', label: 'Games', icon: <Gamepad2 size={12} /> },
  { key: 'database', label: 'Databases', icon: <Database size={12} /> },
];

// DeployFlow: pick a source → configure → launch and watch it go live.
// The flow ends on a working app, not a closed dialog.
export function DeployFlow() {
  const { nodes, projects, envs, nodeName } = useData();
  const navigate = useNavigate();
  const toast = useToast();

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [source, setSource] = useState<Source>('template');

  // step 1 state
  const [templates, setTemplates] = useState<Template[]>([]);
  const [category, setCategory] = useState('');
  const [search, setSearch] = useState('');
  const [tpl, setTpl] = useState<Template | null>(null);
  const [image, setImage] = useState('');
  const [gitRepo, setGitRepo] = useState('');
  const [gitBranch, setGitBranch] = useState('main');
  const [gitToken, setGitToken] = useState('');
  const [composeYml, setComposeYml] = useState('');

  // step 2 state — containerPort is what the app listens on; hostPort is what
  // it's published/reached on (auto-suggested free unless the user edits it).
  const [name, setName] = useState('');
  const [containerPort, setContainerPort] = useState('');
  const [hostPort, setHostPort] = useState('');
  const [hostTouched, setHostTouched] = useState(false);
  const [suggesting, setSuggesting] = useState(false);
  const [nodeId, setNodeId] = useState('auto');
  const [envVars, setEnvVars] = useState<EnvVar[]>([]);
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [busy, setBusy] = useState(false);

  const isCompose = source === 'compose' || !!tpl?.composeYml;

  // step 3 state
  const [svcId, setSvcId] = useState('');

  useEffect(() => { api.templates().then((t) => setTemplates(t ?? [])).catch(() => {}); }, []);
  useEffect(() => {
    api.placement(tpl ? { memMb: tpl.memMb, needsGpu: !!tpl.gpu } : {}).then(setScores).catch(() => {});
  }, [tpl]);

  const filtered = useMemo(() => templates.filter((t) =>
    (!category || t.category === category) &&
    (!search || (t.name + t.description).toLowerCase().includes(search.toLowerCase()))
  ), [templates, category, search]);

  const pickTemplate = (t: Template) => {
    setTpl(t);
    setName(t.id);
    setContainerPort(t.composeYml ? '' : String(t.port));
    setHostTouched(false); // re-suggest a free host port for this template
    setEnvVars((t.env ?? []).map((d) => ({ key: d.key, value: d.default, secret: d.secret })));
    setStep(2);
  };

  // Auto-fill a free host port when the target/port changes (until the user
  // types their own). For a compose stack the routing port is the template's.
  useEffect(() => {
    if (step !== 2 || hostTouched) return;
    const prefer = isCompose ? (tpl?.port ?? 0) : parseInt(containerPort, 10) || 0;
    if (!prefer) return;
    setSuggesting(true);
    api.suggestPort(nodeId, prefer)
      .then((r) => setHostPort(String(r.port)))
      .catch(() => setHostPort(String(prefer)))
      .finally(() => setSuggesting(false));
  }, [step, nodeId, containerPort, isCompose, tpl, hostTouched]);

  const sourceReady =
    (source === 'template' && !!tpl) ||
    (source === 'image' && image.trim() !== '') ||
    (source === 'git' && gitRepo.trim() !== '') ||
    (source === 'compose' && composeYml.trim() !== '');

  const best = scores.find((s) => s.viable);

  const launch = async () => {
    if (!name.trim()) { toast.err('Give the app a name.'); return; }
    const body: Record<string, unknown> = {
      name: name.trim(),
      projectId: projects[0]?.id ?? '',
      envId: envs.find((e) => e.name === 'production')?.id ?? envs[0]?.id ?? '',
      nodeId,
      port: hostPort ? parseInt(hostPort, 10) : 0,
      containerPort: isCompose ? 0 : (parseInt(containerPort, 10) || 0),
      env: envVars.filter((v) => v.key),
    };
    if (source === 'template') {
      body.templateId = tpl!.id;
      body.placement = { memMb: tpl!.memMb, needsGpu: !!tpl!.gpu };
    } else if (source === 'image') {
      body.kind = 'app'; body.image = image.trim();
    } else if (source === 'git') {
      body.kind = 'app'; body.gitRepo = gitRepo.trim(); body.gitBranch = gitBranch.trim() || 'main'; body.gitToken = gitToken;
    } else {
      body.kind = 'compose'; body.composeYml = composeYml;
    }
    setBusy(true);
    try {
      const s = await api.createService(body);
      setSvcId(s.id);
      setStep(3);
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  return (
    <div className="page" style={{ maxWidth: 760 }}>
      <div className="row" style={{ marginBottom: 4 }}>
        <button className="btn sm ghost" onClick={() => (step === 1 ? navigate('/apps') : setStep((s) => (s - 1) as 1 | 2))} disabled={step === 3}>
          <ArrowLeft size={13} />
        </button>
        <div className="page-title" style={{ margin: 0 }}>Deploy an app</div>
      </div>
      <div className="page-sub">From zero to a running app — RackOps picks the best machine for it.</div>

      <div className="stepper">
        {(['Source', 'Configure', 'Launch'] as const).map((label, i) => (
          <div key={label} className="row" style={{ gap: 10 }}>
            {i > 0 && <div className="step-rail" />}
            <div className={`step ${step === i + 1 ? 'active' : step > i + 1 ? 'done' : ''}`}>
              <span className="step-dot">{step > i + 1 ? '✓' : i + 1}</span> {label}
            </div>
          </div>
        ))}
      </div>

      {step === 1 && (
        <>
          <div className="grid-2" style={{ gridTemplateColumns: 'repeat(4, 1fr)', gap: 8, marginBottom: 18 }}>
            {([
              ['template', <Package size={16} key="t" />, 'Template', 'Ready-made apps'],
              ['image', <Container size={16} key="i" />, 'Image', 'Any Docker image'],
              ['git', <GitBranch size={16} key="g" />, 'Git repo', 'Build from source'],
              ['compose', <Layers size={16} key="c" />, 'Compose', 'Multi-container stack'],
            ] as [Source, React.ReactNode, string, string][]).map(([key, icon, label, sub]) => (
              <button key={key} className="card clickable" style={{
                textAlign: 'center', padding: '14px 8px', fontFamily: 'inherit',
                borderColor: source === key ? 'var(--amber)' : undefined,
              }} onClick={() => setSource(key)}>
                <div style={{ color: source === key ? 'var(--amber)' : 'var(--dim)', marginBottom: 6 }}>{icon}</div>
                <div style={{ fontWeight: 600, fontSize: 13 }}>{label}</div>
                <div className="faint">{sub}</div>
              </button>
            ))}
          </div>

          {source === 'template' && (
            <>
              <div className="row wrap" style={{ marginBottom: 10 }}>
                {CATEGORIES.map((c) => (
                  <button key={c.key} className={`btn sm ${category === c.key ? 'primary' : ''}`} onClick={() => setCategory(c.key)}>
                    {c.icon} {c.label}
                  </button>
                ))}
              </div>
              <input className="input" placeholder={`Search ${templates.length || '45+'} templates…`} value={search}
                onChange={(e) => setSearch(e.target.value)} style={{ marginBottom: 10 }} autoFocus />
              <div style={{ maxHeight: 380, overflowY: 'auto', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                {filtered.map((t) => (
                  <button key={t.id} className="card clickable" style={{
                    textAlign: 'left', fontFamily: 'inherit', padding: 12,
                  }} onClick={() => pickTemplate(t)}>
                    <div className="row spread">
                      <strong style={{ fontSize: 13, color: 'var(--text)' }}>{t.name}</strong>
                      <span className="tag">{t.category}</span>
                    </div>
                    <div className="faint" style={{ marginTop: 4 }}>{t.description}</div>
                    {t.gpu && <span className="badge purple" style={{ marginTop: 6 }}>needs GPU</span>}
                  </button>
                ))}
                {filtered.length === 0 && <div className="faint" style={{ padding: 12 }}>No template matches.</div>}
              </div>
            </>
          )}

          {source === 'image' && (
            <div className="field">
              <label>Container image</label>
              <input className="input mono" placeholder="nginx:alpine" value={image} autoFocus
                onChange={(e) => setImage(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && sourceReady && setStep(2)} />
              <span className="faint">Anything public on Docker Hub, GHCR, or any registry your machines can reach.</span>
            </div>
          )}

          {source === 'git' && (
            <>
              <div className="field">
                <label>Repository URL</label>
                <input className="input mono" placeholder="https://github.com/user/repo" value={gitRepo} autoFocus
                  onChange={(e) => setGitRepo(e.target.value)} />
                <span className="faint">Dockerfile used if present; otherwise the stack (Node/Go/Python/static) is detected automatically.</span>
              </div>
              <div className="grid-2">
                <div className="field">
                  <label>Branch</label>
                  <input className="input mono" value={gitBranch} onChange={(e) => setGitBranch(e.target.value)} />
                </div>
                <div className="field">
                  <label>Access token (private repos)</label>
                  <input className="input" type="password" placeholder="optional" value={gitToken} onChange={(e) => setGitToken(e.target.value)} />
                </div>
              </div>
            </>
          )}

          {source === 'compose' && (
            <div className="field">
              <label>docker-compose.yml</label>
              <textarea className="input mono" rows={10} placeholder={'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8088:80"'}
                value={composeYml} onChange={(e) => setComposeYml(e.target.value)} style={{ resize: 'vertical' }} autoFocus />
            </div>
          )}

          {source !== 'template' && (
            <button className="btn primary" style={{ marginTop: 6 }} disabled={!sourceReady}
              onClick={() => { if (!name) setName(''); setStep(2); }}>
              Continue <ArrowRight size={13} />
            </button>
          )}
        </>
      )}

      {step === 2 && (
        <>
          {tpl && (
            <div className="card inset row" style={{ marginBottom: 16, padding: '10px 14px' }}>
              <Package size={14} style={{ color: 'var(--amber)' }} />
              <strong>{tpl.name}</strong>
              <span className="dim">{tpl.description}</span>
              {tpl.notes && <span className="badge yellow" style={{ marginLeft: 'auto' }}>{tpl.notes}</span>}
            </div>
          )}

          <div className="field">
            <label>App name</label>
            <input className="input" placeholder="my-app" value={name} autoFocus
              onChange={(e) => setName(e.target.value)} />
          </div>

          {isCompose ? (
            <div className="field">
              <label>Web port {suggesting && <span className="faint">· finding a free one…</span>}</label>
              <input className="input mono" placeholder="8080" value={hostPort}
                onChange={(e) => { setHostPort(e.target.value.replace(/\D/g, '')); setHostTouched(true); }} />
              <span className="faint">The stack publishes its own ports; RackOps routes to this one. Auto-picked to avoid clashes.</span>
            </div>
          ) : (
            <div className="grid-2">
              <div className="field">
                <label>App listens on port</label>
                <input className="input mono" placeholder="8080" value={containerPort}
                  onChange={(e) => setContainerPort(e.target.value.replace(/\D/g, ''))} />
                <span className="faint">{tpl ? 'From the template — the port inside the container.' : "The port your app listens on inside the container."}</span>
              </div>
              <div className="field">
                <label>Reachable on host port {suggesting && <span className="faint">· finding a free one…</span>}</label>
                <input className="input mono" placeholder="auto" value={hostPort}
                  onChange={(e) => { setHostPort(e.target.value.replace(/\D/g, '')); setHostTouched(true); }} />
                <span className="faint">
                  How you'll reach it — auto-picked to avoid clashes.
                  {containerPort && hostPort && ` Maps ${hostPort} → ${containerPort}.`}
                </span>
              </div>
            </div>
          )}

          <div className="field">
            <label>Machine</label>
            <select className="input" value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
              <option value="auto">Auto — pick the best machine for me</option>
              {nodes.map((n) => <option key={n.id} value={n.id}>{n.name}{n.status !== 'online' ? ' — offline' : ''}</option>)}
            </select>
          </div>
          {nodeId === 'auto' && best && (
            <div className="card" style={{ marginBottom: 14, borderColor: 'var(--amber)' }}>
              <div className="row">
                <Sparkles size={14} style={{ color: 'var(--amber)' }} />
                <strong>Will run on {nodeName(best.nodeId)}</strong>
                <span className="badge amber">score {best.score}</span>
              </div>
              <div className="faint" style={{ marginTop: 6 }}>{best.reasons.join(' · ')}</div>
            </div>
          )}
          {nodeId === 'auto' && !best && scores.length > 0 && (
            <div className="card" style={{ marginBottom: 14, borderColor: 'var(--crit)' }}>
              <strong style={{ color: 'var(--crit)' }}>No machine can run this right now.</strong>
              <div className="dim" style={{ marginTop: 4 }}>
                {scores[0]?.reasons.join(' · ') || 'Check that at least one machine is online with enough free memory.'}
              </div>
            </div>
          )}

          <div className="field">
            <label>Environment variables{tpl && (tpl.env ?? []).length > 0 ? ' — pre-filled from the template' : ' (optional)'}</label>
            {envVars.map((v, i) => (
              <div className="row" key={i}>
                <input className="input mono" placeholder="KEY" value={v.key}
                  onChange={(e) => setEnvVars(envVars.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))} />
                <input className="input mono" placeholder={v.secret ? 'auto-generated if blank' : 'value'}
                  type={v.secret ? 'password' : 'text'} value={v.value}
                  onChange={(e) => setEnvVars(envVars.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} />
                <button className="btn sm danger" aria-label="Remove variable"
                  onClick={() => setEnvVars(envVars.filter((_, j) => j !== i))}><Trash2 size={12} /></button>
              </div>
            ))}
            <button className="btn sm" style={{ alignSelf: 'flex-start' }}
              onClick={() => setEnvVars([...envVars, { key: '', value: '', secret: false }])}>
              <Plus size={12} /> Add variable
            </button>
          </div>

          <button className="btn primary" style={{ width: '100%' }} disabled={busy || !name.trim()} onClick={launch}>
            {busy ? <Spinner /> : <Rocket size={14} />} {busy ? 'Starting…' : 'Launch'}
          </button>
        </>
      )}

      {step === 3 && svcId && <LaunchProgress serviceId={svcId} />}
    </div>
  );
}

// LaunchProgress watches the new app go live: status timeline + build log,
// ending on the "make it public" payoff instead of a closed dialog.
function LaunchProgress({ serviceId }: { serviceId: string }) {
  const navigate = useNavigate();
  const { nodes, ingress } = useData();
  const [svc, setSvc] = useState<Service | null>(null);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const logRef = useRef<HTMLDivElement>(null);
  const settled = svc?.status === 'running' || svc?.status === 'failed';

  useEffect(() => {
    let alive = true;
    const poll = async () => {
      try {
        const s = await api.service(serviceId);
        if (!alive) return;
        setSvc(s);
        const d = await api.serviceDeployments(serviceId).catch(() => []);
        if (alive) setDeploys(d ?? []);
      } catch { /* service may briefly 404 right after create */ }
    };
    poll();
    const t = setInterval(() => {
      poll();
    }, 2000);
    return () => { alive = false; clearInterval(t); };
  }, [serviceId]);

  // keep the live log pinned to the bottom while building
  useEffect(() => {
    if (!settled && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [deploys, settled]);

  const latest = deploys[0];
  const node = nodes.find((n) => n.id === svc?.nodeId);
  const urls = svc ? publicUrls(svc, ingress) : [];

  return (
    <>
      <div className="card row" style={{ marginBottom: 14, padding: '14px 16px' }}>
        {svc?.status === 'running' ? <Led lg status="running" />
          : svc?.status === 'failed' ? <Led lg status="failed" />
          : <Spinner size={16} />}
        <div style={{ flex: 1 }}>
          <div style={{ fontFamily: 'var(--display)', fontWeight: 600, fontSize: 15 }}>
            {!svc ? 'Provisioning…'
              : svc.status === 'running' ? `${svc.name} is live`
              : svc.status === 'failed' ? `${svc.name} failed to start`
              : svc.status === 'building' ? 'Building…'
              : 'Deploying…'}
          </div>
          <div className="dim">
            {svc?.status === 'running' && node ? `running on ${node.name}`
              : svc?.status === 'failed' ? 'the full output is below — fix and redeploy from the app page'
              : 'pulling the image and starting the container'}
          </div>
        </div>
      </div>

      {svc?.status === 'running' && (
        <div className="card" style={{ borderColor: 'var(--ok)', marginBottom: 14 }}>
          <div className="row" style={{ marginBottom: 10 }}>
            <CheckCircle2 size={15} style={{ color: 'var(--ok)' }} />
            <strong>Reach it</strong>
          </div>
          <div className="row wrap" style={{ marginBottom: 12 }}>
            {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
            {node && svc.port > 0 && <UrlChip url={`http://${node.ip}:${svc.port}`} />}
            {!svc.port && <span className="faint">no port published — this app is internal</span>}
          </div>
          <div className="row">
            <button className="btn primary" onClick={() => navigate(`/apps/${svc.id}/networking`)}>
              <Globe size={13} /> Make it public
            </button>
            <button className="btn" onClick={() => navigate(`/apps/${svc.id}`)}>Open app page</button>
          </div>
        </div>
      )}

      {svc?.status === 'failed' && (
        <div className="row" style={{ marginBottom: 14 }}>
          <button className="btn primary" onClick={() => navigate(`/apps/${svc.id}/logs`)}><ScrollText size={13} /> Open logs</button>
          <button className="btn" onClick={() => navigate(`/apps/${svc.id}`)}>Open app page</button>
        </div>
      )}

      <div className="eyebrow" style={{ marginBottom: 6 }}>Deployment output</div>
      <div className="terminal" ref={logRef} style={{ maxHeight: 400 }}>
        {latest?.log || 'waiting for the machine to pick up the job…'}
      </div>
    </>
  );
}
