import { useState } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import SectionRule from '@/components/SectionRule';
import './Migration.css';

interface CloudProviderOption {
  id: 'aws' | 'azure' | 'gcp';
  name: string;
  badge: string;
  shape: string;
  vCPU: number;
  ramGB: number;
  diskType: string;
  diskGB: number;
  computeCost: string;
  diskCost: string;
  transferCost: string;
  monthlyTotal: string;
  downtimeEstimate: string;
}

const PROVIDERS: CloudProviderOption[] = [
  {
    id: 'aws',
    name: 'Amazon Web Services',
    badge: 'AWS EC2',
    shape: 'c6i.xlarge (us-east-1)',
    vCPU: 4,
    ramGB: 8,
    diskType: 'gp3 (3000 IOPS, 125 MB/s)',
    diskGB: 100,
    computeCost: '$122.64',
    diskCost: '$8.00',
    transferCost: '$4.50',
    monthlyTotal: '$135.14',
    downtimeEstimate: '~1m 45s planned window',
  },
  {
    id: 'azure',
    name: 'Microsoft Azure',
    badge: 'Azure VM',
    shape: 'Standard_D4s_v5 (eastus)',
    vCPU: 4,
    ramGB: 16,
    diskType: 'Standard SSD LRS (E10)',
    diskGB: 128,
    computeCost: '$140.16',
    diskCost: '$9.60',
    transferCost: '$4.35',
    monthlyTotal: '$154.11',
    downtimeEstimate: '~2m 10s planned window',
  },
  {
    id: 'gcp',
    name: 'Google Cloud Platform',
    badge: 'GCP Compute',
    shape: 'e2-standard-4 (us-central1-a)',
    vCPU: 4,
    ramGB: 16,
    diskType: 'pd-balanced (zonal)',
    diskGB: 100,
    computeCost: '$97.08',
    diskCost: '$10.00',
    transferCost: '$4.25',
    monthlyTotal: '$111.33',
    downtimeEstimate: '~1m 55s planned window',
  },
];

const STAGES = [
  { id: '01', name: 'Provision', desc: 'Immutable cloud VM launch & agent enrollment' },
  { id: '02', name: 'Quiesce', desc: 'Application writer fencing & stop container writes' },
  { id: '03', name: 'Backup', desc: 'AES-GCM encrypted database dumps & volume archives' },
  { id: '04', name: 'Transfer', desc: 'Resumable chunked HTTPS relay with SHA-256 checks' },
  { id: '05', name: 'Restore', desc: 'Key distribution & private destination database load' },
  { id: '06', name: 'Validate', desc: 'Read-only health verification (SELECT 1 queries)' },
  { id: '07', name: 'Cutover', desc: 'Traefik reverse-proxy & DNS ingress repointing' },
  { id: '08', name: 'Enable Writes', desc: 'Clear read-only mode & activate live writers' },
  { id: '09', name: 'Complete', desc: 'Durable receipt saved & source preserved safely' },
];

export default function Migration() {
  const sectionRef = useScrollReveal<HTMLElement>(0.05);
  const [selectedProvider, setSelectedProvider] = useState<'aws' | 'azure' | 'gcp'>('aws');
  const activePlan = PROVIDERS.find((p) => p.id === selectedProvider) ?? PROVIDERS[0];

  return (
    <section className="section migration-section" id="migration" ref={sectionRef}>
      <div className="container">
        <SectionRule index="05" name="Cloud Migration" />
        <h2 className="reveal">Move to AWS, Azure, or GCP in five steps.</h2>
        <p className="section-body reveal">
          No manual lift-and-shift projects or surprise cloud bills. RackOps sizes instances,
          calculates sourced bandwidth and storage pricing, and migrates quiesced containers and
          databases with guaranteed zero data loss.
        </p>

        <div className="migration-showcase reveal">
          <div className="migration-tabs" role="tablist" aria-label="Target Cloud Provider">
            {PROVIDERS.map((provider) => (
              <button
                key={provider.id}
                type="button"
                role="tab"
                aria-selected={provider.id === selectedProvider}
                className={`migration-tab mono-label ${provider.id === selectedProvider ? 'active' : ''}`}
                onClick={() => setSelectedProvider(provider.id)}
              >
                <span className="migration-tab-dot" />
                {provider.name}
              </button>
            ))}
          </div>

          <div className="migration-card">
            <div className="migration-card-header">
              <div>
                <span className="mono-label signal migration-badge">{activePlan.badge}</span>
                <h3 className="migration-shape-title">{activePlan.shape}</h3>
                <p className="migration-specs mono-label">
                  {activePlan.vCPU} vCPU · {activePlan.ramGB} GB RAM · {activePlan.diskGB} GB {activePlan.diskType}
                </p>
              </div>
              <div className="migration-quote-total">
                <span className="migration-total-label mono-label">Sourced Total Quote</span>
                <span className="migration-total-amount">{activePlan.monthlyTotal}</span>
                <span className="migration-downtime mono-label">{activePlan.downtimeEstimate}</span>
              </div>
            </div>

            <div className="migration-breakdown">
              <div className="breakdown-item">
                <span className="mono-label faint">Compute Subtotal</span>
                <strong>{activePlan.computeCost} <span className="mono-label faint">/mo</span></strong>
              </div>
              <div className="breakdown-item">
                <span className="mono-label faint">Root Disk Storage</span>
                <strong>{activePlan.diskCost} <span className="mono-label faint">/mo</span></strong>
              </div>
              <div className="breakdown-item">
                <span className="mono-label faint">Outbound Transfer Quote</span>
                <strong>{activePlan.transferCost} <span className="mono-label faint">est</span></strong>
              </div>
              <div className="breakdown-item">
                <span className="mono-label faint">Approval Status</span>
                <strong className="signal">Signed & Verified</strong>
              </div>
            </div>

            <div className="migration-pipeline-header">
              <span className="mono-label signal">Workflow Engine</span>
              <span className="mono-label faint">9-Stage Deterministic Cutover Sequence</span>
            </div>

            <div className="migration-pipeline">
              {STAGES.map((stage) => (
                <div key={stage.id} className="pipeline-step">
                  <div className="pipeline-step-head">
                    <span className="pipeline-num mono-label">{stage.id}</span>
                    <span className="pipeline-name">{stage.name}</span>
                  </div>
                  <p className="pipeline-desc">{stage.desc}</p>
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="migration-guarantees reveal">
          <article className="guarantee-card">
            <span className="mono-label signal">01 / SAFETY</span>
            <h3>Preserve Source Hardware</h3>
            <p>
              Your source server and original volumes remain completely intact. If anything unexpected
              occurs during preflight or validation, your on-premise workload un-fences cleanly.
            </p>
          </article>
          <article className="guarantee-card">
            <span className="mono-label signal">02 / ENCRYPTION</span>
            <h3>AES-GCM 256-Bit Protection</h3>
            <p>
              Data in transit and at rest is secured with ephemeral 32-byte keys derived per artifact.
              Keys are released exclusively to the verified enrolled cloud node once download integrity passes.
            </p>
          </article>
          <article className="guarantee-card">
            <span className="mono-label signal">03 / CONSISTENCY</span>
            <h3>Zero Stale Routing Boundary</h3>
            <p>
              The control plane persists destination write permissions before enabling live traffic,
              guaranteeing you never silently route customer updates to stale storage.
            </p>
          </article>
        </div>
      </div>
    </section>
  );
}
