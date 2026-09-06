import { useCallback, useEffect, useState } from 'react';
import { Globe, ShieldAlert, Radio, Ban, Check, Copy } from 'lucide-react';
import { api } from '../api';
import type { FrontDoorStatus, Tenant } from '../api';
import { EmptyState, Kpi, timeAgo } from '../components/ui';

// formatBytes renders transfer for humans. Binary units, one decimal, so a
// 100 GiB quota reads as "100 GB" rather than 107374182400.
function formatBytes(n: number): string {
  if (!n || n < 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

// FrontDoorPage is the tunnel edge's console: who is connected, what they are
// using, and the abuse kill switch.
//
// It lives in the same app as the support inbox on purpose. An abuse report
// arrives as a ticket; acting on it should not mean SSHing to a POP and
// curling an admin API from memory. Here it is two clicks away from the
// message that reported it.
export function FrontDoorPage() {
  const [status, setStatus] = useState<FrontDoorStatus | null>(null);
  const [tenants, setTenants] = useState<Tenant[] | null>(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [tenantId, setTenantId] = useState('');
  const [publicKey, setPublicKey] = useState('');
  const [workspace, setWorkspace] = useState('');
  const [grant, setGrant] = useState<{ token: string; expiresAt: string } | null>(null);

  const issueGrant = async (id: string) => {
    setBusy(id); setError(''); setGrant(null);
    try { setGrant(await api.enrollment(id)); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(''); }
  };

  const register = async () => {
    setBusy('register'); setError(''); setGrant(null);
    try {
      if ((tenants ?? []).some((t) => t.id === tenantId.trim())) {
        throw new Error('This install is already registered. Issue a new credential from its row below.');
      }
      await api.registerTenant(tenantId.trim(), publicKey.trim(), workspace.trim());
      setGrant(await api.enrollment(tenantId.trim()));
      load();
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(''); }
  };

  const load = useCallback(() => {
    api.frontDoor().then(setStatus).catch(() => {});
    api.tenants().then(setTenants).catch(() => setTenants([]));
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  const suspend = async (t: Tenant) => {
    // A reason is mandatory server-side: it is shown to the customer and to
    // their visitors, so suspending without one produces an outage nobody
    // can explain. Ask for it here rather than surfacing a 400.
    let reason = '';
    if (!t.suspended) {
      reason = window.prompt(
        `Take ${t.workspace || t.id} offline?\n\n`
        + 'This reason is shown to the customer and to anyone visiting their apps:',
        'Suspended after an abuse report',
      ) ?? '';
      if (!reason.trim()) return;
    }
    setBusy(t.id);
    setError('');
    try {
      await api.suspendTenant(t.id, !t.suspended, reason);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
    setBusy('');
  };

  if (!status) return null;

  // Not enabled: say what to do about it rather than showing empty tables.
  if (!status.enabled) {
    return (
      <div className="page">
        <div className="page-title">Front Door</div>
        <div className="page-sub">The tunnel edge customers connect to for public HTTPS.</div>
        <EmptyState icon={<Globe size={22} />} title="Tunnel not running here">
          {status.reason || 'Start rackops-admin with --frontdoor to run the edge on this machine.'}
        </EmptyState>
      </div>
    );
  }

  const list = tenants ?? [];
  const connected = list.filter((t) => t.connected).length;

  return (
    <div className="page">
      <div className="page-title">Front Door</div>
      <div className="page-sub">
        Tunnel edge <span className="mono">{status.popId}</span> serving{' '}
        <span className="mono">*.{status.zone}</span>
      </div>

      {status.selfSigned && (
        <div className="card" style={{ borderColor: 'var(--warn)', marginBottom: 16 }}>
          <div className="row" style={{ gap: 10, alignItems: 'flex-start' }}>
            <ShieldAlert size={16} style={{ color: 'var(--warn)', flex: 'none', marginTop: 2 }} />
            <div>
              <div style={{ fontWeight: 500 }}>Development certificate</div>
              <div className="dim" style={{ marginTop: 2 }}>
                This edge is serving a self-signed certificate — browsers will refuse every
                customer URL. Pass <span className="mono">--cert</span> and{' '}
                <span className="mono">--key</span> with a wildcard for{' '}
                <span className="mono">*.{status.zone}</span>.
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="kpi-row">
        <Kpi icon={<Radio size={13} />} label="Connected" value={connected}
          unit={`of ${status.tenants}`} />
        <Kpi icon={<Globe size={13} />} label="Requests served" value={status.requests.toLocaleString()} />
        <Kpi icon={<Globe size={13} />} label="Traffic" value={formatBytes(status.bytes)} />
        <Kpi icon={<ShieldAlert size={13} />} label="Certificate"
          value={status.certDaysLeft} unit="days left" />
      </div>

      <div className="card" style={{ marginTop: 16 }}>
        <div className="eyebrow">Point an install at this edge</div>
        <div className="dim" style={{ marginBottom: 8 }}>
          Register the install identity first, then issue its own enrollment credential.
          Credentials expire after 24 hours; existing tunnels keep serving, but later
          hostname changes require a fresh credential. Never share the vendor signing secret.
        </div>
        <CopyLine text={enrollFlags(status)} />
        <form onSubmit={(event) => { event.preventDefault(); void register(); }} style={{ marginTop: 12 }}>
          <label>Install tenant ID<input className="input" required value={tenantId} onChange={(e) => setTenantId(e.target.value)} /></label>
          <label>Agent Ed25519 public key<input className="input" required pattern="[a-fA-F0-9]{64}" value={publicKey} onChange={(e) => setPublicKey(e.target.value)} /></label>
          <label>Workspace<input className="input" required pattern="[a-z0-9]([a-z0-9-]*[a-z0-9])?" value={workspace} onChange={(e) => setWorkspace(e.target.value)} /></label>
          <button className="btn sm" disabled={!!busy}>Register install and issue credential</button>
        </form>
        {grant && <div role="status" style={{ marginTop: 12 }}>
          <div>Credential expires {new Date(grant.expiresAt).toLocaleString()}</div>
          <CopyLine text={`--frontdoor-token ${grant.token}`} />
          <button className="btn sm" onClick={() => setGrant(null)}>Hide credential</button>
        </div>}
        <div className="dim" style={{ marginTop: 8, fontSize: 12 }}>
          The token authenticates the customer&rsquo;s control plane, not a person.
          Rotate it by deleting <span className="mono">frontdoor-enroll.token</span> from
          the data directory and restarting.
        </div>
      </div>

      {error && (
        <div className="card" style={{ borderColor: 'var(--crit)', marginTop: 16 }}>
          <span style={{ color: 'var(--crit)' }}>{error}</span>
        </div>
      )}

      <div className="section-title" style={{ marginTop: 22 }}>Tenants</div>
      {list.length === 0 ? (
        <EmptyState icon={<Globe size={22} />} title="No installs enrolled yet">
          Register an install using its tenant ID, workspace, and agent public key above.
        </EmptyState>
      ) : (
        <div className="card" style={{ padding: 0 }}>
          <table className="table">
            <thead>
              <tr>
                <th>Workspace</th><th>Addresses</th><th>Transport</th>
                <th>Traffic this month</th><th>State</th><th></th>
              </tr>
            </thead>
            <tbody>
              {list.map((t) => {
                const pct = t.quotaBytes > 0 ? (t.usedBytes / t.quotaBytes) * 100 : 0;
                return (
                  <tr key={t.id}>
                    <td>
                      <span className="row" style={{ gap: 8 }}>
                        <span className={`led ${t.connected ? 'ok' : 'off'}`} />
                        <span style={{ fontWeight: 500 }}>{t.workspace || t.id}</span>
                      </span>
                      <div className="dim mono" style={{ fontSize: 11, marginTop: 2 }}>{t.id}</div>
                    </td>
                    <td>
                      {(t.hostnames ?? []).length === 0
                        ? <span className="dim">none yet</span>
                        : (t.hostnames ?? []).map((h) => (
                          <div key={h} className="mono" style={{ fontSize: 12 }}>{h}</div>
                        ))}
                    </td>
                    <td className="mono dim">
                      {t.connected
                        // TCP means the customer's network blocks UDP. Worth
                        // showing: it is the first thing to check if they
                        // report the tunnel feeling slow.
                        ? (t.transport === 'tcp' ? <span className="tag">TCP fallback</span> : t.transport)
                        : '—'}
                    </td>
                    <td>
                      <div className="mono" style={{ fontSize: 12 }}>
                        {formatBytes(t.usedBytes)}
                        {t.quotaBytes > 0 && <span className="dim"> / {formatBytes(t.quotaBytes)}</span>}
                      </div>
                      {t.quotaBytes > 0 && (
                        <div className="bar" style={{ marginTop: 4 }}>
                          <div className={`bar-fill ${pct > 90 ? 'crit' : pct > 75 ? 'warn' : 'ok'}`}
                            style={{ width: `${Math.min(pct, 100)}%` }} />
                        </div>
                      )}
                    </td>
                    <td>
                      {t.suspended
                        ? <span style={{ color: 'var(--crit)', fontSize: 12 }}>
                            suspended
                            {t.suspendReason && <div className="dim" style={{ fontSize: 11 }}>{t.suspendReason}</div>}
                          </span>
                        : <span className="dim" style={{ fontSize: 12 }}>
                            {t.connected ? 'live' : `last seen ${timeAgo(t.updatedAt)}`}
                          </span>}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <button className="btn sm" disabled={!!busy || t.suspended} onClick={() => void issueGrant(t.id)}>
                        Issue credential
                      </button>
                      <button className="btn sm" disabled={busy === t.id} onClick={() => suspend(t)}>
                        {t.suspended ? <><Check size={13} /> Restore</> : <><Ban size={13} /> Suspend</>}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// enrollFlags builds the exact flags a customer's control plane needs. The
// admin URL stays a placeholder because only the operator knows how their
// console is reachable from a customer network — over a tunnel, a VPN, or a
// public hostname.
function enrollFlags(s: FrontDoorStatus): string {
  return [
    `--frontdoor-pop ${s.publicAddr}`,
    `--frontdoor-zone ${s.zone}`,
    '--frontdoor-admin https://<this-console>',
    '--frontdoor-token <tenant-enrollment-credential>',
  ].join(' ');
}

// CopyLine shows a command and copies it, so setup values are never retyped
// from a screenshot.
function CopyLine({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="row" style={{ gap: 8 }}>
      <code className="mono" style={{ flex: 1, fontSize: 12, wordBreak: 'break-all' }}>{text}</code>
      <button className="btn sm" onClick={() => {
        navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        }).catch(() => {});
      }}>
        {copied ? <Check size={13} /> : <Copy size={13} />} {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  );
}
