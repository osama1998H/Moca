package generate

// ---------------------------------------------------------------------------
// Caddy
// ---------------------------------------------------------------------------

const caddyTmpl = `{{.Header}}

{
	{{- if .TLSEmail}}
	email {{.TLSEmail}}
	{{- end}}
}

{{- if .Multitenant}}
*.{{.Domain}} {
{{- else}}
{{.Domain}} {
{{- end}}
	{{- if eq .TLSProvider "manual"}}
	tls /etc/ssl/certs/moca.crt /etc/ssl/private/moca.key
	{{- else if eq .TLSProvider ""}}
	tls internal
	{{- end}}

	encode gzip zstd

	@websocket {
		header Connection *Upgrade*
		header Upgrade websocket
	}
	reverse_proxy @websocket localhost:{{.Port}}

	reverse_proxy localhost:{{.Port}} {
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-For {remote_host}
		header_up X-Forwarded-Proto {scheme}
		{{- if .Multitenant}}
		header_up X-Moca-Site {host}
		{{- end}}
	}

	handle_path /assets/* {
		root * {{.ProjectRoot}}/desk/dist
		file_server
	}

	log {
		output file {{.ProjectRoot}}/logs/caddy.log
		format json
	}
}
`

// ---------------------------------------------------------------------------
// NGINX
// ---------------------------------------------------------------------------

const nginxTmpl = `{{.Header}}

upstream moca_server {
    server 127.0.0.1:{{.Port}};
}

server {
    listen 80;
    {{- if .Multitenant}}
    server_name *.{{.Domain}};
    {{- else}}
    server_name {{.Domain}};
    {{- end}}
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    {{- if .Multitenant}}
    server_name *.{{.Domain}};
    {{- else}}
    server_name {{.Domain}};
    {{- end}}

    {{- if eq .TLSProvider "acme"}}
    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    {{- else}}
    ssl_certificate /etc/ssl/certs/moca.crt;
    ssl_certificate_key /etc/ssl/private/moca.key;
    {{- end}}

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    client_max_body_size 50m;

    gzip on;
    gzip_types text/plain application/json application/javascript text/css;

    location /assets/ {
        alias {{.ProjectRoot}}/desk/dist/;
        expires 30d;
        add_header Cache-Control "public, immutable";
    }

    location / {
        proxy_pass http://moca_server;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        {{- if .Multitenant}}
        proxy_set_header X-Moca-Site $host;
        {{- end}}

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }
}
`

// ---------------------------------------------------------------------------
// Systemd
// ---------------------------------------------------------------------------

const systemdServerTmpl = `{{.Header}}

[Unit]
Description=Moca API Server (instance %i)
After=network.target postgresql.service redis.service
Wants=network.target

[Service]
Type=simple
User={{.User}}
Group={{.User}}
WorkingDirectory={{.ProjectRoot}}
ExecStart={{.ProjectRoot}}/bin/moca-server --port %i
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=moca-server-%i
Environment=MOCA_ENV=production
Environment=MOCA_LOG_LEVEL={{.LogLevel}}
LimitNOFILE=65535

[Install]
WantedBy=moca.target
`

const systemdWorkerTmpl = `{{.Header}}

[Unit]
Description=Moca Background Worker (instance %i)
After=network.target postgresql.service redis.service
Wants=network.target

[Service]
Type=simple
User={{.User}}
Group={{.User}}
WorkingDirectory={{.ProjectRoot}}
ExecStart={{.ProjectRoot}}/bin/moca-worker --id %i
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=moca-worker-%i
Environment=MOCA_ENV=production
Environment=MOCA_LOG_LEVEL={{.LogLevel}}
LimitNOFILE=65535

[Install]
WantedBy=moca.target
`

const systemdSchedulerTmpl = `{{.Header}}

[Unit]
Description=Moca Cron Scheduler
After=network.target postgresql.service redis.service
Wants=network.target

[Service]
Type=simple
User={{.User}}
Group={{.User}}
WorkingDirectory={{.ProjectRoot}}
ExecStart={{.ProjectRoot}}/bin/moca-scheduler
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=moca-scheduler
Environment=MOCA_ENV=production
Environment=MOCA_LOG_LEVEL={{.LogLevel}}

[Install]
WantedBy=moca.target
`

const systemdOutboxTmpl = `{{.Header}}

[Unit]
Description=Moca Outbox Poller (Outbox to Kafka)
After=network.target postgresql.service redis.service
Wants=network.target

[Service]
Type=simple
User={{.User}}
Group={{.User}}
WorkingDirectory={{.ProjectRoot}}
ExecStart={{.ProjectRoot}}/bin/moca-outbox
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=moca-outbox
Environment=MOCA_ENV=production
Environment=MOCA_LOG_LEVEL={{.LogLevel}}

[Install]
WantedBy=moca.target
`

const systemdSearchSyncTmpl = `{{.Header}}

[Unit]
Description=Moca Search Sync (Kafka to Meilisearch)
After=network.target postgresql.service redis.service
Wants=network.target

[Service]
Type=simple
User={{.User}}
Group={{.User}}
WorkingDirectory={{.ProjectRoot}}
ExecStart={{.ProjectRoot}}/bin/moca-outbox --mode search-sync
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=moca-search-sync
Environment=MOCA_ENV=production
Environment=MOCA_LOG_LEVEL={{.LogLevel}}

[Install]
WantedBy=moca.target
`

const systemdTargetTmpl = `{{.Header}}

[Unit]
Description=Moca Application ({{.ProjectName}})
{{- range .Wants}}
Wants={{.}}
{{- end}}
{{- range .Wants}}
After={{.}}
{{- end}}

[Install]
WantedBy=multi-user.target
`

// ---------------------------------------------------------------------------
// Docker
// ---------------------------------------------------------------------------

const dockerComposeTmpl = `{{.Header}}

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: {{.DBUser}}
      POSTGRES_PASSWORD: {{.DBPassword}}
      POSTGRES_DB: {{.DBName}}
    ports:
      - "{{.DBPort}}:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U {{.DBUser}}"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    {{- if .RedisPassword}}
    command: redis-server --requirepass {{.RedisPassword}}
    {{- end}}
    ports:
      - "{{.RedisPort}}:6379"
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5
{{- if .IncludeKafka}}

  zookeeper:
    image: confluentinc/cp-zookeeper:7.6.0
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
    volumes:
      - zkdata:/var/lib/zookeeper/data

  kafka:
    image: confluentinc/cp-kafka:7.6.0
    depends_on:
      - zookeeper
    ports:
      - "9092:9092"
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:29092,PLAINTEXT_HOST://localhost:9092
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT
      KAFKA_INTER_BROKER_LISTENER_NAME: PLAINTEXT
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
    volumes:
      - kafkadata:/var/lib/kafka/data
{{- end}}
{{- if .IncludeMeili}}

  meilisearch:
    image: getmeili/meilisearch:v1.12
    ports:
      - "{{.SearchPort}}:7700"
    {{- if .SearchAPIKey}}
    environment:
      MEILI_MASTER_KEY: {{.SearchAPIKey}}
    {{- end}}
    volumes:
      - meilidata:/meili_data
{{- end}}
{{- if .IncludeMinio}}

  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    volumes:
      - miniodata:/data
{{- end}}

  moca-server:
    build:
      context: ../../
      dockerfile: config/docker/Dockerfile
    ports:
      - "{{.Port}}:{{.Port}}"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      MOCA_ENV: production
      MOCA_DB_HOST: postgres
      MOCA_DB_PORT: 5432
      MOCA_DB_USER: {{.DBUser}}
      MOCA_DB_PASSWORD: {{.DBPassword}}
      MOCA_REDIS_HOST: redis
      MOCA_REDIS_PORT: 6379
      MOCA_PORT: {{.Port}}
      MOCA_LOG_LEVEL: {{.LogLevel}}
    command: ["./bin/moca-server"]

  moca-worker:
    build:
      context: ../../
      dockerfile: config/docker/Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      MOCA_ENV: production
      MOCA_DB_HOST: postgres
      MOCA_DB_PORT: 5432
      MOCA_DB_USER: {{.DBUser}}
      MOCA_DB_PASSWORD: {{.DBPassword}}
      MOCA_REDIS_HOST: redis
      MOCA_REDIS_PORT: 6379
      MOCA_LOG_LEVEL: {{.LogLevel}}
    command: ["./bin/moca-worker"]

  moca-scheduler:
    build:
      context: ../../
      dockerfile: config/docker/Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      MOCA_ENV: production
      MOCA_DB_HOST: postgres
      MOCA_DB_PORT: 5432
      MOCA_DB_USER: {{.DBUser}}
      MOCA_DB_PASSWORD: {{.DBPassword}}
      MOCA_REDIS_HOST: redis
      MOCA_REDIS_PORT: 6379
      MOCA_LOG_LEVEL: {{.LogLevel}}
    command: ["./bin/moca-scheduler"]
{{- if .IncludeKafka}}

  moca-outbox:
    build:
      context: ../../
      dockerfile: config/docker/Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      kafka:
        condition: service_started
    environment:
      MOCA_ENV: production
      MOCA_DB_HOST: postgres
      MOCA_DB_PORT: 5432
      MOCA_DB_USER: {{.DBUser}}
      MOCA_DB_PASSWORD: {{.DBPassword}}
      MOCA_REDIS_HOST: redis
      MOCA_REDIS_PORT: 6379
      MOCA_KAFKA_BROKERS: kafka:29092
      MOCA_LOG_LEVEL: {{.LogLevel}}
    command: ["./bin/moca-outbox"]
{{- end}}

volumes:
  pgdata:
  redisdata:
{{- if .IncludeKafka}}
  zkdata:
  kafkadata:
{{- end}}
{{- if .IncludeMeili}}
  meilidata:
{{- end}}
{{- if .IncludeMinio}}
  miniodata:
{{- end}}
`

const dockerComposeProdTmpl = `{{.Header}}

services:
  moca-server:
    restart: always
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 1G
        reservations:
          cpus: "0.5"
          memory: 256M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

  moca-worker:
    restart: always
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
        reservations:
          cpus: "0.25"
          memory: 128M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

  moca-scheduler:
    restart: always
    deploy:
      resources:
        limits:
          cpus: "0.5"
          memory: 256M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
{{- if .IncludeKafka}}

  moca-outbox:
    restart: always
    deploy:
      resources:
        limits:
          cpus: "0.5"
          memory: 256M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
{{- end}}

  postgres:
    restart: always

  redis:
    restart: always
`

const dockerfileTmpl = `{{.Header}}

FROM golang:1.26-bookworm AS builder

WORKDIR /src

COPY go.work ./
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o bin/moca-server ./cmd/moca-server/ && \
    CGO_ENABLED=0 go build -o bin/moca-worker ./cmd/moca-worker/ && \
    CGO_ENABLED=0 go build -o bin/moca-scheduler ./cmd/moca-scheduler/ && \
    CGO_ENABLED=0 go build -o bin/moca-outbox ./cmd/moca-outbox/ && \
    CGO_ENABLED=0 go build -o bin/moca ./cmd/moca/

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates postgresql-client && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /src/bin/ ./bin/

EXPOSE {{.Port}}

CMD ["./bin/moca-server"]
`

const dockerIgnoreTmpl = `{{.Header}}
.git
.github
.moca
node_modules
desk/node_modules
*.log
backups
spikes
docs
*.md
!README.md
.env
.env.*
`

// ---------------------------------------------------------------------------
// Kubernetes
// ---------------------------------------------------------------------------

const k8sDeploymentTmpl = `{{.Header}}

apiVersion: apps/v1
kind: Deployment
metadata:
  name: moca-server
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: moca-server
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  replicas: {{.Replicas}}
  selector:
    matchLabels:
      app.kubernetes.io/name: moca-server
  template:
    metadata:
      labels:
        app.kubernetes.io/name: moca-server
    spec:
      containers:
        - name: moca-server
          image: {{.ImageName}}
          command: ["./bin/moca-server"]
          ports:
            - containerPort: {{.Port}}
              name: http
          envFrom:
            - configMapRef:
                name: moca-config
            - secretRef:
                name: moca-secrets
          resources:
            requests:
              cpu: 500m
              memory: 256Mi
            limits:
              cpu: "2"
              memory: 1Gi
          readinessProbe:
            httpGet:
              path: /api/method/ping
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /api/method/ping
              port: http
            initialDelaySeconds: 15
            periodSeconds: 20
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: moca-worker
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: moca-worker
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: moca-worker
  template:
    metadata:
      labels:
        app.kubernetes.io/name: moca-worker
    spec:
      containers:
        - name: moca-worker
          image: {{.ImageName}}
          command: ["./bin/moca-worker"]
          envFrom:
            - configMapRef:
                name: moca-config
            - secretRef:
                name: moca-secrets
          resources:
            requests:
              cpu: 250m
              memory: 128Mi
            limits:
              cpu: "1"
              memory: 512Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: moca-scheduler
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: moca-scheduler
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: moca-scheduler
  template:
    metadata:
      labels:
        app.kubernetes.io/name: moca-scheduler
    spec:
      containers:
        - name: moca-scheduler
          image: {{.ImageName}}
          command: ["./bin/moca-scheduler"]
          envFrom:
            - configMapRef:
                name: moca-config
            - secretRef:
                name: moca-secrets
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 500m
              memory: 256Mi
{{- if .KafkaEnabled}}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: moca-outbox
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: moca-outbox
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: moca-outbox
  template:
    metadata:
      labels:
        app.kubernetes.io/name: moca-outbox
    spec:
      containers:
        - name: moca-outbox
          image: {{.ImageName}}
          command: ["./bin/moca-outbox"]
          envFrom:
            - configMapRef:
                name: moca-config
            - secretRef:
                name: moca-secrets
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 500m
              memory: 256Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: moca-search-sync
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: moca-search-sync
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: moca-search-sync
  template:
    metadata:
      labels:
        app.kubernetes.io/name: moca-search-sync
    spec:
      containers:
        - name: moca-search-sync
          image: {{.ImageName}}
          command: ["./bin/moca-outbox", "--mode", "search-sync"]
          envFrom:
            - configMapRef:
                name: moca-config
            - secretRef:
                name: moca-secrets
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 500m
              memory: 256Mi
{{- end}}
`

const k8sServiceTmpl = `{{.Header}}

apiVersion: v1
kind: Service
metadata:
  name: moca-server
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: moca-server
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  type: ClusterIP
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app.kubernetes.io/name: moca-server
`

const k8sIngressTmpl = `{{.Header}}

apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: moca-ingress
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/part-of: {{.ProjectName}}
  annotations:
    {{- if eq .TLSProvider "acme"}}
    cert-manager.io/cluster-issuer: letsencrypt-prod
    {{- end}}
    nginx.ingress.kubernetes.io/proxy-body-size: 50m
    nginx.ingress.kubernetes.io/proxy-read-timeout: "86400"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "86400"
spec:
  ingressClassName: nginx
  {{- if .Domain}}
  tls:
    - hosts:
        - {{.Domain}}
      secretName: moca-tls
  rules:
    - host: {{.Domain}}
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: moca-server
                port:
                  number: 80
  {{- end}}
`

const k8sConfigMapTmpl = `{{.Header}}

apiVersion: v1
kind: ConfigMap
metadata:
  name: moca-config
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/part-of: {{.ProjectName}}
data:
  MOCA_ENV: production
  MOCA_PORT: "{{.Port}}"
  MOCA_LOG_LEVEL: "{{.LogLevel}}"
  MOCA_DB_HOST: "{{.DBHost}}"
  MOCA_DB_PORT: "{{.DBPort}}"
  MOCA_REDIS_HOST: "{{.RedisHost}}"
  MOCA_REDIS_PORT: "{{.RedisPort}}"
  {{- if .SearchEnabled}}
  MOCA_SEARCH_HOST: "{{.SearchHost}}"
  MOCA_SEARCH_PORT: "{{.SearchPort}}"
  {{- end}}
  {{- if .KafkaEnabled}}
  MOCA_KAFKA_ENABLED: "true"
  MOCA_KAFKA_BROKERS: "{{.KafkaBrokers}}"
  {{- end}}
`

const k8sSecretTmpl = `{{.Header}}
# Replace placeholder values before applying.

apiVersion: v1
kind: Secret
metadata:
  name: moca-secrets
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/part-of: {{.ProjectName}}
type: Opaque
stringData:
  MOCA_DB_USER: "REPLACE_ME"
  MOCA_DB_PASSWORD: "REPLACE_ME"
  MOCA_REDIS_PASSWORD: "REPLACE_ME"
  {{- if .SearchEnabled}}
  MOCA_SEARCH_API_KEY: "REPLACE_ME"
  {{- end}}
  {{- if .KafkaEnabled}}
  MOCA_KAFKA_CREDENTIALS: "REPLACE_ME"
  {{- end}}
`

const k8sHPATmpl = `{{.Header}}
# TODO: Configure Prometheus adapter for custom metrics (MS-24).

apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: moca-server-hpa
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: moca-server
  minReplicas: {{.Replicas}}
  maxReplicas: {{.MaxReplicas}}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
`

const k8sPDBTmpl = `{{.Header}}

apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: moca-server-pdb
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/part-of: {{.ProjectName}}
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: moca-server
`

// ---------------------------------------------------------------------------
// Supervisor
// ---------------------------------------------------------------------------

const supervisorTmpl = `{{.Header}}

[supervisord]
logfile={{.ProjectRoot}}/logs/supervisord.log
pidfile={{.ProjectRoot}}/.moca/supervisord.pid
nodaemon=false

[program:moca-server]
command={{.ProjectRoot}}/bin/moca-server
directory={{.ProjectRoot}}
user={{.User}}
autostart=true
autorestart=true
redirect_stderr=true
stdout_logfile={{.ProjectRoot}}/logs/moca-server.log
environment=MOCA_ENV="production",MOCA_LOG_LEVEL="{{.LogLevel}}"

[program:moca-worker]
command={{.ProjectRoot}}/bin/moca-worker --id %(process_num)02d
directory={{.ProjectRoot}}
user={{.User}}
numprocs=2
process_name=%(program_name)s_%(process_num)02d
autostart=true
autorestart=true
redirect_stderr=true
stdout_logfile={{.ProjectRoot}}/logs/moca-worker-%(process_num)02d.log
environment=MOCA_ENV="production",MOCA_LOG_LEVEL="{{.LogLevel}}"

[program:moca-scheduler]
command={{.ProjectRoot}}/bin/moca-scheduler
directory={{.ProjectRoot}}
user={{.User}}
autostart=true
autorestart=true
redirect_stderr=true
stdout_logfile={{.ProjectRoot}}/logs/moca-scheduler.log
environment=MOCA_ENV="production",MOCA_LOG_LEVEL="{{.LogLevel}}"
{{- if .KafkaEnabled}}

[program:moca-outbox]
command={{.ProjectRoot}}/bin/moca-outbox
directory={{.ProjectRoot}}
user={{.User}}
autostart=true
autorestart=true
redirect_stderr=true
stdout_logfile={{.ProjectRoot}}/logs/moca-outbox.log
environment=MOCA_ENV="production",MOCA_LOG_LEVEL="{{.LogLevel}}"

[program:moca-search-sync]
command={{.ProjectRoot}}/bin/moca-outbox --mode search-sync
directory={{.ProjectRoot}}
user={{.User}}
autostart=true
autorestart=true
redirect_stderr=true
stdout_logfile={{.ProjectRoot}}/logs/moca-search-sync.log
environment=MOCA_ENV="production",MOCA_LOG_LEVEL="{{.LogLevel}}"
{{- end}}

[group:moca]
programs=moca-server,moca-worker,moca-scheduler{{- if .KafkaEnabled}},moca-outbox,moca-search-sync{{- end}}
`

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

const envDotenvTmpl = `{{.Header}}

# Project
MOCA_PROJECT_NAME={{.ProjectName}}
MOCA_PROJECT_VERSION={{.ProjectVersion}}
MOCA_ENV=production

# Server
MOCA_PORT={{.Port}}
MOCA_WORKERS={{.Workers}}
MOCA_LOG_LEVEL={{.LogLevel}}

# Database
MOCA_DB_HOST={{.DBHost}}
MOCA_DB_PORT={{.DBPort}}
MOCA_DB_USER={{.DBUser}}
MOCA_DB_PASSWORD={{.DBPassword}}
MOCA_DB_NAME={{.DBName}}
MOCA_DB_POOL_SIZE={{.DBPoolSize}}

# Redis
MOCA_REDIS_HOST={{.RedisHost}}
MOCA_REDIS_PORT={{.RedisPort}}
MOCA_REDIS_PASSWORD={{.RedisPassword}}
MOCA_REDIS_DB_CACHE={{.RedisDbCache}}
MOCA_REDIS_DB_QUEUE={{.RedisDbQueue}}
MOCA_REDIS_DB_SESSION={{.RedisDbSession}}
MOCA_REDIS_DB_PUBSUB={{.RedisDbPubSub}}
{{- if .KafkaEnabled}}

# Kafka
MOCA_KAFKA_ENABLED=true
MOCA_KAFKA_BROKERS={{.KafkaBrokers}}
{{- end}}
{{- if .SearchEnabled}}

# Search
MOCA_SEARCH_ENGINE={{.SearchEngine}}
MOCA_SEARCH_HOST={{.SearchHost}}
MOCA_SEARCH_PORT={{.SearchPort}}
MOCA_SEARCH_API_KEY={{.SearchAPIKey}}
{{- end}}
{{- if .StorageEnabled}}

# Storage
MOCA_STORAGE_DRIVER={{.StorageDriver}}
MOCA_STORAGE_ENDPOINT={{.StorageEndpoint}}
MOCA_STORAGE_BUCKET={{.StorageBucket}}
MOCA_STORAGE_ACCESS_KEY={{.StorageAccessKey}}
MOCA_STORAGE_SECRET_KEY={{.StorageSecretKey}}
{{- end}}

# TLS
MOCA_TLS_PROVIDER={{.TLSProvider}}
MOCA_TLS_EMAIL={{.TLSEmail}}

# Scheduler
MOCA_SCHEDULER_ENABLED={{.SchedulerEnabled}}
MOCA_SCHEDULER_TICK={{.SchedulerTick}}
`

const envDockerTmpl = `{{.Header}}

MOCA_PROJECT_NAME={{.ProjectName}}
MOCA_PROJECT_VERSION={{.ProjectVersion}}
MOCA_ENV=production
MOCA_PORT={{.Port}}
MOCA_WORKERS={{.Workers}}
MOCA_LOG_LEVEL={{.LogLevel}}
MOCA_DB_HOST={{.DBHost}}
MOCA_DB_PORT={{.DBPort}}
MOCA_DB_USER={{.DBUser}}
MOCA_DB_PASSWORD={{.DBPassword}}
MOCA_DB_NAME={{.DBName}}
MOCA_DB_POOL_SIZE={{.DBPoolSize}}
MOCA_REDIS_HOST={{.RedisHost}}
MOCA_REDIS_PORT={{.RedisPort}}
MOCA_REDIS_PASSWORD={{.RedisPassword}}
MOCA_REDIS_DB_CACHE={{.RedisDbCache}}
MOCA_REDIS_DB_QUEUE={{.RedisDbQueue}}
MOCA_REDIS_DB_SESSION={{.RedisDbSession}}
MOCA_REDIS_DB_PUBSUB={{.RedisDbPubSub}}
{{- if .KafkaEnabled}}
MOCA_KAFKA_ENABLED=true
MOCA_KAFKA_BROKERS={{.KafkaBrokers}}
{{- end}}
{{- if .SearchEnabled}}
MOCA_SEARCH_ENGINE={{.SearchEngine}}
MOCA_SEARCH_HOST={{.SearchHost}}
MOCA_SEARCH_PORT={{.SearchPort}}
MOCA_SEARCH_API_KEY={{.SearchAPIKey}}
{{- end}}
{{- if .StorageEnabled}}
MOCA_STORAGE_DRIVER={{.StorageDriver}}
MOCA_STORAGE_ENDPOINT={{.StorageEndpoint}}
MOCA_STORAGE_BUCKET={{.StorageBucket}}
MOCA_STORAGE_ACCESS_KEY={{.StorageAccessKey}}
MOCA_STORAGE_SECRET_KEY={{.StorageSecretKey}}
{{- end}}
MOCA_TLS_PROVIDER={{.TLSProvider}}
MOCA_TLS_EMAIL={{.TLSEmail}}
MOCA_SCHEDULER_ENABLED={{.SchedulerEnabled}}
MOCA_SCHEDULER_TICK={{.SchedulerTick}}
`

const envSystemdTmpl = `{{.Header}}

Environment="MOCA_PROJECT_NAME={{.ProjectName}}"
Environment="MOCA_PROJECT_VERSION={{.ProjectVersion}}"
Environment="MOCA_ENV=production"
Environment="MOCA_PORT={{.Port}}"
Environment="MOCA_WORKERS={{.Workers}}"
Environment="MOCA_LOG_LEVEL={{.LogLevel}}"
Environment="MOCA_DB_HOST={{.DBHost}}"
Environment="MOCA_DB_PORT={{.DBPort}}"
Environment="MOCA_DB_USER={{.DBUser}}"
Environment="MOCA_DB_PASSWORD={{.DBPassword}}"
Environment="MOCA_DB_NAME={{.DBName}}"
Environment="MOCA_DB_POOL_SIZE={{.DBPoolSize}}"
Environment="MOCA_REDIS_HOST={{.RedisHost}}"
Environment="MOCA_REDIS_PORT={{.RedisPort}}"
Environment="MOCA_REDIS_PASSWORD={{.RedisPassword}}"
Environment="MOCA_REDIS_DB_CACHE={{.RedisDbCache}}"
Environment="MOCA_REDIS_DB_QUEUE={{.RedisDbQueue}}"
Environment="MOCA_REDIS_DB_SESSION={{.RedisDbSession}}"
Environment="MOCA_REDIS_DB_PUBSUB={{.RedisDbPubSub}}"
{{- if .KafkaEnabled}}
Environment="MOCA_KAFKA_ENABLED=true"
Environment="MOCA_KAFKA_BROKERS={{.KafkaBrokers}}"
{{- end}}
{{- if .SearchEnabled}}
Environment="MOCA_SEARCH_ENGINE={{.SearchEngine}}"
Environment="MOCA_SEARCH_HOST={{.SearchHost}}"
Environment="MOCA_SEARCH_PORT={{.SearchPort}}"
Environment="MOCA_SEARCH_API_KEY={{.SearchAPIKey}}"
{{- end}}
{{- if .StorageEnabled}}
Environment="MOCA_STORAGE_DRIVER={{.StorageDriver}}"
Environment="MOCA_STORAGE_ENDPOINT={{.StorageEndpoint}}"
Environment="MOCA_STORAGE_BUCKET={{.StorageBucket}}"
Environment="MOCA_STORAGE_ACCESS_KEY={{.StorageAccessKey}}"
Environment="MOCA_STORAGE_SECRET_KEY={{.StorageSecretKey}}"
{{- end}}
Environment="MOCA_TLS_PROVIDER={{.TLSProvider}}"
Environment="MOCA_TLS_EMAIL={{.TLSEmail}}"
Environment="MOCA_SCHEDULER_ENABLED={{.SchedulerEnabled}}"
Environment="MOCA_SCHEDULER_TICK={{.SchedulerTick}}"
`
