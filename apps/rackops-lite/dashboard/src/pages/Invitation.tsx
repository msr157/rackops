import { useEffect, useState } from 'react';
import { LogIn, UserPlus } from 'lucide-react';
import { api, setToken } from '../api';
import { AuthShell } from '../components/AuthShell';
import { cleanError } from '../components/toast';
import { Field, PasswordInput, SkeletonRows, Spinner } from '../components/ui';

// InvitationPage: the landing for /invitation?token=… copy-links. The invitee
// sets a name and password and lands in the org — no admin round-trip.
export function InvitationPage() {
  const token = new URLSearchParams(window.location.search).get('token') ?? '';
  const [info, setInfo] = useState<{ email: string; org: string; role: string } | null>(null);
  const [invalid, setInvalid] = useState('');
  const [fullName, setFullName] = useState('');
  const [nameError, setNameError] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!token) { setInvalid('This invitation link is incomplete — ask for a new one.'); return; }
    api.invitationInfo(token)
      .then(setInfo)
      .catch((e) => setInvalid(cleanError(e)));
  }, [token]);

  // Still waiting on the invitation lookup: show skeletons, never a blank
  // card that reads as a dead link.
  const loading = !invalid && !info;

  const submit = async () => {
    setError('');
    if (!fullName.trim()) { setNameError('Enter your full name.'); return; }
    if (password.length < 8) { setError('Password must be at least 8 characters.'); return; }
    if (password !== confirm) { setError('Passwords do not match.'); return; }
    setBusy(true);
    try {
      const r = await api.acceptInvitation(token, fullName, password);
      setToken(r.token);
      window.location.href = '/'; // fresh boot into the authed shell
    } catch (e) {
      setError(cleanError(e));
      setBusy(false);
    }
  };

  // "admin" is a database value; "Admin" is a role in a sentence.
  const role = info ? info.role.charAt(0).toUpperCase() + info.role.slice(1) : '';

  return (
    <AuthShell
      subtitle={invalid ? undefined
        : info ? <>Join <strong>{info.org}</strong></>
        : 'Checking your invitation…'}>
      {invalid && (
        <>
          <div className="auth-notice err" role="alert">{invalid}</div>
          <a className="btn" style={{ width: '100%' }} href="/"><LogIn size={13} /> Go to sign in</a>
        </>
      )}

      {loading && <SkeletonRows count={4} height={58} />}

      {info && (
        <>
          <p className="dim" style={{ marginBottom: 16, lineHeight: 1.6 }}>
            You&rsquo;ve been invited as <strong>{role}</strong>. Create your account to get started.
          </p>

          {/* A real <form>, so Enter submits from any field. */}
          <form noValidate onSubmit={(e) => { e.preventDefault(); if (!busy) submit(); }}>
            <Field label="Email" hint="Set by your invitation.">
              {(p) => <input className="input" {...p} value={info.email} disabled />}
            </Field>
            <Field label="Full name" error={nameError || undefined}>
              {(p) => (
                <input className="input" {...p} value={fullName} placeholder="Your name" autoComplete="name"
                  onChange={(e) => { setFullName(e.target.value); if (nameError) setNameError(''); }} />
              )}
            </Field>
            <Field label="Password">
              {(p) => (
                <PasswordInput {...p} value={password} onChange={setPassword} placeholder="8+ characters"
                  autoComplete="new-password" />
              )}
            </Field>
            <Field label="Confirm password"
              hint={confirm.length > 0 && confirm !== password
                ? <span style={{ color: 'var(--warn)' }}>Passwords don&rsquo;t match yet.</span>
                : undefined}>
              {(p) => (
                <PasswordInput {...p} value={confirm} onChange={setConfirm} placeholder="same password again"
                  autoComplete="new-password" />
              )}
            </Field>

            {error && <div className="auth-notice err" role="alert">{error}</div>}

            <button type="submit" className="btn primary" style={{ width: '100%' }} disabled={busy}>
              {busy ? <><Spinner size={12} /> Joining…</> : <><UserPlus size={14} /> Create account &amp; join</>}
            </button>
          </form>
        </>
      )}
    </AuthShell>
  );
}
