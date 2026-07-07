import { useEffect, useRef } from 'react';
import { gsap } from '@/lib/gsap';
import SceneCanvas from '@/three/SceneCanvas';
import './Hero.css';

const STATS = [
  { value: '99.9%', label: 'Uptime SLA' },
  { value: '< 30s', label: 'Deploy Time' },
  { value: 'Zero', label: 'Downtime Deploys' },
];

export default function Hero() {
  const sectionRef = useRef<HTMLElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    const ctx = gsap.context(() => {
      if (titleRef.current) {
        gsap.from(titleRef.current.querySelectorAll('.hero-line'), {
          y: 60,
          opacity: 0,
          duration: 1,
          stagger: 0.15,
          ease: 'power3.out',
          delay: 0.2,
        });
      }
      gsap.from('.hero-reveal', {
        y: 30,
        opacity: 0,
        duration: 0.8,
        stagger: 0.1,
        ease: 'power2.out',
        delay: 0.6,
      });
    }, sectionRef);

    return () => ctx.revert();
  }, []);

  return (
    <section className="hero" id="hero" ref={sectionRef}>
      <SceneCanvas />
      <div className="hero-bg">
        <div className="grid-overlay" />
        <div className="glow glow-1" />
        <div className="glow glow-2" />
      </div>
      <div className="container hero-layout">
        <div className="hero-content">
          <div className="badge hero-reveal" aria-label="Status">
            <span className="badge-dot" />
            The Infrastructure Operating System
          </div>
          <h1 className="hero-title" ref={titleRef}>
            <span className="hero-line">One Dashboard.</span>
            <span className="hero-line gradient-text">One Control Plane.</span>
            <span className="hero-line">One Platform.</span>
          </h1>
          <p className="hero-subtitle hero-reveal">
            RackOps unifies servers, clusters, networking, and workloads into a single
            intelligent control plane — so infrastructure feels approachable, not overwhelming.
          </p>
          <div className="hero-actions hero-reveal">
            <a href="#architecture" className="btn btn-primary">Explore the Platform</a>
            <a href="#features" className="btn btn-ghost">See RackOps Lite</a>
          </div>
          <div className="hero-stats hero-reveal">
            {STATS.map((stat, i) => (
              <div key={stat.label} className="hero-stat-group">
                {i > 0 && <div className="stat-divider" />}
                <div className="stat">
                  <span className="stat-value">{stat.value}</span>
                  <span className="stat-label">{stat.label}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
      <div className="scroll-indicator" aria-hidden="true">
        <div className="scroll-line" />
      </div>
    </section>
  );
}
