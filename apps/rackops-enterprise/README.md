# RackOps Enterprise

**Future product — not yet started.**

RackOps Enterprise will build on RackOps Lite with organization-scale features
(multi-tenant orgs, RBAC/SSO, cluster orchestration, GPU scheduling, policies,
audit logs).

This directory reserves its place in the monorepo:

```
apps/rackops-enterprise/
  backend/     Go control plane (planned)
  dashboard/   React dashboard (planned)
```

When work starts, add the dashboard to `pnpm-workspace.yaml` and the backend
module to `go.work` at the repo root.
