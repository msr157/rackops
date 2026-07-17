import { useEffect } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  ArrowLeft, Clock, Cpu, HardDrive, MemoryStick, Network as NetworkIcon, Plus, Thermometer, Zap,
} from 'lucide-react';
import { useData } from '../data';
import { Rack } from '../components/Rack';
import { InfoTip, Led, StatusBadge, UsageBar, Kpi, EmptyState, formatUptime, shortGpu } from '../components/ui';

// MachinesPage: the physical rack, plus fleet-wide capacity.
export function MachinesPage({ onAddMachine }: { onAddMachine: () => void }) {
  const { nodes, services, overview } = useData();

  return (
    <div className="page">
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Machines</div>
        <button className="btn primary" onClick={onAddMachine}><Plus size={14} /> Add machine</button>
      </div>
      <div className="page-sub">Every box in your rack — hardware discovered automatically</div>

      {nodes.length > 0 && overview && (
        <div className="kpi-row">
          <Kpi icon={<HardDrive size={12} />} label="Machines" value={`${overview.nodesOnline}/${overview.nodesTotal}`} unit="online" />
          <Kpi icon={<Cpu size={12} />} label="Fleet CPU" value={overview.avgCpuPct} unit="%" />
          <Kpi icon={<MemoryStick size={12} />} label="Memory"
            value={(overview.usedMemMb / 1024).toFixed(1)} unit={`/ ${(overview.totalMemMb / 1024).toFixed(0)} GB`} />
          <Kpi icon={<HardDrive size={12} />} label="Storage"
            value={overview.usedStorageGb.toFixed(0)} unit={`/ ${overview.totalStorageGb.toFixed(0)} GB`} />
        </div>
      )}

      <Rack nodes={nodes} services={services} />

      {nodes.length === 0 && (
        <div style={{ marginTop: 16 }}>
          <EmptyState title="One command turns any Linux box into part of your rack">
            <div style={{ marginBottom: 14, lineHeight: 1.6 }}>
              Old PC, Raspberry Pi, VPS — install the agent and it shows up here in seconds,<br />
              hardware and all. Works behind CGNAT; only outbound connections.
            </div>
            <button className="btn primary" onClick={onAddMachine}><Plus size={13} /> Show the command</button>
          </EmptyState>
        </div>
      )}
    </div>
  );
}

// Rolling CPU history per node for the sparkline (module-level so it
// accumulates across re-renders and navigations).
const histories = new Map<string, number[]>();

export function MachineDetail() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const { nodes, services } = useData();
  const node = nodes.find((n) => n.id === id);

  useEffect(() => {
    if (!node) return;
    const h = histories.get(node.id) ?? [];
    h.push(node.metrics.cpuPct);
    if (h.length > 40) h.shift();
    histories.set(node.id, h);
  }, [node, node?.metrics.timestamp]);

  if (!node) {
    return (
      <div className="page">
        <EmptyState title="Machine not found">
          <Link to="/machines" className="btn" style={{ marginTop: 10 }}>Back to machines</Link>
        </EmptyState>
      </div>
    );
  }

  const m = node.metrics;
  const hw = node.hardware;
  const apps = services.filter((s) => s.nodeId === node.id);
  const history = histories.get(node.id);

  return (
    <div className="page">
      <div className="row" style={{ marginBottom: 4 }}>
        <button className="btn sm ghost" onClick={() => navigate('/machines')} aria-label="Back to machines"><ArrowLeft size={14} /></button>
        <div className="page-title" style={{ margin: 0 }}>{node.name}</div>
        <StatusBadge status={node.status} />
      </div>
      <div className="row wrap page-sub" style={{ gap: 8 }}>
        <span className="tag">{node.os}</span>
        <span className="tag">{node.arch}</span>
        <span className="tag">kernel {node.kernel}</span>
        <span className="dim mono">{node.ip}</span>
        {node.publicIp && <span className="dim mono">public {node.publicIp}</span>}
        <span className="dim row" style={{ gap: 5 }}><Clock size={12} /> up {formatUptime(m.uptimeSec)}</span>
      </div>

      {node.status === 'offline' && (
        <div className="card" style={{ borderColor: 'var(--crit)', marginBottom: 16 }}>
          <strong style={{ color: 'var(--crit)' }}>This machine stopped reporting.</strong>
          <div className="dim" style={{ marginTop: 4 }}>
            Check that it's powered on and connected, then make sure the agent is running:
            <code className="mono" style={{ marginLeft: 6 }}>systemctl status rackops-agent</code> (or re-run the join command).
          </div>
        </div>
      )}

      <div className="kpi-row">
        <div className="card">
          <div className="eyebrow"><Cpu size={12} /> CPU</div>
          <div className="kpi-value">{m.cpuPct.toFixed(1)}<span className="unit">%</span></div>
          <div className="faint">{hw.cpuCores}C/{hw.cpuThreads}T · load {m.load1.toFixed(2)}</div>
          {history && history.length > 1 ? (
            <div className="spark" style={{ marginTop: 8 }}>
              {history.map((v, i) => <div key={i} style={{ height: `${Math.max(v, 3)}%` }} />)}
            </div>
          ) : <UsageBar pct={m.cpuPct} />}
        </div>
        <div className="card">
          <div className="eyebrow"><MemoryStick size={12} /> Memory</div>
          <div className="kpi-value">{(m.memUsedMb / 1024).toFixed(1)}<span className="unit">/ {(hw.memTotalMb / 1024).toFixed(1)} GB</span></div>
          <div className="faint">{m.memPct.toFixed(0)}% used</div>
          <UsageBar pct={m.memPct} />
        </div>
        <div className="card">
          <div className="eyebrow">
            <Thermometer size={12} /> Package temp
            <InfoTip>
              The CPU package sensor, read straight from the hardware. Above ~85°C most CPUs slow
              themselves down to cool off — an alert opens if it stays there.
            </InfoTip>
          </div>
          <div className="kpi-value">{m.tempC.toFixed(0)}<span className="unit">°C</span></div>
          <div className="faint">{m.tempC > 85 ? 'throttling risk' : m.tempC > 70 ? 'warm' : 'healthy'}</div>
          <UsageBar pct={m.tempC} />
        </div>
        <div className="card">
          <div className="eyebrow"><Zap size={12} /> Workload</div>
          <div className="kpi-value">{m.containers}<span className="unit">containers</span></div>
          <div className="faint">{m.processCount} processes</div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 14 }}>
        <div className="eyebrow" style={{ marginBottom: 4 }}>Processor</div>
        <strong style={{ fontFamily: 'var(--display)' }}>{hw.cpuModel}</strong>
      </div>

      {(hw.gpus ?? []).length > 0 && (
        <>
          <div className="section-title">GPUs & accelerators</div>
          <div className="grid-2">
            {(hw.gpus ?? []).map((g) => (
              <div className="card" key={g.index}>
                <div className="row spread">
                  <strong>{shortGpu(g.model)}</strong>
                  <span className={`badge ${g.vendor === 'nvidia' ? 'green' : 'purple'}`}>{g.vendor.toUpperCase()}</span>
                </div>
                <div className="grid-3" style={{ marginTop: 12 }}>
                  <div>
                    <div className="row spread dim"><span>VRAM</span><span className="mono">{g.vramUsedMb}/{g.vramTotalMb} MB</span></div>
                    <UsageBar pct={g.vramTotalMb ? (g.vramUsedMb / g.vramTotalMb) * 100 : 0} />
                  </div>
                  <div>
                    <div className="row spread dim"><span>Util</span><span className="mono">{g.utilPct.toFixed(0)}%</span></div>
                    <UsageBar pct={g.utilPct} />
                  </div>
                  <div>
                    <div className="row spread dim"><span>Temp · power</span><span className="mono">{g.tempC.toFixed(0)}°C · {g.powerW.toFixed(0)}W</span></div>
                    <UsageBar pct={g.tempC} />
                  </div>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      <div className="section-title">
        <HardDrive size={15} /> Drives
        <InfoTip>
          Live read/write throughput and fill level per drive. The health badge warns at 85% full
          and turns critical at 95% — full disks are the top cause of failed deploys.
        </InfoTip>
      </div>
      <div className="grid-2">
        {(hw.drives ?? []).map((d) => {
          const pct = d.sizeGb > 0 ? (d.usedGb / d.sizeGb) * 100 : 0;
          return (
            <div className="card" key={d.device}>
              <div className="row spread">
                <div className="row">
                  <strong className="mono">{d.device}</strong>
                  <span className={`badge ${d.type === 'nvme' ? 'amber' : d.type === 'ssd' ? 'purple' : 'gray'}`}>{d.type.toUpperCase()}</span>
                  <span className={`badge ${{ ok: 'green', warning: 'yellow', critical: 'red' }[d.health] ?? 'gray'}`}>{d.health}</span>
                </div>
                {d.tempC ? <span className="dim row" style={{ gap: 4 }}><Thermometer size={12} />{d.tempC.toFixed(0)}°C</span> : null}
              </div>
              <div className="faint" style={{ margin: '6px 0 10px' }}>
                {d.model || 'Unknown model'}{d.mountPoint ? ` · mounted at ${d.mountPoint}` : ' · not mounted'}
              </div>
              <div className="row spread dim">
                <span>{d.usedGb.toFixed(0)} GB used</span>
                <span className="mono">{d.sizeGb.toFixed(0)} GB · {pct.toFixed(0)}%</span>
              </div>
              <UsageBar pct={pct} thick />
              <div className="row" style={{ marginTop: 10, gap: 16 }}>
                <span className="dim mono">R {d.readMbps.toFixed(1)} MB/s</span>
                <span className="dim mono">W {d.writeMbps.toFixed(1)} MB/s</span>
              </div>
            </div>
          );
        })}
        {(hw.drives ?? []).length === 0 && <div className="empty">No drives discovered.</div>}
      </div>

      <div className="section-title"><NetworkIcon size={15} /> Network interfaces</div>
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
        {(hw.nics ?? []).length === 0 && <div className="faint">No interfaces reported.</div>}
      </div>

      <div className="section-title">Apps on this machine</div>
      <div className="stack" style={{ gap: 8 }}>
        {apps.map((s) => (
          <Link key={s.id} to={`/apps/${s.id}`} className="card clickable row" style={{ padding: '10px 14px', color: 'var(--text)' }}>
            <Led status={s.status} />
            <strong>{s.name}</strong>
            <span className="faint mono" style={{ marginLeft: 'auto' }}>
              {s.status === 'running' ? `${s.cpuPct.toFixed(1)}% cpu · ${s.memUsedMb} MB` : s.status}
            </span>
          </Link>
        ))}
        {apps.length === 0 && <span className="faint">Nothing deployed here yet.</span>}
      </div>
    </div>
  );
}
