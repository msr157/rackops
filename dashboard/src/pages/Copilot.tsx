import { useEffect, useRef, useState } from 'react';
import { Bot, Send, Sparkles, Terminal } from 'lucide-react';
import { api } from '../api';
import type { Overview } from '../api';

interface Msg { role: 'operator' | 'copilot'; text: string; provider?: string }

const BLUEPRINTS = [
  'Where should I deploy a Postgres database?',
  'Why is my node running hot?',
  "What's the storage situation across the fleet?",
  'Is anything failing right now?',
];

export function CopilotPage({ overview }: { overview: Overview | null }) {
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState('');
  const [thinking, setThinking] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [msgs, thinking]);

  const send = async (text?: string) => {
    const q = (text ?? input).trim();
    if (!q || thinking) return;
    setInput('');
    setMsgs((m) => [...m, { role: 'operator', text: q }]);
    setThinking(true);
    try {
      const r = await api.assistant(q);
      setMsgs((m) => [...m, { role: 'copilot', text: r.answer, provider: r.provider }]);
    } catch (e) {
      setMsgs((m) => [...m, { role: 'copilot', text: 'Error: ' + String(e) }]);
    }
    setThinking(false);
  };

  return (
    <>
      <div className="page-title">AI Co-Pilot</div>
      <div className="page-sub">Operations assistant grounded in live fleet telemetry — configure providers in Settings</div>

      <div className="grid-2" style={{ gridTemplateColumns: '260px 1fr', alignItems: 'start' }}>
        <div className="card">
          <div className="kpi-label" style={{ marginBottom: 10 }}><Sparkles size={12} /> Cluster Telemetry</div>
          <div className="dim" style={{ lineHeight: 2 }}>
            Nodes online: <strong className="mono">{overview?.nodesOnline ?? '—'}/{overview?.nodesTotal ?? '—'}</strong><br />
            Services up: <strong className="mono">{overview?.servicesUp ?? '—'}</strong><br />
            Fleet CPU: <strong className="mono">{overview?.avgCpuPct ?? '—'}%</strong><br />
            Open alerts: <strong className="mono">{overview?.openAlerts ?? '—'}</strong>
          </div>
        </div>

        <div className="card" style={{ display: 'flex', flexDirection: 'column', minHeight: 480 }}>
          <div style={{ flex: 1, overflowY: 'auto', maxHeight: 520 }}>
            {msgs.length === 0 && (
              <div style={{ padding: '30px 10px', textAlign: 'center' }}>
                <Bot size={28} style={{ color: 'var(--accent)', marginBottom: 12 }} />
                <div className="dim" style={{ marginBottom: 16 }}>Ask about your infrastructure. Quick blueprints:</div>
                <div className="row" style={{ flexWrap: 'wrap', justifyContent: 'center', gap: 8 }}>
                  {BLUEPRINTS.map((b) => (
                    <button key={b} className="btn sm" onClick={() => send(b)}>{b}</button>
                  ))}
                </div>
              </div>
            )}
            {msgs.map((m, i) => (
              <div key={i} style={{ marginBottom: 14 }}>
                <div className="kpi-label" style={{ marginBottom: 4 }}>
                  {m.role === 'operator' ? 'OPERATOR' : <><Terminal size={11} /> CO-PILOT{m.provider ? ` · ${m.provider}` : ''}</>}
                </div>
                <div style={{
                  background: m.role === 'operator' ? 'var(--accent-dim)' : 'var(--bg-2)',
                  border: '1px solid var(--border)', borderRadius: 10, padding: '10px 14px',
                  whiteSpace: 'pre-wrap', lineHeight: 1.65, fontSize: 13.5,
                }}>
                  {m.text}
                </div>
              </div>
            ))}
            {thinking && <div className="dim" style={{ padding: 8 }}><span className="online-dot" /> synthesizing…</div>}
            <div ref={endRef} />
          </div>
          <div className="row" style={{ marginTop: 12 }}>
            <input className="input" placeholder="Ask the co-pilot… (Enter to send)" value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && send()} />
            <button className="btn primary" disabled={thinking} onClick={() => send()}><Send size={14} /></button>
          </div>
        </div>
      </div>
    </>
  );
}
