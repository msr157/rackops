import { useScrollReveal } from '@/hooks/useScrollReveal';
import SectionRule from '@/components/SectionRule';
import './Editions.css';

const EDITIONS = [
  {
    name: 'RackOps Lite',
    tagline: 'HOME LABS · DEVELOPERS · MAKERS · SMALL BUSINESS',
    line: 'Runs behind CGNAT — no static IP, no port forwarding.',
    features: [
      'One-click deploys',
      'Secure remote connectivity',
      'Hardware monitoring',
      'Storage visualization',
      'Logs & alerts',
      'AI assistance',
      'Simple backups',
    ],
    flagship: false,
  },
  {
    name: 'RackOps Enterprise',
    tagline: 'PRODUCTION ORGS · FLEETS · COMPLIANCE',
    line: 'For teams running infrastructure that other people depend on.',
    features: [
      'Multi-server management',
      'Cluster orchestration',
      'RBAC & SSO',
      'GPU scheduling',
      'High availability',
      'Policy management',
      'Audit logs',
      'AI-assisted operations',
      'Private deployments',
    ],
    flagship: true,
  },
];

export default function Editions() {
  const sectionRef = useScrollReveal<HTMLElement>(0.08);

  return (
    <section className="section editions" id="editions" ref={sectionRef}>
      <div className="container">
        <SectionRule index="07" name="Editions" />
        <h2 className="reveal">Start in the garage. Scale to the org chart.</h2>

        <div className="editions-grid">
          {EDITIONS.map((edition, i) => (
            <article
              key={edition.name}
              className={`edition reveal ${edition.flagship ? 'flagship' : ''}`}
              style={{ transitionDelay: `${i * 120}ms` }}
            >
              {edition.flagship && (
                <span className="edition-corner mono-label signal">[ FLAGSHIP ]</span>
              )}
              <h3>{edition.name}</h3>
              <p className="mono-label edition-tagline">{edition.tagline}</p>
              <p className="edition-line">{edition.line}</p>
              <ul className="edition-features">
                {edition.features.map((feature, j) => (
                  <li
                    key={feature}
                    className="reveal"
                    style={{ transitionDelay: `${i * 120 + j * 45}ms` }}
                  >
                    <span className="edition-plus">+</span> {feature}
                  </li>
                ))}
              </ul>
              <a href="#access" className="btn btn-outline">
                Request access
              </a>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
