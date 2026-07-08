// Package types defines the shared data models for RackOps.
package types

import "time"

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
	IP        string     `json:"ip"`      // LAN/public IP
	MeshIP    string     `json:"meshIp"`  // WireGuard mesh IP (10.77.x.x)
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
	Index     int    `json:"index"`
	Vendor    string `json:"vendor"` // nvidia | amd | intel
	Model     string `json:"model"`
	VRAMTotal int64  `json:"vramTotalMb"`
	VRAMUsed  int64  `json:"vramUsedMb"`
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
	Timestamp   time.Time `json:"timestamp"`
	CPUPct      float64   `json:"cpuPct"`
	Load1       float64   `json:"load1"`
	Load5       float64   `json:"load5"`
	Load15      float64   `json:"load15"`
	MemUsedMB   int64     `json:"memUsedMb"`
	MemPct      float64   `json:"memPct"`
	SwapUsedMB  int64     `json:"swapUsedMb"`
	TempC       float64   `json:"tempC"` // hottest package sensor
	UptimeSec   int64     `json:"uptimeSec"`
	Containers  int       `json:"containers"` // running containers
	ProcessCnt  int       `json:"processCount"`
}

// ---- Projects / Environments / Services ----

// Project is the top-level logical grouping (a "project folder").
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
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

	// Placement
	NodeID     string `json:"nodeId"`               // where it runs
	AutoPlace  bool   `json:"autoPlace"`            // scheduler picked the node
	PinnedPath string `json:"pinnedPath,omitempty"` // mount point of pinned drive

	// Runtime
	Port      int         `json:"port"`    // internal container port
	Ports     []int       `json:"ports,omitempty"` // extra published ports
	Command   string      `json:"command,omitempty"` // container command override
	Volumes   []string    `json:"volumes,omitempty"` // container paths persisted under the data dir
	Domain    string      `json:"domain"`  // chosen domain (empty = generated)
	Ingress   IngressType `json:"ingress"`
	Env       []EnvVar    `json:"env"`
	CPUPct    float64     `json:"cpuPct"`
	MemUsedMB int64       `json:"memUsedMb"`
	MemLimitMB int64      `json:"memLimitMb,omitempty"`

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
	CPUMilli    int   `json:"cpuMilli"`    // requested mCPU (0 = unknown)
	MemMB       int64 `json:"memMb"`       // requested memory
	StorageGB   int64 `json:"storageGb"`   // persistent storage needed
	NeedsGPU    bool  `json:"needsGpu"`
	MinVRAMMB   int64 `json:"minVramMb,omitempty"`
	PreferNVMe  bool  `json:"preferNvme"`
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

// ---- Users & Auth ----

// UserRole is the platform-level RBAC role.
type UserRole string

const (
	RoleOwner  UserRole = "owner"
	RoleAdmin  UserRole = "admin"
	RoleMember UserRole = "member"
	RoleViewer UserRole = "viewer"
)

// User is an operator account.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	FullName     string    `json:"fullName"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

// APIToken is a bearer token for the REST API / CLI.
type APIToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	TokenHash string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	LastUsed  time.Time `json:"lastUsed,omitempty"`
}

// ---- Agent protocol ----

// AgentReport is what an agent pushes to the control plane each interval.
type AgentReport struct {
	NodeID     string            `json:"nodeId"`
	Name       string            `json:"name"`
	Hardware   Hardware          `json:"hardware"`
	Metrics    Metrics           `json:"metrics"`
	Host       HostInfo          `json:"host"`
	Containers []ContainerStatus `json:"containers"`
	AgentVer   string            `json:"agentVersion"`
	TaskResults []TaskResult     `json:"taskResults,omitempty"`
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

// ContainerStatus is the state of one container as seen by the agent.
type ContainerStatus struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Image     string  `json:"image"`
	State     string  `json:"state"` // running | exited | ...
	CPUPct    float64 `json:"cpuPct"`
	MemUsedMB int64   `json:"memUsedMb"`
	ServiceID string  `json:"serviceId,omitempty"` // rackops label
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
	TaskBuild   TaskKind = "build"
	TaskBackup  TaskKind = "backup"
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
	OK          bool   `json:"ok"`
	Log         string `json:"log"`
	ContainerID string `json:"containerId,omitempty"`
}

// AgentResponse is the control plane's reply to an AgentReport push.
type AgentResponse struct {
	Tasks    []AgentTask `json:"tasks"`
	LeaderAddr string    `json:"leaderAddr,omitempty"` // set when this server is not the Raft leader
}
