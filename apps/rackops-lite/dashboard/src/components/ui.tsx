import { useId, useState } from 'react';
import { Copy, Check, X, ExternalLink, Loader, Info, Eye, EyeOff } from 'lucide-react';
import type { Service, IngressConfig } from '../api';
import { useDialogBehavior } from './useDialog';

/* ---- InfoTip: the ⓘ that explains anything non-obvious ---- */

// Hover or keyboard-focus reveals the bubble (CSS); click/tap toggles it so
// touch users aren't left out. Content is plain language, never jargon.
export function InfoTip({ children, wide }: { children: React.ReactNode; wide?: boolean }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <button
      type="button"
      className={`infotip ${wide ? 'wide' : ''} ${open ? 'open' : ''}`}
      aria-label="More information"
      aria-describedby={id}
      aria-expanded={open}
      // preventDefault keeps a tip inside a <label> from also toggling its field.
      onClick={(e) => { e.preventDefault(); e.stopPropagation(); setOpen((v) => !v); }}
      onBlur={() => setOpen(false)}
    >
      <Info size={12} aria-hidden />
      <span className="infotip-bubble" role="tooltip" id={id}>{children}</span>
    </button>
  );
}

/* ---- Field: the one labelled-input pattern ---- */

// Field owns the label/control association every form in the product depends
// on: it mints an id, points the <label> at it, and hands the id to the
// control. Screen readers announce a real name instead of "edit text".
export function Field({ label, info, hint, error, style, children }: {
  label: React.ReactNode;
  info?: React.ReactNode;
  hint?: React.ReactNode;
  error?: string;
  style?: React.CSSProperties;
  children: (props: { id: string; 'aria-describedby'?: string }) => React.ReactNode;
}) {
  const id = useId();
  const hintId = `${id}-hint`;
  const describedBy = error || hint ? hintId : undefined;
  return (
    <div className="field" style={style}>
      {/* The tip sits beside the <label>, never inside it: label content
          becomes the field's accessible name, and a paragraph of help text
          buried in that name is worse than no tip at all. */}
      <div className="field-label">
        <label htmlFor={id}>{label}</label>
        {info && <InfoTip wide>{info}</InfoTip>}
      </div>
      {children({ id, 'aria-describedby': describedBy })}
      {error
        ? <span className="field-error" id={hintId}>{error}</span>
        : hint && <span className="faint" id={hintId}>{hint}</span>}
    </div>
  );
}

/* ---- PasswordInput: input with a show/hide eye toggle ---- */

export function PasswordInput({ id, value, onChange, placeholder, autoComplete, onEnter, ...rest }: {
  id?: string; value: string; onChange: (v: string) => void;
  placeholder?: string; autoComplete?: string; onEnter?: () => void;
  'aria-describedby'?: string;
}) {
  const [show, setShow] = useState(false);
  return (
    <div className="pw-wrap">
      <input className="input" id={id} type={show ? 'text' : 'password'} value={value}
        placeholder={placeholder} autoComplete={autoComplete} {...rest}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && onEnter?.()} />
      <button type="button" className="pw-eye" tabIndex={-1}
        aria-label={show ? 'Hide password' : 'Show password'}
        onClick={() => setShow((v) => !v)}>
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

// The LED is colour-only, so anything that carries status by LED alone passes
// `label` to emit the word for screen readers.
export function Led({ status, lg, label }: { status: string; lg?: boolean; label?: boolean }) {
  const dot = <span className={`led ${ledClass(status)} ${lg ? 'lg' : ''}`} aria-hidden />;
  if (!label) return dot;
  return <>{dot}<span className="sr-only">{status}</span></>;
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

// Pill: dot + word in a tinted capsule — the status language for tables and
// dense lists, where a bare colored word would drown in surrounding text.
export function Pill({ status, children }: { status: string; children?: React.ReactNode }) {
  const cls = ledClass(status);
  return <span className={`pill ${cls === 'off' ? '' : cls}`}>{children ?? status}</span>;
}

// IconChip: a glyph in a soft tinted container. The tone carries meaning;
// neutral is the default for identity and inventory facts.
export function IconChip({ tone, sm, children }: {
  tone?: 'ok' | 'warn' | 'crit' | 'accent'; sm?: boolean; children: React.ReactNode;
}) {
  return <span className={`icon-chip ${tone ?? ''} ${sm ? 'sm' : ''}`} aria-hidden>{children}</span>;
}

// Delta: "how does now compare to before" beside a KPI value. The caller
// says which direction is good — more apps running is good, more CPU is not.
export function Delta({ pct, goodWhenUp, label }: {
  pct: number | null; goodWhenUp?: boolean; label?: string;
}) {
  // No history yet (fresh install, new machine): say nothing rather than "0%".
  if (pct === null || !Number.isFinite(pct)) return null;
  const flat = Math.abs(pct) < 1;
  const tone = flat ? 'flat' : (pct > 0) === !!goodWhenUp ? 'good' : 'bad';
  const arrow = flat ? '→' : pct > 0 ? '↑' : '↓';
  return (
    <span className={`delta ${tone}`} title={label ?? 'vs 24 hours ago'}>
      <span aria-hidden>{arrow}</span>
      {Math.abs(pct).toFixed(Math.abs(pct) >= 10 ? 0 : 1)}%
      <span className="sr-only">{pct > 0 ? 'up' : flat ? 'unchanged' : 'down'} {label ?? 'vs 24 hours ago'}</span>
    </span>
  );
}

/* ---- KPI + usage bars ---- */

// The one KPI tile. `chip` puts an IconChip at the row's right edge (the
// reference-register form); bare `icon` keeps the inline eyebrow glyph.
// Both feed the same .kpi-row slot — one component, one structure.
export function Kpi({ icon, label, chip, value, unit, delta, children }: {
  icon?: React.ReactNode; label: string; chip?: React.ReactNode;
  value: React.ReactNode; unit?: string; delta?: React.ReactNode; children?: React.ReactNode;
}) {
  return (
    <div className="card">
      {chip
        ? <div className="row spread"><span className="eyebrow">{label}</span>{chip}</div>
        : <div className="eyebrow">{icon} {label}</div>}
      <div className="kpi-value">{value} {unit && <span className="unit">{unit}</span>}</div>
      {delta && <div className="kpi-delta">{delta}</div>}
      {children}
    </div>
  );
}

// Every usage sits beside its own numeric readout, so the bar itself is
// decoration. It scales rather than animating width — no layout work per tick.
export function UsageBar({ pct, thick }: { pct: number; thick?: boolean }) {
  const cls = pct > 90 ? 'crit' : pct > 75 ? 'warn' : 'ok';
  const safe = Math.max(0, Math.min(Number.isFinite(pct) ? pct : 0, 100));
  return (
    <div className={`bar ${thick ? 'thick' : ''}`} aria-hidden>
      <div className={`bar-fill ${cls}`} style={{ transform: `scaleX(${safe / 100})` }} />
    </div>
  );
}

/* ---- Loading placeholders ---- */

// Skeletons stand in for content that is on its way. They exist so a list that
// is still loading never renders as a list that is empty.
export function Skeleton({ height = 14, width, radius }: {
  height?: number | string; width?: number | string; radius?: number | string;
}) {
  return <span className="skeleton" style={{ height, width, borderRadius: radius }} />;
}

export function SkeletonRows({ count = 3, height = 52 }: { count?: number; height?: number }) {
  return (
    <div className="stack" style={{ gap: 8 }} aria-hidden>
      {Array.from({ length: count }, (_, i) => <Skeleton key={i} height={height} radius="var(--r-md)" />)}
    </div>
  );
}

/* ---- Empty state ---- */

// slim: a one-line routine absence between dense regions — quiet aside,
// not a 40px-tall event. Full size stays for empties that own their page.
export function EmptyState({ icon, title, slim, children }: {
  icon?: React.ReactNode; title: string; slim?: boolean; children?: React.ReactNode;
}) {
  return (
    <div className={`empty ${slim ? 'slim' : ''}`}>
      {icon && <div className="empty-icon" aria-hidden>{icon}</div>}
      <div className="empty-title">{title}</div>
      {children}
    </div>
  );
}

/* ---- Copy affordances ---- */

export function CopyBtn({ text, label }: { text: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'ok' | 'fail'>('idle');
  const copy = async () => {
    // clipboard is undefined on insecure origins (a LAN dashboard over http) —
    // failing silently while flashing a success check would be a lie.
    try {
      await navigator.clipboard.writeText(text);
      setState('ok');
    } catch {
      setState('fail');
    }
    setTimeout(() => setState('idle'), 1800);
  };
  return (
    <button className="btn sm" onClick={copy}
      title={state === 'fail' ? 'Copy failed. Select and copy it manually.' : 'Copy'}>
      {state === 'ok' && <Check size={12} style={{ color: 'var(--ok)' }} />}
      {state === 'fail' && <X size={12} style={{ color: 'var(--crit)' }} />}
      {state === 'idle' && <Copy size={12} />}
      {state === 'fail' ? 'Copy failed' : label}
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
  // container: a modal that opens on its Close button reads as "Close" first;
  // focusing the box announces the title. Modals whose own field asks for
  // focus keep it.
  const ref = useDialogBehavior<HTMLDivElement>(onClose, { initialFocus: 'container' });
  const titleId = useId();
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div ref={ref} className="modal" tabIndex={-1} style={width ? { width } : undefined}
        onClick={(e) => e.stopPropagation()}
        role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <div className="row spread" style={{ marginBottom: 16 }}>
          <h2 id={titleId} style={{ margin: 0 }} className="row">{icon} {title}</h2>
          <button className="btn sm ghost" onClick={onClose} aria-label="Close"><X size={14} /></button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function Spinner({ size = 14 }: { size?: number }) {
  return <Loader size={size} className="spin" style={{ color: 'var(--accent)' }} />;
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
