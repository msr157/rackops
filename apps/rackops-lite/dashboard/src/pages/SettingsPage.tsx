import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, ArrowRight, Bell, Building2, Key, KeyRound, RotateCw, Zap } from 'lucide-react';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import type { Settings } from '../api';
import { useToast, cleanError } from '../components/toast';
import { OrgPanel } from '../components/OrgPanel';
import { Field, InfoTip, Spinner } from '../components/ui';

type Tab = 'org' | 'ai' | 'notify' | 'tokens';

export function SettingsPage() {
  const toast = useToast();
  const [params, setParams] = useSearchParams();
  const tab = (['org', 'ai', 'notify', 'tokens'].includes(params.get('tab') ?? '') ? params.get('tab') : 'org') as Tab;
  const setTab = (t: Tab) => setParams({ tab: t }, { replace: true });
  const [s, setS] = useState<Settings>({ notifyDeploys: true, notifyAlerts: true });
  // The server only ever tests what it has stored, so the UI tracks what's
  // been saved rather than asking the operator to remember.
  const [savedState, setSavedState] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; provider: string; error?: string } | null>(null);
  // Dirty means "differs from what the server has" — meaningless before the
  // server has answered, so it stays false (and Save stays disabled) until then.
  const dirty = loaded && JSON.stringify(s) !== savedState;

  const loadSettings = useCallback(() => {
    setLoadFailed(false);
    api.settings()
      .then((cfg) => { setS(cfg); setSavedState(JSON.stringify(cfg)); setLoaded(true); })
      .catch(() => setLoadFailed(true));
  }, []);
  useEffect(loadSettings, [loadSettings]);

  const save = async () => {
    setSaving(true);
    try {
      await api.setSettings(s);
      setSavedState(JSON.stringify(s));
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

  // Until settings() answers, the whole form stays disabled: typing into
  // defaults that are about to be replaced by the server's values would
  // silently clobber the keystrokes.
  const loadNotice = !loaded && loadFailed && (
    <div className="row" style={{ marginBottom: 14 }}>
      <AlertTriangle size={14} style={{ color: 'var(--crit)' }} aria-hidden />
      <span className="dim" style={{ flex: 1 }}>
        Couldn&rsquo;t load the stored settings — editing stays locked so nothing is overwritten blind.
      </span>
      <button className="btn sm" onClick={loadSettings}><RotateCw size={12} /> Retry</button>
    </div>
  );

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
          {loadNotice}
          <fieldset className="field-set" disabled={!loaded} aria-busy={!loaded}
            style={{ opacity: loaded ? 1 : 0.6 }}>
            <Field label="Provider">
              {(p) => (
                <select className="input" {...p} value={prov} onChange={(e) => set({ aiProvider: e.target.value })}>
                  <option value="auto">Auto — first configured key wins</option>
                  <option value="anthropic">Anthropic (Claude)</option>
                  <option value="gemini">Google Gemini</option>
                  <option value="openai">OpenAI / compatible (incl. Ollama)</option>
                  <option value="builtin">Built-in rule engine only</option>
                </select>
              )}
            </Field>
            {show('anthropic') && (
              <>
                <Field label="Anthropic API key">
                  {(p) => (
                    <input className="input" {...p} type="password" placeholder="sk-ant-…"
                      value={s.anthropicKey ?? ''} onChange={(e) => set({ anthropicKey: e.target.value })} />
                  )}
                </Field>
                <Field label="Anthropic model" hint="Leave blank for the recommended default (Opus 4.8).">
                  {(p) => (
                    <input className="input" {...p} list="anthropic-models" placeholder="claude-opus-4-8 (default)"
                      value={s.anthropicModel ?? ''} onChange={(e) => set({ anthropicModel: e.target.value })} />
                  )}
                </Field>
                <datalist id="anthropic-models">
                  <option value="claude-opus-4-8">Claude Opus 4.8 — most capable</option>
                  <option value="claude-sonnet-5">Claude Sonnet 5 — balanced</option>
                  <option value="claude-haiku-4-5">Claude Haiku 4.5 — fastest</option>
                  <option value="claude-fable-5">Claude Fable 5 — frontier</option>
                </datalist>
              </>
            )}
            {show('gemini') && (
              <>
                <Field label="Gemini API key"
                  hint={'Create at aistudio.google.com — "consumer suspended" means Google suspended that key\'s project; make a key in a fresh project.'}>
                  {(p) => (
                    <input className="input" {...p} type="password" placeholder="AIza…"
                      value={s.geminiKey ?? ''} onChange={(e) => set({ geminiKey: e.target.value })} />
                  )}
                </Field>
                <Field label="Gemini model">
                  {(p) => (
                    <input className="input" {...p} list="gemini-models" placeholder="gemini-3.5-flash (default)"
                      value={s.geminiModel ?? ''} onChange={(e) => set({ geminiModel: e.target.value })} />
                  )}
                </Field>
                <datalist id="gemini-models">
                  <option value="gemini-3.5-flash">Gemini 3.5 Flash — recommended</option>
                  <option value="gemini-3-flash">Gemini 3 Flash</option>
                  <option value="gemini-3.1-flash-lite">Gemini 3.1 Flash-Lite — cheapest</option>
                  <option value="gemini-3-pro-preview">Gemini 3 Pro</option>
                </datalist>
              </>
            )}
            {show('openai') && (
              <>
                <div className="grid-2">
                  <Field label="OpenAI-compatible key">
                    {(p) => (
                      <input className="input" {...p} type="password" placeholder="sk-… (blank for Ollama)"
                        value={s.openaiKey ?? ''} onChange={(e) => set({ openaiKey: e.target.value })} />
                    )}
                  </Field>
                  <Field label="Base URL (Ollama: http://host:11434/v1)">
                    {(p) => (
                      <input className="input" {...p} placeholder="https://api.openai.com/v1"
                        value={s.openaiBase ?? ''} onChange={(e) => set({ openaiBase: e.target.value })} />
                    )}
                  </Field>
                </div>
                <Field label="OpenAI model" hint="Pick a preset or type any model id (Ollama model names work here too).">
                  {(p) => (
                    <input className="input" {...p} list="openai-models" placeholder="gpt-5.5 (default) / llama3.1 for Ollama"
                      value={s.openaiModel ?? ''} onChange={(e) => set({ openaiModel: e.target.value })} />
                  )}
                </Field>
                <datalist id="openai-models">
                  <option value="gpt-5.5">GPT-5.5 — flagship</option>
                  <option value="gpt-5.4-mini">GPT-5.4 mini — cheap/fast</option>
                  <option value="gpt-5.4-nano">GPT-5.4 nano — cheapest</option>
                  <option value="llama3.1">llama3.1 (Ollama)</option>
                  <option value="qwen2.5">qwen2.5 (Ollama)</option>
                </datalist>
              </>
            )}
            {prov === 'builtin' && (
              <div className="dim" style={{ marginBottom: 14 }}>
                No key needed — the rule engine answers placement, thermal, storage and failure questions from live telemetry.
              </div>
            )}
            <div className="row wrap">
              <button className="btn primary" disabled={saving || !dirty} onClick={save}>
                {saving ? <Spinner size={13} /> : null} {!loaded || dirty ? 'Save settings' : 'Saved'}
              </button>
              {/* The test hits the stored key, so it stays out of reach until
                  there is nothing unsaved left to confuse the result. */}
              <button className="btn" disabled={testing || dirty || !loaded} onClick={testProvider}
                title={dirty ? 'Save your changes first — the test uses the stored key' : undefined}>
                {testing ? <Spinner size={13} /> : <Zap size={13} />} Test provider
              </button>
              {dirty && <span className="faint">Save first — the test uses the stored key.</span>}
              {testResult && (
                <span className={`badge ${testResult.ok ? 'ok' : 'crit'}`} role="status" style={{ maxWidth: 340, overflow: 'hidden' }}>
                  {testResult.ok ? `${testResult.provider} OK` : `${testResult.provider}: ${testResult.error?.slice(0, 140)}`}
                </span>
              )}
            </div>
          </fieldset>
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
          {loadNotice}
          <fieldset className="field-set" disabled={!loaded} aria-busy={!loaded}
            style={{ opacity: loaded ? 1 : 0.6 }}>
            <Field label="Discord webhook URL">
              {(p) => (
                <input className="input" {...p} placeholder="https://discord.com/api/webhooks/…"
                  value={s.discordWebhook ?? ''} onChange={(e) => set({ discordWebhook: e.target.value })} />
              )}
            </Field>
            <Field label="Slack incoming webhook URL">
              {(p) => (
                <input className="input" {...p} placeholder="https://hooks.slack.com/services/…"
                  value={s.slackWebhook ?? ''} onChange={(e) => set({ slackWebhook: e.target.value })} />
              )}
            </Field>
            <div className="grid-2">
              <Field label="Telegram bot token">
                {(p) => (
                  <input className="input" {...p} type="password"
                    value={s.telegramToken ?? ''} onChange={(e) => set({ telegramToken: e.target.value })} />
                )}
              </Field>
              <Field label="Telegram chat ID">
                {(p) => (
                  <input className="input" {...p}
                    value={s.telegramChatId ?? ''} onChange={(e) => set({ telegramChatId: e.target.value })} />
                )}
              </Field>
            </div>
            <Field label="Generic webhook (JSON POST — ntfy, Gotify, PagerDuty…)">
              {(p) => (
                <input className="input" {...p} placeholder="https://…"
                  value={s.genericWebhook ?? ''} onChange={(e) => set({ genericWebhook: e.target.value })} />
              )}
            </Field>
            <div className="row" style={{ gap: 20, marginBottom: 16 }}>
              <label className="check">
                <input type="checkbox" checked={s.notifyDeploys} onChange={(e) => set({ notifyDeploys: e.target.checked })} /> Deploy events
              </label>
              <label className="check">
                <input type="checkbox" checked={s.notifyAlerts} onChange={(e) => set({ notifyAlerts: e.target.checked })} /> Alerts
              </label>
            </div>
            <button className="btn primary" disabled={saving || !dirty} onClick={save}>
              {saving ? <Spinner size={13} /> : null} {!loaded || dirty ? 'Save settings' : 'Saved'}
            </button>
          </fieldset>
        </div>
      )}

      {tab === 'tokens' && (
        <div className="card">
          <div className="row" style={{ gap: 10, marginBottom: 8 }}>
            <KeyRound size={15} style={{ color: 'var(--accent)' }} aria-hidden />
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
