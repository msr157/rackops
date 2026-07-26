import { useEffect, useState } from 'react';
import { Building2, LogIn, UserPlus } from 'lucide-react';
import { api, setToken } from '../api';
import { cleanError } from '../components/toast';
import { Field, PasswordInput } from '../components/ui';

// InvitationPage: the landing for /invitation?token=… copy-links. The invitee
// sets a name and password and lands in the org — no admin round-trip.
export function InvitationPage() {
  const token = new URLSearchParams(window.location.search).get('token') ?? '';
  const [info, setInfo] = useState<{ email: string; org: string; role: string } | null>(null);
  const [invalid, setInvalid] = useState('');
  const [fullName, setFullName] = useState('');
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

  const submit = async () => {
    setError('');
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

  return (
    <div style={{
      position: 'fixed', inset: 0, display: 'grid', placeItems: 'center',
      background: 'var(--bg)', overflow: 'hidden',
    }}>
      <div style={{
        position: 'absolute', inset: 0,
        backgroundImage: 'repeating-linear-gradient(180deg, var(--border) 0 1px, transparent 1px 42px)',
        maskImage: 'radial-gradient(ellipse 65% 60% at 50% 45%, black 25%, transparent 75%)',
        WebkitMaskImage: 'radial-gradient(ellipse 65% 60% at 50% 45%, black 25%, transparent 75%)',
        opacity: 0.6,
      }} />
      <div style={{
        position: 'relative', width: 400, maxWidth: '92vw',
        background: 'rgba(20, 18, 14, 0.78)', backdropFilter: 'blur(20px)',
        border: '1px solid var(--border-hi)', borderRadius: 16, padding: 30,
      }}>
        <div style={{ textAlign: 'center', marginBottom: 22 }}>
          <Building2 size={26} style={{ color: 'var(--amber)', marginBottom: 10 }} />
          <div style={{ fontFamily: 'var(--display)', fontSize: 19, fontWeight: 700 }}>
            {info ? `Join ${info.org}` : 'RackOps invitation'}
          </div>
          {info && (
            <div className="dim" style={{ marginTop: 6 }}>
              You&rsquo;ve been invited as <strong>{info.role}</strong>. Create your account to get started.
            </div>
          )}
        </div>

        {invalid && (
          <>
            <div className="badge red" role="alert" style={{ width: '100%', marginBottom: 14, lineHeight: 1.5 }}>{invalid}</div>
            <a className="btn" style={{ width: '100%' }} href="/"><LogIn size={13} /> Go to sign in</a>
          </>
        )}

        {info && (
          <>
            <Field label="Email">
              {(p) => <input className="input" {...p} value={info.email} disabled />}
            </Field>
            <Field label="Full name">
              {(p) => (
                <input className="input" {...p} value={fullName} placeholder="Your name" autoComplete="name"
                  onChange={(e) => setFullName(e.target.value)} />
              )}
            </Field>
            <Field label="Password">
              {(p) => (
                <PasswordInput {...p} value={password} onChange={setPassword} placeholder="8+ characters"
                  autoComplete="new-password" onEnter={submit} />
              )}
            </Field>
            <Field label="Confirm password"
              hint={confirm.length > 0 && confirm !== password
                ? <span style={{ color: 'var(--warn)' }}>Passwords don&rsquo;t match yet.</span>
                : undefined}>
              {(p) => (
                <PasswordInput {...p} value={confirm} onChange={setConfirm} placeholder="same password again"
                  autoComplete="new-password" onEnter={submit} />
              )}
            </Field>

            {error && <div className="badge red" role="alert" style={{ width: '100%', marginBottom: 12 }}>{error}</div>}

            <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={submit}>
              <UserPlus size={14} /> Create account & join
            </button>
          </>
        )}
      </div>
    </div>
  );
}
