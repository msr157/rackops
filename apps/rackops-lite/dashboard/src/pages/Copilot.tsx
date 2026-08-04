import { useEffect, useId, useRef } from 'react';
import { Bot, Send, SquarePen, Trash2 } from 'lucide-react';
import { ChatBubbles, ChatLiveRegion, STARTERS, useCopilot } from '../components/CopilotDrawer';
import type { Chat } from '../components/CopilotDrawer';
import { useConfirm } from '../components/confirm';
import { Crumbs, Spinner, timeAgo } from '../components/ui';
import { useData } from '../data';

// CopilotPage: the full-width home of the AI Co-Pilot. Same brain as the
// drawer — identical conversations, shared history — but with room to work:
// a rail of past chats on the left, the thread on the right.
export function CopilotPage() {
  const {
    chats, activeId, msgs, input, setInput, thinking,
    send, retry, newChat, openChat, deleteChat, clearAll,
  } = useCopilot();
  const { overview } = useData();
  const confirm = useConfirm();
  const endRef = useRef<HTMLDivElement>(null);
  const inputId = useId();

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [msgs, thinking]);

  const del = async (c: Chat) => {
    const yes = await confirm({
      title: 'Delete this chat?',
      body: `“${c.title}” will be removed from this browser.`,
      confirmLabel: 'Delete chat',
      danger: true,
    });
    if (yes) deleteChat(c.id);
  };

  const wipe = async () => {
    const yes = await confirm({
      title: 'Clear chat history?',
      body: 'All saved Copilot chats in this browser will be removed.',
      confirmLabel: 'Clear history',
      danger: true,
    });
    if (yes) clearAll();
  };

  return (
    <div className="page chat-page">
      <Crumbs items={[{ label: 'Copilot', icon: <Bot size={13} aria-hidden /> }]} />
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Copilot</div>
        <button className="btn" onClick={newChat} disabled={msgs.length === 0}>
          <SquarePen size={14} aria-hidden /> New chat
        </button>
      </div>
      <div className="page-sub">
        Ask anything about your rack. Answers are grounded in your fleet&rsquo;s live state.
        Chats are saved in this browser.
      </div>

      <div className="chat-layout">
        <aside className="chat-rail" aria-label="Chat history">
          <div className="chat-rail-list">
            {chats.map((c) => (
              <div key={c.id} className={`chat-rail-item ${c.id === activeId ? 'active' : ''}`}>
                <button className="chat-rail-open" onClick={() => openChat(c.id)}
                  aria-current={c.id === activeId ? 'true' : undefined}>
                  <span className="chat-rail-title">{c.title}</span>
                  <span className="faint">{timeAgo(c.updatedAt)}</span>
                </button>
                <button className="chat-rail-del" aria-label={`Delete chat: ${c.title}`}
                  title="Delete chat" onClick={() => void del(c)}>
                  <Trash2 size={13} />
                </button>
              </div>
            ))}
            {chats.length === 0 && (
              <div className="faint" style={{ padding: '6px 4px' }}>
                Chats you start are saved here.
              </div>
            )}
          </div>
          {chats.length > 0 && (
            <button className="btn sm ghost" onClick={() => void wipe()}>
              <Trash2 size={12} aria-hidden /> Clear history
            </button>
          )}
        </aside>

        <section className="chat-panel" aria-label="Conversation">
          <div className="chat-list">
            {msgs.length === 0 && (
              <div style={{ textAlign: 'center', paddingTop: 40 }}>
                <Bot size={30} style={{ color: 'var(--accent)', marginBottom: 12 }} aria-hidden />
                <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>
                  Ask anything about your rack.
                </div>
                <div className="faint">
                  {overview ? `${overview.nodesOnline}/${overview.nodesTotal} machines online · ${overview.servicesUp} apps up, ${overview.openAlerts} alerts` : ''}
                </div>
                <div className="starter-grid">
                  {STARTERS.map((s) => (
                    <button key={s} className="btn sm" disabled={thinking} onClick={() => send(s)}>
                      {s}
                    </button>
                  ))}
                </div>
              </div>
            )}
            <ChatBubbles msgs={msgs} thinking={thinking} retry={retry} />
            <div ref={endRef} />
          </div>

          <ChatLiveRegion msgs={msgs} thinking={thinking} />

          <div className="chat-composer">
            <label className="sr-only" htmlFor={inputId}>Ask Copilot</label>
            <input className="input" id={inputId} placeholder="Ask Copilot…" autoFocus value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && send()} />
            <button className="btn primary" disabled={thinking || !input.trim()}
              onClick={() => send()} aria-label="Send" title="Send">
              {thinking ? <Spinner size={14} /> : <Send size={14} />}
            </button>
          </div>
        </section>
      </div>
    </div>
  );
}
