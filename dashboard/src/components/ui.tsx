import { useState } from 'react';
import { Copy, Check, X, ExternalLink, Loader, Info, Eye, EyeOff } from 'lucide-react';
import type { Service, IngressConfig } from '../api';

/* ---- InfoTip: the ⓘ that explains everything ---- */

// InfoTip renders a small info icon with a hover/focus tooltip. Sprinkled
// next to every label that could confuse a first-time self-hoster.
export function InfoTip({ text, wide }: { text: string; wide?: boolean }) {
  return (
    <span className={`infotip ${wide ? 'wide' : ''}`} tabIndex={0} role="note" aria-label={text}>
      <Info size={12} aria-hidden />
      <span className="infotip-bubble">{text}</span>
    </span>
  );
}

/* ---- PasswordInput: eye toggle to view/unview ---- */

export function PasswordInput({ value, onChange, placeholder, onEnter, autoComplete }: {
  value: string; onChange: (v: string) => void; placeholder?: string;
  onEnter?: () => void; autoComplete?: string;
}) {
  const [show, setShow] = useState(false);
  return (
    <div className="pw-wrap">
      <input className="input" type={show ? 'text' : 'password'} value={value}
        placeholder={placeholder} autoComplete={autoComplete}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && onEnter?.()} />
      <button type="button" className="pw-eye" tabIndex={-1}
        aria-label={show ? 'Hide password' : 'Show password'}
        onClick={() => setShow((s) => !s)}>
        {show ? <EyeOff size={14} /> : <Eye size={14} />}
      </button>
    </div>
  );
}

/* ---- LED: the system-wide status language ---- */

// ledClass maps any backend status string to one of four LED states.
export function ledClass(status: string): 'ok' | 'warn' | 'crit' | 'off' {
  switch (status) {
    case 'running': case 'online': case 'verified': case 'success': case 'ok':
      return 'ok';
    case 'building': case 'pending': case 'deploying': case 'starting': case 'queued':
      return 'warn';
    case 'failed': case 'offline': case 'misconfigured': case 'error': case 'critical':
      return 'crit';
    default:
      return 'off'; // stopped, unknown
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

export function UsageBar({ pct, thick }: { pct: number; thick?: boolean }) {
  const cls = pct > 90 ? 'crit' : pct > 75 ? 'warn' : 'ok';
  return (
    <div className={`bar ${thick ? 'thick' : ''}`}>
      <div className={`bar-fill ${cls}`} style={{ width: `${Math.min(pct, 100)}%` }} />
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

/* ---- Copy affordances ---- */

export function CopyBtn({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button className="btn sm" title="Copy" onClick={() => {
      navigator.clipboard.writeText(text);
      setDone(true); setTimeout(() => setDone(false), 1400);
    }}>
      {done ? <Check size={12} style={{ color: 'var(--ok)' }} /> : <Copy size={12} />}
      {label}
    </button>
  );
}

export function CopyField({ text }: { text: string }) {
  return (
    <div className="row" style={{ gap: 8 }}>
      <code className="code-block" style={{ flex: 1 }}>{text || 'loading…'}</code>
      <CopyBtn text={text} />
    </div>
  );
}

/* ---- Modal ---- */

export function Modal({ title, icon, width, onClose, children }: {
  title: React.ReactNode; icon?: React.ReactNode; width?: number;
  onClose: () => void; children: React.ReactNode;
}) {
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={width ? { width } : undefined} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div className="row spread" style={{ marginBottom: 16 }}>
          <h2 style={{ margin: 0 }} className="row">{icon} {title}</h2>
          <button className="btn sm ghost" onClick={onClose} aria-label="Close"><X size={14} /></button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function Spinner({ size = 14 }: { size?: number }) {
  return <Loader size={size} className="spin" style={{ color: 'var(--amber)' }} />;
}

/* ---- Live URL chips: the payoff of the whole product ---- */

export interface PublicUrl { url: string; kind: 'domain' | 'tunnel' }

// publicUrls lists every address where an app is reachable from outside.
export function publicUrls(svc: Service, ingress: IngressConfig | null): PublicUrl[] {
  const out: PublicUrl[] = [];
  if (svc.domain && ingress?.enabled) {
    const tls = !!ingress.acmeEmail;
    const port = tls ? ingress.httpsPort : ingress.httpPort;
    const std = (tls && port === 443) || (!tls && port === 80);
    out.push({ url: `${tls ? 'https' : 'http'}://${svc.domain}${std ? '' : `:${port}`}`, kind: 'domain' });
  }
  if (svc.quickTunnelUrl) out.push({ url: svc.quickTunnelUrl, kind: 'tunnel' });
  return out;
}

export function UrlChip({ url }: { url: string }) {
  return (
    <a className="url-chip" href={url} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>
      <ExternalLink size={11} />
      {url.replace(/^https?:\/\//, '')}
    </a>
  );
}

/* ---- Shared helpers ---- */

// dnsLabel mirrors the backend sanitizer: DNS-safe app names.
export function dnsLabel(s: string): string {
  const out = s.toLowerCase().replace(/[ ._]/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '');
  return out || 'app';
}

export function timeAgo(iso: string): string {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), min = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${min}m`;
  return `${min}m`;
}

// shortGpu trims verbose PCI vendor strings down to a readable model name.
export function shortGpu(model: string): string {
  return model.replace(/Advanced Micro Devices.*\[AMD\/ATI\]\s*/, 'AMD ').replace(/\s*\(rev .+\)$/, '');
}
