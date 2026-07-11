import { useEffect, useState } from 'react';
import { Server, LogIn, UserPlus, KeyRound, ArrowLeft, MailCheck } from 'lucide-react';
import { api, setToken } from '../api';
import { PasswordInput } from '../components/ui';

type Mode = 'login' | 'signup' | 'forgot' | 'reset';

export function AuthScreen({ onAuthed }: { onAuthed: () => void }) {
  // /reset-password?token=… (from the emailed link) lands straight in reset mode.
  const resetToken = new URLSearchParams(window.location.search).get('token') ?? '';
  const [mode, setMode] = useState<Mode>(
    window.location.pathname === '/reset-password' && resetToken ? 'reset' : 'login',
  );
  const [needsSetup, setNeedsSetup] = useState(false);
  const [email, setEmail] = useState('');
  const [fullName, setFullName] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
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
      if (!email.includes('@')) { setError('Enter the email address of your account.'); return; }
      setBusy(true);
      try {
        const r = await api.forgotPassword(email);
        setNotice(r.smtpConfigured
          ? 'If that address has an account, a reset link is on its way. Check your inbox (and spam).'
          : 'Reset link created. Email isn’t configured on this server, so the link was written to the server log — run: journalctl -u rackops | grep "reset link" (or check ~/.rackops/server.log).');
      } catch (e) { setError(cleanAuthError(e)); }
      setBusy(false);
      return;
    }

    if (mode === 'reset') {
      if (password.length < 8) { setError('Password must be at least 8 characters.'); return; }
      if (password !== confirm) { setError('Passwords don’t match.'); return; }
      setBusy(true);
      try {
        const r = await api.resetPassword(resetToken, password);
        setToken(r.token);
        window.history.replaceState(null, '', '/');
        onAuthed();
        return;
      } catch (e) { setError(cleanAuthError(e)); setBusy(false); }
      return;
    }

    if (!email.includes('@')) { setError('Enter a valid email address.'); return; }
    if (password.length < 8) { setError('Password must be at least 8 characters.'); return; }
    if (mode === 'signup' && password !== confirm) { setError('Passwords don’t match — retype them.'); return; }
    setBusy(true);
    try {
      const r = mode === 'signup'
        ? await api.signup(email, fullName, password)
        : await api.login(email, password);
      setToken(r.token);
      onAuthed();
    } catch (e) {
      setError(cleanAuthError(e));
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
            <span className="led ok" />
          </div>
          <div style={{ fontFamily: 'var(--display)', fontSize: 20, fontWeight: 700, letterSpacing: '-0.02em' }}>RackOps</div>
          <div className="dim" style={{ marginTop: 4 }}>
            {mode === 'forgot' ? 'Reset your password'
              : mode === 'reset' ? 'Choose a new password'
              : 'Your hardware. Your cloud.'}
          </div>
        </div>

        {needsSetup && mode === 'signup' && (
          <div className="badge amber" style={{ width: '100%', justifyContent: 'center', marginBottom: 16 }}>
            First run — create the owner account
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

        {(mode === 'login' || mode === 'signup' || mode === 'reset') && (
          <div className="field">
            <label>{mode === 'reset' ? 'New password' : 'Password'}</label>
            <PasswordInput value={password} onChange={setPassword} onEnter={submit}
              placeholder="8+ characters" autoComplete={mode === 'login' ? 'current-password' : 'new-password'} />
          </div>
        )}

        {(mode === 'signup' || mode === 'reset') && (
          <div className="field">
            <label>Confirm password</label>
            <PasswordInput value={confirm} onChange={setConfirm} onEnter={submit}
              placeholder="type it again" autoComplete="new-password" />
            {confirm.length > 0 && confirm !== password && (
              <span className="faint" style={{ color: 'var(--warn)' }}>doesn&rsquo;t match yet</span>
            )}
          </div>
        )}

        {error && <div className="badge red" style={{ width: '100%', marginBottom: 12, whiteSpace: 'normal', height: 'auto', lineHeight: 1.5, padding: '8px 10px' }}>{error}</div>}
        {notice && (
          <div className="badge green" style={{ width: '100%', marginBottom: 12, whiteSpace: 'normal', height: 'auto', lineHeight: 1.5, padding: '8px 10px' }}>
            <MailCheck size={12} style={{ flexShrink: 0 }} /> {notice}
          </div>
        )}

        <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={submit}>
          {mode === 'signup' ? <><UserPlus size={14} /> Create account</>
            : mode === 'forgot' ? <><KeyRound size={14} /> Send reset link</>
            : mode === 'reset' ? <><KeyRound size={14} /> Set new password</>
            : <><LogIn size={14} /> Sign in</>}
        </button>

        <div style={{ textAlign: 'center', marginTop: 16, display: 'flex', flexDirection: 'column', gap: 4 }}>
          {mode === 'login' && (
            <>
              <button className="btn ghost sm" onClick={() => switchMode('forgot')}>Forgot your password?</button>
              {!needsSetup && (
                <button className="btn ghost sm" onClick={() => switchMode('signup')}>Create a new account</button>
              )}
            </>
          )}
          {mode === 'signup' && !needsSetup && (
            <button className="btn ghost sm" onClick={() => switchMode('login')}>Back to sign in</button>
          )}
          {(mode === 'forgot' || mode === 'reset') && (
            <button className="btn ghost sm" onClick={() => switchMode('login')}><ArrowLeft size={12} /> Back to sign in</button>
          )}
        </div>

        <div className="faint row" style={{ marginTop: 20, justifyContent: 'center', gap: 6 }}>
          <Server size={11} /> Self-hosted · your hardware, your data
        </div>
      </div>
    </div>
  );
}

function cleanAuthError(e: unknown): string {
  return String(e).replace(/^Error: \d+: /, '').trim() || 'Something went wrong — try again.';
}
