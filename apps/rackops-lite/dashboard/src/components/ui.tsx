import { useEffect, useId, useRef, useState } from 'react';
import { Copy, Check, X, ExternalLink, Loader, Info, Eye, EyeOff, MoreVertical, ChevronRight } from 'lucide-react';
import { Link } from 'react-router-dom';
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

/* ---- Segmented switch: white-active, the register's signature ---- */

// Status filters and small view switches. The active segment inverts to
// white with near-black text; never used for navigation.
export function Seg<T extends string>({ value, onChange, options, label }: {
  value: T; onChange: (v: T) => void;
  options: ReadonlyArray<readonly [T, string]>; label: string;
}) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map(([v, text]) => (
        <button key={v} type="button" className={v === value ? 'on' : ''}
          aria-pressed={v === value} onClick={() => onChange(v)}>
          {text}
        </button>
      ))}
    </div>
  );
}

/* ---- Switch: the boolean control on setting blocks ---- */

export function Switch({ checked, onChange, disabled, ariaLabel, ariaLabelledBy }: {
  checked: boolean; onChange: (v: boolean) => void; disabled?: boolean;
  ariaLabel?: string; ariaLabelledBy?: string;
}) {
  return (
    <button type="button" className="switch" role="switch" aria-checked={checked}
      aria-label={ariaLabel} aria-labelledby={ariaLabelledBy} disabled={disabled}
      onClick={() => onChange(!checked)} />
  );
}

/* ---- Breadcrumbs ---- */

export interface Crumb { label: React.ReactNode; to?: string; icon?: React.ReactNode }

// The wayfinding line above every page title: section icon + name, then the
// entity. The last crumb is the current place and stays plain text.
export function Crumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className="crumbs" aria-label="Breadcrumb">
      {items.map((c, i) => (
        <span key={i} className="row" style={{ gap: 7, minWidth: 0 }}>
          {i > 0 && <ChevronRight size={12} className="crumb-sep" aria-hidden />}
          {c.to
            ? <Link to={c.to}>{c.icon}{c.label}</Link>
            : <span className="crumb-here" aria-current="page">{c.icon}{c.label}</span>}
        </span>
      ))}
    </nav>
  );
}

/* ---- RowMenu: the kebab at the end of a table row ---- */

export interface RowMenuItem {
  label: string; icon?: React.ReactNode;
  danger?: boolean; disabled?: boolean;
  onSelect: () => void;
}

// A bordered icon-square opening a small action menu. Outside click closes;
// Escape closes without bubbling into row/dialog handlers.
export function RowMenu({ label, items }: { label: string; items: RowMenuItem[] }) {
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey, true);
    };
  }, [open]);

  return (
    <div className="rowmenu" ref={boxRef} onClick={(e) => e.stopPropagation()}>
      <button ref={triggerRef} type="button" className="icon-sq" aria-haspopup="menu"
        aria-expanded={open} aria-label={label} title={label}
        onClick={() => setOpen((v) => !v)}>
        <MoreVertical size={15} />
      </button>
      {open && (
        <div className="rowmenu-pop" role="menu" aria-label={label}>
          {items.map((it) => (
            <button key={it.label} role="menuitem" className={`org-item ${it.danger ? 'danger' : ''}`}
              disabled={it.disabled}
              onClick={() => { setOpen(false); it.onSelect(); }}>
              {it.icon} {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---- Unsaved-changes guard ---- */

// Warns before the tab closes or reloads while a draft is dirty. In-app
// route changes are not interceptable under BrowserRouter (useBlocker needs
// the data router), so the draft surfaces also keep Save/Cancel visibly
// disabled-or-armed in their headers as the primary signal.
export function useUnsavedChanges(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [dirty]);
}

/* ---- Usage bars ---- */

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

export interface PublicUrl { url: string; kind: 'domain' | 'tunnel' | 'frontdoor' }

// publicUrls lists every address where an app is reachable from outside.
export function publicUrls(svc: Service, ingress: IngressConfig | null): PublicUrl[] {
  const out: PublicUrl[] = [];
  // A Front Door address is always HTTPS on the standard port and does not
  // depend on the local edge's ports or ACME state — the vendor POP holds the
  // certificate, so applying the edge's scheme/port here would print a URL
  // that does not exist.
  if (svc.domain && svc.frontDoor) {
    out.push({ url: `https://${svc.domain}`, kind: 'frontdoor' });
  } else if (svc.domain && ingress?.enabled) {
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

// formatBytes renders a transfer figure for humans. Binary units, one
// decimal, so 100 GiB of quota reads as "100 GB" rather than "107374182400".
export function formatBytes(n: number): string {
  if (!n || n < 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
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
