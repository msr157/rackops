import { useCallback, useEffect, useId, useMemo, useState } from 'react';
import { AlertTriangle, Bell, Building2, Key, RotateCw, Zap } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api';
import type { Settings } from '../api';
import { useToast, cleanError } from '../components/toast';
import { OrgPanel } from '../components/OrgPanel';
import { Field, InfoTip, Spinner } from '../components/ui';

// The active tab lives in the URL (?tab=…), so refresh keeps the tab and Back
// walks through tabs the operator actually visited. Unknown values fall back
// to the first tab instead of rendering nothing.
const TABS = ['organization', 'copilot', 'notifications'] as const;
type Tab = (typeof TABS)[number];

// Pre-rename URLs (bookmarks, in-app links) keep landing on the tab they meant.
const LEGACY_TABS: Record<string, Tab> = { org: 'organization', ai: 'copilot', notify: 'notifications' };

type VendorId = 'anthropic' | 'gemini' | 'openai';

// One radio-card per provider; what each one is lives in the card itself, so
// the choice never depends on remembering a dropdown's fine print.
const PROVIDERS: { id: string; name: string; hint: string }[] = [
  { id: 'auto', name: 'Auto', hint: 'Uses the first provider with a stored key and falls back to the built-in rule engine.' },
  { id: 'anthropic', name: 'Anthropic', hint: 'Claude models, using your Anthropic API key.' },
  { id: 'gemini', name: 'Gemini', hint: 'Gemini models, using a Google AI Studio key.' },
  { id: 'openai', name: 'OpenAI-compatible', hint: 'OpenAI or any compatible endpoint, including Ollama.' },
  { id: 'builtin', name: 'Built-in', hint: 'The rule engine alone: answers from live telemetry, no key, no data leaves your machines.' },
];

const MODEL_PRESETS: Record<VendorId, string[]> = {
  anthropic: ['claude-opus-4-8', 'claude-sonnet-5', 'claude-haiku-4-5', 'claude-fable-5'],
  gemini: ['gemini-3.5-flash', 'gemini-3-flash', 'gemini-3.1-flash-lite', 'gemini-3-pro-preview'],
  openai: ['gpt-5.5', 'gpt-5.4-mini', 'gpt-5.4-nano', 'llama3.1', 'qwen2.5'],
};

const CUSTOM = '__custom__';

// ModelSelect: presets as a real <select> (ids alone), with a Custom option
// that reveals a free-text input for anything the preset list doesn't cover.
function ModelSelect({ label, presets, defaultId, customHint, value, onChange }: {
  label: string; presets: string[]; defaultId: string; customHint?: string;
  value: string; onChange: (v: string) => void;
}) {
  // Custom stays sticky while its input is empty, so picking it doesn't snap
  // back to Default before anything is typed.
  const [forcedCustom, setForcedCustom] = useState(false);
  const isCustom = forcedCustom || (!!value && !presets.includes(value));
  return (
    <>
      <Field label={label} hint={`Default is ${defaultId}.`}>
        {(p) => (
          <select className="input" {...p} value={isCustom ? CUSTOM : value}
            onChange={(e) => {
              const v = e.target.value;
              if (v === CUSTOM) {
                setForcedCustom(true);
                if (!value || presets.includes(value)) onChange('');
              } else {
                setForcedCustom(false);
                onChange(v);
              }
            }}>
            <option value="">Default</option>
            {presets.map((m) => <option key={m} value={m}>{m}</option>)}
            <option value={CUSTOM}>Custom</option>
          </select>
        )}
      </Field>
      {isCustom && (
        <Field label="Custom model id" hint={customHint ?? 'Any model id the provider accepts.'}>
          {(p) => (
            <input className="input" {...p} value={value} placeholder="model-id"
              onChange={(e) => onChange(e.target.value)} />
          )}
        </Field>
      )}
    </>
  );
}

export function SettingsPage() {
  const toast = useToast();
  const [params, setParams] = useSearchParams();
  const rawTab = params.get('tab') ?? '';
  const tab: Tab = (TABS as readonly string[]).includes(rawTab) ? (rawTab as Tab)
    : LEGACY_TABS[rawTab] ?? 'organization';
  // replace: false pushes a history entry per tab switch, so Back works.
  // Other params (none today) survive the write untouched.
  const setTab = (t: Tab) => setParams((prev) => {
    const next = new URLSearchParams(prev);
    next.set('tab', t);
    return next;
  }, { replace: false });
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
  const vendorProv: VendorId | null =
    prov === 'anthropic' ? 'anthropic' : prov === 'gemini' ? 'gemini' : prov === 'openai' ? 'openai' : null;
  const provGroup = useId();
  // Under Auto, each provider's credentials expand in place instead of all
  // three vendors' fields stacking up at once.
  const [expanded, setExpanded] = useState<VendorId | null>(null);
  // The Auto status lines describe what the server has stored, not what is
  // typed and unsaved: "key stored" must never be a promise.
  const stored = useMemo<Settings | null>(
    () => (savedState ? JSON.parse(savedState) as Settings : null),
    [savedState],
  );

  const vendorFields = (v: VendorId) => v === 'anthropic' ? (
    <>
      <Field label="Anthropic API key">
        {(p) => (
          <input className="input" {...p} type="password" placeholder="sk-ant-…"
            value={s.anthropicKey ?? ''} onChange={(e) => set({ anthropicKey: e.target.value })} />
        )}
      </Field>
      <ModelSelect label="Anthropic model" presets={MODEL_PRESETS.anthropic} defaultId="claude-opus-4-8"
        value={s.anthropicModel ?? ''} onChange={(m) => set({ anthropicModel: m })} />
    </>
  ) : v === 'gemini' ? (
    <>
      <Field label="Gemini API key"
        hint={'Create at aistudio.google.com. "consumer suspended" means Google suspended that key\'s project; make a key in a fresh project.'}>
        {(p) => (
          <input className="input" {...p} type="password" placeholder="AIza…"
            value={s.geminiKey ?? ''} onChange={(e) => set({ geminiKey: e.target.value })} />
        )}
      </Field>
      <ModelSelect label="Gemini model" presets={MODEL_PRESETS.gemini} defaultId="gemini-3.5-flash"
        value={s.geminiModel ?? ''} onChange={(m) => set({ geminiModel: m })} />
    </>
  ) : (
    <>
      <div className="grid-2">
        <Field label="OpenAI-compatible key" hint="Leave blank for Ollama.">
          {(p) => (
            <input className="input" {...p} type="password" placeholder="sk-…"
              value={s.openaiKey ?? ''} onChange={(e) => set({ openaiKey: e.target.value })} />
          )}
        </Field>
        <Field label="Base URL" hint="Ollama: http://host:11434/v1">
          {(p) => (
            <input className="input" {...p} placeholder="https://api.openai.com/v1"
              value={s.openaiBase ?? ''} onChange={(e) => set({ openaiBase: e.target.value })} />
          )}
        </Field>
      </div>
      <ModelSelect label="OpenAI model" presets={MODEL_PRESETS.openai} defaultId="gpt-5.5"
        customHint="Any model id works here, including Ollama model names."
        value={s.openaiModel ?? ''} onChange={(m) => set({ openaiModel: m })} />
    </>
  );

  // Until settings() answers, the whole form stays disabled: typing into
  // defaults that are about to be replaced by the server's values would
  // silently clobber the keystrokes.
  const loadNotice = !loaded && loadFailed && (
    <div className="row" style={{ marginBottom: 14 }}>
      <AlertTriangle size={14} style={{ color: 'var(--crit)' }} aria-hidden />
      <span className="dim" style={{ flex: 1 }}>
        Couldn&rsquo;t load the stored settings. Editing stays locked so nothing is overwritten blind.
      </span>
      <button className="btn sm" onClick={loadSettings}><RotateCw size={12} /> Retry</button>
    </div>
  );

  return (
    <div className="page" style={{ maxWidth: 720 }}>
      <div className="page-title">Settings</div>
      <div className="page-sub">Organization, Copilot providers, and notifications</div>

      <div className="tabs">
        {([
          ['organization', <Building2 size={14} key="o" />, 'Organization'],
          ['copilot', <Key size={14} key="a" />, 'Copilot AI'],
          ['notifications', <Bell size={14} key="n" />, 'Notifications'],
        ] as [Tab, React.ReactNode, string][]).map(([key, icon, label]) => (
          <button key={key} className={`tab ${tab === key ? 'active' : ''}`} onClick={() => setTab(key)}>
            {icon} {label}
          </button>
        ))}
      </div>

      {tab === 'organization' && <OrgPanel />}

      {tab === 'copilot' && (
        <div className="card">
          <div className="dim" style={{ marginBottom: 14 }}>
            The Copilot answers questions about your fleet using live telemetry.
            <InfoTip wide>
              Keys are stored on your own server (per organization) and sent only to the provider
              you pick. Without a key, the built-in rule engine still answers placement, thermal,
              and failure questions from telemetry; no data leaves your machines.
            </InfoTip>
          </div>
          {loadNotice}
          <fieldset className="field-set" disabled={!loaded} aria-busy={!loaded}
            style={{ opacity: loaded ? 1 : 0.6 }}>
            <fieldset className="field-set" style={{ marginBottom: 14 }}>
              <legend>Provider</legend>
              <div className="stack" style={{ gap: 6 }}>
                {PROVIDERS.map((pr) => (
                  <label key={pr.id} className={`card inset check ${prov === pr.id ? 'accent' : ''}`}
                    style={{ alignItems: 'flex-start', gap: 10, padding: '10px 12px' }}>
                    <input type="radio" name={provGroup} value={pr.id} checked={prov === pr.id}
                      onChange={() => set({ aiProvider: pr.id })} style={{ marginTop: 3 }} />
                    <span style={{ minWidth: 0 }}>
                      <span className="row" style={{ gap: 8 }}>
                        <strong style={{ fontSize: 13, color: 'var(--fg)' }}>{pr.name}</strong>
                        {pr.id === 'auto' && <span className="badge accent">Recommended</span>}
                      </span>
                      <span className="faint" style={{ display: 'block' }}>{pr.hint}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
            {prov === 'auto' && (
              <div className="card inset" style={{ marginBottom: 14, padding: '4px 14px 10px' }}>
                <div className="list">
                  {(['anthropic', 'gemini', 'openai'] as VendorId[]).map((v) => {
                    const vName = v === 'anthropic' ? 'Anthropic' : v === 'gemini' ? 'Gemini' : 'OpenAI-compatible';
                    const status = v === 'anthropic' ? (stored?.anthropicKey ? 'key stored' : 'not configured')
                      : v === 'gemini' ? (stored?.geminiKey ? 'key stored' : 'not configured')
                      : stored?.openaiKey ? 'key stored'
                      : stored?.openaiBase ? 'endpoint set' : 'not configured';
                    const isOpen = expanded === v;
                    return (
                      <div key={v} className="list-row" style={{ display: 'block', padding: '9px 2px' }}>
                        <div className="row spread">
                          <span className="dim">
                            {vName}:{' '}
                            <span style={{ color: status === 'not configured' ? 'var(--faint)' : 'var(--fg)' }}>
                              {status}
                            </span>
                          </span>
                          <button type="button" className="btn sm ghost" aria-expanded={isOpen}
                            aria-label={`${isOpen ? 'Hide' : 'Edit'} ${vName} credentials`}
                            onClick={() => setExpanded(isOpen ? null : v)}>
                            {isOpen ? 'Hide' : 'Edit'}
                          </button>
                        </div>
                        {isOpen && <div style={{ marginTop: 10 }}>{vendorFields(v)}</div>}
                      </div>
                    );
                  })}
                </div>
                <div className="faint" style={{ marginTop: 6 }}>
                  Auto picks the first configured provider above and falls back to the built-in rule engine.
                </div>
              </div>
            )}
            {vendorProv && vendorFields(vendorProv)}
            {prov === 'builtin' && (
              <div className="dim" style={{ marginBottom: 14 }}>
                No key needed. The rule engine answers placement, thermal, storage, and failure
                questions from live telemetry.
              </div>
            )}
            <div className="row wrap">
              <button className="btn primary" disabled={saving || !dirty} onClick={save}>
                {saving ? <Spinner size={13} /> : null} {!loaded || dirty ? 'Save settings' : 'Saved'}
              </button>
              {/* The test hits the stored key, so it stays out of reach until
                  there is nothing unsaved left to confuse the result. */}
              <button className="btn" disabled={testing || dirty || !loaded} onClick={testProvider}
                title={dirty ? 'Save your changes first. The test uses the stored key.' : undefined}>
                {testing ? <Spinner size={13} /> : <Zap size={13} />} Test provider
              </button>
              {dirty && <span className="faint">Save first. The test uses the stored key.</span>}
              {testResult && (
                <span className={`badge ${testResult.ok ? 'ok' : 'crit'}`} role="status" style={{ maxWidth: 340, overflow: 'hidden' }}>
                  {testResult.ok ? `${testResult.provider} OK` : `${testResult.provider}: ${testResult.error?.slice(0, 140)}`}
                </span>
              )}
            </div>
          </fieldset>
        </div>
      )}

      {tab === 'notifications' && (
        <div className="card">
          <div className="dim" style={{ marginBottom: 14 }}>
            Get pinged when deploys finish or alerts fire, per organization.
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
            <Field label="Generic webhook (JSON POST: ntfy, Gotify, PagerDuty…)">
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
    </div>
  );
}
