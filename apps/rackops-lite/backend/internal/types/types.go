// Package types defines the shared data models for RackOps.
package types

import (
	"strings"
	"time"
)

// ---- Node & Hardware ----

// NodeRole describes a node's role in the cluster.
type NodeRole string

const (
	RoleServer NodeRole = "server" // control-plane member
	RoleWorker NodeRole = "worker" // agent-only node
)

// NodeStatus is the liveness state of a node.
type NodeStatus string

const (
	NodeOnline  NodeStatus = "online"
	NodeOffline NodeStatus = "offline"
	NodeJoining NodeStatus = "joining"
)

// Node is a machine in the fleet.
type Node struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Role      NodeRole   `json:"role"`
	Status    NodeStatus `json:"status"`
	OrgID     string     `json:"orgId,omitempty"` // owning organization
	IP        string     `json:"ip"`              // LAN/public IP
	MeshIP    string     `json:"meshIp"`          // WireGuard mesh IP (10.77.x.x)
	PublicIP  string     `json:"publicIp,omitempty"`
	OS        string     `json:"os"`
	Arch      string     `json:"arch"`
	Kernel    string     `json:"kernel"`
	Hostname  string     `json:"hostname"`
	JoinedAt  time.Time  `json:"joinedAt"`
	LastSeen  time.Time  `json:"lastSeen"`
	AgentVer  string     `json:"agentVersion"`
	HasPublic bool       `json:"hasPublicIp"` // eligible as relay/ingress node
	Hardware  Hardware   `json:"hardware"`
	Metrics   Metrics    `json:"metrics"`
}

// Hardware is the discovered, mostly-static hardware inventory of a node.
type Hardware struct {
	CPUModel   string  `json:"cpuModel"`
	CPUCores   int     `json:"cpuCores"`
	CPUThreads int     `json:"cpuThreads"`
	MemTotalMB int64   `json:"memTotalMb"`
	GPUs       []GPU   `json:"gpus"`
	Drives     []Drive `json:"drives"`
	NICs       []NIC   `json:"nics"`
}

// GPU is a discovered graphics/compute accelerator.
type GPU struct {
	Index     int     `json:"index"`
	Vendor    string  `json:"vendor"` // nvidia | amd | intel
	Model     string  `json:"model"`
	VRAMTotal int64   `json:"vramTotalMb"`
	VRAMUsed  int64   `json:"vramUsedMb"`
	UtilPct   float64 `json:"utilPct"`
	TempC     float64 `json:"tempC"`
	PowerW    float64 `json:"powerW"`
}

// Drive is a physical block device.
type Drive struct {
	Device     string  `json:"device"` // /dev/nvme0n1
	Model      string  `json:"model"`
	Type       string  `json:"type"` // nvme | ssd | hdd
	SizeGB     float64 `json:"sizeGb"`
	UsedGB     float64 `json:"usedGb"`
	MountPoint string  `json:"mountPoint,omitempty"`
	ReadMBps   float64 `json:"readMbps"`  // live throughput
	WriteMBps  float64 `json:"writeMbps"` // live throughput
	TempC      float64 `json:"tempC,omitempty"`
	Health     string  `json:"health"` // ok | warning | critical | unknown
}

// NIC is a network interface.
type NIC struct {
	Name    string  `json:"name"`
	MAC     string  `json:"mac"`
	SpeedMb int64   `json:"speedMb"` // link speed, -1 unknown
	RxMBps  float64 `json:"rxMbps"`
	TxMBps  float64 `json:"txMbps"`
}

// Metrics is the live utilization snapshot pushed by an agent.
type Metrics struct {
	Timestamp  time.Time `json:"timestamp"`
	CPUPct     float64   `json:"cpuPct"`
	Load1      float64   `json:"load1"`
	Load5      float64   `json:"load5"`
	Load15     float64   `json:"load15"`
	MemUsedMB  int64     `json:"memUsedMb"`
	MemPct     float64   `json:"memPct"`
	SwapUsedMB int64     `json:"swapUsedMb"`
	TempC      float64   `json:"tempC"` // hottest package sensor
	UptimeSec  int64     `json:"uptimeSec"`
	Containers int       `json:"containers"` // running containers
	ProcessCnt int       `json:"processCount"`
}

// ---- Projects / Environments / Services ----

// Project is the top-level logical grouping (a "project folder").
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OrgID       string    `json:"orgId,omitempty"` // owning organization
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	SharedEnv   []EnvVar  `json:"sharedEnv"` // project-level vars inherited by all envs
}

// Environment lives inside a project (production, staging, dev, custom).
type Environment struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// EnvVar is a key/value pair; Secret values are masked in the UI.
type EnvVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// ServiceKind is what sort of workload a service is.
type ServiceKind string

const (
	KindApp      ServiceKind = "app"      // git or image deploy
	KindDatabase ServiceKind = "database" // managed DB
	KindCompose  ServiceKind = "compose"  // docker-compose stack
	KindTemplate ServiceKind = "template" // catalog one-click
)

// ServiceStatus is the lifecycle state of a service.
type ServiceStatus string

const (
	StatusPending  ServiceStatus = "pending"
	StatusBuilding ServiceStatus = "building"
	StatusRunning  ServiceStatus = "running"
	StatusStopped  ServiceStatus = "stopped"
	StatusFailed   ServiceStatus = "failed"
)

// MountType distinguishes storage mount kinds.
type MountType string

const (
	MountBind   MountType = "bind"   // host folder -> container path
	MountVolume MountType = "volume" // rackops-managed dir on a chosen drive
)

// Mount attaches node storage to a container path.
type Mount struct {
	Type     MountType `json:"type"`
	Source   string    `json:"source"` // bind: host path · volume: volume name
	Target   string    `json:"target"` // container path
	ReadOnly bool      `json:"readOnly"`
	// DrivePath (volume only): mount point of the drive the volume lives on
	// (e.g. /mnt/bigdisk). Empty = default docker volume storage.
	DrivePath string `json:"drivePath,omitempty"`
}

// IngressType controls how a service is exposed.
type IngressType string

const (
	IngressNone   IngressType = "none"   // internal only
	IngressStatic IngressType = "static" // Traefik on node's public IP
	IngressRelay  IngressType = "relay"  // tunneled via a public-IP fleet node
)

// Service is a deployed workload (app, database, compose stack, or template).
type Service struct {
	ID        string        `json:"id"`
	ProjectID string        `json:"projectId"`
	EnvID     string        `json:"envId"`
	Name      string        `json:"name"`
	Kind      ServiceKind   `json:"kind"`
	Status    ServiceStatus `json:"status"`

	// Source (one of)
	GitRepo    string `json:"gitRepo,omitempty"`
	GitBranch  string `json:"gitBranch,omitempty"`
	GitToken   string `json:"gitToken,omitempty"` // deploy token, never sent to UI
	Image      string `json:"image,omitempty"`
	ComposeYML string `json:"composeYml,omitempty"`
	TemplateID string `json:"templateId,omitempty"`

	// Adopted marks a service RackOps discovered already running on a node
	// (created outside RackOps). RackOps manages its lifecycle (start/stop/
	// restart/remove/logs) by acting on the real container, but never rebuilds
	// or auto-heals it — it has no deploy recipe for something it didn't create.
	Adopted bool `json:"adopted,omitempty"`
	// ExternalName is the real container name to target for adopted services
	// (their container isn't named/labelled by RackOps).
	ExternalName string `json:"externalName,omitempty"`

	// Placement
	NodeID     string `json:"nodeId"`               // where it runs
	AutoPlace  bool   `json:"autoPlace"`            // scheduler picked the node
	PinnedPath string `json:"pinnedPath,omitempty"` // mount point of pinned drive
	// RelayPort is the local port on the ingress node that tunnels to this
	// service over the mesh (assigned when the service is on a remote node).
	RelayPort int `json:"relayPort,omitempty"`

	// Runtime
	Port          int         `json:"port"`                    // published host port (what you reach it on / routing uses)
	ContainerPort int         `json:"containerPort,omitempty"` // port the app listens on inside the container
	Ports         []int       `json:"ports,omitempty"`         // extra published ports (host==container)
	Command       string      `json:"command,omitempty"`       // container command override
	Volumes       []string    `json:"volumes,omitempty"`       // legacy template paths (auto-converted to Mounts)
	Mounts        []Mount     `json:"mounts,omitempty"`        // storage attachments
	Domain        string      `json:"domain"`                  // chosen domain (empty = generated)
	Ingress       IngressType `json:"ingress"`
	// QuickTunnelURL is a temporary trycloudflare.com URL (testing only —
	// changes on every tunnel restart; not for production traffic).
	QuickTunnelURL string   `json:"quickTunnelUrl,omitempty"`
	Env            []EnvVar `json:"env"`
	CPUPct         float64  `json:"cpuPct"`
	MemUsedMB      int64    `json:"memUsedMb"`
	MemLimitMB     int64    `json:"memLimitMb,omitempty"`
	CPULimit       float64  `json:"cpuLimit,omitempty"`      // cores, 0 = unlimited
	RestartPol     string   `json:"restartPolicy,omitempty"` // unless-stopped | always | on-failure | no
	GPU            bool     `json:"gpu,omitempty"`
	// LogsClearedAt: UI-level log clearing — fetches use --since this time.
	LogsClearedAt time.Time `json:"logsClearedAt,omitempty"`

	ContainerID string    `json:"containerId,omitempty"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Deployment is one build+release attempt of a service.
type Deployment struct {
	ID         string        `json:"id"`
	ServiceID  string        `json:"serviceId"`
	Version    int           `json:"version"`
	Status     ServiceStatus `json:"status"`
	Trigger    string        `json:"trigger"` // manual | webhook | rollback
	CommitSHA  string        `json:"commitSha,omitempty"`
	Image      string        `json:"image,omitempty"`
	Log        string        `json:"log"`
	StartedAt  time.Time     `json:"startedAt"`
	FinishedAt time.Time     `json:"finishedAt,omitempty"`
	DurationMS int64         `json:"durationMs"`
}

// ---- Placement ----

// PlacementRequest asks the scheduler where a workload should run.
type PlacementRequest struct {
	CPUMilli   int    `json:"cpuMilli"`  // requested mCPU (0 = unknown)
	MemMB      int64  `json:"memMb"`     // requested memory
	StorageGB  int64  `json:"storageGb"` // persistent storage needed
	NeedsGPU   bool   `json:"needsGpu"`
	MinVRAMMB  int64  `json:"minVramMb,omitempty"`
	PreferNVMe bool   `json:"preferNvme"`
	OrgID      string `json:"-"` // server-set: restrict to one org's machines
}

// PlacementScore is one node's suitability verdict.
type PlacementScore struct {
	NodeID  string   `json:"nodeId"`
	Score   float64  `json:"score"` // 0..100
	Reasons []string `json:"reasons"`
	Viable  bool     `json:"viable"`
}

// ---- Alerts & Events ----

// AlertLevel is the severity of an alert.
type AlertLevel string

const (
	AlertInfo     AlertLevel = "info"
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

// Alert is a threshold or health event raised by the alert engine.
type Alert struct {
	ID        string     `json:"id"`
	NodeID    string     `json:"nodeId,omitempty"`
	ServiceID string     `json:"serviceId,omitempty"`
	Level     AlertLevel `json:"level"`
	Kind      string     `json:"kind"` // cpu_high | mem_high | disk_full | thermal | node_down | deploy_failed ...
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"createdAt"`
	Resolved  bool       `json:"resolved"`
}

// Event is an entry in the live system log feed.
type Event struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	NodeID    string    `json:"nodeId,omitempty"`
	ServiceID string    `json:"serviceId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---- Domains & Ingress ----

// DomainStatus is a domain's DNS verification state.
type DomainStatus string

const (
	DomainPending       DomainStatus = "pending"       // not resolving yet (record missing or propagating)
	DomainVerified      DomainStatus = "verified"      // resolves to the ingress node's IP
	DomainMisconfigured DomainStatus = "misconfigured" // resolves somewhere else
)

// Domain is a user-owned domain name usable in the per-deployment dropdown.
// The server continuously verifies its DNS against the ingress node's IP and
// probes the edge so misconfiguration surfaces instead of failing silently.
type Domain struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`            // e.g. app.example.com or *.example.com
	Wildcard  bool      `json:"wildcard"`        // name starts with "*." — covers one subdomain level
	OrgID     string    `json:"orgId,omitempty"` // owning organization
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`

	// Verification results (server-maintained; replicated like all state).
	Status      DomainStatus `json:"status"`
	Message     string       `json:"message,omitempty"`     // human explanation of the current state
	ExpectedIP  string       `json:"expectedIp,omitempty"`  // ingress node IP the record must point at
	ResolvedIPs []string     `json:"resolvedIps,omitempty"` // what the name currently resolves to
	EdgeOK      bool         `json:"edgeOk"`                // HTTP probe confirmed OUR edge answers on that IP
	CertOK      bool         `json:"certOk"`                // TLS probe found a valid non-default cert
	CertIssuer  string       `json:"certIssuer,omitempty"`
	CertExpiry  time.Time    `json:"certExpiry,omitempty"`
	LastChecked time.Time    `json:"lastChecked,omitempty"`
}

// WildcardBase returns the suffix a wildcard domain covers ("" if not wildcard).
func (d *Domain) WildcardBase() string {
	if !d.Wildcard {
		return ""
	}
	return strings.TrimPrefix(d.Name, "*.")
}

// IngressConfig is the cluster's edge configuration.
type IngressConfig struct {
	Enabled   bool   `json:"enabled"`
	NodeID    string `json:"nodeId"`    // node running Traefik (must have public IP or LAN reachability)
	HTTPPort  int    `json:"httpPort"`  // default 80
	HTTPSPort int    `json:"httpsPort"` // default 443
	ACMEEmail string `json:"acmeEmail"` // enables Let's Encrypt when set

	// Runtime status (server-maintained, not user input).
	Status     string    `json:"status,omitempty"`     // pending | running | failed
	StatusMsg  string    `json:"statusMsg,omitempty"`  // last deploy log tail / error
	LastDeploy time.Time `json:"lastDeploy,omitempty"` // when Traefik last (re)deployed
}

// AutoDomainSuffix is the wildcard base for instant, zero-DNS URLs.
// <service>.<ingress-ip-with-dashes>.<suffix> resolves to the ingress IP,
// e.g. whoami.203-0-113-9.nip.io — no domain ownership or DNS setup needed.
const AutoDomainSuffix = "nip.io"

// ---- Users & Auth ----

// UserRole is the platform-level RBAC role.
type UserRole string

const (
	RoleOwner  UserRole = "owner"
	RoleAdmin  UserRole = "admin"
	RoleMember UserRole = "member"
	RoleViewer UserRole = "viewer"
)

// User is an operator account. PasswordHash must replicate across the cluster
// (raft state), so it is serialized; API responses strip it via Sanitized().
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	FullName     string    `json:"fullName"`
	PasswordHash string    `json:"passwordHash,omitempty"`
	Role         UserRole  `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	// Password reset: only the SHA-256 of the reset token is stored, and it is
	// single-use — consumed (cleared) on a successful reset or on expiry.
	ResetTokenHash string    `json:"resetTokenHash,omitempty"`
	ResetExpires   time.Time `json:"resetExpires,omitempty"`
}

// Sanitized returns a copy safe to send to API clients (no password hash,
// no reset-token material).
func (u *User) Sanitized() *User {
	c := *u
	c.PasswordHash = ""
	c.ResetTokenHash = ""
	c.ResetExpires = time.Time{}
	return &c
}

// APIToken is a bearer token for the REST API / CLI. TokenHash replicates
// (raft state) so tokens validate cluster-wide; Sanitized() strips it for API.
// Every token belongs to a user and acts with that user's role — a key is
// never more powerful than the account that created it.
type APIToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	TokenHash string    `json:"tokenHash,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	LastUsed  time.Time `json:"lastUsed,omitempty"`
	// ExpiresAt zero means the token never expires.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// Sanitized returns a copy safe for API clients (no token hash).
func (t *APIToken) Sanitized() *APIToken {
	c := *t
	c.TokenHash = ""
	return &c
}

// ---- Organizations ----

// OrgRole is a member's role INSIDE one organization, independent of the
// platform role. Platform owner/admin manage the install (updates, users,
// edge); org roles govern that org's projects, machines, and domains.
type OrgRole string

const (
	OrgRoleOwner  OrgRole = "owner"
	OrgRoleAdmin  OrgRole = "admin"
	OrgRoleMember OrgRole = "member"
)

// OrgPermissions are the Dokploy-style capability switches evaluated for
// org-role "member". Owners and admins implicitly hold every permission.
type OrgPermissions struct {
	CreateProjects bool `json:"createProjects"`
	DeleteProjects bool `json:"deleteProjects"`
	CreateServices bool `json:"createServices"`
	DeleteServices bool `json:"deleteServices"`
	ManageDomains  bool `json:"manageDomains"`
	ManageMachines bool `json:"manageMachines"` // join/rename/remove nodes
	AccessAPI      bool `json:"accessApi"`      // may create API keys
	// AccessedProjects restricts a member to specific projects. Empty means
	// every project in the org (Dokploy's checkbox-tree semantics).
	AccessedProjects []string `json:"accessedProjects,omitempty"`
}

// OrgMember links a user to an organization.
type OrgMember struct {
	UserID      string         `json:"userId"`
	Role        OrgRole        `json:"role"`
	Permissions OrgPermissions `json:"permissions"`
	AddedAt     time.Time      `json:"addedAt"`
}

// Organization is the multi-tenancy boundary: projects, machines, domains,
// notification targets, and Copilot settings are scoped to one org.
type Organization struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	CreatedBy string      `json:"createdBy"`
	CreatedAt time.Time   `json:"createdAt"`
	Members   []OrgMember `json:"members"`
}

// Member returns the membership entry for a user, or nil.
func (o *Organization) Member(userID string) *OrgMember {
	for i := range o.Members {
		if o.Members[i].UserID == userID {
			return &o.Members[i]
		}
	}
	return nil
}

// InvitationStatus tracks an invitation's lifecycle.
type InvitationStatus string

const (
	InvitePending  InvitationStatus = "pending"
	InviteAccepted InvitationStatus = "accepted"
)

// Invitation is a 48h single-use link that lets someone create an account
// already enrolled in an org — the self-hosted answer to email invites.
// Only the token's SHA-256 is stored; the raw token exists in the copy-link.
type Invitation struct {
	ID        string           `json:"id"`
	OrgID     string           `json:"orgId"`
	Email     string           `json:"email"`
	Role      OrgRole          `json:"role"`
	TokenHash string           `json:"tokenHash,omitempty"`
	Status    InvitationStatus `json:"status"`
	CreatedBy string           `json:"createdBy"`
	CreatedAt time.Time        `json:"createdAt"`
	ExpiresAt time.Time        `json:"expiresAt"`
}

// Sanitized strips the token hash for API responses.
func (i *Invitation) Sanitized() *Invitation {
	c := *i
	c.TokenHash = ""
	return &c
}

// ---- Support tickets ----

// TicketStatus mirrors the CRM's semantics: open = needs a support agent,
// pending = waiting on the customer, resolved = closed (manual).
type TicketStatus string

const (
	TicketOpen     TicketStatus = "open"
	TicketPending  TicketStatus = "pending"
	TicketResolved TicketStatus = "resolved"
)

// TicketMessage is one entry in a ticket thread. Author is "user" (someone on
// this install) or "agent" (RackOps support, fetched from the CRM).
type TicketMessage struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Name   string    `json:"name"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

// SupportTicket is a conversation with RackOps support. It lives here and is
// mirrored to the vendor CRM; PollSecret authenticates reply polling and must
// never leave the server (Sanitized strips it).
type SupportTicket struct {
	ID        string          `json:"id"`
	Subject   string          `json:"subject"`
	Category  string          `json:"category"` // general|deploys|networking|updates|billing|bug
	Severity  string          `json:"severity"` // low|normal|high|urgent
	Status    TicketStatus    `json:"status"`
	Email     string          `json:"email"` // reply-to for the human
	CreatedBy string          `json:"createdBy"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Messages  []TicketMessage `json:"messages"`
	// PollSecret is issued by the CRM at first forward (SHA-256 stored there);
	// it authenticates this install's reply polls for this ticket.
	PollSecret string `json:"pollSecret,omitempty"`
	// ForwardPending marks a forward that failed and awaits the retry loop.
	ForwardPending bool `json:"forwardPending,omitempty"`
}

// Sanitized returns a copy safe for API clients (no poll secret).
func (t *SupportTicket) Sanitized() *SupportTicket {
	c := *t
	c.PollSecret = ""
	return &c
}

// ---- Agent protocol ----

// AgentReport is what an agent pushes to the control plane each interval.
type AgentReport struct {
	NodeID      string            `json:"nodeId"`
	Name        string            `json:"name"`
	OrgID       string            `json:"orgId,omitempty"` // org this node was joined into (--org)
	Hardware    Hardware          `json:"hardware"`
	Metrics     Metrics           `json:"metrics"`
	Host        HostInfo          `json:"host"`
	Containers  []ContainerStatus `json:"containers"`
	AgentVer    string            `json:"agentVersion"`
	Mesh        *MeshInfo         `json:"mesh,omitempty"`
	TaskResults []TaskResult      `json:"taskResults,omitempty"`
}

// HostInfo is static host identity info.
type HostInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kernel   string `json:"kernel"`
	IP       string `json:"ip"`
	PublicIP string `json:"publicIp,omitempty"`
}

// MeshInfo carries mesh key exchange data in agent reports/responses.
type MeshInfo struct {
	PublicKey string     `json:"publicKey,omitempty"` // agent's WG public key
	MeshIP    string     `json:"meshIp,omitempty"`    // assigned by server
	HubKey    string     `json:"hubKey,omitempty"`    // server's WG public key
	HubAddr   string     `json:"hubAddr,omitempty"`   // host:udpport of the hub
	Peers     []MeshPeer `json:"peers,omitempty"`     // full peer list
	// ExposePorts (server->agent) lists local service ports to proxy onto
	// the mesh so ingress/relay nodes can reach them.
	ExposePorts []int `json:"exposePorts,omitempty"`
	// Relays (server->agent, ingress node only): local listeners to open
	// that forward over the mesh to services on other nodes.
	Relays []RelaySpec `json:"relays,omitempty"`
}

// RelaySpec asks an agent to forward 127.0.0.1:LocalPort -> TargetAddr (mesh).
type RelaySpec struct {
	LocalPort  int    `json:"localPort"`
	TargetAddr string `json:"targetAddr"` // meshIP:port
}

// MeshPeer is one node's mesh identity, shared with all members.
type MeshPeer struct {
	NodeID    string `json:"nodeId"`
	PublicKey string `json:"publicKey"`
	MeshIP    string `json:"meshIp"`
}

// ContainerStatus is the state of one container as seen by the agent.
type ContainerStatus struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Image     string  `json:"image"`
	State     string  `json:"state"` // running | exited | ...
	CPUPct    float64 `json:"cpuPct"`
	MemUsedMB int64   `json:"memUsedMb"`
	ServiceID string  `json:"serviceId,omitempty"` // rackops label
	// Tunnel marks a quick-tunnel (cloudflared) helper container; it belongs
	// to the service but must not drive the service's own status.
	Tunnel bool `json:"tunnel,omitempty"`
	// External marks a container NOT created by RackOps (no rackops label).
	// The server adopts these so pre-existing workloads show up and become
	// manageable; they're matched to services by container name, not label.
	External bool `json:"external,omitempty"`
	// HostPort is the first published host port (external containers only), so
	// an adopted app surfaces a reachable address.
	HostPort int `json:"hostPort,omitempty"`
}

// TaskKind is the type of work the control plane asks an agent to do.
type TaskKind string

const (
	TaskDeploy  TaskKind = "deploy"
	TaskStop    TaskKind = "stop"
	TaskStart   TaskKind = "start"
	TaskRestart TaskKind = "restart"
	TaskRemove  TaskKind = "remove"
	TaskLogs    TaskKind = "logs"
	TaskListDir TaskKind = "listdir"
	TaskBuild   TaskKind = "build"
	TaskBackup  TaskKind = "backup"
	// Log streaming: follow a service's runtime logs (docker logs -f) and push
	// them to the control plane out-of-band until told to stop.
	TaskLogsFollow TaskKind = "logs-follow"
	TaskLogsStop   TaskKind = "logs-stop"
	// Cloudflare quick tunnel (testing): run/stop a cloudflared container
	// that exposes the service at a random trycloudflare.com URL.
	TaskTunnelStart TaskKind = "tunnel-start"
	TaskTunnelStop  TaskKind = "tunnel-stop"
)

// AgentTask is a unit of work queued for an agent.
type AgentTask struct {
	ID        string    `json:"id"`
	Kind      TaskKind  `json:"kind"`
	ServiceID string    `json:"serviceId"`
	Payload   string    `json:"payload"` // JSON blob, kind-specific
	CreatedAt time.Time `json:"createdAt"`
}

// TaskResult reports the outcome of an executed AgentTask.
type TaskResult struct {
	TaskID      string `json:"taskId"`
	ServiceID   string `json:"serviceId,omitempty"` // echoes the task's service (correlates non-deploy results)
	OK          bool   `json:"ok"`
	Log         string `json:"log"`
	ContainerID string `json:"containerId,omitempty"`
	HostPort    int    `json:"hostPort,omitempty"` // actual host port the container was published on
}

// AgentResponse is the control plane's reply to an AgentReport push.
type AgentResponse struct {
	Tasks      []AgentTask `json:"tasks"`
	Mesh       *MeshInfo   `json:"mesh,omitempty"`
	LeaderAddr string      `json:"leaderAddr,omitempty"` // set when this server is not the Raft leader
}

// EarlyAccessLead is an enterprise early-access request submitted from the
// marketing site's form. The public website POSTs these cross-origin to
// /api/v1/early-access; the field names are the wire contract with
// apps/website/src/website/components/sections/CTA.tsx and must not change
// without updating that form.
type EarlyAccessLead struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Industry  string    `json:"industry"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
