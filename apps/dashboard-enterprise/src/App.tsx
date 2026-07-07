import { useEffect, useState } from 'react';
import { fetchMeta, useNodes, type Meta } from '@rackops/api';
import { NodeCard } from '@rackops/ui';

const API_BASE = import.meta.env.VITE_API_URL ?? '';
const AGENT_CMD = 'go run ./cmd/agent -server http://localhost:8081 -interval 3s';

// Enterprise features on the roadmap — wired up as the control plane grows
// past its 501 stubs (see internal/enterprise/routes.go).
const PLANNED_NAV = ['Clusters', 'Users & Roles', 'Policies', 'Audit Logs', 'GPU Scheduling'];

export default function App() {
  const { nodes, error, lastUpdated } = useNodes(API_BASE);
  const [meta, setMeta] = useState<Meta | null>(null);
  const online = nodes.filter((n) => n.status === 'online').length;

  useEffect(() => {
    fetchMeta(API_BASE)
      .then(setMeta)
      .catch(() => setMeta(null));
  }, []);

  return (
    <div className="shell">
      <nav className="nav">
        <h1 className="nav-brand">
          RackOps <span className="nav-edition">Enterprise</span>
        </h1>
        <a className="nav-item active" href="#nodes">
          Nodes
        </a>
        {PLANNED_NAV.map((item) => (
          <span key={item} className="nav-item disabled" title="Coming soon">
            {item}
            <em>soon</em>
          </span>
        ))}
        {meta && (
          <p className="nav-meta">
            {meta.edition} · v{meta.version}
          </p>
        )}
      </nav>

      <div className="dash" id="nodes">
        <header className="dash-header">
          <div>
            <h2 className="dash-title">Nodes</h2>
            <p className="dash-subtitle">
              {nodes.length} node{nodes.length === 1 ? '' : 's'} · {online} online
              {lastUpdated && ` · updated ${lastUpdated.toLocaleTimeString()}`}
            </p>
          </div>
        </header>

        {error && <p className="dash-error">Control plane unreachable: {error}</p>}

        {nodes.length === 0 && !error ? (
          <div className="dash-empty">
            <h2>No nodes yet</h2>
            <p>Start an agent on any machine to see it here:</p>
            <code>{AGENT_CMD}</code>
          </div>
        ) : (
          <main className="dash-grid">
            {nodes.map((node) => (
              <NodeCard key={node.id} node={node} />
            ))}
          </main>
        )}
      </div>
    </div>
  );
}
