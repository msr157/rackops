import { useEffect, useState } from 'react';
import { api, type CloudOperation } from '../api';
import { cleanError } from './toast';

export function CloudRecovery() {
  const [operations, setOperations] = useState<CloudOperation[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let alive = true;
    api.cloudOperations().then(value => { if (alive) setOperations(value); }).catch(err => { if (alive) setError(cleanError(err)); });
    return () => { alive = false; };
  }, []);
  async function update(id?: string, action?: 'pause' | 'resume') {
    setBusy(true); setError('');
    try {
      if (id && action) await api.controlCloudRecovery(id, action);
      setOperations(await api.cloudOperations());
    } catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <details style={{ marginTop: 20 }}>
    <summary>Cloud operation recovery ({operations.length})</summary>
    <p>Pause or resume status checks for previously submitted operations. Pausing checks does not stop cloud resources or their charges. Resuming checks does not create a replacement machine.</p>
    <button className="btn" disabled={busy} onClick={() => void update()}>Refresh operations</button>
    {error && <p role="alert">{error}</p>}
    {operations.map(operation => <article key={operation.id} style={{ marginTop: 12 }}>
      <strong>{operation.provider} · {operation.region}</strong>
      <p>Operation {operation.id}</p>
      <p>{operation.recoveryMessage || operation.message}</p>
      <p>Provider resource: {operation.resourceId || 'Not yet verified'} · Total checks: {operation.recoveryTotalAttempts}</p>
      <p>{operation.message}</p>
      {['submitting', 'submitted', 'uncertain'].includes(operation.state) && operation.recoveryState !== 'provider_ready' && <button className="btn" disabled={busy} onClick={() => void update(operation.id, operation.recoveryState === 'paused' || operation.recoveryState === 'needs_attention' ? 'resume' : 'pause')}>
        {operation.recoveryState === 'paused' || operation.recoveryState === 'needs_attention' ? 'Resume status checks' : 'Pause status checks'}
      </button>}
    </article>)}
  </details>;
}
