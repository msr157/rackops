import { useState, type FormEvent } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import './CTA.css';

// TODO: swap for POST /waitlist when the API lands.
const CONTACT_EMAIL = 'early@rackops.dev';

export default function CTA() {
  const sectionRef = useScrollReveal<HTMLElement>();
  const [email, setEmail] = useState('');
  const [drafted, setDrafted] = useState(false);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return;
    window.location.href = `mailto:${CONTACT_EMAIL}?subject=${encodeURIComponent(
      'Early access request',
    )}&body=${encodeURIComponent(`Requesting early access for: ${email}`)}`;
    setDrafted(true);
  };

  return (
    <section className="section cta-section" id="access" ref={sectionRef}>
      <div className="container">
        <h2 className="reveal">Get on the list.</h2>
        <p className="section-body reveal">
          RackOps is pre-launch. Early access opens in waves — first come, first racked.
        </p>

        <form className="cta-prompt reveal" onSubmit={handleSubmit}>
          <label className="cta-path mono-label" htmlFor="cta-email">
            ~/rackops $ join --email
          </label>
          {drafted ? (
            <p className="cta-confirm mono-label signal">
              ✓ request drafted in your mail client — hit send
            </p>
          ) : (
            <div className="cta-input-row">
              <input
                id="cta-email"
                type="email"
                required
                placeholder="you@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="email"
              />
              <button type="submit" className="btn btn-solid">
                [ Enter ]
              </button>
            </div>
          )}
        </form>

        <p className="cta-fallback mono-label reveal">
          no mail client? write to {CONTACT_EMAIL}
        </p>
      </div>
    </section>
  );
}
