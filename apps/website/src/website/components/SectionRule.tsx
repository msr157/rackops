interface SectionRuleProps {
  index: string;
  name: string;
}

export default function SectionRule({ index, name }: SectionRuleProps) {
  return (
    <div className="section-rule" aria-hidden="true">
      <span className="mono-label signal">{index}</span>
      <hr />
      <span className="mono-label">{name}</span>
    </div>
  );
}
