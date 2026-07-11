import { useCallback, useEffect, useState } from 'react';
import { LifeBuoy, Plus, Send, ChevronDown, ChevronRight, CheckCircle2 } from 'lucide-react';
import { api } from '../api';
import type { SupportTicket } from '../api';
import { useToast, cleanError } from '../components/toast';
import { InfoTip, Led, Spinner, timeAgo } from '../components/ui';

const CATEGORIES = [
  ['bug', 'Something is broken'],
  ['question', 'How do I…?'],
  ['feature-request', 'Feature request'],
  ['billing', 'Billing / licensing'],
  ['other', 'Other'],
] as const;

const SEVERITIES = [
  ['low', 'Low — whenever you get to it'],
  ['normal', 'Normal'],
  ['high', 'High — blocking my work'],
  ['urgent', 'Urgent — production is down'],
] as const;

function ticketLed(status: SupportTicket['status']): string {
  switch (status) {
    case 'open': return 'pending';
    case 'pending': return 'building';
    case 'resolved': return 'running';
    default: return 'stopped';
  }
}

// SupportPage: open tickets, track replies, close the loop — tickets stay on
// this server AND flow to the vendor's CRM (when configured) so real support
// can happen.
export function SupportPage() {
  const toast = useToast();
  const [tickets, setTickets] = useState<SupportTicket[]>([]);
  const [composing, setComposing] = useState(false);
  const [subject, setSubject] = useState('');
  const [category, setCategory] = useState('bug');
  const [severity, setSeverity] = useState('normal');
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [openId, setOpenId] = useState('');
  const [reply, setReply] = useState('');

  const load = useCallback(() => {
    api.tickets().then((t) => setTickets(t ?? [])).catch(() => {});
  }, []);
  useEffect(load, [load]);

  const create = async () => {
    if (!subject.trim() || !body.trim()) { toast.err('Give the ticket a subject and describe the issue.'); return; }
    setBusy(true);
    try {
      const t = await api.createTicket({ subject: subject.trim(), category, severity, body: body.trim() });
      toast.ok('Ticket opened — we’ll get back to you.');
      setComposing(false); setSubject(''); setBody(''); setCategory('bug'); setSeverity('normal');
      setOpenId(t.id);
      load();
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const sendReply = async (t: SupportTicket, status?: string) => {
    if (!reply.trim() && !status) return;
    setBusy(true);
    try {
      await api.replyTicket(t.id, reply.trim(), status);
      setReply('');
      load();
      if (status === 'resolved') toast.ok('Marked resolved — thanks for closing the loop!');
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const active = tickets.filter((t) => t.status === 'open' || t.status === 'pending');
  const done = tickets.filter((t) => t.status === 'resolved' || t.status === 'closed');

  return (
    <div className="page" style={{ maxWidth: 760 }}>
      <div className="row spread wrap" style={{ marginBottom: 4 }}>
        <div>
          <div className="page-title">Support</div>
          <div className="page-sub">Stuck? Found a bug? Open a ticket — it goes straight to the RackOps team</div>
        </div>
        {!composing && (
          <button className="btn primary" onClick={() => setComposing(true)}><Plus size={13} /> New ticket</button>
        )}
      </div>

      {composing && (
        <div className="card" style={{ borderColor: 'var(--amber)' }}>
          <div className="eyebrow" style={{ marginBottom: 12 }}><LifeBuoy size={12} /> New support ticket</div>
          <div className="field">
            <label>Subject</label>
            <input className="input" placeholder="One line: what's wrong or what do you need?"
              value={subject} onChange={(e) => setSubject(e.target.value)} autoFocus />
          </div>
          <div className="grid-2">
            <div className="field">
              <label>Category</label>
              <select className="input" value={category} onChange={(e) => setCategory(e.target.value)}>
                {CATEGORIES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
              </select>
            </div>
            <div className="field">
              <label>Severity
                <InfoTip text="Urgent means production traffic is affected right now. High/urgent tickets surface first on our side." />
              </label>
              <select className="input" value={severity} onChange={(e) => setSeverity(e.target.value)}>
                {SEVERITIES.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
              </select>
            </div>
          </div>
          <div className="field">
            <label>What happened?
              <InfoTip text="Include what you expected, what you got, and roughly when. Your ticket travels with an anonymous system snapshot (version, machine/app counts, features in use) so we can help faster — never your data or logs." wide />
            </label>
            <textarea className="input" rows={6} style={{ resize: 'vertical', fontFamily: 'inherit' }}
              placeholder={'Steps to reproduce, error messages, what you already tried…'}
              value={body} onChange={(e) => setBody(e.target.value)} />
          </div>
          <div className="row">
            <button className="btn primary" disabled={busy} onClick={create}>
              {busy ? <Spinner size={13} /> : <Send size={13} />} Open ticket
            </button>
            <button className="btn ghost" disabled={busy} onClick={() => setComposing(false)}>Cancel</button>
          </div>
        </div>
      )}

      {[['Active', active], ['Resolved', done]].map(([label, list]) => (
        (list as SupportTicket[]).length > 0 && (
          <div key={label as string}>
            <div className="section-title">{label as string}</div>
            <div className="stack" style={{ gap: 8 }}>
              {(list as SupportTicket[]).map((t) => {
                const open = openId === t.id;
                const msgs = t.messages ?? [];
                return (
                  <div className="card" key={t.id} style={{ padding: open ? undefined : '12px 16px' }}>
                    <div className="row spread" style={{ cursor: 'pointer' }} onClick={() => setOpenId(open ? '' : t.id)}>
                      <div className="row" style={{ minWidth: 0 }}>
                        {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                        <Led status={ticketLed(t.status)} />
                        <strong style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{t.subject}</strong>
                        <span className="tag">{t.category}</span>
                        {(t.severity === 'high' || t.severity === 'urgent') && <span className="badge red">{t.severity}</span>}
                      </div>
                      <span className="faint" style={{ flexShrink: 0 }}>
                        {t.status} · {timeAgo(t.updatedAt)}
                      </span>
                    </div>

                    {open && (
                      <div style={{ marginTop: 12 }}>
                        {msgs.map((m) => (
                          <div key={m.id} className="card inset" style={{ marginBottom: 8 }}>
                            <div className="row spread" style={{ marginBottom: 6 }}>
                              <strong style={{ fontSize: 12.5 }}>{m.author}</strong>
                              <span className="faint">{new Date(m.createdAt).toLocaleString()}</span>
                            </div>
                            <div style={{ whiteSpace: 'pre-wrap', fontSize: 13, lineHeight: 1.6 }}>{m.body}</div>
                          </div>
                        ))}
                        {(t.status === 'open' || t.status === 'pending') && (
                          <>
                            <textarea className="input" rows={3} style={{ resize: 'vertical', fontFamily: 'inherit', marginBottom: 8 }}
                              placeholder="Add details or reply…" value={reply} onChange={(e) => setReply(e.target.value)} />
                            <div className="row">
                              <button className="btn sm primary" disabled={busy || !reply.trim()} onClick={() => sendReply(t)}>
                                <Send size={12} /> Reply
                              </button>
                              <button className="btn sm" disabled={busy} onClick={() => sendReply(t, 'resolved')}>
                                <CheckCircle2 size={12} /> Mark resolved
                              </button>
                            </div>
                          </>
                        )}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        )
      ))}

      {tickets.length === 0 && !composing && (
        <div className="empty">
          <LifeBuoy size={22} style={{ marginBottom: 8 }} />
          <div className="empty-title">No tickets yet</div>
          <div style={{ marginBottom: 14 }}>Hit a wall, found a bug, or wish RackOps did something it doesn&rsquo;t? Tell us.</div>
          <button className="btn primary" onClick={() => setComposing(true)}><Plus size={13} /> Open your first ticket</button>
        </div>
      )}
    </div>
  );
}
