import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  Rocket, Package, Container, GitBranch, Layers, Sparkles, Plus, Trash2, ArrowLeft, ArrowRight,
  Globe, Check, CheckCircle2, ScrollText, Bot, Server, House, Wrench, Gamepad2, Database, KeyRound,
  Search, RotateCw, Activity, Box, Clapperboard, HardDrive, Music, Shield,
} from 'lucide-react';
import { api, subscribeLogChunks } from '../api';
import type { Template, EnvVar, PlacementScore, Service, Deployment, LogChunk } from '../api';
import { useData } from '../data';
import { useToast, cleanError } from '../components/toast';
import {
  EmptyState, Field, IconChip, Led, PasswordInput, Skeleton, Spinner, UrlChip, dnsLabel, publicUrls,
} from '../components/ui';

type Source = 'template' | 'image' | 'git' | 'compose';

const CATEGORIES = [
  { key: '', label: 'All', icon: <Layers size={12} /> },
  { key: 'infra', label: 'Infra', icon: <Server size={12} /> },
  { key: 'homelab', label: 'Homelab', icon: <House size={12} /> },
  { key: 'ai', label: 'AI', icon: <Bot size={12} /> },
  { key: 'web', label: 'Web', icon: <Globe size={12} /> },
  { key: 'tools', label: 'Tools', icon: <Wrench size={12} /> },
  { key: 'games', label: 'Games', icon: <Gamepad2 size={12} /> },
  { key: 'database', label: 'Databases', icon: <Database size={12} /> },
];

const SOURCES: { key: Source; icon: React.ReactNode; label: string; sub: string }[] = [
  { key: 'template', icon: <Package size={16} />, label: 'Template', sub: 'Ready-made apps' },
  { key: 'image', icon: <Container size={16} />, label: 'Image', sub: 'Any Docker image' },
  { key: 'git', icon: <GitBranch size={16} />, label: 'Git repo', sub: 'Build from source' },
  { key: 'compose', icon: <Layers size={16} />, label: 'Compose', sub: 'Multi-container stack' },
];

// Templates render in as many columns as fit; a lone result keeps card width.
const TPL_GRID: React.CSSProperties = {
  display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: 8,
};

// templateIcon: one recognizable glyph per template so the grid scans by shape,
// not by reading 45+ names. Keyed off id/name keywords first, then the
// category. Chips stay neutral: template identity is a fact, not a status.
const TPL_ICON = 14;

function templateIcon(t: Template): React.ReactNode {
  const hay = `${t.id} ${t.name}`.toLowerCase();
  const has = (...words: string[]) => words.some((w) => hay.includes(w));
  if (has('postgres', 'mysql', 'maria', 'mongo', 'redis', 'clickhouse', 'qdrant', 'sql', 'database')) return <Database size={TPL_ICON} />;
  if (has('music', 'audio', 'navidrome')) return <Music size={TPL_ICON} />;
  if (has('jellyfin', 'plex', 'immich', 'media', 'video', 'photo')) return <Clapperboard size={TPL_ICON} />;
  if (has('git')) return <GitBranch size={TPL_ICON} />;
  if (has('docker', 'registry', 'container')) return <Container size={TPL_ICON} />;
  if (has('prometheus', 'grafana', 'uptime', 'monitor', 'metric', 'plausible', 'umami', 'analytic')) return <Activity size={TPL_ICON} />;
  if (has('minio', 'seaweed', 'nextcloud', 'syncthing', 'filebrowser', 'file', 'storage', 'backup')) return <HardDrive size={TPL_ICON} />;
  if (has('home assistant', 'home-assistant', 'homeassistant')) return <House size={TPL_ICON} />;
  if (has('vaultwarden', 'vault', 'password', 'secret')) return <KeyRound size={TPL_ICON} />;
  if (has('adguard', 'pihole', 'pi-hole', 'firewall')) return <Shield size={TPL_ICON} />;
  if (has('nginx', 'caddy', 'traefik', 'proxy')) return <Globe size={TPL_ICON} />;
  if (has('ollama', 'openai', 'llm', 'webui', 'flowise')) return <Bot size={TPL_ICON} />;
  switch (t.category) {
    case 'database': return <Database size={TPL_ICON} />;
    case 'ai': return <Bot size={TPL_ICON} />;
    case 'games': return <Gamepad2 size={TPL_ICON} />;
    case 'web': return <Globe size={TPL_ICON} />;
    case 'homelab': return <House size={TPL_ICON} />;
    case 'infra': return <Server size={TPL_ICON} />;
    case 'tools': return <Wrench size={TPL_ICON} />;
    default: return <Box size={TPL_ICON} />;
  }
}

// Env rows need identity that survives removal from the middle of the list;
// index keys would hand row N's focused input to row N+1's data.
type EnvRow = EnvVar & { _id: number };
let envRowSeq = 0;
const envRow = (v: EnvVar): EnvRow => ({ ...v, _id: ++envRowSeq });

// The in-progress wizard lives in sessionStorage (tab-scoped) so a refresh at
// step 1 or 2 lands back where the operator left off. Cleared the moment a
// launch succeeds so a finished deploy never resurrects old input.
const DRAFT_KEY = 'rackops-deploy-draft';

interface DeployDraft {
  step: 1 | 2;
  source: Source;
  // The whole template is stored, not just its id, so rehydration works even
  // while the remote catalog is still loading (or failing to).
  tpl: Template | null;
  image: string;
  gitRepo: string;
  gitBranch: string;
  gitToken: string;
  composeYml: string;
  name: string;
  containerPort: string;
  hostPort: string;
  hostTouched: boolean;
  nodeId: string;
  envVars: EnvVar[];
}

function readDraft(): DeployDraft | null {
  try {
    const raw = sessionStorage.getItem(DRAFT_KEY);
    if (!raw) return null;
    const d = JSON.parse(raw) as Partial<DeployDraft> | null;
    if (!d || typeof d !== 'object') return null;
    const source: Source =
      d.source === 'image' || d.source === 'git' || d.source === 'compose' ? d.source : 'template';
    return {
      step: d.step === 2 ? 2 : 1,
      source,
      tpl: d.tpl && typeof d.tpl === 'object' ? d.tpl : null,
      image: typeof d.image === 'string' ? d.image : '',
      gitRepo: typeof d.gitRepo === 'string' ? d.gitRepo : '',
      gitBranch: typeof d.gitBranch === 'string' ? d.gitBranch : 'main',
      gitToken: typeof d.gitToken === 'string' ? d.gitToken : '',
      composeYml: typeof d.composeYml === 'string' ? d.composeYml : '',
      name: typeof d.name === 'string' ? d.name : '',
      containerPort: typeof d.containerPort === 'string' ? d.containerPort : '',
      hostPort: typeof d.hostPort === 'string' ? d.hostPort : '',
      hostTouched: !!d.hostTouched,
      nodeId: typeof d.nodeId === 'string' && d.nodeId !== '' ? d.nodeId : 'auto',
      envVars: Array.isArray(d.envVars)
        ? d.envVars
          .filter((v): v is EnvVar => !!v && typeof v.key === 'string' && typeof v.value === 'string')
          .map((v) => ({ key: v.key, value: v.value, secret: !!v.secret }))
        : [],
    };
  } catch {
    return null;
  }
}

// The placement panel renders its skeleton, result, and error states inside
// one reserved slot so the fields below never jump while scores load.
const PLACEMENT_SLOT = 118;

// DeployFlow: pick a source, configure, launch and watch it go live.
// The flow ends on a working app, not a closed dialog.
export function DeployFlow() {
  const { nodes, projects, envs, nodeName } = useData();
  const navigate = useNavigate();
  const toast = useToast();

  // /apps/new/launch/:serviceId is the refresh-safe home of a running launch;
  // with a serviceId in the URL this page shows live progress, not the form.
  const { serviceId: launchId } = useParams();
  const onLaunch = !!launchId;

  // Machines detail links here with ?node=<id> preselected.
  const [searchParams] = useSearchParams();
  const nodeParam = searchParams.get('node');

  const [draft] = useState<DeployDraft | null>(readDraft);

  const [step, setStep] = useState<1 | 2>(draft?.step ?? 1);
  const [source, setSource] = useState<Source>(draft?.source ?? 'template');
  const searchId = useId();
  const sourceRefs = useRef<(HTMLButtonElement | null)[]>([]);

  // step 1 state
  const [templates, setTemplates] = useState<Template[]>([]);
  const [tplState, setTplState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [category, setCategory] = useState('');
  const [search, setSearch] = useState('');
  const [tpl, setTpl] = useState<Template | null>(draft?.tpl ?? null);
  const [image, setImage] = useState(draft?.image ?? '');
  const [gitRepo, setGitRepo] = useState(draft?.gitRepo ?? '');
  const [gitBranch, setGitBranch] = useState(draft?.gitBranch ?? 'main');
  const [gitToken, setGitToken] = useState(draft?.gitToken ?? '');
  const [composeYml, setComposeYml] = useState(draft?.composeYml ?? '');

  // step 2 state. containerPort is what the app listens on; hostPort is what
  // it's published/reached on (auto-suggested free unless the user edits it).
  const [name, setName] = useState(draft?.name ?? '');
  const [containerPort, setContainerPort] = useState(draft?.containerPort ?? '');
  const [hostPort, setHostPort] = useState(draft?.hostPort ?? '');
  const [hostTouched, setHostTouched] = useState(draft?.hostTouched ?? false);
  const [suggesting, setSuggesting] = useState(false);
  const [nodeId, setNodeId] = useState(nodeParam || draft?.nodeId || 'auto');
  const [envVars, setEnvVars] = useState<EnvRow[]>((draft?.envVars ?? []).map(envRow));
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [scoresLoaded, setScoresLoaded] = useState(false);
  const [busy, setBusy] = useState(false);

  // A picked compose template only matters while the source IS the template;
  // switching to another source must not drag its compose behavior along.
  const isCompose = source === 'compose' || (source === 'template' && !!tpl?.composeYml);

  // Every change updates the draft; a refresh at step 1 or 2 rehydrates it.
  useEffect(() => {
    if (onLaunch) return;
    const d: DeployDraft = {
      step, source, tpl, image, gitRepo, gitBranch, gitToken, composeYml,
      name, containerPort, hostPort, hostTouched, nodeId,
      envVars: envVars.map(({ key, value, secret }) => ({ key, value, secret })),
    };
    try { sessionStorage.setItem(DRAFT_KEY, JSON.stringify(d)); } catch { /* a blocked store only costs the draft */ }
  }, [onLaunch, step, source, tpl, image, gitRepo, gitBranch, gitToken, composeYml,
    name, containerPort, hostPort, hostTouched, nodeId, envVars]);

  // Honor a ?node=<id> that arrives after mount (a link clicked mid-session).
  useEffect(() => { if (nodeParam) setNodeId(nodeParam); }, [nodeParam]);

  // A draft or link can name a machine that has since left the fleet.
  useEffect(() => {
    if (nodeId === 'auto' || nodes.length === 0) return;
    if (!nodes.some((n) => n.id === nodeId)) setNodeId('auto');
  }, [nodes, nodeId]);

  // The catalog is remote: show skeletons while it loads and a real error with
  // a retry when it doesn't. A failed fetch must never read as "no templates".
  const loadTemplates = useCallback(() => {
    setTplState('loading');
    api.templates()
      .then((t) => { setTemplates(t ?? []); setTplState('ready'); })
      .catch(() => setTplState('error'));
  }, []);
  useEffect(() => { if (!onLaunch) loadTemplates(); }, [loadTemplates, onLaunch]);

  useEffect(() => {
    if (onLaunch) return;
    setScoresLoaded(false);
    api.placement(tpl ? { memMb: tpl.memMb, needsGpu: !!tpl.gpu } : {})
      .then((s) => { setScores(s ?? []); setScoresLoaded(true); })
      .catch(() => setScoresLoaded(true));
  }, [tpl, onLaunch]);

  const filtered = useMemo(() => templates.filter((t) =>
    (!category || t.category === category) &&
    (!search || (t.name + t.description).toLowerCase().includes(search.toLowerCase()))
  ), [templates, category, search]);

  // Selecting a template seeds the config step. Re-picking the same one keeps
  // any edits; only a different template resets name, ports, and env rows.
  const pickTemplate = (t: Template) => {
    if (tpl?.id === t.id) return;
    setTpl(t);
    setName(t.id);
    setContainerPort(t.composeYml ? '' : String(t.port));
    setHostTouched(false); // re-suggest a free host port for this template
    setEnvVars((t.env ?? []).map((d) => envRow({ key: d.key, value: d.default, secret: d.secret })));
  };

  // Auto-fill a free host port when the target/port changes (until the user
  // types their own). For a compose stack the routing port is the template's.
  useEffect(() => {
    if (onLaunch || step !== 2 || hostTouched) return;
    const prefer = isCompose ? (tpl?.port ?? 0) : parseInt(containerPort, 10) || 0;
    if (!prefer) return;
    setSuggesting(true);
    api.suggestPort(nodeId, prefer)
      .then((r) => setHostPort(String(r.port)))
      .catch(() => setHostPort(String(prefer)))
      .finally(() => setSuggesting(false));
  }, [onLaunch, step, nodeId, containerPort, isCompose, tpl, hostTouched]);

  const sourceReady =
    (source === 'template' && !!tpl) ||
    (source === 'image' && image.trim() !== '') ||
    (source === 'git' && gitRepo.trim() !== '') ||
    (source === 'compose' && composeYml.trim() !== '');

  const best = scores.find((s) => s.viable);

  // Radiogroup keyboard contract: arrows move both focus and selection.
  const onSourceKey = (e: React.KeyboardEvent<HTMLButtonElement>, idx: number) => {
    const dir = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1
      : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0;
    if (!dir) return;
    e.preventDefault();
    const next = (idx + dir + SOURCES.length) % SOURCES.length;
    setSource(SOURCES[next].key);
    sourceRefs.current[next]?.focus();
  };

  const advance = () => { if (sourceReady) setStep(2); };

  const launch = async () => {
    if (busy) return;
    if (!name.trim()) { toast.err('Give the app a name.'); return; }
    // A rehydrated draft can only reach step 2 with a template when one was
    // picked, but never trust storage: without one there is nothing to deploy.
    if (source === 'template' && !tpl) { setStep(1); toast.err('Pick a template first.'); return; }
    const body: Record<string, unknown> = {
      name: name.trim(),
      projectId: projects[0]?.id ?? '',
      envId: envs.find((e) => e.name === 'production')?.id ?? envs[0]?.id ?? '',
      nodeId,
      port: hostPort ? parseInt(hostPort, 10) : 0,
      containerPort: isCompose ? 0 : (parseInt(containerPort, 10) || 0),
      env: envVars.filter((v) => v.key).map((v) => ({ key: v.key, value: v.value, secret: v.secret })),
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
      try { sessionStorage.removeItem(DRAFT_KEY); } catch { /* nothing to clear */ }
      // The launch URL is the source of truth from here: a refresh lands back
      // on live progress instead of an empty form.
      navigate(`/apps/new/launch/${s.id}`, { replace: true });
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const shownStep: 1 | 2 | 3 = onLaunch ? 3 : step;

  return (
    <div className="page" style={{ maxWidth: 760 }}>
      <div className="page-title">Deploy an app</div>
      <div className="page-sub">From zero to a running app. RackOps picks the best machine for it.</div>

      <div className="stepper">
        {(['Source', 'Configure', 'Launch'] as const).map((label, i) => (
          <div key={label} className="row" style={{ gap: 10 }}>
            {i > 0 && <div className="step-rail" />}
            <div className={`step ${shownStep === i + 1 ? 'active' : shownStep > i + 1 ? 'done' : ''}`}>
              <span className="step-dot">{shownStep > i + 1 ? <Check size={12} aria-hidden /> : i + 1}</span> {label}
            </div>
          </div>
        ))}
      </div>

      {!onLaunch && step === 1 && (
        <>
          <div role="radiogroup" aria-label="Deploy source"
            style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8, marginBottom: 18 }}>
            {SOURCES.map((s, i) => {
              const selected = source === s.key;
              return (
                <button key={s.key} type="button" role="radio" aria-checked={selected}
                  tabIndex={selected ? 0 : -1}
                  ref={(el) => { sourceRefs.current[i] = el; }}
                  className="card clickable"
                  style={{
                    textAlign: 'center', padding: '14px 8px', fontFamily: 'inherit',
                    borderColor: selected ? 'var(--accent)' : undefined,
                    background: selected ? 'var(--accent-dim)' : undefined,
                  }}
                  onClick={() => setSource(s.key)}
                  onKeyDown={(e) => onSourceKey(e, i)}>
                  <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 8 }}>
                    <IconChip tone={selected ? 'accent' : undefined}>{s.icon}</IconChip>
                  </div>
                  <div style={{ fontWeight: 600, fontSize: 13 }}>{s.label}</div>
                  <div className="faint">{s.sub}</div>
                </button>
              );
            })}
          </div>

          {source === 'template' && (
            <>
              <div className="row wrap" style={{ marginBottom: 10 }} role="group" aria-label="Filter templates by category">
                {CATEGORIES.map((c) => (
                  <button key={c.key} type="button" className={`btn sm ${category === c.key ? 'chip-on' : ''}`}
                    aria-pressed={category === c.key} onClick={() => setCategory(c.key)}>
                    {c.icon} {c.label}
                  </button>
                ))}
              </div>
              <label className="sr-only" htmlFor={searchId}>Search templates</label>
              <div className="search-input" style={{ marginBottom: 10 }}>
                <Search size={14} aria-hidden />
                <input className="input" id={searchId} placeholder={`Search ${templates.length || '45+'} templates…`}
                  value={search} onChange={(e) => setSearch(e.target.value)} autoFocus />
              </div>

              {tplState === 'loading' && (
                <div style={TPL_GRID} aria-hidden>
                  {Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={92} radius="var(--r-lg)" />)}
                </div>
              )}
              {tplState === 'error' && (
                <div className="card crit">
                  <strong style={{ color: 'var(--crit)' }}>The template catalog didn&rsquo;t load.</strong>
                  <div className="dim" style={{ margin: '4px 0 10px' }}>
                    The server didn&rsquo;t answer. Your machines and apps are unaffected.
                  </div>
                  <button type="button" className="btn sm" onClick={loadTemplates}>
                    <RotateCw size={12} /> Try again
                  </button>
                </div>
              )}
              {tplState === 'ready' && (
                <div role="radiogroup" aria-label="Template"
                  style={{ ...TPL_GRID, maxHeight: 380, overflowY: 'auto', alignContent: 'start' }}>
                  {filtered.map((t) => {
                    const selected = tpl?.id === t.id;
                    return (
                      <button key={t.id} type="button" role="radio" aria-checked={selected}
                        className="card clickable" style={{
                          textAlign: 'left', fontFamily: 'inherit', padding: 12,
                          borderColor: selected ? 'var(--accent)' : undefined,
                          background: selected ? 'var(--accent-dim)' : undefined,
                        }} onClick={() => pickTemplate(t)}>
                        <div className="row" style={{ gap: 10, alignItems: 'flex-start' }}>
                          <IconChip sm>{templateIcon(t)}</IconChip>
                          <div style={{ flex: 1, minWidth: 0 }}>
                            <div className="row spread">
                              <strong style={{ fontSize: 13, color: 'var(--text)' }}>{t.name}</strong>
                              <span className="tag">{t.category}</span>
                            </div>
                            <div className="faint" style={{ marginTop: 4 }}>{t.description}</div>
                            {t.gpu && <span className="tag" style={{ marginTop: 6, display: 'inline-block' }}>needs GPU</span>}
                          </div>
                        </div>
                      </button>
                    );
                  })}
                  {filtered.length === 0 && (
                    <div style={{ gridColumn: '1 / -1' }}>
                      <EmptyState icon={<Search size={18} />} title="No template matches">
                        <div>Try different words or another category, or deploy from a Docker image instead.</div>
                      </EmptyState>
                    </div>
                  )}
                </div>
              )}
            </>
          )}

          {source === 'image' && (
            <Field label="Container image"
              hint="Anything public on Docker Hub, GHCR, or any registry your machines can reach.">
              {(p) => (
                <input className="input mono" {...p} placeholder="nginx:alpine" value={image} autoFocus
                  onChange={(e) => setImage(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && advance()} />
              )}
            </Field>
          )}

          {source === 'git' && (
            <>
              <Field label="Repository URL"
                hint="Dockerfile used if present; otherwise the stack (Node/Go/Python/static) is detected automatically.">
                {(p) => (
                  <input className="input mono" {...p} placeholder="https://github.com/user/repo" value={gitRepo} autoFocus
                    onChange={(e) => setGitRepo(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && advance()} />
                )}
              </Field>
              <div className="grid-2">
                <Field label="Branch">
                  {(p) => (
                    <input className="input mono" {...p} value={gitBranch}
                      onChange={(e) => setGitBranch(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && advance()} />
                  )}
                </Field>
                <Field label="Access token (private repos)">
                  {(p) => (
                    <PasswordInput {...p} value={gitToken} onChange={setGitToken}
                      placeholder="optional" autoComplete="off" onEnter={advance} />
                  )}
                </Field>
              </div>
            </>
          )}

          {source === 'compose' && (
            <Field label="docker-compose.yml">
              {(p) => (
                <textarea className="input mono" {...p} rows={10}
                  placeholder={'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8088:80"'}
                  value={composeYml} onChange={(e) => setComposeYml(e.target.value)} style={{ resize: 'vertical' }} autoFocus />
              )}
            </Field>
          )}

          <div className="row spread" style={{ marginTop: 18 }}>
            <Link to="/apps" className="btn ghost">Cancel</Link>
            <button type="button" className="btn primary" disabled={!sourceReady} onClick={advance}>
              Continue <ArrowRight size={13} aria-hidden />
            </button>
          </div>
        </>
      )}

      {!onLaunch && step === 2 && (
        <form onSubmit={(e) => { e.preventDefault(); launch(); }}>
          {source === 'template' && tpl && (
            <div className="card inset row" style={{ marginBottom: 16, padding: '10px 14px' }}>
              <Package size={14} style={{ color: 'var(--accent)' }} aria-hidden />
              <strong>{tpl.name}</strong>
              <span className="dim">{tpl.description}</span>
              {tpl.notes && <span className="badge warn" style={{ marginLeft: 'auto' }}>{tpl.notes}</span>}
            </div>
          )}

          <Field label="App name"
            error={name.trim() !== '' && dnsLabel(name) !== name.trim().toLowerCase()
              ? `Will be published as "${dnsLabel(name)}". URLs only allow lowercase letters, numbers, and dashes.`
              : undefined}>
            {(p) => (
              <input className="input" {...p} placeholder="my-app" value={name} autoFocus
                onChange={(e) => setName(e.target.value)} />
            )}
          </Field>

          {isCompose ? (
            <Field
              label={<>Web port {suggesting && <span className="faint">· finding a free one…</span>}</>}
              hint="The stack publishes its own ports; RackOps routes to this one. Auto-picked to avoid clashes.">
              {(p) => (
                <input className="input mono" {...p} placeholder="8080" value={hostPort}
                  onChange={(e) => { setHostPort(e.target.value.replace(/\D/g, '')); setHostTouched(true); }} />
              )}
            </Field>
          ) : (
            <div className="grid-2">
              <Field label="App listens on port"
                hint={tpl ? 'From the template: the port inside the container.' : 'The port your app listens on inside the container.'}>
                {(p) => (
                  <input className="input mono" {...p} placeholder="8080" value={containerPort}
                    onChange={(e) => setContainerPort(e.target.value.replace(/\D/g, ''))} />
                )}
              </Field>
              <Field
                label={<>Reachable on host port {suggesting && <span className="faint">· finding a free one…</span>}</>}
                hint={<>How you&rsquo;ll reach it, auto-picked to avoid clashes.
                  {containerPort && hostPort && ` Maps ${hostPort} → ${containerPort}.`}</>}>
                {(p) => (
                  <input className="input mono" {...p} placeholder="auto" value={hostPort}
                    onChange={(e) => { setHostPort(e.target.value.replace(/\D/g, '')); setHostTouched(true); }} />
                )}
              </Field>
            </div>
          )}

          <Field label="Machine">
            {(p) => (
              <select className="input" {...p} value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
                <option value="auto">Auto (recommended)</option>
                {nodes.map((n) => (
                  <option key={n.id} value={n.id} disabled={n.status !== 'online'}>
                    {n.name}{n.status !== 'online' ? ' (offline)' : ''}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {nodeId === 'auto' && (
            <div style={{ minHeight: PLACEMENT_SLOT, marginBottom: 14 }}>
              {!scoresLoaded && <Skeleton height={PLACEMENT_SLOT} radius="var(--r-lg)" />}
              {scoresLoaded && best && (
                <div className="card accent">
                  <div className="row">
                    <Sparkles size={14} style={{ color: 'var(--accent)' }} aria-hidden />
                    <strong>Will run on {nodeName(best.nodeId)}</strong>
                    <span className="badge accent">score {best.score}</span>
                  </div>
                  <div className="faint" style={{ marginTop: 6 }}>{best.reasons.join(', ')}</div>
                  <div className="faint" style={{ marginTop: 4 }}>
                    Auto scored every online machine in this organization on CPU, memory, thermal, and
                    storage headroom (plus GPU when the app needs one). Nothing deploys until you launch.
                  </div>
                </div>
              )}
              {scoresLoaded && !best && scores.length > 0 && (
                <div className="card crit">
                  <strong style={{ color: 'var(--crit)' }}>No machine can run this right now.</strong>
                  <div className="dim" style={{ marginTop: 4 }}>
                    {scores[0]?.reasons.join(', ') || 'Check that at least one machine is online with enough free memory.'}
                  </div>
                </div>
              )}
              {scoresLoaded && scores.length === 0 && (
                <div className="card inset">
                  <div className="faint">
                    The placement preview didn&rsquo;t load. Auto still picks the best machine when you launch.
                  </div>
                </div>
              )}
            </div>
          )}

          {/* A repeating row can't use one <label>, so each input names itself
              and its own variable: "value for POSTGRES_PASSWORD", not "edit text". */}
          <fieldset className="field field-set">
            <legend>Environment variables{source === 'template' && tpl && (tpl.env ?? []).length > 0
              ? ' (pre-filled from the template)' : ' (optional)'}</legend>
            {envVars.map((v, i) => (
              <div className="row" key={v._id}>
                {/* Enter in a repeating row must never fire the whole launch. */}
                <input className="input mono" placeholder="KEY" value={v.key} aria-label={`Variable ${i + 1} name`}
                  onKeyDown={(e) => { if (e.key === 'Enter') e.preventDefault(); }}
                  onChange={(e) => setEnvVars(envVars.map((x) => (x._id === v._id ? { ...x, key: e.target.value } : x)))} />
                <input className="input mono" placeholder={v.secret ? 'auto-generated if blank' : 'value'}
                  type={v.secret ? 'password' : 'text'} value={v.value}
                  aria-label={v.key ? `Value for ${v.key}` : `Variable ${i + 1} value`}
                  onKeyDown={(e) => { if (e.key === 'Enter') e.preventDefault(); }}
                  onChange={(e) => setEnvVars(envVars.map((x) => (x._id === v._id ? { ...x, value: e.target.value } : x)))} />
                <button type="button" className="btn sm" aria-pressed={v.secret}
                  title={v.secret ? 'Shown as secret' : 'Mask as secret'}
                  aria-label={v.secret ? `Stop masking ${v.key || 'this variable'}` : `Mask ${v.key || 'this variable'} as secret`}
                  onClick={() => setEnvVars(envVars.map((x) => (x._id === v._id ? { ...x, secret: !x.secret } : x)))}>
                  <KeyRound size={12} style={v.secret ? { color: 'var(--accent)' } : undefined} />
                </button>
                <button type="button" className="btn sm danger" aria-label={`Remove ${v.key || `variable ${i + 1}`}`}
                  onClick={() => setEnvVars(envVars.filter((x) => x._id !== v._id))}><Trash2 size={12} /></button>
              </div>
            ))}
            <button type="button" className="btn sm" style={{ alignSelf: 'flex-start' }}
              onClick={() => setEnvVars([...envVars, envRow({ key: '', value: '', secret: false })])}>
              <Plus size={12} /> Add variable
            </button>
          </fieldset>

          <div className="row spread" style={{ marginTop: 18 }}>
            <button type="button" className="btn ghost" onClick={() => setStep(1)}>
              <ArrowLeft size={13} aria-hidden /> Back
            </button>
            <button type="submit" className="btn primary" disabled={busy || !name.trim()}>
              {busy ? <Spinner /> : <Rocket size={14} aria-hidden />} {busy ? 'Starting…' : 'Launch'}
            </button>
          </div>
        </form>
      )}

      {launchId && (
        <>
          <LaunchProgress serviceId={launchId} />
          {/* Leaving is always safe: the deploy runs on the server, not this tab. */}
          <div className="row spread" style={{ marginTop: 18 }}>
            <Link to="/apps" className="btn ghost" title="The deploy keeps running on the server">
              <ArrowLeft size={13} aria-hidden /> Back to apps
            </Link>
            <Link to={`/apps/${launchId}`} className="btn primary">Open app page</Link>
          </div>
        </>
      )}
    </div>
  );
}

// LaunchProgress watches the new app go live: status timeline + build log,
// ending on the "make it public" payoff instead of a closed dialog. It reads
// everything from the serviceId in the URL, so a refresh resumes cleanly.
function LaunchProgress({ serviceId }: { serviceId: string }) {
  const { nodes, ingress, services } = useData();
  const [polled, setPolled] = useState<Service | null>(null);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [liveLog, setLiveLog] = useState('');
  const logRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true); // keep scrolling to the bottom unless the user scrolled up

  // The fleet stream usually knows the service already (instant paint after a
  // refresh); polling keeps status and deployments converging regardless.
  const svc = polled ?? services.find((s) => s.id === serviceId) ?? null;

  useEffect(() => {
    let alive = true;
    const poll = async () => {
      try {
        const s = await api.service(serviceId);
        if (!alive) return;
        setPolled(s);
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

  // Stream the build/pull/run output live as it happens, instead of waiting for
  // the whole deploy to finish and land in the polled deployment record.
  useEffect(() => {
    const unsub = subscribeLogChunks(
      (c: LogChunk) => c.kind === 'deploy' && c.serviceId === serviceId,
      (c: LogChunk) => { if (c.text) setLiveLog((prev) => (prev + c.text).slice(-200_000)); },
    );
    return unsub;
  }, [serviceId]);

  // Auto-scroll to the newest line only while the reader is already at the
  // bottom; scrolling up to reread an error must stick.
  useEffect(() => {
    const el = logRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [liveLog, deploys]);

  const onScroll = () => {
    const el = logRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

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
          <div style={{ fontWeight: 600, fontSize: 15 }}>
            {!svc ? 'Provisioning…'
              : svc.status === 'running' ? `${svc.name} is live`
              : svc.status === 'failed' ? `${svc.name} failed to start`
              : svc.status === 'building' ? 'Building…'
              : 'Deploying…'}
          </div>
          <div className="dim">
            {svc?.status === 'running' && node ? `running on ${node.name}`
              : svc?.status === 'failed' ? 'the full output is below. Fix and redeploy from the app page.'
              : 'pulling the image and starting the container'}
          </div>
        </div>
      </div>

      {svc?.status === 'running' && (
        <div className="card ok" style={{ marginBottom: 14 }}>
          <div className="row" style={{ marginBottom: 10 }}>
            <CheckCircle2 size={15} style={{ color: 'var(--ok)' }} aria-hidden />
            <strong>Reach it</strong>
          </div>
          <div className="row wrap" style={{ marginBottom: 12 }}>
            {urls.map((u) => <UrlChip key={u.url} url={u.url} />)}
            {node && svc.port > 0 && <UrlChip url={`http://${node.ip}:${svc.port}`} />}
            {!svc.port && <span className="faint">no port published (this app is internal)</span>}
          </div>
          <Link to={`/apps/${svc.id}/networking`} className="btn">
            <Globe size={13} aria-hidden /> Make it public
          </Link>
        </div>
      )}

      {svc?.status === 'failed' && (
        <div className="row" style={{ marginBottom: 14 }}>
          <Link to={`/apps/${svc.id}/logs`} className="btn">
            <ScrollText size={13} aria-hidden /> Open logs
          </Link>
        </div>
      )}

      <div className="eyebrow" style={{ marginBottom: 6 }}>Deployment output</div>
      <div className="terminal" ref={logRef} onScroll={onScroll} style={{ maxHeight: 400 }}>
        {liveLog || latest?.log || 'waiting for the machine to pick up the job…'}
      </div>
    </>
  );
}
