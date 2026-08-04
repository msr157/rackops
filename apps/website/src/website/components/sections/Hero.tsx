import { useEffect, useRef } from 'react';
import { gsap } from '@/lib/gsap';
import RackField from '@/three/RackField';
import './Hero.css';

const TICKER_ITEMS = [
  'NODES:ONLINE 14/14',
  'GPU:POOL 82% ALLOC',
  'MESH:P2P ESTABLISHED',
  'THERMAL:NOMINAL',
  'STORAGE:REPLICATED 3×',
  'WORKLOADS:PLACED AUTO',
];

export default function Hero() {
  const sectionRef = useRef<HTMLElement>(null);

  useEffect(() => {
    const ctx = gsap.context(() => {
      gsap.from('.hero-line-inner', {
        yPercent: 110,
        duration: 1,
        stagger: 0.12,
        ease: 'power4.out',
        delay: 0.15,
      });
      gsap.from('.hero-reveal', {
        y: 24,
        opacity: 0,
        duration: 0.8,
        stagger: 0.1,
        ease: 'power2.out',
        delay: 0.7,
      });
    }, sectionRef);

    return () => ctx.revert();
  }, []);

  return (
    <section className="hero" id="hero" ref={sectionRef}>
      <RackField />
      <div className="container hero-layout">
        <p className="hero-status mono-label" aria-label="Status">
          <span className="hero-status-dot" />
          SYS:ONLINE / RACKOPS LITE AVAILABLE NOW
        </p>
        <h1 className="hero-title">
          <span className="hero-line">
            <span className="hero-line-inner">Your servers run your work.</span>
          </span>
          <span className="hero-line">
            <span className="hero-line-inner">
              Who runs your servers?
              <span className="hero-cursor" aria-hidden="true" />
            </span>
          </span>
        </h1>
        <p className="hero-subtitle hero-reveal">
          See every machine, deploy workloads, and manage storage and networking from one
          control plane. RackOps Lite is available now.
        </p>
        <div className="hero-actions hero-reveal">
          <a href="#install-lite" className="btn btn-solid">
            Install RackOps Lite
          </a>
          <a href="#platform" className="btn btn-outline">
            See the platform
          </a>
        </div>
      </div>
      <div className="hero-ticker hero-reveal" aria-hidden="true">
        <div className="hero-ticker-track">
          {[...TICKER_ITEMS, ...TICKER_ITEMS].map((item, i) => (
            <span key={i} className="hero-ticker-item mono-label">
              {item}
            </span>
          ))}
        </div>
      </div>
    </section>
  );
}
