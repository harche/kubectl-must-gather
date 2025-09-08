package testhelpers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// TarEntry represents a file in a tar archive for testing
type TarEntry struct {
	Path     string
	Content  string
	Mode     int64
	IsDir    bool
	ModTime  time.Time
}

// CreateTestTar creates a tar.gz archive with the given entries for testing
func CreateTestTar(entries []TarEntry) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for _, entry := range entries {
		var hdr *tar.Header
		if entry.IsDir {
			hdr = &tar.Header{
				Name:     entry.Path,
				Mode:     entry.Mode,
				Typeflag: tar.TypeDir,
				ModTime:  entry.ModTime,
			}
		} else {
			hdr = &tar.Header{
				Name:    entry.Path,
				Mode:    entry.Mode,
				Size:    int64(len(entry.Content)),
				ModTime: entry.ModTime,
			}
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}

		if !entry.IsDir {
			if _, err := tw.Write([]byte(entry.Content)); err != nil {
				return nil, err
			}
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

// ReadTarEntries reads all entries from a tar.gz archive for testing
func ReadTarEntries(data []byte) ([]TarEntry, error) {
	buf := bytes.NewReader(data)
	gzr, err := gzip.NewReader(buf)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var entries []TarEntry

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		entry := TarEntry{
			Path:    hdr.Name,
			Mode:    hdr.Mode,
			IsDir:   hdr.Typeflag == tar.TypeDir,
			ModTime: hdr.ModTime,
		}

		if !entry.IsDir {
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			entry.Content = string(content)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// AssertTarContains checks if a tar archive contains the expected entry
func AssertTarContains(t *testing.T, data []byte, expectedPath string, expectedContent string) {
	entries, err := ReadTarEntries(data)
	if err != nil {
		t.Fatalf("failed to read tar entries: %v", err)
	}

	for _, entry := range entries {
		if entry.Path == expectedPath {
			if entry.Content != expectedContent {
				t.Errorf("content mismatch for %s.\nExpected: %q\nGot: %q", expectedPath, expectedContent, entry.Content)
			}
			return
		}
	}

	t.Errorf("expected entry %q not found in tar archive", expectedPath)
}

// AssertTarHasFile checks if a tar archive contains a file at the expected path
func AssertTarHasFile(t *testing.T, data []byte, expectedPath string) {
	entries, err := ReadTarEntries(data)
	if err != nil {
		t.Fatalf("failed to read tar entries: %v", err)
	}

	for _, entry := range entries {
		if entry.Path == expectedPath && !entry.IsDir {
			return
		}
	}

	t.Errorf("expected file %q not found in tar archive", expectedPath)
}

// MockWorkspaceID returns a valid-looking workspace ID for testing
func MockWorkspaceID() string {
	return "/subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/test-rg/providers/Microsoft.OperationalInsights/workspaces/test-workspace"
}

// ValidateWorkspaceIDFormat checks if a workspace ID has the correct format
func ValidateWorkspaceIDFormat(workspaceID string) bool {
	parts := strings.Split(workspaceID, "/")
	if len(parts) < 9 {
		return false
	}

	expectedParts := map[string]bool{
		"subscriptions":                           false,
		"resourcegroups":                          false,
		"providers":                               false,
		"microsoft.operationalinsights":           false,
		"workspaces":                              false,
	}

	for _, part := range parts {
		lowerPart := strings.ToLower(part)
		if _, exists := expectedParts[lowerPart]; exists {
			expectedParts[lowerPart] = true
		}
	}

	for _, found := range expectedParts {
		if !found {
			return false
		}
	}

	return true
}

// CreateMockTableData creates mock table data for testing
func CreateMockTableData(tableName string, rowCount int) []map[string]interface{} {
	rows := make([]map[string]interface{}, rowCount)
	
	for i := 0; i < rowCount; i++ {
		row := map[string]interface{}{
			"TimeGenerated": time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			"TableName":     tableName,
			"RowIndex":      i,
			"Message":       "Mock log message " + string(rune('A'+i%26)),
		}

		// Add table-specific columns based on table name
		switch tableName {
		case "ContainerLogV2":
			row["PodNamespace"] = "test-namespace"
			row["PodName"] = "test-pod-" + string(rune('A'+i%3))
			row["ContainerName"] = "test-container"
			row["LogSource"] = "stdout"
			row["LogMessage"] = "Container log message " + string(rune('A'+i%26))
		case "KubeEvents":
			row["Namespace"] = "test-namespace"
			row["Name"] = "test-event-" + string(rune('A'+i%3))
			row["Reason"] = "Created"
			row["Message"] = "Event message " + string(rune('A'+i%26))
		case "InsightsMetrics":
			row["MetricName"] = "cpu_usage_percent"
			row["MetricValue"] = float64(i % 100)
			row["Tags"] = map[string]string{"host": "test-host"}
		}

		rows[i] = row
	}

	return rows
}

// AssertStringSliceEqual compares two string slices for equality
func AssertStringSliceEqual(t *testing.T, expected, actual []string, msgAndArgs ...interface{}) {
	if len(expected) != len(actual) {
		t.Errorf("slice length mismatch: expected %d, got %d. Expected: %v, Actual: %v", 
			len(expected), len(actual), expected, actual)
		return
	}

	for i, exp := range expected {
		if actual[i] != exp {
			t.Errorf("slice element %d mismatch: expected %q, got %q", i, exp, actual[i])
		}
	}
}

// AssertStringSliceContains checks if a slice contains all expected strings
func AssertStringSliceContains(t *testing.T, slice []string, expected []string, msgAndArgs ...interface{}) {
	sliceMap := make(map[string]bool)
	for _, s := range slice {
		sliceMap[s] = true
	}

	for _, exp := range expected {
		if !sliceMap[exp] {
			t.Errorf("expected slice to contain %q, but it was not found. Slice: %v", exp, slice)
		}
	}
}

// MockTimeRange creates a time range for testing
type MockTimeRange struct {
	Start time.Time
	End   time.Time
}

// NewMockTimeRange creates a new mock time range
func NewMockTimeRange(hoursAgo int) MockTimeRange {
	end := time.Now().UTC()
	start := end.Add(-time.Duration(hoursAgo) * time.Hour)
	return MockTimeRange{Start: start, End: end}
}

// FormatISO8601 formats the duration as ISO8601
func (r MockTimeRange) FormatISO8601() string {
	duration := r.End.Sub(r.Start)
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	return fmt.Sprintf("PT%dH%dM0S", hours, minutes)
}

// TestConfig creates a test configuration with sensible defaults
type TestConfig struct {
	WorkspaceID         string
	Timespan            string
	OutputFile          string
	TableFilter         string
	Profiles            string
	AllTables           bool
	StitchLogs          bool
	StitchIncludeEvents bool
}

// NewTestConfig creates a new test configuration
func NewTestConfig() *TestConfig {
	return &TestConfig{
		WorkspaceID:         MockWorkspaceID(),
		Timespan:            "PT2H",
		OutputFile:          "",
		StitchLogs:          true,
		StitchIncludeEvents: true,
	}
}

// WithWorkspaceID sets the workspace ID
func (c *TestConfig) WithWorkspaceID(id string) *TestConfig {
	c.WorkspaceID = id
	return c
}

// WithTimespan sets the timespan
func (c *TestConfig) WithTimespan(span string) *TestConfig {
	c.Timespan = span
	return c
}

// WithProfiles sets the profiles
func (c *TestConfig) WithProfiles(profiles string) *TestConfig {
	c.Profiles = profiles
	return c
}

// WithTables sets the table filter
func (c *TestConfig) WithTables(tables string) *TestConfig {
	c.TableFilter = tables
	return c
}

// WithAllTables sets the all tables flag
func (c *TestConfig) WithAllTables(all bool) *TestConfig {
	c.AllTables = all
	return c
}