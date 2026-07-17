import { useEffect, useState } from 'react';
import { Server, LogIn, UserPlus, KeyRound, ArrowLeft, MailQuestion } from 'lucide-react';
import { api, setToken } from '../api';
import { InfoTip, PasswordInput } from '../components/ui';

type Mode = 'login' | 'signup' | 'forgot' | 'reset';

export function AuthScreen({ onAuthed }: { onAuthed: () => void }) {
  // Landing on /reset-password?token=… (from the email or the server log)
  // opens straight into reset mode.
  const resetToken = new URLSearchParams(window.location.search).get('token') ?? '';
  const [mode, setMode] = useState<Mode>(
    window.location.pathname === '/reset-password' && resetToken ? 'reset' : 'login');
  const [needsSetup, setNeedsSetup] = useState(false);
  const [email, setEmail] = useState('');
  const [fullName, setFullName] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.authStatus().then((s) => {
      setNeedsSetup(s.needsSetup);
      if (s.needsSetup) setMode('signup');
    }).catch(() => {});
  }, []);

  const switchMode = (m: Mode) => {
    setMode(m); setError(''); setNotice(''); setPassword(''); setConfirm('');
  };

  const submit = async () => {
    setError(''); setNotice('');

    if (mode === 'forgot') {
      if (!email.includes('@')) { setError('Enter a valid email address.'); return; }
      setBusy(true);
      try {
        const r = await api.forgotPassword(email);
        setNotice(r.message);
      } catch (e) { setError(String(e).replace(/^Error: \d+: /, '')); }
      setBusy(false);
      return;
    }

    if (!(mode === 'reset') && !email.includes('@')) { setError('Enter a valid email address.'); return; }
    if (password.length < 8) { setError('Password must be at least 8 characters.'); return; }
    if ((mode === 'signup' || mode === 'reset') && password !== confirm) {
      setError('Passwords do not match.'); return;
    }

    setBusy(true);
    try {
      const r = mode === 'signup' ? await api.signup(email, fullName, password)
        : mode === 'reset' ? await api.resetPassword(resetToken, password)
        : await api.login(email, password);
      setToken(r.token);
      if (mode === 'reset') window.history.replaceState(null, '', '/');
      onAuthed();
    } catch (e) {
      setError(String(e).replace(/^Error: \d+: /, ''));
      setBusy(false);
    }
  };

  const title = mode === 'forgot' ? 'Reset your password'
    : mode === 'reset' ? 'Choose a new password' : null;

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
            <span className="led ok" />
          </div>
          <div style={{ fontFamily: 'var(--display)', fontSize: 20, fontWeight: 700, letterSpacing: '-0.02em' }}>RackOps</div>
          <div className="dim" style={{ marginTop: 4 }}>{title ?? 'Your hardware. Your cloud.'}</div>
        </div>

        {needsSetup && mode === 'signup' && (
          <div className="badge amber" style={{ width: '100%', justifyContent: 'center', marginBottom: 16 }}>
            First run — create the owner account
            <InfoTip wide>
              The first account becomes the Owner: it manages users, organizations, and updates.
              After this, new people join through invitations or an admin creating their account.
            </InfoTip>
          </div>
        )}

        {mode === 'forgot' && (
          <div className="dim" style={{ marginBottom: 16, lineHeight: 1.6 }}>
            Enter your account email. We&rsquo;ll send a reset link — it&rsquo;s also printed in the
            server log (<code className="mono">journalctl -u rackops</code>) if email can&rsquo;t reach you.
          </div>
        )}

        {mode === 'signup' && (
          <div className="field">
            <label>Full name</label>
            <input className="input" value={fullName} onChange={(e) => setFullName(e.target.value)} placeholder="Chief Operator" />
          </div>
        )}
        {mode !== 'reset' && (
          <div className="field">
            <label>Email address</label>
            <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@lab.local"
              onKeyDown={(e) => e.key === 'Enter' && submit()} />
          </div>
        )}
        {mode !== 'forgot' && (
          <div className="field">
            <label>{mode === 'reset' ? 'New password' : 'Password'}</label>
            <PasswordInput value={password} onChange={setPassword} placeholder="8+ characters"
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'} onEnter={submit} />
          </div>
        )}
        {(mode === 'signup' || mode === 'reset') && (
          <div className="field">
            <label>Confirm password</label>
            <PasswordInput value={confirm} onChange={setConfirm} placeholder="same password again"
              autoComplete="new-password" onEnter={submit} />
            {confirm.length > 0 && confirm !== password && (
              <span className="faint" style={{ color: 'var(--warn)' }}>Passwords don&rsquo;t match yet.</span>
            )}
          </div>
        )}

        {error && <div className="badge red" style={{ width: '100%', marginBottom: 12 }}>{error}</div>}
        {notice && <div className="badge amber" style={{ width: '100%', marginBottom: 12, lineHeight: 1.5 }}>{notice}</div>}

        <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={submit}>
          {mode === 'signup' ? <><UserPlus size={14} /> Create account</>
            : mode === 'forgot' ? <><MailQuestion size={14} /> Send reset link</>
            : mode === 'reset' ? <><KeyRound size={14} /> Set new password</>
            : <><LogIn size={14} /> Sign in</>}
        </button>

        {mode === 'login' && (
          <div style={{ textAlign: 'center', marginTop: 16 }}>
            <button className="btn ghost sm" onClick={() => switchMode('forgot')}>Forgot password?</button>
          </div>
        )}
        {(mode === 'forgot' || mode === 'reset') && (
          <div style={{ textAlign: 'center', marginTop: 16 }}>
            <button className="btn ghost sm" onClick={() => switchMode('login')}>
              <ArrowLeft size={12} /> Back to sign in
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
