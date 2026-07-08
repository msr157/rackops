import { useEffect, useState } from 'react';
import { Settings as SettingsIcon, X, Key, Bell, Copy, Trash2, Plus } from 'lucide-react';
import { api } from '../api';
import type { Settings, APIToken } from '../api';

export function SettingsModal({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<'ai' | 'notify' | 'tokens'>('ai');
  const [s, setS] = useState<Settings>({ notifyDeploys: true, notifyAlerts: true });
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [newTokenName, setNewTokenName] = useState('');
  const [freshToken, setFreshToken] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api.settings().then(setS).catch(() => {});
    api.tokens().then((t) => setTokens(t ?? [])).catch(() => {});
  }, []);

  const save = async () => {
    await api.setSettings(s);
    setSaved(true);
    setTimeout(() => setSaved(false), 1500);
  };

  const mkToken = async () => {
    if (!newTokenName.trim()) return;
    const r = await api.createApiToken(newTokenName.trim());
    setFreshToken(r.token);
    setNewTokenName('');
    api.tokens().then((t) => setTokens(t ?? [])).catch(() => {});
  };

  const set = (patch: Partial<Settings>) => setS({ ...s, ...patch });

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={{ width: 640 }} onClick={(e) => e.stopPropagation()}>
        <div className="row spread" style={{ marginBottom: 16 }}>
          <h2 style={{ margin: 0 }}><SettingsIcon size={15} style={{ verticalAlign: -2 }} /> Settings & Integrations</h2>
          <button className="btn sm" onClick={onClose}><X size={13} /></button>
        </div>

        <div className="row" style={{ marginBottom: 18 }}>
          <button className={`btn sm ${tab === 'ai' ? 'primary' : ''}`} onClick={() => setTab('ai')}><Key size={12} /> AI Providers</button>
          <button className={`btn sm ${tab === 'notify' ? 'primary' : ''}`} onClick={() => setTab('notify')}><Bell size={12} /> Notifications</button>
          <button className={`btn sm ${tab === 'tokens' ? 'primary' : ''}`} onClick={() => setTab('tokens')}><Key size={12} /> API Tokens</button>
        </div>

        {tab === 'ai' && (
          <>
            <div className="field">
              <label>Provider preference</label>
              <select className="input" value={s.aiProvider ?? 'auto'} onChange={(e) => set({ aiProvider: e.target.value })}>
                <option value="auto">Auto — first configured key wins</option>
                <option value="anthropic">Anthropic (Claude)</option>
                <option value="gemini">Google Gemini</option>
                <option value="openai">OpenAI / compatible (incl. Ollama)</option>
                <option value="builtin">Built-in rule engine only</option>
              </select>
            </div>
            <div className="field">
              <label>Anthropic API key</label>
              <input className="input" type="password" placeholder="sk-ant-…" value={s.anthropicKey ?? ''} onChange={(e) => set({ anthropicKey: e.target.value })} />
            </div>
            <div className="field">
              <label>Gemini API key</label>
              <input className="input" type="password" placeholder="AIza…" value={s.geminiKey ?? ''} onChange={(e) => set({ geminiKey: e.target.value })} />
            </div>
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
              <label>Model (OpenAI-compatible only)</label>
              <input className="input" placeholder="gpt-4o-mini / llama3.1" value={s.openaiModel ?? ''} onChange={(e) => set({ openaiModel: e.target.value })} />
            </div>
          </>
        )}

        {tab === 'notify' && (
          <>
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
            <div className="row" style={{ gap: 20, marginBottom: 14 }}>
              <label className="row dim" style={{ cursor: 'pointer' }}>
                <input type="checkbox" checked={s.notifyDeploys} onChange={(e) => set({ notifyDeploys: e.target.checked })} /> Deploy events
              </label>
              <label className="row dim" style={{ cursor: 'pointer' }}>
                <input type="checkbox" checked={s.notifyAlerts} onChange={(e) => set({ notifyAlerts: e.target.checked })} /> Alerts
              </label>
            </div>
          </>
        )}

        {tab === 'tokens' && (
          <>
            <div className="row" style={{ marginBottom: 14 }}>
              <input className="input" placeholder="Token name (e.g. ci-pipeline)" value={newTokenName}
                onChange={(e) => setNewTokenName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && mkToken()} />
              <button className="btn primary" onClick={mkToken}><Plus size={13} /> Create</button>
            </div>
            {freshToken && (
              <div className="card" style={{ borderColor: 'var(--accent)', marginBottom: 14 }}>
                <div className="dim" style={{ marginBottom: 8 }}>Copy now — shown only once:</div>
                <div className="row">
                  <code className="code-block" style={{ flex: 1 }}>{freshToken}</code>
                  <button className="btn sm" onClick={() => navigator.clipboard.writeText(freshToken)}><Copy size={12} /></button>
                </div>
              </div>
            )}
            {tokens.map((t) => (
              <div className="row spread" key={t.id} style={{ padding: '7px 0', borderTop: '1px solid var(--border)' }}>
                <span>{t.name || '(unnamed)'} <span className="faint">{new Date(t.createdAt).toLocaleDateString()}</span></span>
                <button className="btn sm danger" onClick={async () => { await api.deleteApiToken(t.id); api.tokens().then((x) => setTokens(x ?? [])); }}>
                  <Trash2 size={12} />
                </button>
              </div>
            ))}
            {tokens.length === 0 && <div className="faint">No API tokens. Use them with the CLI: <code className="mono">RACKOPS_TOKEN=rk_… rackops nodes</code></div>}
          </>
        )}

        {tab !== 'tokens' && (
          <button className="btn primary" style={{ width: '100%', justifyContent: 'center' }} onClick={save}>
            {saved ? '✓ Saved' : 'Commit Keys & Settings'}
          </button>
        )}
      </div>
    </div>
  );
}
