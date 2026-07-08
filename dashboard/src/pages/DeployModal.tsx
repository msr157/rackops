import { useEffect, useMemo, useState } from 'react';
import { Rocket, Sparkles, X, Plus, Trash2, Package, GitBranch, Container, Layers, Gamepad2, Database, Wrench, Globe, Bot, Server, Home } from 'lucide-react';
import { api } from '../api';
import type { Node, Project, Environment, PlacementScore, EnvVar, Template } from '../api';

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

export function DeployModal({ nodes, projects, envs, onClose, onDeployed }: {
  nodes: Node[]; projects: Project[]; envs: Environment[];
  onClose: () => void; onDeployed: () => void;
}) {
  const [source, setSource] = useState<Source>('template');
  const [templates, setTemplates] = useState<Template[]>([]);
  const [category, setCategory] = useState('');
  const [search, setSearch] = useState('');
  const [tpl, setTpl] = useState<Template | null>(null);

  const [name, setName] = useState('');
  const [image, setImage] = useState('');
  const [gitRepo, setGitRepo] = useState('');
  const [gitBranch, setGitBranch] = useState('main');
  const [gitToken, setGitToken] = useState('');
  const [composeYml, setComposeYml] = useState('');
  const [port, setPort] = useState('');
  const [projectId, setProjectId] = useState(projects[0]?.id ?? '');
  const [projEnvs, setProjEnvs] = useState<Environment[]>(envs);
  const [envId, setEnvId] = useState('');
  const [nodeId, setNodeId] = useState('auto');
  const [envVars, setEnvVars] = useState<EnvVar[]>([]);
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => { api.templates().then((t) => setTemplates(t ?? [])).catch(() => {}); }, []);

  useEffect(() => {
    if (!projectId) return;
    api.envs(projectId).then((es) => {
      setProjEnvs(es ?? []);
      const prod = (es ?? []).find((e) => e.name === 'production');
      setEnvId(prod?.id ?? es?.[0]?.id ?? '');
    }).catch(() => {});
  }, [projectId]);

  // Placement preview refreshes when the selected template changes (mem/GPU needs).
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
    setPort(String(t.port));
    // Pre-fill env form from template defs (secrets left blank unless defaulted).
    setEnvVars((t.env ?? []).map((d) => ({ key: d.key, value: d.default, secret: d.secret })));
  };

  const best = scores.find((s) => s.viable);
  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;

  const deploy = async () => {
    setError('');
    if (!name.trim()) { setError('Service name is required.'); return; }
    const body: Record<string, unknown> = {
      name: name.trim(), projectId, envId, nodeId,
      port: port ? parseInt(port, 10) : 0,
      env: envVars.filter((v) => v.key),
    };
    if (source === 'template') {
      if (!tpl) { setError('Pick a template.'); return; }
      body.templateId = tpl.id;
      body.placement = { memMb: tpl.memMb, needsGpu: !!tpl.gpu };
    } else if (source === 'image') {
      if (!image.trim()) { setError('Container image is required.'); return; }
      body.kind = 'app'; body.image = image.trim();
    } else if (source === 'git') {
      if (!gitRepo.trim()) { setError('Repository URL is required.'); return; }
      body.kind = 'app'; body.gitRepo = gitRepo.trim(); body.gitBranch = gitBranch.trim(); body.gitToken = gitToken;
    } else {
      if (!composeYml.trim()) { setError('Compose YAML is required.'); return; }
      body.kind = 'compose'; body.composeYml = composeYml;
    }
    setBusy(true);
    try {
      await api.createService(body);
      onDeployed();
    } catch (e) { setError(String(e)); setBusy(false); }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={{ width: 720 }} onClick={(e) => e.stopPropagation()}>
        <div className="row spread" style={{ marginBottom: 16 }}>
          <h2 style={{ margin: 0 }}><Rocket size={16} style={{ verticalAlign: -2 }} /> Deploy</h2>
          <button className="btn sm" onClick={onClose}><X size={13} /></button>
        </div>

        <div className="row" style={{ marginBottom: 16 }}>
          {([
            ['template', <Package size={13} key="t" />, 'Template'],
            ['image', <Container size={13} key="i" />, 'Image'],
            ['git', <GitBranch size={13} key="g" />, 'Git Repo'],
            ['compose', <Layers size={13} key="c" />, 'Compose'],
          ] as [Source, React.ReactNode, string][]).map(([key, icon, label]) => (
            <button key={key} className={`btn ${source === key ? 'primary' : ''}`} onClick={() => setSource(key)}>
              {icon} {label}
            </button>
          ))}
        </div>

        {source === 'template' && (
          <>
            <div className="row" style={{ marginBottom: 10, flexWrap: 'wrap' }}>
              {CATEGORIES.map((c) => (
                <button key={c.key} className={`btn sm ${category === c.key ? 'primary' : ''}`} onClick={() => setCategory(c.key)}>
                  {c.icon} {c.label}
                </button>
              ))}
            </div>
            <input className="input" placeholder="Search 45+ templates…" value={search}
              onChange={(e) => setSearch(e.target.value)} style={{ marginBottom: 10 }} />
            <div style={{ maxHeight: 220, overflowY: 'auto', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 14 }}>
              {filtered.map((t) => (
                <button key={t.id} className="card" style={{
                  cursor: 'pointer', textAlign: 'left', fontFamily: 'inherit', padding: 12,
                  borderColor: tpl?.id === t.id ? 'var(--accent)' : 'var(--border)',
                }} onClick={() => pickTemplate(t)}>
                  <div className="row spread">
                    <strong style={{ fontSize: 13, color: 'var(--text)' }}>{t.name}</strong>
                    <span className="tag">{t.category}</span>
                  </div>
                  <div className="faint" style={{ marginTop: 4 }}>{t.description}</div>
                  {t.gpu && <span className="badge purple" style={{ marginTop: 6 }}>GPU</span>}
                </button>
              ))}
            </div>
            {tpl?.notes && <div className="badge yellow" style={{ marginBottom: 12 }}>{tpl.notes}</div>}
          </>
        )}

        {source === 'image' && (
          <div className="field">
            <label>Container image</label>
            <input className="input" placeholder="nginx:alpine" value={image} onChange={(e) => setImage(e.target.value)} />
          </div>
        )}

        {source === 'git' && (
          <>
            <div className="field">
              <label>Repository URL</label>
              <input className="input" placeholder="https://github.com/user/repo" value={gitRepo} onChange={(e) => setGitRepo(e.target.value)} />
              <span className="faint">Dockerfile used if present; otherwise the stack (Node/Go/Python/static) is auto-detected.</span>
            </div>
            <div className="grid-2">
              <div className="field">
                <label>Branch</label>
                <input className="input" value={gitBranch} onChange={(e) => setGitBranch(e.target.value)} />
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
            <textarea className="input mono" rows={8} placeholder={'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8088:80"'}
              value={composeYml} onChange={(e) => setComposeYml(e.target.value)} style={{ resize: 'vertical' }} />
          </div>
        )}

        <div className="grid-2">
          <div className="field">
            <label>Service name</label>
            <input className="input" placeholder="my-app" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="field">
            <label>Port {source === 'template' && tpl ? `(template default: ${tpl.port})` : '(optional)'}</label>
            <input className="input" placeholder="8080" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))} />
          </div>
        </div>
        <div className="grid-2">
          <div className="field">
            <label>Project</label>
            <select className="input" value={projectId} onChange={(e) => setProjectId(e.target.value)}>
              {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </div>
          <div className="field">
            <label>Environment</label>
            <select className="input" value={envId} onChange={(e) => setEnvId(e.target.value)}>
              {projEnvs.map((e) => <option key={e.id} value={e.id}>{e.name}</option>)}
            </select>
          </div>
        </div>
        <div className="field">
          <label>Target node</label>
          <select className="input" value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
            <option value="auto">⚡ Auto — let RackOps decide</option>
            {nodes.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
          </select>
        </div>

        {nodeId === 'auto' && best && (
          <div className="card" style={{ marginBottom: 14, borderColor: 'var(--accent)' }}>
            <div className="row">
              <Sparkles size={14} color="var(--accent)" />
              <strong>Recommended: {nodeName(best.nodeId)}</strong>
              <span className="badge cyan">score {best.score}</span>
            </div>
            <div className="faint" style={{ marginTop: 6 }}>{best.reasons.join(' · ')}</div>
          </div>
        )}
        {nodeId === 'auto' && !best && scores.length > 0 && (
          <div className="badge red" style={{ marginBottom: 14 }}>No node satisfies this workload's requirements</div>
        )}

        <div className="field">
          <label>Environment variables</label>
          {envVars.map((v, i) => (
            <div className="row" key={i}>
              <input className="input" placeholder="KEY" value={v.key}
                onChange={(e) => setEnvVars(envVars.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))} />
              <input className="input" placeholder={v.secret ? 'auto-generated if blank' : 'value'} type={v.secret ? 'password' : 'text'} value={v.value}
                onChange={(e) => setEnvVars(envVars.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} />
              <button className="btn sm danger" onClick={() => setEnvVars(envVars.filter((_, j) => j !== i))}><Trash2 size={12} /></button>
            </div>
          ))}
          <button className="btn sm" style={{ alignSelf: 'flex-start' }}
            onClick={() => setEnvVars([...envVars, { key: '', value: '', secret: false }])}>
            <Plus size={12} /> Add variable
          </button>
        </div>

        {error && <div className="badge red" style={{ marginBottom: 12, display: 'flex' }}>{error}</div>}

        <button className="btn primary" style={{ width: '100%', justifyContent: 'center' }} disabled={busy} onClick={deploy}>
          <Rocket size={14} /> {busy ? 'Deploying…' : 'Provision & Deploy'}
        </button>
      </div>
    </div>
  );
}
