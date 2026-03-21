# NetVantage Production Deployment Guide

This guide covers deploying NetVantage in production across different environments: Kubernetes (Helm), Docker Compose, and bare-metal. For development setup, see [quickstart.md](quickstart.md).

## Architecture Overview

A production NetVantage deployment consists of:

- **Hub** (centralized): Control Plane Server, Metrics Processor, PostgreSQL, Prometheus, Grafana, Alertmanager, NATS/Kafka, Routinator
- **POPs** (distributed): Canary Agents deployed at each vantage point
- **BGP Analyzer**: Can run on the hub or as an independent service

## Kubernetes Deployment (Helm)

### Prerequisites

- Kubernetes 1.26+
- Helm 3.12+
- `kubectl` configured for your cluster
- Container registry access (GHCR or your own)

### Install

```bash
# Add the chart (or use local path)
helm install netvantage deploy/helm/netvantage/ \
  --namespace netvantage --create-namespace \
  -f values-prod.yaml
```

### Production values-prod.yaml

Create a `values-prod.yaml` with your production overrides:

```yaml
server:
  replicaCount: 3
  envFromSecrets:
    - netvantage-prod-secrets

postgresql:
  auth:
    existingSecret: netvantage-db-prod
  primary:
    persistence:
      size: 100Gi

prometheus:
  server:
    retention: 90d
    persistentVolume:
      size: 200Gi

grafana:
  auth:
    oidc:
      enabled: true
      clientId: "your-client-id"
      clientSecretName: grafana-oidc-secret
      authUrl: "https://idp.example.com/authorize"
      tokenUrl: "https://idp.example.com/token"
      apiUrl: "https://idp.example.com/userinfo"

ingress:
  enabled: true
  className: nginx
  tls:
    - secretName: netvantage-tls
      hosts:
        - netvantage.example.com
```

### Secrets Management

Create Kubernetes secrets before deploying:

```bash
kubectl create secret generic netvantage-prod-secrets \
  --namespace netvantage \
  --from-literal=NETVANTAGE_DB_URL='postgres://...' \
  --from-literal=NETVANTAGE_JWT_SECRET='...'

kubectl create secret generic netvantage-db-prod \
  --namespace netvantage \
  --from-literal=postgres-password='...' \
  --from-literal=password='...'
```

For production, use an external secrets operator (e.g., External Secrets Operator with Vault, AWS Secrets Manager, or GCP Secret Manager).

### Verify Deployment

```bash
kubectl get pods -n netvantage
kubectl logs -f deployment/netvantage-server -n netvantage
helm test netvantage -n netvantage
```

## Docker Compose Deployment

Suitable for small deployments (<10 POPs) or staging environments.

### Setup

```bash
# Copy and configure environment
cp .env.example .env
# Edit .env with production values (strong passwords, OIDC config, etc.)

# Start services
docker compose --env-file .env up -d

# Run migrations
docker compose exec postgres psql -U netvantage -d netvantage \
  -f /migrations/001_initial_schema.sql
docker compose exec postgres psql -U netvantage -d netvantage \
  -f /migrations/002_audit_log.sql
```

### TLS Termination

For Docker Compose, place an nginx or Caddy reverse proxy in front:

```yaml
# Add to docker-compose.override.yml
services:
  caddy:
    image: caddy:2-alpine
    ports:
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
    depends_on:
      - grafana
      - server
```

## POP Agent Deployment

### AWS

Deploy agents on EC2 instances across regions:

```bash
# User data script for EC2 launch template
#!/bin/bash
curl -fsSL https://get.netvantage.io/agent | bash -s -- \
  --server-url https://netvantage.example.com \
  --pop-name us-east-1-aws \
  --api-key $AGENT_API_KEY
```

Recommended instance types: `t3.micro` or `t4g.micro` (ARM). The agent uses minimal resources (<50MB RAM, <0.1 vCPU).

Security group requirements: see [Network Requirements](#network-requirements) below.

### GCP

Deploy as Compute Engine instances or GKE DaemonSets:

```bash
gcloud compute instances create netvantage-agent-us-central1 \
  --zone=us-central1-a \
  --machine-type=e2-micro \
  --metadata-from-file=startup-script=agent-install.sh
```

### Azure

Deploy as Azure VMs or AKS DaemonSets:

```bash
az vm create --resource-group netvantage \
  --name netvantage-agent-eastus \
  --image Ubuntu2204 \
  --size Standard_B1ls \
  --custom-data agent-install.sh
```

### Bare-Metal / On-Premises

Download the agent binary directly:

```bash
# Download latest release
curl -fsSL -o netvantage-agent \
  https://github.com/shankar0123/netvantage/releases/latest/download/netvantage-agent-linux-amd64
chmod +x netvantage-agent

# Verify signature
cosign verify-blob --signature netvantage-agent.sig netvantage-agent

# Create systemd service
cat > /etc/systemd/system/netvantage-agent.service << 'EOF'
[Unit]
Description=NetVantage Canary Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=netvantage
ExecStart=/usr/local/bin/netvantage-agent
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

systemctl enable --now netvantage-agent
```

## Network Requirements

### Agent (POP) Outbound

| Destination | Port | Protocol | Purpose |
|---|---|---|---|
| Control Plane | 8080/tcp (or 443) | HTTPS | Config sync, heartbeat, registration |
| NATS | 4222/tcp | NATS | Publish test results |
| Kafka (if used) | 9092/tcp | Kafka | Publish test results (alternative) |
| Test targets | ICMP | ICMP | Ping canary |
| Test targets | 53/udp, 53/tcp | DNS | DNS canary |
| Test targets | 80/tcp, 443/tcp | HTTP/S | HTTP canary |
| Test targets | Any | UDP/ICMP | Traceroute canary |

### Agent Required Capabilities

- `CAP_NET_RAW` — Required for ICMP ping and traceroute
- `mtr` binary — Required for traceroute (mtr backend)
- `scamper` binary — Optional (scamper backend)

### Hub Inbound

| Source | Port | Protocol | Purpose |
|---|---|---|---|
| Agents | 8080/tcp | HTTPS | Control Plane API |
| Agents | 4222/tcp | NATS | Transport (results) |
| Prometheus | 9091/tcp | HTTP | Processor metrics scrape |
| Users | 3000/tcp | HTTPS | Grafana dashboards |
| Users | 9090/tcp | HTTP | Prometheus UI (restrict in production) |
| Users | 9093/tcp | HTTP | Alertmanager UI (restrict in production) |

### Firewall Notes

- In production, Prometheus (9090) and Alertmanager (9093) UIs should be behind an authenticated reverse proxy or restricted to internal networks only.
- For Kafka deployments, ensure brokers are accessible from all agents on port 9092 (or your configured port).
- BGP Analyzer requires outbound access to RouteViews/RIPE RIS collectors.

## Security Hardening Checklist

- [ ] Change all default passwords (PostgreSQL, Grafana admin)
- [ ] Enable TLS for Control Plane API (`NETVANTAGE_TLS_ENABLED=true`)
- [ ] Enable SASL/TLS for Kafka transport (if using Kafka)
- [ ] Configure Grafana OAuth2/OIDC SSO
- [ ] Restrict Prometheus/Alertmanager UI access
- [ ] Enable audit logging (`NETVANTAGE_AUDIT_ENABLED=true`)
- [ ] Use Kubernetes Secrets or Vault for sensitive configuration
- [ ] Apply NetworkPolicy in Kubernetes deployments
- [ ] Verify container image signatures with `cosign verify`
- [ ] Review SBOM for known vulnerabilities
- [ ] Set resource limits on all containers
- [ ] Enable `securityContext.runAsNonRoot` on all pods
- [ ] Rotate API keys and JWT secrets regularly
