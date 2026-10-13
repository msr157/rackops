import { useEffect, useRef, useState } from 'react';
import { gsap, ScrollTrigger } from '@/lib/gsap';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import SectionRule from '@/components/SectionRule';
import './Platform.css';

const LAYERS = [
  { id: 'L1', name: 'Hardware Discovery & Telemetry', items: ['CPU CORES', 'RAM', 'NVME/SSD/HDD', 'GPU ACCELERATORS', 'THERMALS', 'POWER', 'NETWORK'] },
  { id: 'L2', name: 'Fleet & Multi-Tenancy', items: ['ORGANIZATIONS', 'PROJECTS', 'RBAC ROLES', 'SESSION REVOCATION', 'OFFLINE RECOVERY'] },
  { id: 'L3', name: 'Real-Time Observability', items: ['METRICS', 'CONTAINER LOGS', 'LIVE SSE STREAMS', 'ALERTS', 'HARDWARE SENSORS'] },
  { id: 'L4', name: 'Workloads & Placement', items: ['DOCKER', 'COMPOSE', 'SMART PLACEMENT', 'AUTONOMOUS REPLACEMENT', 'PORT SUGGESTION'] },
  { id: 'L5', name: 'Encrypted Storage & Backups', items: ['AES-GCM 256', 'POSTGRESQL', 'MYSQL', 'NAMED VOLUMES', 'DISASTER RECOVERY'] },
  { id: 'L6', name: 'Private Mesh & Front Door', items: ['WIREGUARD MESH', 'CGNAT BYPASS', 'AUTO HTTPS', 'QUIC+TCP TUNNELS'] },
  { id: 'L7', name: 'Cloud Migration & Sizing', items: ['AWS EC2', 'AZURE VM', 'GCP COMPUTE', 'TRANSFER PRICING', 'ZERO-LOSS CUTOVER'] },
];

export default function Platform() {
  const sectionRef = useScrollReveal<HTMLElement>(0.05);
  const stageRef = useRef<HTMLDivElement>(null);
  const progressRef = useRef<HTMLDivElement>(null);
  const reducedMotion = useReducedMotion();
  const [active, setActive] = useState(0);

  useEffect(() => {
    if (reducedMotion) return;

    const mm = gsap.matchMedia();
    mm.add('(min-width: 900px)', () => {
      const st = ScrollTrigger.create({
        trigger: stageRef.current,
        start: 'top top',
        end: '+=2400',
        pin: true,
        onUpdate: (self) => {
          const idx = Math.min(LAYERS.length - 1, Math.floor(self.progress * LAYERS.length));
          setActive((prev) => (prev === idx ? prev : idx));
          if (progressRef.current) {
            progressRef.current.style.transform = `scaleY(${self.progress})`;
          }
        },
      });
      return () => st.kill();
    });

    return () => mm.revert();
  }, [reducedMotion]);

  const layer = LAYERS[active];

  return (
    <section className="section platform" id="platform" ref={sectionRef}>
      <div className="container">
        <SectionRule index="04" name="Platform" />
        <h2 className="reveal">Seven layers. One system.</h2>
        <p className="section-body reveal">
          Kubernetes is powerful, but it needs specialists. RackOps is built for teams that
          need infrastructure control without building an infrastructure team.
        </p>
      </div>

      <div className="platform-stage" ref={stageRef}>
        <div className="container platform-grid">
          <div className="platform-progress" aria-hidden="true">
            <div className="platform-progress-fill" ref={progressRef} />
          </div>

          <ol className="platform-list">
            {LAYERS.map((l, i) => (
              <li
                key={l.id}
                className={`platform-row ${i === active ? 'active' : ''}`}
                aria-current={i === active ? 'step' : undefined}
              >
                <span className="platform-row-id mono-label">{l.id}</span>
                <span className="platform-row-name">{l.name}</span>
                {/* inline readout — visible on mobile / reduced-motion flat layout */}
                <span className="platform-row-items mono-label">{l.items.join(' · ')}</span>
              </li>
            ))}
          </ol>

          <div className="platform-detail" aria-hidden="true">
            <p className="mono-label signal" key={`id-${layer.id}`}>
              {layer.id} / 07
            </p>
            <h3 key={`name-${layer.id}`}>{layer.name}</h3>
            <ul className="platform-chips" key={`items-${layer.id}`}>
              {layer.items.map((item) => (
                <li key={item} className="mono-label">
                  {item}
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}
