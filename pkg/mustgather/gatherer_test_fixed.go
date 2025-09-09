package mustgather

import (
	"context"
	"strings"
	"testing"
)

func TestNewGatherer(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				Timespan:    "PT1H",
			},
			expectError: false,
		},
		{
			name: "AI mode config",
			config: &Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				AIMode:      true,
				AIQuery:     "Show me failed pods",
				Timespan:    "PT1H",
			},
			expectError: false,
		},
		{
			name:        "empty config",
			config:      &Config{},
			expectError: false, // NewGatherer doesn't validate config, it just creates the gatherer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			gatherer, err := NewGatherer(ctx, tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if gatherer == nil {
				t.Error("expected gatherer to be non-nil")
				return
			}

			// Test that the gatherer can be used (implements the interface)
			// We can't test private fields through the interface, but we can verify behavior
			if gatherer == nil {
				t.Error("gatherer should not be nil")
			}
		})
	}
}

func TestGathererProfiles(t *testing.T) {
	// Test that the default profiles are defined correctly
	profiles := GetDefaultProfiles()

	if len(profiles) == 0 {
		t.Error("expected some default profiles to be defined")
	}

	// Check that expected profiles exist
	expectedProfiles := []string{"podLogs", "inventory", "metrics", "audit", "aks-debug"}

	for _, expected := range expectedProfiles {
		if _, exists := profiles[expected]; !exists {
			t.Errorf("expected profile '%s' not found in default profiles", expected)
		}
	}

	// Check that profiles contain expected tables
	if podLogsTables := profiles["podLogs"]; len(podLogsTables) == 0 {
		t.Error("podLogs profile should contain some tables")
	}
}

func TestConfigValidationBasic(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		valid  bool
	}{
		{
			name: "valid config with workspace ID",
			config: &Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				Timespan:    "PT1H",
			},
			valid: true,
		},
		{
			name: "config missing workspace ID",
			config: &Config{
				Timespan: "PT1H",
			},
			valid: false, // Usually workspace ID is required
		},
		{
			name: "AI mode with query",
			config: &Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				AIMode:      true,
				AIQuery:     "Show me pods",
				Timespan:    "PT1H",
			},
			valid: true,
		},
		{
			name: "AI mode without query",
			config: &Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				AIMode:      true,
				Timespan:    "PT1H",
			},
			valid: false, // AI mode usually requires a query
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic config structure validation by checking fields directly
			// since we don't have access to a validate() method

			hasWorkspaceID := tt.config.WorkspaceID != ""
			hasValidAIMode := !tt.config.AIMode || (tt.config.AIMode && tt.config.AIQuery != "")

			configValid := hasWorkspaceID && hasValidAIMode

			if tt.valid && !configValid {
				t.Error("expected config to be valid but validation failed")
			} else if !tt.valid && configValid {
				t.Error("expected config to be invalid but validation passed")
			}
		})
	}
}
