package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// buildFromGit clones a repository and builds a container image.
// Priority: existing Dockerfile > nixpacks (if installed) > auto-generated
// Dockerfile from stack detection (Node/Python/Go/static).
func (r *Runtime) buildFromGit(p DeployPayload, logs *strings.Builder) (image string, err error) {
	workDir, err := os.MkdirTemp("", "rackops-build-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workDir)

	// Clone (token-authenticated when provided).
	repo := p.GitRepo
	if p.GitToken != "" && strings.HasPrefix(repo, "https://") {
		repo = "https://oauth2:" + p.GitToken + "@" + strings.TrimPrefix(repo, "https://")
	}
	branch := p.GitBranch
	if branch == "" {
		branch = "main"
	}
	logs.WriteString(fmt.Sprintf("$ git clone --depth 1 -b %s %s\n", branch, p.GitRepo))
	out, err := exec.Command("git", "clone", "--depth", "1", "-b", branch, repo, workDir).CombinedOutput()
	logs.Write(redactToken(out, p.GitToken))
	if err != nil {
		// Retry default branch if the requested one doesn't exist.
		out2, err2 := exec.Command("git", "clone", "--depth", "1", repo, workDir).CombinedOutput()
		logs.Write(redactToken(out2, p.GitToken))
		if err2 != nil {
			return "", fmt.Errorf("clone failed: %w", err2)
		}
	}

	image = "rackops/" + sanitize(p.Name) + ":" + p.ServiceID

	// 1. Repo Dockerfile wins.
	if _, statErr := os.Stat(filepath.Join(workDir, "Dockerfile")); statErr == nil {
		logs.WriteString("Dockerfile found — building\n")
		return image, r.dockerBuild(workDir, image, logs)
	}

	// 2. Nixpacks if available.
	if nix, lookErr := exec.LookPath("nixpacks"); lookErr == nil {
		logs.WriteString("building with nixpacks\n")
		out, err := exec.Command(nix, "build", workDir, "--name", image).CombinedOutput()
		logs.Write(out)
		if err == nil {
			return image, nil
		}
		logs.WriteString("nixpacks failed, falling back to stack detection\n")
	}

	// 3. Auto-generate a Dockerfile from stack detection.
	df, desc := detectStack(workDir)
	if df == "" {
		return "", fmt.Errorf("no Dockerfile and unable to detect stack — add a Dockerfile to the repo")
	}
	logs.WriteString("detected " + desc + " — generated Dockerfile\n")
	if err := os.WriteFile(filepath.Join(workDir, "Dockerfile"), []byte(df), 0o644); err != nil {
		return "", err
	}
	return image, r.dockerBuild(workDir, image, logs)
}

func (r *Runtime) dockerBuild(dir, image string, logs *strings.Builder) error {
	logs.WriteString("$ " + r.Bin + " build -t " + image + " .\n")
	cmd := exec.Command(r.Bin, "build", "-t", image, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	// Keep the tail — build logs can be huge.
	s := string(out)
	if len(s) > 8000 {
		s = "…(truncated)…\n" + s[len(s)-8000:]
	}
	logs.WriteString(s)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}

// detectStack returns a generated Dockerfile + description for common stacks.
func detectStack(dir string) (dockerfile, desc string) {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("package.json"):
		return `FROM node:22-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install --omit=dev || npm install
COPY . .
RUN npm run build || true
EXPOSE 3000
CMD ["npm", "start"]
`, "Node.js (package.json)"
	case exists("go.mod"):
		return `FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/app .
FROM alpine
COPY --from=build /out/app /app
EXPOSE 8080
CMD ["/app"]
`, "Go (go.mod)"
	case exists("requirements.txt"):
		return `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
`, "Python (requirements.txt)"
	case exists("pyproject.toml"):
		return `FROM python:3.12-slim
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir .
EXPOSE 8000
CMD ["python", "-m", "app"]
`, "Python (pyproject.toml)"
	case exists("index.html"):
		return `FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE 80
`, "static site (index.html)"
	}
	return "", ""
}

func redactToken(b []byte, token string) []byte {
	if token == "" {
		return b
	}
	return []byte(strings.ReplaceAll(string(b), token, "***"))
}
