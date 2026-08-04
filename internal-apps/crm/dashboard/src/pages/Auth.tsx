import { useState } from 'react';
import { Headset, LogIn } from 'lucide-react';
import { api, setToken } from '../api';

export function AuthScreen({ onAuthed }: { onAuthed: () => void }) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setError('');
    if (!email.includes('@')) { setError('Enter a valid email address.'); return; }
    if (!password) { setError('Enter your password.'); return; }
    setBusy(true);
    try {
      const r = await api.login(email, password);
      setToken(r.token);
      onAuthed();
    } catch (e) {
      setError(String(e).replace(/^Error: /, ''));
      setBusy(false);
    }
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, display: 'grid', placeItems: 'center',
      background: 'var(--bg)', overflow: 'hidden',
    }}>
      {/* backdrop: rack slats fading out — the hardware behind the glass */}
      <div style={{
        position: 'absolute', inset: 0,
        backgroundImage: 'repeating-linear-gradient(180deg, var(--border) 0 1px, transparent 1px 42px)',
        maskImage: 'radial-gradient(ellipse 65% 60% at 50% 45%, black 25%, transparent 75%)',
        WebkitMaskImage: 'radial-gradient(ellipse 65% 60% at 50% 45%, black 25%, transparent 75%)',
        opacity: 0.6,
      }} />
      <div style={{
        position: 'absolute', width: 420, height: 420, borderRadius: '50%',
        background: 'var(--amber)', opacity: 0.06, filter: 'blur(110px)',
      }} />

      <div style={{
        position: 'relative', width: 400, maxWidth: '92vw',
        background: 'rgba(20, 18, 14, 0.78)', backdropFilter: 'blur(20px)',
        border: '1px solid var(--border-hi)', borderRadius: 16, padding: 30,
      }}>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <div className="brand-unit" style={{ width: 52, height: 38, margin: '0 auto 12px', padding: '0 10px' }}>
            <span className="led warn" />
          </div>
          <div style={{ fontFamily: 'var(--display)', fontSize: 20, fontWeight: 700, letterSpacing: '-0.02em' }}>RackOps CRM</div>
          <div className="dim" style={{ marginTop: 4 }}>Vendor operations console</div>
        </div>

        <div className="field">
          <label>Email address</label>
          <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
            placeholder="ops@rackops.in" autoComplete="username"
            onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>
        <div className="field">
          <label>Password</label>
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••" autoComplete="current-password"
            onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>

        {error && <div className="badge red" style={{ width: '100%', marginBottom: 12 }}>{error}</div>}

        <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={submit}>
          <LogIn size={14} /> Sign in
        </button>

        <div className="faint row" style={{ marginTop: 20, justifyContent: 'center', gap: 6 }}>
          <Headset size={11} /> Internal — RackOps support &amp; telemetry
        </div>
      </div>
    </div>
  );
}
