import { useEffect, useState } from 'react';
import { Key, Bell, Zap, KeyRound, ArrowRight, Building2 } from 'lucide-react';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import type { Settings } from '../api';
import { useToast, cleanError } from '../components/toast';
import { OrgPanel } from '../components/OrgPanel';
import { InfoTip, Spinner } from '../components/ui';

type Tab = 'org' | 'ai' | 'notify' | 'tokens';

export function SettingsPage() {
  const toast = useToast();
  const [params, setParams] = useSearchParams();
  const tab = (['org', 'ai', 'notify', 'tokens'].includes(params.get('tab') ?? '') ? params.get('tab') : 'org') as Tab;
  const setTab = (t: Tab) => setParams({ tab: t }, { replace: true });
  const [s, setS] = useState<Settings>({ notifyDeploys: true, notifyAlerts: true });
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; provider: string; error?: string } | null>(null);

  useEffect(() => {
    api.settings().then(setS).catch(() => {});
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

  const set = (patch: Partial<Settings>) => setS({ ...s, ...patch });
  const prov = s.aiProvider ?? 'auto';
  const show = (p: string) => prov === 'auto' || prov === p;

  return (
    <div className="page" style={{ maxWidth: 720 }}>
      <div className="page-title">Settings</div>
      <div className="page-sub">Organization, Copilot providers, notifications, and API access</div>

      <div className="tabs">
        {([
          ['org', <Building2 size={14} key="o" />, 'Organization'],
          ['ai', <Key size={14} key="a" />, 'Copilot AI'],
          ['notify', <Bell size={14} key="n" />, 'Notifications'],
          ['tokens', <Key size={14} key="t" />, 'API tokens'],
        ] as [Tab, React.ReactNode, string][]).map(([key, icon, label]) => (
          <button key={key} className={`tab ${tab === key ? 'active' : ''}`} onClick={() => setTab(key)}>
            {icon} {label}
          </button>
        ))}
      </div>

      {tab === 'org' && <OrgPanel />}

      {tab === 'ai' && (
        <div className="card">
          <div className="dim" style={{ marginBottom: 14 }}>
            The Copilot answers questions about your fleet using live telemetry.
            <InfoTip wide>
              Keys are stored on your own server (per organization) and sent only to the provider
              you pick. Without a key, the built-in rule engine still answers placement, thermal,
              and failure questions from telemetry — no data leaves your machines.
            </InfoTip>
          </div>
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
          <div className="dim" style={{ marginBottom: 14 }}>
            Get pinged when deploys finish or alerts fire — per organization.
            <InfoTip>
              Fill in any channel you use; empty ones are ignored. Notifications fire for this
              organization&rsquo;s machines and apps only.
            </InfoTip>
          </div>
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
          <div className="row" style={{ gap: 10, marginBottom: 8 }}>
            <KeyRound size={15} style={{ color: 'var(--amber)' }} />
            <strong>API keys moved to your profile</strong>
          </div>
          <div className="dim" style={{ marginBottom: 14, lineHeight: 1.6 }}>
            Keys belong to the person who creates them — they act with your role and are
            managed alongside your account and password.
          </div>
          <Link to="/profile" className="btn primary" style={{ width: 'fit-content' }}>
            Open profile <ArrowRight size={13} />
          </Link>
        </div>
      )}
    </div>
  );
}
