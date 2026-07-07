import { useScrollReveal } from '@/hooks/useScrollReveal';
import './Solution.css';

const HARDWARE = [
  { id: 'cpu', label: 'CPUs', value: '12 cores · 24 threads', status: 'healthy' },
  { id: 'gpu', label: 'GPUs', value: 'RTX 4090 · 24GB VRAM', status: 'healthy' },
  { id: 'npu', label: 'NPUs', value: 'Intel AI Boost', status: 'idle' },
  { id: 'mem', label: 'Memory', value: '64 GB · 78% free', status: 'healthy' },
  { id: 'storage', label: 'Storage', value: '2.4 TB NVMe · 61% used', status: 'warn' },
  { id: 'thermal', label: 'Thermals', value: '42°C · Optimal', status: 'healthy' },
];

export default function Solution() {
  const sectionRef = useScrollReveal<HTMLElement>();

  return (
    <section className="solution" id="solution" ref={sectionRef}>
      <div className="container solution-grid">
        <div className="solution-content reveal">
          <span className="section-tag">Hardware-Aware</span>
          <h2>
            Infrastructure that
            <br />
            <span className="gradient-text">understands itself</span>
          </h2>
          <p>
            Most platforms understand applications. RackOps understands the hardware
            running them — continuously discovering CPUs, GPUs, NPUs, memory, storage,
            network, power, and thermals across your entire fleet.
          </p>
          <p>
            Intelligent workload placement evaluates available resources and automatically
            selects the most suitable machine. Infrastructure decisions become automatic,
            not manual.
          </p>
        </div>
        <div className="solution-panel glass-card reveal">
          <div className="panel-header">
            <span className="panel-dot" />
            <span>Live Hardware Discovery</span>
          </div>
          <div className="hardware-grid">
            {HARDWARE.map((item) => (
              <div key={item.id} className={`hardware-module ${item.status}`}>
                <div className="module-indicator" />
                <div className="module-info">
                  <span className="module-label">{item.label}</span>
                  <span className="module-value">{item.value}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
