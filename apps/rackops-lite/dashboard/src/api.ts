// Shared API types mirrored from the Go backend.
export interface CloudOperation {
  id: string; provider: string; region: string; state: string;
  resourceId?: string; operationId?: string; message: string;
  recoveryState?: string; recoveryMessage?: string; recoveryTotalAttempts: number;
}

export interface AWSMachineSpec {
  operationId: string; imageId: string; instanceType: string; subnetId: string;
  securityGroupId: string; rootDevice: string; diskGb: number;
}
export interface GCPMachineSpec {
  operationId: string; projectId: string; zone: string; machineType: string;
  image: string; subnetwork: string; diskGb: number;
}
export interface AzureMachineSpec {
  operationId: string; subscriptionId: string; resourceGroup: string; size: string;
  imageId: string; networkInterfaceId: string; sshPublicKey: string; diskGb: number;
}
export interface MachineInspection {
  savedPlanId?: string;
  expiresAt?: string;
  provider: string; region: string; specFingerprint: string; checkedAt: string;
  architecture: string; vcpus: number; memoryMb: number; zone: string; minimumDiskGb: number;
  resourceCompatible: boolean; canLaunch: boolean; monthlyCostEstimate: number | null;
  blockers: { code: string; message: string }[]; remainingChecks: string[];
}
export interface CloudConnection {
  id: string; orgId: string; name: string; provider: string; region: string;
  accountId: string; principal: string; validatedAt: string;
}

export interface CloudReviewPlan {
  computeQuote?: ComputeQuote;
  diskQuote?: DiskQuote;
  transferQuote?: TransferQuote;
  id: string; connectionId: string; provider: string; region: string; accountId: string;
  architecture: string; createdAt: string; expiresAt: string; status: string;
  canApprove: boolean; monthlyCost: number | null; transferCost: number | null;
  dataSizeBytes?: number | null; downtimeSeconds: number | null; blockers: string[];
  inspection: MachineInspection;
  spec: { aws?: AWSMachineSpec; azure?: AzureMachineSpec; gcp?: GCPMachineSpec };
}

export interface DiskQuote {
  provider: string; region: string; diskType: string; diskGb: number;
  currency: string; monthly: number; perGbMonth: number; meterId: string;
  source: string; retrievedAt: string; expiresAt: string; exclusions: string[];
}

export interface ComputeQuote {
  currency: string; hourly: number; monthly: number; hoursPerMonth: number;
  source: string; meterId: string; retrievedAt: string; effectiveAt: string;
  expiresAt: string; exclusions: string[];
}

export interface TransferQuote {
  provider: string; region: string; transferGb: number; tier: string;
  currency: string; monthly: number; perGbMonth: number; meterId: string;
  source: string; retrievedAt: string; expiresAt: string; exclusions: string[];
}

export interface MigrationPlanSummary {
  id: string; cloudReviewId: string; serviceIds: string[];
  createdAt: string; expiresAt: string; canStart: boolean;
}

export interface MigrationJobProgress {
  id: string;
  stage: 'provision' | 'quiesce' | 'backup' | 'transfer' | 'restore' | 'validate' | 'cutover' | 'enable_writes' | 'complete';
  status: string;
  updatedAt: string;
  destinationWritesPossible: boolean;
  receipts?: Record<string, string>;
}

export interface MigrationReview {
  serviceId: string; provider: string; region: string; architecture: string;
  canStart: boolean; checkedAt: string;
  blockers: { code: string; message: string }[];
  monthlyCostEstimate: number | null; dataBytes: number | null; downtimeSeconds: number | null;
}
export interface GPU {
  index: number; vendor: string; model: string;
  vramTotalMb: number; vramUsedMb: number; utilPct: number; tempC: number; powerW: number;
}
export interface Drive {
  device: string; model: string; type: string; sizeGb: number; usedGb: number;
  mountPoint?: string; readMbps: number; writeMbps: number; tempC?: number; health: string;
}
export interface NIC { name: string; mac: string; speedMb: number; rxMbps: number; txMbps: number }
export interface Hardware {
  cpuModel: string; cpuCores: number; cpuThreads: number; memTotalMb: number;
  gpus: GPU[] | null; drives: Drive[] | null; nics: NIC[] | null;
}
export interface Metrics {
  timestamp: string; cpuPct: number; load1: number; load5: number; load15: number;
  memUsedMb: number; memPct: number; swapUsedMb: number; tempC: number;
  uptimeSec: number; containers: number; processCount: number;
}
export interface Node {
  id: string; name: string; role: string; status: string; ip: string; meshIp: string;
  publicIp?: string;
  os: string; arch: string; kernel: string; hostname: string; joinedAt: string;
  lastSeen: string; agentVersion: string; hasPublicIp: boolean;
  hardware: Hardware; metrics: Metrics;
}
export interface Project { id: string; name: string; description: string; createdAt: string }
export interface Environment { id: string; projectId: string; name: string }
export interface EnvVar { key: string; value: string; secret: boolean; redacted?: boolean }
export interface Mount {
  type: 'bind' | 'volume'; source: string; target: string; readOnly: boolean; drivePath?: string;
}
export interface Service {
  id: string; projectId: string; envId: string; name: string; kind: string; status: string;
  gitRepo?: string; gitBranch?: string; image?: string; composeYml?: string; templateId?: string;
  adopted?: boolean; externalName?: string;
  nodeId: string; autoPlace: boolean;
  port: number; containerPort?: number; ports?: number[]; command?: string; domain: string; ingress: string;
  /** True when `domain` is a RackOps Front Door address rather than one you own. */
  frontDoor?: boolean;
  quickTunnelUrl?: string;
  env: EnvVar[] | null; mounts?: Mount[]; volumes?: string[];
  cpuPct: number; memUsedMb: number; memLimitMb?: number; cpuLimit?: number;
  restartPolicy?: string; gpu?: boolean;
  containerId?: string; createdAt: string; updatedAt?: string;
}
export interface DirEntry { name: string; isDir: boolean; size?: number }
export interface ServiceUpdate {
  image?: string; gitBranch?: string; command?: string; port?: number; containerPort?: number; ports?: number[];
  env?: EnvVar[]; mounts?: Mount[]; memLimitMb?: number; cpuLimit?: number;
  restartPolicy?: string; gpu?: boolean; nodeId?: string; redeploy: boolean;
}
export interface Overview {
  nodesOnline: number; nodesTotal: number; servicesUp: number; servicesTotal: number;
  totalMemMb: number; usedMemMb: number; totalStorageGb: number; usedStorageGb: number;
  avgCpuPct: number; openAlerts: number;
}
export interface Alert { id: string; nodeId?: string; level: string; kind: string; message: string; createdAt: string; resolved: boolean }
export interface EventItem { id: string; kind: string; message: string; createdAt: string }
/** One prior exchange replayed to the Co-Pilot for multi-turn context. */
export interface ChatTurn { role: 'user' | 'assistant'; content: string }
export interface PlacementScore { nodeId: string; score: number; reasons: string[]; viable: boolean }
export interface EnvDef { key: string; label: string; default: string; secret: boolean; required: boolean; generate?: boolean }
export interface Template {
  id: string; name: string; category: string; description: string; image: string;
  port: number; ports?: number[]; env?: EnvDef[]; volumes?: string[]; memMb: number;
  gpu?: boolean; notes?: string; composeYml?: string;
}
export interface Deployment {
  id: string; serviceId: string; version: number; status: string; trigger: string;
  log: string; startedAt: string; durationMs: number;
}
export interface Domain {
  id: string; name: string; wildcard: boolean; createdAt: string;
  status: 'pending' | 'verified' | 'misconfigured';
  message?: string; expectedIp?: string; resolvedIps?: string[];
  edgeOk: boolean; certOk: boolean; certIssuer?: string; certExpiry?: string;
  lastChecked?: string;
}
export interface NetworkDiagnosis {
  edgeEnabled: boolean; edgeStatus?: string; edgeNodeId?: string;
  hasDirectIp: boolean; lanOnly: boolean; meshEnabled: boolean;
  nodes: { id: string; name: string; lanIp: string; publicIp?: string; reachability: 'direct' | 'nat' | 'unknown' }[] | null;
}
export interface IngressConfig {
  enabled: boolean; nodeId: string; httpPort: number; httpsPort: number; acmeEmail: string;
  dashboardDomain?: string; dashboardTunnel?: boolean; dashboardTunnelUrl?: string;
  status?: string; statusMsg?: string; lastDeploy?: string;
}
/** RackOps Front Door: the vendor-run tunnel that gives apps a public HTTPS
 *  address with no public IP, no port forwarding and no DNS. */
export interface FrontDoorConfig {
  enabled: boolean;
  tenantId?: string;
  workspace?: string;
  popAddr?: string;
  zone?: string;
  nodeId?: string;
  /** off | connecting | connected | rejected */
  status?: string;
  statusMsg?: string;
  popId?: string;
  /** quic | tcp — tcp means the network blocks UDP. */
  transport?: string;
  usedBytes?: number;
  quotaBytes?: number;
  connectedAt?: string;
  lastChange?: string;
}
export interface MeshStatus {
  enabled: boolean; hubIp?: string; hubKey?: string; udpPort?: number;
  peers?: { nodeId: string; meshIp: string }[];
}
export interface ClusterStatus {
  enabled: boolean; nodeId?: string; isLeader?: boolean; leaderAddr?: string;
  peers?: { id: string; addr: string; role: string }[];
}
export interface User { id: string; email: string; fullName: string; role: string; createdAt: string }
export interface APIToken { id: string; name: string; userId: string; createdAt: string; lastUsed?: string; expiresAt?: string }
export interface OrgPermissions {
  createProjects: boolean; deleteProjects: boolean; createServices: boolean; deleteServices: boolean;
  manageDomains: boolean; manageMachines: boolean; accessApi: boolean; accessedProjects?: string[];
}
export interface OrgMember { userId: string; role: 'owner' | 'admin' | 'member'; permissions: OrgPermissions; addedAt: string }
export interface Organization { id: string; name: string; createdBy: string; createdAt: string; members: OrgMember[] }
export interface Invitation {
  id: string; orgId: string; email: string; role: string; status: string; createdAt: string; expiresAt: string;
}
export interface TicketMessage { id: string; author: 'user' | 'agent'; name: string; body: string; at: string }
export interface SupportTicket {
  forwardPending?: boolean;
  deliveryError?: string;
  resolutionPending?: boolean;
  replySyncError?: string;
  id: string; subject: string; category: string; severity: string;
  status: 'open' | 'pending' | 'resolved'; email: string; createdBy: string;
  createdAt: string; updatedAt: string; messages: TicketMessage[];
}
export interface VersionInfo {
  current: string; latest: string; updateAvailable: boolean;
  releaseUrl: string; checkedAt: string; error?: string;
}
export interface Settings {
  discordWebhook?: string; slackWebhook?: string; telegramToken?: string; telegramChatId?: string;
  genericWebhook?: string; notifyDeploys: boolean; notifyAlerts: boolean;
  aiProvider?: string; anthropicKey?: string; anthropicModel?: string;
  geminiKey?: string; geminiModel?: string; openaiKey?: string;
  openaiBase?: string; openaiModel?: string;
}

const base = '';

// Session token management.
export function getToken(): string { return localStorage.getItem('rackops_session') ?? ''; }
export function setToken(t: string) { localStorage.setItem('rackops_session', t); }
export function clearToken() { localStorage.removeItem('rackops_session'); }

// Active organization: sent as a header with every request; the server
// validates membership per call, so switching is instant and safe.
export function getActiveOrg(): string { return localStorage.getItem('rackops_org') ?? ''; }
export function setActiveOrg(id: string) {
  localStorage.setItem('rackops_org', id);
  window.dispatchEvent(new Event('rackops-org-changed'));
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { ...(init?.headers as Record<string, string>) };
  const tok = getToken();
  if (tok) headers['Authorization'] = `Bearer ${tok}`;
  const org = getActiveOrg();
  if (org) headers['X-Rackops-Org'] = org;
  const r = await fetch(base + path, { ...init, headers });
  if (r.status === 401 && !path.startsWith('/api/auth/')) {
    clearToken();
    window.dispatchEvent(new Event('rackops-logout'));
  }
  if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
  if (r.status === 204) return undefined as T;
  return r.json();
}

// One downsampled point of metric history. tempC is only present for nodes.
export interface MetricPoint { t: number; cpuPct: number; memUsedMb: number; tempC?: number }
export interface MetricsHistory {
  scope: 'fleet' | 'node' | 'service';
  id?: string;
  window: '24h' | '7d';
  stepSec: number;
  points: MetricPoint[];
}

export const api = {
  cloudReviewPlans: () => req<CloudReviewPlan[]>('/api/cloud-plans'),
  quoteCloudCompute: (id: string) => req<ComputeQuote>(`/api/cloud-plans/${encodeURIComponent(id)}/compute-estimate`, { method: 'POST' }),
  quoteCloudDisk: (id: string) => req<DiskQuote>(`/api/cloud-plans/${encodeURIComponent(id)}/disk-estimate`, { method: 'POST' }),
  quoteCloudTransfer: (id: string, transferGb?: number, dataSizeBytes?: number) => req<TransferQuote>(`/api/cloud-plans/${encodeURIComponent(id)}/transfer-estimate`, { method: 'POST', body: JSON.stringify({ transferGb, dataSizeBytes }) }),
  createMigrationDraft: (draft: { id: string; cloudReviewId: string; serviceIds: string[] }) => req<{ id: string; status: string; canStart: boolean; message: string }>('/api/migration-plans', { method: 'POST', body: JSON.stringify(draft) }),
  migrationPlans: () => req<MigrationPlanSummary[]>('/api/migration-plans'),
  migrationPlan: (id: string) => req<MigrationPlanSummary>(`/api/migration-plans/${encodeURIComponent(id)}`),
  approveMigrationPlan: (id: string) => req<{ jobId: string; planId: string; stage: string; status: string; message: string }>(`/api/migration-plans/${encodeURIComponent(id)}/approve`, { method: 'POST' }),
  migrations: () => req<MigrationJobProgress[]>('/api/migrations'),
  migration: (id: string) => req<MigrationJobProgress>(`/api/migrations/${encodeURIComponent(id)}`),
  advanceMigration: (id: string) => req<MigrationJobProgress>(`/api/migrations/${encodeURIComponent(id)}/advance`, { method: 'POST' }),
  recoverMigration: (id: string) => req<MigrationJobProgress>(`/api/migrations/${encodeURIComponent(id)}/recover`, { method: 'POST' }),
  createCloudConnection: (name: string, provider: string, region: string, credentials: Record<string, string>) => req<CloudConnection>('/api/cloud-connections', { method: 'POST', body: JSON.stringify({ name, provider, region, credentials }) }),
  cloudOperations: () => req<CloudOperation[]>('/api/cloud-operations'),
  controlCloudRecovery: (id: string, action: 'pause' | 'resume') => req<CloudOperation>(`/api/cloud-operations/${encodeURIComponent(id)}/recovery`, { method: 'POST', body: JSON.stringify({ action }) }),
  cloudConnections: () => req<CloudConnection[]>('/api/cloud-connections'),
  inspectAWSMachine: (id: string, spec: AWSMachineSpec, architecture: string) => req<{ id: string; expiresAt: string; inspection: MachineInspection }>(`/api/cloud-connections/${encodeURIComponent(id)}/plans`, { method: 'POST', body: JSON.stringify({ spec: { aws: spec }, architecture }) }).then(plan => ({ ...plan.inspection, savedPlanId: plan.id, expiresAt: plan.expiresAt })),
  inspectGCPMachine: (id: string, spec: GCPMachineSpec, architecture: string) => req<{ id: string; expiresAt: string; inspection: MachineInspection }>(`/api/cloud-connections/${encodeURIComponent(id)}/plans`, { method: 'POST', body: JSON.stringify({ spec: { gcp: spec }, architecture }) }).then(plan => ({ ...plan.inspection, savedPlanId: plan.id, expiresAt: plan.expiresAt })),
  inspectAzureMachine: (id: string, spec: AzureMachineSpec, architecture: string) => req<{ id: string; expiresAt: string; inspection: MachineInspection }>(`/api/cloud-connections/${encodeURIComponent(id)}/plans`, { method: 'POST', body: JSON.stringify({ spec: { azure: spec }, architecture }) }).then(plan => ({ ...plan.inspection, savedPlanId: plan.id, expiresAt: plan.expiresAt })),
  createAWSConnection: (name: string, region: string, credentials: { accessKeyId: string; secretAccessKey: string; sessionToken: string }) => req<CloudConnection>('/api/cloud-connections', { method: 'POST', body: JSON.stringify({ name, provider: 'aws', region, credentials }) }),
  validateCloudConnection: (id: string) => req<CloudConnection>(`/api/cloud-connections/${encodeURIComponent(id)}/validate`, { method: 'POST' }),
  deleteCloudConnection: (id: string) => req<void>(`/api/cloud-connections/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  migrationPreflight: (id: string, provider: string, region: string) => req<MigrationReview>(`/api/services/${encodeURIComponent(id)}/migration-preflight`, { method: 'POST', body: JSON.stringify({ provider, region }) }),
  overview: () => req<Overview>('/api/overview'),
  nodes: () => req<Node[]>('/api/nodes'),
  node: (id: string) => req<Node>(`/api/nodes/${id}`),
  metricsHistory: (scope: 'fleet' | 'node' | 'service', id?: string, window: '24h' | '7d' = '24h') =>
    req<MetricsHistory>(`/api/metrics/history?scope=${scope}${id ? `&id=${encodeURIComponent(id)}` : ''}&window=${window}`),
  events: () => req<EventItem[]>('/api/events'),
  alerts: () => req<Alert[]>('/api/alerts'),
  projects: () => req<Project[]>('/api/projects'),
  createProject: (name: string, description: string) =>
    req<Project>('/api/projects', { method: 'POST', body: JSON.stringify({ name, description }) }),
  envs: (projectId: string) => req<Environment[]>(`/api/projects/${projectId}/envs`),
  services: (project?: string, env?: string) => {
    const q = new URLSearchParams();
    if (project) q.set('project', project);
    if (env) q.set('env', env);
    return req<Service[]>(`/api/services?${q}`);
  },
  createService: (body: object) => req<Service>('/api/services', { method: 'POST', body: JSON.stringify(body) }),
  service: (id: string) => req<Service>(`/api/services/${id}`),
  updateService: (id: string, upd: ServiceUpdate) => req<Service>(`/api/services/${id}`, { method: 'PATCH', body: JSON.stringify(upd) }),
  clearServiceLogs: (id: string) => req<{ clearedAt: string }>(`/api/services/${id}/clear-logs`, { method: 'POST' }),
  nodeFS: (nodeId: string, path: string) =>
    req<{ path: string; entries: DirEntry[] }>(`/api/nodes/${nodeId}/fs?path=${encodeURIComponent(path)}`),
  serviceAction: (id: string, action: string) => req<Service>(`/api/services/${id}/${action}`, { method: 'POST' }),
  deleteService: (id: string) => req<void>(`/api/services/${id}`, { method: 'DELETE' }),
  placement: (body: object) => req<PlacementScore[]>('/api/placement', { method: 'POST', body: JSON.stringify(body) }),
  suggestPort: (node: string, port: number) =>
    req<{ port: number }>(`/api/suggest-port?node=${encodeURIComponent(node)}&port=${port}`),
  templates: () => req<Template[]>('/api/templates'),
  serviceLogs: (id: string) => req<{ logs: string }>(`/api/services/${id}/logs`),
  startLogStream: (id: string, streamId: string) =>
    req<{ streamId: string }>(`/api/services/${id}/logs/stream`, { method: 'POST', body: JSON.stringify({ streamId }) }),
  stopLogStream: (id: string, streamId: string) =>
    req<void>(`/api/services/${id}/logs/stream?streamId=${encodeURIComponent(streamId)}`, { method: 'DELETE' }),
  serviceDeployments: (id: string) => req<Deployment[]>(`/api/services/${id}/deployments`),
	serviceOperations: (id: string) => req<{ id: string; kind: string; state: string; createdAt: string }[]>(`/api/services/${id}/operations`),
	resolveServiceOperation: (id: string, operation: string) => req(`/api/services/${id}/operations/${operation}/resolve`, { method: 'POST', body: JSON.stringify({ inspected: true }) }),
  domains: () => req<Domain[]>('/api/domains'),
  createDomain: (name: string) => req<Domain>('/api/domains', { method: 'POST', body: JSON.stringify({ name }) }),
  deleteDomain: (id: string) => req<void>(`/api/domains/${id}`, { method: 'DELETE' }),
  verifyDomain: (id: string) => req<Domain>(`/api/domains/${id}/verify`, { method: 'POST' }),
  networkDiagnosis: () => req<NetworkDiagnosis>('/api/network/diagnosis'),
  ingress: () => req<IngressConfig>('/api/ingress'),
  setIngress: (cfg: IngressConfig) => req<IngressConfig>('/api/ingress', { method: 'POST', body: JSON.stringify(cfg) }),
  setDashboardTunnel: (enabled: boolean) =>
    req<IngressConfig | { status: string }>('/api/network/dashboard-tunnel', { method: 'POST', body: JSON.stringify({ enabled }) }),
  setServiceDomain: (id: string, domain: string, ingress: string) =>
    req<Service>(`/api/services/${id}/domain`, { method: 'POST', body: JSON.stringify({ domain, ingress }) }),
  exposeService: (id: string) =>
    req<{ domain: string; url: string }>(`/api/services/${id}/expose`, { method: 'POST' }),
  startQuickTunnel: (id: string) =>
    req<{ status: string }>(`/api/services/${id}/quick-tunnel`, { method: 'POST' }),
  stopQuickTunnel: (id: string) => req<void>(`/api/services/${id}/quick-tunnel`, { method: 'DELETE' }),
  frontDoor: () => req<FrontDoorConfig>('/api/frontdoor'),
  setFrontDoor: (enabled: boolean, workspace?: string) =>
    req<FrontDoorConfig>('/api/frontdoor', { method: 'POST', body: JSON.stringify({ enabled, workspace }) }),
  enableServiceFrontDoor: (id: string) =>
    req<{ domain: string; url: string }>(`/api/services/${id}/frontdoor`, { method: 'POST' }),
  disableServiceFrontDoor: (id: string) =>
    req<void>(`/api/services/${id}/frontdoor`, { method: 'DELETE' }),
  mesh: () => req<MeshStatus>('/api/mesh'),
  cluster: () => req<ClusterStatus>('/api/cluster'),
  authStatus: () => req<{ needsSetup: boolean }>('/api/auth/status'),
  signup: (email: string, fullName: string, password: string, setupToken?: string) =>
    req<{ token: string; user: User }>('/api/auth/signup', { method: 'POST', body: JSON.stringify({ email, fullName, password, setupToken }) }),
  login: (email: string, password: string) =>
    req<{ token: string; user: User }>('/api/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  users: () => req<User[]>('/api/users'),
  versionCheck: (force = false) => req<VersionInfo>(`/api/version-check${force ? '?force=true' : ''}`),
  applyUpdate: () => req<{ ok: boolean; version: string; restarting: boolean }>('/api/update', { method: 'POST' }),
  settings: () => req<Settings>('/api/settings'),
  setSettings: (s: Settings) => req<void>('/api/settings', { method: 'POST', body: JSON.stringify(s) }),
  tokens: () => req<APIToken[]>('/api/tokens'),
  createApiToken: (name: string, expiresIn?: string) =>
    req<{ id: string; name: string; token: string; expiresAt?: string }>('/api/tokens', { method: 'POST', body: JSON.stringify({ name, expiresIn }) }),
  deleteApiToken: (id: string) => req<void>(`/api/tokens/${id}`, { method: 'DELETE' }),
  me: () => req<User>('/api/auth/me'),
  changePassword: async (current: string, newPassword: string) => {
    await req<void>('/api/auth/change-password', { method: 'POST', body: JSON.stringify({ current, new: newPassword }) });
    clearToken();
    window.dispatchEvent(new Event('rackops-logout'));
  },
  forgotPassword: (email: string) =>
    req<{ ok: boolean; message: string }>('/api/auth/forgot-password', { method: 'POST', body: JSON.stringify({ email }) }),
  resetPassword: (token: string, password: string) =>
    req<{ token: string; user: User }>('/api/auth/reset-password', { method: 'POST', body: JSON.stringify({ token, password }) }),
  assistant: (message: string, history?: ChatTurn[], page?: string) =>
    req<{ answer: string; provider: string }>('/api/assistant', { method: 'POST', body: JSON.stringify({ message, history, page }) }),
  testAssistant: () => req<{ ok: boolean; provider: string; error?: string }>('/api/assistant/test', { method: 'POST' }),
  joinCommand: () => req<{ command: string; installCommand?: string; token: string }>('/api/join-command'),
  // Organizations
  orgs: () => req<Organization[]>('/api/orgs'),
  createOrg: (name: string) => req<Organization>('/api/orgs', { method: 'POST', body: JSON.stringify({ name }) }),
  renameOrg: (id: string, name: string) => req<Organization>(`/api/orgs/${id}`, { method: 'PATCH', body: JSON.stringify({ name }) }),
  deleteOrg: (id: string) => req<void>(`/api/orgs/${id}`, { method: 'DELETE' }),
  setOrgMember: (orgId: string, userId: string, patch: { role?: string; permissions?: OrgPermissions }) =>
    req<Organization>(`/api/orgs/${orgId}/members/${userId}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  removeOrgMember: (orgId: string, userId: string) =>
    req<void>(`/api/orgs/${orgId}/members/${userId}`, { method: 'DELETE' }),
  invitations: (orgId: string) => req<Invitation[]>(`/api/orgs/${orgId}/invitations`),
  createInvitation: (orgId: string, email: string, role: string) =>
    req<{ invitation: Invitation; link: string }>(`/api/orgs/${orgId}/invitations`, { method: 'POST', body: JSON.stringify({ email, role }) }),
  deleteInvitation: (orgId: string, invId: string) =>
    req<void>(`/api/orgs/${orgId}/invitations/${invId}`, { method: 'DELETE' }),
  invitationInfo: (token: string) =>
    req<{ email: string; org: string; role: string }>(`/api/auth/invitation/${encodeURIComponent(token)}`),
  acceptInvitation: (token: string, fullName: string, password: string) =>
    req<{ token: string; user: User; org: string }>('/api/auth/invitation', { method: 'POST', body: JSON.stringify({ token, fullName, password }) }),
  // Support
  tickets: () => req<SupportTicket[]>('/api/support/tickets'),
  ticket: (id: string) => req<SupportTicket>(`/api/support/tickets/${id}`),
  createTicket: (t: { subject: string; category: string; severity: string; body: string }) =>
    req<SupportTicket>('/api/support/tickets', { method: 'POST', body: JSON.stringify(t) }),
  replyTicket: (id: string, body: string) =>
    req<SupportTicket>(`/api/support/tickets/${id}/reply`, { method: 'POST', body: JSON.stringify({ body }) }),
  resolveTicket: (id: string) =>
    req<SupportTicket>(`/api/support/tickets/${id}/resolve`, { method: 'POST' }),
};

// subscribe opens the SSE stream and dispatches typed events. onOpen/onError
// report the live connection state so the UI can tell "nothing is happening"
// apart from "we lost the server".
export function subscribe(
  handlers: Record<string, (data: unknown) => void>,
  opts?: { onOpen?: () => void; onError?: () => void },
): () => void {
  const tok = getToken();
  const org = getActiveOrg();
  const params = new URLSearchParams();
  if (tok) params.set('token', tok);
  if (org) params.set('org', org);
  const qs = params.toString();
  const es = new EventSource('/api/stream' + (qs ? `?${qs}` : ''));
  for (const [event, fn] of Object.entries(handlers)) {
    es.addEventListener(event, (e) => fn(JSON.parse((e as MessageEvent).data)));
  }
  if (opts?.onOpen) es.addEventListener('open', opts.onOpen);
  // EventSource retries on its own; this only reports that it is currently down.
  if (opts?.onError) es.addEventListener('error', opts.onError);
  return () => es.close();
}

// LogChunk is one increment of a live log stream (deploy build output or a
// runtime `logs -f` tail), pushed over SSE as a `log_chunk` event.
export interface LogChunk {
  stream: string;
  serviceId?: string;
  kind: 'deploy' | 'runtime';
  text?: string;
  end?: boolean;
}

// subscribeLogChunks opens the SSE stream and invokes onChunk for every
// `log_chunk` event matching the given predicate (by stream ID or service).
// Returns an unsubscribe function. Used for real-time deploy + runtime logs.
export function subscribeLogChunks(match: (c: LogChunk) => boolean, onChunk: (c: LogChunk) => void): () => void {
  return subscribe({
    log_chunk: (data) => {
      const c = data as LogChunk;
      if (match(c)) onChunk(c);
    },
  });
}
