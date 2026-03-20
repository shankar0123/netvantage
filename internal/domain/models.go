// Package domain contains shared domain models used across NetVantage services.
package domain

import "time"

// Agent represents a registered canary agent in the system.
type Agent struct {
	ID              string            `json:"id"`
	POPName         string            `json:"pop_name"`
	POPLocation     *GeoLocation      `json:"pop_location,omitempty"`
	POPProvider     string            `json:"pop_provider,omitempty"`
	Version         string            `json:"version"`
	Status          AgentStatus       `json:"status"`
	Capabilities    []string          `json:"capabilities"`
	Labels          map[string]string `json:"labels,omitempty"`
	LastHeartbeat   time.Time         `json:"last_heartbeat"`
	RegisteredAt    time.Time         `json:"registered_at"`
}

// GeoLocation represents geographic coordinates for a POP.
type GeoLocation struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	City      string  `json:"city,omitempty"`
	Country   string  `json:"country,omitempty"`
}

// AgentStatus represents the operational state of an agent.
type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusOffline AgentStatus = "offline"
	AgentStatusDegraded AgentStatus = "degraded"
)

// Heartbeat is sent by agents at regular intervals to signal liveness.
type Heartbeat struct {
	AgentID   string    `json:"agent_id"`
	POPName   string    `json:"pop_name"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Status    AgentStatus `json:"status"`
	// ActiveTests is the count of currently running test definitions.
	ActiveTests int `json:"active_tests"`
}
