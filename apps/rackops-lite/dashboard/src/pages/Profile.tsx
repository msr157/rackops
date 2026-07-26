import { useCallback, useEffect, useState } from 'react';
import { BookOpenText, Bot, KeyRound, Plus, Trash2, UserRound, ShieldCheck } from 'lucide-react';
import { api } from '../api';
import type { APIToken, User } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { CopyBtn, CopyField, Field, InfoTip, PasswordInput, Spinner, timeAgo } from '../components/ui';

// Profile: the signed-in operator's own account — identity, password, and the
// API keys that act on their behalf. Fleet-wide settings live in Settings.
export function ProfilePage() {
  const [me, setMe] = useState<User | null>(null);
  useEffect(() => { api.me().then(setMe).catch(() => {}); }, []);

  return (
    <div className="page">
      <div className="page-title">Profile</div>
      <div className="page-sub">Your account, password, API keys, and AI-tool access</div>

      <div className="stack">
        <AccountCard me={me} />
        <ChangePasswordCard />
        <ApiKeysCard />
        <McpCard />
      </div>
    </div>
  );
}

function AccountCard({ me }: { me: User | null }) {
  return (
    <div className="card">
      <div className="eyebrow" style={{ marginBottom: 12 }}><UserRound size={12} /> Account</div>
      {!me ? <div className="row dim"><Spinner /> loading…</div> : (
        <table style={{ borderCollapse: 'collapse' }}>
          <tbody>
            {[
              ['Name', me.fullName || '—'],
              ['Email', me.email],
              ['Role', me.role],
              ['Member since', new Date(me.createdAt).toLocaleDateString()],
            ].map(([k, v]) => (
              <tr key={k}>
                <td className="dim" style={{ padding: '6px 28px 6px 0', whiteSpace: 'nowrap' }}>{k}</td>
                <td style={{ padding: '6px 0' }}>{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
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
      <button className="btn" disabled={busy || !current || !next || !confirm} onClick={submit}>
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

function ApiKeysCard() {
  const toast = useToast();
  const confirm = useConfirm();
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [name, setName] = useState('');
  const [expiresIn, setExpiresIn] = useState('never');
  const [fresh, setFresh] = useState<{ name: string; token: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => { api.tokens().then((t) => setTokens(t ?? [])).catch(() => {}); }, []);
  useEffect(load, [load]);

  const create = async () => {
    if (!name.trim()) { toast.err('Give the key a name so you can recognize it later.'); return; }
    setBusy(true);
    try {
      const r = await api.createApiToken(name.trim(), expiresIn);
      setFresh({ name: r.name, token: r.token });
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
      if (fresh && fresh.name === t.name) setFresh(null);
      toast.ok('Key revoked.');
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="card">
      <div className="eyebrow" style={{ marginBottom: 6 }}>
        <KeyRound size={12} /> API keys
        <InfoTip wide>
          API keys let tools call the RackOps API as you — the CLI, scripts, VS Code via MCP,
          or anything speaking HTTP. A key has exactly your permissions, and revoking it here
          shuts that tool out instantly. Send it as <code>Authorization: Bearer</code> or
          <code> X-Api-Key</code>.
        </InfoTip>
      </div>
      <div className="dim" style={{ marginBottom: 14 }}>
        Keys act with your role and are shown once at creation — store them somewhere safe.
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
        <div className="card inset" style={{ borderColor: 'var(--amber)', marginBottom: 14 }}>
          <div style={{ marginBottom: 8 }}>
            <strong>{fresh.name}</strong>
            <span className="dim"> — copy it now, it won&rsquo;t be shown again.</span>
          </div>
          <CopyField text={fresh.token} />
        </div>
      )}

      {tokens.length === 0 && !fresh && <div className="faint">No API keys yet.</div>}
      {tokens.map((t) => (
        <div key={t.id} className="row spread" style={{ borderTop: '1px solid var(--border)', padding: '9px 0' }}>
          <div className="row" style={{ gap: 10 }}>
            <KeyRound size={13} style={{ color: 'var(--faint)' }} />
            <strong>{t.name || '(unnamed)'}</strong>
            <span className="faint">created {timeAgo(t.createdAt)}</span>
            {t.lastUsed && !t.lastUsed.startsWith('0001-') && <span className="faint">· last used {timeAgo(t.lastUsed)}</span>}
            {t.expiresAt && !t.expiresAt.startsWith('0001-') && (
              new Date(t.expiresAt) < new Date()
                ? <span className="badge red">expired</span>
                : <span className="tag">expires {new Date(t.expiresAt).toLocaleDateString()}</span>
            )}
          </div>
          <button className="btn sm danger" onClick={() => revoke(t)} aria-label={`Revoke ${t.name}`}>
            <Trash2 size={12} />
          </button>
        </div>
      ))}

      <div className="row" style={{ marginTop: 14, gap: 14 }}>
        <a className="faint row" style={{ gap: 5 }} href="/swagger" target="_blank" rel="noreferrer">
          <BookOpenText size={12} /> API reference (Swagger)
        </a>
      </div>
    </div>
  );
}

// McpCard: ready-to-paste MCP config so VS Code / Claude / Cursor can manage
// the fleet through `rackops mcp` with one of the keys above.
function McpCard() {
  const origin = window.location.origin;
  const config = `{
  "mcpServers": {
    "rackops": {
      "command": "rackops",
      "args": ["mcp"],
      "env": {
        "RACKOPS_SERVER": "${origin}",
        "RACKOPS_TOKEN": "rk_YOUR_API_KEY"
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
          and check alerts — using an API key with exactly your permissions.
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
          <CopyBtn text={config} />
        </div>
      </div>
      <div className="faint" style={{ marginTop: 10 }}>
        The <code className="mono">rackops</code> binary must be installed where the AI tool runs
        (the same one-line installer works on a workstation).
      </div>
    </div>
  );
}
