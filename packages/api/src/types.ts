// Wire types for the RackOps control-plane API.
// KEEP IN SYNC with internal/api/types.go — that file is the source of truth.

export interface CPUInfo {
  model: string;
  cores: number;
  usagePct: number;
}

export interface MemInfo {
  totalBytes: number;
  usedBytes: number;
  usedPct: number;
}

export interface DiskInfo {
  mountpoint: string;
  fs: string;
  totalBytes: number;
  usedBytes: number;
  usedPct: number;
}

export interface NetIface {
  name: string;
  ipv4: string;
}

export interface Snapshot {
  hostname: string;
  os: string;
  platform: string;
  arch: string;
  uptimeSec: number;
  cpu: CPUInfo;
  memory: MemInfo;
  disks: DiskInfo[];
  network: NetIface[];
  collectedAt: string;
}

export interface EarlyAccessRequest {
  email: string;
  name: string;
  industry: string;
  notes?: string;
}

export type NodeStatus = 'online' | 'stale';

export interface Node {
  id: string;
  hostname: string;
  status: NodeStatus;
  lastSeen: string;
  latest: Snapshot;
}

export interface Meta {
  edition: 'lite' | 'enterprise';
  version: string;
}
