package mustgather

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGetDefaultProfiles(t *testing.T) {
	profiles := GetDefaultProfiles()

	// Test that all expected profiles exist
	expectedProfiles := []string{"podLogs", "inventory", "metrics", "audit", "aks-debug"}
	for _, profile := range expectedProfiles {
		if _, exists := profiles[profile]; !exists {
			t.Errorf("expected profile %q not found", profile)
		}
	}

	// Test podLogs profile content
	expectedPodLogs := []string{"ContainerLogV2", "ContainerLog", "KubeEvents", "KubeMonAgentEvents", "Syslog"}
	if !reflect.DeepEqual(profiles["podLogs"], expectedPodLogs) {
		t.Errorf("podLogs profile mismatch.\nExpected: %v\nGot: %v", expectedPodLogs, profiles["podLogs"])
	}

	// Test inventory profile content
	expectedInventory := []string{
		"KubePodInventory", "KubeNodeInventory", "KubeServices", "KubePVInventory",
		"ContainerInventory", "ContainerImageInventory", "ContainerNodeInventory", "KubeHealth",
	}
	if !reflect.DeepEqual(profiles["inventory"], expectedInventory) {
		t.Errorf("inventory profile mismatch.\nExpected: %v\nGot: %v", expectedInventory, profiles["inventory"])
	}

	// Test metrics profile content
	expectedMetrics := []string{"InsightsMetrics", "Perf", "Heartbeat"}
	if !reflect.DeepEqual(profiles["metrics"], expectedMetrics) {
		t.Errorf("metrics profile mismatch.\nExpected: %v\nGot: %v", expectedMetrics, profiles["metrics"])
	}

	// Test audit profile content
	expectedAudit := []string{"AKSControlPlane", "AKSAudit", "AKSAuditAdmin"}
	if !reflect.DeepEqual(profiles["audit"], expectedAudit) {
		t.Errorf("audit profile mismatch.\nExpected: %v\nGot: %v", expectedAudit, profiles["audit"])
	}

	// Test that aks-debug is a union of podLogs, inventory, and metrics
	aksDebugTables := profiles["aks-debug"]
	
	// Create a set to check for all expected tables
	expectedTables := make(map[string]bool)
	for _, table := range expectedPodLogs {
		expectedTables[table] = true
	}
	for _, table := range expectedInventory {
		expectedTables[table] = true
	}
	for _, table := range expectedMetrics {
		expectedTables[table] = true
	}

	if len(aksDebugTables) != len(expectedTables) {
		t.Errorf("aks-debug should have %d unique tables, got %d", len(expectedTables), len(aksDebugTables))
	}

	// Verify all expected tables are in aks-debug
	for _, table := range aksDebugTables {
		if !expectedTables[table] {
			t.Errorf("unexpected table %q in aks-debug profile", table)
		}
	}

	// Verify no duplicates in aks-debug
	seen := make(map[string]bool)
	for _, table := range aksDebugTables {
		if seen[table] {
			t.Errorf("duplicate table %q in aks-debug profile", table)
		}
		seen[table] = true
	}
}

func TestConfigGenerateDefaultOutputName(t *testing.T) {
	tests := []struct {
		name           string
		outputFile     string
		expectDefault  bool
		expectPattern  string
	}{
		{
			name:          "empty output file",
			outputFile:    "",
			expectDefault: true,
			expectPattern: "must-gather-",
		},
		{
			name:          "existing output file",
			outputFile:    "custom-output.tar.gz",
			expectDefault: false,
			expectPattern: "custom-output.tar.gz",
		},
		{
			name:          "path with directory",
			outputFile:    "/tmp/my-gather.tar.gz",
			expectDefault: false,
			expectPattern: "/tmp/my-gather.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				OutputFile: tt.outputFile,
			}

			result := config.GenerateDefaultOutputName()

			if tt.expectDefault {
				if !strings.HasPrefix(result, tt.expectPattern) {
					t.Errorf("expected result to start with %q, got %q", tt.expectPattern, result)
				}
				if !strings.HasSuffix(result, ".tar.gz") {
					t.Errorf("expected result to end with .tar.gz, got %q", result)
				}
				// Verify the timestamp format (YYYYMMDD-HHMMSS)
				if len(result) != len("must-gather-20060102-150405.tar.gz") {
					t.Errorf("expected timestamp format in filename, got %q", result)
				}
			} else {
				if result != tt.expectPattern {
					t.Errorf("expected %q, got %q", tt.expectPattern, result)
				}
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{
			name: "valid minimal config",
			config: Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				Timespan:    "PT2H",
			},
			valid: true,
		},
		{
			name: "valid config with all fields",
			config: Config{
				WorkspaceID:         "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				Timespan:            "PT6H",
				OutputFile:          "output.tar.gz",
				TableFilter:         "table1,table2",
				Profiles:            "aks-debug,audit",
				AllTables:           false,
				StitchLogs:          true,
				StitchIncludeEvents: true,
			},
			valid: true,
		},
		{
			name: "config with Go duration",
			config: Config{
				WorkspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				Timespan:    "6h",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For now, we don't have validation methods, but we can test the structure
			if tt.config.WorkspaceID == "" && tt.valid {
				t.Errorf("valid config should have WorkspaceID")
			}
			
			if tt.config.Timespan == "" && tt.valid {
				t.Errorf("valid config should have Timespan")
			}
		})
	}
}

func TestProfileMapImmutability(t *testing.T) {
	// Test that calling GetDefaultProfiles multiple times returns consistent results
	profiles1 := GetDefaultProfiles()
	profiles2 := GetDefaultProfiles()

	// Modify one of the profiles to ensure they're independent copies
	profiles1["podLogs"] = append(profiles1["podLogs"], "ExtraTable")

	if reflect.DeepEqual(profiles1["podLogs"], profiles2["podLogs"]) {
		t.Error("profiles should be independent copies")
	}

	// Verify the original profile wasn't modified
	expectedPodLogs := []string{"ContainerLogV2", "ContainerLog", "KubeEvents", "KubeMonAgentEvents", "Syslog"}
	if !reflect.DeepEqual(profiles2["podLogs"], expectedPodLogs) {
		t.Error("original profile was modified")
	}
}

func TestConfigDefaults(t *testing.T) {
	config := &Config{}

	// Test default output generation
	output := config.GenerateDefaultOutputName()
	if !strings.Contains(output, "must-gather-") {
		t.Errorf("expected default output to contain 'must-gather-', got %q", output)
	}

	// Test that boolean defaults work as expected
	if config.StitchLogs != false {
		t.Errorf("expected StitchLogs default to be false, got %v", config.StitchLogs)
	}

	if config.StitchIncludeEvents != false {
		t.Errorf("expected StitchIncludeEvents default to be false, got %v", config.StitchIncludeEvents)
	}

	if config.AllTables != false {
		t.Errorf("expected AllTables default to be false, got %v", config.AllTables)
	}
}

func TestTimestampGeneration(t *testing.T) {
	// Test that the timestamp in the default output name is reasonable
	config := &Config{}
	output := config.GenerateDefaultOutputName()

	// Extract timestamp from filename
	// Format: must-gather-20060102-150405.tar.gz
	prefix := "must-gather-"
	suffix := ".tar.gz"
	
	if !strings.HasPrefix(output, prefix) || !strings.HasSuffix(output, suffix) {
		t.Fatalf("unexpected output format: %q", output)
	}

	timestampStr := output[len(prefix) : len(output)-len(suffix)]
	timestamp, err := time.Parse("20060102-150405", timestampStr)
	if err != nil {
		t.Fatalf("failed to parse timestamp %q: %v", timestampStr, err)
	}

	// Verify timestamp is within reasonable bounds (just check it's a valid date/time)
	// The timestamp should be from today
	now := time.Now()
	if timestamp.Year() != now.Year() || timestamp.Month() != now.Month() || timestamp.Day() != now.Day() {
		t.Errorf("timestamp date %v should be from today %v", timestamp, now)
	}
}