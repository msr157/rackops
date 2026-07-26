import { createContext, useContext, useEffect, useId, useRef, useState } from 'react';
import { Bot, Send, X } from 'lucide-react';
import { api } from '../api';
import { useData } from '../data';
import { cleanError } from './toast';
import { Spinner } from './ui';
import { useDialogBehavior } from './useDialog';

interface Msg { role: 'you' | 'copilot'; text: string; provider?: string }

interface CopilotApi { open: boolean; setOpen: (v: boolean) => void }

const CopilotCtx = createContext<CopilotApi | null>(null);

export function useCopilot(): CopilotApi {
  const c = useContext(CopilotCtx);
  if (!c) throw new Error('useCopilot outside CopilotProvider');
  return c;
}

const STARTERS = [
  'Is anything failing right now?',
  'Where should I deploy a Postgres database?',
  'Why is my machine running hot?',
  'How do I make an app public?',
];

// CopilotProvider keeps the conversation alive across page changes and
// renders the slide-over drawer on top of any page.
export function CopilotProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  // The conversation lives here, not in the panel, so closing the drawer never
  // throws away what was said.
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState('');
  const [thinking, setThinking] = useState(false);

  const send = async (text?: string) => {
    const q = (text ?? input).trim();
    if (!q || thinking) return;
    setInput('');
    setMsgs((m) => [...m, { role: 'you', text: q }]);
    setThinking(true);
    try {
      const r = await api.assistant(q);
      setMsgs((m) => [...m, { role: 'copilot', text: r.answer, provider: r.provider }]);
    } catch (e) {
      setMsgs((m) => [...m, { role: 'copilot', text: cleanError(e) }]);
    }
    setThinking(false);
  };

  return (
    <CopilotCtx.Provider value={{ open, setOpen }}>
      {children}
      {open && (
        <CopilotPanel msgs={msgs} input={input} thinking={thinking}
          setInput={setInput} send={send} onClose={() => setOpen(false)} />
      )}
    </CopilotCtx.Provider>
  );
}

function CopilotPanel({ msgs, input, thinking, setInput, send, onClose }: {
  msgs: Msg[]; input: string; thinking: boolean;
  setInput: (v: string) => void; send: (text?: string) => void; onClose: () => void;
}) {
  const { overview } = useData();
  const endRef = useRef<HTMLDivElement>(null);
  const ref = useDialogBehavior<HTMLDivElement>(onClose, { initialFocus: 'container' });
  const titleId = useId();
  const inputId = useId();

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [msgs, thinking]);

  return (
    <>
      <div className="drawer-overlay" onClick={onClose} />
      <div ref={ref} className="drawer" tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <div className="row spread" style={{ padding: '14px 16px', borderBottom: '1px solid var(--border)' }}>
          <div className="row" style={{ gap: 8 }}>
            <Bot size={16} style={{ color: 'var(--amber)' }} aria-hidden />
            <strong id={titleId} style={{ fontFamily: 'var(--display)' }}>Copilot</strong>
            <span className="faint">grounded in live telemetry</span>
          </div>
          <button className="btn sm ghost" onClick={onClose} aria-label="Close Copilot"><X size={14} /></button>
        </div>

        <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
          {msgs.length === 0 && (
            <div style={{ textAlign: 'center', paddingTop: 24 }}>
              <Bot size={26} style={{ color: 'var(--amber)', marginBottom: 10 }} aria-hidden />
              <div className="dim" style={{ marginBottom: 6 }}>
                Ask anything about your rack.
              </div>
              <div className="faint" style={{ marginBottom: 18 }}>
                {overview ? `${overview.nodesOnline}/${overview.nodesTotal} machines · ${overview.servicesUp} apps up · ${overview.openAlerts} alerts` : ''}
              </div>
              <div className="stack" style={{ gap: 8 }}>
                {STARTERS.map((s) => (
                  <button key={s} className="btn sm" style={{ justifyContent: 'flex-start' }} onClick={() => send(s)}>{s}</button>
                ))}
              </div>
            </div>
          )}
          <div aria-live="polite">
            {msgs.map((m, i) => (
              <div key={i} style={{ marginBottom: 14 }}>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  {m.role === 'you' ? 'You' : `Copilot${m.provider ? ` · ${m.provider}` : ''}`}
                </div>
                <div style={{
                  background: m.role === 'you' ? 'var(--amber-dim)' : 'var(--bg-2)',
                  border: '1px solid var(--border)', borderRadius: 10, padding: '10px 13px',
                  whiteSpace: 'pre-wrap', lineHeight: 1.65, fontSize: 13,
                }}>
                  {m.text}
                </div>
              </div>
            ))}
            {thinking && <div className="row dim" style={{ padding: 6 }}><Spinner /> thinking…</div>}
          </div>
          <div ref={endRef} />
        </div>

        <div className="row" style={{ padding: 12, borderTop: '1px solid var(--border)' }}>
          <label className="sr-only" htmlFor={inputId}>Ask Copilot</label>
          <input className="input" id={inputId} placeholder="Ask Copilot…" value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && send()} />
          <button className="btn primary" disabled={thinking || !input.trim()} onClick={() => send()} aria-label="Send">
            <Send size={14} />
          </button>
        </div>
      </div>
    </>
  );
}
