import { useScrollReveal } from '@/hooks/useScrollReveal';
import SectionRule from '@/components/SectionRule';
import './Audience.css';

const OPERATORS = [
  {
    id: 'A1',
    name: 'AI Builders & GPU Labs',
    line: "Pool every GPU you own. Schedule training like it's one machine.",
  },
  {
    id: 'A2',
    name: 'Startups With Their Own Servers',
    line: 'Cloud bills said buy. RackOps makes it run.',
  },
  {
    id: 'A3',
    name: 'Government & Regulated Orgs',
    line: "Data that can't leave the building, tooling that doesn't need to.",
  },
  {
    id: 'A4',
    name: 'Managed Service Providers',
    line: 'Every client fleet in one pane. Multi-tenant by design.',
  },
  {
    id: 'A5',
    name: 'Private Cloud Teams',
    line: 'Cloud ergonomics on hardware you control.',
  },
  {
    id: 'A6',
    name: 'Edge Operators',
    line: 'Hundreds of small boxes in odd places, managed like one rack.',
  },
];

export default function Audience() {
  const sectionRef = useScrollReveal<HTMLElement>(0.08);

  return (
    <section className="section audience" id="audience" ref={sectionRef}>
      <div className="container">
        <SectionRule index="07" name="Operators" />
        <h2 className="reveal">Built for people who own their machines.</h2>

        <div className="audience-grid">
          {OPERATORS.map((op, i) => (
            <article
              key={op.id}
              className="audience-cell reveal"
              style={{ transitionDelay: `${(i % 3) * 80 + Math.floor(i / 3) * 120}ms` }}
            >
              <p className="mono-label audience-index">{op.id}</p>
              <h3>{op.name}</h3>
              <p className="audience-line">{op.line}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
