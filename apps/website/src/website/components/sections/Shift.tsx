import { useEffect, useRef } from 'react';
import { gsap } from '@/lib/gsap';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import SectionRule from '@/components/SectionRule';
import './Shift.css';

const STATS = [
  {
    value: 79,
    caption: 'of enterprises have moved at least one AI workload back from the public cloud.',
  },
  {
    value: 73,
    caption: 'plan further on-prem or hybrid moves in the next two years.',
  },
];

export default function Shift() {
  const sectionRef = useScrollReveal<HTMLElement>(0.1);
  const reducedMotion = useReducedMotion();
  const countRefs = useRef<(HTMLSpanElement | null)[]>([]);

  useEffect(() => {
    if (reducedMotion) return;

    const ctx = gsap.context(() => {
      countRefs.current.forEach((el, i) => {
        if (!el) return;
        const counter = { val: 0 };
        gsap.to(counter, {
          val: STATS[i].value,
          duration: 1.4,
          ease: 'power2.out',
          snap: { val: 1 },
          scrollTrigger: { trigger: el, start: 'top 78%', once: true },
          onUpdate: () => {
            el.textContent = String(Math.round(counter.val));
          },
        });
      });
    }, sectionRef);

    return () => ctx.revert();
  }, [reducedMotion, sectionRef]);

  return (
    <section className="section shift" id="shift" ref={sectionRef}>
      <div className="container">
        <SectionRule index="02" name="Shift" />
        <h2 className="reveal">The hardware is coming home.</h2>

        <div className="shift-stats">
          {STATS.map((stat, i) => (
            <div key={stat.value} className="shift-stat reveal" style={{ transitionDelay: `${i * 100}ms` }}>
              <p className="shift-number">
                <span
                  ref={(el) => {
                    countRefs.current[i] = el;
                  }}
                >
                  {reducedMotion ? stat.value : 0}
                </span>
                %
              </p>
              <p className="shift-caption">{stat.caption}</p>
            </div>
          ))}
        </div>

        <p className="shift-kicker reveal">
          The racks are coming back. The tooling to run them never arrived.
          <br />
          <span className="shift-kicker-signal">That's the gap RackOps fills.</span>
        </p>
      </div>
    </section>
  );
}
