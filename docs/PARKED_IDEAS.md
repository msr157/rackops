# RackOps — Parked Ideas & Feature Backlog

This document captures valuable ideas, architectural enhancements, and potential extensions discovered during implementation. To prevent scope creep and ensure core migration goals remain tightly focused, these items are parked here with impact and necessity ratings rather than being added to the active implementation scope.

## Evaluation Criteria
- **Impact Rating**: 
  - **High**: Significant user experience, performance, or operational reliability advantage.
  - **Medium**: Incremental improvement to usability, observability, or convenience.
  - **Low**: Aesthetic or minor workflow tweak.
- **Necessity Rating**:
  - **Required (Future Milestone)**: Mandatory for enterprise scale or post-v1 releases, but intentionally excluded from v1 launch scope.
  - **Nice-to-Have**: Valuable convenience, but v1 functions completely without it.
  - **Secondary**: Non-critical feature (e.g. advanced analytics, cosmetic customization).

---

## Parked Items

### 1. Cross-Cloud Hot Standby / Live Replication
- **Description**: Continuous log shipping (PostgreSQL WAL streaming / MySQL binlog replication) to keep a target cloud VM continuously warm before cutover, reducing downtime from minutes to seconds.
- **Impact**: High (reduces migration downtime window to near zero).
- **Necessity**: Nice-to-Have (for v1 launch, planned maintenance window with quiesce & backup/restore is explicitly specified and thoroughly robust; live streaming replication introduces complex network bridging and state synchronization).
- **Status**: Parked.

### 2. Multi-Region Auto-Failover Router Integration
- **Description**: Dynamic DNS or Anycast IP routing integration (e.g., Cloudflare DNS / Route53 health check failover) during cutover instead of Traefik reverse-proxy host re-routing.
- **Impact**: Medium (simplifies external DNS repointing across multi-cluster environments).
- **Necessity**: Secondary (Front Door and Traefik router updates already handle cutover seamlessly in v1).
- **Status**: Parked.

### 3. Granular Per-Table Database Selective Migration
- **Description**: Allow users to cherry-pick individual tables or schemas rather than moving the entire database container.
- **Impact**: Medium (convenient for partial dataset transfers).
- **Necessity**: Secondary (v1 prioritizes complete application stack preservation to avoid foreign key or relational integrity drift).
- **Status**: Parked.

### 4. GPU Machine Image Provisioning & Driver Auto-Injection
- **Description**: Sizing and automatic NVIDIA driver / CUDA toolkit injection for GPU-accelerated workloads (e.g. AI inference nodes) on AWS, Azure, and GCP.
- **Impact**: Medium (enables specialized machine learning workloads).
- **Necessity**: Secondary (explicitly outside v1 scope per COMPLETION_PLAN.md).
- **Status**: Parked.

### 5. Automated Cloud Resource Down-Sizing / Cost Optimizer Recommendations
- **Description**: Machine learning or heuristic-based recommendation engine that suggests smaller instance sizes or spot instances based on 7-day source telemetry.
- **Impact**: Medium (cost saving optimization).
- **Necessity**: Nice-to-Have (v1 calculates accurate transparent baseline cost quotes; dynamic right-sizing can be delivered post-launch).
- **Status**: Parked.

---
*Updated autonomously as implementation progresses.*
