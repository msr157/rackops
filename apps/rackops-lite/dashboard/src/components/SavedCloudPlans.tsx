import { useEffect, useState } from 'react';
import { api, type CloudReviewPlan } from '../api';
import { cleanError } from './toast';
import { MigrationWizard } from './MigrationWizard';

export function SavedCloudPlans() {
  const [plans, setPlans] = useState<CloudReviewPlan[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [showWizard, setShowWizard] = useState(false);

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

  async function estimate(id: string, type: 'compute' | 'disk' | 'transfer') {
    setBusy(true); setError('');
    try {
      if (type === 'disk') await api.quoteCloudDisk(id);
      else if (type === 'transfer') await api.quoteCloudTransfer(id, 50);
      else await api.quoteCloudCompute(id);
      setPlans(await api.cloudReviewPlans());
    }
    catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }

  return (
    <>
      <details style={{ marginTop: 20 }}>
        <summary>Saved cloud resource reviews ({plans.length})</summary>
        <p>Review snapshots survive server restarts. Verified recurring compute, root disk, and outbound transfer estimates can be calculated and aggregated.</p>
        <div className="row" style={{ gap: 8, marginBottom: 12 }}>
          <button className="btn" disabled={busy} onClick={() => void refresh()}>{busy ? 'Refreshing…' : 'Refresh saved reviews'}</button>
          <button className="btn primary" onClick={() => setShowWizard(true)}>Move Apps to Cloud (Wizard)</button>
        </div>
        {error && <p role="alert">{error}</p>}
        {plans.map(plan => <details key={plan.id} style={{ marginTop: 12 }}>
          <summary>
            {plan.provider} · {plan.region} · {plan.status === 'connection_unavailable' ? 'Connection unavailable' : new Date(plan.expiresAt).getTime() < Date.now() ? 'Review expired' : plan.canApprove ? 'Ready for approval' : 'Launch checks incomplete'}
          </summary>
          <p>Account {plan.accountId} · {plan.architecture} · Review {plan.id}</p>
          <p>Created {new Date(plan.createdAt).toLocaleString()} · Expires {new Date(plan.expiresAt).toLocaleString()}</p>
          {plan.monthlyCost !== null ? (
            <div style={{ margin: '8px 0', padding: 8, background: 'rgba(74, 222, 128, 0.08)', borderRadius: 6, border: '1px solid var(--border)' }}>
              <strong>Total Monthly Cost:</strong> ${plan.monthlyCost.toFixed(2)} / month
              {plan.canApprove && <span className="badge sm ok" style={{ marginLeft: 8 }}>Ready for customer approval</span>}
            </div>
          ) : (
            <p>Recurring cost: pending calculation · Transfer cost: {plan.transferCost ? `$${plan.transferCost.toFixed(2)}/mo` : 'unknown'} · Downtime: {plan.downtimeSeconds ? `~${Math.round(plan.downtimeSeconds / 60)} min` : 'unknown'}</p>
          )}

          <div className="row" style={{ gap: 6, margin: '8px 0', flexWrap: 'wrap' }}>
            {(plan.provider === 'azure' || plan.provider === 'aws' || plan.provider === 'gcp') && (
              <button className="btn sm" disabled={busy || plan.status === 'connection_unavailable' || new Date(plan.expiresAt).getTime() < Date.now()} onClick={() => void estimate(plan.id, 'compute')}>
                Estimate base VM compute
              </button>
            )}
            {(plan.provider === 'aws' || plan.provider === 'azure' || plan.provider === 'gcp') && (
              <button className="btn sm" disabled={busy || plan.status === 'connection_unavailable' || new Date(plan.expiresAt).getTime() < Date.now()} onClick={() => void estimate(plan.id, 'disk')}>
                Estimate root disk
              </button>
            )}
            <button className="btn sm" disabled={busy || plan.status === 'connection_unavailable' || new Date(plan.expiresAt).getTime() < Date.now()} onClick={() => void estimate(plan.id, 'transfer')}>
              Estimate data transfer
            </button>
          </div>

          {plan.computeQuote && <div>
            <p><strong>Base VM compute only:</strong> {plan.computeQuote.currency} {plan.computeQuote.monthly.toFixed(2)} / month at {plan.computeQuote.hoursPerMonth} hours ({plan.computeQuote.hourly} / hour).</p>
            <p>{new Date(plan.computeQuote.expiresAt).getTime() < Date.now() ? 'Estimate expired. ' : ''}Retrieved {new Date(plan.computeQuote.retrievedAt).toLocaleString()}. <a href={plan.computeQuote.source} target="_blank" rel="noreferrer">Provider pricing source</a></p>
            <p>Excluded from this subtotal:</p>
            <ul>{plan.computeQuote.exclusions.map(item => <li key={item}>{item}</li>)}</ul>
          </div>}

          {plan.diskQuote && <div>
            <p><strong>Root disk only:</strong> {plan.diskQuote.currency} {plan.diskQuote.monthly.toFixed(2)} / month · {plan.diskQuote.diskType} · {plan.diskQuote.diskGb} GiB requested.</p>
            <p>{new Date(plan.diskQuote.expiresAt).getTime() < Date.now() ? 'Estimate expired. ' : ''}Retrieved {new Date(plan.diskQuote.retrievedAt).toLocaleString()}. <a href={plan.diskQuote.source} target="_blank" rel="noreferrer">Disk pricing source</a></p>
            <ul>{plan.diskQuote.exclusions.map(item => <li key={item}>{item}</li>)}</ul>
          </div>}

          {plan.transferQuote && <div>
            <p><strong>Outbound data transfer:</strong> {plan.transferQuote.currency} {plan.transferQuote.monthly.toFixed(2)} / month · {plan.transferQuote.transferGb} GiB ({plan.transferQuote.tier} tier) at ${plan.transferQuote.perGbMonth}/GB.</p>
            <p>Retrieved {new Date(plan.transferQuote.retrievedAt).toLocaleString()}. <a href={plan.transferQuote.source} target="_blank" rel="noreferrer">Data transfer source</a></p>
            <ul>{plan.transferQuote.exclusions.map(item => <li key={item}>{item}</li>)}</ul>
          </div>}

          {plan.blockers.length > 0 && <ul>{plan.blockers.map(blocker => <li key={blocker}>{blocker}</li>)}</ul>}
          {plan.inspection.blockers.length > 0 && <ul>{plan.inspection.blockers.map(blocker => <li key={blocker.code}>{blocker.message}</li>)}</ul>}
          <p>Selected resources</p>
          <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{JSON.stringify(plan.spec, null, 2)}</pre>
        </details>)}
      </details>
      {showWizard && <MigrationWizard onClose={() => setShowWizard(false)} />}
    </>
  );
}
