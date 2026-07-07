import { useScrollReveal } from '@/hooks/useScrollReveal';
import './Features.css';

const FEATURES = [
  { title: 'Automated Provisioning', desc: 'Spin up racks and resources in seconds using declarative configs.', tag: 'Zero Touch', featured: false },
  { title: 'Real-Time Health Monitoring', desc: 'Continuous health checks, alerting, and dashboards across your fleet.', tag: 'Live', featured: true },
  { title: 'Secure Mesh Connectivity', desc: 'Connect machines behind CGNAT without VPN setup or port forwarding.', tag: 'NetBird', featured: false },
  { title: 'Intelligent Placement', desc: 'Auto-select the best server based on compute, storage, and health.', tag: 'AI-Powered', featured: false },
  { title: 'Integrated Tooling', desc: 'Deploy MinIO, Gitea, Jenkins, Prometheus — RackOps manages the infrastructure.', tag: 'Open', featured: false },
  { title: 'Observability & Logs', desc: 'Structured logging, metrics export, and full audit trails.', tag: 'Insights', featured: false },
];

const LITE_FEATURES = [
  'One-click deployment', 'Secure remote connectivity', 'Hardware monitoring',
  'Storage visualization', 'Logs & alerts', 'AI-powered assistance', 'Simple backups',
];

const ENTERPRISE_FEATURES = [
  'Multi-server management', 'Cluster orchestration', 'RBAC & SSO',
  'GPU scheduling', 'High availability', 'Policy management',
  'Audit logs', 'Enterprise monitoring', 'Private deployments',
];

export default function Features() {
  const sectionRef = useScrollReveal<HTMLElement>();

  return (
    <section className="features" id="features" ref={sectionRef}>
      <div className="container">
        <div className="section-header reveal">
          <span className="section-tag">Features</span>
          <h2>
            Built for production
            <br />
            <span className="gradient-text">from day one</span>
          </h2>
          <p>Every capability designed with reliability, scalability, and developer experience in mind.</p>
        </div>

        <div className="features-grid">
          {FEATURES.map((f, i) => (
            <div
              key={f.title}
              className={`feature-card reveal ${f.featured ? 'featured' : ''}`}
              style={{ transitionDelay: `${i * 80}ms` }}
            >
              <h3>{f.title}</h3>
              <p>{f.desc}</p>
              <div className="feature-tag">{f.tag}</div>
            </div>
          ))}
        </div>

        <div className="tiers reveal">
          <div className="tier-card glass-card">
            <span className="tier-badge">Lite</span>
            <h3>RackOps Lite</h3>
            <p className="tier-audience">Home labs · Developers · Makers · Small businesses</p>
            <ul>
              {LITE_FEATURES.map((f) => (
                <li key={f}>{f}</li>
              ))}
            </ul>
          </div>
          <div className="tier-card glass-card tier-enterprise">
            <span className="tier-badge">Enterprise</span>
            <h3>RackOps Enterprise</h3>
            <p className="tier-audience">Organizations operating production infrastructure at scale</p>
            <ul>
              {ENTERPRISE_FEATURES.map((f) => (
                <li key={f}>{f}</li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}
