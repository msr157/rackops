import { useEffect, useState } from 'react';
import { api, type CloudConnection } from '../api';
import { cleanError } from './toast';
import { useConfirm } from './confirm';
import { AWSMachinePreflight } from './AWSMachinePreflight';
import { GCPMachinePreflight } from './GCPMachinePreflight';
import { AzureMachinePreflight } from './AzureMachinePreflight';

export function CloudConnections() {
  const [connections, setConnections] = useState<CloudConnection[]>([]);
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('aws');
  const [extra, setExtra] = useState<Record<string, string>>({});
  const [region, setRegion] = useState('');
  const [accessKeyId, setAccessKeyId] = useState('');
  const [secretAccessKey, setSecretAccessKey] = useState('');
  const [sessionToken, setSessionToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const confirm = useConfirm();
  useEffect(() => {
    let alive = true;
    api.cloudConnections().then(items => { if (alive) setConnections(items); }).catch(err => { if (alive) setError(cleanError(err)); });
    return () => { alive = false; };
  }, []);
  async function save() {
    setBusy(true); setError('');
    try {
      await api.createCloudConnection(name, provider, region.trim(), provider === 'aws' ? { accessKeyId: accessKeyId.trim(), secretAccessKey, sessionToken } : extra);
      setAccessKeyId(''); setSecretAccessKey(''); setSessionToken(''); setExtra({}); setName('');
      setConnections(await api.cloudConnections());
    } catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  async function act(connection: CloudConnection, remove: boolean) {
    if (remove && !await confirm({ title: 'Remove cloud connection?', body: 'Remove the credentials saved in RackOps. This does not revoke them at your cloud provider or delete any cloud resources.', confirmLabel: 'Remove connection' })) return;
    setBusy(true); setError('');
    try {
      if (remove) await api.deleteCloudConnection(connection.id); else await api.validateCloudConnection(connection.id);
      setConnections(await api.cloudConnections());
    } catch (err) { setError(cleanError(err)); }
    finally { setBusy(false); }
  }
  return <section className="card" style={{ padding: 20 }}>
    <h2 className="section-title">Cloud accounts</h2>
    <p>Connect your cloud account for migration setup. Saving checks account access and stores encrypted credentials in this control plane. It creates no cloud resources.</p>
    <p className="dim">Identity validation does not verify launch permissions. Resource provisioning and app migration are not enabled in the dashboard yet.</p>
    {error && <p role="alert">{error}</p>}
    <form onSubmit={event => { event.preventDefault(); void save(); }}>
      <fieldset disabled={busy} style={{ border: 0, padding: 0 }}>
        <label>Connection name<input className="input" required maxLength={100} value={name} onChange={e => setName(e.target.value)} /></label>
        <label>Cloud<select className="input" value={provider} onChange={e => { setProvider(e.target.value); setRegion(''); setExtra({}); setAccessKeyId(''); setSecretAccessKey(''); setSessionToken(''); }}><option value="aws">AWS</option><option value="azure">Azure</option><option value="gcp">Google Cloud</option></select></label>
        <label>Region<input className="input" required placeholder={provider === 'aws' ? 'us-east-1' : provider === 'azure' ? 'eastus' : 'us-central1'} value={region} onChange={e => setRegion(e.target.value)} /></label>
        {provider === 'aws' && <>
        <label>Access key ID<input className="input" required autoComplete="off" value={accessKeyId} onChange={e => setAccessKeyId(e.target.value)} /></label>
        <label>Secret access key<input className="input" required type="password" autoComplete="new-password" value={secretAccessKey} onChange={e => setSecretAccessKey(e.target.value)} /></label>
        <label>Session token (for temporary credentials)<input className="input" type="password" autoComplete="new-password" value={sessionToken} onChange={e => setSessionToken(e.target.value)} /></label>
        </>}
        {provider === 'azure' && [['tenantId', 'Tenant ID'], ['subscriptionId', 'Subscription ID'], ['clientId', 'Client ID'], ['clientSecret', 'Client secret']].map(([key, label]) => <label key={key}>{label}<input className="input" required type={key === 'clientSecret' ? 'password' : 'text'} autoComplete="off" value={extra[key] ?? ''} onChange={e => setExtra({ ...extra, [key]: e.target.value })} /></label>)}
        {provider === 'gcp' && <>
          <label>Project ID<input className="input" required value={extra.projectId ?? ''} onChange={e => setExtra({ ...extra, projectId: e.target.value })} /></label>
          <label>Service-account JSON<textarea className="input" required autoComplete="off" spellCheck={false} rows={5} value={extra.serviceAccountJson ?? ''} onChange={e => setExtra({ ...extra, serviceAccountJson: e.target.value })} /></label>
        </>}
        <button className="btn" type="submit">{busy ? 'Working…' : 'Validate and save account'}</button>
      </fieldset>
    </form>
    {connections.map(connection => <article key={connection.id} style={{ marginTop: 20 }}>
      <strong>{connection.name}</strong><p>{connection.provider} · {connection.accountId} · {connection.region}</p>
      <p className="dim">Identity last checked {new Date(connection.validatedAt).toLocaleString()}</p>
      <button className="btn" disabled={busy} onClick={() => void act(connection, false)}>Revalidate</button>{' '}
      <button className="btn" disabled={busy} onClick={() => void act(connection, true)}>Remove</button>
      {connection.provider === 'aws' && <AWSMachinePreflight connection={connection} />}
      {connection.provider === 'gcp' && <GCPMachinePreflight connection={connection} />}
      {connection.provider === 'azure' && <AzureMachinePreflight connection={connection} />}
    </article>)}
  </section>;
}
