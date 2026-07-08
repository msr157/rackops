import { useEffect, useState } from 'react';
import { Rocket, Sparkles, X, Plus, Trash2 } from 'lucide-react';
import { api } from '../api';
import type { Node, Project, Environment, PlacementScore, EnvVar } from '../api';

export function DeployModal({ nodes, projects, envs, onClose, onDeployed }: {
  nodes: Node[]; projects: Project[]; envs: Environment[];
  onClose: () => void; onDeployed: () => void;
}) {
  const [name, setName] = useState('');
  const [image, setImage] = useState('');
  const [port, setPort] = useState('');
  const [projectId, setProjectId] = useState(projects[0]?.id ?? '');
  const [projEnvs, setProjEnvs] = useState<Environment[]>(envs);
  const [envId, setEnvId] = useState('');
  const [nodeId, setNodeId] = useState('auto');
  const [envVars, setEnvVars] = useState<EnvVar[]>([]);
  const [scores, setScores] = useState<PlacementScore[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  // Refresh environments when the project changes.
  useEffect(() => {
    if (!projectId) return;
    api.envs(projectId).then((es) => {
      setProjEnvs(es ?? []);
      const prod = (es ?? []).find((e) => e.name === 'production');
      setEnvId(prod?.id ?? es?.[0]?.id ?? '');
    }).catch(() => {});
  }, [projectId]);

  // Live placement recommendation.
  useEffect(() => {
    api.placement({}).then(setScores).catch(() => {});
  }, []);

  const best = scores.find((s) => s.viable);
  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;

  const deploy = async () => {
    setError('');
    if (!name.trim() || !image.trim()) { setError('Name and image are required.'); return; }
    setBusy(true);
    try {
      await api.createService({
        name: name.trim(), kind: 'app', image: image.trim(),
        port: port ? parseInt(port, 10) : 0,
        projectId, envId, nodeId,
        env: envVars.filter((v) => v.key),
      });
      onDeployed();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="row spread" style={{ marginBottom: 16 }}>
          <h2 style={{ margin: 0 }}><Rocket size={16} style={{ verticalAlign: -2 }} /> Deploy Application</h2>
          <button className="btn sm" onClick={onClose}><X size={13} /></button>
        </div>

        <div className="field">
          <label>Service name</label>
          <input className="input" placeholder="my-app" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="field">
          <label>Container image</label>
          <input className="input" placeholder="docker.io/library/nginx:alpine" value={image} onChange={(e) => setImage(e.target.value)} />
          <span className="faint">Git & compose deploys arrive in the next milestone — image deploys are live now.</span>
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
        <div className="grid-2">
          <div className="field">
            <label>Target node</label>
            <select className="input" value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
              <option value="auto">⚡ Auto — let RackOps decide</option>
              {nodes.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
            </select>
          </div>
          <div className="field">
            <label>Internal port (optional)</label>
            <input className="input" placeholder="8080" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))} />
          </div>
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

        <div className="field">
          <label>Environment variables</label>
          {envVars.map((v, i) => (
            <div className="row" key={i}>
              <input className="input" placeholder="KEY" value={v.key}
                onChange={(e) => setEnvVars(envVars.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))} />
              <input className="input" placeholder="value" type={v.secret ? 'password' : 'text'} value={v.value}
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
