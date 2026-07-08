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
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; provider: string; error?: string } | null>(null);

  const testProvider = async () => {
    setTesting(true); setTestResult(null);
    try { setTestResult(await api.testAssistant()); }
    catch (e) { setTestResult({ ok: false, provider: '?', error: String(e) }); }
    setTesting(false);
  };

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

        {tab === 'ai' && (() => {
          const prov = s.aiProvider ?? 'auto';
          const show = (p: string) => prov === 'auto' || prov === p;
          return (
            <>
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
              <div className="row" style={{ marginBottom: 12 }}>
                <button className="btn sm" disabled={testing} onClick={testProvider}>
                  {testing ? 'Testing…' : '⚡ Test provider (save first)'}
                </button>
                {testResult && (
                  <span className={`badge ${testResult.ok ? 'green' : 'red'}`} style={{ maxWidth: 380, overflow: 'hidden' }}>
                    {testResult.ok ? `${testResult.provider} OK` : `${testResult.provider}: ${testResult.error?.slice(0, 160)}`}
                  </span>
                )}
              </div>
            </>
          );
        })()}

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
