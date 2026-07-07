import type { Node } from '@rackops/api';
import { MetricBar } from './MetricBar';
import { StatusDot } from './StatusDot';
import { formatBytes, formatUptime } from './format';
import './NodeCard.css';

export function NodeCard({ node }: { node: Node }) {
  const s = node.latest;
  return (
    <article className="ro-node-card">
      <header className="ro-node-head">
        <div>
          <h3 className="ro-node-hostname">{s.hostname}</h3>
          <p className="ro-node-platform">
            {s.platform} · {s.arch} · up {formatUptime(s.uptimeSec)}
          </p>
        </div>
        <StatusDot status={node.status} />
      </header>

      <div className="ro-node-metrics">
        <MetricBar
          label={`CPU — ${s.cpu.model || 'unknown'} (${s.cpu.cores} cores)`}
          pct={s.cpu.usagePct}
        />
        <MetricBar
          label="Memory"
          pct={s.memory.usedPct}
          detail={`${formatBytes(s.memory.usedBytes)} / ${formatBytes(s.memory.totalBytes)}`}
        />
        {s.disks.map((disk) => (
          <MetricBar
            key={disk.mountpoint}
            label={`Disk ${disk.mountpoint} (${disk.fs})`}
            pct={disk.usedPct}
            detail={`${formatBytes(disk.usedBytes)} / ${formatBytes(disk.totalBytes)}`}
          />
        ))}
      </div>

      {s.network.length > 0 && (
        <footer className="ro-node-net">
          {s.network.map((iface) => (
            <span key={iface.name} className="ro-node-iface">
              {iface.name} <code>{iface.ipv4}</code>
            </span>
          ))}
        </footer>
      )}
    </article>
  );
}
