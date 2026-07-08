import { HardDrive, Thermometer } from 'lucide-react';
import type { Node } from '../api';

export function StoragePage({ nodes }: { nodes: Node[] }) {
  return (
    <>
      <div className="page-title">Storage</div>
      <div className="page-sub">Every physical drive in the fleet, visualized</div>

      {nodes.map((n) => (
        <div key={n.id}>
          <div className="section-title"><HardDrive size={15} /> {n.name}</div>
          <div className="grid-2">
            {(n.hardware.drives ?? []).map((d) => {
              const pct = d.sizeGb > 0 ? (d.usedGb / d.sizeGb) * 100 : 0;
              const cls = pct > 92 ? 'crit' : pct > 85 ? 'warn' : '';
              return (
                <div className="card" key={d.device}>
                  <div className="row spread">
                    <div className="row">
                      <strong className="mono">{d.device}</strong>
                      <span className={`badge ${d.type === 'nvme' ? 'cyan' : d.type === 'ssd' ? 'purple' : 'gray'}`}>
                        {d.type.toUpperCase()}
                      </span>
                      <HealthBadge health={d.health} />
                    </div>
                    {d.tempC ? <span className="dim row"><Thermometer size={12} />{d.tempC.toFixed(0)}°C</span> : null}
                  </div>
                  <div className="faint" style={{ margin: '6px 0 10px' }}>{d.model || 'Unknown model'}{d.mountPoint ? ` · mounted at ${d.mountPoint}` : ' · not mounted'}</div>
                  <div className="row spread dim">
                    <span>{d.usedGb.toFixed(0)} GB used</span>
                    <span className="mono">{d.sizeGb.toFixed(0)} GB · {pct.toFixed(0)}%</span>
                  </div>
                  <div className="bar" style={{ height: 8 }}>
                    <div className={`bar-fill ${cls}`} style={{ width: `${pct}%` }} />
                  </div>
                  <div className="row" style={{ marginTop: 10, gap: 16 }}>
                    <span className="dim mono">R {d.readMbps.toFixed(1)} MB/s</span>
                    <span className="dim mono">W {d.writeMbps.toFixed(1)} MB/s</span>
                  </div>
                </div>
              );
            })}
            {(n.hardware.drives ?? []).length === 0 && <div className="empty">No drives discovered.</div>}
          </div>
        </div>
      ))}
    </>
  );
}

function HealthBadge({ health }: { health: string }) {
  const map: Record<string, string> = { ok: 'green', warning: 'yellow', critical: 'red', unknown: 'gray' };
  return <span className={`badge ${map[health] ?? 'gray'}`}>{health}</span>;
}
