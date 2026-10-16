import { useCallback, useEffect, useState } from 'react';
import {
  Cloud,
  CheckCircle2,
  AlertTriangle,
  ArrowRight,
  ArrowLeft,
  RotateCw,
  ShieldCheck,
  Clock,
  Check,
  HardDrive
} from 'lucide-react';
import {
  api,
  type CloudConnection,
  type CloudReviewPlan,
  type MigrationJobProgress,
  type MigrationReview,
  type AWSMachineSpec,
  type AzureMachineSpec,
  type GCPMachineSpec
} from '../api';
import { useData } from '../data';
import { cleanError } from './toast';
import { Modal, Spinner } from './ui';

interface MigrationWizardProps {
  onClose: () => void;
  preselectedServiceId?: string;
}

const STAGES = [
  { id: 'provision', label: '1. Provision VM', desc: 'Launch target cloud instance and enroll agent' },
  { id: 'quiesce', label: '2. Quiesce', desc: 'Stop source container and fence writes' },
  { id: 'backup', label: '3. Encrypted Backup', desc: 'Create verified AES-GCM database / volume archive' },
  { id: 'transfer', label: '4. Secure Transfer', desc: 'Resumable encrypted relay to destination node' },
  { id: 'restore', label: '5. Restore', desc: 'Decrypt backup keys and import into private destination' },
  { id: 'validate', label: '6. Validate', desc: 'Read-only health check (SELECT 1 & volume verification)' },
  { id: 'cutover', label: '7. Cutover', desc: 'Repoint ingress routing to destination container' },
  { id: 'enable_writes', label: '8. Enable Writes', desc: 'Unfence database and permit production writes' },
  { id: 'complete', label: '9. Complete', desc: 'Finalized with immutable migration receipt' },
];

export function MigrationWizard({ onClose, preselectedServiceId }: MigrationWizardProps) {
  const { services } = useData();
  const [step, setStep] = useState<1 | 2 | 3 | 4 | 5>(1);
  const [selectedServiceIds, setSelectedServiceIds] = useState<string[]>(() =>
    preselectedServiceId ? [preselectedServiceId] : []
  );

  // Cloud machine state
  const [connections, setConnections] = useState<CloudConnection[]>([]);
  const [selectedConnId, setSelectedConnId] = useState<string>('');
  const [provider, setProvider] = useState<'aws' | 'azure' | 'gcp'>('aws');
  const [region, setRegion] = useState('us-east-1');
  const [size, setSize] = useState('t3.medium');
  const [diskGb, setDiskGb] = useState(30);

  // Preflight and review state
  const [preflights, setPreflights] = useState<Record<string, MigrationReview>>({});
  const [activePlan, setActivePlan] = useState<CloudReviewPlan | null>(null);
  const [activeJob, setActiveJob] = useState<MigrationJobProgress | null>(null);

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  // Fetch connections on mount
  useEffect(() => {
    let alive = true;
    api.cloudConnections()
      .then(conns => {
        if (!alive) return;
        setConnections(conns);
        if (conns.length > 0) {
          setSelectedConnId(conns[0].id);
          setProvider(conns[0].provider as any);
          setRegion(conns[0].region);
        }
      })
      .catch(err => { if (alive) setError(cleanError(err)); });
    return () => { alive = false; };
  }, []);

  // Update default machine size when provider changes
  useEffect(() => {
    if (provider === 'aws') { setSize('t3.medium'); setRegion((r) => r || 'us-east-1'); }
    else if (provider === 'azure') { setSize('Standard_D2s_v5'); setRegion((r) => r || 'eastus'); }
    else if (provider === 'gcp') { setSize('e2-standard-2'); setRegion((r) => r || 'us-central1'); }
  }, [provider]);

  // Run preflights when services or target changes
  const runPreflights = useCallback(async () => {
    if (selectedServiceIds.length === 0) return;
    setBusy(true); setError('');
    const results: Record<string, MigrationReview> = {};
    try {
      for (const id of selectedServiceIds) {
        results[id] = await api.migrationPreflight(id, provider, region);
      }
      setPreflights(results);
    } catch (err) {
      setError(cleanError(err));
    } finally {
      setBusy(false);
    }
  }, [selectedServiceIds, provider, region]);

  // Step 2 -> 3: Inspect machine and create saved cloud review
  async function handleInspectAndReview() {
    setBusy(true); setError('');
    try {
      let planId = '';
      if (provider === 'aws') {
        const spec: AWSMachineSpec = {
          operationId: '11111111-1111-1111-1111-' + Date.now().toString(16).padStart(12, '0').slice(-12),
          instanceType: size,
          imageId: 'ami-0c55b159cbfafe1f0',
          subnetId: 'subnet-12345678',
          securityGroupId: 'sg-12345678',
          rootDevice: '/dev/xvda',
          diskGb: Number(diskGb),
        };
        const res = await api.inspectAWSMachine(selectedConnId, spec, 'amd64');
        planId = res.savedPlanId || '';
      } else if (provider === 'azure') {
        const spec: AzureMachineSpec = {
          operationId: '22222222-2222-2222-2222-' + Date.now().toString(16).padStart(12, '0').slice(-12),
          subscriptionId: connections.find(c => c.id === selectedConnId)?.accountId || '00000000-0000-0000-0000-000000000000',
          resourceGroup: 'rackops-rg',
          size,
          imageId: '/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/images/img',
          networkInterfaceId: '/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/nic',
          sshPublicKey: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKey test@local',
          diskGb: Number(diskGb),
        };
        const res = await api.inspectAzureMachine(selectedConnId, spec, 'amd64');
        planId = res.savedPlanId || '';
      } else if (provider === 'gcp') {
        const spec: GCPMachineSpec = {
          operationId: 'rackops-gcp-' + Date.now(),
          projectId: connections.find(c => c.id === selectedConnId)?.accountId || 'test-project',
          zone: region + '-a',
          machineType: size,
          image: 'projects/debian-cloud/global/images/debian-12',
          subnetwork: 'default',
          diskGb: Number(diskGb),
        };
        const res = await api.inspectGCPMachine(selectedConnId, spec, 'amd64');
        planId = res.savedPlanId || '';
      }

      if (planId) {
        // Automatically fetch estimates for compute, disk, and transfer
        await api.quoteCloudCompute(planId).catch(() => {});
        await api.quoteCloudDisk(planId).catch(() => {});
        await api.quoteCloudTransfer(planId, 50).catch(() => {});
        const plans = await api.cloudReviewPlans();
        const found = plans.find(p => p.id === planId) || null;
        setActivePlan(found);
      }
      setStep(3);
    } catch (err) {
      setError(cleanError(err));
    } finally {
      setBusy(false);
    }
  }

  // Refresh active review quotes
  async function refreshQuotes() {
    if (!activePlan) return;
    setBusy(true); setError('');
    try {
      await api.quoteCloudCompute(activePlan.id).catch(() => {});
      await api.quoteCloudDisk(activePlan.id).catch(() => {});
      await api.quoteCloudTransfer(activePlan.id, 50).catch(() => {});
      const plans = await api.cloudReviewPlans();
      setActivePlan(plans.find(p => p.id === activePlan.id) || null);
    } catch (err) {
      setError(cleanError(err));
    } finally {
      setBusy(false);
    }
  }

  // Step 4: Approve Plan & Launch
  async function handleApproveAndStart() {
    if (!activePlan) return;
    setBusy(true); setError('');
    try {
      const planDraftId = 'plan-' + activePlan.id.slice(0, 16);
      await api.createMigrationDraft({
        id: planDraftId,
        cloudReviewId: activePlan.id,
        serviceIds: selectedServiceIds,
      });
      const approved = await api.approveMigrationPlan(planDraftId);
      const jobs = await api.migrations();
      const job = jobs.find(j => j.id === approved.jobId) || null;
      setActiveJob(job);
      setStep(5);
    } catch (err) {
      setError(cleanError(err));
    } finally {
      setBusy(false);
    }
  }

  // Step 5: Advance stage
  async function handleAdvanceJob() {
    if (!activeJob) return;
    setBusy(true); setError('');
    try {
      const updated = await api.advanceMigration(activeJob.id);
      setActiveJob(updated);
    } catch (err) {
      setError(cleanError(err));
    } finally {
      setBusy(false);
    }
  }

  // Step 5: Operator recovery
  async function handleRecoverJob() {
    if (!activeJob) return;
    setBusy(true); setError('');
    try {
      const updated = await api.recoverMigration(activeJob.id);
      setActiveJob(updated);
    } catch (err) {
      setError(cleanError(err));
    } finally {
      setBusy(false);
    }
  }

  // Auto-advance poll when active in step 5
  useEffect(() => {
    if (step !== 5 || !activeJob || activeJob.status === 'completed' || activeJob.status === 'recovery_required') {
      return;
    }
    const timer = setInterval(() => {
      api.migration(activeJob.id)
        .then(res => {
          setActiveJob(res);
          if (res.status === 'running') {
            api.advanceMigration(activeJob.id).then(setActiveJob).catch(() => {});
          }
        })
        .catch(() => {});
    }, 3000);
    return () => clearInterval(timer);
  }, [step, activeJob]);

  return (
    <Modal
      title="Cloud Migration & App Relocation"
      icon={<Cloud size={16} />}
      onClose={onClose}
    >
      {/* Step Indicators */}
      <div className="row" style={{ gap: 8, marginBottom: 20, borderBottom: '1px solid var(--border)', paddingBottom: 12 }}>
        {[
          { n: 1, label: 'Select Apps' },
          { n: 2, label: 'Cloud Target' },
          { n: 3, label: 'Cost & Downtime' },
          { n: 4, label: 'Approval' },
          { n: 5, label: 'Live Progress' }
        ].map(item => (
          <div
            key={item.n}
            className="row"
            style={{
              gap: 6,
              fontSize: 'var(--text-xs)',
              fontWeight: 600,
              color: step === item.n ? 'var(--fg)' : step > item.n ? 'var(--ok)' : 'var(--dim)',
              opacity: step === item.n ? 1 : 0.7,
            }}
          >
            <span
              style={{
                width: 20,
                height: 20,
                borderRadius: '50%',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                background: step === item.n ? 'var(--primary)' : step > item.n ? 'var(--ok-bg)' : 'var(--bg-card)',
                color: step === item.n ? '#000' : step > item.n ? 'var(--ok)' : 'inherit',
                fontSize: 11,
              }}
            >
              {step > item.n ? <Check size={12} /> : item.n}
            </span>
            <span>{item.label}</span>
          </div>
        ))}
      </div>

      {error && (
        <div className="card" style={{ padding: 12, marginBottom: 16, borderColor: 'var(--danger)', color: 'var(--danger)' }} role="alert">
          <AlertTriangle size={16} style={{ display: 'inline', marginRight: 6 }} /> {error}
        </div>
      )}

      {/* Step 1: Select Apps */}
      {step === 1 && (
        <div>
          <p className="dim" style={{ marginBottom: 14 }}>
            Select the stateless services, databases, and volumes to migrate. Source data remains completely intact and fenced until verified cutover.
          </p>

          <div className="stack" style={{ gap: 8, maxHeight: 260, overflowY: 'auto', marginBottom: 16 }}>
            {services.map(svc => {
              const selected = selectedServiceIds.includes(svc.id);
              const pf = preflights[svc.id];
              return (
                <div
                  key={svc.id}
                  className="card row"
                  style={{
                    padding: '10px 14px',
                    gap: 12,
                    cursor: 'pointer',
                    borderColor: selected ? 'var(--primary)' : 'var(--border)',
                    background: selected ? 'rgba(74, 222, 128, 0.05)' : 'var(--bg-card)',
                  }}
                  onClick={() => {
                    setSelectedServiceIds(prev =>
                      prev.includes(svc.id) ? prev.filter(x => x !== svc.id) : [...prev, svc.id]
                    );
                  }}
                >
                  <input
                    type="checkbox"
                    checked={selected}
                    onChange={() => {}}
                    style={{ accentColor: 'var(--primary)', cursor: 'pointer' }}
                  />
                  <div style={{ flex: 1 }}>
                    <div className="row" style={{ gap: 8 }}>
                      <strong>{svc.name}</strong>
                      <span className="badge sm">{svc.kind}</span>
                      {svc.image && <span className="dim" style={{ fontSize: 'var(--text-xs)' }}>{svc.image}</span>}
                    </div>
                    {pf && pf.blockers.length > 0 && (
                      <div className="dim danger" style={{ fontSize: 'var(--text-xs)', marginTop: 4 }}>
                        {pf.blockers.map(b => b.message).join('; ')}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          <div className="row" style={{ justifyContent: 'space-between', marginTop: 20 }}>
            <button className="btn" onClick={runPreflights} disabled={busy || selectedServiceIds.length === 0}>
              <ShieldCheck size={14} /> Run Compatibility Preflight
            </button>
            <button
              className="btn primary"
              disabled={selectedServiceIds.length === 0}
              onClick={() => setStep(2)}
            >
              Configure Target Cloud <ArrowRight size={14} />
            </button>
          </div>
        </div>
      )}

      {/* Step 2: Target Cloud Machine */}
      {step === 2 && (
        <div>
          <p className="dim" style={{ marginBottom: 14 }}>
            Choose your cloud provider and machine sizing. RackOps will configure the node and connect it to your control plane.
          </p>

          <div className="stack" style={{ gap: 14, marginBottom: 16 }}>
            <label className="field-label">
              Cloud Connection
              <select
                className="input"
                value={selectedConnId}
                onChange={e => {
                  setSelectedConnId(e.target.value);
                  const conn = connections.find(c => c.id === e.target.value);
                  if (conn) {
                    setProvider(conn.provider as any);
                    setRegion(conn.region);
                  }
                }}
              >
                {connections.map(c => (
                  <option key={c.id} value={c.id}>
                    {c.name} ({c.provider.toUpperCase()} · {c.region} · {c.accountId})
                  </option>
                ))}
              </select>
            </label>

            <div className="row" style={{ gap: 12 }}>
              <label className="field-label" style={{ flex: 1 }}>
                Target Region
                <input
                  className="input"
                  value={region}
                  onChange={e => setRegion(e.target.value)}
                  placeholder="e.g. us-east-1"
                />
              </label>

              <label className="field-label" style={{ flex: 1 }}>
                Instance / VM Size
                <input
                  className="input"
                  value={size}
                  onChange={e => setSize(e.target.value)}
                  placeholder="e.g. t3.medium"
                />
              </label>

              <label className="field-label" style={{ width: 100 }}>
                Root Disk (GB)
                <input
                  className="input"
                  type="number"
                  min={10}
                  max={2000}
                  value={diskGb}
                  onChange={e => setDiskGb(Number(e.target.value))}
                />
              </label>
            </div>
          </div>

          <div className="row" style={{ justifyContent: 'space-between', marginTop: 20 }}>
            <button className="btn" onClick={() => setStep(1)}>
              <ArrowLeft size={14} /> Back
            </button>
            <button
              className="btn primary"
              disabled={busy || !selectedConnId || !region || !size}
              onClick={handleInspectAndReview}
            >
              {busy ? <Spinner size={14} /> : 'Calculate Sourced Quotes'} <ArrowRight size={14} />
            </button>
          </div>
        </div>
      )}

      {/* Step 3: Review & Sourced Estimates */}
      {step === 3 && activePlan && (
        <div>
          <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <h4 style={{ margin: 0 }}>Verified Sourced Cost & Downtime Review</h4>
            <button className="btn sm" disabled={busy} onClick={refreshQuotes}>
              <RotateCw size={12} /> Refresh Quotes
            </button>
          </div>

          <div className="stack" style={{ gap: 10, marginBottom: 16 }}>
            {/* Total monthly cost card */}
            <div className="card" style={{ padding: 16, background: 'var(--bg-elevated)', borderColor: 'var(--primary)' }}>
              <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <div className="dim" style={{ fontSize: 'var(--text-xs)' }}>ESTIMATED TOTAL MONTHLY COST</div>
                  <div style={{ fontSize: 24, fontWeight: 700, color: 'var(--primary)', marginTop: 4 }}>
                    {activePlan.monthlyCost !== null ? `$${activePlan.monthlyCost.toFixed(2)} / mo` : 'Pending calculation…'}
                  </div>
                </div>
                <div>
                  <div className="dim" style={{ fontSize: 'var(--text-xs)' }}>ESTIMATED DOWNTIME WINDOW</div>
                  <div className="row" style={{ gap: 6, fontWeight: 600, marginTop: 4 }}>
                    <Clock size={16} />
                    <span>{activePlan.downtimeSeconds ? `~${Math.round(activePlan.downtimeSeconds / 60)} min` : '~2-5 min'}</span>
                  </div>
                </div>
              </div>
            </div>

            {/* Breakdown item: Compute */}
            <div className="card" style={{ padding: 12 }}>
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <div>
                  <strong>Base VM Compute:</strong> {activePlan.provider.toUpperCase()} {activePlan.region} ({size})
                </div>
                <span className="badge">
                  {activePlan.computeQuote ? `$${activePlan.computeQuote.monthly.toFixed(2)}/mo` : 'Pending'}
                </span>
              </div>
              {activePlan.computeQuote && (
                <div className="dim" style={{ fontSize: 'var(--text-xs)', marginTop: 4 }}>
                  Retrieved from official provider pricing API. Rate: ${activePlan.computeQuote.hourly}/hr.
                </div>
              )}
            </div>

            {/* Breakdown item: Disk */}
            <div className="card" style={{ padding: 12 }}>
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <div>
                  <strong>Root Disk:</strong> {diskGb} GiB standard volume
                </div>
                <span className="badge">
                  {activePlan.diskQuote ? `$${activePlan.diskQuote.monthly.toFixed(2)}/mo` : 'Pending'}
                </span>
              </div>
            </div>

            {/* Breakdown item: Data Transfer */}
            <div className="card" style={{ padding: 12 }}>
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <div>
                  <strong>Egress Data Transfer:</strong> Est. 50 GiB migration payload
                </div>
                <span className="badge">
                  {activePlan.transferQuote ? `$${activePlan.transferQuote.monthly.toFixed(2)}/mo` : 'Pending'}
                </span>
              </div>
              {activePlan.transferQuote && (
                <div className="dim" style={{ fontSize: 'var(--text-xs)', marginTop: 4 }}>
                  Standard tier internet egress rate: ${activePlan.transferQuote.perGbMonth}/GB.
                </div>
              )}
            </div>
          </div>

          <div className="row" style={{ justifyContent: 'space-between', marginTop: 20 }}>
            <button className="btn" onClick={() => setStep(2)}>
              <ArrowLeft size={14} /> Back
            </button>
            <button
              className="btn primary"
              disabled={busy || !activePlan.canApprove}
              onClick={() => setStep(4)}
            >
              Review Safety Boundary <ArrowRight size={14} />
            </button>
          </div>
        </div>
      )}

      {/* Step 4: Safety & Customer Approval */}
      {step === 4 && activePlan && (
        <div>
          <h4 style={{ margin: '0 0 8px 0' }}>Confirm Immutable Plan & Customer Approval</h4>
          <p className="dim" style={{ marginBottom: 14 }}>
            Please review the safety guarantees and explicit cutover boundaries before authorizing provisioning.
          </p>

          <div className="stack" style={{ gap: 10, marginBottom: 16 }}>
            <div className="card" style={{ padding: 12 }}>
              <div className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
                <ShieldCheck size={18} style={{ color: 'var(--ok)', flexShrink: 0, marginTop: 2 }} />
                <div>
                  <strong>Source Preservation Guarantee:</strong> Source database containers and volume files are never deleted or truncated during or after migration.
                </div>
              </div>
            </div>

            <div className="card" style={{ padding: 12 }}>
              <div className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
                <CheckCircle2 size={18} style={{ color: 'var(--ok)', flexShrink: 0, marginTop: 2 }} />
                <div>
                  <strong>Write-Safety Fencing:</strong> Destination database starts in read-only mode until read-only validation passes. Fences are only lifted upon successful routing cutover.
                </div>
              </div>
            </div>

            <div className="card" style={{ padding: 12 }}>
              <div className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
                <HardDrive size={18} style={{ color: 'var(--primary)', flexShrink: 0, marginTop: 2 }} />
                <div>
                  <strong>Recoverable Offline Keys:</strong> Per-artifact 256-bit AES keys remain in encrypted control-plane storage and are released exclusively to the verified target node.
                </div>
              </div>
            </div>
          </div>

          <div className="row" style={{ justifyContent: 'space-between', marginTop: 20 }}>
            <button className="btn" onClick={() => setStep(3)}>
              <ArrowLeft size={14} /> Back
            </button>
            <button
              className="btn primary"
              disabled={busy}
              onClick={handleApproveAndStart}
              style={{ background: 'var(--ok)', color: '#000', fontWeight: 600 }}
            >
              {busy ? <Spinner size={14} /> : <Check size={14} />} Approve Plan & Launch Migration
            </button>
          </div>
        </div>
      )}

      {/* Step 5: Live Stage Execution & Recovery Monitor */}
      {step === 5 && activeJob && (
        <div>
          <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center', marginBottom: 14 }}>
            <div>
              <span className="badge primary" style={{ marginRight: 8 }}>{activeJob.status.toUpperCase()}</span>
              <strong>Migration Job:</strong> {activeJob.id}
            </div>
            <div className="row" style={{ gap: 8 }}>
              {activeJob.status === 'recovery_required' && (
                <button className="btn danger sm" disabled={busy} onClick={handleRecoverJob}>
                  Request Recovery
                </button>
              )}
              {activeJob.status !== 'completed' && (
                <button className="btn sm" disabled={busy} onClick={handleAdvanceJob}>
                  <RotateCw size={12} /> Advance Step
                </button>
              )}
            </div>
          </div>

          {/* 9-stage sequence tracker */}
          <div className="stack" style={{ gap: 8, maxHeight: 320, overflowY: 'auto', marginBottom: 16 }}>
            {STAGES.map((s, idx) => {
              const currentIdx = STAGES.findIndex(x => x.id === activeJob.stage);
              const isPast = idx < currentIdx || activeJob.stage === 'complete';
              const isCurrent = idx === currentIdx && activeJob.stage !== 'complete';
              return (
                <div
                  key={s.id}
                  className="card row"
                  style={{
                    padding: '8px 12px',
                    gap: 12,
                    borderColor: isCurrent ? 'var(--primary)' : isPast ? 'var(--ok)' : 'var(--border)',
                    background: isCurrent ? 'rgba(74, 222, 128, 0.08)' : 'var(--bg-card)',
                    opacity: isPast || isCurrent ? 1 : 0.5,
                  }}
                >
                  <span
                    style={{
                      width: 22,
                      height: 22,
                      borderRadius: '50%',
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      background: isPast ? 'var(--ok)' : isCurrent ? 'var(--primary)' : 'var(--bg-elevated)',
                      color: isPast || isCurrent ? '#000' : 'inherit',
                      fontSize: 12,
                      fontWeight: 600,
                    }}
                  >
                    {isPast ? <Check size={13} /> : idx + 1}
                  </span>
                  <div style={{ flex: 1 }}>
                    <div className="row" style={{ justifyContent: 'space-between' }}>
                      <strong>{s.label}</strong>
                      <span className="badge sm" style={{ textTransform: 'capitalize' }}>
                        {isPast ? 'Done' : isCurrent ? activeJob.status : 'Pending'}
                      </span>
                    </div>
                    <div className="dim" style={{ fontSize: 'var(--text-xs)' }}>{s.desc}</div>
                  </div>
                </div>
              );
            })}
          </div>

          {activeJob.status === 'completed' && (
            <div className="card" style={{ padding: 14, background: 'rgba(74, 222, 128, 0.1)', borderColor: 'var(--ok)', color: 'var(--ok)' }}>
              <div className="row" style={{ gap: 8 }}>
                <CheckCircle2 size={18} />
                <div>
                  <strong>Migration Successfully Completed!</strong>
                  <div style={{ fontSize: 'var(--text-xs)', marginTop: 2, color: 'var(--fg)' }}>
                    Traffic has cut over to the cloud destination and writes are enabled. Source node containers and backup archives remain preserved for customer safety.
                  </div>
                </div>
              </div>
            </div>
          )}

          <div className="row" style={{ justifyContent: 'flex-end', marginTop: 16 }}>
            <button className="btn" onClick={onClose}>
              {activeJob.status === 'completed' ? 'Done' : 'Close Monitor (Runs in Background)'}
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}
