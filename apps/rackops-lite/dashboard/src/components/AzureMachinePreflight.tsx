import { useState } from 'react';
import { api, type AzureMachineSpec, type CloudConnection, type MachineInspection } from '../api';
import { cleanError } from './toast';

export function AzureMachinePreflight({ connection }: { connection: CloudConnection }) {
  const [spec, setSpec] = useState<Omit<AzureMachineSpec, 'operationId' | 'subscriptionId'>>({ resourceGroup: '', size: '', imageId: '', networkInterfaceId: '', sshPublicKey: '', diskGb: 30 });
  const [architecture, setArchitecture] = useState('amd64');
  const [review, setReview] = useState<MachineInspection | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  function changed(patch: Partial<typeof spec>) { setSpec({ ...spec, ...patch }); setReview(null); setError(''); }
  async function check() {
    setBusy(true); setError(''); setReview(null);
    try { setReview(await api.inspectAzureMachine(connection.id, { ...spec, operationId: crypto.randomUUID(), subscriptionId: connection.accountId }, architecture)); }
    catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <details style={{ marginTop: 12 }}>
    <summary>Check Azure machine resources</summary>
    <p>Inspect a managed Linux image or pinned Compute Gallery image version and an unused private network interface in {connection.region}. Gallery metadata can verify architecture; managed images still require independent verification.</p>
    <form onSubmit={event => { event.preventDefault(); void check(); }}>
      <fieldset disabled={busy} style={{ border: 0, padding: 0 }}>
        {([['resourceGroup', 'Target resource group'], ['size', 'VM size'], ['imageId', 'Managed image or pinned gallery version resource ID'], ['networkInterfaceId', 'Network interface resource ID']] as const).map(([key, label]) => <label key={key}>{label}<input className="input" required value={spec[key]} onChange={event => changed({ [key]: event.target.value.trim() })} /></label>)}
        <label>SSH public key<textarea className="input" required rows={2} value={spec.sshPublicKey} placeholder="ssh-ed25519 … (public key only)" onChange={event => changed({ sshPublicKey: event.target.value })} /></label>
        <label>Architecture<select className="input" value={architecture} onChange={event => { setArchitecture(event.target.value); setReview(null); setError(''); }}><option value="amd64">amd64 (x86-64)</option><option value="arm64">arm64</option></select></label>
        <label>Root disk size (GB)<input className="input" required type="number" min={8} max={32767} step={1} value={spec.diskGb || ''} onChange={event => changed({ diskGb: Number(event.target.value) })} /></label>
        <button className="btn" type="submit">{busy ? 'Inspecting resources…' : 'Check and save resource review'}</button>
      </fieldset>
    </form>
    {error && <p role="alert">{error}</p>}
    {review && <div aria-live="polite">
      <p>Saved review {review.savedPlanId}. Expires {review.expiresAt ? new Date(review.expiresAt).toLocaleString() : 'unknown'}. This is not launch approval.</p>
      <p><strong>{review.resourceCompatible ? 'Selected metadata checks passed' : 'Resource compatibility issues found'}</strong></p>
      <p>{review.vcpus || 'Unknown'} vCPUs · {review.memoryMb || 'Unknown'} MiB RAM · {review.region} · {review.architecture}</p>
      <ul>{review.blockers.map(item => <li key={item.code}>{item.message}</li>)}</ul>
      <p>Launch remains unavailable. Cost has not been estimated. Remaining checks:</p>
      <ul>{review.remainingChecks.map(item => <li key={item}>{item}</li>)}</ul>
    </div>}
  </details>;
}
