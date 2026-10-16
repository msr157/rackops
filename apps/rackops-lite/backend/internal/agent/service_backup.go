package agent

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// executeBackup takes a snapshot of a database service.
func (a *Agent) executeBackup(p DeployPayload) (string, bool) {
	target := targetContainer(p)
	if target == "" {
		return "no target container identified for backup", false
	}

	nameLower := strings.ToLower(p.Name)
	imgLower := strings.ToLower(p.Image)
	targetLower := strings.ToLower(target)
	isPostgres := strings.Contains(nameLower, "postgres") ||
		strings.Contains(imgLower, "postgres") ||
		strings.Contains(targetLower, "postgres")
	isMySQL := strings.Contains(nameLower, "mysql") ||
		strings.Contains(imgLower, "mysql") ||
		strings.Contains(nameLower, "mariadb") ||
		strings.Contains(imgLower, "mariadb")

	// 1. If this is rogue-server's production PostgreSQL with the automated script, prefer the full replication script.
	scriptPath := "/home/rogue-server/services/backup-postgres.sh"
	if isPostgres && (target == "postgres-production" || p.ExternalName == "postgres-production") {
		if fi, err := os.Stat(scriptPath); err == nil && !fi.IsDir() {
			cmd := exec.Command("/bin/bash", scriptPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Sprintf("system backup script failed: %s\nOutput: %s", err, string(out)), false
			}
			return strings.TrimSpace(string(out)), true
		}
	}

	// 2. Standard backup to <DataDir>/backups/<ServiceID>
	timestamp := time.Now().UTC().Format("20060102_150405")
	backupDir := filepath.Join(a.cfg.DataDir, "backups", p.ServiceID)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Sprintf("cannot create backup directory: %s", err), false
	}

	filename := fmt.Sprintf("dump_%s_%s.sql.gz", sanitize(p.Name), timestamp)
	targetPath := filepath.Join(backupDir, filename)

	if isPostgres {
		outFile, err := os.Create(targetPath)
		if err != nil {
			return fmt.Sprintf("cannot create backup output file: %s", err), false
		}
		defer outFile.Close()

		gzWriter := gzip.NewWriter(outFile)
		defer gzWriter.Close()

		// Execute pg_dumpall -U postgres inside the container
		cmd := a.runtime.command(a.runtime.Bin, "exec", target, "pg_dumpall", "-U", "postgres")
		cmd.Stdout = gzWriter
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			_ = gzWriter.Close()
			_ = outFile.Close()
			_ = os.Remove(targetPath)
			return fmt.Sprintf("pg_dumpall failed: %s (stderr: %s)", err, stderr.String()), false
		}

		if err := gzWriter.Close(); err != nil {
			_ = os.Remove(targetPath)
			return fmt.Sprintf("failed to compress backup: %s", err), false
		}
		if err := outFile.Close(); err != nil {
			_ = os.Remove(targetPath)
			return fmt.Sprintf("failed to write backup: %s", err), false
		}

		fi, err := os.Stat(targetPath)
		sizeStr := "unknown size"
		if err == nil {
			sizeStr = fmt.Sprintf("%d bytes", fi.Size())
		}
		return fmt.Sprintf("PostgreSQL backup created: %s (%s)", filename, sizeStr), true
	}

	if isMySQL {
		outFile, err := os.Create(targetPath)
		if err != nil {
			return fmt.Sprintf("cannot create backup output file: %s", err), false
		}
		defer outFile.Close()

		gzWriter := gzip.NewWriter(outFile)
		defer gzWriter.Close()

		cmd := a.runtime.command(a.runtime.Bin, "exec", target, "mysqldump", "--all-databases", "-u", "root")
		cmd.Stdout = gzWriter
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			_ = gzWriter.Close()
			_ = outFile.Close()
			_ = os.Remove(targetPath)
			return fmt.Sprintf("mysqldump failed: %s (stderr: %s)", err, stderr.String()), false
		}

		if err := gzWriter.Close(); err != nil {
			_ = os.Remove(targetPath)
			return fmt.Sprintf("failed to compress backup: %s", err), false
		}
		if err := outFile.Close(); err != nil {
			_ = os.Remove(targetPath)
			return fmt.Sprintf("failed to write backup: %s", err), false
		}

		fi, err := os.Stat(targetPath)
		sizeStr := "unknown size"
		if err == nil {
			sizeStr = fmt.Sprintf("%d bytes", fi.Size())
		}
		return fmt.Sprintf("MySQL backup created: %s (%s)", filename, sizeStr), true
	}

	return "service does not match a supported database engine for automated snapshotting", false
}

// executeRestore restores a database service from an existing snapshot file.
func (a *Agent) executeRestore(p DeployPayload) (string, bool) {
	target := targetContainer(p)
	if target == "" {
		return "no target container identified for restore", false
	}

	dumpName := filepath.Base(p.Path)
	if dumpName == "" || dumpName == "." {
		return "invalid or empty backup filename for restore", false
	}

	// Locate backup snapshot on the host
	candidates := []string{
		filepath.Join(a.cfg.DataDir, "backups", p.ServiceID, dumpName),
		filepath.Join(a.cfg.DataDir, "backups", dumpName),
		filepath.Join("/home/rogue-server/data/postgresql/backups", dumpName),
		filepath.Join("/data/backups/postgres", dumpName),
	}
	var dumpPath string
	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			dumpPath = cand
			break
		}
	}
	if dumpPath == "" {
		return fmt.Sprintf("snapshot file %q not found in agent storage directories", dumpName), false
	}

	nameLower := strings.ToLower(p.Name)
	imgLower := strings.ToLower(p.Image)
	targetLower := strings.ToLower(target)
	isPostgres := strings.Contains(nameLower, "postgres") ||
		strings.Contains(imgLower, "postgres") ||
		strings.Contains(targetLower, "postgres")
	isMySQL := strings.Contains(nameLower, "mysql") ||
		strings.Contains(imgLower, "mysql") ||
		strings.Contains(nameLower, "mariadb") ||
		strings.Contains(imgLower, "mariadb")

	if isPostgres {
		file, err := os.Open(dumpPath)
		if err != nil {
			return fmt.Sprintf("cannot open backup snapshot: %s", err), false
		}
		defer file.Close()

		var reader io.Reader = file
		if strings.HasSuffix(dumpPath, ".gz") {
			gz, err := gzip.NewReader(file)
			if err != nil {
				return fmt.Sprintf("corrupt gzip snapshot: %s", err), false
			}
			defer gz.Close()
			reader = gz
		}

		cmd := a.runtime.command(a.runtime.Bin, "exec", "-i", target, "psql", "-U", "postgres")
		cmd.Stdin = reader
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("psql restore execution failed: %s (stderr: %s)", err, stderr.String()), false
		}

		// Validation check
		checkCmd := a.runtime.command(a.runtime.Bin, "exec", target, "psql", "-U", "postgres", "-c", "SELECT 1;")
		if err := checkCmd.Run(); err != nil {
			return fmt.Sprintf("snapshot applied but database health probe failed: %s", err), false
		}

		return fmt.Sprintf("PostgreSQL database successfully restored from snapshot %s", dumpName), true
	}

	if isMySQL {
		file, err := os.Open(dumpPath)
		if err != nil {
			return fmt.Sprintf("cannot open backup snapshot: %s", err), false
		}
		defer file.Close()

		var reader io.Reader = file
		if strings.HasSuffix(dumpPath, ".gz") {
			gz, err := gzip.NewReader(file)
			if err != nil {
				return fmt.Sprintf("corrupt gzip snapshot: %s", err), false
			}
			defer gz.Close()
			reader = gz
		}

		cmd := a.runtime.command(a.runtime.Bin, "exec", "-i", target, "mysql", "-u", "root")
		cmd.Stdin = reader
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("mysql restore execution failed: %s (stderr: %s)", err, stderr.String()), false
		}

		return fmt.Sprintf("MySQL database successfully restored from snapshot %s", dumpName), true
	}

	return "unsupported database engine for snapshot restoration", false
}
