package testhelpers

import (
	"fmt"
	"testing"
	"time"
)

func TestCreateAndReadTarEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []TarEntry
	}{
		{
			name: "single file",
			entries: []TarEntry{
				{
					Path:    "test.txt",
					Content: "hello world",
					Mode:    0644,
					ModTime: time.Now(),
				},
			},
		},
		{
			name: "multiple files with directory",
			entries: []TarEntry{
				{
					Path:    "dir/",
					IsDir:   true,
					Mode:    0755,
					ModTime: time.Now(),
				},
				{
					Path:    "dir/file1.txt",
					Content: "content1",
					Mode:    0644,
					ModTime: time.Now(),
				},
				{
					Path:    "dir/file2.txt",
					Content: "content2",
					Mode:    0644,
					ModTime: time.Now(),
				},
			},
		},
		{
			name: "empty file",
			entries: []TarEntry{
				{
					Path:    "empty.txt",
					Content: "",
					Mode:    0644,
					ModTime: time.Now(),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create tar
			buf, err := CreateTestTar(tt.entries)
			if err != nil {
				t.Fatalf("CreateTestTar failed: %v", err)
			}

			// Read tar back
			readEntries, err := ReadTarEntries(buf.Bytes())
			if err != nil {
				t.Fatalf("ReadTarEntries failed: %v", err)
			}

			if len(readEntries) != len(tt.entries) {
				t.Errorf("expected %d entries, got %d", len(tt.entries), len(readEntries))
			}

			// Verify each entry
			for i, expected := range tt.entries {
				if i >= len(readEntries) {
					t.Errorf("missing entry %d", i)
					continue
				}

				actual := readEntries[i]
				if actual.Path != expected.Path {
					t.Errorf("entry %d path mismatch: expected %q, got %q", i, expected.Path, actual.Path)
				}

				if actual.IsDir != expected.IsDir {
					t.Errorf("entry %d IsDir mismatch: expected %v, got %v", i, expected.IsDir, actual.IsDir)
				}

				if !expected.IsDir && actual.Content != expected.Content {
					t.Errorf("entry %d content mismatch: expected %q, got %q", i, expected.Content, actual.Content)
				}
			}
		})
	}
}

func TestAssertTarContains(t *testing.T) {
	entries := []TarEntry{
		{Path: "file1.txt", Content: "content1", Mode: 0644, ModTime: time.Now()},
		{Path: "file2.txt", Content: "content2", Mode: 0644, ModTime: time.Now()},
	}

	buf, err := CreateTestTar(entries)
	if err != nil {
		t.Fatalf("CreateTestTar failed: %v", err)
	}

	tests := []struct {
		name            string
		path            string
		expectedContent string
		shouldPass      bool
	}{
		{
			name:            "existing file with correct content",
			path:            "file1.txt",
			expectedContent: "content1",
			shouldPass:      true,
		},
		{
			name:            "existing file with wrong content",
			path:            "file1.txt",
			expectedContent: "wrong content",
			shouldPass:      false,
		},
		{
			name:            "non-existing file",
			path:            "nonexistent.txt",
			expectedContent: "any content",
			shouldPass:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockT := &testing.T{}
			AssertTarContains(mockT, buf.Bytes(), tt.path, tt.expectedContent)

			failed := mockT.Failed()
			if tt.shouldPass && failed {
				t.Errorf("AssertTarContains should have passed but failed")
			}
			if !tt.shouldPass && !failed {
				t.Errorf("AssertTarContains should have failed but passed")
			}
		})
	}
}

func TestValidateWorkspaceIDFormat(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		expected    bool
	}{
		{
			name:        "valid workspace ID",
			workspaceID: "/subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/test-rg/providers/Microsoft.OperationalInsights/workspaces/test-workspace",
			expected:    true,
		},
		{
			name:        "missing subscriptions",
			workspaceID: "/resourceGroups/test-rg/providers/Microsoft.OperationalInsights/workspaces/test-workspace",
			expected:    false,
		},
		{
			name:        "missing workspaces",
			workspaceID: "/subscriptions/12345/resourceGroups/test-rg/providers/Microsoft.OperationalInsights",
			expected:    false,
		},
		{
			name:        "too short",
			workspaceID: "/subscriptions/12345",
			expected:    false,
		},
		{
			name:        "empty string",
			workspaceID: "",
			expected:    false,
		},
		{
			name:        "case insensitive",
			workspaceID: "/SUBSCRIPTIONS/12345/RESOURCEGROUPS/test-rg/PROVIDERS/MICROSOFT.OPERATIONALINSIGHTS/WORKSPACES/test-workspace",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateWorkspaceIDFormat(tt.workspaceID)
			if result != tt.expected {
				t.Errorf("expected %v, got %v for workspace ID: %s", tt.expected, result, tt.workspaceID)
			}
		})
	}
}

func TestMockWorkspaceID(t *testing.T) {
	workspaceID := MockWorkspaceID()
	
	if workspaceID == "" {
		t.Error("MockWorkspaceID should not return empty string")
	}
	
	if !ValidateWorkspaceIDFormat(workspaceID) {
		t.Errorf("MockWorkspaceID should return valid format, got: %s", workspaceID)
	}
}

func TestCreateMockTableData(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		rowCount  int
	}{
		{name: "ContainerLogV2", tableName: "ContainerLogV2", rowCount: 5},
		{name: "KubeEvents", tableName: "KubeEvents", rowCount: 3},
		{name: "InsightsMetrics", tableName: "InsightsMetrics", rowCount: 10},
		{name: "Generic table", tableName: "CustomTable", rowCount: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := CreateMockTableData(tt.tableName, tt.rowCount)
			
			if len(data) != tt.rowCount {
				t.Errorf("expected %d rows, got %d", tt.rowCount, len(data))
			}
			
			for i, row := range data {
				// Check common fields
				if row["TableName"] != tt.tableName {
					t.Errorf("row %d: expected TableName %q, got %q", i, tt.tableName, row["TableName"])
				}
				
				if row["RowIndex"] != i {
					t.Errorf("row %d: expected RowIndex %d, got %v", i, i, row["RowIndex"])
				}
				
				// Check table-specific fields
				switch tt.tableName {
				case "ContainerLogV2":
					if row["PodNamespace"] == nil {
						t.Errorf("row %d: ContainerLogV2 should have PodNamespace", i)
					}
					if row["LogMessage"] == nil {
						t.Errorf("row %d: ContainerLogV2 should have LogMessage", i)
					}
				case "KubeEvents":
					if row["Namespace"] == nil {
						t.Errorf("row %d: KubeEvents should have Namespace", i)
					}
					if row["Reason"] == nil {
						t.Errorf("row %d: KubeEvents should have Reason", i)
					}
				case "InsightsMetrics":
					if row["MetricName"] == nil {
						t.Errorf("row %d: InsightsMetrics should have MetricName", i)
					}
					if row["MetricValue"] == nil {
						t.Errorf("row %d: InsightsMetrics should have MetricValue", i)
					}
				}
			}
		})
	}
}

func TestAssertStringSliceEqual(t *testing.T) {
	tests := []struct {
		name       string
		expected   []string
		actual     []string
		shouldPass bool
	}{
		{
			name:       "equal slices",
			expected:   []string{"a", "b", "c"},
			actual:     []string{"a", "b", "c"},
			shouldPass: true,
		},
		{
			name:       "different length",
			expected:   []string{"a", "b"},
			actual:     []string{"a", "b", "c"},
			shouldPass: false,
		},
		{
			name:       "different content",
			expected:   []string{"a", "b", "c"},
			actual:     []string{"a", "x", "c"},
			shouldPass: false,
		},
		{
			name:       "empty slices",
			expected:   []string{},
			actual:     []string{},
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockT := &testing.T{}
			AssertStringSliceEqual(mockT, tt.expected, tt.actual)

			failed := mockT.Failed()
			if tt.shouldPass && failed {
				t.Errorf("AssertStringSliceEqual should have passed but failed")
			}
			if !tt.shouldPass && !failed {
				t.Errorf("AssertStringSliceEqual should have failed but passed")
			}
		})
	}
}

func TestNewMockTimeRange(t *testing.T) {
	tests := []struct {
		name     string
		hoursAgo int
	}{
		{name: "2 hours ago", hoursAgo: 2},
		{name: "6 hours ago", hoursAgo: 6},
		{name: "24 hours ago", hoursAgo: 24},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeRange := NewMockTimeRange(tt.hoursAgo)
			
			duration := timeRange.End.Sub(timeRange.Start)
			expectedDuration := time.Duration(tt.hoursAgo) * time.Hour
			
			if duration != expectedDuration {
				t.Errorf("expected duration %v, got %v", expectedDuration, duration)
			}
			
			// Check that end time is roughly now
			now := time.Now().UTC()
			if timeRange.End.Sub(now) > time.Minute {
				t.Errorf("end time should be close to now, got difference: %v", timeRange.End.Sub(now))
			}
			
			// Test ISO8601 formatting
			iso := timeRange.FormatISO8601()
			expectedISO := fmt.Sprintf("PT%dH0M0S", tt.hoursAgo)
			if iso != expectedISO {
				t.Errorf("expected ISO8601 %q, got %q", expectedISO, iso)
			}
		})
	}
}

func TestTestConfig(t *testing.T) {
	tests := []struct {
		name         string
		builderFunc  func(*TestConfig) *TestConfig
		expectedWS   string
		expectedSpan string
	}{
		{
			name: "default config",
			builderFunc: func(c *TestConfig) *TestConfig {
				return c
			},
			expectedWS:   MockWorkspaceID(),
			expectedSpan: "PT2H",
		},
		{
			name: "custom workspace",
			builderFunc: func(c *TestConfig) *TestConfig {
				return c.WithWorkspaceID("custom-workspace")
			},
			expectedWS:   "custom-workspace",
			expectedSpan: "PT2H",
		},
		{
			name: "custom timespan",
			builderFunc: func(c *TestConfig) *TestConfig {
				return c.WithTimespan("PT6H")
			},
			expectedWS:   MockWorkspaceID(),
			expectedSpan: "PT6H",
		},
		{
			name: "chained configuration",
			builderFunc: func(c *TestConfig) *TestConfig {
				return c.WithWorkspaceID("chained-ws").WithTimespan("PT12H").WithProfiles("aks-debug")
			},
			expectedWS:   "chained-ws",
			expectedSpan: "PT12H",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.builderFunc(NewTestConfig())
			
			if config.WorkspaceID != tt.expectedWS {
				t.Errorf("expected WorkspaceID %q, got %q", tt.expectedWS, config.WorkspaceID)
			}
			
			if config.Timespan != tt.expectedSpan {
				t.Errorf("expected Timespan %q, got %q", tt.expectedSpan, config.Timespan)
			}
		})
	}
}