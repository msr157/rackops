import { useCallback, useEffect, useId, useRef, useState } from 'react';
import {
  AlertTriangle, ArrowLeft, CheckCircle2, LifeBuoy, MessageSquareText, Plus, RotateCw, Send,
} from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { api, subscribe } from '../api';
import type { SupportTicket } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { Crumbs, EmptyState, Field, InfoTip, Pill, SkeletonRows, Spinner, timeAgo } from '../components/ui';

const CATEGORIES = [
  ['general', 'General question'],
  ['deploys', 'Deploys & apps'],
  ['networking', 'Networking & domains'],
  ['updates', 'Updates & install'],
  ['bug', 'Bug report'],
  ['billing', 'Billing'],
] as const;

// Option labels are the severity names alone; what each one means lives in
// the field hint below the select, keyed to the current choice.
const SEVERITIES = [
  ['low', 'Low'],
  ['normal', 'Normal'],
  ['high', 'High'],
  ['urgent', 'Urgent'],
] as const;

const SEVERITY_HINTS: Record<string, string> = {
  low: 'No rush.',
  normal: 'A standard question or request.',
  high: 'Something important is broken.',
  urgent: 'Production is down.',
};

// Support: talk to RackOps support without leaving the dashboard. Tickets are
// forwarded to the support desk; replies land back in the thread (~1 min).
export function SupportPage() {
  const confirm = useConfirm();
  const [tickets, setTickets] = useState<SupportTicket[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  // The open ticket lives in the URL, so a refresh (or a shared link) lands
  // back on the same thread instead of the inbox.
  const [params, setParams] = useSearchParams();
  const open = params.get('ticket') ?? '';
  const [composing, setComposing] = useState(false);
  // A typed-but-unsent reply in the open thread; a ref because only the
  // navigation guards read it, never the render.
  const replyDirty = useRef(false);

  const setOpen = useCallback((id: string) => {
    setParams(id ? { ticket: id } : {}, { replace: true });
  }, [setParams]);

  // Leaving a thread with a typed reply pauses for the same decision the
  // compose form asks for — real words must not vanish on a stray click.
  const guardReply = useCallback(async (): Promise<boolean> => {
    if (!replyDirty.current) return true;
    const yes = await confirm({
      title: 'Discard your reply?',
      body: 'The reply you typed will be lost.',
      confirmLabel: 'Discard reply',
      danger: true,
    });
    if (yes) replyDirty.current = false;
    return yes;
  }, [confirm]);

  const load = useCallback(() => {
    api.tickets()
      .then((t) => { setTickets(t ?? []); setLoaded(true); setLoadFailed(false); })
      .catch(() => setLoadFailed(true));
  }, []);
  useEffect(() => {
    load();
    // Agent replies arrive over SSE; polling covers missed events.
    const off = subscribe({ ticket_update: () => load() });
    const t = setInterval(load, 30000);
    return () => { off(); clearInterval(t); };
  }, [load]);

  const active = tickets.filter((t) => t.status !== 'resolved');
  const resolved = tickets.filter((t) => t.status === 'resolved');
  const current = tickets.find((t) => t.id === open);
  // One visible way in at a time: the header button yields only while the
  // compose form (the action itself) is open. The empty state never carries
  // its own copy of the action; the header owns it.
  const showNewTicket = !composing;

  return (
    <div className="page" style={{ maxWidth: 760 }}>
      <Crumbs items={[{ label: 'Support', icon: <LifeBuoy size={13} aria-hidden /> }]} />
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Support</div>
        {showNewTicket && (
          <button className="btn primary"
            onClick={async () => {
              if (!(await guardReply())) return;
              setComposing(true); setOpen('');
            }}>
            <Plus size={14} aria-hidden /> New ticket
          </button>
        )}
      </div>
      <div className="page-sub">
        Ask RackOps support for help. Replies show up right here
        <InfoTip wide>
          Tickets are sent to the RackOps support team along with an anonymous usage summary
          (version, machine/app counts, which features you use; never names, domains, or
          credentials). It helps support see your setup at a glance and helps us improve RackOps.
        </InfoTip>
      </div>

      {composing && <Compose onDone={(id) => { setComposing(false); load(); setOpen(id); }} onCancel={() => setComposing(false)} />}

      {current ? (
        <Thread key={current.id} ticket={current} onChanged={load}
          onDirty={(d) => { replyDirty.current = d; }}
          onBack={async () => { if (await guardReply()) setOpen(''); }} />
      ) : !composing && (
        <>
          {/* Until the first fetch answers, this is a list on its way — never
              an empty inbox, and never a silent nothing when the fetch failed. */}
          {!loaded && !loadFailed && <SkeletonRows count={3} height={46} />}
          {!loaded && loadFailed && (
            <div className="notice crit">
              <AlertTriangle size={14} style={{ color: 'var(--crit)' }} aria-hidden />
              <span className="dim" style={{ flex: 1 }}>
                Couldn&rsquo;t load your tickets. The server didn&rsquo;t answer.
              </span>
              <button className="btn sm" onClick={load}><RotateCw size={12} /> Retry</button>
            </div>
          )}
          {loaded && tickets.length === 0 && (
            <EmptyState icon={<LifeBuoy size={18} />} title="No tickets yet">
              <div>Stuck on something? Open a ticket with the button above. A human answers.</div>
            </EmptyState>
          )}
          {active.length > 0 && <TicketList title="Active" items={active} onOpen={setOpen} />}
          {resolved.length > 0 && <TicketList title="Resolved" items={resolved} onOpen={setOpen} />}
        </>
      )}
    </div>
  );
}

function ticketLed(status: SupportTicket['status']): string {
  return status === 'open' ? 'pending' : status === 'pending' ? 'running' : 'stopped';
}

function statusLabel(status: SupportTicket['status']): string {
  return status === 'open' ? 'waiting for support'
    : status === 'pending' ? 'support replied, your turn' : 'resolved';
}

// One severity treatment everywhere: status ink only for real escalations,
// neutral tag for the ordinary facts (low, normal).
function SeverityTag({ severity }: { severity: string }) {
  if (severity === 'urgent') return <span className="badge crit">urgent</span>;
  if (severity === 'high') return <span className="badge warn">high</span>;
  return <span className="tag">{severity}</span>;
}

function TicketList({ title, items, onOpen }: {
  title: string; items: SupportTicket[]; onOpen: (id: string) => void;
}) {
  return (
    <>
      <div className="section-title">{title}</div>
      <div className="list" style={{ marginBottom: 18 }}>
        {/* Opening a ticket is a state change, not navigation — so each row
            is a button, which Tab reaches and Enter activates. .ticket-row
            strips the button chrome; the hairline stays with .list-row. */}
        {items.map((t) => (
          <button key={t.id} type="button" className="list-row ticket-row"
            onClick={() => onOpen(t.id)}>
            <div className="row" style={{ minWidth: 0, flex: 1 }}>
              <strong style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{t.subject}</strong>
              <span className="tag">{t.category}</span>
              <SeverityTag severity={t.severity} />
            </div>
            <Pill status={ticketLed(t.status)}>{statusLabel(t.status)}</Pill>
            <span className="faint" style={{ whiteSpace: 'nowrap' }}>{timeAgo(t.updatedAt)}</span>
          </button>
        ))}
      </div>
    </>
  );
}

function Compose({ onDone, onCancel }: { onDone: (id: string) => void; onCancel: () => void }) {
  const toast = useToast();
  const confirm = useConfirm();
  const [subject, setSubject] = useState('');
  const [category, setCategory] = useState('general');
  const [severity, setSeverity] = useState('normal');
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (busy) return;
    if (!subject.trim() || !body.trim()) { toast.err('Give the ticket a subject and describe the problem.'); return; }
    setBusy(true);
    try {
      const t = await api.createTicket({ subject: subject.trim(), category, severity, body: body.trim() });
      toast.ok('Ticket sent. Support usually answers within a business day.');
      onDone(t.id);
    } catch (e) { toast.err(cleanError(e)); setBusy(false); }
  };

  // Cancel with typed content pauses for a decision — a half-written problem
  // report is real work, and one stray click must not eat it.
  const cancel = async () => {
    if (subject.trim() || body.trim()) {
      const yes = await confirm({
        title: 'Discard this ticket?',
        body: 'The draft you typed will be lost.',
        confirmLabel: 'Discard draft',
        danger: true,
      });
      if (!yes) return;
    }
    onCancel();
  };

  return (
    <section style={{ marginBottom: 18 }}>
      <div className="section-title" style={{ marginTop: 4 }}>New support ticket</div>
      <Field label="Subject">
        {(p) => (
          <input className="input" {...p} value={subject} placeholder="One line: what's wrong?"
            onChange={(e) => setSubject(e.target.value)} autoFocus />
        )}
      </Field>
      <div className="grid-2">
        <Field label="Category">
          {(p) => (
            <select className="input" {...p} value={category} onChange={(e) => setCategory(e.target.value)}>
              {CATEGORIES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
          )}
        </Field>
        <Field label="Severity" hint={SEVERITY_HINTS[severity]}>
          {(p) => (
            <select className="input" {...p} value={severity} onChange={(e) => setSeverity(e.target.value)}>
              {SEVERITIES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
          )}
        </Field>
      </div>
      <Field label="What happened?" hint="Error messages and logs help a lot. Ctrl/⌘+Enter sends.">
        {(p) => (
          <textarea className="input" {...p} rows={6} value={body}
            placeholder="What did you try, what did you expect, what happened instead?"
            onChange={(e) => setBody(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); submit(); }
            }} />
        )}
      </Field>
      <div className="row">
        <button className="btn primary" disabled={busy} onClick={submit}>
          {busy ? <Spinner size={13} /> : <Send size={13} />} Send ticket
        </button>
        <button className="btn ghost" disabled={busy} onClick={cancel}>Cancel</button>
      </div>
    </section>
  );
}

function Thread({ ticket, onBack, onChanged, onDirty }: {
  ticket: SupportTicket; onBack: () => void; onChanged: () => void;
  onDirty: (dirty: boolean) => void;
}) {
  const toast = useToast();
  const [reply, setReply] = useState('');
  const [busy, setBusy] = useState(false);
  const [resolving, setResolving] = useState(false);
  const replyId = useId();
  const listRef = useRef<HTMLDivElement>(null);

  // The parent's dirty flag must never outlive the thread it belongs to.
  const onDirtyRef = useRef(onDirty);
  onDirtyRef.current = onDirty;
  useEffect(() => () => onDirtyRef.current(false), []);

  // Newest message in view on mount and whenever one lands, so the reply row
  // below never has to chase a growing list.
  const lastMsgId = ticket.messages[ticket.messages.length - 1]?.id;
  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lastMsgId]);

  const send = async () => {
    if (!reply.trim() || busy) return;
    setBusy(true);
    try {
      await api.replyTicket(ticket.id, reply.trim());
      setReply('');
      onDirty(false);
      onChanged();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const resolve = async () => {
    setResolving(true);
    try {
      await api.resolveTicket(ticket.id);
      toast.ok('Ticket resolved. Reply any time to reopen it.');
      onChanged();
    } catch (e) { toast.err(cleanError(e)); }
    setResolving(false);
  };

  return (
    <section>
      <div className="row spread wrap" style={{ marginBottom: 4 }}>
        <div className="row" style={{ minWidth: 0 }}>
          <button className="btn sm ghost" onClick={onBack} aria-label="Back to all tickets"><ArrowLeft size={14} /></button>
          <strong style={{ fontSize: 14.5 }}>{ticket.subject}</strong>
          <span className="tag">{ticket.category}</span>
          <SeverityTag severity={ticket.severity} />
        </div>
        <div className="row">
          <Pill status={ticketLed(ticket.status)}>{statusLabel(ticket.status)}</Pill>
          {ticket.status !== 'resolved' && (
            <button className="btn sm" disabled={resolving} onClick={resolve}>
              {resolving ? <Spinner size={12} /> : <CheckCircle2 size={12} />} Mark resolved
            </button>
          )}
        </div>
      </div>

      {/* The thread speaks the product's one bubble grammar: support (agent)
          on the left as the bot surface, the operator on the right in the
          accent-tinted fill. */}
      <div ref={listRef} className="stack"
        style={{ gap: 10, margin: '14px 0', maxHeight: '55vh', overflowY: 'auto', paddingRight: 2 }}>
        {ticket.messages.map((m) => (
          <div key={m.id} className={`chat-msg ${m.author === 'agent' ? 'bot' : 'user'}`}>
            <div className="row spread" style={{ marginBottom: 6, gap: 14 }}>
              <strong className="row" style={{ gap: 6, fontSize: 12.5 }}>
                <MessageSquareText size={12} style={{ color: m.author === 'agent' ? 'var(--accent)' : 'var(--faint)' }} aria-hidden />
                {m.author === 'agent' ? `${m.name} (RackOps)` : m.name || 'You'}
              </strong>
              <span className="faint">{timeAgo(m.at)}</span>
            </div>
            <div style={{ whiteSpace: 'pre-wrap', lineHeight: 1.6, fontSize: 13 }}>{m.body}</div>
          </div>
        ))}
      </div>

      <div className="row" style={{ alignItems: 'flex-end' }}>
        <label className="sr-only" htmlFor={replyId}>Reply to this ticket</label>
        <textarea className="input" id={replyId} rows={3} style={{ flex: 1 }} value={reply}
          placeholder={ticket.status === 'resolved'
            ? 'Reply to reopen this ticket… (Ctrl/⌘+Enter to send)'
            : 'Write a reply… (Ctrl/⌘+Enter to send)'}
          onChange={(e) => {
            const v = e.target.value;
            setReply(v);
            onDirty(!!v.trim());
          }}
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); send(); }
          }} />
        <button className="btn primary" disabled={busy || !reply.trim()} onClick={send}>
          {busy ? <Spinner size={13} /> : <Send size={13} />} Send
        </button>
      </div>
    </section>
  );
}
