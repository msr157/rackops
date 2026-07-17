import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { Inbox, ArrowLeft, Send, StickyNote, CheckCircle2, RotateCcw, Server } from 'lucide-react';
import { api } from '../api';
import type { Ticket, TicketStatus } from '../api';
import { EmptyState, SeverityBadge, StatusBadge, timeAgo, fmtDate } from '../components/ui';

const STATUS_ORDER: TicketStatus[] = ['open', 'pending', 'resolved'];
const STATUS_LABEL: Record<TicketStatus, string> = {
  open: 'Open — needs an agent',
  pending: 'Pending — waiting on customer',
  resolved: 'Resolved',
};

export function TicketsPage() {
  const [tickets, setTickets] = useState<Ticket[] | null>(null);
  const navigate = useNavigate();

  // Poll the inbox every 10s.
  useEffect(() => {
    let alive = true;
    const load = () => api.tickets().then((r) => { if (alive) setTickets(r); }).catch(() => {});
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, []);

  if (!tickets) return null;

  return (
    <div className="page">
      <div className="page-title">Tickets</div>
      <div className="page-sub">Support inbox — everything customers forwarded from their instances.</div>

      {tickets.length === 0 ? (
        <EmptyState icon={<Inbox size={22} />} title="Inbox zero">
          Tickets forwarded by customer instances land here.
        </EmptyState>
      ) : (
        STATUS_ORDER.map((status) => {
          const group = tickets.filter((t) => t.status === status);
          if (group.length === 0) return null;
          return (
            <div key={status}>
              <div className="section-title">
                <StatusBadge status={status} />
                <span className="faint">· {group.length} · {STATUS_LABEL[status]}</span>
              </div>
              <div className="card" style={{ padding: 0, marginBottom: 8 }}>
                <table className="table">
                  <tbody>
                    {group.map((t) => (
                      <tr key={`${t.installId}/${t.id}`} onClick={() => navigate(`/tickets/${t.installId}/${t.id}`)}>
                        <td style={{ fontWeight: 500, width: '38%' }}>{t.subject}</td>
                        <td className="dim"><span className="row" style={{ gap: 6 }}><Server size={12} /> {t.installName || t.installId.slice(0, 8)}</span></td>
                        <td><SeverityBadge severity={t.severity} /></td>
                        <td><span className="tag">{t.category}</span></td>
                        <td className="dim">{t.email}</td>
                        <td className="dim mono" style={{ textAlign: 'right' }}>{timeAgo(t.updatedAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          );
        })
      )}
    </div>
  );
}

export function TicketDetailPage() {
  const { id, installId } = useParams<{ id: string; installId: string }>();
  const [ticket, setTicket] = useState<Ticket | null>(null);
  const [error, setError] = useState('');
  const [body, setBody] = useState('');
  const [asNote, setAsNote] = useState(false);
  const [busy, setBusy] = useState(false);
  const threadEnd = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!id || !installId) return;
    let alive = true;
    const load = () => api.ticket(id, installId)
      .then((t) => { if (alive) { setTicket(t); setError(''); } })
      .catch((e) => { if (alive) setError(String(e)); });
    load();
    const t = setInterval(load, 10_000);
    return () => { alive = false; clearInterval(t); };
  }, [id, installId]);

  const send = async () => {
    if (!id || !installId || !body.trim() || busy) return;
    setBusy(true);
    try {
      const updated = asNote
        ? await api.note(id, installId, body)
        : await api.reply(id, installId, body);
      setTicket(updated);
      setBody('');
      setTimeout(() => threadEnd.current?.scrollIntoView({ behavior: 'smooth' }), 50);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const setStatus = async (status: TicketStatus) => {
    if (!id || !installId || busy) return;
    setBusy(true);
    try {
      setTicket(await api.setStatus(id, installId, status));
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  if (error && !ticket) return <EmptyState title="Ticket not found">{error}</EmptyState>;
  if (!ticket) return null;

  return (
    <div className="page">
      <Link to="/tickets" className="row dim" style={{ gap: 6, marginBottom: 12 }}>
        <ArrowLeft size={14} /> Inbox
      </Link>

      <div className="row spread wrap" style={{ marginBottom: 4 }}>
        <div className="page-title" style={{ marginBottom: 0 }}>{ticket.subject}</div>
        <div className="row">
          {ticket.status !== 'resolved' ? (
            <button className="btn sm" disabled={busy} onClick={() => setStatus('resolved')}>
              <CheckCircle2 size={13} /> Resolve
            </button>
          ) : (
            <button className="btn sm" disabled={busy} onClick={() => setStatus('open')}>
              <RotateCcw size={13} /> Reopen
            </button>
          )}
        </div>
      </div>

      <div className="row wrap page-sub" style={{ gap: 8 }}>
        <StatusBadge status={ticket.status} />
        <SeverityBadge severity={ticket.severity} />
        <span className="tag">{ticket.category}</span>
        <span className="dim">{ticket.email}</span>
        <span className="dim">·</span>
        <Link to={`/installs/${ticket.installId}`} className="row dim" style={{ gap: 5 }}>
          <Server size={12} /> {ticket.installName || ticket.installId.slice(0, 8)}
        </Link>
        <span className="faint mono">opened {fmtDate(ticket.createdAt)}</span>
      </div>

      <div className="card" style={{ marginBottom: 14 }}>
        <div className="thread">
          {(ticket.messages ?? []).map((m) => (
            <div key={m.id} className={`bubble ${m.author}`}>
              <div className="bubble-meta">
                <span style={{ color: m.author === 'note' ? 'var(--gpu)' : m.author === 'agent' ? 'var(--amber-hi)' : 'var(--dim)' }}>
                  {m.author === 'note' ? 'INTERNAL NOTE' : m.name || m.author}
                </span>
                <span>{fmtDate(m.at)}</span>
              </div>
              <div className="bubble-body">{m.body}</div>
            </div>
          ))}
          {(ticket.messages ?? []).length === 0 && <div className="faint">No messages in this thread.</div>}
          <div ref={threadEnd} />
        </div>
      </div>

      <div className="card inset">
        <textarea
          className="input"
          rows={4}
          placeholder={asNote ? 'Internal note — never sent to the customer…' : 'Reply as RackOps Support — the instance picks this up on its next poll…'}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          style={asNote ? { borderStyle: 'dashed' } : undefined}
        />
        <div className="row spread" style={{ marginTop: 10 }}>
          <label className="check">
            <input type="checkbox" checked={asNote} onChange={(e) => setAsNote(e.target.checked)} />
            <StickyNote size={13} /> Internal note (not visible to customer)
          </label>
          <button className={`btn ${asNote ? '' : 'primary'}`} disabled={busy || !body.trim()} onClick={send}>
            {asNote ? <><StickyNote size={14} /> Add note</> : <><Send size={14} /> Send reply</>}
          </button>
        </div>
      </div>
    </div>
  );
}
