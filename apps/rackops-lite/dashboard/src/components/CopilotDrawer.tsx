import { createContext, useContext, useEffect, useId, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Bot, History, Maximize2, RotateCw, Send, SquarePen, Trash2, X } from 'lucide-react';
import { api, getActiveOrg } from '../api';
import type { ChatTurn, Node as FleetNode, Service } from '../api';
import { useData } from '../data';
import { useConfirm } from './confirm';
import { cleanError } from './toast';
import { Spinner, timeAgo, uid } from './ui';
import { useDialogBehavior } from './useDialog';

export interface Msg {
  id: string;
  role: 'user' | 'bot';
  text: string;
  provider?: string;
  /** Present on failed replies: the prompt Retry resends. */
  prompt?: string;
  error?: boolean;
}

export interface Chat {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  msgs: Msg[];
}

interface CopilotApi {
  open: boolean;
  setOpen: (v: boolean) => void;
  /** All saved conversations, most recently touched first. */
  chats: Chat[];
  activeId: string | null;
  /** Messages of the active conversation ([] when none is open). */
  msgs: Msg[];
  input: string;
  setInput: (v: string) => void;
  /** True while a reply for the ACTIVE conversation is in flight. */
  thinking: boolean;
  send: (text?: string) => void;
  retry: (msgId: string) => void;
  newChat: () => void;
  openChat: (id: string) => void;
  deleteChat: (id: string) => void;
  clearAll: () => void;
}

const CopilotCtx = createContext<CopilotApi | null>(null);

export function useCopilot(): CopilotApi {
  const c = useContext(CopilotCtx);
  if (!c) throw new Error('useCopilot outside CopilotProvider');
  return c;
}

export const STARTERS = [
  'Is anything failing right now?',
  'Where should I deploy a Postgres database?',
  'Why is my machine running hot?',
  'How do I make an app public?',
];

const MAX_CHATS = 50;
const MAX_MSGS = 200;

// History lives in this browser, keyed per org so switching workspaces never
// mixes conversations. Deliberately NOT server state: chat transcripts don't
// belong in state.json, which is re-replicated whole on every mutation.
const storeKey = () => `rackops-copilot-chats:${getActiveOrg() || 'default'}`;

function loadChats(): Chat[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(storeKey()) ?? '[]');
    if (!Array.isArray(parsed)) return [];
    return (parsed as Chat[]).filter((c) => c && typeof c.id === 'string' && Array.isArray(c.msgs));
  } catch {
    return [];
  }
}

function saveChats(chats: Chat[]) {
  try {
    localStorage.setItem(storeKey(), JSON.stringify(chats));
  } catch {
    // Storage full or blocked: chat still works, history just won't survive a reload.
  }
}

// toHistory turns the visible thread into provider turns; error bubbles are
// UI-only and never replayed.
const toHistory = (list: Msg[]): ChatTurn[] =>
  list.filter((m) => !m.error)
    .map((m) => ({ role: m.role === 'user' ? 'user' as const : 'assistant' as const, content: m.text }));

// describePage tells the Co-Pilot what screen the question was asked from, so
// "why is this failing?" means the app on screen, not the whole fleet.
function describePage(pathname: string, services: Service[], nodes: FleetNode[]): string {
  if (pathname === '/') return 'the Home overview page';
  if (pathname === '/apps') return `the Apps page (${services.length} apps listed)`;
  if (pathname.startsWith('/apps/new')) return 'the Deploy-an-app wizard';
  let m = pathname.match(/^\/apps\/([^/]+)(?:\/([^/]+))?/);
  if (m) {
    const s = services.find((x) => x.id === m![1]);
    if (s) {
      const node = nodes.find((n) => n.id === s.nodeId);
      return `the app "${s.name}" (status ${s.status}` +
        `${node ? `, on machine "${node.name}"` : ''}` +
        `${s.domain ? `, domain ${s.domain}` : ''}) — ${m[2] ?? 'overview'} tab`;
    }
  }
  m = pathname.match(/^\/machines\/([^/]+)/);
  if (m) {
    const n = nodes.find((x) => x.id === m![1]);
    if (n) {
      return `the machine "${n.name}" detail page (status ${n.status}, ` +
        `CPU ${Math.round(n.metrics.cpuPct)}%, memory ${Math.round(n.metrics.memPct)}%, ${Math.round(n.metrics.tempC)}°C)`;
    }
  }
  if (pathname === '/machines') return `the Machines page (${nodes.length} machines)`;
  if (pathname === '/network') return 'the Network page (domains, edge, mesh)';
  if (pathname === '/settings') return 'the Settings page';
  if (pathname === '/support') return 'the Support page';
  if (pathname === '/profile') return 'their Profile page';
  if (pathname === '/changelog') return "the What's-new page";
  return ''; // /copilot and unknown paths add nothing useful
}

// CopilotProvider owns every conversation, so the drawer and the /copilot page
// are two views of the same history: switch surfaces mid-thought and nothing
// is lost. Replies are appended by chat id, so an answer that arrives after
// you switched (or deleted) chats lands in the right thread — or nowhere.
export function CopilotProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const [chats, setChats] = useState<Chat[]>(loadChats);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [input, setInput] = useState('');
  // Chat ids with a reply in flight; per-chat, so one slow answer never locks
  // the other conversations.
  const [pending, setPending] = useState<ReadonlySet<string>>(new Set());
  const { services, nodes } = useData();
  const { pathname } = useLocation();

  useEffect(() => { saveChats(chats); }, [chats]);

  // The /copilot page IS the conversation — a drawer over it would show the
  // same thread twice.
  useEffect(() => {
    if (pathname === '/copilot') setOpen(false);
  }, [pathname]);

  const active = chats.find((c) => c.id === activeId) ?? null;
  const msgs = active?.msgs ?? [];
  const thinking = activeId !== null && pending.has(activeId);

  // appendMsg also moves the chat to the front: array order IS recency order,
  // which is what the history lists render and what the size cap trims.
  const appendMsg = (chatId: string, m: Msg) => {
    setChats((cs) => {
      const c = cs.find((x) => x.id === chatId);
      if (!c) return cs; // chat was deleted while the reply was in flight
      const upd: Chat = { ...c, msgs: [...c.msgs, m].slice(-MAX_MSGS), updatedAt: new Date().toISOString() };
      return [upd, ...cs.filter((x) => x.id !== chatId)];
    });
  };

  const ask = async (chatId: string, q: string, history: ChatTurn[], page: string) => {
    setPending((p) => new Set(p).add(chatId));
    let reply: Msg;
    try {
      const r = await api.assistant(q, history, page);
      reply = { id: uid(), role: 'bot', text: r.answer, provider: r.provider };
    } catch (e) {
      reply = { id: uid(), role: 'bot', text: cleanError(e), error: true, prompt: q };
    }
    appendMsg(chatId, reply);
    setPending((p) => {
      const n = new Set(p);
      n.delete(chatId);
      return n;
    });
  };

  const send = (text?: string) => {
    const q = (text ?? input).trim();
    if (!q) return;
    if (activeId && pending.has(activeId)) return;
    let chatId = activeId;
    if (!chatId || !chats.some((c) => c.id === chatId)) {
      chatId = uid();
      const now = new Date().toISOString();
      const title = q.length > 60 ? q.slice(0, 57) + '…' : q;
      setChats((cs) => [{ id: chatId!, title, createdAt: now, updatedAt: now, msgs: [] }, ...cs].slice(0, MAX_CHATS));
      setActiveId(chatId);
    }
    const history = toHistory(active?.msgs ?? []);
    setInput('');
    appendMsg(chatId, { id: uid(), role: 'user', text: q });
    void ask(chatId, q, history, describePage(pathname, services, nodes));
  };

  // Retry swaps the error bubble for a fresh attempt at the same prompt, so
  // the question stays in the thread without being repeated. History is cut at
  // the user message that produced the error — the failed exchange itself is
  // never replayed.
  const retry = (msgId: string) => {
    if (!active || pending.has(active.id)) return;
    const idx = active.msgs.findIndex((m) => m.id === msgId);
    const failed = idx >= 0 ? active.msgs[idx] : undefined;
    if (!failed?.prompt) return;
    let cut = idx;
    for (let i = idx - 1; i >= 0; i--) {
      if (active.msgs[i].role === 'user' && active.msgs[i].text === failed.prompt) { cut = i; break; }
    }
    const history = toHistory(active.msgs.slice(0, cut));
    const chatId = active.id;
    setChats((cs) => cs.map((c) => (c.id === chatId ? { ...c, msgs: c.msgs.filter((m) => m.id !== msgId) } : c)));
    void ask(chatId, failed.prompt, history, describePage(pathname, services, nodes));
  };

  // New chat just points away from the thread; the conversation itself stays
  // in history. Safe even while a reply is in flight — it lands by chat id.
  const newChat = () => {
    setActiveId(null);
    setInput('');
  };

  const openChat = (id: string) => setActiveId(id);

  const deleteChat = (id: string) => {
    setChats((cs) => cs.filter((c) => c.id !== id));
    setActiveId((cur) => (cur === id ? null : cur));
  };

  const clearAll = () => {
    setChats([]);
    setActiveId(null);
  };

  return (
    <CopilotCtx.Provider value={{
      open, setOpen, chats, activeId, msgs, input, setInput, thinking,
      send, retry, newChat, openChat, deleteChat, clearAll,
    }}>
      {children}
      {open && <CopilotPanel onClose={() => setOpen(false)} />}
    </CopilotCtx.Provider>
  );
}

// ChatBubbles renders one conversation thread — shared verbatim by the drawer
// and the /copilot page so a chat looks identical on both surfaces.
export function ChatBubbles({ msgs, thinking, retry }: {
  msgs: Msg[]; thinking: boolean; retry: (id: string) => void;
}) {
  return (
    <>
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
    </>
  );
}

// ChatLiveRegion announces only what's new (the latest reply) instead of
// re-reading the whole thread on every change.
export function ChatLiveRegion({ msgs, thinking }: { msgs: Msg[]; thinking: boolean }) {
  let lastBot: Msg | undefined;
  for (let i = msgs.length - 1; i >= 0; i--) {
    if (msgs[i].role === 'bot') { lastBot = msgs[i]; break; }
  }
  return (
    <div className="sr-only" role="status">
      {thinking ? 'Copilot is thinking…'
        : lastBot ? (lastBot.error ? `Copilot error: ${lastBot.text}` : `Copilot: ${lastBot.text}`) : ''}
    </div>
  );
}

function CopilotPanel({ onClose }: { onClose: () => void }) {
  const { chats, msgs, input, setInput, thinking, send, retry, newChat } = useCopilot();
  const { overview } = useData();
  const navigate = useNavigate();
  const endRef = useRef<HTMLDivElement>(null);
  // The input claims focus itself (autoFocus); the dialog hook sees that and
  // leaves it, so typing can start the moment the drawer opens.
  const ref = useDialogBehavior<HTMLDivElement>(onClose);
  const titleId = useId();
  const inputId = useId();

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [msgs, thinking]);

  // No overlay: the drawer is non-modal on purpose, so the page stays visible
  // and clickable as context while the conversation runs. Escape (dialog hook)
  // and the X still close it. aria-modal stays off because the rest of the
  // page really is interactive.
  return (
    <div ref={ref} className="drawer" tabIndex={-1} role="dialog" aria-labelledby={titleId}>
      <div className="drawer-head">
        <Bot size={16} style={{ color: 'var(--accent)' }} aria-hidden />
        <strong id={titleId}>Copilot</strong>
        <div className="row" style={{ marginLeft: 'auto', gap: 2 }}>
          {chats.length > 0 && <HistoryMenu />}
          <button className="btn sm ghost" onClick={newChat} disabled={msgs.length === 0}>
            <SquarePen size={13} aria-hidden /> New chat
          </button>
          <button className="btn sm ghost" onClick={() => { navigate('/copilot'); onClose(); }}
            aria-label="Open the full Copilot page" title="Open full page">
            <Maximize2 size={13} />
          </button>
          <button className="btn sm ghost"
            onClick={onClose} aria-label="Close Copilot"><X size={14} /></button>
        </div>
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
        <ChatBubbles msgs={msgs} thinking={thinking} retry={retry} />
        <div ref={endRef} />
      </div>

      <ChatLiveRegion msgs={msgs} thinking={thinking} />

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

// HistoryMenu: past conversations without leaving the drawer — reopen, delete
// one, or clear everything. The full-width version of this list lives on the
// /copilot page.
function HistoryMenu() {
  const { chats, activeId, openChat, deleteChat, clearAll } = useCopilot();
  const confirm = useConfirm();
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    // Capture phase so Escape closes the menu and never reaches the drawer's
    // dialog handler — one Escape, one layer.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey, true);
    };
  }, [open]);

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
    if (yes) { clearAll(); setOpen(false); }
  };

  return (
    <div className="chat-hist" ref={boxRef}>
      <button ref={triggerRef} className="btn sm ghost" onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu" aria-expanded={open} title="Chat history">
        <History size={13} aria-hidden /> History
      </button>
      {open && (
        <div className="chat-hist-pop" role="menu" aria-label="Chat history">
          {chats.map((c) => (
            <div key={c.id} className={`chat-rail-item ${c.id === activeId ? 'active' : ''}`}>
              <button role="menuitem" className="chat-rail-open"
                onClick={() => { openChat(c.id); setOpen(false); }}>
                <span className="chat-rail-title">{c.title}</span>
                <span className="faint">{timeAgo(c.updatedAt)}</span>
              </button>
              <button className="chat-rail-del" aria-label={`Delete chat: ${c.title}`}
                title="Delete chat" onClick={() => void del(c)}>
                <Trash2 size={13} />
              </button>
            </div>
          ))}
          <div className="chat-hist-foot">
            <button className="btn sm ghost" onClick={() => void wipe()}>
              <Trash2 size={12} aria-hidden /> Clear history
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
