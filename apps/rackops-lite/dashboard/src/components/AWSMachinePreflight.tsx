import { useState } from 'react';
import { api, type AWSMachineSpec, type CloudConnection, type MachineInspection } from '../api';
import { cleanError } from './toast';

export function AWSMachinePreflight({ connection }: { connection: CloudConnection }) {
  const [spec, setSpec] = useState<Omit<AWSMachineSpec, 'operationId'>>({ imageId: '', instanceType: '', subnetId: '', securityGroupId: '', rootDevice: '/dev/sda1', diskGb: 20 });
  const [architecture, setArchitecture] = useState('amd64');
  const [review, setReview] = useState<MachineInspection | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  function change(patch: Partial<typeof spec>) { setSpec({ ...spec, ...patch }); setReview(null); setError(''); }
  async function check() {
    setBusy(true); setError(''); setReview(null);
    try {
      setReview(await api.inspectAWSMachine(connection.id, { ...spec, operationId: crypto.randomUUID() }, architecture));
    } catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <details style={{ marginTop: 12 }}>
    <summary>Check AWS machine resources</summary>
    <p>Inspect resources you selected in {connection.region}. This reads AWS metadata and creates no resources. Use a prepared Linux image with Docker, Compose, Git and systemd.</p>
    <form onSubmit={event => { event.preventDefault(); void check(); }}>
      <fieldset disabled={busy} style={{ border: 0, padding: 0 }}>
        {([
          ['imageId', 'AMI ID', 'ami-…'], ['instanceType', 'VM size', 't3.small'],
          ['subnetId', 'Subnet ID', 'subnet-…'], ['securityGroupId', 'Security group ID', 'sg-…'],
        ] as const).map(([key, label, placeholder]) => <label key={key}>{label}<input className="input" required maxLength={64} placeholder={placeholder} value={spec[key]} onChange={event => change({ [key]: event.target.value.trim() })} /></label>)}
        <label>Architecture<select className="input" value={architecture} onChange={event => { setArchitecture(event.target.value); setReview(null); setError(''); }}><option value="amd64">amd64 (x86-64)</option><option value="arm64">arm64</option></select></label>
        <label>Image root device<select className="input" value={spec.rootDevice} onChange={event => change({ rootDevice: event.target.value })}><option value="/dev/sda1">/dev/sda1</option><option value="/dev/xvda">/dev/xvda</option></select></label>
        <label>Root disk size (GiB)<input className="input" required type="number" min={8} max={16384} step={1} value={spec.diskGb || ''} onChange={event => change({ diskGb: Number(event.target.value) })} /></label>
        <button className="btn" type="submit">{busy ? 'Inspecting AWS resources…' : 'Check and save resource review'}</button>
      </fieldset>
    </form>
    {error && <p role="alert">{error}</p>}
    {review && <div aria-live="polite">
      <p>Saved review {review.savedPlanId}. Expires {review.expiresAt ? new Date(review.expiresAt).toLocaleString() : 'unknown'}. This is not launch approval.</p>
      <p><strong>{review.resourceCompatible ? 'Selected metadata checks passed' : 'Resource compatibility issues found'}</strong></p>
      <p>{review.vcpus || 'Unknown'} vCPUs · {review.memoryMb ? `${review.memoryMb} MiB RAM` : 'Memory unknown'} · {review.zone || 'Zone unknown'} · {review.architecture}</p>
      <p>Image minimum disk: {review.minimumDiskGb ? `${review.minimumDiskGb} GiB` : 'unknown'}. Checked {new Date(review.checkedAt).toLocaleString()}.</p>
      {!!review.blockers.length && <ul>{review.blockers.map(item => <li key={item.code}>{item.message}</li>)}</ul>}
      <p>Launch remains unavailable. Cost has not been estimated. These checks are still needed:</p>
      <ul>{review.remainingChecks.map(item => <li key={item}>{item}</li>)}</ul>
    </div>}
  </details>;
}
