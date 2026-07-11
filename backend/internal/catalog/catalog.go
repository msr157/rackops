// Package catalog ships the one-click template library: infra, homelab,
// AI/builder, web, tools, game servers, and managed databases.
package catalog

// Template is a one-click deployable app definition.
type Template struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"` // infra | homelab | ai | web | tools | games | database
	Description string   `json:"description"`
	Image       string   `json:"image"`
	Port        int      `json:"port"`            // primary UI/service port
	Ports       []int    `json:"ports,omitempty"` // extra ports to publish
	Env         []EnvDef `json:"env,omitempty"`
	Volumes     []string `json:"volumes,omitempty"` // container paths needing persistence
	MemMB       int64    `json:"memMb"`             // typical footprint, used by placement
	GPU         bool     `json:"gpu,omitempty"`
	Command     string   `json:"command,omitempty"`    // container command override
	ComposeYML  string   `json:"composeYml,omitempty"` // multi-container stack; when set, Image is ignored
	Notes       string   `json:"notes,omitempty"`
}

// EnvDef is a configurable environment variable in the deploy form.
type EnvDef struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Default  string `json:"default"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Generate bool   `json:"generate,omitempty"` // auto-generate a random secret
}

// All returns the full template catalog.
func All() []Template { return templates }

// Get finds a template by ID.
func Get(id string) *Template {
	for i := range templates {
		if templates[i].ID == id {
			return &templates[i]
		}
	}
	return nil
}

var templates = []Template{
	// ---- Infra / DevOps ----
	{ID: "minio", Name: "MinIO", Category: "infra", Description: "S3-compatible object storage", Image: "minio/minio:latest", Port: 9001, Ports: []int{9000}, MemMB: 512,
		Env:     []EnvDef{{Key: "MINIO_ROOT_USER", Label: "Admin user", Default: "rackops", Required: true}, {Key: "MINIO_ROOT_PASSWORD", Label: "Admin password", Secret: true, Required: true, Generate: true}},
		Volumes: []string{"/data"}, Command: "server /data --console-address :9001", Notes: "console on 9001, S3 API on 9000"},
	{ID: "gitea", Name: "Gitea", Category: "infra", Description: "Self-hosted Git service", Image: "gitea/gitea:latest", Port: 3000, Ports: []int{222}, MemMB: 512, Volumes: []string{"/data"}},
	{ID: "jenkins", Name: "Jenkins", Category: "infra", Description: "CI/CD automation server", Image: "jenkins/jenkins:lts", Port: 8080, MemMB: 1024, Volumes: []string{"/var/jenkins_home"}},
	{ID: "prometheus", Name: "Prometheus", Category: "infra", Description: "Metrics & monitoring", Image: "prom/prometheus:latest", Port: 9090, MemMB: 512, Volumes: []string{"/prometheus"}},
	{ID: "grafana", Name: "Grafana", Category: "infra", Description: "Dashboards & visualization", Image: "grafana/grafana:latest", Port: 3000, MemMB: 256, Volumes: []string{"/var/lib/grafana"}},
	{ID: "uptime-kuma", Name: "Uptime Kuma", Category: "infra", Description: "Uptime monitoring with status pages", Image: "louislam/uptime-kuma:1", Port: 3001, MemMB: 256, Volumes: []string{"/app/data"}},
	{ID: "registry", Name: "Docker Registry", Category: "infra", Description: "Private container image registry", Image: "registry:2", Port: 5000, MemMB: 256, Volumes: []string{"/var/lib/registry"}},
	{ID: "rabbitmq", Name: "RabbitMQ", Category: "infra", Description: "Message broker with management UI", Image: "rabbitmq:3-management", Port: 15672, Ports: []int{5672}, MemMB: 512, Volumes: []string{"/var/lib/rabbitmq"}},
	{ID: "seaweedfs", Name: "SeaweedFS", Category: "infra", Description: "Distributed storage: pool drives across multiple nodes into one S3-compatible space (files spread automatically)", Image: "chrislusf/seaweedfs:latest", Port: 8333, Ports: []int{9333, 8080}, MemMB: 512, Volumes: []string{"/data"},
		Command: "server -s3 -dir=/data", Notes: "deploy one per node and join them: master on node 1, `server -mserver=<node1-mesh-ip>:9333` on others — combined free space becomes one storage pool"},

	// ---- Homelab ----
	{ID: "jellyfin", Name: "Jellyfin", Category: "homelab", Description: "Media server for movies, TV & music", Image: "jellyfin/jellyfin:latest", Port: 8096, MemMB: 1024, Volumes: []string{"/config", "/media"}},
	{ID: "immich", Name: "Immich", Category: "homelab", Description: "Self-hosted photo & video backup (Google Photos alternative)", Port: 2283, MemMB: 3072, ComposeYML: immichCompose,
		Env:   []EnvDef{{Key: "DB_PASSWORD", Label: "Database password", Secret: true, Generate: true}},
		Notes: "full stack (server + machine-learning + Postgres + Redis) — one click, no setup"},
	{ID: "nextcloud", Name: "Nextcloud", Category: "homelab", Description: "Files, calendar, contacts — your own cloud", Image: "nextcloud:latest", Port: 80, MemMB: 1024, Volumes: []string{"/var/www/html"}},
	{ID: "home-assistant", Name: "Home Assistant", Category: "homelab", Description: "Home automation hub", Image: "ghcr.io/home-assistant/home-assistant:stable", Port: 8123, MemMB: 1024, Volumes: []string{"/config"}},
	{ID: "vaultwarden", Name: "Vaultwarden", Category: "homelab", Description: "Bitwarden-compatible password manager", Image: "vaultwarden/server:latest", Port: 80, MemMB: 256, Volumes: []string{"/data"}},
	{ID: "adguard", Name: "AdGuard Home", Category: "homelab", Description: "Network-wide ad & tracker blocking DNS", Image: "adguard/adguardhome:latest", Port: 3000, Ports: []int{53}, MemMB: 256, Volumes: []string{"/opt/adguardhome/work", "/opt/adguardhome/conf"}},
	{ID: "pihole", Name: "Pi-hole", Category: "homelab", Description: "DNS sinkhole ad blocker", Image: "pihole/pihole:latest", Port: 80, Ports: []int{53}, MemMB: 256,
		Env: []EnvDef{{Key: "WEBPASSWORD", Label: "Admin password", Secret: true, Required: true, Generate: true}}, Volumes: []string{"/etc/pihole"}},
	{ID: "syncthing", Name: "Syncthing", Category: "homelab", Description: "Continuous P2P file synchronization", Image: "syncthing/syncthing:latest", Port: 8384, Ports: []int{22000}, MemMB: 256, Volumes: []string{"/var/syncthing"}},

	// ---- AI / Builder ----
	{ID: "ollama", Name: "Ollama", Category: "ai", Description: "Run LLMs locally (Llama, Mistral, Gemma…)", Image: "ollama/ollama:latest", Port: 11434, MemMB: 4096, GPU: true, Volumes: []string{"/root/.ollama"}},
	{ID: "open-webui", Name: "Open WebUI", Category: "ai", Description: "ChatGPT-style UI for Ollama & OpenAI APIs", Image: "ghcr.io/open-webui/open-webui:main", Port: 8080, MemMB: 512, Volumes: []string{"/app/backend/data"},
		Env: []EnvDef{{Key: "OLLAMA_BASE_URL", Label: "Ollama URL", Default: "http://host.docker.internal:11434"}}},
	{ID: "n8n", Name: "n8n", Category: "ai", Description: "Workflow automation with 400+ integrations", Image: "docker.n8n.io/n8nio/n8n:latest", Port: 5678, MemMB: 512, Volumes: []string{"/home/node/.n8n"}},
	{ID: "pocketbase", Name: "PocketBase", Category: "ai", Description: "Backend-in-one-file: DB, auth, realtime", Image: "ghcr.io/muchobien/pocketbase:latest", Port: 8090, MemMB: 128, Volumes: []string{"/pb_data"}},
	{ID: "flowise", Name: "Flowise", Category: "ai", Description: "Drag & drop LLM flow builder", Image: "flowiseai/flowise:latest", Port: 3000, MemMB: 512, Volumes: []string{"/root/.flowise"}},
	{ID: "qdrant", Name: "Qdrant", Category: "ai", Description: "Vector database for AI applications", Image: "qdrant/qdrant:latest", Port: 6333, MemMB: 512, Volumes: []string{"/qdrant/storage"}},

	// ---- Web / Business ----
	{ID: "wordpress", Name: "WordPress", Category: "web", Description: "The classic CMS", Port: 8081, MemMB: 1024, ComposeYML: wordpressCompose,
		Env:   []EnvDef{{Key: "DB_PASSWORD", Label: "Database password", Secret: true, Generate: true}},
		Notes: "full stack (WordPress + MariaDB) — one click, no setup"},
	{ID: "ghost", Name: "Ghost", Category: "web", Description: "Modern publishing & newsletters", Image: "ghost:5-alpine", Port: 2368, MemMB: 512, Volumes: []string{"/var/lib/ghost/content"},
		Env: []EnvDef{{Key: "url", Label: "Public URL", Default: "http://localhost:2368", Required: true}, {Key: "database__client", Label: "DB client", Default: "sqlite3"}}},
	{ID: "plausible", Name: "Plausible CE", Category: "web", Description: "Privacy-friendly web analytics", Port: 8000, MemMB: 1536, ComposeYML: plausibleCompose,
		Env: []EnvDef{{Key: "SECRET_KEY_BASE", Label: "Secret key", Secret: true, Generate: true}, {Key: "DB_PASSWORD", Label: "Database password", Secret: true, Generate: true}, {Key: "BASE_URL", Label: "Public URL", Default: "http://localhost:8000", Required: true}},
		Notes: "full stack (Plausible + Postgres + ClickHouse) — set Public URL to your domain once routed"},
	{ID: "umami", Name: "Umami", Category: "web", Description: "Simple website analytics", Port: 3000, MemMB: 512, ComposeYML: umamiCompose,
		Env:   []EnvDef{{Key: "APP_SECRET", Label: "App secret", Secret: true, Generate: true}, {Key: "DB_PASSWORD", Label: "Database password", Secret: true, Generate: true}},
		Notes: "full stack (Umami + Postgres) — one click. Default login admin / umami"},
	{ID: "listmonk", Name: "Listmonk", Category: "web", Description: "Newsletter & mailing list manager", Port: 9000, MemMB: 512, ComposeYML: listmonkCompose,
		Env:   []EnvDef{{Key: "DB_PASSWORD", Label: "Database password", Secret: true, Generate: true}},
		Notes: "full stack (Listmonk + Postgres) — one click. Default login admin / listmonk"},

	// ---- Tools ----
	{ID: "excalidraw", Name: "Excalidraw", Category: "tools", Description: "Collaborative whiteboard sketching", Image: "excalidraw/excalidraw:latest", Port: 80, MemMB: 128},
	{ID: "it-tools", Name: "IT-Tools", Category: "tools", Description: "80+ handy developer utilities", Image: "corentinth/it-tools:latest", Port: 80, MemMB: 128},
	{ID: "stirling-pdf", Name: "Stirling PDF", Category: "tools", Description: "PDF merge, split, convert, OCR", Image: "stirlingtools/stirling-pdf:latest", Port: 8080, MemMB: 1024, Volumes: []string{"/usr/share/tessdata"}},
	{ID: "vscode-server", Name: "code-server", Category: "tools", Description: "VS Code in the browser", Image: "codercom/code-server:latest", Port: 8080, MemMB: 1024, Volumes: []string{"/home/coder"},
		Env: []EnvDef{{Key: "PASSWORD", Label: "Access password", Secret: true, Required: true, Generate: true}}},
	{ID: "filebrowser", Name: "File Browser", Category: "tools", Description: "Web file manager for your volumes", Image: "filebrowser/filebrowser:latest", Port: 80, MemMB: 128, Volumes: []string{"/srv"}},
	{ID: "drawio", Name: "draw.io", Category: "tools", Description: "Diagrams & flowcharts", Image: "jgraph/drawio:latest", Port: 8080, MemMB: 256},

	// ---- Game servers ----
	{ID: "minecraft-java", Name: "Minecraft (Java)", Category: "games", Description: "Vanilla/Paper/Forge/Fabric — configurable", Image: "itzg/minecraft-server:latest", Port: 25565, MemMB: 4096, Volumes: []string{"/data"},
		Env: []EnvDef{{Key: "EULA", Label: "Accept EULA", Default: "TRUE", Required: true}, {Key: "TYPE", Label: "Server type (VANILLA/PAPER/FORGE/FABRIC)", Default: "PAPER"}, {Key: "MEMORY", Label: "Heap size", Default: "2G"}, {Key: "VERSION", Label: "MC version", Default: "LATEST"}}},
	{ID: "minecraft-bedrock", Name: "Minecraft (Bedrock)", Category: "games", Description: "For phones, consoles & Windows", Image: "itzg/minecraft-bedrock-server:latest", Port: 19132, MemMB: 2048, Volumes: []string{"/data"},
		Env: []EnvDef{{Key: "EULA", Label: "Accept EULA", Default: "TRUE", Required: true}}},
	{ID: "cs2", Name: "Counter-Strike 2", Category: "games", Description: "Dedicated CS2 server (SteamCMD)", Image: "joedwards32/cs2:latest", Port: 27015, MemMB: 8192, Volumes: []string{"/home/steam/cs2-dedicated"},
		Env: []EnvDef{{Key: "SRCDS_TOKEN", Label: "Steam GSLT token", Secret: true, Required: true}, {Key: "CS2_SERVERNAME", Label: "Server name", Default: "RackOps CS2"}}, Notes: "~30GB download on first start"},
	{ID: "valheim", Name: "Valheim", Category: "games", Description: "Viking survival dedicated server", Image: "lloesche/valheim-server:latest", Port: 2456, MemMB: 4096, Volumes: []string{"/config"},
		Env: []EnvDef{{Key: "SERVER_NAME", Label: "Server name", Default: "RackOps Valheim"}, {Key: "WORLD_NAME", Label: "World name", Default: "Midgard"}, {Key: "SERVER_PASS", Label: "Password (min 5 chars)", Secret: true, Required: true}}},
	{ID: "palworld", Name: "Palworld", Category: "games", Description: "Palworld dedicated server", Image: "thijsvanloef/palworld-server-docker:latest", Port: 8211, MemMB: 16384, Volumes: []string{"/palworld"},
		Env: []EnvDef{{Key: "SERVER_NAME", Label: "Server name", Default: "RackOps Palworld"}, {Key: "PLAYERS", Label: "Max players", Default: "16"}}},
	{ID: "terraria", Name: "Terraria", Category: "games", Description: "Terraria (TShock) server", Image: "ryshe/terraria:latest", Port: 7777, MemMB: 1024, Volumes: []string{"/root/.local/share/Terraria/Worlds"}},
	{ID: "factorio", Name: "Factorio", Category: "games", Description: "Factory-building dedicated server", Image: "factoriotools/factorio:stable", Port: 34197, MemMB: 2048, Volumes: []string{"/factorio"}},
	{ID: "rust", Name: "Rust", Category: "games", Description: "Rust dedicated server", Image: "didstopia/rust-server:latest", Port: 28015, MemMB: 8192, Volumes: []string{"/steamcmd/rust"},
		Env: []EnvDef{{Key: "RUST_SERVER_NAME", Label: "Server name", Default: "RackOps Rust"}}, Notes: "~10GB download on first start"},
	{ID: "ark", Name: "ARK: Survival Evolved", Category: "games", Description: "ARK dedicated server", Image: "hermsi/ark-server:latest", Port: 7778, Ports: []int{27020}, MemMB: 12288, Volumes: []string{"/app"},
		Env: []EnvDef{{Key: "SESSION_NAME", Label: "Session name", Default: "RackOps ARK"}}},

	// ---- Managed databases ----
	{ID: "postgres", Name: "PostgreSQL", Category: "database", Description: "The world's most advanced open-source DB", Image: "postgres:16-alpine", Port: 5432, MemMB: 512, Volumes: []string{"/var/lib/postgresql/data"},
		Env: []EnvDef{{Key: "POSTGRES_USER", Label: "User", Default: "rackops", Required: true}, {Key: "POSTGRES_PASSWORD", Label: "Password", Secret: true, Required: true, Generate: true}, {Key: "POSTGRES_DB", Label: "Database", Default: "app"}}},
	{ID: "mysql", Name: "MySQL", Category: "database", Description: "Popular relational database", Image: "mysql:8", Port: 3306, MemMB: 1024, Volumes: []string{"/var/lib/mysql"},
		Env: []EnvDef{{Key: "MYSQL_ROOT_PASSWORD", Label: "Root password", Secret: true, Required: true, Generate: true}, {Key: "MYSQL_DATABASE", Label: "Database", Default: "app"}, {Key: "MYSQL_USER", Label: "User", Default: "rackops"}, {Key: "MYSQL_PASSWORD", Label: "User password", Secret: true, Generate: true}}},
	{ID: "mariadb", Name: "MariaDB", Category: "database", Description: "MySQL-compatible community fork", Image: "mariadb:11", Port: 3306, MemMB: 512, Volumes: []string{"/var/lib/mysql"},
		Env: []EnvDef{{Key: "MARIADB_ROOT_PASSWORD", Label: "Root password", Secret: true, Required: true, Generate: true}, {Key: "MARIADB_DATABASE", Label: "Database", Default: "app"}}},
	{ID: "mongodb", Name: "MongoDB", Category: "database", Description: "Document database", Image: "mongo:7", Port: 27017, MemMB: 1024, Volumes: []string{"/data/db"},
		Env: []EnvDef{{Key: "MONGO_INITDB_ROOT_USERNAME", Label: "Root user", Default: "rackops", Required: true}, {Key: "MONGO_INITDB_ROOT_PASSWORD", Label: "Root password", Secret: true, Required: true, Generate: true}}},
	{ID: "redis", Name: "Redis", Category: "database", Description: "In-memory cache & data store", Image: "redis:7-alpine", Port: 6379, MemMB: 256, Volumes: []string{"/data"}},
	{ID: "clickhouse", Name: "ClickHouse", Category: "database", Description: "Columnar OLAP database", Image: "clickhouse/clickhouse-server:latest", Port: 8123, Ports: []int{9000}, MemMB: 2048, Volumes: []string{"/var/lib/clickhouse"}},
}

// immichCompose is Immich's official four-service stack (server, machine
// learning, Postgres with the VectorChord extension, and Valkey/Redis),
// pinned to the images upstream ships with the `release` channel. The server
// defaults DB_HOSTNAME=database / DB_USERNAME=postgres / DB_DATABASE_NAME=immich
// / REDIS_HOSTNAME=redis, all matching the service names on the stack's private
// network — so the only value we inject is the generated DB_PASSWORD, shared
// between Postgres and the server via ${DB_PASSWORD} (supplied through the
// stack's .env). Named volumes keep the library and database across redeploys.
const immichCompose = `name: immich

services:
  immich-server:
    container_name: immich_server
    image: ghcr.io/immich-app/immich-server:release
    environment:
      DB_PASSWORD: ${DB_PASSWORD}
    volumes:
      - library:/data
      - /etc/localtime:/etc/localtime:ro
    ports:
      - "2283:2283"
    depends_on:
      - redis
      - database
    restart: unless-stopped
    healthcheck:
      disable: false

  immich-machine-learning:
    container_name: immich_machine_learning
    image: ghcr.io/immich-app/immich-machine-learning:release
    volumes:
      - model-cache:/cache
    restart: unless-stopped
    healthcheck:
      disable: false

  redis:
    container_name: immich_redis
    image: docker.io/valkey/valkey:8-bookworm
    healthcheck:
      test: redis-cli ping || exit 1
    restart: unless-stopped

  database:
    container_name: immich_postgres
    image: ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_USER: postgres
      POSTGRES_DB: immich
      POSTGRES_INITDB_ARGS: '--data-checksums'
    volumes:
      - pgdata:/var/lib/postgresql/data
    shm_size: 128mb
    restart: unless-stopped

volumes:
  library:
  model-cache:
  pgdata:
`

// wordpressCompose is WordPress + MariaDB. WordPress reaches the database at
// the service name `db`; the generated DB_PASSWORD is shared between them.
const wordpressCompose = `name: wordpress

services:
  wordpress:
    container_name: wordpress_app
    image: wordpress:latest
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_USER: wordpress
      WORDPRESS_DB_PASSWORD: ${DB_PASSWORD}
      WORDPRESS_DB_NAME: wordpress
    volumes:
      - wp-data:/var/www/html
    ports:
      - "8081:80"
    depends_on:
      - db
    restart: unless-stopped

  db:
    container_name: wordpress_db
    image: mariadb:11
    environment:
      MARIADB_DATABASE: wordpress
      MARIADB_USER: wordpress
      MARIADB_PASSWORD: ${DB_PASSWORD}
      MARIADB_ROOT_PASSWORD: ${DB_PASSWORD}
    volumes:
      - db-data:/var/lib/mysql
    restart: unless-stopped

volumes:
  wp-data:
  db-data:
`

// umamiCompose is Umami + Postgres. Umami connects via DATABASE_URL to the
// service name `db`; first-run seeds the default admin (admin / umami).
const umamiCompose = `name: umami

services:
  umami:
    container_name: umami_app
    image: ghcr.io/umami-software/umami:postgresql-latest
    environment:
      DATABASE_TYPE: postgresql
      DATABASE_URL: postgresql://umami:${DB_PASSWORD}@db:5432/umami
      APP_SECRET: ${APP_SECRET}
    ports:
      - "3000:3000"
    depends_on:
      - db
    restart: unless-stopped

  db:
    container_name: umami_db
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: umami
      POSTGRES_USER: umami
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - db-data:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  db-data:
`

// listmonkCompose is Listmonk + Postgres. The app runs --install/--upgrade
// idempotently on every start (safe to re-run), reaches Postgres at `db`, and
// seeds the super admin (admin / listmonk).
const listmonkCompose = `name: listmonk

services:
  app:
    container_name: listmonk_app
    image: listmonk/listmonk:latest
    depends_on:
      - db
    command: ["sh", "-c", "./listmonk --install --idempotent --yes --config '' && ./listmonk --upgrade --yes --config '' && ./listmonk --config ''"]
    environment:
      LISTMONK_app__address: 0.0.0.0:9000
      LISTMONK_db__host: db
      LISTMONK_db__port: 5432
      LISTMONK_db__user: listmonk
      LISTMONK_db__password: ${DB_PASSWORD}
      LISTMONK_db__database: listmonk
      LISTMONK_db__ssl_mode: disable
      LISTMONK_ADMIN_USER: admin
      LISTMONK_ADMIN_PASSWORD: listmonk
      TZ: Etc/UTC
    ports:
      - "9000:9000"
    volumes:
      - uploads:/listmonk/uploads
    restart: unless-stopped

  db:
    container_name: listmonk_db
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: listmonk
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: listmonk
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U listmonk"]
      interval: 10s
      timeout: 5s
      retries: 6
    volumes:
      - db-data:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  uploads:
  db-data:
`

// plausibleCompose is Plausible + Postgres + ClickHouse. The app runs its
// createdb/migrate steps then serves. ClickHouse's IPv4-bind and low-resource
// tuning (needed on small boxes) are inlined via compose configs so the stack
// stays a single self-contained file. DATABASE_URL/CLICKHOUSE_DATABASE_URL
// point at the service names; SECRET_KEY_BASE is generated, BASE_URL defaults
// to localhost until a domain is routed.
const plausibleCompose = `name: plausible

services:
  plausible_db:
    container_name: plausible_db
    image: postgres:16-alpine
    restart: unless-stopped
    volumes:
      - db-data:/var/lib/postgresql/data
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      start_period: 1m

  plausible_events_db:
    container_name: plausible_events_db
    image: clickhouse/clickhouse-server:24.12-alpine
    restart: unless-stopped
    volumes:
      - event-data:/var/lib/clickhouse
    environment:
      CLICKHOUSE_SKIP_USER_SETUP: "1"
    ulimits:
      nofile:
        soft: 262144
        hard: 262144
    configs:
      - source: ch_ipv4
        target: /etc/clickhouse-server/config.d/ipv4-only.xml
      - source: ch_lowres
        target: /etc/clickhouse-server/config.d/low-resources.xml
      - source: ch_logs
        target: /etc/clickhouse-server/config.d/logs.xml
    healthcheck:
      test: ["CMD-SHELL", "wget --no-verbose --tries=1 -O - http://127.0.0.1:8123/ping || exit 1"]
      start_period: 1m

  plausible:
    container_name: plausible_app
    image: ghcr.io/plausible/community-edition:v3.2.1
    restart: unless-stopped
    command: sh -c "/entrypoint.sh db createdb && /entrypoint.sh db migrate && /entrypoint.sh run"
    depends_on:
      plausible_db:
        condition: service_healthy
      plausible_events_db:
        condition: service_healthy
    volumes:
      - plausible-data:/var/lib/plausible
    ports:
      - "8000:8000"
    environment:
      TMPDIR: /var/lib/plausible/tmp
      BASE_URL: ${BASE_URL}
      SECRET_KEY_BASE: ${SECRET_KEY_BASE}
      DATABASE_URL: postgres://postgres:${DB_PASSWORD}@plausible_db:5432/plausible_db
      CLICKHOUSE_DATABASE_URL: http://plausible_events_db:8123/plausible_events_db

configs:
  ch_ipv4:
    content: |
      <clickhouse><listen_host>0.0.0.0</listen_host></clickhouse>
  ch_lowres:
    content: |
      <clickhouse>
        <mark_cache_size>524288000</mark_cache_size>
        <max_server_memory_usage_to_ram_ratio>0.5</max_server_memory_usage_to_ram_ratio>
      </clickhouse>
  ch_logs:
    content: |
      <clickhouse>
        <logger><level>warning</level><console>true</console></logger>
        <query_log remove="remove"/>
        <query_thread_log remove="remove"/>
      </clickhouse>

volumes:
  db-data:
  event-data:
  plausible-data:
`
