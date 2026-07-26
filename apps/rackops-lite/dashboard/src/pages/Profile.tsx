import { useCallback, useEffect, useState } from 'react';
import {
  AlertTriangle, BookOpenText, Bot, Check, Copy, KeyRound, Plus, RotateCw, ShieldCheck, Trash2, UserRound,
} from 'lucide-react';
import { api } from '../api';
import type { APIToken, User } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { Field, InfoTip, PasswordInput, SkeletonRows, Spinner, timeAgo } from '../components/ui';

// A key is shown in full exactly once, at creation. Everything about that
// moment — the highlighted card, the MCP snippet below picking it up, revoking
// it clearing the card — matches on this id, never on the (reusable) name.
interface FreshKey { id: string; name: string; token: string }

// Profile: the signed-in operator's own account — identity, password, and the
// API keys that act on their behalf. Fleet-wide settings live in Settings.
export function ProfilePage() {
  const [fresh, setFresh] = useState<FreshKey | null>(null);

  return (
    <div className="page">
      <div className="page-title">Profile</div>
      <div className="page-sub">Your API keys, AI-tool access, account, and password</div>

      {/* API keys lead: they are what operators come here for. Identity and
          password housekeeping follow. */}
      <div className="stack">
        <ApiKeysCard fresh={fresh} onFresh={setFresh} />
        <McpCard freshToken={fresh?.token} />
        <AccountCard />
        <ChangePasswordCard />
      </div>
    </div>
  );
}

// "0001-…" is Go's zero time — the backend's way of saying "not set".
function realDate(iso?: string): iso is string {
  return !!iso && !iso.startsWith('0001-');
}

function isExpired(t: APIToken): boolean {
  return realDate(t.expiresAt) && new Date(t.expiresAt) < new Date();
}

// The kit CopyBtn assumes the clipboard write succeeds; on a plain-HTTP LAN
// install the Clipboard API is unavailable and it fails silently. The two
// copies on this page must never fail silently — an API key shown once, and
// the config built from it — so they use this guarded button instead.
function CopyGuard({ text }: { text: string }) {
  const toast = useToast();
  const [done, setDone] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setDone(true);
      window.setTimeout(() => setDone(false), 1400);
    } catch {
      toast.err('Copy failed. Copy it manually.');
    }
  };
  return (
    <button className="btn sm" title="Copy" aria-label="Copy" onClick={copy}>
      {done ? <Check size={12} style={{ color: 'var(--ok)' }} aria-hidden /> : <Copy size={12} aria-hidden />}
    </button>
  );
}

function AccountCard() {
  const [me, setMe] = useState<User | null>(null);
  const [failed, setFailed] = useState(false);

  const load = useCallback(() => {
    setFailed(false);
    api.me().then(setMe).catch(() => setFailed(true));
  }, []);
  useEffect(load, [load]);

  return (
    <div className="card">
      <div className="eyebrow" style={{ marginBottom: 12 }}><UserRound size={12} /> Account</div>
      {me ? (
        <dl className="kv">
          <dt>Name</dt><dd>{me.fullName || '(not set)'}</dd>
          <dt>Email</dt><dd>{me.email}</dd>
          <dt>Role</dt><dd>{me.role}</dd>
          <dt>Member since</dt><dd>{new Date(me.createdAt).toLocaleDateString()}</dd>
        </dl>
      ) : failed ? (
        <div className="row">
          <AlertTriangle size={14} style={{ color: 'var(--crit)' }} aria-hidden />
          <span className="dim" style={{ flex: 1 }}>Couldn&rsquo;t load your account. The server didn&rsquo;t answer.</span>
          <button className="btn sm" onClick={load}><RotateCw size={12} /> Retry</button>
        </div>
      ) : (
        <SkeletonRows count={4} height={16} />
      )}
    </div>
  );
}

function ChangePasswordCard() {
  const toast = useToast();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (next.length < 8) { toast.err('New password must be at least 8 characters.'); return; }
    if (next !== confirm) { toast.err('New passwords do not match.'); return; }
    setBusy(true);
    try {
      await api.changePassword(current, next);
      toast.ok('Password changed.');
      setCurrent(''); setNext(''); setConfirm('');
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  return (
    <div className="card">
      <div className="eyebrow" style={{ marginBottom: 12 }}><ShieldCheck size={12} /> Change password</div>
      <div className="grid-3">
        <Field label="Current password">
          {(p) => <PasswordInput {...p} value={current} onChange={setCurrent} autoComplete="current-password" />}
        </Field>
        <Field label="New password">
          {(p) => <PasswordInput {...p} value={next} onChange={setNext} placeholder="8+ characters" autoComplete="new-password" />}
        </Field>
        <Field label="Confirm new password"
          hint={confirm.length > 0 && confirm !== next
            ? <span style={{ color: 'var(--warn)' }}>Passwords don&rsquo;t match yet.</span>
            : undefined}>
          {(p) => <PasswordInput {...p} value={confirm} onChange={setConfirm} autoComplete="new-password" onEnter={submit} />}
        </Field>
      </div>
      <button className="btn primary" disabled={busy || !current || !next || !confirm} onClick={submit}>
        {busy ? <Spinner size={12} /> : <ShieldCheck size={12} />} Update password
      </button>
    </div>
  );
}

const EXPIRY_OPTIONS = [
  { value: 'never', label: 'Never expires' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
  { value: '90d', label: '90 days' },
  { value: '1y', label: '1 year' },
];

function ApiKeysCard({ fresh, onFresh }: {
  fresh: FreshKey | null; onFresh: (k: FreshKey | null) => void;
}) {
  const toast = useToast();
  const confirm = useConfirm();
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [name, setName] = useState('');
  const [expiresIn, setExpiresIn] = useState('never');
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    setLoadFailed(false);
    api.tokens().then((t) => { setTokens(t ?? []); setLoaded(true); }).catch(() => setLoadFailed(true));
  }, []);
  useEffect(load, [load]);

  const create = async () => {
    if (!name.trim()) { toast.err('Give the key a name so you can recognize it later.'); return; }
    setBusy(true);
    try {
      const r = await api.createApiToken(name.trim(), expiresIn);
      onFresh({ id: r.id, name: r.name, token: r.token });
      setName('');
      load();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const revoke = async (t: APIToken) => {
    const yes = await confirm({
      title: `Revoke "${t.name}"?`,
      body: 'Anything using this key loses access immediately. This cannot be undone.',
      confirmLabel: 'Revoke key', danger: true,
    });
    if (!yes) return;
    try {
      await api.deleteApiToken(t.id);
      if (fresh && fresh.id === t.id) onFresh(null);
      toast.ok('Key revoked.');
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="card">
      <div className="eyebrow" style={{ marginBottom: 6 }}>
        <KeyRound size={12} /> API keys
        <InfoTip wide>
          API keys let tools call the RackOps API as you: the CLI, scripts, VS Code via MCP,
          or anything speaking HTTP. A key has exactly your permissions, and revoking it here
          shuts that tool out instantly. Send it as <code>Authorization: Bearer</code> or
          <code> X-Api-Key</code>.
        </InfoTip>
      </div>
      <div className="dim" style={{ marginBottom: 14 }}>
        Keys act with your role and are shown once at creation. Store them somewhere safe.
      </div>

      <div className="row wrap" style={{ alignItems: 'flex-end', marginBottom: 14 }}>
        <Field label="Key name" style={{ width: 220, marginBottom: 0 }}>
          {(p) => (
            <input className="input" {...p} placeholder="what will use it? e.g. vscode-mcp"
              value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} />
          )}
        </Field>
        <Field label="Expires" style={{ width: 160, marginBottom: 0 }}>
          {(p) => (
            <select className="input" {...p} value={expiresIn} onChange={(e) => setExpiresIn(e.target.value)}>
              {EXPIRY_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
          )}
        </Field>
        <button className="btn primary" disabled={busy} onClick={create}>
          {busy ? <Spinner size={12} /> : <Plus size={12} />} Create key
        </button>
      </div>

      {fresh && (
        <div className="card inset" style={{ borderColor: 'rgba(87, 162, 255, 0.45)', marginBottom: 14 }}>
          <div className="row spread" style={{ marginBottom: 8, alignItems: 'flex-start' }}>
            <div>
              <strong>{fresh.name}</strong>
              <span className="dim">: copy it now, it won&rsquo;t be shown again.</span>
            </div>
            <button className="btn sm ghost" onClick={() => onFresh(null)}>Dismiss</button>
          </div>
          <div className="row" style={{ gap: 8 }}>
            <code className="code-block" style={{ flex: 1 }}>{fresh.token}</code>
            <CopyGuard text={fresh.token} />
          </div>
        </div>
      )}

      {!loaded && !loadFailed && <SkeletonRows count={2} height={38} />}
      {!loaded && loadFailed && (
        <div className="row" style={{ padding: '4px 0' }}>
          <AlertTriangle size={14} style={{ color: 'var(--crit)' }} aria-hidden />
          <span className="dim" style={{ flex: 1 }}>Couldn&rsquo;t load your keys. The server didn&rsquo;t answer.</span>
          <button className="btn sm" onClick={load}><RotateCw size={12} /> Retry</button>
        </div>
      )}
      {loaded && tokens.length === 0 && !fresh && (
        <div className="faint">No API keys yet. Create one above.</div>
      )}
      {loaded && tokens.length > 0 && (
        <div className="list">
          {tokens.map((t) => {
            const expired = isExpired(t);
            return (
              // An expired key is inert — the row dims, and the badge says why.
              <div key={t.id} className="list-row" style={expired ? { opacity: 0.6 } : undefined}>
                <KeyRound size={13} style={{ color: 'var(--faint)' }} aria-hidden />
                <strong>{t.name || '(unnamed)'}</strong>
                <span className="faint">created {timeAgo(t.createdAt)}</span>
                {realDate(t.lastUsed) && <span className="faint">· last used {timeAgo(t.lastUsed)}</span>}
                {expired
                  ? <span className="badge crit">expired</span>
                  : realDate(t.expiresAt) && <span className="tag">expires {new Date(t.expiresAt).toLocaleDateString()}</span>}
                <button className="btn sm danger" style={{ marginLeft: 'auto' }} onClick={() => revoke(t)}
                  aria-label={`Revoke ${t.name || 'unnamed key'}`} title="Revoke key">
                  <Trash2 size={12} />
                </button>
              </div>
            );
          })}
        </div>
      )}

      <div className="row" style={{ marginTop: 14, gap: 14 }}>
        <a className="faint row" style={{ gap: 5 }} href="/swagger" target="_blank" rel="noreferrer">
          <BookOpenText size={12} /> API reference (Swagger)
        </a>
      </div>
    </div>
  );
}

// McpCard: ready-to-paste MCP config so VS Code / Claude / Cursor can manage
// the fleet through `rackops mcp` with one of the keys above. While a freshly
// created key is on screen, it is injected here so the snippet is paste-ready.
function McpCard({ freshToken }: { freshToken?: string }) {
  const origin = window.location.origin;
  const config = `{
  "mcpServers": {
    "rackops": {
      "command": "rackops",
      "args": ["mcp"],
      "env": {
        "RACKOPS_SERVER": "${origin}",
        "RACKOPS_TOKEN": "${freshToken ?? 'rk_YOUR_API_KEY'}"
      }
    }
  }
}`;
  return (
    <div className="card">
      <div className="eyebrow" style={{ marginBottom: 6 }}>
        <Bot size={12} /> Connect AI tools (MCP)
        <InfoTip wide>
          The RackOps binary doubles as an MCP server: <code>rackops mcp</code> speaks the Model
          Context Protocol over stdio, so AI assistants can list machines, deploy apps, read logs,
          and check alerts, using an API key with exactly your permissions.
        </InfoTip>
      </div>
      <div className="dim" style={{ marginBottom: 12 }}>
        Works with VS Code (Copilot/Claude), Claude Desktop, Claude Code, and Cursor. Create an API
        key above, then add this to your tool&rsquo;s MCP config (<code className="mono">mcp.json</code> /
        <code className="mono"> claude mcp add</code>):
      </div>
      <div style={{ position: 'relative' }}>
        <pre className="code-block" style={{ whiteSpace: 'pre', overflowX: 'auto', margin: 0 }}>{config}</pre>
        <div style={{ position: 'absolute', top: 8, right: 8 }}>
          <CopyGuard text={config} />
        </div>
      </div>
      {freshToken && (
        <div className="faint" style={{ marginTop: 10 }}>
          Your new key is filled in above; paste the snippet as-is. It disappears from here when
          you dismiss the key card.
        </div>
      )}
      <div className="faint" style={{ marginTop: 10 }}>
        The <code className="mono">rackops</code> binary must be installed where the AI tool runs
        (the same one-line installer works on a workstation).
      </div>
    </div>
  );
}
