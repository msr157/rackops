import './MetricBar.css';

interface MetricBarProps {
  label: string;
  pct: number;
  detail?: string;
}

export function MetricBar({ label, pct, detail }: MetricBarProps) {
  const clamped = Math.max(0, Math.min(100, pct));
  return (
    <div className="ro-metric">
      <div className="ro-metric-head">
        <span className="ro-metric-label">{label}</span>
        <span className="ro-metric-value">
          {detail ? `${detail} · ` : ''}
          {clamped.toFixed(0)}%
        </span>
      </div>
      <div className="ro-metric-track">
        <div
          className={`ro-metric-fill${clamped > 80 ? ' warn' : ''}`}
          style={{ width: `${clamped}%` }}
        />
      </div>
    </div>
  );
}
