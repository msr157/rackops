import { useState } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import './CTA.css';

const COMMAND = 'git clone https://github.com/your-org/rackops.git\ncd rackops && git push origin main';

export default function CTA() {
  const sectionRef = useScrollReveal<HTMLElement>();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(COMMAND);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard unavailable */
    }
  };

  return (
    <section className="cta-section" id="cta" ref={sectionRef}>
      <div className="container">
        <div className="cta-card reveal">
          <div className="cta-glow" />
          <div className="cta-content">
            <h2>Start with one machine.<br />Scale to a fleet.</h2>
            <p>
              Clone the repo, configure your secrets, and push to main.
              <br />
              Your infrastructure will take it from there.
            </p>
            <div className="cta-code">
              <code>
                git clone https://github.com/your-org/rackops.git
                <br />
                cd rackops &amp;&amp; git push origin main
              </code>
              <button
                className="copy-btn"
                aria-label="Copy command"
                onClick={handleCopy}
              >
                {copied ? (
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                    <path d="M5 13l4 4L19 7" />
                  </svg>
                ) : (
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                    <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
                  </svg>
                )}
              </button>
            </div>
            <a href="#features" className="btn btn-primary btn-large">Get Started</a>
          </div>
        </div>
      </div>
    </section>
  );
}
