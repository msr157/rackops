import { useEffect, useState } from 'react';
import { api, type Lead } from '../api';

export function LeadsPage() {
  const [leads, setLeads] = useState<Lead[]>([]);
  const [offset, setOffset] = useState(0);
  const [refresh, setRefresh] = useState(0);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError('');
    api.leads(offset).then(data => { if (alive) setLeads(data); })
      .catch(() => { if (alive) setError('Could not load leads. Please try again.'); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [offset, refresh]);
  async function update(lead: Lead, status: string) {
    setSaving(true); setError('');
    try {
      await api.leadStatus(lead, status);
      setLeads(current => current.map(item => item.id === lead.id && item.installId === lead.installId ? { ...item, status } : item));
    } catch { setError('Could not save status. Please try again.'); }
    finally { setSaving(false); }
  }
  return <div className="page">
    <div className="page-title">Enterprise leads</div>
    <p className="page-sub">Enterprise enquiries submitted through RackOps. Contact details are unverified. Changing status does not send an email.</p>
    {error && <p role="alert">{error}</p>}
    {loading ? <p>Loading leads…</p> : !error && leads.length === 0 ? <p>No leads on this page.</p> : <div className="card">
      {leads.map(lead => <article key={`${lead.installId}:${lead.id}`} style={{ padding: 16, borderBottom: '1px solid var(--border)' }}>
        <strong>{lead.name}</strong> · {lead.email}
        <p>{lead.industry} · Received {new Date(lead.receivedAt).toLocaleString()}</p>
        <p style={{ whiteSpace: 'pre-wrap' }}>{lead.notes}</p>
        <label>Status <select aria-label={`Status for ${lead.email}`} value={lead.status} disabled={saving} onChange={event => void update(lead, event.target.value)}>
          {['new', 'contacted', 'qualified', 'closed'].map(status => <option key={status} value={status}>{status}</option>)}
        </select></label>
      </article>)}
    </div>}
    <div className="row" style={{ marginTop: 16, gap: 12 }}>
      <button className="btn" disabled={loading || saving} onClick={() => setRefresh(value => value + 1)}>Refresh</button>
      <button className="btn" disabled={loading || saving || offset === 0} onClick={() => setOffset(offset - 200)}>Previous</button>
      <button className="btn" disabled={loading || saving || leads.length < 200} onClick={() => setOffset(offset + 200)}>Next</button>
    </div>
  </div>;
}
