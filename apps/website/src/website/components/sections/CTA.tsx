import { useState, type FormEvent } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import type { EarlyAccessRequest } from '@rackops/api';
import './CTA.css';

const CONTACT_EMAIL = 'early@rackops.dev';
const API_URL = 'https://api.rackops.in/api/v1/early-access';

export default function CTA() {
  const sectionRef = useScrollReveal<HTMLElement>();
  const [formData, setFormData] = useState<EarlyAccessRequest>({
    email: '',
    name: '',
    industry: '',
    notes: '',
  });
  const [status, setStatus] = useState<'idle' | 'loading' | 'success' | 'error'>('idle');
  const [errorMessage, setErrorMessage] = useState('');

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(formData.email)) return;
    
    setStatus('loading');
    setErrorMessage('');

    try {
      const res = await fetch(API_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(formData),
      });

      if (!res.ok) {
        if (res.status === 409) {
          throw new Error('This email has already requested early access.');
        }
        throw new Error('Failed to submit request. Please try again later.');
      }

      setStatus('success');
    } catch (err) {
      setStatus('error');
      setErrorMessage(err instanceof Error ? err.message : String(err));
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    setFormData({ ...formData, [e.target.name]: e.target.value });
  };

  return (
    <section className="section cta-section" id="access" ref={sectionRef}>
      <div className="container">
        <h2 className="reveal">Need RackOps Enterprise?</h2>
        <p className="section-body reveal">
          Join early access for multi-server fleets, identity controls, high availability,
          and private deployments.
        </p>

        <form className="cta-prompt reveal" onSubmit={handleSubmit}>
          <label className="cta-path mono-label" htmlFor="cta-email">
            ~/rackops $ join --enterprise
          </label>
          
          {status === 'success' ? (
            <p className="cta-confirm mono-label signal">
              ✓ you're on the list. we'll be in touch soon.
            </p>
          ) : (
            <div className="cta-form-grid">
              <input
                id="cta-name"
                name="name"
                type="text"
                required
                placeholder="Name"
                value={formData.name}
                onChange={handleChange}
                disabled={status === 'loading'}
              />
              <input
                id="cta-email"
                name="email"
                type="email"
                required
                placeholder="Email Address"
                value={formData.email}
                onChange={handleChange}
                autoComplete="email"
                disabled={status === 'loading'}
              />
              <input
                id="cta-industry"
                name="industry"
                type="text"
                required
                placeholder="Industry / Occupation"
                value={formData.industry}
                onChange={handleChange}
                disabled={status === 'loading'}
              />
              <textarea
                id="cta-notes"
                name="notes"
                placeholder="Description or notes (optional)"
                value={formData.notes}
                onChange={handleChange}
                disabled={status === 'loading'}
                rows={2}
              />
              
              {status === 'error' && (
                <p className="cta-error mono-label" style={{ color: 'var(--red, #ff4444)' }}>
                  ✗ {errorMessage}
                </p>
              )}
              
              <button type="submit" className="btn btn-solid cta-submit" disabled={status === 'loading'}>
                {status === 'loading' ? '[ Processing... ]' : '[ Enter ]'}
              </button>
            </div>
          )}
        </form>

        <p className="cta-fallback mono-label reveal">
          questions? write to {CONTACT_EMAIL}
        </p>
      </div>
    </section>
  );
}
