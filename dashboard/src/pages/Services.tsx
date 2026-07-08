import { useState } from 'react';
import { Play, Square, RotateCw, Trash2, Rocket, Globe, Cpu, ScrollText, X } from 'lucide-react';
import { api } from '../api';
import type { Node, Service, Project, Environment } from '../api';
import { StatusBadge } from '../App';

export function ServicesPage({ services, nodes, projects, envs, onDeploy, onChanged }: {
  services: Service[]; nodes: Node[]; projects: Project[]; envs: Environment[];
  onDeploy: () => void; onChanged: () => void;
}) {
  const [projFilter, setProjFilter] = useState('');
  const [envFilter, setEnvFilter] = useState('');
  const [busy, setBusy] = useState('');
  const [logsFor, setLogsFor] = useState<Service | null>(null);
  const [logs, setLogs] = useState('');
  const [logsLoading, setLogsLoading] = useState(false);

  const showLogs = async (s: Service) => {
    setLogsFor(s); setLogs(''); setLogsLoading(true);
    try {
      const r = await api.serviceLogs(s.id);
      setLogs(r.logs || '(no output)');
    } catch (e) { setLogs(String(e)); }
    setLogsLoading(false);
  };

  const filtered = services.filter((s) =>
    (!projFilter || s.projectId === projFilter) && (!envFilter || s.envId === envFilter));

  const act = async (id: string, action: string) => {
    setBusy(id + action);
    try {
      if (action === 'delete') await api.deleteService(id);
      else await api.serviceAction(id, action);
      onChanged();
    } finally { setBusy(''); }
  };

  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;
  const envName = (id: string) => envs.find((e) => e.id === id)?.name ?? '';

  return (
    <>
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Apps & Services</div>
        <button className="btn primary" onClick={onDeploy}><Rocket size={14} /> Deploy App</button>
      </div>
      <div className="page-sub">Workloads across every node, placed intelligently</div>

      <div className="row" style={{ marginBottom: 18 }}>
        <select className="input" style={{ width: 220 }} value={projFilter} onChange={(e) => setProjFilter(e.target.value)}>
          <option value="">All projects</option>
          {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
        <select className="input" style={{ width: 180 }} value={envFilter} onChange={(e) => setEnvFilter(e.target.value)}>
          <option value="">All environments</option>
          {envs.map((e) => <option key={e.id} value={e.id}>{e.name}</option>)}
        </select>
      </div>

      <div className="node-card">
        {filtered.map((s) => (
          <div className="card" key={s.id}>
            <div className="row spread">
              <div className="row">
                <strong>{s.name}</strong>
                <StatusBadge status={s.status} />
                {envName(s.envId) && <span className="tag">{envName(s.envId)}</span>}
                {s.autoPlace && <span className="badge cyan"><Cpu size={11} /> auto-placed</span>}
              </div>
              <div className="row">
                <button className="btn sm" title="Logs" onClick={() => showLogs(s)}>
                  <ScrollText size={12} />
                </button>
                <button className="btn sm" disabled={busy !== ''} onClick={() => act(s.id, s.status === 'running' ? 'stop' : 'start')}>
                  {s.status === 'running' ? <Square size={12} /> : <Play size={12} />}
                </button>
                <button className="btn sm" disabled={busy !== ''} onClick={() => act(s.id, 'redeploy')} title="Redeploy">
                  <RotateCw size={12} />
                </button>
                <button className="btn sm danger" disabled={busy !== ''} onClick={() => act(s.id, 'delete')} title="Remove">
                  <Trash2 size={12} />
                </button>
              </div>
            </div>
            <div className="row" style={{ marginTop: 10, gap: 18, flexWrap: 'wrap' }}>
              <span className="dim mono">{s.image || s.gitRepo || s.kind}</span>
              <span className="dim">node: <strong>{nodeName(s.nodeId)}</strong></span>
              {s.port > 0 && <span className="dim mono"><Globe size={12} style={{ verticalAlign: -2 }} /> :{s.port}</span>}
              {s.containerId && <span className="tag">{s.containerId}</span>}
              {s.status === 'running' && (
                <span className="dim mono">CPU {s.cpuPct.toFixed(1)}% · {s.memUsedMb} MB</span>
              )}
            </div>
          </div>
        ))}
        {filtered.length === 0 && (
          <div className="empty">
            No services yet. Hit <strong>Deploy App</strong> to launch your first workload —<br />
            RackOps will pick the best node for it automatically.
          </div>
        )}
      </div>

      {logsFor && (
        <div className="modal-overlay" onClick={() => setLogsFor(null)}>
          <div className="modal" style={{ width: 800 }} onClick={(e) => e.stopPropagation()}>
            <div className="row spread" style={{ marginBottom: 14 }}>
              <h2 style={{ margin: 0 }}><ScrollText size={15} style={{ verticalAlign: -2 }} /> Logs — {logsFor.name}</h2>
              <div className="row">
                <button className="btn sm" onClick={() => showLogs(logsFor)}><RotateCw size={12} /> Refresh</button>
                <button className="btn sm" onClick={() => setLogsFor(null)}><X size={13} /></button>
              </div>
            </div>
            <div className="terminal" style={{ maxHeight: 480, whiteSpace: 'pre-wrap' }}>
              {logsLoading ? 'fetching logs from agent…' : logs}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
