import { useEffect, useState } from 'react';
import { Server, LogIn, UserPlus } from 'lucide-react';
import { api, setToken } from '../api';

export function AuthScreen({ onAuthed }: { onAuthed: () => void }) {
  const [mode, setMode] = useState<'signup' | 'login'>('login');
  const [needsSetup, setNeedsSetup] = useState(false);
  const [email, setEmail] = useState('');
  const [fullName, setFullName] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.authStatus().then((s) => {
      setNeedsSetup(s.needsSetup);
      if (s.needsSetup) setMode('signup');
    }).catch(() => {});
  }, []);

  const submit = async () => {
    setError('');
    if (!email.includes('@')) { setError('Enter a valid email address.'); return; }
    if (password.length < 8) { setError('Password must be at least 8 characters.'); return; }
    setBusy(true);
    try {
      const r = mode === 'signup'
        ? await api.signup(email, fullName, password)
        : await api.login(email, password);
      setToken(r.token);
      onAuthed();
    } catch (e) {
      setError(String(e).replace(/^Error: \d+: /, ''));
      setBusy(false);
    }
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, display: 'grid', placeItems: 'center',
      background: 'var(--bg)', overflow: 'hidden',
    }}>
      {/* grid mesh + radial glow backdrop */}
      <div style={{
        position: 'absolute', inset: 0,
        backgroundImage: 'linear-gradient(var(--border) 1px, transparent 1px), linear-gradient(90deg, var(--border) 1px, transparent 1px)',
        backgroundSize: '48px 48px',
        maskImage: 'radial-gradient(ellipse 70% 60% at 50% 45%, black 30%, transparent 75%)',
        WebkitMaskImage: 'radial-gradient(ellipse 70% 60% at 50% 45%, black 30%, transparent 75%)',
        opacity: 0.5,
      }} />
      <div style={{
        position: 'absolute', width: 420, height: 420, borderRadius: '50%',
        background: 'var(--accent)', opacity: 0.07, filter: 'blur(110px)',
      }} />

      <div style={{
        position: 'relative', width: 400, maxWidth: '92vw',
        background: 'rgba(16,16,20,0.72)', backdropFilter: 'blur(20px)',
        border: '1px solid var(--border-hi)', borderRadius: 16, padding: 30,
      }}>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <div className="brand-icon" style={{ width: 44, height: 44, fontSize: 21, margin: '0 auto 12px' }}>R</div>
          <div style={{ fontSize: 19, fontWeight: 700 }}>RackOps Lite</div>
          <div className="dim" style={{ marginTop: 4 }}>The Infrastructure Operating System</div>
        </div>

        {needsSetup && (
          <div className="badge cyan" style={{ width: '100%', justifyContent: 'center', marginBottom: 16 }}>
            First run — provision the owner account
          </div>
        )}

        {mode === 'signup' && (
          <div className="field">
            <label>Full name</label>
            <input className="input" value={fullName} onChange={(e) => setFullName(e.target.value)} placeholder="Chief Operator" />
          </div>
        )}
        <div className="field">
          <label>Email address</label>
          <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@lab.local"
            onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>
        <div className="field">
          <label>Master password</label>
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="8+ characters"
            onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>

        {error && <div className="badge red" style={{ width: '100%', marginBottom: 12 }}>{error}</div>}

        <button className="btn primary" style={{ width: '100%', justifyContent: 'center' }} disabled={busy} onClick={submit}>
          {mode === 'signup' ? <><UserPlus size={14} /> Provision Account</> : <><LogIn size={14} /> Operator Login</>}
        </button>

        {!needsSetup && (
          <div style={{ textAlign: 'center', marginTop: 16 }}>
            <button className="nav-item" style={{ display: 'inline', textDecoration: 'underline dotted', color: 'var(--text-dim)' }}
              onClick={() => setMode(mode === 'signup' ? 'login' : 'signup')}>
              {mode === 'signup' ? 'Back to operator login' : 'Provision a new account'}
            </button>
          </div>
        )}

        <div className="faint row" style={{ marginTop: 20, justifyContent: 'center', gap: 6 }}>
          <Server size={11} /> Self-hosted · your hardware, your data
        </div>
      </div>
    </div>
  );
}
