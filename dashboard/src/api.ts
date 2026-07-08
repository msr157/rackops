// Shared API types mirrored from the Go backend.
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
  os: string; arch: string; kernel: string; hostname: string; joinedAt: string;
  lastSeen: string; agentVersion: string; hasPublicIp: boolean;
  hardware: Hardware; metrics: Metrics;
}
export interface Project { id: string; name: string; description: string; createdAt: string }
export interface Environment { id: string; projectId: string; name: string }
export interface EnvVar { key: string; value: string; secret: boolean }
export interface Service {
  id: string; projectId: string; envId: string; name: string; kind: string; status: string;
  gitRepo?: string; gitBranch?: string; image?: string; nodeId: string; autoPlace: boolean;
  port: number; domain: string; ingress: string; env: EnvVar[] | null;
  cpuPct: number; memUsedMb: number; containerId?: string; createdAt: string;
}
export interface Overview {
  nodesOnline: number; nodesTotal: number; servicesUp: number; servicesTotal: number;
  totalMemMb: number; usedMemMb: number; totalStorageGb: number; usedStorageGb: number;
  avgCpuPct: number; openAlerts: number;
}
export interface Alert { id: string; nodeId?: string; level: string; kind: string; message: string; createdAt: string; resolved: boolean }
export interface EventItem { id: string; kind: string; message: string; createdAt: string }
export interface PlacementScore { nodeId: string; score: number; reasons: string[]; viable: boolean }
export interface EnvDef { key: string; label: string; default: string; secret: boolean; required: boolean; generate?: boolean }
export interface Template {
  id: string; name: string; category: string; description: string; image: string;
  port: number; ports?: number[]; env?: EnvDef[]; volumes?: string[]; memMb: number;
  gpu?: boolean; notes?: string;
}
export interface Deployment {
  id: string; serviceId: string; version: number; status: string; trigger: string;
  log: string; startedAt: string; durationMs: number;
}

const base = '';

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(base + path, init);
  if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
  if (r.status === 204) return undefined as T;
  return r.json();
}

export const api = {
  overview: () => req<Overview>('/api/overview'),
  nodes: () => req<Node[]>('/api/nodes'),
  node: (id: string) => req<Node>(`/api/nodes/${id}`),
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
  serviceAction: (id: string, action: string) => req<Service>(`/api/services/${id}/${action}`, { method: 'POST' }),
  deleteService: (id: string) => req<void>(`/api/services/${id}`, { method: 'DELETE' }),
  placement: (body: object) => req<PlacementScore[]>('/api/placement', { method: 'POST', body: JSON.stringify(body) }),
  templates: () => req<Template[]>('/api/templates'),
  serviceLogs: (id: string) => req<{ logs: string }>(`/api/services/${id}/logs`),
  serviceDeployments: (id: string) => req<Deployment[]>(`/api/services/${id}/deployments`),
  joinCommand: () => req<{ command: string; token: string }>('/api/join-command'),
};

// subscribe opens the SSE stream and dispatches typed events.
export function subscribe(handlers: Record<string, (data: unknown) => void>): () => void {
  const es = new EventSource('/api/stream');
  for (const [event, fn] of Object.entries(handlers)) {
    es.addEventListener(event, (e) => fn(JSON.parse((e as MessageEvent).data)));
  }
  return () => es.close();
}
