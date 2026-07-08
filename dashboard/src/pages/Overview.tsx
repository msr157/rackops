import { Server, Boxes, MemoryStick, HardDrive, Gauge, Radio } from 'lucide-react';
import type { Node, Overview, EventItem, Service } from '../api';
import { StatusBadge } from '../App';

export function OverviewPage({ overview, nodes, events, services }: {
  overview: Overview | null; nodes: Node[]; events: EventItem[]; services: Service[];
}) {
  const o = overview;
  return (
    <>
      <div className="page-title">Cluster Overview</div>
      <div className="page-sub">Real-time state of every machine in your fleet</div>

      <div className="kpi-row">
        <Kpi icon={<Server size={13} />} label="Nodes Online" value={`${o?.nodesOnline ?? 0}/${o?.nodesTotal ?? 0}`} />
        <Kpi icon={<Boxes size={13} />} label="Services Running" value={`${o?.servicesUp ?? 0}/${o?.servicesTotal ?? 0}`} />
        <Kpi icon={<Gauge size={13} />} label="Fleet CPU" value={`${o?.avgCpuPct ?? 0}`} unit="%" />
        <Kpi icon={<MemoryStick size={13} />} label="Fleet Memory"
          value={`${((o?.usedMemMb ?? 0) / 1024).toFixed(1)}`} unit={`/ ${((o?.totalMemMb ?? 0) / 1024).toFixed(0)} GB`} />
        <Kpi icon={<HardDrive size={13} />} label="Pooled Storage"
          value={`${((o?.usedStorageGb ?? 0)).toFixed(0)}`} unit={`/ ${(o?.totalStorageGb ?? 0).toFixed(0)} GB`} />
      </div>

      <div className="grid-2">
        <div>
          <div className="section-title"><Server size={15} /> Fleet Nodes</div>
          <div className="node-card">
            {nodes.map((n) => <NodeRow key={n.id} node={n} services={services} />)}
            {nodes.length === 0 && <div className="empty">No nodes yet — add one with the button below-left.</div>}
          </div>
        </div>
        <div>
          <div className="section-title">
            <Radio size={15} /> System Events
            <span className="badge cyan" style={{ marginLeft: 'auto' }}>LIVE</span>
          </div>
          <div className="terminal">
            {events.map((e) => (
              <div key={e.id}>
                <span className="t-time">{new Date(e.createdAt).toLocaleTimeString()}</span>
                <span className="t-kind">[{e.kind}]</span>
                {e.message}
              </div>
            ))}
            {events.length === 0 && <span className="faint">no events yet…</span>}
          </div>
        </div>
      </div>
    </>
  );
}

function Kpi({ icon, label, value, unit }: { icon: React.ReactNode; label: string; value: string; unit?: string }) {
  return (
    <div className="card">
      <div className="kpi-label">{icon} {label}</div>
      <div className="kpi-value">{value} {unit && <span className="unit">{unit}</span>}</div>
    </div>
  );
}

function NodeRow({ node, services }: { node: Node; services: Service[] }) {
  const m = node.metrics;
  const cls = (v: number) => (v > 90 ? 'crit' : v > 75 ? 'warn' : '');
  const svcCount = services.filter((s) => s.nodeId === node.id && s.status === 'running').length;
  return (
    <div className="card">
      <div className="row spread" style={{ marginBottom: 8 }}>
        <div className="row">
          <strong>{node.name}</strong>
          <span className="tag">{node.role}</span>
          <StatusBadge status={node.status} />
        </div>
        <span className="dim mono">{node.ip}</span>
      </div>
      <div className="faint" style={{ marginBottom: 10 }}>{node.hardware.cpuModel}</div>
      <div className="grid-3">
        <div>
          <div className="row spread dim"><span>CPU</span><span className="mono">{m.cpuPct.toFixed(0)}%</span></div>
          <div className="bar"><div className={`bar-fill ${cls(m.cpuPct)}`} style={{ width: `${m.cpuPct}%` }} /></div>
        </div>
        <div>
          <div className="row spread dim"><span>Memory</span><span className="mono">{m.memPct.toFixed(0)}%</span></div>
          <div className="bar"><div className={`bar-fill ${cls(m.memPct)}`} style={{ width: `${m.memPct}%` }} /></div>
        </div>
        <div>
          <div className="row spread dim"><span>Temp</span><span className="mono">{m.tempC.toFixed(0)}°C</span></div>
          <div className="bar"><div className={`bar-fill ${cls(m.tempC)}`} style={{ width: `${Math.min(m.tempC, 100)}%` }} /></div>
        </div>
      </div>
      <div className="row" style={{ marginTop: 10, gap: 14 }}>
        <span className="faint">{svcCount} services</span>
        {(node.hardware.gpus ?? []).map((g) => (
          <span key={g.index} className="badge purple">{g.model.replace(/Advanced Micro Devices.*\[AMD\/ATI\]\s*/, 'AMD ')} · {g.tempC.toFixed(0)}°C</span>
        ))}
      </div>
    </div>
  );
}
