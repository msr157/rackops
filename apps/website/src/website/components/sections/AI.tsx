import { useEffect, useRef, useState } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import SectionRule from '@/components/SectionRule';
import './AI.css';

const PROMPT = 'rackops ai diagnose --node gpu-node-04';
const RESPONSES = [
  { delay: 700, text: '· reading thermal sensors on gpu-node-04', type: 'output' },
  { delay: 1500, text: '! GPU temp 84°C — 9° above threshold', type: 'warn' },
  { delay: 2300, text: '· cause: fan 2 degraded, airflow -40%', type: 'output' },
  { delay: 3100, text: '· candidate: edge-02 — 41°C, 3.1 TB free, GPU idle', type: 'output' },
  { delay: 3900, text: '✓ plan ready: rackops migrate --workload inference-api --to edge-02', type: 'success' },
];

const QUESTIONS = [
  'Why did this deployment fail?',
  'Which machine is overheating?',
  'Which node should run this workload?',
  'Which disk is about to fail?',
  'What do I do before this becomes an outage?',
];

export default function AI() {
  const sectionRef = useScrollReveal<HTMLElement>();
  const reducedMotion = useReducedMotion();
  const [visibleLines, setVisibleLines] = useState(0);
  const triggered = useRef(false);

  useEffect(() => {
    if (reducedMotion) {
      setVisibleLines(RESPONSES.length);
      return;
    }

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
  }, [reducedMotion, sectionRef]);

  return (
    <section className="section ai-section" id="ai" ref={sectionRef}>
      <div className="container">
        <SectionRule index="05" name="Intelligence" />
        <div className="ai-grid">
          <div className="ai-content">
            <h2 className="reveal">Ops that explain themselves.</h2>
            <p className="section-body reveal">
              RackOps ships with an assistant that reads the same telemetry you do — and
              answers the questions that used to cost you an afternoon.
            </p>
            <ul className="ai-examples reveal">
              {QUESTIONS.map((q) => (
                <li key={q}>
                  <span className="ai-examples-prefix">&gt;</span> {q}
                </li>
              ))}
            </ul>
          </div>

          <div className="terminal reveal">
            <div className="terminal-header">
              <span className="terminal-title mono-label">rackops-ai — tty0</span>
              <span className="terminal-led" />
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
                  {i === visibleLines - 1 && <span className="terminal-cursor" aria-hidden="true" />}
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
