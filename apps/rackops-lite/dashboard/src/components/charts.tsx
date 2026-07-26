import { useId, useMemo, useRef, useState } from 'react';
import type { MetricPoint } from '../api';

// TrendChart: the product's one time-series primitive. Self-contained SVG —
// no chart library, air-gap safe, themed entirely by CSS variables. One
// metric per chart; pages that offer CPU and memory switch with a `Seg`.
//
// Geometry: the SVG stretches (preserveAspectRatio="none") so paths scale
// with the card, while every piece of text is HTML positioned over it —
// stretched text is how charts start looking broken.

const W = 600; // internal coordinate width; strokes are non-scaling

export function TrendChart({ points, metric, height = 170, yMax, format, label, unavailable }: {
  points: MetricPoint[];
  metric: 'cpuPct' | 'memUsedMb' | 'tempC';
  height?: number;
  /** Fixed scale top (e.g. 100 for percentages). Omit to fit the data. */
  yMax?: number;
  format: (v: number) => string;
  /** Plain-language name for the accessible summary and tooltip. */
  label: string;
  /** The history read failed. "Collecting history" would be a lie during an
      outage, so the caller says which empty state is the truth. */
  unavailable?: boolean;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const gradId = useId();

  const { path, area, xs, ys, top, ticks } = useMemo(() => {
    const vals = points.map((p) => (p[metric] ?? 0) as number);
    const t0 = points[0]?.t ?? 0;
    const span = Math.max((points[points.length - 1]?.t ?? 0) - t0, 1);
    const top = yMax ?? Math.max(Math.max(...vals, 0) * 1.15, 1);
    const xs = points.map((p) => ((p.t - t0) / span) * W);
    const ys = vals.map((v) => height - (Math.min(v, top) / top) * (height - 6));
    const path = xs.map((x, i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${ys[i].toFixed(1)}`).join(' ');
    const area = path ? `${path} L${W},${height} L0,${height} Z` : '';
    // ~4 time ticks, evenly spaced across the real time range.
    const n = 4;
    const ticks = Array.from({ length: n }, (_, i) => {
      const frac = i / (n - 1);
      return { frac, t: t0 + span * frac };
    });
    return { path, area, xs, ys, top, ticks };
  }, [points, metric, yMax, height]);

  if (points.length < 2) {
    return (
      <div className="chart-empty" style={{ height: height + 22 }} role={unavailable ? 'alert' : undefined}>
        {unavailable
          ? 'History is unavailable right now. It resumes on its own once the server answers again.'
          : 'Collecting history: the chart appears after a few minutes of reports.'}
      </div>
    );
  }

  const spanSec = points[points.length - 1].t - points[0].t;
  const fmtTime = (t: number) => {
    const d = new Date(t * 1000);
    return spanSec > 36 * 3600
      ? d.toLocaleDateString(undefined, { weekday: 'short' }) + ' ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
      : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  };

  const onMove = (e: React.MouseEvent) => {
    const rect = wrapRef.current?.getBoundingClientRect();
    if (!rect) return;
    const fx = ((e.clientX - rect.left) / rect.width) * W;
    let best = 0;
    for (let i = 1; i < xs.length; i++) if (Math.abs(xs[i] - fx) < Math.abs(xs[best] - fx)) best = i;
    setHover(best);
  };

  const cur = points[points.length - 1][metric] ?? 0;
  const gridFracs = [0.25, 0.5, 0.75];

  return (
    <div className="chart" ref={wrapRef} role="img"
      aria-label={`${label}: currently ${format(cur as number)}, from ${fmtTime(points[0].t)} to ${fmtTime(points[points.length - 1].t)}`}
      onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
      <svg viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" style={{ width: '100%', height, display: 'block' }} aria-hidden>
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.22" />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
          </linearGradient>
        </defs>
        {gridFracs.map((f) => (
          <line key={f} x1="0" x2={W} y1={height * f} y2={height * f}
            stroke="var(--border)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        ))}
        {area && <path d={area} fill={`url(#${gradId})`} />}
        {path && <path d={path} fill="none" stroke="var(--accent)" strokeWidth="1.6" vectorEffect="non-scaling-stroke" />}
        {hover !== null && (
          <line x1={xs[hover]} x2={xs[hover]} y1="0" y2={height}
            stroke="var(--border-hi)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        )}
      </svg>
      {/* y-scale labels, HTML so they never stretch */}
      <span className="chart-y" style={{ top: 0 }}>{format(top)}</span>
      <span className="chart-y" style={{ top: '50%' }}>{format(top / 2)}</span>
      {/* hover marker + tooltip */}
      {hover !== null && (
        <>
          <span className="chart-dot" style={{ left: `${(xs[hover] / W) * 100}%`, top: ys[hover] }} />
          <div className="chart-tip" style={{
            left: `clamp(60px, ${(xs[hover] / W) * 100}%, calc(100% - 60px))`,
          }}>
            <span className="faint">{fmtTime(points[hover].t)}</span>
            <strong>{format((points[hover][metric] ?? 0) as number)}</strong>
          </div>
        </>
      )}
      <div className="chart-x">
        {ticks.map((tk) => (
          <span key={tk.frac} style={{ left: `${tk.frac * 100}%` }}
            className={tk.frac === 0 ? 'first' : tk.frac === 1 ? 'last' : ''}>
            {fmtTime(tk.t)}
          </span>
        ))}
      </div>
    </div>
  );
}

// Seg: the pill-tab switch used beside charts (metric, time window).
export function Seg<T extends string>({ value, onChange, options, label }: {
  value: T; onChange: (v: T) => void;
  options: { value: T; label: string }[];
  label: string;
}) {
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" role="radio" aria-checked={value === o.value}
          className={value === o.value ? 'on' : ''} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

// trendDelta: percent change of the last hour's average vs the hour around
// 24h earlier. Returns null when the series doesn't honestly support the
// comparison (fresh install, short history, near-zero baseline).
export function trendDelta(points: MetricPoint[], metric: 'cpuPct' | 'memUsedMb' | 'tempC'): number | null {
  if (points.length < 4) return null;
  const tN = points[points.length - 1].t;
  const t0 = points[0].t;
  if (tN - t0 < 20 * 3600) return null;

  const avg = (from: number, to: number) => {
    let sum = 0, n = 0;
    for (const p of points) {
      if (p.t >= from && p.t <= to) { sum += (p[metric] ?? 0) as number; n++; }
    }
    return n > 0 ? sum / n : null;
  };
  const cur = avg(tN - 3600, tN);
  const target = tN - 86400;
  const past = avg(target - 1800, target + 1800);
  if (cur === null || past === null || past < 0.1) return null;
  return ((cur - past) / past) * 100;
}
