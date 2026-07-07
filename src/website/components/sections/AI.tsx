import { useEffect, useRef, useState } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import './AI.css';

const PROMPT = '$ rackops ai diagnose --node rackops-vm';
const RESPONSES = [
  { delay: 800, text: '✓ Analyzing thermal sensors on rackops-vm...', type: 'output' },
  { delay: 1600, text: '⚠ GPU temperature elevated: 78°C (threshold: 75°C)', type: 'warn' },
  { delay: 2400, text: '→ Root cause: airflow obstruction in rack slot 3', type: 'output' },
  { delay: 3200, text: '→ Recommended: migrate workload to BetterRoad-VM (42°C, 68% CPU free)', type: 'output' },
  { delay: 4000, text: '✓ Auto-placement available. Run: rackops migrate --workload api-gateway', type: 'success' },
];

export default function AI() {
  const sectionRef = useScrollReveal<HTMLElement>();
  const [visibleLines, setVisibleLines] = useState(0);
  const triggered = useRef(false);

  useEffect(() => {
    const el = sectionRef.current;
    if (!el) return;

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !triggered.current) {
          triggered.current = true;
          RESPONSES.forEach((line, i) => {
            setTimeout(() => setVisibleLines(i + 1), line.delay);
          });
        }
      },
      { threshold: 0.3 },
    );

    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return (
    <section className="ai-section" id="ai" ref={sectionRef}>
      <div className="container ai-grid">
        <div className="ai-content reveal">
          <span className="section-tag">AI Assistant</span>
          <h2>
            Infrastructure intelligence
            <br />
            <span className="gradient-text">built in</span>
          </h2>
          <p>
            RackOps integrates AI as an operational assistant — explaining why deployments
            fail, which server is overheating, and which node is best suited for a workload.
          </p>
          <ul className="ai-examples">
            <li>Why did this deployment fail?</li>
            <li>Which server is best for this workload?</li>
            <li>Which storage device is approaching failure?</li>
            <li>What action should I take before this becomes an outage?</li>
          </ul>
        </div>
        <div className="terminal reveal">
          <div className="terminal-header">
            <span className="dot red" />
            <span className="dot yellow" />
            <span className="dot green" />
            <span className="terminal-title">rackops ai</span>
          </div>
          <div className="terminal-body">
            <div className="terminal-line show">
              <span className="prompt">$</span> {PROMPT}
            </div>
            {RESPONSES.map((line, i) => (
              <div
                key={i}
                className={`terminal-line ${line.type} ${i < visibleLines ? 'show' : ''}`}
              >
                {line.text}
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
