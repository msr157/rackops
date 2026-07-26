import { useCallback, useEffect, useId, useState } from 'react';
import { ArrowLeft, CheckCircle2, LifeBuoy, MessageSquareText, Plus, Send } from 'lucide-react';
import { api, subscribe } from '../api';
import type { SupportTicket } from '../api';
import { useToast, cleanError } from '../components/toast';
import { Field, InfoTip, Led, Spinner, timeAgo } from '../components/ui';

const CATEGORIES = [
  ['general', 'General question'],
  ['deploys', 'Deploys & apps'],
  ['networking', 'Networking & domains'],
  ['updates', 'Updates & install'],
  ['bug', 'Bug report'],
  ['billing', 'Billing'],
] as const;

const SEVERITIES = [
  ['low', 'Low — no rush'],
  ['normal', 'Normal'],
  ['high', 'High — something important is broken'],
  ['urgent', 'Urgent — production is down'],
] as const;

// Support: talk to RackOps support without leaving the dashboard. Tickets are
// forwarded to the support desk; replies land back in the thread (~1 min).
export function SupportPage() {
  const [tickets, setTickets] = useState<SupportTicket[]>([]);
  const [open, setOpen] = useState('');
  const [composing, setComposing] = useState(false);

  const load = useCallback(() => {
    api.tickets().then((t) => setTickets(t ?? [])).catch(() => {});
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

  return (
    <div className="page" style={{ maxWidth: 780 }}>
      <div className="row spread" style={{ marginBottom: 4 }}>
        <div className="page-title">Support</div>
        <button className="btn primary" onClick={() => { setComposing(true); setOpen(''); }}>
          <Plus size={14} /> New ticket
        </button>
      </div>
      <div className="page-sub">
        Ask RackOps support for help — replies show up right here
        <InfoTip wide>
          Tickets are sent to the RackOps support team along with an anonymous usage summary
          (version, machine/app counts, which features you use — never names, domains, or
          credentials). It helps support see your setup at a glance and helps us improve RackOps.
        </InfoTip>
      </div>

      {composing && <Compose onDone={(id) => { setComposing(false); load(); setOpen(id); }} onCancel={() => setComposing(false)} />}

      {current ? (
        <Thread ticket={current} onBack={() => setOpen('')} onChanged={load} />
      ) : !composing && (
        <>
          {tickets.length === 0 && (
            <div className="empty" style={{ padding: '56px 20px' }}>
              <LifeBuoy size={26} style={{ marginBottom: 10 }} />
              <div className="empty-title">No tickets yet</div>
              <div>Stuck on something? Open a ticket — a human answers.</div>
            </div>
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
    : status === 'pending' ? 'support replied — your turn' : 'resolved';
}

function TicketList({ title, items, onOpen }: {
  title: string; items: SupportTicket[]; onOpen: (id: string) => void;
}) {
  return (
    <>
      <div className="section-title">{title}</div>
      <div className="stack" style={{ gap: 8, marginBottom: 18 }}>
        {/* Opening a ticket is a state change, not navigation — so it's a
            button, which Tab reaches and Enter activates. */}
        {items.map((t) => (
          <button key={t.id} type="button" className="card clickable row spread ticket-row"
            style={{ padding: '12px 16px' }} onClick={() => onOpen(t.id)}>
            <div className="row" style={{ minWidth: 0 }}>
              <Led status={ticketLed(t.status)} />
              <strong style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{t.subject}</strong>
              <span className="tag">{t.category}</span>
              {t.severity === 'high' || t.severity === 'urgent' ? <span className="badge red">{t.severity}</span> : null}
            </div>
            <span className="faint" style={{ whiteSpace: 'nowrap' }}>
              {statusLabel(t.status)} · {timeAgo(t.updatedAt)}
            </span>
          </button>
        ))}
      </div>
    </>
  );
}

function Compose({ onDone, onCancel }: { onDone: (id: string) => void; onCancel: () => void }) {
  const toast = useToast();
  const [subject, setSubject] = useState('');
  const [category, setCategory] = useState('general');
  const [severity, setSeverity] = useState('normal');
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!subject.trim() || !body.trim()) { toast.err('Give the ticket a subject and describe the problem.'); return; }
    setBusy(true);
    try {
      const t = await api.createTicket({ subject: subject.trim(), category, severity, body: body.trim() });
      toast.ok('Ticket sent — support usually answers within a business day.');
      onDone(t.id);
    } catch (e) { toast.err(cleanError(e)); setBusy(false); }
  };

  return (
    <div className="card" style={{ marginBottom: 18 }}>
      <div className="eyebrow" style={{ marginBottom: 12 }}><LifeBuoy size={12} /> New support ticket</div>
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
        <Field label="Severity">
          {(p) => (
            <select className="input" {...p} value={severity} onChange={(e) => setSeverity(e.target.value)}>
              {SEVERITIES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </select>
          )}
        </Field>
      </div>
      <Field label="What happened?">
        {(p) => (
          <textarea className="input" {...p} rows={6} value={body}
            placeholder="What did you try, what did you expect, what happened instead? Error messages and logs help a lot."
            onChange={(e) => setBody(e.target.value)} />
        )}
      </Field>
      <div className="row">
        <button className="btn primary" disabled={busy} onClick={submit}>
          {busy ? <Spinner size={13} /> : <Send size={13} />} Send ticket
        </button>
        <button className="btn ghost" onClick={onCancel}>Cancel</button>
      </div>
    </div>
  );
}

function Thread({ ticket, onBack, onChanged }: {
  ticket: SupportTicket; onBack: () => void; onChanged: () => void;
}) {
  const toast = useToast();
  const [reply, setReply] = useState('');
  const [busy, setBusy] = useState(false);
  const replyId = useId();

  const send = async () => {
    if (!reply.trim()) return;
    setBusy(true);
    try {
      await api.replyTicket(ticket.id, reply.trim());
      setReply('');
      onChanged();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const resolve = async () => {
    try {
      await api.resolveTicket(ticket.id);
      toast.ok('Ticket resolved. Reply any time to reopen it.');
      onChanged();
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="card">
      <div className="row spread wrap" style={{ marginBottom: 4 }}>
        <div className="row" style={{ minWidth: 0 }}>
          <button className="btn sm ghost" onClick={onBack} aria-label="Back to all tickets"><ArrowLeft size={14} /></button>
          <strong style={{ fontSize: 14.5 }}>{ticket.subject}</strong>
          <span className="tag">{ticket.category}</span>
          <span className="tag">{ticket.severity}</span>
        </div>
        <div className="row">
          <span className="row faint" style={{ gap: 6 }}>
            <Led status={ticketLed(ticket.status)} /> {statusLabel(ticket.status)}
          </span>
          {ticket.status !== 'resolved' && (
            <button className="btn sm" onClick={resolve}><CheckCircle2 size={12} /> Mark resolved</button>
          )}
        </div>
      </div>

      <div className="stack" style={{ gap: 10, margin: '14px 0' }}>
        {ticket.messages.map((m) => (
          <div key={m.id} className="card inset" style={{
            borderColor: m.author === 'agent' ? 'var(--amber)' : 'var(--border)',
            marginLeft: m.author === 'agent' ? 0 : 24,
            marginRight: m.author === 'agent' ? 24 : 0,
          }}>
            <div className="row spread" style={{ marginBottom: 6 }}>
              <strong className="row" style={{ gap: 6, fontSize: 12.5 }}>
                <MessageSquareText size={12} style={{ color: m.author === 'agent' ? 'var(--amber)' : 'var(--faint)' }} />
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
          placeholder={ticket.status === 'resolved' ? 'Reply to reopen this ticket…' : 'Write a reply…'}
          onChange={(e) => setReply(e.target.value)} />
        <button className="btn primary" disabled={busy || !reply.trim()} onClick={send} aria-label="Send reply">
          {busy ? <Spinner size={13} /> : <Send size={13} />}
        </button>
      </div>
    </div>
  );
}
