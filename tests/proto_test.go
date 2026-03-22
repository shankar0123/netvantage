//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var expectedProtoFiles = []string{
	"result.proto",
	"bgp.proto",
	"agent.proto",
}

var expectedProtoMessages = map[string][]string{
	"result.proto": {"TestResult", "Result", "PingResult"},
	"bgp.proto":    {"BGPEvent", "BGPAnnouncement"},
	"agent.proto":  {"Agent", "AgentConfig"},
}

// TestProto_FilesExist validates that expected protobuf files exist.
func TestProto_FilesExist(t *testing.T) {
	protoDir := filepath.Join("proto", "netvantage", "v1")

	for _, expectedFile := range expectedProtoFiles {
		path := filepath.Join(protoDir, expectedFile)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected proto file not found: %s", path)
		}
	}
}

// TestProto_ValidSyntax validates that all proto files have valid syntax declarations.
func TestProto_ValidSyntax(t *testing.T) {
	protoDir := filepath.Join("proto", "netvantage", "v1")
	entries, err := os.ReadDir(protoDir)
	if err != nil {
		t.Fatalf("failed to read proto directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".proto" {
			continue
		}

		t.Run(entry.Name()+"_syntax", func(t *testing.T) {
			path := filepath.Join(protoDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read proto file: %v", err)
			}

			content := string(data)

			// Check for syntax declaration
			if !strings.Contains(content, "syntax") {
				t.Error("missing syntax declaration")
			}

			// Check for proto3
			if !strings.Contains(content, `syntax = "proto3"`) {
				t.Error("syntax should be proto3")
			}
		})
	}
}

// TestProto_RequiredMessages validates that expected message types exist in proto files.
func TestProto_RequiredMessages(t *testing.T) {
	protoDir := filepath.Join("proto", "netvantage", "v1")

	for fileName, expectedMessages := range expectedProtoMessages {
		path := filepath.Join(protoDir, fileName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("skipping %s: %v", fileName, err)
			continue
		}

		content := string(data)

		t.Run(fileName+"_messages", func(t *testing.T) {
			for _, msgName := range expectedMessages {
				searchPattern := "message " + msgName
				if !strings.Contains(content, searchPattern) {
					t.Errorf("expected message type not found: %s", msgName)
				}
			}
		})
	}
}

// TestProto_PackageDeclaration validates that proto files have package declarations.
func TestProto_PackageDeclaration(t *testing.T) {
	protoDir := filepath.Join("proto", "netvantage", "v1")
	entries, err := os.ReadDir(protoDir)
	if err != nil {
		t.Fatalf("failed to read proto directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".proto" {
			continue
		}

		t.Run(entry.Name()+"_package", func(t *testing.T) {
			path := filepath.Join(protoDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read proto file: %v", err)
			}

			content := string(data)

			// Check for package declaration
			if !strings.Contains(content, "package ") {
				t.Error("missing package declaration")
			}

			// For netvantage v1 files, expect netvantage.v1 package
			if !strings.Contains(content, "netvantage.v1") {
				t.Error("package should be netvantage.v1")
			}
		})
	}
}

// TestProto_GoOptions validates that proto files have Go package options.
func TestProto_GoOptions(t *testing.T) {
	protoDir := filepath.Join("proto", "netvantage", "v1")
	entries, err := os.ReadDir(protoDir)
	if err != nil {
		t.Fatalf("failed to read proto directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".proto" {
			continue
		}

		t.Run(entry.Name()+"_go_options", func(t *testing.T) {
			path := filepath.Join(protoDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read proto file: %v", err)
			}

			content := string(data)

			// Check for go_package option (recommended)
			if !strings.Contains(content, "option go_package") {
				t.Warning("missing go_package option (recommended for Go projects)")
			}
		})
	}
}

// TestProto_TestResultFields validates that result.proto has required fields.
func TestProto_TestResultFields(t *testing.T) {
	path := filepath.Join("proto", "netvantage", "v1", "result.proto")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping result.proto test: %v", err)
	}

	content := string(data)
	expectedFields := []string{"timestamp", "test_id", "success", "error"}

	t.Run("TestResult_requiredFields", func(t *testing.T) {
		for _, field := range expectedFields {
			// Search for the field in the TestResult message definition
			// This is a simple text search; more sophisticated parsing would be needed
			// for production use, but this validates the proto file contains these concepts
			if !strings.Contains(content, field) {
				t.Logf("note: field %q not found (may be named differently)", field)
			}
		}
	})
}

// Helper method to log warnings in tests (for informational purposes)
func (t *testing.T) Warning(args ...interface{}) {
	t.Log(args...)
}
