# NetVantage Security Hardening Guide

> **Who is this for?** This guide is for anyone deploying NetVantage beyond a local development environment. If you're running `docker compose up` on your laptop for testing, most of this doesn't apply yet. Once you're deploying to staging or production — especially if monitoring production infrastructure or handling sensitive network data — work through this guide section by section. Each section is independent; prioritize based on your threat model.

This guide provides comprehensive security best practices and configuration options for hardening NetVantage deployments across development, staging, and production environments.

**Table of Contents:**
- [1. Overview & Security Principles](#1-overview--security-principles)
- [2. TLS Configuration](#2-tls-configuration)
- [3. Authentication & Authorization](#3-authentication--authorization)
- [4. Grafana SSO Setup](#4-grafana-sso-setup)
- [5. Secrets Management](#5-secrets-management)
- [6. Transport Security](#6-transport-security)
- [7. Database Security](#7-database-security)
- [8. Network Security](#8-network-security)
- [9. Container Security](#9-container-security)
- [10. Audit Logging](#10-audit-logging)
- [11. Agent Security](#11-agent-security)
- [12. Supply Chain Security](#12-supply-chain-security)
- [13. Pre-Deployment Security Checklist](#13-pre-deployment-security-checklist)

---

## 1. Overview & Security Principles

### Defense in Depth

NetVantage security is built on the principle of **defense in depth** — multiple overlapping security controls at each layer:

- **Perimeter**: TLS encryption, firewall rules, network policies
- **Application**: API key authentication, role-based access control (RBAC), OIDC/OAuth2 SSO
- **Transport**: NATS/Kafka encryption and SASL authentication
- **Data**: PostgreSQL SSL, encrypted secrets in transit and at rest
- **Operations**: Audit logging, container image signing, SBOM verification

```mermaid
graph TB
    subgraph Perimeter
        TLS[TLS 1.2+/1.3 Encryption]
        FW[Firewall Rules]
        NP[K8s NetworkPolicy]
    end

    subgraph Application
        Bearer[Bearer Token Auth]
        RBAC[Role-Based Access]
        RL[Rate Limiting]
        OIDC[OIDC/OAuth2 SSO]
    end

    subgraph Transport
        NATS_TLS[NATS TLS]
        KAFKA_SASL[Kafka SASL/SCRAM]
    end

    subgraph Data
        PG_SSL[PostgreSQL SSL]
        VAULT[Vault/SOPS Secrets]
        HASH[SHA-256 Key Hashing]
    end

    subgraph Operations
        AUDIT[Audit Logging]
        COSIGN[Image Signing — cosign]
        SBOM[SBOM Generation]
    end

    Perimeter --> Application --> Transport --> Data --> Operations
```

### Least Privilege

All components run with minimal required privileges:

- **Containers**: Non-root user (`uid 65534`), read-only root filesystem, all capabilities dropped except `NET_RAW` for agents
- **API Keys**: Role-based scoping (`admin`, `agent`), expiration enforced
- **Kubernetes**: NetworkPolicy defaults to deny-all ingress, whitelisting only necessary traffic
- **Agents**: Only `CAP_NET_RAW` for ping/traceroute (never `CAP_NET_ADMIN`)

---

## 2. TLS Configuration

### Control Plane API Server

**Why TLS matters here:** The Control Plane API transmits API keys, agent configurations, and test definitions. Without TLS, these travel in plaintext — anyone on the network path can intercept API keys and impersonate agents or administrators. In production, TLS is non-negotiable.

Enable TLS on the server to encrypt all API communication:

```bash
# Generate self-signed certificates (for testing only)
openssl req -x509 -newkey rsa:4096 -nodes \
  -out server.crt -keyout server.key -days 365 \
  -subj "/CN=netvantage.example.com"

# For production, use certificates from a trusted CA or Let's Encrypt
```

**Environment Variables:**

```bash
export NETVANTAGE_TLS_ENABLED=true
export NETVANTAGE_TLS_CERT=/etc/netvantage/certs/server.crt
export NETVANTAGE_TLS_KEY=/etc/netvantage/certs/server.key
export NETVANTAGE_TLS_CA=/etc/netvantage/certs/ca.crt  # For mTLS client verification
```

**Docker Compose:**

```yaml
server:
  volumes:
    - ./certs/server.crt:/etc/netvantage/certs/server.crt:ro
    - ./certs/server.key:/etc/netvantage/certs/server.key:ro
    - ./certs/ca.crt:/etc/netvantage/certs/ca.crt:ro
  environment:
    NETVANTAGE_TLS_ENABLED: "true"
    NETVANTAGE_TLS_CERT: /etc/netvantage/certs/server.crt
    NETVANTAGE_TLS_KEY: /etc/netvantage/certs/server.key
    NETVANTAGE_TLS_CA: /etc/netvantage/certs/ca.crt
```

**Kubernetes (Helm):**

```bash
# Create a TLS secret
kubectl create secret tls netvantage-server-tls \
  --cert=server.crt --key=server.key \
  -n netvantage

# Update values.yaml
cat > values-prod.yaml << 'EOF'
server:
  tls:
    enabled: true
    secretName: netvantage-server-tls
EOF

helm install netvantage deploy/helm/netvantage/ \
  -f values-prod.yaml -n netvantage
```

### Mutual TLS (mTLS) for Agents

Enforce client certificate verification to ensure only authorized agents can connect:

```bash
# Generate agent client certificate
openssl req -x509 -newkey rsa:4096 -nodes \
  -out agent-client.crt -keyout agent-client.key -days 365 \
  -subj "/CN=agent-client"

# Pass CA cert to agents for server verification
export NETVANTAGE_SERVER_CA=/etc/netvantage/certs/ca.crt
export NETVANTAGE_CLIENT_CERT=/etc/netvantage/certs/agent-client.crt
export NETVANTAGE_CLIENT_KEY=/etc/netvantage/certs/agent-client.key
```

### Certificate Rotation

Implement automated certificate rotation (pre-M9, manual process):

```bash
# Renewal checklist (30 days before expiry)
openssl x509 -in /etc/netvantage/certs/server.crt -noout -dates

# For Kubernetes, update the secret and trigger pod restart
kubectl create secret tls netvantage-server-tls \
  --cert=new-server.crt --key=new-server.key \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart pods to pick up new certificate
kubectl rollout restart deployment netvantage-server -n netvantage
```

---

## 3. Authentication & Authorization

### API Key Management

NetVantage uses **Bearer token authentication** with SHA-256 hashed storage:

**Provisioning API Keys:**

API keys are currently provisioned via the server's initialization process or database seeding. A key management API endpoint (`/api/v1/keys`) is planned for a future release.

To seed an initial admin key (typically done during first deployment):

```bash
# Generate a secure key
export API_KEY=$(openssl rand -hex 32)

# Hash it for storage
export KEY_HASH=$(echo -n "$API_KEY" | sha256sum | cut -d' ' -f1)

# Insert directly into PostgreSQL (initial setup only)
psql $NETVANTAGE_DB_URL -c "
  INSERT INTO api_keys (id, key_hash, name, role, created_at)
  VALUES ('key-admin-001', '$KEY_HASH', 'initial-admin', 'admin', NOW())
  ON CONFLICT DO NOTHING;
"

# Store the unhashed key securely — this is the value agents use
echo "API Key: $API_KEY"
```

**Key Rotation:**

```bash
# 1. Generate new key and insert into database
# 2. Deploy new key to agents
# 3. After grace period, delete old key from database
psql $NETVANTAGE_DB_URL -c "DELETE FROM api_keys WHERE id = 'key-old-001';"
```

**Roles and Permissions:**

| Role | Permissions | Use Case |
|------|-------------|----------|
| `admin` | Full API access (test CRUD, agent management, audit log, POP management) | Ops/SRE access only |
| `agent` | Read config, write heartbeat, register self only | Agent pods in Kubernetes |

**JWT Secret (for internal token signing):**

```bash
# Generate a strong 64-character secret
openssl rand -hex 32 > jwt_secret.txt

export NETVANTAGE_JWT_SECRET=$(cat jwt_secret.txt)

# Store securely in your secrets manager (Vault, K8s Secrets, etc.)
```

### Bearer Token Usage

All API requests must include the token:

```bash
# Example: Retrieve agent status
curl -H "Authorization: Bearer nv_pop1_c8f9d2e1a4b7..." \
  https://netvantage.example.com/api/v1/agents

# Agent heartbeat (automatic in agent code)
curl -H "Authorization: Bearer nv_pop1_..." \
  -X POST https://netvantage.example.com/api/v1/heartbeat
```

### API Key Best Practices

1. **Never commit keys to version control** — use `.gitignore` for `.env` files
2. **Rotate quarterly minimum** — automate via secrets manager
3. **Scope keys to the minimum required role** — agents use `agent` role only
4. **Use short-lived keys** — 90-day expiry for service accounts, 30-day for developers
5. **Audit all key operations** — see [Audit Logging](#10-audit-logging)
6. **Revoke immediately if compromised** — all agent traffic via revoked key is rejected

---

## 4. Grafana SSO Setup

### Prerequisites

- OIDC/OAuth2 identity provider (Okta, Auth0, Azure AD, Keycloak, etc.)
- Client credentials (client ID, client secret)
- Redirect URI whitelisted in IdP

### Configuration Options

Grafana uses **Generic OAuth2** (compatible with any OIDC provider):

```bash
export GF_AUTH_GENERIC_OAUTH_ENABLED=true
export GF_AUTH_GENERIC_OAUTH_NAME="Corporate SSO"
export GF_AUTH_GENERIC_OAUTH_CLIENT_ID="your-client-id"
export GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET="your-client-secret"
export GF_AUTH_GENERIC_OAUTH_AUTH_URL="https://idp.example.com/authorize"
export GF_AUTH_GENERIC_OAUTH_TOKEN_URL="https://idp.example.com/token"
export GF_AUTH_GENERIC_OAUTH_API_URL="https://idp.example.com/userinfo"
export GF_AUTH_GENERIC_OAUTH_SCOPES="openid profile email"
export GF_AUTH_GENERIC_OAUTH_ALLOW_SIGN_UP=true
export GF_COOKIE_SECURE=true
```

### Okta Integration Example

```bash
# 1. Create OAuth 2.0 application in Okta admin console
#    - App type: OIDC Web Application
#    - Sign-in method: Authorization Code
#    - Login URI: https://netvantage.example.com/login
#    - Redirect URIs: https://netvantage.example.com/login/generic_oauth

# 2. Get client credentials from Okta

export GF_AUTH_GENERIC_OAUTH_NAME="Okta"
export GF_AUTH_GENERIC_OAUTH_CLIENT_ID="0oaxxxxxxxxxxxxx"
export GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET="your-client-secret"
export GF_AUTH_GENERIC_OAUTH_AUTH_URL="https://your-okta-org.okta.com/oauth2/v1/authorize"
export GF_AUTH_GENERIC_OAUTH_TOKEN_URL="https://your-okta-org.okta.com/oauth2/v1/token"
export GF_AUTH_GENERIC_OAUTH_API_URL="https://your-okta-org.okta.com/oauth2/v1/userinfo"
export GF_AUTH_GENERIC_OAUTH_SCOPES="openid profile email"

# 3. Role mapping (optional)
# Map Okta groups to Grafana roles via:
export GF_AUTH_GENERIC_OAUTH_ROLE_ATTRIBUTE_PATH="groups"
```

### Auth0 Integration Example

```bash
# 1. Create Native Application in Auth0 dashboard

export GF_AUTH_GENERIC_OAUTH_NAME="Auth0"
export GF_AUTH_GENERIC_OAUTH_CLIENT_ID="your-auth0-client-id"
export GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET="your-auth0-client-secret"
export GF_AUTH_GENERIC_OAUTH_AUTH_URL="https://your-auth0-domain.auth0.com/authorize"
export GF_AUTH_GENERIC_OAUTH_TOKEN_URL="https://your-auth0-domain.auth0.com/oauth/token"
export GF_AUTH_GENERIC_OAUTH_API_URL="https://your-auth0-domain.auth0.com/userinfo"
export GF_AUTH_GENERIC_OAUTH_SCOPES="openid profile email"
```

### Azure AD Integration Example

```bash
# 1. Register application in Azure AD
# 2. Add credentials (Client Secret) to the app
# 3. Add Redirect URI: https://netvantage.example.com/login/generic_oauth

export GF_AUTH_GENERIC_OAUTH_NAME="Azure AD"
export GF_AUTH_GENERIC_OAUTH_CLIENT_ID="your-azure-app-id"
export GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET="your-azure-app-secret"
export GF_AUTH_GENERIC_OAUTH_AUTH_URL="https://login.microsoftonline.com/your-tenant-id/oauth2/v2.0/authorize"
export GF_AUTH_GENERIC_OAUTH_TOKEN_URL="https://login.microsoftonline.com/your-tenant-id/oauth2/v2.0/token"
export GF_AUTH_GENERIC_OAUTH_API_URL="https://graph.microsoft.com/oidc/userinfo"
export GF_AUTH_GENERIC_OAUTH_SCOPES="openid profile email"
```

### Docker Compose Setup

```yaml
grafana:
  environment:
    GF_AUTH_GENERIC_OAUTH_ENABLED: "true"
    GF_AUTH_GENERIC_OAUTH_NAME: "Corporate SSO"
    GF_AUTH_GENERIC_OAUTH_CLIENT_ID: "${GF_AUTH_OIDC_CLIENT_ID}"
    GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET: "${GF_AUTH_OIDC_CLIENT_SECRET}"
    GF_AUTH_GENERIC_OAUTH_AUTH_URL: "${GF_AUTH_OIDC_AUTH_URL}"
    GF_AUTH_GENERIC_OAUTH_TOKEN_URL: "${GF_AUTH_OIDC_TOKEN_URL}"
    GF_AUTH_GENERIC_OAUTH_API_URL: "${GF_AUTH_OIDC_API_URL}"
    GF_AUTH_GENERIC_OAUTH_SCOPES: "openid profile email"
    GF_AUTH_GENERIC_OAUTH_ALLOW_SIGN_UP: "true"
    GF_COOKIE_SECURE: "true"
    GF_AUTH_ANONYMOUS_ENABLED: "false"
```

### Kubernetes Helm Setup

```yaml
grafana:
  enabled: true
  auth:
    oidc:
      enabled: true
      name: "Corporate SSO"
      clientId: "your-client-id"
      clientSecretName: grafana-oidc-secret
      authUrl: "https://idp.example.com/authorize"
      tokenUrl: "https://idp.example.com/token"
      apiUrl: "https://idp.example.com/userinfo"
      scopes: "openid profile email"
    anonymous:
      enabled: false
```

Create the Grafana OIDC secret:

```bash
kubectl create secret generic grafana-oidc-secret \
  --from-literal=client-secret="your-client-secret" \
  -n netvantage
```

### Security Best Practices

1. **Disable anonymous access** — `GF_AUTH_ANONYMOUS_ENABLED=false`
2. **Enforce HTTPS cookies** — `GF_COOKIE_SECURE=true`
3. **Require group membership** — map IdP groups to Grafana roles
4. **Enable session timeout** — `GF_SESSION_MAX_LIFETIME=86400` (24 hours)
5. **Audit Grafana user activity** — check Grafana audit logs periodically

---

## 5. Secrets Management

NetVantage supports multiple secrets providers for production deployments:

### Provider Options

| Provider | Best For | Complexity |
|----------|----------|-----------|
| `env` (default) | Development, Docker Compose | Low |
| `vault` | HashiCorp Vault integration | Medium |
| `k8s` | Kubernetes Secrets | Medium |
| `sops` | GitOps with encrypted secrets in Git | High |

### Environment Variables (Default)

Suitable for small deployments, Docker Compose, and development:

```bash
export NETVANTAGE_SECRETS_PROVIDER=env
export NETVANTAGE_DB_URL="postgres://netvantage:strong-password@postgres:5432/netvantage?sslmode=require"
export NETVANTAGE_JWT_SECRET="random-64-character-secret"
export NETVANTAGE_KAFKA_SASL_PASSWORD="kafka-broker-password"
```

**Never store secrets in `.env` files committed to Git:**

```bash
# .gitignore
.env
.env.local
.env.production
secrets/
certs/
```

### HashiCorp Vault Integration

For enterprise deployments:

```bash
export NETVANTAGE_SECRETS_PROVIDER=vault
export VAULT_ADDR=https://vault.example.com:8200
export VAULT_TOKEN=s.xxxxxxxxxxxxxxxx
export NETVANTAGE_VAULT_MOUNT=secret/data/netvantage
export VAULT_NAMESPACE=admin/netvantage
```

**Vault Secret Structure:**

```
secret/data/netvantage
├── db-url
├── jwt-secret
├── kafka-password
├── tls-cert
└── tls-key
```

**Example: Reading secrets from Vault**

```bash
# Authenticate to Vault
vault login -method=oidc

# Write secrets
vault kv put secret/netvantage \
  db-url="postgres://..." \
  jwt-secret="..." \
  kafka-password="..."

# NetVantage server automatically reads and decrypts
```

**Vault AppRole (for agents):**

```bash
# Create AppRole for agents
vault write auth/approle/role/netvantage-agent \
  bind_secret_id=true \
  secret_id_ttl=24h

# Issue RoleID and SecretID to agents
vault read auth/approle/role/netvantage-agent/role-id
vault write -f auth/approle/role/netvantage-agent/secret-id
```

### Kubernetes Secrets

For Kubernetes deployments:

```bash
export NETVANTAGE_SECRETS_PROVIDER=k8s

# Create secrets manually
kubectl create secret generic netvantage-secrets \
  --from-literal=NETVANTAGE_DB_URL="postgres://..." \
  --from-literal=NETVANTAGE_JWT_SECRET="..." \
  -n netvantage

# Or use External Secrets Operator with Vault/AWS/GCP
```

**Helm values.yaml:**

```yaml
server:
  envFromSecrets:
    - netvantage-secrets
```

### SOPS (Secrets Operations) for GitOps

For managing encrypted secrets in Git:

```bash
export NETVANTAGE_SECRETS_PROVIDER=sops

# Install sops and age key
brew install sops age
age-keygen -o key.txt

# Encrypt secrets file
sops secrets.yaml

# Deploy from encrypted secrets
sops -d secrets.yaml | kubectl apply -f -
```

**`.sops.yaml` configuration:**

```yaml
creation_rules:
  - path_regex: secrets\..*
    age: age1xxx...xxx  # Public key from age-keygen
    provider: age
```

### Secret Rotation

Implement a secret rotation schedule:

```bash
# Monthly rotation for:
# 1. JWT_SECRET — issue new token signing key
# 2. Database password — update PostgreSQL user
# 3. API key credentials — create new keys, revoke old

# Process:
# 1. Generate new secret in secrets manager
# 2. Update environment or Vault
# 3. Restart affected services (rolling restart in Kubernetes)
# 4. Monitor for issues
# 5. Audit logs for successful rotation
```

---

## 6. Transport Security

### NATS JetStream (Default, M1-M8)

**Why encrypt the transport?** Test results flowing through NATS/Kafka contain network topology information — target IPs, DNS records, HTTP responses, traceroute paths, BGP routing data. In multi-tenant or shared-network environments, this data could reveal your infrastructure's attack surface if intercepted.

NATS runs over plaintext by default in development. Secure for production:

```bash
# Self-signed TLS for NATS (development only)
openssl req -x509 -newkey rsa:4096 -nodes \
  -out nats.crt -keyout nats.key -days 365 \
  -subj "/CN=nats"

# Docker Compose
nats:
  command:
    - "--jetstream"
    - "--tls"
    - "--tlscert=/etc/nats/nats.crt"
    - "--tlskey=/etc/nats/nats.key"
  volumes:
    - ./certs/nats.crt:/etc/nats/nats.crt:ro
    - ./certs/nats.key:/etc/nats/nats.key:ro
```

**NATS client (agent) configuration:**

```bash
# Agent connects to NATS over TLS
export NATS_URL=tls://nats:4222
export NATS_TLS_CERT=/etc/nats/client.crt
export NATS_TLS_KEY=/etc/nats/client.key
export NATS_TLS_CA=/etc/nats/ca.crt
```

### Kafka Transport (Production Scale, M9+)

For >50 POPs, use Kafka with SASL/SCRAM and TLS:

```bash
export NETVANTAGE_TRANSPORT=kafka
export NETVANTAGE_KAFKA_BROKERS=kafka-1:9092,kafka-2:9092,kafka-3:9092
export NETVANTAGE_KAFKA_SASL_ENABLED=true
export NETVANTAGE_KAFKA_SASL_MECHANISM=SCRAM-SHA-256  # or SCRAM-SHA-512
export NETVANTAGE_KAFKA_SASL_USERNAME=netvantage
export NETVANTAGE_KAFKA_SASL_PASSWORD=strong-password-here
export NETVANTAGE_KAFKA_TLS_ENABLED=true
export NETVANTAGE_KAFKA_TLS_CERT=/etc/kafka/client.crt
export NETVANTAGE_KAFKA_TLS_KEY=/etc/kafka/client.key
export NETVANTAGE_KAFKA_TLS_CA=/etc/kafka/ca.crt
```

**Kafka Broker Configuration (server.properties):**

```properties
# Enable SASL/SCRAM authentication
security.inter.broker.protocol=SASL_SSL
sasl.mechanism.inter.broker.protocol=SCRAM-SHA-256
listeners=SASL_SSL://0.0.0.0:9092
advertised.listeners=SASL_SSL://kafka-1.example.com:9092

# TLS configuration
ssl.keystore.location=/etc/kafka/secrets/kafka.server.keystore.jks
ssl.keystore.password=keystore-password
ssl.key.password=key-password
ssl.truststore.location=/etc/kafka/secrets/kafka.server.truststore.jks
ssl.truststore.password=truststore-password

# SCRAM user database
listener.security.protocol.map=SASL_SSL:SASL_SSL
sasl.enabled.mechanisms=SCRAM-SHA-256
```

**Create SCRAM user in Kafka:**

```bash
kafka-configs.sh --bootstrap-server localhost:9092 \
  --alter --add-config 'SCRAM-SHA-256=[iterations=8192,password=strong-password]' \
  --entity-type users --entity-name netvantage
```

**Docker Compose with Kafka:**

```yaml
kafka:
  image: confluentinc/cp-kafka:7.5.0
  environment:
    KAFKA_BROKER_ID: 1
    KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
    KAFKA_ADVERTISED_LISTENERS: SASL_SSL://kafka:9092
    KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: SASL_SSL:SASL_SSL
    KAFKA_SASL_MECHANISM: SCRAM-SHA-256
    KAFKA_SASL_JAAS_CONFIG: |
      org.apache.kafka.common.security.scram.ScramLoginModule required
      username="netvantage"
      password="strong-password";
    KAFKA_SSL_KEYSTORE_LOCATION: /etc/kafka/secrets/kafka.server.keystore.jks
    KAFKA_SSL_KEYSTORE_PASSWORD: keystore-password
    KAFKA_SSL_KEY_PASSWORD: key-password
    KAFKA_SSL_TRUSTSTORE_LOCATION: /etc/kafka/secrets/kafka.server.truststore.jks
    KAFKA_SSL_TRUSTSTORE_PASSWORD: truststore-password
  volumes:
    - ./certs/kafka.keystore.jks:/etc/kafka/secrets/kafka.server.keystore.jks:ro
    - ./certs/kafka.truststore.jks:/etc/kafka/secrets/kafka.server.truststore.jks:ro
```

---

## 7. Database Security

### PostgreSQL SSL Configuration

Enforce SSL for all database connections:

```bash
export NETVANTAGE_DB_URL="postgres://netvantage:password@postgres:5432/netvantage?sslmode=require&sslcert=/certs/client.crt&sslkey=/certs/client.key&sslrootcert=/certs/ca.crt"
```

**PostgreSQL server configuration (`postgresql.conf`):**

```conf
# Enforce SSL
ssl = on
ssl_cert_file = '/etc/postgresql/certs/server.crt'
ssl_key_file = '/etc/postgresql/certs/server.key'

# Require SSL for all connections (except local Unix socket)
hostssl all all 0.0.0.0/0 scram-sha-256
```

**Docker Compose:**

```yaml
postgres:
  image: postgres:16-alpine
  environment:
    POSTGRES_DB: netvantage
    POSTGRES_USER: netvantage
    POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    POSTGRES_INITDB_ARGS: "-c ssl=on -c ssl_cert_file=/etc/postgresql/certs/server.crt -c ssl_key_file=/etc/postgresql/certs/server.key"
  volumes:
    - ./certs/server.crt:/etc/postgresql/certs/server.crt:ro
    - ./certs/server.key:/etc/postgresql/certs/server.key:ro
    - postgres-data:/var/lib/postgresql/data
```

### Database Backups

Implement secure backup procedures:

```bash
# Automated daily backup with encryption
pg_dump -Fc -U netvantage \
  -h postgres.netvantage.svc.cluster.local \
  netvantage > backup-$(date +%Y%m%d).dump

# Encrypt backup
gpg --encrypt --recipient backup-key@example.com backup-20250321.dump

# Store in encrypted S3 bucket (or similar)
aws s3 cp backup-20250321.dump.gpg \
  s3://netvantage-backups-encrypted/ \
  --sse=AES256

# Retention: keep 30-day daily backups + 4 weekly backups
```

### Connection Pooling

Use a connection pool (PgBouncer) for production:

```bash
# PgBouncer configuration
[databases]
netvantage = host=postgres port=5432 dbname=netvantage

[pgbouncer]
pool_mode = transaction
max_client_conn = 1000
default_pool_size = 25
min_pool_size = 5
reserve_pool_size = 5
```

**Docker Compose:**

```yaml
pgbouncer:
  image: edoburu/pgbouncer
  environment:
    DATABASE_URL: "postgres://netvantage:${POSTGRES_PASSWORD}@postgres:5432/netvantage"
    PGBOUNCER_POOL_MODE: transaction
    PGBOUNCER_MAX_CLIENT_CONN: 1000
  ports:
    - "6432:6432"
  depends_on:
    - postgres
```

### Migration Safety

Database migrations are versioned and idempotent:

```bash
# Migrations stored in migrations/ directory
ls migrations/
001_init.up.sql
001_init.down.sql
002_add_audit_logs.up.sql
002_add_audit_logs.down.sql

# All migrations use IF NOT EXISTS and ON CONFLICT
# Example: CREATE TABLE IF NOT EXISTS ...
# Example: ON CONFLICT DO UPDATE SET ...

# Backup before running migrations
pg_dump -Fc netvantage > backup-pre-migration.dump

# Run migrations (managed by NetVantage server startup)
netvantage-server  # Automatically runs pending migrations
```

---

## 8. Network Security

### Kubernetes NetworkPolicy

NetVantage deploys with a default-deny NetworkPolicy — only explicitly permitted traffic flows:

```yaml
# Default deny all ingress
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: netvantage-default-deny
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/part-of: netvantage
  policyTypes:
    - Ingress
---
# Allow ingress to server from ingress controller and agents
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: netvantage-server-ingress
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: server
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - port: 8080
          protocol: TCP
---
# Allow processor metrics to Prometheus
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: netvantage-processor-metrics
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: processor
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - port: 9091
          protocol: TCP
```

**To allow agents from outside the cluster:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: netvantage-agents-external
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: server
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - port: 8080
          protocol: TCP
      from:
        - namespaceSelector:
            matchLabels:
              name: netvantage-agents  # External agents namespace
```

### Firewall Rules (Reference)

For bare-metal or cloud deployments, whitelist only necessary inbound traffic:

| Component | Port | Protocol | Source | Purpose |
|-----------|------|----------|--------|---------|
| Control Plane API | 8080 | TCP | Agents, Ingress controller, Admin | API requests |
| Control Plane (TLS) | 8443 | TCP | Agents, Ingress controller | API requests (TLS) |
| Prometheus scrape | 9090 | TCP | Prometheus, internal only | Metrics |
| Grafana | 3000 | TCP | Ingress controller, Admin | Dashboard access |
| AlertManager | 9093 | TCP | Prometheus, internal only | Alert webhooks |
| NATS JetStream | 4222 | TCP | Agents, Processor | Message transport |
| NATS monitoring | 8222 | TCP | Prometheus, internal only | NATS metrics |
| PostgreSQL | 5432 | TCP | Server, Processor, internal only | Database |
| Kafka | 9092 | TCP | Server, Processor, Agents | Message transport |
| Routinator HTTP API | 8323 | TCP | BGP Analyzer, internal only | RPKI validation |
| Routinator RTR | 3323 | TCP | BGP routers (if local), internal only | RPKI cache |

### Prometheus/Alertmanager UI Protection

Never expose Prometheus and Alertmanager UIs directly to the internet:

**Option 1: Behind Ingress with Authentication**

```yaml
apiVersion: networking.k8s.io/networking.k8s.io/v1
kind: Ingress
metadata:
  name: netvantage-prometheus
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/auth-secret: prometheus-basic-auth
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - prometheus.netvantage.example.com
      secretName: prometheus-tls
  rules:
    - host: prometheus.netvantage.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: prometheus
                port:
                  number: 9090
```

**Option 2: OAuth2 Proxy**

```yaml
prometheus:
  authProxy:
    enabled: true
    image:
      repository: quay.io/oauth2-proxy/oauth2-proxy
      tag: v7.6.0
    config:
      provider: oidc
      oidc_issuer_url: https://idp.example.com
      client_id: prometheus-oauth-client-id
      client_secret: prometheus-oauth-client-secret
      cookie_secure: "true"
```

**Option 3: Private Ingress (internal traffic only)**

```bash
# Use a private ingress controller only accessible from VPN/Bastion
kubectl patch service prometheus-server \
  -p '{"spec": {"type": "ClusterIP"}}'
```

### Egress Rules for Agents

Agents require outbound connectivity for:

| Destination | Port | Purpose | Required |
|-------------|------|---------|----------|
| Control Plane API | 8080/8443 | Config sync, heartbeat, result upload | Yes |
| External targets | 53 (DNS) | DNS canary queries | Yes |
| External targets | 80/443 (HTTP/HTTPS) | HTTP/HTTPS canary | Yes |
| External targets | ICMP | Ping canary | Yes (requires CAP_NET_RAW) |
| Traceroute targets | UDP (various) | Traceroute | Yes |
| NTP servers | 123 (UDP) | Time synchronization | Recommended |
| BGP collectors | TCP 179 (for internal BGP) | BGP data (if using ExaBGP) | No (RouteViews/RIPE RIS only) |

**Kubernetes egress NetworkPolicy for agents:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: netvantage-agents-egress
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: agent
  policyTypes:
    - Egress
  egress:
    # Allow to control plane server
    - to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/component: server
      ports:
        - protocol: TCP
          port: 8080
    # Allow DNS to kube-dns
    - to:
        - namespaceSelector:
            matchLabels:
              name: kube-system
      ports:
        - protocol: UDP
          port: 53
    # Allow external traffic (for canaries)
    - to:
        - namespaceSelector: {}
      ports:
        - protocol: TCP
          port: 80
        - protocol: TCP
          port: 443
        - protocol: UDP
          port: 53
```

---

## 9. Container Security

### Image Signing & Verification

NetVantage uses **cosign keyless signing** (sigstore) to ensure image authenticity:

**Automatic in CI/CD (.github/workflows/ci.yml):**

```bash
# Container job signs all images
cosign sign --yes ghcr.io/shankar0123/netvantage-server:abc123def
cosign sign --yes ghcr.io/shankar0123/netvantage-agent:abc123def
cosign sign --yes ghcr.io/shankar0123/netvantage-processor:abc123def
```

**Verify image signature locally:**

```bash
# Install cosign
brew install sigstore/tools/cosign

# Verify before deploying
cosign verify \
  --certificate-identity-regexp="https://github.com/.*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/shankar0123/netvantage-server:v1.0.0

# If valid: Verification successful!
```

**Kubernetes admission webhook (production):**

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: image-signature-verification
webhooks:
  - name: verify.sigstore.dev
    clientConfig:
      service:
        name: webhook-service
        namespace: cosign-system
        path: "/verify"
      caBundle: LS0tLS1CRUdJTi... # base64-encoded CA cert
    rules:
      - operations: ["CREATE", "UPDATE"]
        apiGroups: ["apps"]
        apiVersions: ["v1"]
        resources: ["deployments", "daemonsets"]
```

### Software Bill of Materials (SBOM)

NetVantage generates SPDX-format SBOMs for all images:

```bash
# Automatically generated in CI/CD
ls *.spdx.json
agent-sbom.spdx.json
processor-sbom.spdx.json
server-sbom.spdx.json

# View SBOM
cat server-sbom.spdx.json | jq '.packages[] | {name, version, downloadLocation}'
```

**Audit SBOM for known vulnerabilities:**

```bash
# Use SBOM tools
pip install sbom-tool

sbom-tool validate --manifest-path server-sbom.spdx.json

# Cross-check against CVE database
# (cyclonedx-bom, grype, trivy can consume SBOMs)
grype server-sbom.spdx.json
```

### Container Runtime Security

**Seccomp profiles (restrict syscalls):**

```json
{
  "defaultAction": "SCMP_ACT_ERRNO",
  "defaultErrnoRet": 1,
  "archMap": [
    {
      "architecture": "SCMP_ARCH_X86_64",
      "subArchitectures": ["SCMP_ARCH_X86", "SCMP_ARCH_X32"]
    }
  ],
  "syscalls": [
    {
      "names": ["read", "write", "open", "close", "stat", "fstat", "lstat"],
      "action": "SCMP_ACT_ALLOW"
    }
  ]
}
```

**AppArmor profile (Linux):**

```bash
# Example AppArmor profile for server
profile netvantage-server flags=(attach_disconnected) {
  #include <abstractions/base>

  /etc/netvantage/** r,
  /var/lib/netvantage/** rw,
  capability dac_read_search,
  capability setuid,
  capability setgid,
}

# Load profile
sudo apparmor_parser -r -T -W /etc/apparmor.d/netvantage-server
```

### Security Context (Kubernetes)

All containers enforce strict security contexts:

```yaml
securityContext:
  runAsNonRoot: true        # Never run as root
  runAsUser: 65534          # Nobody user
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
    add:
      - NET_RAW  # Only for agents (ping/traceroute)
```

### Vulnerability Scanning

**Trivy scans all images in CI/CD:**

```bash
# Manual scan
trivy image ghcr.io/shankar0123/netvantage-server:latest --severity CRITICAL,HIGH

# Scan local Dockerfile
trivy image --input Dockerfile.server

# Generate SARIF report (for GitHub Security tab)
trivy image --format sarif --output trivy-report.sarif ghcr.io/.../image:tag
```

---

## 10. Audit Logging

### Audit Logging Configuration

Enable audit logging for all control plane mutations (create, update, delete):

```bash
export NETVANTAGE_AUDIT_ENABLED=true
```

**Kubernetes Helm:**

```yaml
server:
  env:
    NETVANTAGE_AUDIT_ENABLED: "true"
```

### What Gets Logged

Audit logs include:

- **API endpoint**: `/api/v1/tests`, `/api/v1/agents`, `/api/v1/pops`, `/api/v1/audit`
- **Actor**: API key ID (hashed for security)
- **Action**: `create`, `read`, `update`, `delete`, `list`
- **Resource**: Test ID, Agent ID, Key ID, etc.
- **Changes**: Before/after diff for updates
- **Timestamp**: RFC3339 with nanosecond precision
- **Client IP**: Source of request
- **Result**: Success/failure with error details

### Audit Log Format

```json
{
  "timestamp": "2025-03-21T10:23:45.123456789Z",
  "actor": "key_abc123...",
  "action": "create",
  "resource_type": "test",
  "resource_id": "test_xyz789",
  "changes": {
    "name": {"before": "", "after": "ping-google.com"},
    "target": {"before": "", "after": "8.8.8.8"}
  },
  "client_ip": "203.0.113.42",
  "status": "success"
}
```

### Querying Audit Logs

**PostgreSQL:**

```bash
# All mutations by a specific API key
SELECT * FROM audit_logs WHERE actor = 'key_abc123' ORDER BY timestamp DESC;

# All deletions in last 7 days
SELECT * FROM audit_logs
  WHERE action = 'delete'
  AND timestamp > NOW() - INTERVAL '7 days'
  ORDER BY timestamp DESC;

# Test configuration changes
SELECT * FROM audit_logs
  WHERE resource_type = 'test'
  AND action IN ('create', 'update', 'delete')
  ORDER BY timestamp DESC;
```

### Log Retention

Configure retention policy:

```bash
# 90-day retention in PostgreSQL (adjust as needed for compliance)
DELETE FROM audit_logs WHERE timestamp < NOW() - INTERVAL '90 days';

# Automate with cron
0 2 * * * psql -U netvantage netvantage -c \
  "DELETE FROM audit_logs WHERE timestamp < NOW() - INTERVAL '90 days';"
```

### Log Export for SIEM

Export audit logs to external SIEM (Splunk, ELK, Datadog, etc.):

```bash
# Example: Stream to Splunk HEC
SELECT to_jsonb(audit_logs.*) AS event FROM audit_logs
  WHERE timestamp > NOW() - INTERVAL '1 minute'
  ORDER BY timestamp ASC \
| curl -X POST https://splunk.example.com:8088/services/collector \
  -H "Authorization: Splunk XXXXXXXX-..." \
  --data-binary @-
```

---

## 11. Agent Security

### CAP_NET_RAW Least Privilege

Agents require only `CAP_NET_RAW` for ping and traceroute:

```yaml
# Kubernetes securityContext
securityContext:
  capabilities:
    drop:
      - ALL
    add:
      - NET_RAW  # Allows raw socket access for ICMP
```

Never run agents with `CAP_NET_ADMIN`, `CAP_SYS_ADMIN`, or other elevated capabilities.

### Agent Configuration Caching

Agents cache config locally for resilience when control plane is unreachable:

```bash
# Config cached to
~/.netvantage/config-cache.yaml

# Never includes secrets in cache — tokens, passwords stored separately
# Cache automatically synced on startup if control plane is available
```

**Encryption at rest (recommended for M9):**

```bash
# Encrypt config cache with AES-256-GCM
# Key: derived from agent identity certificate or Vault

# In practice: run agent in encrypted container volume
```

### Agent API Key Rotation

Rotate agent credentials regularly. Since API keys are currently provisioned via database seeding (a `/api/v1/keys` management endpoint is planned), rotation is done directly in PostgreSQL:

```bash
# 1. Generate new key
export NEW_KEY=$(openssl rand -hex 32)
export KEY_HASH=$(echo -n "$NEW_KEY" | sha256sum | cut -d' ' -f1)

# 2. Insert new key into database
psql $NETVANTAGE_DB_URL -c "
  INSERT INTO api_keys (id, key_hash, name, role, created_at)
  VALUES ('key-agent-pop1-v2', '$KEY_HASH', 'agent-pop-1-v2', 'agent', NOW());
"

# 3. Deploy new key to agent (via ConfigMap, Secret, or config management)
# 4. After confirming new key works, revoke old key
psql $NETVANTAGE_DB_URL -c "DELETE FROM api_keys WHERE id = 'key-agent-pop1-v1';"
```

### Agent Heartbeat

Agents send heartbeat every 60 seconds (configurable):

```bash
export AGENT_HEARTBEAT_INTERVAL=60s
```

Control plane marks agents offline if heartbeat missing for 5 minutes:

```bash
export NETVANTAGE_AGENT_STALE_AFTER=5m
```

**Monitor agent health:**

```bash
# Prometheus metric
netvantage_agent_heartbeat_total{agent_id="pop-us-east-1"}

# Alert if agent offline
- alert: NetVantageAgentDown
  expr: up{job="netvantage-agents"} == 0
  for: 5m
```

---

## 12. Supply Chain Security

### Build Reproducibility

All builds use pinned dependencies and Go module lock files:

```bash
# go.mod and go.sum committed to repository
go mod tidy && git add go.mod go.sum

# Python: requirements.txt with pinned versions
pip freeze > bgp-analyzer/requirements.txt
```

### Dependency Scanning

**govulncheck** scans Go dependencies:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

**Safety** scans Python dependencies:

```bash
pip install safety
safety check --file bgp-analyzer/requirements.txt
```

### CI/CD Pipeline Integrity

**GitHub Actions branch protection:**

```bash
# Require signed commits
git config commit.gpgsign true
git config user.signingkey XXXXXXX

# Status checks required before merge
- NetVantage/ci/go
- NetVantage/ci/python
- NetVantage/ci/security
- NetVantage/ci/helm
```

**Secret management in CI:**

```yaml
# Never log secrets
env:
  DEBUG: false  # Disable debug logging that might expose secrets

# Use GitHub Secrets for credentials
- name: Login to GHCR
  uses: docker/login-action@v3
  with:
    registry: ghcr.io
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}  # Never log
```

### Release Signing

**Sign releases with GPG:**

```bash
# Create release with signatures
gpg --detach-sign --armor netvantage-server-v1.0.0-linux-amd64

# Publish checksums
sha256sum netvantage-* > CHECKSUMS

# Sign checksums
gpg --detach-sign CHECKSUMS

# Publish release
gh release create v1.0.0 \
  netvantage-* CHECKSUMS CHECKSUMS.sig
```

**Users verify:**

```bash
# Import public key
gpg --import <(curl https://github.com/user.gpg)

# Verify signature
gpg --verify CHECKSUMS.sig CHECKSUMS

# Verify file
sha256sum -c CHECKSUMS
```

---

## 13. Pre-Deployment Security Checklist

Use this checklist before deploying to production:

### TLS & Certificates
- [ ] TLS enabled on control plane API (`NETVANTAGE_TLS_ENABLED=true`)
- [ ] Valid certificate from trusted CA (not self-signed in production)
- [ ] Certificate valid for correct hostname(s)
- [ ] Certificate expiry monitored (alert at 30, 14, 7, 1 days)
- [ ] mTLS enabled for agents if required by security policy
- [ ] NATS/Kafka TLS enabled
- [ ] PostgreSQL SSL enforced (`sslmode=require`)

### Authentication & Authorization
- [ ] JWT secret changed from default (`NETVANTAGE_JWT_SECRET` not "dev-secret-...")
- [ ] JWT secret stored in secure secrets manager, not in code
- [ ] All agents have unique API keys with `agent` role
- [ ] Admin API keys rotated quarterly minimum
- [ ] API key expiry set (recommend 90 days maximum)
- [ ] No API keys committed to version control (`.gitignore` includes `.env`)
- [ ] Bearer token auth enforced on all API endpoints

### Grafana SSO
- [ ] Grafana OIDC/OAuth2 configured with trusted IdP
- [ ] Anonymous access disabled (`GF_AUTH_ANONYMOUS_ENABLED=false`)
- [ ] Secure cookies enforced (`GF_COOKIE_SECURE=true`)
- [ ] Default admin password changed
- [ ] Session timeout configured (e.g., 24 hours)
- [ ] Admin user count minimized (principle of least privilege)

### Secrets Management
- [ ] Secrets provider configured (`vault`, `k8s`, or `sops` for production)
- [ ] No plaintext secrets in `.env` files
- [ ] All secrets rotated before deployment
- [ ] Database password meets strength requirements (16+ chars, mixed case/numbers/symbols)
- [ ] Secrets stored in encrypted at-rest system (Vault, K8s Secrets with etcd encryption)
- [ ] Access to secrets audited and logged

### Transport Security
- [ ] NATS JetStream TLS enabled or Kafka SASL/TLS enabled
- [ ] Kafka SASL mechanism: SCRAM-SHA-256 or SCRAM-SHA-512 (never PLAIN)
- [ ] Kafka TLS certificates validated
- [ ] Message transport authenticated and encrypted end-to-end

### Database Security
- [ ] PostgreSQL SSL enabled (`ssl=on`)
- [ ] Database user has minimal required privileges (no superuser)
- [ ] Connection pooling configured (PgBouncer or similar)
- [ ] Automated backups enabled and tested
- [ ] Backups encrypted and stored securely
- [ ] Database password strong (16+ characters)
- [ ] pg_hba.conf restricts connections to SSL only

### Network Security
- [ ] Kubernetes NetworkPolicy enabled (default-deny ingress)
- [ ] Firewall rules whitelist only necessary ports/sources
- [ ] Prometheus/Alertmanager UIs not exposed to internet
- [ ] Prometheus/Alertmanager behind auth proxy or private network
- [ ] Ingress controller uses valid TLS certificates
- [ ] Egress rules for agents restrict outbound traffic to necessary destinations
- [ ] No unnecessary services exposed publicly

### Container Security
- [ ] All images signed with cosign (verify signatures on pull)
- [ ] Container runtime security enabled (seccomp, AppArmor, or SELinux)
- [ ] Security contexts enforced: non-root user, read-only root, drop ALL caps
- [ ] Agents run with NET_RAW only (never CAP_NET_ADMIN)
- [ ] SBOM generated and audited for vulnerabilities
- [ ] No secrets baked into container images (use mounting)
- [ ] Container image scanning passes (no CRITICAL vulnerabilities)
- [ ] Base images regularly updated

### Audit Logging
- [ ] Audit logging enabled (`NETVANTAGE_AUDIT_ENABLED=true`)
- [ ] Audit logs persistently stored (PostgreSQL table)
- [ ] Audit log retention policy configured (90+ days minimum)
- [ ] Audit logs monitored for suspicious activity (admin alerts)
- [ ] Access to audit logs restricted (read-only to authorized personnel)
- [ ] Audit logs exported to SIEM if required by compliance

### Agent Security
- [ ] Agents use unique API keys (not shared credentials)
- [ ] Agent API keys scoped to `agent` role only
- [ ] Agent config caching configured for resilience
- [ ] Heartbeat monitoring enabled
- [ ] Agent pods run with CAP_NET_RAW only
- [ ] Local result buffer configured (persist if control plane down)

### Supply Chain
- [ ] Go/Python dependencies scanned for vulnerabilities
- [ ] go.mod and go.sum committed and locked
- [ ] Python requirements.txt pinned (no wildcards like `requests>=2.0`)
- [ ] CI/CD pipeline requires security gates to pass
- [ ] Branch protection rules enforce signed commits
- [ ] Releases signed and checksums published
- [ ] Deployment binaries verified (signature check)

### Operational
- [ ] Incident response plan documented
- [ ] Secrets rotation schedule defined (at least quarterly)
- [ ] Backup and restore tested (run restore test monthly)
- [ ] Disaster recovery runbook documented
- [ ] Security patches monitored (base images, dependencies)
- [ ] Access controls documented (who has admin, what POPs they manage)
- [ ] Logging and monitoring working end-to-end
- [ ] Alerting configured for security events
- [ ] Load testing completed at expected scale
- [ ] Penetration testing scheduled (if required by org)

### Compliance (if applicable)
- [ ] Data retention policy complies with regulations (GDPR, HIPAA, etc.)
- [ ] Data classification schema applied
- [ ] Data subject access requests procedure documented
- [ ] Data deletion procedure tested
- [ ] DPA/BAA with cloud providers in place
- [ ] Security assessment completed (SOC2, ISO27001, etc. if required)

---

## Additional Resources

- **NetVantage Documentation**: See [ARCHITECTURE.md](ARCHITECTURE.md), [deployment-guide.md](deployment-guide.md)
- **Go Security**: https://go.dev/blog/security
- **OWASP**: https://owasp.org/
- **CIS Benchmarks**: https://www.cisecurity.org/cis-benchmarks/
- **Kubernetes Security**: https://kubernetes.io/docs/concepts/security/
- **NIST Cybersecurity Framework**: https://www.nist.gov/cyberframework

---

## Questions & Support

For security questions or to report vulnerabilities:

- **Vulnerability Disclosure**: See SECURITY.md (responsible disclosure policy)
- **GitHub Security Advisory**: https://github.com/advisories
- **Community Discussion**: NetVantage GitHub Discussions (security tag)
