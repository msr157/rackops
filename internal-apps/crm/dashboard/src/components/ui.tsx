/* Shared UI atoms — mirrors the RackOps Lite component language. */

/* ---- LED: the system-wide status language ---- */

// ledClass maps ticket/install status to one of four LED states.
export function ledClass(status: string): 'ok' | 'warn' | 'crit' | 'off' {
  switch (status) {
    case 'resolved': case 'online': case 'sent': case 'ok':
      return 'ok';
    case 'pending':
      return 'warn';
    case 'open': case 'failed': case 'offline': case 'urgent':
      return 'crit';
    default:
      return 'off';
  }
}

export function Led({ status, lg }: { status: string; lg?: boolean }) {
  return <span className={`led ${ledClass(status)} ${lg ? 'lg' : ''}`} aria-hidden />;
}

// StatusBadge: LED + plain-word label. Used everywhere a status appears.
export function StatusBadge({ status }: { status: string }) {
  const cls = ledClass(status);
  const color = { ok: 'var(--ok)', warn: 'var(--warn)', crit: 'var(--crit)', off: 'var(--dim)' }[cls];
  return (
    <span className="row" style={{ gap: 6, fontSize: 12, color, fontWeight: 500, whiteSpace: 'nowrap' }}>
      <Led status={status} /> {status}
    </span>
  );
}

/* ---- Severity ---- */

export function SeverityBadge({ severity }: { severity: string }) {
  const cls = severity === 'urgent' ? 'red' : severity === 'high' ? 'yellow' : severity === 'low' ? 'gray' : 'amber';
  return <span className={`badge ${cls}`}>{severity}</span>;
}

/* ---- KPI + usage bars ---- */

export function Kpi({ icon, label, value, unit, children }: {
  icon?: React.ReactNode; label: string; value: React.ReactNode; unit?: string; children?: React.ReactNode;
}) {
  return (
    <div className="card">
      <div className="eyebrow">{icon} {label}</div>
      <div className="kpi-value">{value} {unit && <span className="unit">{unit}</span>}</div>
      {children}
    </div>
  );
}

// DistBar: a labeled horizontal proportion bar (version dist, hero features).
export function DistBar({ label, count, total, suffix }: {
  label: string; count: number; total: number; suffix?: string;
}) {
  const pct = total > 0 ? Math.round((count / total) * 100) : 0;
  return (
    <div style={{ marginBottom: 10 }}>
      <div className="row spread" style={{ fontSize: 12 }}>
        <span className="mono">{label}</span>
        <span className="dim mono">{count}{suffix ?? ''} · {pct}%</span>
      </div>
      <div className="bar thick"><div className="bar-fill" style={{ width: `${pct}%` }} /></div>
    </div>
  );
}

/* ---- Empty state ---- */

export function EmptyState({ icon, title, children }: {
  icon?: React.ReactNode; title: string; children?: React.ReactNode;
}) {
  return (
    <div className="empty">
      {icon && <div style={{ marginBottom: 10, color: 'var(--faint)' }}>{icon}</div>}
      <div className="empty-title">{title}</div>
      {children}
    </div>
  );
}

/* ---- Shared helpers ---- */

export function timeAgo(iso: string): string {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export function fmtDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

// isActive: an install that phoned home within the last 7 days.
export function isActive(lastSeen: string): boolean {
  return Date.now() - new Date(lastSeen).getTime() < 7 * 24 * 3600 * 1000;
}
