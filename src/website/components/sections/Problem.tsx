import { useEffect, useRef } from 'react';
import { gsap, ScrollTrigger } from '@/lib/gsap';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import './Problem.css';

const TOOLS = [
  'Docker', 'Kubernetes', 'Grafana', 'Prometheus', 'Jenkins',
  'NetBird', 'Tailscale', 'Portainer', 'Traefik', 'SSH',
  'Monitoring', 'Storage', 'Backups', 'Logs', 'Security',
];

export default function Problem() {
  const sectionRef = useScrollReveal<HTMLElement>(0.15);
  const toolsRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = toolsRef.current;
    if (!el) return;

    const ctx = gsap.context(() => {
      ScrollTrigger.create({
        trigger: el,
        start: 'top 70%',
        end: 'bottom 30%',
        scrub: 1,
        onUpdate: (self) => {
          const progress = self.progress;
          el.querySelectorAll('.tool-chip').forEach((chip, i) => {
            const angle = (i / TOOLS.length) * Math.PI * 2;
            const scatter = (1 - progress) * 80;
            const el = chip as HTMLElement;
            el.style.transform = `translate(${Math.cos(angle) * scatter}px, ${Math.sin(angle) * scatter * 0.5}px) scale(${0.85 + progress * 0.15})`;
            el.style.opacity = String(0.4 + progress * 0.6);
          });
        },
      });
    }, sectionRef);

    return () => ctx.revert();
  }, []);

  return (
    <section className="problem" id="problem" ref={sectionRef}>
      <div className="container">
        <div className="section-header reveal">
          <span className="section-tag">The Problem</span>
          <h2>
            One platform instead of
            <br />
            <span className="gradient-text">twenty tools</span>
          </h2>
          <p>
            Today&apos;s infrastructure is fragmented. Every tool solves one problem,
            with its own interface and learning curve.
          </p>
        </div>
        <div className="problem-visual reveal">
          <div className="tools-cloud" ref={toolsRef}>
            {TOOLS.map((tool) => (
              <span key={tool} className="tool-chip">{tool}</span>
            ))}
            <div className="convergence-core">
              <span className="logo-icon">⬡</span>
              <span>RackOps</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
