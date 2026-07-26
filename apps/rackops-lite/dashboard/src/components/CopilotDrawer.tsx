import { createContext, useContext, useEffect, useId, useRef, useState } from 'react';
import { Bot, RotateCw, Send, SquarePen, X } from 'lucide-react';
import { api } from '../api';
import { useData } from '../data';
import { cleanError } from './toast';
import { Spinner } from './ui';
import { useDialogBehavior } from './useDialog';

interface Msg {
  id: number;
  role: 'user' | 'bot';
  text: string;
  provider?: string;
  /** Present on failed replies: the prompt Retry resends. */
  prompt?: string;
  error?: boolean;
}

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
  const nextId = useRef(1);

  // ask fires one request and appends the reply. A failure becomes an error
  // bubble that remembers its prompt, so Retry can resend it.
  const ask = async (q: string) => {
    setThinking(true);
    let reply: Msg;
    try {
      const r = await api.assistant(q);
      reply = { id: nextId.current++, role: 'bot', text: r.answer, provider: r.provider };
    } catch (e) {
      reply = { id: nextId.current++, role: 'bot', text: cleanError(e), error: true, prompt: q };
    }
    setMsgs((m) => [...m, reply]);
    setThinking(false);
  };

  const send = async (text?: string) => {
    const q = (text ?? input).trim();
    if (!q || thinking) return;
    setInput('');
    const mine: Msg = { id: nextId.current++, role: 'user', text: q };
    setMsgs((m) => [...m, mine]);
    await ask(q);
  };

  // Retry swaps the error bubble for a fresh attempt at the same prompt, so
  // the question stays in the thread without being repeated.
  const retry = (id: number) => {
    if (thinking) return;
    const failed = msgs.find((m) => m.id === id);
    if (!failed?.prompt) return;
    setMsgs((m) => m.filter((x) => x.id !== id));
    void ask(failed.prompt);
  };

  // New chat wipes the thread for a fresh question. Blocked while a reply is
  // in flight, or the late reply would land in the emptied thread.
  const reset = () => {
    if (thinking) return;
    setMsgs([]);
    setInput('');
  };

  return (
    <CopilotCtx.Provider value={{ open, setOpen }}>
      {children}
      {open && (
        <CopilotPanel msgs={msgs} input={input} thinking={thinking}
          setInput={setInput} send={send} retry={retry} reset={reset}
          onClose={() => setOpen(false)} />
      )}
    </CopilotCtx.Provider>
  );
}

function CopilotPanel({ msgs, input, thinking, setInput, send, retry, reset, onClose }: {
  msgs: Msg[]; input: string; thinking: boolean;
  setInput: (v: string) => void; send: (text?: string) => void;
  retry: (id: number) => void; reset: () => void; onClose: () => void;
}) {
  const { overview } = useData();
  const endRef = useRef<HTMLDivElement>(null);
  // The input claims focus itself (autoFocus); the dialog hook sees that and
  // leaves it, so typing can start the moment the drawer opens.
  const ref = useDialogBehavior<HTMLDivElement>(onClose);
  const titleId = useId();
  const inputId = useId();

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [msgs, thinking]);

  // Announce only what's new (the latest reply) instead of re-reading the
  // whole thread on every change.
  let lastBot: Msg | undefined;
  for (let i = msgs.length - 1; i >= 0; i--) {
    if (msgs[i].role === 'bot') { lastBot = msgs[i]; break; }
  }

  // No overlay: the drawer is non-modal on purpose, so the page stays visible
  // and clickable as context while the conversation runs. Escape (dialog hook)
  // and the X still close it. aria-modal stays off because the rest of the
  // page really is interactive.
  return (
    <div ref={ref} className="drawer" tabIndex={-1} role="dialog" aria-labelledby={titleId}>
      <div className="drawer-head">
        <Bot size={16} style={{ color: 'var(--accent)' }} aria-hidden />
        <strong id={titleId}>Copilot</strong>
        <button className="btn sm ghost" style={{ marginLeft: 'auto' }}
          onClick={reset} disabled={thinking || msgs.length === 0}>
          <SquarePen size={13} aria-hidden /> New chat
        </button>
        <button className="btn sm ghost"
          onClick={onClose} aria-label="Close Copilot"><X size={14} /></button>
      </div>

      <div className="chat-list">
        {msgs.length === 0 && (
          <div style={{ textAlign: 'center', paddingTop: 24 }}>
            <Bot size={26} style={{ color: 'var(--accent)', marginBottom: 10 }} aria-hidden />
            <div className="dim" style={{ marginBottom: 6 }}>
              Ask anything about your rack.
            </div>
            <div className="faint" style={{ marginBottom: 18 }}>
              {overview ? `${overview.nodesOnline}/${overview.nodesTotal} machines online · ${overview.servicesUp} apps up, ${overview.openAlerts} alerts` : ''}
            </div>
            <div className="stack" style={{ gap: 8 }}>
              {STARTERS.map((s) => (
                <button key={s} className="btn sm" style={{ justifyContent: 'flex-start' }}
                  disabled={thinking} onClick={() => send(s)}>{s}</button>
              ))}
            </div>
          </div>
        )}
        {msgs.map((m) => (
          m.error ? (
            <div key={m.id} className="chat-msg error">
              {m.text}
              <div style={{ marginTop: 8 }}>
                <button className="btn sm" disabled={thinking} onClick={() => retry(m.id)}>
                  <RotateCw size={12} /> Retry
                </button>
              </div>
            </div>
          ) : (
            <div key={m.id} className={`chat-msg ${m.role === 'user' ? 'user' : 'bot'}`}>
              {m.text}
              {m.provider && <div className="faint" style={{ marginTop: 6 }}>Copilot · {m.provider}</div>}
            </div>
          )
        ))}
        {thinking && <div className="row dim" style={{ gap: 8 }}><Spinner size={13} /> thinking…</div>}
        <div ref={endRef} />
      </div>

      {/* Live region scoped to the newest reply, not the whole thread. */}
      <div className="sr-only" role="status">
        {thinking ? 'Copilot is thinking…'
          : lastBot ? (lastBot.error ? `Copilot error: ${lastBot.text}` : `Copilot: ${lastBot.text}`) : ''}
      </div>

      <div className="drawer-foot row">
        <label className="sr-only" htmlFor={inputId}>Ask Copilot</label>
        <input className="input" id={inputId} placeholder="Ask Copilot…" autoFocus value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && send()} />
        <button className="btn primary" disabled={thinking || !input.trim()}
          onClick={() => send()} aria-label="Send" title="Send">
          {thinking ? <Spinner size={14} /> : <Send size={14} />}
        </button>
      </div>
    </div>
  );
}
