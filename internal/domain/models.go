// Package domain contains shared domain models used across NetVantage services.
package domain

import (
	"encoding/json"
	"time"
)

// Agent represents a registered canary agent in the system.
type Agent struct {
	ID            string            `json:"id"`
	POPName       string            `json:"pop_name"`
	Version       string            `json:"version"`
	Status        AgentStatus       `json:"status"`
	Capabilities  []string          `json:"capabilities"`
	Labels        map[string]string `json:"labels,omitempty"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	RegisteredAt  time.Time         `json:"registered_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// AgentStatus represents the operational state of an agent.
type AgentStatus string

const (
	AgentStatusOnline   AgentStatus = "online"
	AgentStatusOffline  AgentStatus = "offline"
	AgentStatusDegraded AgentStatus = "degraded"
)

// POP represents a Point of Presence where agents are deployed.
type POP struct {
	Name      string            `json:"name"`
	Provider  string            `json:"provider,omitempty"`
	City      string            `json:"city,omitempty"`
	Country   string            `json:"country,omitempty"`
	Latitude  *float64          `json:"latitude,omitempty"`
	Longitude *float64          `json:"longitude,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// TestDefinition describes a synthetic test managed by the control plane.
type TestDefinition struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	TestType   string          `json:"test_type"`
	Target     string          `json:"target"`
	IntervalMS int64           `json:"interval_ms"`
	TimeoutMS  int64           `json:"timeout_ms"`
	Config     json.RawMessage `json:"config"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// TestAssignment maps a test definition to a POP.
// Empty POPName means the test runs on all POPs.
type TestAssignment struct {
	ID        int64     `json:"id"`
	TestID    string    `json:"test_id"`
	POPName   string    `json:"pop_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Heartbeat is sent by agents at regular intervals to signal liveness.
type Heartbeat struct {
	AgentID     string      `json:"agent_id"`
	POPName     string      `json:"pop_name"`
	Version     string      `json:"version"`
	Timestamp   time.Time   `json:"timestamp"`
	Status      AgentStatus `json:"status"`
	ActiveTests int         `json:"active_tests"`
}

// APIKey represents an authentication credential.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	KeyHash   string    `json:"-"`
	Role      string    `json:"role"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}
