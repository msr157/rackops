# RackOps product context

Verified source baseline: `9f14a6f` on main, 2026-09-05. Implementation status
is tracked in STABILIZATION.md. Code presence is not production verification.
The stabilization implementation is currently uncommitted local work above
that baseline; no production release of these changes has been verified.

## Purpose and customer

RackOps gives small teams one place to discover, deploy, connect and operate
their infrastructure without a dedicated infrastructure team. The product
direction is **your infrastructure, wherever it runs**, with a self-hosted
control plane and customer-owned machines and cloud accounts.

## Products

- Lite: a Go control plane and agents, embedded React dashboard, telemetry,
  placement, Docker/image/Git/Compose deployments, templates, networking,
  organizations, API keys, logs, assistant, support and updates.
- Admin: vendor-side support tickets, reset-email relay, optional usage
  snapshots and Front Door tunnel management. SQLite persists CRM records;
  a separate JSON registry persists tunnel identities and quotas.
- Front Door: vendor-operated public HTTPS over outbound customer tunnels.
  TLS terminates at the vendor edge; it must not be described as an
  end-to-end encrypted application channel or an entirely offline feature.
- Enterprise: a placeholder product directory. Do not imply that an
  independently validated enterprise edition, SSO or compliance program ships.

## Agreed migration scope

First release: RackOps-managed workloads to Linux Docker VMs in AWS, Azure
and GCP, with equal provider coverage at launch. Include stateless workloads,
quiesced filesystem volumes, and explicitly supported PostgreSQL/MySQL
backup and restore. Allow a planned write interruption.

The journey is select apps, select cloud/region, review compatibility/cost/
downtime, provision, transfer, validate, switch traffic and retain recovery
material. Provisioning requires approval of a concrete resource and cost plan.
Never roll back to stale source storage after destination writes begin.

The control plane remains in place. Arbitrary VM import, managed database
conversion, Kubernetes migration, GPU migration and control-plane relocation
are outside the first release. There is currently no finished migration engine.

## Business facts and dependencies

Customer count, revenue, production availability and cloud test accounts are
unverified. Do not infer them from marketing copy or repository history.
Shipping depends on tested restore, task durability, tenant isolation, signed
releases, deployment gates and actual provider-account validation.

The repository is authoritative. A sanitized GitHub Wiki should mirror the
purpose, feature status and roadmap; security records and customer details
must not be published there.
