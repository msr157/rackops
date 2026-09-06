import { useEffect, useState } from 'react';
import { Server, LogIn, UserPlus, KeyRound, ArrowLeft, MailQuestion } from 'lucide-react';
import { api, setToken } from '../api';
import { AuthShell } from '../components/AuthShell';
import { cleanError } from '../components/toast';
import { Field, InfoTip, PasswordInput, Spinner } from '../components/ui';

type Mode = 'login' | 'signup' | 'forgot' | 'reset';

const BROKEN_RESET_MSG = 'This reset link is incomplete. Request a new one.';

export function AuthScreen({ onAuthed }: { onAuthed: () => void }) {
  // Landing on /reset-password?token=… from the recovery email.
  // opens straight into reset mode. A reset link that lost its token lands on
  // the forgot form with an explanation — never silently on plain sign-in,
  // which would read as "the link did nothing".
  const resetToken = new URLSearchParams(window.location.search).get('token') ?? '';
  const brokenResetLink = window.location.pathname === '/reset-password' && !resetToken;
  const [mode, setMode] = useState<Mode>(
    window.location.pathname === '/reset-password' && resetToken ? 'reset'
      : brokenResetLink ? 'forgot' : 'login');
  const [needsSetup, setNeedsSetup] = useState(false);
  const [email, setEmail] = useState('');
  const [fullName, setFullName] = useState('');
  const [setupToken, setSetupToken] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState(brokenResetLink ? BROKEN_RESET_MSG : '');
  const [busy, setBusy] = useState(false);
  // sent: a reset email went out — the send button stays disabled so the same
  // request isn't fired twice; editing the address re-arms it.
  const [sent, setSent] = useState(false);

  useEffect(() => {
    api.authStatus().then((s) => {
      setNeedsSetup(s.needsSetup);
      if (s.needsSetup) {
        setMode('signup');
        // No accounts exist yet, so the broken-reset explanation is moot.
        setError((prev) => (prev === BROKEN_RESET_MSG ? '' : prev));
      }
    }).catch(() => {});
  }, []);

  const switchMode = (m: Mode) => {
    setMode(m); setError(''); setNotice(''); setPassword(''); setConfirm(''); setSent(false);
  };

  const submit = async () => {
    setError(''); setNotice('');

    if (mode === 'forgot') {
      if (!email.includes('@')) { setError('Enter a valid email address.'); return; }
      setBusy(true);
      try {
        const r = await api.forgotPassword(email);
        setNotice(r.message);
        setSent(true);
      } catch (e) { setError(cleanError(e)); }
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
      const r = mode === 'signup' ? await api.signup(email, fullName, password, setupToken)
        : mode === 'reset' ? await api.resetPassword(resetToken, password)
        : await api.login(email, password);
      setToken(r.token);
      if (mode === 'reset') window.history.replaceState(null, '', '/');
      onAuthed();
    } catch (e) {
      setError(cleanError(e));
      setBusy(false);
    }
  };

  const subtitle = mode === 'forgot' ? 'Reset your password'
    : mode === 'reset' ? 'Choose a new password'
    : 'Your hardware. Your cloud.';

  return (
    <AuthShell subtitle={subtitle}
      footer={
        <span className="faint row" style={{ justifyContent: 'center', gap: 6 }}>
          <Server size={11} aria-hidden /> Self-hosted · your hardware, your data
        </span>
      }>
      {needsSetup && mode === 'signup' && (
        <div className="auth-notice">
          First run: create the owner account
          <InfoTip wide>
            The first account becomes the Owner: it manages users, organizations, and updates.
            After this, new people join through invitations or an admin creating their account.
          </InfoTip>
        </div>
      )}

      {mode === 'forgot' && (
        <p className="dim" style={{ marginBottom: 16, lineHeight: 1.6 }}>
          Enter your account email to request a reset link. If it does not arrive,
          contact your installation administrator to check email delivery.
        </p>
      )}

      {/* A real <form>, so Enter submits from any field. noValidate keeps the
          messages below as the single source of validation truth. */}
      <form noValidate onSubmit={(e) => { e.preventDefault(); if (!busy && !(mode === 'forgot' && sent)) submit(); }}>
        {needsSetup && mode === 'signup' && <Field label="Owner setup token"
          hint="Use the single-use token printed in your RackOps server startup log (journalctl -u rackops on Linux).">
          {(p) => <input className="input mono" {...p} type="password" value={setupToken} autoComplete="off"
            onChange={(e) => setSetupToken(e.target.value)} />}
        </Field>}
        {mode === 'signup' && (
          <Field label="Full name">
            {(p) => (
              <input className="input" {...p} value={fullName} autoComplete="name"
                onChange={(e) => setFullName(e.target.value)} placeholder="Chief Operator" />
            )}
          </Field>
        )}
        {mode !== 'reset' && (
          <Field label="Email address">
            {(p) => (
              <input className="input" {...p} type="email" value={email} autoComplete="email"
                onChange={(e) => { setEmail(e.target.value); if (sent) setSent(false); }}
                placeholder="you@lab.local" />
            )}
          </Field>
        )}
        {mode !== 'forgot' && (
          <Field label={mode === 'reset' ? 'New password' : 'Password'}>
            {(p) => (
              <PasswordInput {...p} value={password} onChange={setPassword} placeholder="8+ characters"
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'} />
            )}
          </Field>
        )}
        {(mode === 'signup' || mode === 'reset') && (
          <Field label="Confirm password"
            hint={confirm.length > 0 && confirm !== password
              ? <span style={{ color: 'var(--warn)' }}>Passwords don&rsquo;t match yet.</span>
              : undefined}>
            {(p) => (
              <PasswordInput {...p} value={confirm} onChange={setConfirm} placeholder="same password again"
                autoComplete="new-password" />
            )}
          </Field>
        )}

        {error && <div className="auth-notice err" role="alert">{error}</div>}
        {notice && <div className="auth-notice ok" role="status">{notice}</div>}

        <button type="submit" className="btn primary" style={{ width: '100%' }}
          disabled={busy || (mode === 'forgot' && sent)}>
          {busy ? (
            <>
              <Spinner size={12} />
              {mode === 'signup' ? 'Creating account…'
                : mode === 'forgot' ? 'Sending…'
                : mode === 'reset' ? 'Setting password…'
                : 'Signing in…'}
            </>
          ) : mode === 'signup' ? <><UserPlus size={14} /> Create account</>
            : mode === 'forgot' ? <><MailQuestion size={14} /> {sent ? 'Reset link sent' : 'Send reset link'}</>
            : mode === 'reset' ? <><KeyRound size={14} /> Set new password</>
            : <><LogIn size={14} /> Sign in</>}
        </button>
      </form>

      {mode === 'login' && (
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <button type="button" className="btn ghost sm" onClick={() => switchMode('forgot')}>
            Forgot password?
          </button>
        </div>
      )}
      {(mode === 'forgot' || mode === 'reset') && (
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <button type="button" className="btn ghost sm" onClick={() => switchMode('login')}>
            <ArrowLeft size={12} /> Back to sign in
          </button>
        </div>
      )}
    </AuthShell>
  );
}
