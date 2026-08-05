import { BrandMark } from './ui';

// AuthShell: the one signed-out layout. Sign-in, password reset, and
// invitations all render inside this card, so the first screen a person ever
// sees carries the exact brand mark they'll find in the sidebar afterwards.
export function AuthShell({ subtitle, children, footer }: {
  subtitle?: React.ReactNode;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  return (
    <div className="auth">
      <main className="auth-card">
        <div className="auth-brand">
          <div className="brand-mark" aria-hidden><BrandMark /></div>
          <h1 className="auth-title">RackOps</h1>
        </div>
        {subtitle && <p className="auth-sub">{subtitle}</p>}
        {children}
        {footer && <div className="auth-foot">{footer}</div>}
      </main>
    </div>
  );
}
