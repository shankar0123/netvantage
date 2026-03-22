# NetVantage Control Plane API Reference

The NetVantage Control Plane API provides centralized management of synthetic network tests, point-of-presence (POP) infrastructure, and agent registration. It enables autonomous configuration of distributed canary agents and collects metrics into a unified Prometheus pipeline.

**Version:** v1
**Base URL:** `http://<server>:<port>/api/v1` (authenticated routes)
**Health Check:** `GET /healthz` (unauthenticated)

---

## Table of Contents

1. [Overview](#overview)
2. [Authentication](#authentication)
3. [Rate Limiting](#rate-limiting)
4. [Error Responses](#error-responses)
5. [Agent Management](#agent-management)
6. [Point of Presence (POP) Management](#point-of-presence-pop-management)
7. [Test Definition Management](#test-definition-management)
8. [Config Sync](#config-sync)
9. [Audit Log](#audit-log)
10. [Canary Configuration Examples](#canary-configuration-examples)

---

## Overview

The NetVantage Control Plane API follows REST conventions with JSON request/response bodies. All authenticated endpoints require an API key in the `X-API-Key` header or `Authorization: Bearer` header.

**Key concepts:**

- **Agent:** A running canary instance deployed at a specific POP. Registers itself on startup and sends periodic heartbeats.
- **POP (Point of Presence):** A geographic or logical location where agents are deployed. Agents are assigned to POPs for organizational and scoping purposes.
- **Test Definition:** A reusable test specification (ping, DNS, HTTP, traceroute) that can be assigned to one or more POPs. Tests include timing, retry logic, and canary-specific configuration.
- **Test Assignment:** Links a test definition to a POP (or to all POPs if empty).
- **Audit Log:** Immutable record of all mutations (create, update, delete) with actor, timestamp, source IP, and change diff.

---

## Authentication

All endpoints except `/healthz` require authentication via API key.

### API Key Authentication

Include your API key in one of these headers:

```http
X-API-Key: your-api-key-here
```

Or use Bearer token format:

```http
Authorization: Bearer your-api-key-here
```

API keys have:
- **Role:** admin, operator, or viewer (affects scope of allowed operations)
- **Scopes:** List of resource types the key can access (e.g., `agents`, `tests`, `pops`, `audit`)
- **Expiration:** Optional; expired keys are rejected
- **Rate limit:** Per-client: 100 requests/second with burst of 200

If authentication fails, the API returns:

```json
{
  "error": "unauthorized"
}
```

HTTP Status: **401 Unauthorized**

---

## Rate Limiting

The API applies rate limiting on a per-client basis:

- **Sustained rate:** 100 requests/second
- **Burst capacity:** 200 requests
- **Rejection status:** **429 Too Many Requests**

Clients that exceed the limit receive:

```json
{
  "error": "rate limit exceeded"
}
```

Recommendation: Implement exponential backoff with jitter (1s → 2s → 4s → ...) when you receive a 429 response.

---

## Error Responses

All error responses follow a standard format:

```json
{
  "error": "descriptive error message"
}
```

### HTTP Status Codes

| Status | Condition |
|--------|-----------|
| **400 Bad Request** | Invalid input (missing required field, invalid type, validation failure) |
| **401 Unauthorized** | Missing or invalid API key |
| **403 Forbidden** | API key lacks required scopes or role |
| **404 Not Found** | Resource does not exist |
| **409 Conflict** | Resource already exists (e.g., duplicate agent ID) |
| **429 Too Many Requests** | Rate limit exceeded |
| **500 Internal Server Error** | Unexpected server error |

### Example Error Responses

**Missing required field:**
```http
HTTP/1.1 400 Bad Request
Content-Type: application/json

{
  "error": "invalid input: id is required"
}
```

**Duplicate resource:**
```http
HTTP/1.1 409 Conflict
Content-Type: application/json

{
  "error": "already exists"
}
```

**Not found:**
```http
HTTP/1.1 404 Not Found
Content-Type: application/json

{
  "error": "not found"
}
```

---

## Health Check

### GET /healthz

Liveness check endpoint. No authentication required.

**Request:**

```bash
curl -X GET http://localhost:8080/healthz
```

**Response:**

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "status": "ok"
}
```

---

## Agent Management

Agents are canary instances deployed at POPs. They register themselves on startup and send periodic heartbeats to signal liveness.

### Register Agent

**POST /api/v1/agents**

Register a new agent with the control plane. The agent reports its capabilities, version, and operational status.

**Request:**

```bash
curl -X POST http://localhost:8080/api/v1/agents \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "us-east-1-aws-01",
    "pop_name": "us-east-1-aws",
    "version": "1.0.0",
    "capabilities": ["ping", "dns", "http", "traceroute"],
    "labels": {
      "env": "production",
      "region": "us-east-1"
    }
  }'
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique agent identifier (e.g., `us-east-1-aws-01`). Must be unique across the system. |
| `pop_name` | string | Yes | Name of the POP where agent is deployed. Must exist in the system. |
| `version` | string | No | Semantic version of the agent binary (e.g., `1.0.0`). |
| `capabilities` | array | No | List of canary types agent supports. Valid values: `ping`, `dns`, `http`, `traceroute`. |
| `labels` | object | No | Arbitrary key-value pairs for organizing agents (e.g., env, region, provider). |

**Response (201 Created):**

```json
{
  "id": "us-east-1-aws-01",
  "pop_name": "us-east-1-aws",
  "version": "1.0.0",
  "status": "online",
  "capabilities": ["ping", "dns", "http", "traceroute"],
  "labels": {
    "env": "production",
    "region": "us-east-1"
  },
  "last_heartbeat": "2026-03-21T14:32:15Z",
  "registered_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:32:15Z"
}
```

**Status Codes:**

- **201 Created** — Agent successfully registered
- **400 Bad Request** — Invalid input (missing required field, referenced POP does not exist)
- **409 Conflict** — Agent with this ID already exists

---

### List All Agents

**GET /api/v1/agents**

Retrieve all registered agents in the system.

**Request:**

```bash
curl -X GET http://localhost:8080/api/v1/agents \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
[
  {
    "id": "us-east-1-aws-01",
    "pop_name": "us-east-1-aws",
    "version": "1.0.0",
    "status": "online",
    "capabilities": ["ping", "dns", "http", "traceroute"],
    "labels": {
      "env": "production",
      "region": "us-east-1"
    },
    "last_heartbeat": "2026-03-21T14:32:15Z",
    "registered_at": "2026-03-21T14:32:15Z",
    "updated_at": "2026-03-21T14:32:15Z"
  },
  {
    "id": "eu-west-1-aws-01",
    "pop_name": "eu-west-1-aws",
    "version": "1.0.0",
    "status": "online",
    "capabilities": ["ping", "dns", "http"],
    "labels": {
      "env": "production",
      "region": "eu-west-1"
    },
    "last_heartbeat": "2026-03-21T14:31:45Z",
    "registered_at": "2026-03-21T13:15:00Z",
    "updated_at": "2026-03-21T14:31:45Z"
  }
]
```

**Status Codes:**

- **200 OK** — List retrieved successfully (may be empty)
- **401 Unauthorized** — Invalid or missing API key

---

### Get Agent by ID

**GET /api/v1/agents/{id}**

Retrieve details of a specific agent.

**Request:**

```bash
curl -X GET http://localhost:8080/api/v1/agents/us-east-1-aws-01 \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
{
  "id": "us-east-1-aws-01",
  "pop_name": "us-east-1-aws",
  "version": "1.0.0",
  "status": "online",
  "capabilities": ["ping", "dns", "http", "traceroute"],
  "labels": {
    "env": "production",
    "region": "us-east-1"
  },
  "last_heartbeat": "2026-03-21T14:32:15Z",
  "registered_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:32:15Z"
}
```

**Status Codes:**

- **200 OK** — Agent found
- **404 Not Found** — Agent does not exist

---

### Delete Agent

**DELETE /api/v1/agents/{id}**

Deregister an agent. Removes all agent metadata but does not delete associated test results.

**Request:**

```bash
curl -X DELETE http://localhost:8080/api/v1/agents/us-east-1-aws-01 \
  -H "X-API-Key: your-key"
```

**Response (204 No Content):**

No response body.

**Status Codes:**

- **204 No Content** — Agent successfully deleted
- **404 Not Found** — Agent does not exist

---

### Agent Heartbeat

**POST /api/v1/agents/{id}/heartbeat**

Send a liveness signal from the agent. The control plane uses heartbeats to determine agent status (online/offline/degraded) and track active test execution.

**Request:**

```bash
curl -X POST http://localhost:8080/api/v1/agents/us-east-1-aws-01/heartbeat \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "version": "1.0.0",
    "status": "online",
    "active_tests": 12
  }'
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | string | No | Current version of agent binary. |
| `status` | string | Yes | Operational status: `online`, `offline`, or `degraded`. |
| `active_tests` | integer | No | Number of tests currently running on the agent. |

**Response (200 OK):**

```json
{
  "status": "ok"
}
```

**Status Codes:**

- **200 OK** — Heartbeat recorded
- **404 Not Found** — Agent does not exist

---

## Point of Presence (POP) Management

POPs are geographic or logical locations where agents are deployed. All agents must be assigned to a POP.

### Create POP

**POST /api/v1/pops**

Create a new point of presence.

**Request:**

```bash
curl -X POST http://localhost:8080/api/v1/pops \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "us-east-1-aws",
    "provider": "aws",
    "city": "Ashburn",
    "country": "US",
    "latitude": 39.04,
    "longitude": -77.47,
    "labels": {
      "env": "production",
      "tier": "primary"
    }
  }'
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique POP identifier (e.g., `us-east-1-aws`, `lon-equinix-02`). |
| `provider` | string | No | Infrastructure provider (e.g., `aws`, `gcp`, `azure`, `equinix`, `digitalocean`). |
| `city` | string | No | City where POP is located. |
| `country` | string | No | Country code (ISO 3166-1 alpha-2, e.g., `US`, `GB`, `DE`). |
| `latitude` | float | No | Geographic latitude for map visualization. |
| `longitude` | float | No | Geographic longitude for map visualization. |
| `labels` | object | No | Arbitrary key-value pairs (e.g., env, tier, provider). |

**Response (201 Created):**

```json
{
  "name": "us-east-1-aws",
  "provider": "aws",
  "city": "Ashburn",
  "country": "US",
  "latitude": 39.04,
  "longitude": -77.47,
  "labels": {
    "env": "production",
    "tier": "primary"
  },
  "created_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:32:15Z"
}
```

**Status Codes:**

- **201 Created** — POP successfully created
- **400 Bad Request** — Invalid input (missing name)
- **409 Conflict** — POP with this name already exists

---

### List All POPs

**GET /api/v1/pops**

Retrieve all points of presence.

**Request:**

```bash
curl -X GET http://localhost:8080/api/v1/pops \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
[
  {
    "name": "us-east-1-aws",
    "provider": "aws",
    "city": "Ashburn",
    "country": "US",
    "latitude": 39.04,
    "longitude": -77.47,
    "labels": {
      "env": "production",
      "tier": "primary"
    },
    "created_at": "2026-03-21T14:32:15Z",
    "updated_at": "2026-03-21T14:32:15Z"
  },
  {
    "name": "eu-west-1-aws",
    "provider": "aws",
    "city": "Dublin",
    "country": "IE",
    "latitude": 53.41,
    "longitude": -8.24,
    "labels": {
      "env": "production",
      "tier": "secondary"
    },
    "created_at": "2026-03-21T13:15:00Z",
    "updated_at": "2026-03-21T13:15:00Z"
  }
]
```

**Status Codes:**

- **200 OK** — List retrieved (may be empty)

---

### Get POP by Name

**GET /api/v1/pops/{name}**

Retrieve details of a specific POP.

**Request:**

```bash
curl -X GET http://localhost:8080/api/v1/pops/us-east-1-aws \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
{
  "name": "us-east-1-aws",
  "provider": "aws",
  "city": "Ashburn",
  "country": "US",
  "latitude": 39.04,
  "longitude": -77.47,
  "labels": {
    "env": "production",
    "tier": "primary"
  },
  "created_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:32:15Z"
}
```

**Status Codes:**

- **200 OK** — POP found
- **404 Not Found** — POP does not exist

---

### Delete POP

**DELETE /api/v1/pops/{name}**

Remove a point of presence. Agents must be deregistered from this POP before deletion.

**Request:**

```bash
curl -X DELETE http://localhost:8080/api/v1/pops/us-east-1-aws \
  -H "X-API-Key: your-key"
```

**Response (204 No Content):**

No response body.

**Status Codes:**

- **204 No Content** — POP successfully deleted
- **404 Not Found** — POP does not exist

---

## Test Definition Management

Test definitions describe synthetic tests (ping, DNS, HTTP, traceroute) that agents execute. Tests can be assigned to specific POPs or globally to all POPs.

### Create Test

**POST /api/v1/tests**

Create a new test definition.

**Request:**

```bash
curl -X POST http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-ping-google-1s",
    "name": "Ping to Google DNS",
    "test_type": "ping",
    "target": "8.8.8.8",
    "interval_ms": 60000,
    "timeout_ms": 5000,
    "enabled": true,
    "config": {
      "packet_count": 4,
      "packet_size": 56
    },
    "pops": ["us-east-1-aws", "eu-west-1-aws"]
  }'
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique test identifier (e.g., `test-ping-google-1s`). |
| `name` | string | Yes | Human-readable test name. |
| `test_type` | string | Yes | Type of test: `ping`, `dns`, `http`, `traceroute`. |
| `target` | string | Yes | Target for the test (IP, hostname, URL, or domain). |
| `interval_ms` | integer | Yes | Test execution interval in milliseconds. Must be > 0. |
| `timeout_ms` | integer | Yes | Test timeout in milliseconds. Must be > 0. |
| `config` | object | No | Canary-specific configuration (see Canary Configuration Examples). |
| `enabled` | boolean | No | Whether test is enabled (default: true). Disabled tests are not executed by agents. |
| `pops` | array | No | List of POP names to assign test to. Empty or omitted = assign globally (all POPs). |

**Response (201 Created):**

```json
{
  "id": "test-ping-google-1s",
  "name": "Ping to Google DNS",
  "test_type": "ping",
  "target": "8.8.8.8",
  "interval_ms": 60000,
  "timeout_ms": 5000,
  "config": {
    "packet_count": 4,
    "packet_size": 56
  },
  "enabled": true,
  "created_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:32:15Z"
}
```

**Status Codes:**

- **201 Created** — Test successfully created
- **400 Bad Request** — Invalid input (missing required field, invalid test_type, interval_ms ≤ 0, etc.)
- **409 Conflict** — Test with this ID already exists

---

### List All Tests

**GET /api/v1/tests**

Retrieve all test definitions.

**Request:**

```bash
curl -X GET http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
[
  {
    "id": "test-ping-google-1s",
    "name": "Ping to Google DNS",
    "test_type": "ping",
    "target": "8.8.8.8",
    "interval_ms": 60000,
    "timeout_ms": 5000,
    "config": {
      "packet_count": 4,
      "packet_size": 56
    },
    "enabled": true,
    "created_at": "2026-03-21T14:32:15Z",
    "updated_at": "2026-03-21T14:32:15Z"
  },
  {
    "id": "test-dns-google-30s",
    "name": "DNS Resolve to Google NS",
    "test_type": "dns",
    "target": "google.com",
    "interval_ms": 30000,
    "timeout_ms": 3000,
    "config": {
      "resolvers": ["8.8.8.8", "8.8.4.4"],
      "record_type": "A"
    },
    "enabled": true,
    "created_at": "2026-03-21T13:15:00Z",
    "updated_at": "2026-03-21T13:15:00Z"
  }
]
```

**Status Codes:**

- **200 OK** — List retrieved (may be empty)

---

### Get Test by ID

**GET /api/v1/tests/{id}**

Retrieve details of a specific test.

**Request:**

```bash
curl -X GET http://localhost:8080/api/v1/tests/test-ping-google-1s \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
{
  "id": "test-ping-google-1s",
  "name": "Ping to Google DNS",
  "test_type": "ping",
  "target": "8.8.8.8",
  "interval_ms": 60000,
  "timeout_ms": 5000,
  "config": {
    "packet_count": 4,
    "packet_size": 56
  },
  "enabled": true,
  "created_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:32:15Z"
}
```

**Status Codes:**

- **200 OK** — Test found
- **404 Not Found** — Test does not exist

---

### Update Test

**PUT /api/v1/tests/{id}**

Update an existing test definition. Fields omitted from the request are not modified (partial update).

**Request:**

```bash
curl -X PUT http://localhost:8080/api/v1/tests/test-ping-google-1s \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "interval_ms": 30000,
    "enabled": false
  }'
```

**Request Body:**

All fields are optional. Provided fields are updated; omitted fields are unchanged.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | New test name. |
| `test_type` | string | New test type (ping/dns/http/traceroute). |
| `target` | string | New test target. |
| `interval_ms` | integer | New interval in milliseconds. |
| `timeout_ms` | integer | New timeout in milliseconds. |
| `config` | object | New canary-specific configuration. |
| `enabled` | boolean | New enabled state. |

**Response (200 OK):**

```json
{
  "id": "test-ping-google-1s",
  "name": "Ping to Google DNS",
  "test_type": "ping",
  "target": "8.8.8.8",
  "interval_ms": 30000,
  "timeout_ms": 5000,
  "config": {
    "packet_count": 4,
    "packet_size": 56
  },
  "enabled": false,
  "created_at": "2026-03-21T14:32:15Z",
  "updated_at": "2026-03-21T14:35:22Z"
}
```

**Status Codes:**

- **200 OK** — Test successfully updated
- **400 Bad Request** — Invalid input
- **404 Not Found** — Test does not exist

---

### Delete Test

**DELETE /api/v1/tests/{id}**

Remove a test definition and all its assignments to POPs.

**Request:**

```bash
curl -X DELETE http://localhost:8080/api/v1/tests/test-ping-google-1s \
  -H "X-API-Key: your-key"
```

**Response (204 No Content):**

No response body.

**Status Codes:**

- **204 No Content** — Test successfully deleted
- **404 Not Found** — Test does not exist

---

### Assign Test to POPs

**POST /api/v1/tests/{id}/assign**

Assign (or reassign) a test to one or more POPs. Providing an empty POP list assigns the test globally (all POPs).

**Request:**

```bash
curl -X POST http://localhost:8080/api/v1/tests/test-ping-google-1s/assign \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "pops": ["us-east-1-aws", "eu-west-1-aws", "ap-southeast-1-aws"]
  }'
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pops` | array | Yes | List of POP names. Empty array assigns globally. |

**Response (200 OK):**

```json
{
  "status": "assigned"
}
```

**Status Codes:**

- **200 OK** — Test successfully assigned
- **400 Bad Request** — Invalid input
- **404 Not Found** — Test does not exist

---

## Config Sync

### Get Agent Configuration

**GET /api/v1/agents/{id}/config**

Retrieve the test configuration for an agent. The agent includes its POP name as a query parameter; the server returns all tests assigned to that POP (plus globally assigned tests).

**Request:**

```bash
curl -X GET "http://localhost:8080/api/v1/agents/us-east-1-aws-01/config?pop=us-east-1-aws" \
  -H "X-API-Key: your-key"
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pop` | string | Yes | Name of the POP where the agent is deployed. |

**Response (200 OK):**

```json
{
  "agent_id": "us-east-1-aws-01",
  "pop": "us-east-1-aws",
  "tests": [
    {
      "id": "test-ping-google-1s",
      "name": "Ping to Google DNS",
      "test_type": "ping",
      "target": "8.8.8.8",
      "interval_ms": 60000,
      "timeout_ms": 5000,
      "config": {
        "packet_count": 4,
        "packet_size": 56
      },
      "enabled": true,
      "created_at": "2026-03-21T14:32:15Z",
      "updated_at": "2026-03-21T14:32:15Z"
    },
    {
      "id": "test-dns-google-30s",
      "name": "DNS Resolve to Google NS",
      "test_type": "dns",
      "target": "google.com",
      "interval_ms": 30000,
      "timeout_ms": 3000,
      "config": {
        "resolvers": ["8.8.8.8", "8.8.4.4"],
        "record_type": "A"
      },
      "enabled": true,
      "created_at": "2026-03-21T13:15:00Z",
      "updated_at": "2026-03-21T13:15:00Z"
    }
  ]
}
```

**Status Codes:**

- **200 OK** — Configuration retrieved (tests array may be empty)
- **400 Bad Request** — Missing `pop` query parameter
- **404 Not Found** — Agent does not exist

---

## Audit Log

### List Audit Entries

**GET /api/v1/audit**

Retrieve audit log entries for all mutations (create, update, delete). Results can be filtered by resource type and ID.

**Request:**

```bash
curl -X GET "http://localhost:8080/api/v1/audit?limit=50&offset=0" \
  -H "X-API-Key: your-key"
```

**Query Parameters:**

| Parameter | Type | Default | Max | Description |
|-----------|------|---------|-----|-------------|
| `limit` | integer | 50 | 1000 | Number of entries to return. |
| `offset` | integer | 0 | — | Number of entries to skip (for pagination). |
| `resource` | string | — | — | Filter by resource type (e.g., `agent`, `test`, `pop`). Must be paired with `resource_id`. |
| `resource_id` | string | — | — | Filter by specific resource ID. Must be paired with `resource`. |

**Examples:**

Get last 50 audit entries:
```bash
curl -X GET "http://localhost:8080/api/v1/audit" \
  -H "X-API-Key: your-key"
```

Get audit entries for a specific test:
```bash
curl -X GET "http://localhost:8080/api/v1/audit?resource=test&resource_id=test-ping-google-1s" \
  -H "X-API-Key: your-key"
```

Get paginated results (100 entries per page, page 3):
```bash
curl -X GET "http://localhost:8080/api/v1/audit?limit=100&offset=200" \
  -H "X-API-Key: your-key"
```

**Response (200 OK):**

```json
[
  {
    "id": 1234,
    "timestamp": "2026-03-21T14:35:22Z",
    "actor_id": "user@example.com",
    "actor_role": "admin",
    "action": "create",
    "resource": "test",
    "resource_id": "test-ping-google-1s",
    "source_ip": "192.0.2.1",
    "change_diff": {
      "id": "test-ping-google-1s",
      "name": "Ping to Google DNS",
      "test_type": "ping",
      "target": "8.8.8.8"
    }
  },
  {
    "id": 1235,
    "timestamp": "2026-03-21T14:37:45Z",
    "actor_id": "user@example.com",
    "actor_role": "admin",
    "action": "update",
    "resource": "test",
    "resource_id": "test-ping-google-1s",
    "source_ip": "192.0.2.1",
    "change_diff": {
      "interval_ms": {
        "old": 60000,
        "new": 30000
      },
      "enabled": {
        "old": true,
        "new": false
      }
    }
  }
]
```

**Response Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | integer | Unique audit entry ID. |
| `timestamp` | ISO 8601 | When the mutation occurred (UTC). |
| `actor_id` | string | API key ID or user that performed the action. |
| `actor_role` | string | Role of the actor (admin, operator, viewer). |
| `action` | string | Type of mutation: `create`, `update`, `delete`. |
| `resource` | string | Resource type: `agent`, `test`, `pop`. |
| `resource_id` | string | ID of the affected resource. |
| `source_ip` | string | Client IP address. |
| `change_diff` | object | Detailed diff of what changed (full object for create, old/new for update, full object for delete). |

**Status Codes:**

- **200 OK** — Entries retrieved (may be empty)
- **400 Bad Request** — Invalid pagination parameters
- **403 Forbidden** — API key lacks audit read scope

---

## Canary Configuration Examples

Test configurations are test-type-specific JSON objects embedded in the `config` field of test definitions.

### Ping Canary Config

```json
{
  "packet_count": 4,
  "packet_size": 56,
  "dont_fragment": false
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `packet_count` | integer | 4 | Number of ICMP packets to send per test run. |
| `packet_size` | integer | 56 | Size of each packet in bytes (data payload). |
| `dont_fragment` | boolean | false | Set the Don't Fragment (DF) bit on outbound packets. |

**Full Example:**

```bash
curl -X POST http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-ping-cloudflare-60s",
    "name": "Ping to Cloudflare 1.1.1.1",
    "test_type": "ping",
    "target": "1.1.1.1",
    "interval_ms": 60000,
    "timeout_ms": 5000,
    "config": {
      "packet_count": 4,
      "packet_size": 56,
      "dont_fragment": true
    },
    "enabled": true
  }'
```

---

### DNS Canary Config

```json
{
  "resolvers": ["8.8.8.8", "8.8.4.4"],
  "record_type": "A",
  "expected_ips": ["142.250.185.46"]
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `resolvers` | array | System default | List of resolver IPs to query. Empty = use system resolvers. |
| `record_type` | string | A | DNS record type to query: A, AAAA, CNAME, MX, NS, TXT, SOA, SRV. |
| `expected_ips` | array | — | If provided, assert that resolved IPs match this list. Empty = accept any result. |

**Full Example:**

```bash
curl -X POST http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-dns-google-a-record",
    "name": "Resolve google.com A record",
    "test_type": "dns",
    "target": "google.com",
    "interval_ms": 30000,
    "timeout_ms": 3000,
    "config": {
      "resolvers": ["8.8.8.8", "8.8.4.4"],
      "record_type": "A"
    },
    "enabled": true
  }'
```

---

### HTTP Canary Config

```json
{
  "method": "GET",
  "headers": {
    "User-Agent": "NetVantage/1.0"
  },
  "body": null,
  "expected_status": 200,
  "follow_redirects": true,
  "max_redirects": 5,
  "expected_body_substring": null,
  "expected_body_regex": null
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `method` | string | GET | HTTP method: GET, HEAD, POST, PUT, DELETE, PATCH. |
| `headers` | object | {} | Custom HTTP headers. |
| `body` | string | null | Request body (for POST/PUT/PATCH). |
| `expected_status` | integer | 200 | Expected HTTP status code. Failure if actual ≠ expected. |
| `follow_redirects` | boolean | true | Follow 3xx redirects. |
| `max_redirects` | integer | 5 | Maximum number of redirects to follow. |
| `expected_body_substring` | string | null | If provided, assert response body contains this substring. |
| `expected_body_regex` | string | null | If provided, assert response body matches this regex. |

**Full Example:**

```bash
curl -X POST http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-http-example-200",
    "name": "HTTP GET to example.com",
    "test_type": "http",
    "target": "https://example.com",
    "interval_ms": 60000,
    "timeout_ms": 10000,
    "config": {
      "method": "GET",
      "headers": {
        "User-Agent": "NetVantage/1.0"
      },
      "expected_status": 200,
      "follow_redirects": true,
      "max_redirects": 5,
      "expected_body_substring": "example"
    },
    "enabled": true
  }'
```

---

### Traceroute Canary Config

```json
{
  "max_hops": 30,
  "packet_timeout_ms": 1000,
  "cycle_count": 10,
  "backend": "mtr"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_hops` | integer | 30 | Maximum number of hops to trace. |
| `packet_timeout_ms` | integer | 1000 | Per-hop timeout in milliseconds. |
| `cycle_count` | integer | 10 | Number of probe cycles (rounds) to run. Higher = more statistically meaningful per-hop stats. |
| `backend` | string | mtr | Backend tool: `mtr` or `scamper`. |

**Full Example:**

```bash
curl -X POST http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-traceroute-google-120s",
    "name": "Traceroute to 8.8.8.8",
    "test_type": "traceroute",
    "target": "8.8.8.8",
    "interval_ms": 120000,
    "timeout_ms": 30000,
    "config": {
      "max_hops": 30,
      "packet_timeout_ms": 1000,
      "cycle_count": 10,
      "backend": "mtr"
    },
    "enabled": true
  }'
```

---

## Common Workflows

### Workflow 1: Register a POP and Deploy an Agent

```bash
# 1. Create a POP
curl -X POST http://localhost:8080/api/v1/pops \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "us-west-2-aws",
    "provider": "aws",
    "city": "Los Angeles",
    "country": "US",
    "latitude": 34.05,
    "longitude": -118.24
  }'

# 2. Register an agent at that POP
curl -X POST http://localhost:8080/api/v1/agents \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "us-west-2-aws-01",
    "pop_name": "us-west-2-aws",
    "version": "1.0.0",
    "capabilities": ["ping", "dns", "http"]
  }'

# 3. Agent periodically calls config sync to get test assignments
curl -X GET "http://localhost:8080/api/v1/agents/us-west-2-aws-01/config?pop=us-west-2-aws" \
  -H "X-API-Key: your-key"

# 4. Agent sends heartbeats periodically
curl -X POST http://localhost:8080/api/v1/agents/us-west-2-aws-01/heartbeat \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "version": "1.0.0",
    "status": "online",
    "active_tests": 5
  }'
```

### Workflow 2: Create and Deploy a Test

```bash
# 1. Create a test definition
curl -X POST http://localhost:8080/api/v1/tests \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "test-http-api-health",
    "name": "API Health Check",
    "test_type": "http",
    "target": "https://api.example.com/health",
    "interval_ms": 30000,
    "timeout_ms": 5000,
    "config": {
      "method": "GET",
      "expected_status": 200,
      "expected_body_substring": "healthy"
    },
    "enabled": true
  }'

# 2. Assign the test to specific POPs
curl -X POST http://localhost:8080/api/v1/tests/test-http-api-health/assign \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "pops": ["us-east-1-aws", "us-west-2-aws", "eu-west-1-aws"]
  }'

# 3. On next config sync, agents at those POPs will receive the test
curl -X GET "http://localhost:8080/api/v1/agents/us-east-1-aws-01/config?pop=us-east-1-aws" \
  -H "X-API-Key: your-key"
```

### Workflow 3: Audit Changes to a Specific Test

```bash
# View all mutations on a test
curl -X GET "http://localhost:8080/api/v1/audit?resource=test&resource_id=test-http-api-health" \
  -H "X-API-Key: your-key"
```

---

## Authentication Examples

### Using X-API-Key Header

```bash
curl -X GET http://localhost:8080/api/v1/agents \
  -H "X-API-Key: my-secret-api-key-here"
```

### Using Bearer Token

```bash
curl -X GET http://localhost:8080/api/v1/agents \
  -H "Authorization: Bearer my-secret-api-key-here"
```

### Invalid Key

```bash
curl -X GET http://localhost:8080/api/v1/agents \
  -H "X-API-Key: invalid-key"
```

Response:

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "unauthorized"
}
```

---

## Notes for Implementers

1. **Pagination:** Use `limit` and `offset` for the audit log. Recommended: limit to 50–100 per page.

2. **Config Caching:** Agents should cache the response from `/agents/{id}/config` locally and use it for offline operation if the control plane becomes unavailable.

3. **Heartbeat Interval:** Agents should heartbeat every 30–60 seconds. Heartbeats continue even if test execution fails.

4. **Test Validation:** Before assigning a test, ensure canary-specific config is valid (e.g., ping packet_count > 0, HTTP status is a 3-digit integer).

5. **Error Handling:** Wrap retry logic around 5xx responses. For 4xx responses, fix the request and retry.

6. **Timezone Handling:** All timestamps in responses are UTC (ISO 8601 format). Convert locally as needed.

7. **Rate Limiting:** Implement exponential backoff with jitter (1s, 2s, 4s, ...) when receiving 429 responses.

---

## Related Documentation

- [Agent Deployment Guide](agent-deployment.md)
- [Test Definition Best Practices](test-best-practices.md)
- [Alerting Rules](alerting.md)
- [Architecture Overview](architecture.md)
