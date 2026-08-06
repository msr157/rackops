// Shared API types mirrored from the Go backend (internal/server/types.go).

export interface UsageSnapshot {
  version: string; os: string; arch: string;
  nodes: number; nodesOnline: number; services: number;
  servicesByKind: Record<string, number> | null;
  adopted: number; edgeEnabled: boolean; meshEnabled: boolean; ha: boolean;
  orgs: number; users: number; domains: number;
  featuresUsed: string[] | null;
}

export interface Install {
  id: string; name: string; version: string; os: string; arch: string;
  firstSeen: string; lastSeen: string;
  usage?: UsageSnapshot; openTickets: number;
}

export interface Snapshot { at: string; data: UsageSnapshot | null }

export type TicketStatus = 'open' | 'pending' | 'resolved';
export type TicketAuthor = 'user' | 'agent' | 'note';

export interface TicketMessage {
  id: string; author: TicketAuthor; name: string; body: string; at: string;
}

export interface Ticket {
  id: string; installId: string; installName?: string;
  subject: string; category: string; severity: string; status: TicketStatus;
  email: string; createdAt: string; updatedAt: string;
  messages?: TicketMessage[];
}

export interface VersionCount { version: string; count: number }
export interface FeatureCount { feature: string; count: number }

export interface Overview {
  installs: number; activeWeek: number; openTickets: number;
  versionDist: VersionCount[];
  featureUsage: FeatureCount[];
  byKindTotals: Record<string, number>;
  recentTickets: Ticket[] | null;
}

/** RackOps Front Door: the tunnel edge running inside this admin process. */
export interface FrontDoorStatus {
  enabled: boolean;
  reason?: string;
  popId?: string;
  zone?: string;
  publicAddr?: string;
  enrollToken?: string;
  tenants: number;
  connected: number;
  requests: number;
  bytes: number;
  certExpiry?: string;
  certDaysLeft: number;
  selfSigned: boolean;
}

export interface Tenant {
  id: string;
  publicKey: string;
  workspace: string;
  hostnames: string[] | null;
  quotaBytes: number;
  usedBytes: number;
  periodStart: string;
  suspended: boolean;
  suspendReason?: string;
  createdAt: string;
  updatedAt: string;
  connected: boolean;
  transport?: string;
}

export interface TunnelSession {
  tenantId: string;
  transport: string;
  remoteAddr: string;
  agentVersion?: string;
  openStreams: number;
  openedAt: string;
}

export interface InstallDetail {
  install: Install; snapshots: Snapshot[]; tickets: Ticket[];
}

const base = '';

// Session token management.
export function getToken(): string { return localStorage.getItem('crm_session') ?? ''; }
export function setToken(t: string) { localStorage.setItem('crm_session', t); }
export function clearToken() { localStorage.removeItem('crm_session'); }

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { ...(init?.headers as Record<string, string>) };
  const tok = getToken();
  if (tok) headers['Authorization'] = `Bearer ${tok}`;
  const r = await fetch(base + path, { ...init, headers });
  if (r.status === 401 && !path.startsWith('/api/auth/')) {
    clearToken();
    window.dispatchEvent(new Event('crm-logout'));
  }
  if (!r.ok) {
    let msg = `${r.status}`;
    try {
      const body = await r.json();
      if (body?.error) msg = body.error;
    } catch { /* opaque error */ }
    throw new Error(msg);
  }
  if (r.status === 204) return undefined as T;
  return r.json();
}

export const api = {
  login: (email: string, password: string) =>
    req<{ token: string }>('/api/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  overview: () => req<Overview>('/api/overview'),
  installs: () => req<Install[]>('/api/installs'),
  install: (id: string) => req<InstallDetail>(`/api/installs/${id}`),
  tickets: (status?: string) =>
    req<Ticket[]>(`/api/tickets${status ? `?status=${status}` : ''}`),
  ticket: (id: string, installId: string) =>
    req<Ticket>(`/api/tickets/${encodeURIComponent(id)}?installId=${encodeURIComponent(installId)}`),
  reply: (id: string, installId: string, body: string) =>
    req<Ticket>(`/api/tickets/${encodeURIComponent(id)}/reply?installId=${encodeURIComponent(installId)}`,
      { method: 'POST', body: JSON.stringify({ body }) }),
  note: (id: string, installId: string, body: string) =>
    req<Ticket>(`/api/tickets/${encodeURIComponent(id)}/note?installId=${encodeURIComponent(installId)}`,
      { method: 'POST', body: JSON.stringify({ body }) }),
  frontDoor: () => req<FrontDoorStatus>('/api/frontdoor'),
  tenants: () => req<Tenant[]>('/api/frontdoor/tenants'),
  tunnelSessions: () => req<TunnelSession[]>('/api/frontdoor/sessions'),
  suspendTenant: (id: string, suspended: boolean, reason?: string) =>
    req<{ suspended: boolean; reason: string }>(`/api/frontdoor/tenants/${encodeURIComponent(id)}/suspend`,
      { method: 'POST', body: JSON.stringify({ suspended, reason }) }),
  deleteTenant: (id: string) =>
    req<void>(`/api/frontdoor/tenants/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  setStatus: (id: string, installId: string, status: TicketStatus) =>
    req<Ticket>(`/api/tickets/${encodeURIComponent(id)}/status?installId=${encodeURIComponent(installId)}`,
      { method: 'POST', body: JSON.stringify({ status }) }),
};
