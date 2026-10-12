package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/migration"
	"github.com/rackops/rackops/internal/types"
)

var migrationContainerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

type migrationFence struct {
	JobID         string `json:"jobId"`
	ServiceID     string `json:"serviceId"`
	ContainerID   string `json:"containerId"`
	RestartPolicy string `json:"restartPolicy"`
}

func (a *Agent) migrationFencePath(serviceID string) string {
	id := sha256.Sum256([]byte(serviceID))
	return filepath.Join(a.journal.dir, hex.EncodeToString(id[:])+".migration-fence")
}

func migrationArtifactName(jobID, artifactID string) string {
	digest := sha256.Sum256([]byte(jobID + ":" + artifactID))
	return hex.EncodeToString(digest[:])
}

func (a *Agent) sourceArtifactPath(jobID, artifactID string) string {
	return filepath.Join(a.cfg.DataDir, "migration-artifacts", migrationArtifactName(jobID, artifactID)+".enc")
}

func (a *Agent) receiveArtifactDir(jobID, artifactID string) string {
	return filepath.Join(a.cfg.DataDir, "migration-receive", migrationArtifactName(jobID, artifactID))
}

func migrationServerBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid migration control-plane URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return "", errors.New("migration transfer requires HTTPS except on loopback development")
	}
	return u.String(), nil
}

func validMigrationHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func (a *Agent) migrationRequest(ctx context.Context, method, rawURL string, body io.Reader, headers map[string]string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Rackops-Token", a.cfg.Token)
	request.Header.Set("X-Rackops-Node", a.nodeID)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	client := *a.client
	client.Timeout = 2 * time.Minute
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(request)
}

func encryptedFileDigest(ctx context.Context, path string, expectedBytes int64, expectedSHA256 string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedBytes {
		return errors.New("encrypted artifact is missing or has an unexpected size")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, &migrationContextReader{ctx: ctx, reader: file}); err != nil {
		return err
	}
	if !bytes.Equal(hash.Sum(nil), mustDecodeHash(expectedSHA256)) {
		return errors.New("encrypted artifact digest does not match registered metadata")
	}
	return nil
}

func mustDecodeHash(value string) []byte { decoded, _ := hex.DecodeString(value); return decoded }

func (a *Agent) executeMigrationTransferTask(ctx context.Context, task types.AgentTask) *types.TaskResult {
	result := &types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID}
	fail := func(message string) *types.TaskResult { result.Log = message; result.Uncertain = true; return result }
	var payload types.MigrationTaskPayload
	if json.Unmarshal([]byte(task.Payload), &payload) != nil || payload.JobID == "" || payload.ServiceID != task.ServiceID || payload.ArtifactID == "" || payload.TransferBytes < 1 || !validMigrationHash(payload.TransferSHA256) {
		return fail("invalid migration transfer task")
	}
	base, err := migrationServerBase(a.cfg.ServerURL)
	if err != nil {
		return fail(err.Error())
	}
	endpoint := base + "/api/agent/migration-artifacts/" + url.PathEscape(payload.ArtifactID)
	if task.Kind == types.TaskMigrationTransferSource {
		path := a.sourceArtifactPath(payload.JobID, payload.ArtifactID)
		if err := encryptedFileDigest(ctx, path, payload.TransferBytes, payload.TransferSHA256); err != nil {
			return fail(err.Error())
		}
		response, err := a.migrationRequest(ctx, http.MethodGet, endpoint, nil, nil)
		if err != nil {
			return fail("could not retrieve durable transfer offset")
		}
		var remote struct {
			Offset int64 `json:"offset"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&remote)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || remote.Offset < 0 || remote.Offset > payload.TransferBytes {
			return fail("control-plane transfer offset is unavailable")
		}
		file, err := os.Open(path)
		if err != nil {
			return fail("encrypted artifact disappeared during transfer")
		}
		defer file.Close()
		for offset := remote.Offset; offset < payload.TransferBytes; {
			if err := ctx.Err(); err != nil {
				return fail("transfer interrupted; retry the same migration task")
			}
			size := min(int64(1<<20), payload.TransferBytes-offset)
			chunk := make([]byte, size)
			if _, err := file.ReadAt(chunk, offset); err != nil {
				return fail("could not read encrypted artifact chunk")
			}
			digest := sha256.Sum256(chunk)
			requestURL := endpoint + "?offset=" + strconv.FormatInt(offset, 10)
			response, err := a.migrationRequest(ctx, http.MethodPost, requestURL, bytes.NewReader(chunk), map[string]string{"Content-Type": "application/octet-stream", "X-Rackops-Chunk-SHA256": hex.EncodeToString(digest[:])})
			if err != nil {
				return fail("transfer upload interrupted; retry the same migration task")
			}
			var uploaded struct {
				Offset int64 `json:"offset"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&uploaded)
			response.Body.Close()
			if response.StatusCode != http.StatusOK || decodeErr != nil || uploaded.Offset != offset+size {
				return fail("transfer upload acknowledgement is invalid")
			}
			offset = uploaded.Offset
		}
		response, err = a.migrationRequest(ctx, http.MethodPost, endpoint+"/finalize", nil, nil)
		if err != nil {
			return fail("artifact upload finalization interrupted; retry the same migration task")
		}
		var finalized struct {
			Complete bool `json:"complete"`
		}
		decodeErr = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&finalized)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || !finalized.Complete {
			return fail("whole-artifact verification did not complete")
		}
		result.MigrationTransfer = &types.MigrationTransferResult{JobID: payload.JobID, ArtifactID: payload.ArtifactID, Phase: "source_uploaded"}
		result.OK, result.Log = true, "Encrypted artifact uploaded and verified; source remains fenced"
		return result
	}
	if task.Kind != types.TaskMigrationTransferDestination {
		return fail("unknown migration transfer task")
	}
	receiver, err := migration.OpenReceiver(a.receiveArtifactDir(payload.JobID, payload.ArtifactID), migration.TransferManifest{Bytes: payload.TransferBytes, SHA256: payload.TransferSHA256})
	if err != nil {
		return fail("private destination transfer storage is unavailable")
	}
	for {
		offset, err := receiver.Offset()
		if err != nil {
			return fail("destination transfer prefix is unavailable")
		}
		if offset == payload.TransferBytes {
			break
		}
		if err := ctx.Err(); err != nil {
			return fail("transfer interrupted; retry the same migration task")
		}
		response, err := a.migrationRequest(ctx, http.MethodGet, endpoint+"?offset="+strconv.FormatInt(offset, 10), nil, nil)
		if err != nil {
			return fail("transfer download interrupted; retry the same migration task")
		}
		chunk, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
		digest := response.Header.Get("X-Rackops-Chunk-SHA256")
		status := response.StatusCode
		response.Body.Close()
		if status != http.StatusOK || readErr != nil || len(chunk) == 0 || len(chunk) > 1<<20 {
			return fail("verified artifact is not available to the destination")
		}
		next, err := receiver.Append(ctx, offset, chunk, digest)
		if err != nil || next != offset+int64(len(chunk)) {
			return fail("destination rejected a transfer chunk")
		}
	}
	if _, err := receiver.Finalize(ctx); err != nil {
		return fail("destination encrypted artifact verification failed")
	}
	result.MigrationTransfer = &types.MigrationTransferResult{JobID: payload.JobID, ArtifactID: payload.ArtifactID, Phase: "destination_received"}
	result.OK, result.Log = true, "Encrypted artifact received and verified in private destination storage"
	return result
}

func (a *Agent) migrationFenced(serviceID string) bool {
	if a.journal == nil || serviceID == "" {
		return false
	}
	_, err := os.Lstat(a.migrationFencePath(serviceID))
	return !os.IsNotExist(err) // unreadable/corrupt storage must fail closed
}

func (a *Agent) readMigrationFence(serviceID string) (migrationFence, error) {
	var fence migrationFence
	if a.journal == nil {
		return fence, errors.New("migration journal unavailable")
	}
	path := a.migrationFencePath(serviceID)
	data, err := os.ReadFile(path)
	if err != nil {
		return fence, err
	}
	plain, err := a.journal.cipher.Open(nil, nil, data, []byte(filepath.Base(path)))
	if err != nil || json.Unmarshal(plain, &fence) != nil || fence.ServiceID != serviceID {
		return fence, errors.New("invalid migration fence")
	}
	return fence, nil
}

func ownedMigrationContainer(runtime *Runtime, serviceID, id string) error {
	if serviceID == "" || !migrationContainerID.MatchString(id) {
		return errors.New("invalid migration container")
	}
	data, err := runtime.command(runtime.Bin, "inspect", "--format", "{{json .Config.Labels}}", id).Output()
	var labels map[string]string
	if err != nil || json.Unmarshal(data, &labels) != nil || labels["rackops.service"] != serviceID || labels["com.docker.compose.project"] != "" {
		return errors.New("container is not a verified single-container RackOps workload")
	}
	return nil
}

func migrationContainerStopped(runtime *Runtime, id string) bool {
	data, err := runtime.command(runtime.Bin, "inspect", "--format", "{{.State.Running}}|{{.State.Restarting}}|{{.HostConfig.RestartPolicy.Name}}", id).Output()
	return err == nil && strings.TrimSpace(string(data)) == "false|false|no"
}

func (a *Agent) executeMigrationTask(ctx context.Context, runtime *Runtime, task types.AgentTask) *types.TaskResult {
	result := &types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID}
	fail := func(message string) *types.TaskResult { result.Log = message; result.Uncertain = true; return result }
	var payload types.MigrationTaskPayload
	if json.Unmarshal([]byte(task.Payload), &payload) != nil || payload.JobID == "" || len(payload.JobID) > 128 || payload.ServiceID != task.ServiceID || a.journal == nil {
		return fail("invalid migration task or unavailable durable journal")
	}
	defer clear(payload.BackupKey)
	if (task.Kind != types.TaskMigrationRestore && task.Kind != types.TaskMigrationValidate) || payload.Kind != "volume" || payload.ContainerID != "" {
		if payload.ContainerID != "" {
			if err := ownedMigrationContainer(runtime, task.ServiceID, payload.ContainerID); err != nil {
				return fail(err.Error())
			}
		}
	}
	if task.Kind == types.TaskMigrationQuiesce {
		if err := ownedMigrationContainer(runtime, task.ServiceID, payload.ContainerID); err != nil {
			return fail(err.Error())
		}
		if old, err := a.readMigrationFence(task.ServiceID); err == nil {
			if old.JobID != payload.JobID || old.ContainerID != payload.ContainerID {
				return fail("service is fenced by another migration")
			}
		} else if !os.IsNotExist(err) {
			return fail("cannot verify existing migration fence")
		} else {
			policy, err := runtime.command(runtime.Bin, "inspect", "--format", "{{.HostConfig.RestartPolicy.Name}}:{{.HostConfig.RestartPolicy.MaximumRetryCount}}", payload.ContainerID).Output()
			if err != nil {
				return fail("cannot inspect source restart policy")
			}
			fence := migrationFence{JobID: payload.JobID, ServiceID: task.ServiceID, ContainerID: payload.ContainerID, RestartPolicy: strings.TrimSpace(string(policy))}
			data, err := json.Marshal(fence)
			if err != nil || a.journal.saveSealed(a.migrationFencePath(task.ServiceID), data) != nil {
				return fail("cannot persist source writer fence")
			}
		}
		if err := runtime.command(runtime.Bin, "update", "--restart=no", payload.ContainerID).Run(); err != nil {
			return fail("source restart-policy update requires reconciliation")
		}
		if err := runtime.command(runtime.Bin, "stop", "--time", "30", payload.ContainerID).Run(); err != nil {
			return fail("source stop requires reconciliation")
		}
		if !migrationContainerStopped(runtime, payload.ContainerID) {
			return fail("source writer stop could not be verified")
		}
		result.OK = true
		result.Log = "Source writer fenced and verified stopped; container and data retained"
		return result
	}
	if task.Kind == types.TaskMigrationRestore {
		if payload.ArtifactID == "" || len(payload.BackupKey) != 32 || payload.PlaintextBytes < 1 || !validMigrationHash(payload.PlaintextSHA256) {
			return fail("invalid migration restore request")
		}
		artifactPath := filepath.Join(a.receiveArtifactDir(payload.JobID, payload.ArtifactID), "artifact.enc")
		file, err := os.Open(artifactPath)
		if err != nil {
			return fail("encrypted artifact missing from destination storage")
		}
		defer file.Close()

		stagingParent := filepath.Join(a.cfg.DataDir, "migration-staging")
		if err := os.MkdirAll(stagingParent, 0700); err != nil {
			return fail("destination staging parent unavailable")
		}

		expectedDigest := migration.ArtifactDigest{
			Bytes:  payload.PlaintextBytes,
			SHA256: payload.PlaintextSHA256,
		}

		switch payload.Kind {
		case "postgresql", "mysql":
			if filepath.Base(runtime.Bin) != "docker" && filepath.Base(runtime.Bin) != "docker.exe" {
				return fail("database restore requires Docker")
			}
			if payload.ContainerID == "" {
				return fail("destination database container identity missing")
			}
			dbName := payload.DatabaseName
			if dbName == "" {
				envData, err := runtime.command(runtime.Bin, "inspect", "--format", "{{json .Config.Env}}", payload.ContainerID).Output()
				if err == nil {
					var envs []string
					if json.Unmarshal(envData, &envs) == nil {
						for _, ev := range envs {
							if strings.HasPrefix(ev, "POSTGRES_DB=") {
								dbName = strings.TrimPrefix(ev, "POSTGRES_DB=")
							} else if strings.HasPrefix(ev, "MYSQL_DATABASE=") {
								dbName = strings.TrimPrefix(ev, "MYSQL_DATABASE=")
							}
						}
					}
				}
			}
			if dbName == "" {
				dbName = "migration_restore"
			}
			if err := migration.RestoreDatabase(ctx, migration.Database{Engine: migration.DatabaseEngine(payload.Kind), ContainerID: payload.ContainerID}, file, payload.BackupKey, expectedDigest, stagingParent, dbName, true); err != nil {
				return fail("database restore failed: " + err.Error())
			}
		case "volume":
			if filepath.Base(runtime.Bin) != "docker" && filepath.Base(runtime.Bin) != "docker.exe" {
				return fail("volume restore requires local Docker")
			}
			host := os.Getenv("DOCKER_HOST")
			if host == "" {
				contextData, contextErr := runtime.command(runtime.Bin, "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}").Output()
				if contextErr != nil || json.Unmarshal(contextData, &host) != nil {
					return fail("cannot verify the local Docker endpoint")
				}
			}
			if !strings.HasPrefix(host, "unix:///") {
				return fail("volume restore requires a local Linux Docker socket")
			}
			volName := payload.TargetVolume
			if volName == "" {
				volName = "rackops-vol-" + migrationArtifactName(payload.JobID, payload.ArtifactID)
				_ = runtime.command(runtime.Bin, "volume", "create", volName).Run()
			}
			destData, err := runtime.command(runtime.Bin, "volume", "inspect", "--format", "{{.Mountpoint}}", volName).Output()
			if err != nil || strings.TrimSpace(string(destData)) == "" {
				return fail("cannot inspect destination named volume")
			}
			destMount := strings.TrimSpace(string(destData))
			maxBytes := int64(10 << 30)
			if payload.PlaintextBytes > 0 && payload.PlaintextBytes*5 > maxBytes {
				maxBytes = payload.PlaintextBytes * 5
			}
			if err := migration.RestoreVolume(ctx, file, payload.BackupKey, expectedDigest, stagingParent, destMount, maxBytes); err != nil {
				return fail("volume restore failed: " + err.Error())
			}
		default:
			return fail("restore consistency adapter unavailable")
		}

		result.OK = true
		result.Log = "Restored into destination storage with writers disabled"
		return result
	}
	if task.Kind == types.TaskMigrationValidate {
		if payload.ArtifactID == "" {
			return fail("invalid migration validate request")
		}
		switch payload.Kind {
		case "postgresql", "mysql":
			if filepath.Base(runtime.Bin) != "docker" && filepath.Base(runtime.Bin) != "docker.exe" {
				return fail("database validation requires Docker")
			}
			if payload.ContainerID == "" {
				return fail("destination database container identity missing")
			}
			dbName := payload.DatabaseName
			if dbName == "" {
				envData, err := runtime.command(runtime.Bin, "inspect", "--format", "{{json .Config.Env}}", payload.ContainerID).Output()
				if err == nil {
					var envs []string
					if json.Unmarshal(envData, &envs) == nil {
						for _, ev := range envs {
							if strings.HasPrefix(ev, "POSTGRES_DB=") {
								dbName = strings.TrimPrefix(ev, "POSTGRES_DB=")
							} else if strings.HasPrefix(ev, "MYSQL_DATABASE=") {
								dbName = strings.TrimPrefix(ev, "MYSQL_DATABASE=")
							}
						}
					}
				}
			}
			if dbName == "" {
				dbName = "migration_restore"
			}
			db := migration.Database{Engine: migration.DatabaseEngine(payload.Kind), ContainerID: payload.ContainerID}
			if err := db.Validate(ctx, dbName); err != nil {
				return fail("database validation failed: " + err.Error())
			}
		case "volume":
			volName := payload.TargetVolume
			if volName == "" {
				volName = "rackops-vol-" + migrationArtifactName(payload.JobID, payload.ArtifactID)
			}
			destData, err := runtime.command(runtime.Bin, "volume", "inspect", "--format", "{{.Mountpoint}}", volName).Output()
			if err != nil || strings.TrimSpace(string(destData)) == "" {
				return fail("cannot inspect destination named volume for validation")
			}
			mountPoint := strings.TrimSpace(string(destData))
			entries, err := os.ReadDir(mountPoint)
			if err != nil {
				return fail("cannot read destination volume mountpoint: " + err.Error())
			}
			if len(entries) == 0 {
				return fail("restored destination volume is empty")
			}
		default:
			return fail("validation consistency adapter unavailable")
		}
		result.OK = true
		result.Log = "Destination storage and database verified healthy (read-only)"
		return result
	}
	if task.Kind != types.TaskMigrationBackup || payload.ArtifactID == "" || len(payload.BackupKey) != 32 {
		return fail("invalid migration backup request")
	}
	// Database containers stay running for dumps; every declared application
	// writer must have a matching durable fence and still be stopped.
	if payload.Kind != "volume" && len(payload.Writers) == 0 {
		return fail("database backup requires explicit fenced application writers")
	}
	for _, writer := range payload.Writers {
		fence, err := a.readMigrationFence(writer.ServiceID)
		if err != nil || fence.JobID != payload.JobID || fence.ContainerID != writer.ContainerID || ownedMigrationContainer(runtime, writer.ServiceID, writer.ContainerID) != nil || !migrationContainerStopped(runtime, writer.ContainerID) {
			return fail("application writer fence is missing or no longer stopped")
		}
	}
	if payload.Kind == "volume" {
		fence, err := a.readMigrationFence(task.ServiceID)
		if err != nil || fence.JobID != payload.JobID || fence.ContainerID != payload.ContainerID || !migrationContainerStopped(runtime, payload.ContainerID) {
			return fail("volume source must remain fenced and stopped")
		}
	}
	artifactName := sha256.Sum256([]byte(payload.JobID + ":" + payload.ArtifactID))
	directory := filepath.Join(a.cfg.DataDir, "migration-artifacts")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fail("private artifact storage unavailable")
	}
	destination := filepath.Join(directory, hex.EncodeToString(artifactName[:])+".enc")
	var digest migration.ArtifactDigest
	var err error
	switch payload.Kind {
	case "postgresql", "mysql":
		if filepath.Base(runtime.Bin) != "docker" && filepath.Base(runtime.Bin) != "docker.exe" {
			return fail("database migration currently requires Docker")
		}
		digest, err = migration.BackupDatabase(ctx, migration.Database{Engine: migration.DatabaseEngine(payload.Kind), ContainerID: payload.ContainerID}, destination, payload.BackupKey, true)
	case "volume":
		// Host mount paths are usable only with a local Docker daemon. Remote
		// contexts could otherwise point at unrelated paths on this agent host.
		if filepath.Base(runtime.Bin) != "docker" && filepath.Base(runtime.Bin) != "docker.exe" {
			return fail("volume migration currently requires local Docker")
		}
		host := os.Getenv("DOCKER_HOST")
		if host == "" {
			contextData, contextErr := runtime.command(runtime.Bin, "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}").Output()
			if contextErr != nil || json.Unmarshal(contextData, &host) != nil {
				return fail("cannot verify the local Docker endpoint")
			}
		}
		if !strings.HasPrefix(host, "unix:///") {
			return fail("volume migration requires a local Linux Docker socket")
		}
		data, inspectErr := runtime.command(runtime.Bin, "inspect", "--format", "{{json .Mounts}}", payload.ContainerID).Output()
		var mounts []struct {
			Name        string
			Source      string
			Destination string
			Type        string
		}
		if inspectErr != nil || json.Unmarshal(data, &mounts) != nil {
			return fail("cannot inspect owned volume attachments")
		}
		source := ""
		volumeName := ""
		for _, mount := range mounts {
			if mount.Destination == payload.MountDestination && mount.Type == "volume" {
				if source != "" {
					return fail("ambiguous volume attachment")
				}
				source = mount.Source
				volumeName = mount.Name
			}
		}
		if source == "" || volumeName == "" {
			return fail("selected named volume is not attached to the owned source container")
		}
		consumers, consumerErr := runtime.command(runtime.Bin, "ps", "--quiet", "--filter", "volume="+volumeName).Output()
		if consumerErr != nil || strings.TrimSpace(string(consumers)) != "" {
			return fail("selected volume still has running consumers")
		}
		digest, err = migration.BackupVolume(ctx, source, destination, payload.BackupKey, true)
	default:
		return fail("backup consistency adapter unavailable")
	}
	if err != nil {
		return fail("backup failed; retain source, writer fences and existing artifacts for reconciliation")
	}
	file, err := os.Open(destination)
	if err != nil {
		return fail("cannot verify encrypted artifact")
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, &migrationContextReader{ctx: ctx, reader: file})
	if err != nil {
		return fail("encrypted artifact verification was interrupted")
	}
	result.MigrationArtifact = &types.MigrationArtifactResult{JobID: payload.JobID, ArtifactID: payload.ArtifactID, PlaintextBytes: digest.Bytes, PlaintextSHA256: digest.SHA256, EncryptedBytes: count, EncryptedSHA256: hex.EncodeToString(hash.Sum(nil))}
	result.OK = true
	result.Log = "Encrypted backup created; source retained and writers remain fenced"
	return result
}

type migrationContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *migrationContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
