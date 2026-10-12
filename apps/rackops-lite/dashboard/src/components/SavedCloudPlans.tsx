import { useEffect, useState } from 'react';
import { api, type CloudReviewPlan } from '../api';
import { cleanError } from './toast';

export function SavedCloudPlans() {
  const [plans, setPlans] = useState<CloudReviewPlan[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let alive = true;
    api.cloudReviewPlans().then(value => { if (alive) setPlans(value); }).catch(err => { if (alive) setError(cleanError(err)); });
    return () => { alive = false; };
  }, []);
  async function refresh() {
    setBusy(true); setError('');
    try { setPlans(await api.cloudReviewPlans()); } catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  async function estimate(id: string, disk = false) {
    setBusy(true); setError('');
    try { if (disk) await api.quoteCloudDisk(id); else await api.quoteCloudCompute(id); setPlans(await api.cloudReviewPlans()); }
    catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <details style={{ marginTop: 20 }}>
    <summary>Saved cloud resource reviews ({plans.length})</summary>
    <p>Review snapshots survive server restarts. Total costs and downtime remain unknown. AWS, Azure and supported GCP E2/N2/N2D standard reviews can request a base VM compute subtotal; approval and provisioning remain unavailable.</p>
    <button className="btn" disabled={busy} onClick={() => void refresh()}>{busy ? 'Refreshing…' : 'Refresh saved reviews'}</button>
    {error && <p role="alert">{error}</p>}
    {plans.map(plan => <details key={plan.id} style={{ marginTop: 12 }}>
      <summary>{plan.provider} · {plan.region} · {plan.status === 'connection_unavailable' ? 'Connection unavailable' : new Date(plan.expiresAt).getTime() < Date.now() ? 'Review expired' : 'Launch checks incomplete'}</summary>
      <p>Account {plan.accountId} · {plan.architecture} · Review {plan.id}</p>
      <p>Created {new Date(plan.createdAt).toLocaleString()} · Expires {new Date(plan.expiresAt).toLocaleString()}</p>
      <p>Recurring cost: unknown · Transfer cost: unknown · Downtime: unknown</p>
      {(plan.provider === 'azure' || plan.provider === 'aws' || plan.provider === 'gcp') && <button className="btn" disabled={busy || plan.status === 'connection_unavailable' || new Date(plan.expiresAt).getTime() < Date.now()} onClick={() => void estimate(plan.id)}>Estimate base VM compute</button>}
      {plan.computeQuote && <div>
        <p><strong>Base VM compute only:</strong> {plan.computeQuote.currency} {plan.computeQuote.monthly.toFixed(2)} / month at {plan.computeQuote.hoursPerMonth} hours ({plan.computeQuote.hourly} / hour).</p>
        <p>{new Date(plan.computeQuote.expiresAt).getTime() < Date.now() ? 'Estimate expired. ' : ''}Retrieved {new Date(plan.computeQuote.retrievedAt).toLocaleString()}. <a href={plan.computeQuote.source} target="_blank" rel="noreferrer">Provider pricing source</a></p>
        <p>Excluded from this subtotal:</p>
        <ul>{plan.computeQuote.exclusions.map(item => <li key={item}>{item}</li>)}</ul>
        <p>The total remains unknown. This estimate does not enable approval.</p>
      </div>}
      {(plan.provider === 'aws' || plan.provider === 'azure' || plan.provider === 'gcp') && <button className="btn" disabled={busy || plan.status === 'connection_unavailable' || new Date(plan.expiresAt).getTime() < Date.now()} onClick={() => void estimate(plan.id, true)}>Estimate root disk</button>}
      {plan.diskQuote && <div>
        <p><strong>Root disk only:</strong> {plan.diskQuote.currency} {plan.diskQuote.monthly.toFixed(2)} / month · {plan.diskQuote.diskType} · {plan.diskQuote.diskGb} GiB requested.</p>
        <p>{new Date(plan.diskQuote.expiresAt).getTime() < Date.now() ? 'Estimate expired. ' : ''}Retrieved {new Date(plan.diskQuote.retrievedAt).toLocaleString()}. <a href={plan.diskQuote.source} target="_blank" rel="noreferrer">Disk pricing source</a></p>
        <ul>{plan.diskQuote.exclusions.map(item => <li key={item}>{item}</li>)}</ul>
        <p>Compute and transfer are separate. The total remains unknown; approval is unavailable.</p>
      </div>}
      <ul>{plan.blockers.map(blocker => <li key={blocker}>{blocker}</li>)}</ul>
      <ul>{plan.inspection.blockers.map(blocker => <li key={blocker.code}>{blocker.message}</li>)}</ul>
      <p>Selected resources</p>
      <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{JSON.stringify(plan.spec, null, 2)}</pre>
    </details>)}
  </details>;
}
