import { useState } from 'react';
import { api, type MigrationReview } from '../api';
import { cleanError } from './toast';

export function MigrationPreflight({ serviceId }: { serviceId: string }) {
  const [provider, setProvider] = useState('aws');
  const [region, setRegion] = useState('');
  const [review, setReview] = useState<MigrationReview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function check() {
    setBusy(true); setError(''); setReview(null);
    try { setReview(await api.migrationPreflight(serviceId, provider, region.trim())); }
    catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <details className="card" style={{ padding: 16, marginBottom: 16 }}>
    <summary>Cloud migration readiness</summary>
    <p>Check what this app needs before moving to a cloud machine. This initial review does not create resources or move data.</p>
    <label>Cloud <select value={provider} disabled={busy} onChange={event => { setProvider(event.target.value); setRegion(''); setReview(null); }}>
      <option value="aws">AWS</option><option value="azure">Azure</option><option value="gcp">Google Cloud</option>
    </select></label>{' '}
    <label>Region <input value={region} disabled={busy} maxLength={63} placeholder="Enter your target region" onChange={event => { setRegion(event.target.value); setReview(null); }} /></label>{' '}
    <button className="btn" disabled={busy || !region.trim()} onClick={() => void check()}>{busy ? 'Checking…' : 'Check readiness'}</button>
    {error && <p role="alert">{error}</p>}
    {review && <div aria-live="polite">
      <p>Source architecture: {review.architecture || 'Unknown'}. Cost, data size and downtime: not yet estimated.</p>
      <strong>Migration is not ready to start</strong>
      <ul>{review.blockers.map(item => <li key={item.code}>{item.message}</li>)}</ul>
    </div>}
  </details>;
}
