import { useNodes } from '@rackops/api';
import { NodeCard } from '@rackops/ui';

const API_BASE = import.meta.env.VITE_API_URL ?? '';
const AGENT_CMD = 'go run ./cmd/agent -server http://localhost:8080 -interval 3s';

export default function App() {
  const { nodes, error, lastUpdated } = useNodes(API_BASE);
  const online = nodes.filter((n) => n.status === 'online').length;

  return (
    <div className="dash">
      <header className="dash-header">
        <div>
          <h1 className="dash-title">
            RackOps <span className="dash-edition">Lite</span>
          </h1>
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
  );
}
