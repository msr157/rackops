# Cloud migration implementation status

The app detail page now offers a read-only readiness review for AWS, Azure and
Google Cloud. `POST /api/services/{id}/migration-preflight` accepts provider,
region and optional architecture, with organization, project and deployment
capability checks. The review reads current source metadata without queuing
tasks or changing the active machine.

Findings cover unavailable/non-Linux source machines, unknown or changed CPU
architecture, adopted apps, GPU use, Compose, databases, attached storage,
missing deployment sources and active builds. Secret values and filesystem
paths are excluded from the response. Image metadata, container writable data,
privileges and external dependencies still require agent-level inspection.

This is an initial assessment, not a persisted or approved migration plan.
Provider region names are syntax-checked only. Account permissions, resource
availability, quotas, prices and destinations are not validated. Unknown cost,
data size and downtime are null. Every review explicitly disallows starting.

## AWS foundation added 2026-09-06

Settings now provides AWS account connection creation, listing, revalidation and
removal. These endpoints require organization machine-management permission.
Credentials are encrypted before entering Store, remain encrypted in replication
exports, and are never returned in API responses or sent to CRM. The original
state encryption key is required after restore. Cloud APIs currently reject HA
installations because key distribution and operation ownership are unfinished.

The official AWS Go v2 SDK implements STS identity validation, EC2 launch
permission dry-runs, instance creation and lookup by client token. The adapter
uses explicit supplied credentials, not the host's default credential chain.
Identity validation does not prove that the account can launch an instance.

Internal provisioning records persist the immutable account, region and resource
specification before a launch. Lost responses and interrupted submissions are
reconciled by lookup; an empty lookup never triggers an automatic replacement
launch. Region changes and operation-ID reuse with changed specifications are
rejected. This follows [AWS client-token behavior](https://docs.aws.amazon.com/ec2/latest/devguide/ec2-api-idempotency.html).

**There is no launch endpoint or completed migration workflow.** The EC2 adapter
and operation runner are internal groundwork, tested with fake provider clients.
The dashboard can validate AWS identities but cannot create machines. Image
inspection, resource inventory, prices, confirmation, agent health
checks, volume-ID tracking and cleanup must be completed before enabling launch.
The current launch request retains encrypted root storage on termination and
requests no public address; network egress must be validated in the final plan.

## Three-provider continuation

Azure and Google Cloud account connections now join AWS in Settings. Azure uses
explicit tenant/client credentials and checks subscription access; Google uses
an explicit service-account key and checks access to an active project. Google
credential-provided token URLs and external credential sources are rejected.
Cloud credentials remain encrypted and out of API responses and CRM.

The official SDK adapters implement VM creation and reconciliation for all
three providers behind a shared interface. Azure creates with `If-None-Match: *`
to avoid overwriting a VM, binds ownership tags to the resource specification,
and uses an existing explicitly selected NIC and managed image. Google uses a
stable request ID, records its operation name, and uses an explicitly selected
subnet and pinned image reference. Both retain boot storage; the Google request
adds no public IP or default service account. Azure NIC security, image
compatibility and provider resource availability still need preflight checks.

Shared durable records preserve account, region, immutable specification and
provider receipts. Tests simulate lost responses and restarts on all three
adapters, and exercise Azure/Google SDK request serialization with local HTTP
transports. These tests do not certify live provider behavior.

[Scoped agent enrollment](NODE_ENROLLMENT.md) now connects to the internal cloud
runner through a persisted, encrypted startup script. AWS user data, Azure
custom data and Google startup metadata carry the same single-use enrollment
proof across retries. Scripts require a version-pinned official Linux binary,
SHA-256 verification and a release supporting `--token-file`. No cloud
credential enters the script. Existing released installers are unchanged.

The selected image must already have systemd, curl, Git, a running Docker engine
and the Compose plugin. The script checks prerequisites and architecture before
installing the agent. Private VMs need outbound HTTPS to GitHub and the control
plane. Image preparation, egress validation and actual fresh-VM execution remain
unverified; script generation and shell syntax tests are not installation tests.

`GET /api/cloud-operations/{id}` reports sanitized provider receipts separately
from enrollment/heartbeat evidence, restricted to the owning organization's
machine managers. It reports expired enrollment, missing identity, heartbeat
timeouts, offline agents and incompatible OS/architecture. A connected agent
does not mean application health or data integrity has passed: `canMigrate`
remains false. Launch rejects expired/consumed grants, while reconciliation
remains available after expiry. No automatic replacement VM is created.

**The end-to-end migration workflow remains unavailable.** Resource inventory,
cost estimates and confirmation, fresh-VM bootstrap acceptance, backup/restore, data transfer,
application validation, traffic cutover, cancellation and resource cleanup are
still required. No real cloud account or billable resource was used in these
tests. Real provider acceptance requires authorized non-production accounts and
a spending limit.

## AWS selected-resource inspection

Settings → Cloud accounts → an AWS connection → **Check AWS machine resources**
now reads the selected AMI, instance type, subnet and security group, plus the
instance type's availability-zone offering. The form accepts the source CPU
architecture and planned root disk size. It reports Linux/image-state checks,
architecture mismatches, unexpected image disks, a too-small root disk,
unsupported accelerators/bare metal, unavailable/full/IPv6-only subnets,
security-group VPC mismatches and sizes not offered in the subnet's zone.

`POST /api/cloud-connections/{id}/machine-preflight` accepts `spec` with one
`aws` specification and `architecture` (`amd64` or `arm64`). This is read-only
and not a saved/approved plan. The existing connection identity is revalidated
before inspecting resources. Organization machine-management permission is
required; responses exclude credentials and provider error details. Revoked
access or a removed connection prevents returning the completed review.

In addition to STS identity access, the connection needs these EC2 read actions:
`ec2:DescribeImages`, `ec2:DescribeInstanceTypes`, `ec2:DescribeSubnets`,
`ec2:DescribeSecurityGroups`, and `ec2:DescribeInstanceTypeOfferings`.
The inspection does not call `RunInstances`, even in dry-run mode. A missing,
ambiguous or paginated selected-resource response fails closed.

`resourceCompatible` describes only the selected metadata checks. `canLaunch`
remains false and `monthlyCostEstimate` remains null. Zone offerings do not
guarantee current capacity. Image prerequisites, network reachability/rules,
firmware compatibility, launch permission, quotas, pricing and final approval
still need validation. Resource inspection for Azure and Google remains
unimplemented; the endpoint returns 501 for those connections. Their identity
validation and internal provisioning adapters remain available as described above.

Validation: Windows and Linux Lite tests pass with incompatible-resource,
incomplete-response, authorization, identity-change and secret-redaction cases.
Frontend build and lint pass (16 existing warnings). A browser smoke fixture
exercised disk blockers, compatible metadata, stale-review clearing and API
failure rendering against the actual form. Provider responses were local
fixtures; no live account or billable resource was used.
