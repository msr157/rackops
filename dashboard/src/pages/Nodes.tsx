import { useEffect, useState } from 'react';
import { Cpu, MemoryStick, Thermometer, Network, Zap, Clock } from 'lucide-react';
import type { Node, Service } from '../api';
import { StatusBadge } from '../App';

// Keep a short rolling history per node for sparklines.
const histories = new Map<string, number[]>();

export function NodesPage({ nodes, services }: { nodes: Node[]; services: Service[] }) {
  const [selected, setSelected] = useState<string>('');
  const node = nodes.find((n) => n.id === selected) ?? nodes[0];

  useEffect(() => {
    for (const n of nodes) {
      const h = histories.get(n.id) ?? [];
      h.push(n.metrics.cpuPct);
      if (h.length > 40) h.shift();
      histories.set(n.id, h);
    }
  }, [nodes]);

  if (!node) return <div className="empty">No nodes connected.</div>;

  const m = node.metrics;
  const hw = node.hardware;
  const uptime = formatUptime(m.uptimeSec);

  return (
    <>
      <div className="page-title">Nodes & Hardware</div>
      <div className="page-sub">Continuous hardware discovery across the fleet</div>

      <div className="row" style={{ marginBottom: 18, flexWrap: 'wrap' }}>
        {nodes.map((n) => (
          <button key={n.id} className={`btn ${node.id === n.id ? 'primary' : ''}`} onClick={() => setSelected(n.id)}>
            {n.name}
          </button>
        ))}
      </div>

      <div className="card" style={{ marginBottom: 14 }}>
        <div className="row spread">
          <div className="row">
            <strong style={{ fontSize: 16 }}>{node.name}</strong>
            <StatusBadge status={node.status} />
            <span className="tag">{node.os}</span>
            <span className="tag">{node.arch}</span>
            <span className="tag">kernel {node.kernel}</span>
          </div>
          <span className="dim row"><Clock size={13} /> up {uptime}</span>
        </div>
      </div>

      <div className="kpi-row">
        <MetricCard icon={<Cpu size={13} />} label="CPU" value={`${m.cpuPct.toFixed(1)}%`}
          sub={`${hw.cpuCores}C/${hw.cpuThreads}T · load ${m.load1.toFixed(2)}`} pct={m.cpuPct} history={histories.get(node.id)} />
        <MetricCard icon={<MemoryStick size={13} />} label="Memory" value={`${(m.memUsedMb / 1024).toFixed(1)} GB`}
          sub={`of ${(hw.memTotalMb / 1024).toFixed(1)} GB (${m.memPct.toFixed(0)}%)`} pct={m.memPct} />
        <MetricCard icon={<Thermometer size={13} />} label="Package Temp" value={`${m.tempC.toFixed(0)}°C`}
          sub={m.tempC > 85 ? 'THROTTLING RISK' : m.tempC > 70 ? 'warm' : 'healthy'} pct={m.tempC} />
        <MetricCard icon={<Zap size={13} />} label="Processes" value={`${m.processCount}`}
          sub={`${m.containers} containers`} />
      </div>

      <div className="section-title"><Cpu size={15} /> {hw.cpuModel}</div>

      {(hw.gpus ?? []).length > 0 && (
        <>
          <div className="section-title">GPUs & Accelerators</div>
          <div className="grid-2">
            {(hw.gpus ?? []).map((g) => (
              <div className="card" key={g.index}>
                <div className="row spread">
                  <strong>{g.model.replace(/Advanced Micro Devices.*\[AMD\/ATI\]\s*/, 'AMD ')}</strong>
                  <span className={`badge ${g.vendor === 'nvidia' ? 'green' : 'purple'}`}>{g.vendor.toUpperCase()}</span>
                </div>
                <div className="grid-3" style={{ marginTop: 12 }}>
                  <div>
                    <div className="row spread dim"><span>VRAM</span><span className="mono">{g.vramUsedMb}/{g.vramTotalMb} MB</span></div>
                    <div className="bar"><div className="bar-fill" style={{ width: `${g.vramTotalMb ? (g.vramUsedMb / g.vramTotalMb) * 100 : 0}%` }} /></div>
                  </div>
                  <div>
                    <div className="row spread dim"><span>Util</span><span className="mono">{g.utilPct.toFixed(0)}%</span></div>
                    <div className="bar"><div className="bar-fill" style={{ width: `${g.utilPct}%` }} /></div>
                  </div>
                  <div>
                    <div className="row spread dim"><span>Temp / Power</span><span className="mono">{g.tempC.toFixed(0)}°C · {g.powerW.toFixed(0)}W</span></div>
                    <div className="bar"><div className={`bar-fill ${g.tempC > 80 ? 'crit' : ''}`} style={{ width: `${Math.min(g.tempC, 100)}%` }} /></div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      <div className="section-title"><Network size={15} /> Network Interfaces</div>
      <div className="grid-3">
        {(hw.nics ?? []).map((nic) => (
          <div className="card" key={nic.name}>
            <div className="row spread">
              <strong className="mono">{nic.name}</strong>
              <span className="faint mono">{nic.mac}</span>
            </div>
            <div className="row" style={{ marginTop: 8, gap: 16 }}>
              <span className="dim mono">↓ {nic.rxMbps.toFixed(2)} MB/s</span>
              <span className="dim mono">↑ {nic.txMbps.toFixed(2)} MB/s</span>
              {nic.speedMb > 0 && <span className="faint">{nic.speedMb} Mb link</span>}
            </div>
          </div>
        ))}
      </div>

      <div className="section-title">Services on this node</div>
      <div className="row" style={{ flexWrap: 'wrap' }}>
        {services.filter((s) => s.nodeId === node.id).map((s) => (
          <span key={s.id} className="badge gray">{s.name} · {s.status}</span>
        ))}
        {services.filter((s) => s.nodeId === node.id).length === 0 && <span className="faint">none</span>}
      </div>
    </>
  );
}

function MetricCard({ icon, label, value, sub, pct, history }: {
  icon: React.ReactNode; label: string; value: string; sub?: string; pct?: number; history?: number[];
}) {
  const cls = pct !== undefined && pct > 90 ? 'crit' : pct !== undefined && pct > 75 ? 'warn' : '';
  return (
    <div className="card">
      <div className="kpi-label">{icon} {label}</div>
      <div className="kpi-value">{value}</div>
      {sub && <div className="faint" style={{ marginTop: 2 }}>{sub}</div>}
      {history && history.length > 1 ? (
        <div className="spark" style={{ marginTop: 8 }}>
          {history.map((v, i) => <div key={i} style={{ height: `${Math.max(v, 3)}%` }} />)}
        </div>
      ) : pct !== undefined ? (
        <div className="bar"><div className={`bar-fill ${cls}`} style={{ width: `${Math.min(pct, 100)}%` }} /></div>
      ) : null}
    </div>
  );
}

function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), min = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${min}m`;
  return `${min}m`;
}
