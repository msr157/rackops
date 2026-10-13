import { useEffect, useRef } from 'react';
import { gsap } from '@/lib/gsap';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import SectionRule from '@/components/SectionRule';
import './Solution.css';

const PILLARS = [
  {
    index: 'SEE / 01',
    name: 'Node Agent',
    body: 'A lightweight agent on every machine reads the hardware itself: CPU, GPU, memory, disks, network, power, thermals. Not the app layer — the metal.',
  },
  {
    index: 'CONNECT / 02',
    name: 'Private Mesh & Front Door',
    body: 'Every machine linked over an encrypted WireGuard mesh and high-performance Front Door tunnels. Works behind CGNAT with automated TLS certificates. No port forwarding needed.',
  },
  {
    index: 'RUN / 03',
    name: 'Control Plane & Smart Placement',
    body: "One view of the whole fleet. It doesn't just show where workloads are running — it scores thermal curves and memory headroom to place Docker and Compose apps optimally.",
  },
  {
    index: 'MIGRATE / 04',
    name: 'Multi-Cloud Migration',
    body: 'Move applications and quiesced databases straight to AWS, Azure, or GCP. Sourced pricing, conservative downtime quotes, and a 9-stage zero-data-loss cutover.',
  },
];

const QUOTE = 'Other tools check whether the app is running. RackOps decides where it should run.';

export default function Solution() {
  const sectionRef = useScrollReveal<HTMLElement>(0.1);
  const reducedMotion = useReducedMotion();
  const quoteRef = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    if (reducedMotion) return;

    const ctx = gsap.context(() => {
      const words = quoteRef.current?.querySelectorAll('.solution-quote-word');
      if (!words?.length) return;
      gsap.fromTo(
        words,
        { opacity: 0.18 },
        {
          opacity: 1,
          stagger: 0.06,
          ease: 'none',
          scrollTrigger: {
            trigger: quoteRef.current,
            start: 'top 80%',
            end: 'bottom 45%',
            scrub: 0.5,
          },
        },
      );
    }, sectionRef);

    return () => ctx.revert();
  }, [reducedMotion, sectionRef]);

  return (
    <section className="section solution" id="solution" ref={sectionRef}>
      <div className="container">
        <SectionRule index="03" name="System" />
        <h2 className="reveal">See. Connect. Run. Move to Cloud.</h2>

        <div className="solution-pillars">
          {PILLARS.map((pillar, i) => (
            <article
              key={pillar.name}
              className="solution-pillar reveal"
              style={{ transitionDelay: `${i * 110}ms` }}
            >
              <p className="mono-label signal">{pillar.index}</p>
              <h3>{pillar.name}</h3>
              <p className="solution-pillar-body">{pillar.body}</p>
            </article>
          ))}
        </div>

        <p className="solution-quote" ref={quoteRef}>
          {QUOTE.split(' ').map((word, i) => (
            <span key={i} className="solution-quote-word">
              {word}{' '}
            </span>
          ))}
        </p>
      </div>
    </section>
  );
}
