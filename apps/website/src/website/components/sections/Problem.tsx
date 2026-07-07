import { useScrollReveal } from '@/hooks/useScrollReveal';
import SectionRule from '@/components/SectionRule';
import './Problem.css';

const TOOLS = [
  'Docker', 'Kubernetes', 'Grafana', 'Prometheus', 'Jenkins',
  'NetBird', 'Tailscale', 'Portainer', 'Traefik', 'SSH',
  'Ansible', 'MinIO', 'Nagios', 'Zabbix', 'HAProxy',
];

const QUESTIONS = [
  'WHICH GPU IS FREE?',
  'WHICH SERVER IS OVERLOADED?',
  'WHICH MACHINE IS OVERHEATING?',
  'WHERE IS EACH WORKLOAD RUNNING?',
  'HOW MUCH STORAGE IS LEFT?',
  "WHAT'S ABOUT TO FAIL?",
];

const LEDGER = [
  { label: 'SINGLE-SERVER TOOLS', verdict: 'Easy to start. Stops at one machine.' },
  { label: 'KUBERNETES', verdict: 'Powerful. Needs a team that speaks it.' },
  { label: 'CLOUD DASHBOARDS', verdict: "Convenient. Someone else's computer." },
  { label: 'MONITORING STACKS', verdict: "Show you the fire. Don't put it out." },
  { label: 'ENTERPRISE SUITES', verdict: 'Capable. Heavy, slow, and priced like it.' },
];

function MarqueeRow({ reverse = false }: { reverse?: boolean }) {
  return (
    <div className={`problem-marquee-row ${reverse ? 'reverse' : ''}`} aria-hidden="true">
      <div className="problem-marquee-track">
        {[...QUESTIONS, ...QUESTIONS].map((q, i) => (
          <span key={i} className="problem-question">
            {q} <span className="problem-question-sep">·</span>
          </span>
        ))}
      </div>
    </div>
  );
}

export default function Problem() {
  const sectionRef = useScrollReveal<HTMLElement>(0.1);

  return (
    <section className="section problem" id="problem" ref={sectionRef}>
      <div className="container">
        <SectionRule index="01" name="Problem" />
        <h2 className="reveal">Twenty tabs is not a control plane.</h2>
        <p className="section-body reveal">
          Docker here, Kubernetes there. Grafana for graphs, Jenkins for builds, Tailscale
          for tunnels, SSH for everything else. Every tool solves one problem — and hands
          you three new ones.
        </p>
        <p className="problem-tools mono-label reveal">
          {TOOLS.join('  /  ')}
        </p>
      </div>

      <div className="problem-marquee reveal">
        <MarqueeRow />
        <MarqueeRow reverse />
      </div>

      <div className="container">
        <dl className="problem-ledger">
          {LEDGER.map((row, i) => (
            <div
              key={row.label}
              className="problem-ledger-row reveal"
              style={{ transitionDelay: `${i * 70}ms` }}
            >
              <dt className="mono-label">{row.label}</dt>
              <dd>{row.verdict}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}
