import { useState } from 'react';
import { api, type CloudConnection, type MachineInspection } from '../api';
import { cleanError } from './toast';

export function GCPMachinePreflight({ connection }: { connection: CloudConnection }) {
  const [zone, setZone] = useState('');
  const [machineType, setMachineType] = useState('');
  const [image, setImage] = useState('');
  const [subnet, setSubnet] = useState('');
  const [disk, setDisk] = useState(30);
  const [architecture, setArchitecture] = useState('amd64');
  const [review, setReview] = useState<MachineInspection | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  function changed() { setReview(null); setError(''); }
  async function check() {
    setBusy(true); changed();
    try { setReview(await api.inspectGCPMachine(connection.id, { operationId: crypto.randomUUID(), projectId: connection.accountId, zone: zone.trim(), machineType: machineType.trim(), image: image.trim(), subnetwork: `projects/${connection.accountId}/regions/${connection.region}/subnetworks/${subnet.trim()}`, diskGb: disk }, architecture)); }
    catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <details style={{ marginTop: 12 }}>
    <summary>Check Google Cloud machine resources</summary>
    <p>Read selected resources in project {connection.accountId}, region {connection.region}. Use a pinned Linux image with the Google guest environment and RackOps prerequisites.</p>
    <form onSubmit={event => { event.preventDefault(); void check(); }}>
      <fieldset disabled={busy} style={{ border: 0, padding: 0 }}>
        <label>Zone<input className="input" required value={zone} placeholder={`${connection.region}-a`} onChange={e => { setZone(e.target.value); changed(); }} /></label>
        <label>Machine type<input className="input" required value={machineType} placeholder="e2-small" onChange={e => { setMachineType(e.target.value); changed(); }} /></label>
        <label>Pinned image resource<input className="input" required value={image} placeholder="projects/your-project/global/images/your-image" onChange={e => { setImage(e.target.value); changed(); }} /></label>
        <label>Subnet name<input className="input" required value={subnet} onChange={e => { setSubnet(e.target.value); changed(); }} /></label>
        <label>Architecture<select className="input" value={architecture} onChange={e => { setArchitecture(e.target.value); changed(); }}><option value="amd64">amd64 (x86-64)</option><option value="arm64">arm64</option></select></label>
        <label>Root disk size (GB)<input className="input" required type="number" min={10} max={65536} step={1} value={disk || ''} onChange={e => { setDisk(Number(e.target.value)); changed(); }} /></label>
        <button className="btn" type="submit">{busy ? 'Inspecting resources…' : 'Check and save resource review'}</button>
      </fieldset>
    </form>
    {error && <p role="alert">{error}</p>}
    {review && <div aria-live="polite">
      <p>Saved review {review.savedPlanId}. Expires {review.expiresAt ? new Date(review.expiresAt).toLocaleString() : 'unknown'}. This is not launch approval.</p>
      <p><strong>{review.resourceCompatible ? 'Selected metadata checks passed' : 'Resource compatibility issues found'}</strong></p>
      <p>{review.vcpus || 'Unknown'} vCPUs · {review.memoryMb || 'Unknown'} MB RAM · {review.zone} · {review.architecture}</p>
      {!!review.blockers.length && <ul>{review.blockers.map(item => <li key={item.code}>{item.message}</li>)}</ul>}
      <p>Launch remains unavailable. Cost has not been estimated. Remaining checks:</p>
      <ul>{review.remainingChecks.map(item => <li key={item}>{item}</li>)}</ul>
    </div>}
  </details>;
}
