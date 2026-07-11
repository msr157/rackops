import { useEffect, useState } from 'react';
import { Key, Bell, Trash2, Plus, Zap } from 'lucide-react';
import { api } from '../api';
import type { Settings, APIToken } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { CopyField, Spinner } from '../components/ui';

type Tab = 'ai' | 'notify' | 'tokens';

export function SettingsPage() {
  const toast = useToast();
  const confirm = useConfirm();
  const [tab, setTab] = useState<Tab>('ai');
  const [s, setS] = useState<Settings>({ notifyDeploys: true, notifyAlerts: true });
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [newTokenName, setNewTokenName] = useState('');
  const [freshToken, setFreshToken] = useState('');
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; provider: string; error?: string } | null>(null);

  useEffect(() => {
    api.settings().then(setS).catch(() => {});
    api.tokens().then((t) => setTokens(t ?? [])).catch(() => {});
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      await api.setSettings(s);
      toast.ok('Settings saved.');
    } catch (e) { toast.err(cleanError(e)); }
    setSaving(false);
  };

  const testProvider = async () => {
    setTesting(true); setTestResult(null);
    try { setTestResult(await api.testAssistant()); }
    catch (e) { setTestResult({ ok: false, provider: '?', error: cleanError(e) }); }
    setTesting(false);
  };

  const mkToken = async () => {
    if (!newTokenName.trim()) return;
    try {
      const r = await api.createApiToken(newTokenName.trim());
      setFreshToken(r.token);
      setNewTokenName('');
      api.tokens().then((t) => setTokens(t ?? [])).catch(() => {});
    } catch (e) { toast.err(cleanError(e)); }
  };

  const rmToken = async (t: APIToken) => {
    const yes = await confirm({
      title: `Revoke "${t.name || 'unnamed token'}"?`,
      body: 'Anything using this token loses access immediately.',
      confirmLabel: 'Revoke', danger: true,
    });
    if (!yes) return;
    await api.deleteApiToken(t.id).catch((e) => toast.err(cleanError(e)));
    api.tokens().then((x) => setTokens(x ?? [])).catch(() => {});
  };

  const set = (patch: Partial<Settings>) => setS({ ...s, ...patch });
  const prov = s.aiProvider ?? 'auto';
  const show = (p: string) => prov === 'auto' || prov === p;

  return (
    <div className="page" style={{ maxWidth: 720 }}>
      <div className="page-title">Settings</div>
      <div className="page-sub">Copilot providers, notifications, and API access</div>

      <div className="tabs">
        {([
          ['ai', <Key size={14} key="a" />, 'Copilot AI'],
          ['notify', <Bell size={14} key="n" />, 'Notifications'],
          ['tokens', <Key size={14} key="t" />, 'API tokens'],
        ] as [Tab, React.ReactNode, string][]).map(([key, icon, label]) => (
          <button key={key} className={`tab ${tab === key ? 'active' : ''}`} onClick={() => setTab(key)}>
            {icon} {label}
          </button>
        ))}
      </div>

      {tab === 'ai' && (
        <div className="card">
          <div className="field">
            <label>Provider</label>
            <select className="input" value={prov} onChange={(e) => set({ aiProvider: e.target.value })}>
              <option value="auto">Auto — first configured key wins</option>
              <option value="anthropic">Anthropic (Claude)</option>
              <option value="gemini">Google Gemini</option>
              <option value="openai">OpenAI / compatible (incl. Ollama)</option>
              <option value="builtin">Built-in rule engine only</option>
            </select>
          </div>
          {show('anthropic') && (
            <>
              <div className="field">
                <label>Anthropic API key</label>
                <input className="input" type="password" placeholder="sk-ant-…" value={s.anthropicKey ?? ''} onChange={(e) => set({ anthropicKey: e.target.value })} />
              </div>
              <div className="field">
                <label>Model</label>
                <input className="input" list="anthropic-models" placeholder="claude-opus-4-8 (default)" value={s.anthropicModel ?? ''} onChange={(e) => set({ anthropicModel: e.target.value })} />
                <datalist id="anthropic-models">
                  <option value="claude-opus-4-8">Claude Opus 4.8 — most capable</option>
                  <option value="claude-sonnet-5">Claude Sonnet 5 — balanced</option>
                  <option value="claude-haiku-4-5">Claude Haiku 4.5 — fastest</option>
                  <option value="claude-fable-5">Claude Fable 5 — frontier</option>
                </datalist>
                <span className="faint">Leave blank for the recommended default (Opus 4.8).</span>
              </div>
            </>
          )}
          {show('gemini') && (
            <>
              <div className="field">
                <label>Gemini API key</label>
                <input className="input" type="password" placeholder="AIza…" value={s.geminiKey ?? ''} onChange={(e) => set({ geminiKey: e.target.value })} />
                <span className="faint">Create at aistudio.google.com — "consumer suspended" means Google suspended that key's project; make a key in a fresh project.</span>
              </div>
              <div className="field">
                <label>Model</label>
                <input className="input" list="gemini-models" placeholder="gemini-3.5-flash (default)" value={s.geminiModel ?? ''} onChange={(e) => set({ geminiModel: e.target.value })} />
                <datalist id="gemini-models">
                  <option value="gemini-3.5-flash">Gemini 3.5 Flash — recommended</option>
                  <option value="gemini-3-flash">Gemini 3 Flash</option>
                  <option value="gemini-3.1-flash-lite">Gemini 3.1 Flash-Lite — cheapest</option>
                  <option value="gemini-3-pro-preview">Gemini 3 Pro</option>
                </datalist>
              </div>
            </>
          )}
          {show('openai') && (
            <>
              <div className="grid-2">
                <div className="field">
                  <label>OpenAI-compatible key</label>
                  <input className="input" type="password" placeholder="sk-… (blank for Ollama)" value={s.openaiKey ?? ''} onChange={(e) => set({ openaiKey: e.target.value })} />
                </div>
                <div className="field">
                  <label>Base URL (Ollama: http://host:11434/v1)</label>
                  <input className="input" placeholder="https://api.openai.com/v1" value={s.openaiBase ?? ''} onChange={(e) => set({ openaiBase: e.target.value })} />
                </div>
              </div>
              <div className="field">
                <label>Model</label>
                <input className="input" list="openai-models" placeholder="gpt-5.5 (default) / llama3.1 for Ollama" value={s.openaiModel ?? ''} onChange={(e) => set({ openaiModel: e.target.value })} />
                <datalist id="openai-models">
                  <option value="gpt-5.5">GPT-5.5 — flagship</option>
                  <option value="gpt-5.4-mini">GPT-5.4 mini — cheap/fast</option>
                  <option value="gpt-5.4-nano">GPT-5.4 nano — cheapest</option>
                  <option value="llama3.1">llama3.1 (Ollama)</option>
                  <option value="qwen2.5">qwen2.5 (Ollama)</option>
                </datalist>
                <span className="faint">Pick a preset or type any model id (Ollama model names work here too).</span>
              </div>
            </>
          )}
          {prov === 'builtin' && (
            <div className="dim" style={{ marginBottom: 14 }}>
              No key needed — the rule engine answers placement, thermal, storage and failure questions from live telemetry.
            </div>
          )}
          <div className="row">
            <button className="btn primary" disabled={saving} onClick={save}>{saving ? <Spinner size={13} /> : null} Save settings</button>
            <button className="btn" disabled={testing} onClick={testProvider}>
              <Zap size={13} /> {testing ? 'Testing…' : 'Test provider (save first)'}
            </button>
            {testResult && (
              <span className={`badge ${testResult.ok ? 'green' : 'red'}`} style={{ maxWidth: 340, overflow: 'hidden' }}>
                {testResult.ok ? `${testResult.provider} OK` : `${testResult.provider}: ${testResult.error?.slice(0, 140)}`}
              </span>
            )}
          </div>
        </div>
      )}

      {tab === 'notify' && (
        <div className="card">
          <div className="field">
            <label>Discord webhook URL</label>
            <input className="input" placeholder="https://discord.com/api/webhooks/…" value={s.discordWebhook ?? ''} onChange={(e) => set({ discordWebhook: e.target.value })} />
          </div>
          <div className="field">
            <label>Slack incoming webhook URL</label>
            <input className="input" placeholder="https://hooks.slack.com/services/…" value={s.slackWebhook ?? ''} onChange={(e) => set({ slackWebhook: e.target.value })} />
          </div>
          <div className="grid-2">
            <div className="field">
              <label>Telegram bot token</label>
              <input className="input" type="password" value={s.telegramToken ?? ''} onChange={(e) => set({ telegramToken: e.target.value })} />
            </div>
            <div className="field">
              <label>Telegram chat ID</label>
              <input className="input" value={s.telegramChatId ?? ''} onChange={(e) => set({ telegramChatId: e.target.value })} />
            </div>
          </div>
          <div className="field">
            <label>Generic webhook (JSON POST — ntfy, Gotify, PagerDuty…)</label>
            <input className="input" placeholder="https://…" value={s.genericWebhook ?? ''} onChange={(e) => set({ genericWebhook: e.target.value })} />
          </div>
          <div className="row" style={{ gap: 20, marginBottom: 16 }}>
            <label className="check">
              <input type="checkbox" checked={s.notifyDeploys} onChange={(e) => set({ notifyDeploys: e.target.checked })} /> Deploy events
            </label>
            <label className="check">
              <input type="checkbox" checked={s.notifyAlerts} onChange={(e) => set({ notifyAlerts: e.target.checked })} /> Alerts
            </label>
          </div>
          <button className="btn primary" disabled={saving} onClick={save}>{saving ? <Spinner size={13} /> : null} Save settings</button>
        </div>
      )}

      {tab === 'tokens' && (
        <div className="card">
          <div className="row" style={{ marginBottom: 14 }}>
            <input className="input" placeholder="Token name (e.g. ci-pipeline)" value={newTokenName}
              onChange={(e) => setNewTokenName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && mkToken()} />
            <button className="btn primary" onClick={mkToken}><Plus size={13} /> Create</button>
          </div>
          {freshToken && (
            <div className="card inset" style={{ borderColor: 'var(--amber)', marginBottom: 14 }}>
              <div className="dim" style={{ marginBottom: 8 }}>Copy now — shown only once:</div>
              <CopyField text={freshToken} />
            </div>
          )}
          {tokens.map((t) => (
            <div className="row spread" key={t.id} style={{ padding: '7px 0', borderTop: '1px solid var(--border)' }}>
              <span>{t.name || '(unnamed)'} <span className="faint">{new Date(t.createdAt).toLocaleDateString()}</span></span>
              <button className="btn sm danger" aria-label={`Revoke ${t.name}`} onClick={() => rmToken(t)}>
                <Trash2 size={12} />
              </button>
            </div>
          ))}
          {tokens.length === 0 && (
            <div className="faint">No API tokens. Use them with the CLI: <code className="mono">RACKOPS_TOKEN=rk_… rackops nodes</code></div>
          )}
        </div>
      )}
    </div>
  );
}
