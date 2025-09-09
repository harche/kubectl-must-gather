package mustgather

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// Test the integration of validation logic without requiring real Azure API calls
func TestValidationWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		userQuery   string
		expectedKQL string
		expectError bool
	}{
		{
			name:        "Simple pod query",
			userQuery:   "Show me all pods",
			expectedKQL: "", // We can't predict exact Claude output
			expectError: false,
		},
		{
			name:        "Empty user query should generate something",
			userQuery:   "",
			expectedKQL: "",
			expectError: false, // AI might still generate a default query
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the basic KQL validation function directly
			ag := &AIGatherer{
				config: &Config{
					AIQuery: tt.userQuery,
				},
				ctx:  context.Background(),
				cred: &azidentity.DefaultAzureCredential{},
			}

			// Test basic validation with some sample queries
			validQueries := []string{
				"KubePodInventory | take 10",
				"ContainerLogV2 | where TimeGenerated > ago(1h)",
				"KubeEvents | order by TimeGenerated desc",
			}

			for _, query := range validQueries {
				err := ag.basicKQLValidation(query)
				if err != nil {
					t.Errorf("Valid query failed basic validation: %s, error: %v", query, err)
				}
			}

			// Test invalid queries
			invalidQueries := []string{
				"",                       // empty
				"SELECT * FROM table",    // SQL syntax
				`{"kql": "test"}`,        // JSON format
				"InvalidTable | take 10", // invalid table
			}

			for _, query := range invalidQueries {
				err := ag.basicKQLValidation(query)
				if err == nil {
					t.Errorf("Invalid query passed basic validation: %s", query)
				}
			}
		})
	}
}

// Test AI query generator prompt building
func TestAIPromptBuilding(t *testing.T) {
	ai := &AIQueryGenerator{}

	tests := []struct {
		name            string
		userQuery       string
		availableTables []string
		expectContains  []string
	}{
		{
			name:            "Basic pod query",
			userQuery:       "Show me failed pods",
			availableTables: []string{"KubePodInventory", "KubeEvents"},
			expectContains:  []string{"Show me failed pods", "KubePodInventory", "JSON"},
		},
		{
			name:            "Log query",
			userQuery:       "Show me container logs with errors",
			availableTables: []string{"ContainerLogV2", "KubeEvents"},
			expectContains:  []string{"container logs with errors", "ContainerLogV2", "docs/tables/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := ai.buildKQLPrompt(tt.userQuery, tt.availableTables)

			for _, expected := range tt.expectContains {
				if !stringContains(prompt, expected) {
					t.Errorf("Prompt should contain '%s', but didn't. Prompt: %s", expected, prompt)
				}
			}
		})
	}
}

// Test the fix prompt building
func TestFixPromptBuilding(t *testing.T) {
	ai := &AIQueryGenerator{}

	userQuery := "Show me pods"
	brokenQuery := "KubePodInventory | where InvalidColumn == 'test'"
	errorMessage := "Column 'InvalidColumn' not found"
	availableTables := []string{"KubePodInventory"}

	prompt := ai.buildFixPrompt(userQuery, brokenQuery, errorMessage, availableTables)

	expectedContains := []string{
		userQuery,
		brokenQuery,
		errorMessage,
		"fix_explanation",
		"JSON",
	}

	for _, expected := range expectedContains {
		if !stringContains(prompt, expected) {
			t.Errorf("Fix prompt should contain '%s'", expected)
		}
	}
}

// Helper function that doesn't depend on external packages
func stringContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Test error handling in validation
func TestValidationErrorHandling(t *testing.T) {
	ag := &AIGatherer{
		config: &Config{},
		ctx:    context.Background(),
		cred:   &azidentity.DefaultAzureCredential{},
	}

	// Test that different types of validation errors are handled correctly
	testCases := []struct {
		name        string
		query       string
		expectError bool
		errorType   string
	}{
		{
			name:        "Empty query",
			query:       "",
			expectError: true,
			errorType:   "empty",
		},
		{
			name:        "JSON formatted query",
			query:       `{"kql": "KubePodInventory | take 10"}`,
			expectError: true,
			errorType:   "JSON formatting",
		},
		{
			name:        "SQL syntax",
			query:       "SELECT * FROM KubePodInventory",
			expectError: true,
			errorType:   "SQL syntax",
		},
		{
			name:        "Valid query",
			query:       "KubePodInventory | take 10",
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ag.basicKQLValidation(tc.query)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error for %s but got none", tc.name)
				} else if tc.errorType != "" && !stringContains(err.Error(), tc.errorType) {
					t.Errorf("Expected error type '%s' but got: %v", tc.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tc.name, err)
				}
			}
		})
	}
}
