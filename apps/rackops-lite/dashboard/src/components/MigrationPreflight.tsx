import { useState } from 'react';
import { Cloud } from 'lucide-react';
import { api, type MigrationReview } from '../api';
import { cleanError } from './toast';
import { MigrationWizard } from './MigrationWizard';

export function MigrationPreflight({ serviceId }: { serviceId: string }) {
  const [provider, setProvider] = useState('aws');
  const [region, setRegion] = useState('');
  const [review, setReview] = useState<MigrationReview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [showWizard, setShowWizard] = useState(false);

  async function check() {
    setBusy(true); setError(''); setReview(null);
    try { setReview(await api.migrationPreflight(serviceId, provider, region.trim())); }
    catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }

  return (
    <>
      <details className="card" style={{ padding: 16, marginBottom: 16 }}>
        <summary>Cloud migration readiness</summary>
        <p>Check what this app needs before moving to a cloud machine. This initial review does not create resources or move data.</p>
        <div className="row" style={{ gap: 8, alignItems: 'center', marginBottom: 12 }}>
          <label>Cloud <select value={provider} disabled={busy} onChange={event => { setProvider(event.target.value); setRegion(''); setReview(null); }}>
            <option value="aws">AWS</option><option value="azure">Azure</option><option value="gcp">Google Cloud</option>
          </select></label>{' '}
          <label>Region <input value={region} disabled={busy} maxLength={63} placeholder="Enter your target region" onChange={event => { setRegion(event.target.value); setReview(null); }} /></label>{' '}
          <button className="btn" disabled={busy || !region.trim()} onClick={() => void check()}>{busy ? 'Checking…' : 'Check readiness'}</button>
          <button className="btn primary" onClick={() => setShowWizard(true)}>
            <Cloud size={14} /> Start Guided Migration
          </button>
        </div>
        {error && <p role="alert">{error}</p>}
        {review && <div aria-live="polite">
          <p>Source architecture: {review.architecture || 'Unknown'}.</p>
          {review.blockers.length > 0 ? (
            <>
              <strong>Migration preflight identified blockers:</strong>
              <ul>{review.blockers.map(item => <li key={item.code}>{item.message}</li>)}</ul>
            </>
          ) : (
            <p className="ok">Preflight passed. Ready to configure cloud target and pricing review.</p>
          )}
        </div>}
      </details>
      {showWizard && (
        <MigrationWizard
          preselectedServiceId={serviceId}
          onClose={() => setShowWizard(false)}
        />
      )}
    </>
  );
}
