package utils

import (
	"testing"
	"time"
)

func TestParseResourceID(t *testing.T) {
	tests := []struct {
		name          string
		resourceID    string
		expectedSub   string
		expectedRG    string
		expectedWS    string
		expectedError bool
	}{
		{
			name:          "valid resource ID",
			resourceID:    "/subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/myRG/providers/Microsoft.OperationalInsights/workspaces/myWorkspace",
			expectedSub:   "12345678-1234-1234-1234-123456789012",
			expectedRG:    "myRG",
			expectedWS:    "myWorkspace",
			expectedError: false,
		},
		{
			name:          "empty resource ID",
			resourceID:    "",
			expectedError: true,
		},
		{
			name:          "invalid resource ID - too short",
			resourceID:    "/subscriptions/12345/resourceGroups",
			expectedError: true,
		},
		{
			name:          "missing subscription",
			resourceID:    "/resourceGroups/myRG/providers/Microsoft.OperationalInsights/workspaces/myWorkspace",
			expectedError: true,
		},
		{
			name:          "missing resource group",
			resourceID:    "/subscriptions/12345678-1234-1234-1234-123456789012/providers/Microsoft.OperationalInsights/workspaces/myWorkspace",
			expectedError: true,
		},
		{
			name:          "missing workspace",
			resourceID:    "/subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/myRG/providers/Microsoft.OperationalInsights",
			expectedError: true,
		},
		{
			name:          "whitespace resource ID",
			resourceID:    "  /subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/myRG/providers/Microsoft.OperationalInsights/workspaces/myWorkspace  ",
			expectedSub:   "12345678-1234-1234-1234-123456789012",
			expectedRG:    "myRG",
			expectedWS:    "myWorkspace",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, rg, workspace, err := ParseResourceID(tt.resourceID)

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if sub != tt.expectedSub {
				t.Errorf("expected subscription %q, got %q", tt.expectedSub, sub)
			}
			if rg != tt.expectedRG {
				t.Errorf("expected resource group %q, got %q", tt.expectedRG, rg)
			}
			if workspace != tt.expectedWS {
				t.Errorf("expected workspace %q, got %q", tt.expectedWS, workspace)
			}
		})
	}
}

func TestISO8601Duration(t *testing.T) {
	tests := []struct {
		name        string
		duration    string
		expected    string
		expectError bool
	}{
		{
			name:     "already ISO8601",
			duration: "PT2H",
			expected: "PT2H",
		},
		{
			name:     "Go duration - hours",
			duration: "2h",
			expected: "PT2H0M0S",
		},
		{
			name:     "Go duration - minutes",
			duration: "30m",
			expected: "PT0H30M0S",
		},
		{
			name:     "Go duration - seconds",
			duration: "45s",
			expected: "PT0H0M45S",
		},
		{
			name:     "Go duration - complex",
			duration: "2h30m45s",
			expected: "PT2H30M45S",
		},
		{
			name:        "empty duration",
			duration:    "",
			expectError: true,
		},
		{
			name:        "invalid duration",
			duration:    "invalid",
			expectError: true,
		},
		{
			name:     "whitespace duration",
			duration: "  2h  ",
			expected: "PT2H0M0S",
		},
		{
			name:     "already ISO8601 with lowercase",
			duration: "pt6h",
			expected: "pt6h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ISO8601Duration(tt.duration)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSafeFileName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal string",
			input:    "normal",
			expected: "normal",
		},
		{
			name:     "with dots",
			input:    "file.name.txt",
			expected: "file_name_txt",
		},
		{
			name:     "with slashes",
			input:    "path/to/file",
			expected: "path_to_file",
		},
		{
			name:     "with special characters",
			input:    "file@#$%^&*()name",
			expected: "file_________name",
		},
		{
			name:     "with spaces",
			input:    "file name with spaces",
			expected: "file_name_with_spaces",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "unnamed",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "unnamed",
		},
		{
			name:     "alphanumeric with underscore and dash",
			input:    "valid_file-name123",
			expected: "valid_file-name123",
		},
		{
			name:     "with whitespace around",
			input:    "  filename  ",
			expected: "filename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SafeFileName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestParseISO8601ToDuration(t *testing.T) {
	tests := []struct {
		name        string
		iso         string
		expected    time.Duration
		expectError bool
	}{
		{
			name:     "hours only",
			iso:      "PT6H",
			expected: 6 * time.Hour,
		},
		{
			name:     "minutes only",
			iso:      "PT30M",
			expected: 30 * time.Minute,
		},
		{
			name:     "seconds only",
			iso:      "PT45S",
			expected: 45 * time.Second,
		},
		{
			name:     "hours and minutes",
			iso:      "PT2H30M",
			expected: 2*time.Hour + 30*time.Minute,
		},
		{
			name:     "hours, minutes and seconds",
			iso:      "PT1H30M45S",
			expected: 1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name:     "lowercase",
			iso:      "pt2h30m",
			expected: 2*time.Hour + 30*time.Minute,
		},
		{
			name:        "missing P prefix",
			iso:         "T2H",
			expectError: true,
		},
		{
			name:        "missing T",
			iso:         "P2H",
			expectError: true,
		},
		{
			name:        "empty string",
			iso:         "",
			expectError: true,
		},
		{
			name:     "with whitespace",
			iso:      "  PT2H  ",
			expected: 2 * time.Hour,
		},
		{
			name:     "zero duration",
			iso:      "PT0H0M0S",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseISO8601ToDuration(tt.iso)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestParseTimeRFC3339(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Time
		isZero   bool
	}{
		{
			name:     "valid RFC3339",
			input:    "2023-01-01T12:00:00Z",
			expected: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:     "valid RFC3339Nano",
			input:    "2023-01-01T12:00:00.123456789Z",
			expected: time.Date(2023, 1, 1, 12, 0, 0, 123456789, time.UTC),
		},
		{
			name:   "empty string",
			input:  "",
			isZero: true,
		},
		{
			name:   "invalid format",
			input:  "invalid-time",
			isZero: true,
		},
		{
			name:   "whitespace only",
			input:  "   ",
			isZero: true,
		},
		{
			name:     "with whitespace",
			input:    "  2023-01-01T12:00:00Z  ",
			expected: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseTimeRFC3339(tt.input)

			if tt.isZero {
				if !result.IsZero() {
					t.Errorf("expected zero time, got %v", result)
				}
				return
			}

			if !result.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
