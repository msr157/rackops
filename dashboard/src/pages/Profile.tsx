import { useEffect, useState } from 'react';
import { KeyRound, Plus, Trash2, User as UserIcon, Plug, BookOpen, ExternalLink } from 'lucide-react';
import { api } from '../api';
import type { APIToken, User } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { CopyField, InfoTip, PasswordInput, Spinner, timeAgo } from '../components/ui';

// ProfilePage: the signed-in account — identity, password, and API keys
// (Dokploy-style) with copy-paste MCP setup for VS Code / Claude / Cursor.
export function ProfilePage() {
  const toast = useToast();
  const confirm = useConfirm();

  const [me, setMe] = useState<User | null>(null);
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [newTokenName, setNewTokenName] = useState('');
  const [freshToken, setFreshToken] = useState('');

  const [curPw, setCurPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [confirmPw, setConfirmPw] = useState('');
  const [pwBusy, setPwBusy] = useState(false);

  const loadTokens = () => api.tokens().then((t) => setTokens(t ?? [])).catch(() => {});
  useEffect(() => {
    api.me().then(setMe).catch(() => {});
    loadTokens();
  }, []);

  const changePw = async () => {
    if (newPw.length < 8) { toast.err('New password must be at least 8 characters.'); return; }
    if (newPw !== confirmPw) { toast.err('New passwords don’t match — retype them.'); return; }
    setPwBusy(true);
    try {
      await api.changePassword(curPw, newPw);
      toast.ok('Password changed.');
      setCurPw(''); setNewPw(''); setConfirmPw('');
    } catch (e) { toast.err(cleanError(e)); }
    setPwBusy(false);
  };

  const mkToken = async () => {
    if (!newTokenName.trim()) { toast.err('Give the key a name so you can recognise it later.'); return; }
    try {
      const r = await api.createApiToken(newTokenName.trim());
      setFreshToken(r.token);
      setNewTokenName('');
      loadTokens();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const rmToken = async (t: APIToken) => {
    const yes = await confirm({
      title: `Revoke "${t.name || 'unnamed key'}"?`,
      body: 'Anything using this key — CLI, MCP in your editor, scripts — loses access immediately.',
      confirmLabel: 'Revoke', danger: true,
    });
    if (!yes) return;
    await api.deleteApiToken(t.id).catch((e) => toast.err(cleanError(e)));
    loadTokens();
  };

  const serverUrl = window.location.origin;
  const keyPlaceholder = freshToken || 'rk_YOUR_API_KEY';
  const mcpConfig = `{
  "mcpServers": {
    "rackops": {
      "command": "rackops",
      "args": ["mcp"],
      "env": {
        "RACKOPS_SERVER": "${serverUrl}",
        "RACKOPS_TOKEN": "${keyPlaceholder}"
      }
    }
  }
}`;

  return (
    <div className="page" style={{ maxWidth: 760 }}>
      <div className="page-title">Profile</div>
      <div className="page-sub">Your account, password, and API keys</div>

      {/* Account */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}><UserIcon size={12} /> Account</div>
        {me ? (
          <div className="row wrap" style={{ gap: 24 }}>
            <div>
              <div className="faint">Name</div>
              <strong>{me.fullName || '—'}</strong>
            </div>
            <div>
              <div className="faint">Email</div>
              <strong>{me.email}</strong>
            </div>
            <div>
              <div className="faint">Role
                <InfoTip text="owner: full control incl. updates and users · admin: manage everything · member: deploy and manage apps · viewer: read-only." />
              </div>
              <span className="badge amber">{me.role}</span>
            </div>
            <div>
              <div className="faint">Member since</div>
              <strong>{new Date(me.createdAt).toLocaleDateString()}</strong>
            </div>
          </div>
        ) : <div className="row dim"><Spinner /> loading…</div>}
      </div>

      {/* Change password */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}>
          <KeyRound size={12} /> Change password
          <InfoTip text="Locked out instead? Use “Forgot your password?” on the sign-in screen — the reset link is emailed (if SMTP is set up in Settings) and always written to the server log." wide />
        </div>
        <div className="grid-2">
          <div className="field">
            <label>Current password</label>
            <PasswordInput value={curPw} onChange={setCurPw} autoComplete="current-password" />
          </div>
          <div />
          <div className="field">
            <label>New password</label>
            <PasswordInput value={newPw} onChange={setNewPw} placeholder="8+ characters" autoComplete="new-password" />
          </div>
          <div className="field">
            <label>Confirm new password</label>
            <PasswordInput value={confirmPw} onChange={setConfirmPw} placeholder="type it again" autoComplete="new-password" onEnter={changePw} />
            {confirmPw.length > 0 && confirmPw !== newPw && (
              <span className="faint" style={{ color: 'var(--warn)' }}>doesn&rsquo;t match yet</span>
            )}
          </div>
        </div>
        <button className="btn primary" disabled={pwBusy || !curPw || !newPw} onClick={changePw}>
          {pwBusy ? <Spinner size={13} /> : <KeyRound size={13} />} Update password
        </button>
      </div>

      {/* API keys */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 6 }}>
          <Plug size={12} /> API keys
          <InfoTip text="A key acts as you — anything with it can manage your fleet over the REST API, the CLI, or MCP. Create one key per tool so you can revoke them individually." wide />
        </div>
        <div className="dim" style={{ marginBottom: 14 }}>
          Authenticate the REST API (<span className="mono">Authorization: Bearer rk_…</span> or <span className="mono">x-api-key</span>),
          the CLI, and the MCP server with these. Explore every endpoint in the{' '}
          <a href="/swagger" target="_blank" rel="noreferrer">API docs (Swagger) <ExternalLink size={10} /></a>.
        </div>
        <div className="row" style={{ marginBottom: 14 }}>
          <input className="input" placeholder="Key name (e.g. vscode-mcp, ci-pipeline)" value={newTokenName}
            onChange={(e) => setNewTokenName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && mkToken()} />
          <button className="btn primary" onClick={mkToken}><Plus size={13} /> Create key</button>
        </div>
        {freshToken && (
          <div className="card inset" style={{ borderColor: 'var(--amber)', marginBottom: 14 }}>
            <div className="dim" style={{ marginBottom: 8 }}>
              Copy it now — for your security it&rsquo;s shown <strong>only once</strong>:
            </div>
            <CopyField text={freshToken} />
          </div>
        )}
        {tokens.map((t) => (
          <div className="row spread" key={t.id} style={{ padding: '8px 0', borderTop: '1px solid var(--border)' }}>
            <span>
              {t.name || '(unnamed)'}{' '}
              <span className="faint">
                created {new Date(t.createdAt).toLocaleDateString()}
                {t.lastUsed ? ` · last used ${timeAgo(t.lastUsed)}` : ' · never used'}
              </span>
            </span>
            <button className="btn sm danger" aria-label={`Revoke ${t.name}`} onClick={() => rmToken(t)}>
              <Trash2 size={12} />
            </button>
          </div>
        ))}
        {tokens.length === 0 && <div className="faint">No API keys yet — create one above to connect tools to your rack.</div>}
      </div>

      {/* MCP / editor hookup */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 6 }}>
          <BookOpen size={12} /> Connect your editor (MCP)
          <InfoTip text="MCP (Model Context Protocol) lets AI tools call RackOps directly: “deploy nginx”, “show me the logs for plausible”, “restart jellyfin” — from your editor chat." wide />
        </div>
        <div className="dim" style={{ marginBottom: 12, lineHeight: 1.7 }}>
          The <span className="mono">rackops</span> binary doubles as an MCP server. In <strong>VS Code</strong>{' '}
          add this to <span className="mono">.vscode/mcp.json</span> (or via “MCP: Add Server”); in{' '}
          <strong>Claude Desktop / Claude Code / Cursor</strong> add it to their MCP config —
          same JSON everywhere:
        </div>
        <div className="terminal" style={{ marginBottom: 10, whiteSpace: 'pre', userSelect: 'all' }}>{mcpConfig}</div>
        <div className="faint" style={{ lineHeight: 1.7 }}>
          {freshToken
            ? 'Your fresh key is already filled in above — paste and go.'
            : 'Create an API key above and put it in RACKOPS_TOKEN.'}{' '}
          The machine running the editor needs the <span className="mono">rackops</span> binary
          (same one-line installer) and network access to this server.
        </div>
      </div>
    </div>
  );
}
