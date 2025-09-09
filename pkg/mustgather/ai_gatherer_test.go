package mustgather

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"strings"
	"testing"
)

func TestBasicKQLValidation(t *testing.T) {
	// Create a test AIGatherer instance
	ag := &AIGatherer{
		config: &Config{},
		ctx:    context.Background(),
		cred:   &azidentity.DefaultAzureCredential{},
	}

	tests := []struct {
		name        string
		query       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid KubePodInventory query",
			query:       "KubePodInventory | where Namespace == 'default' | project Name, PodStatus",
			expectError: false,
		},
		{
			name:        "Valid ContainerLogV2 query",
			query:       "ContainerLogV2 | where TimeGenerated > ago(1h) | take 100",
			expectError: false,
		},
		{
			name:        "Valid multi-line query",
			query:       "KubePodInventory\n| where Namespace == 'kube-system'\n| project Name, PodStatus",
			expectError: false,
		},
		{
			name:        "Empty query",
			query:       "",
			expectError: true,
			errorMsg:    "query is empty",
		},
		{
			name:        "Query with JSON formatting",
			query:       `{"kql": "KubePodInventory | take 10"}`,
			expectError: true,
			errorMsg:    "query contains JSON formatting",
		},
		{
			name:        "SQL syntax instead of KQL",
			query:       "SELECT * FROM KubePodInventory WHERE Namespace = 'default'",
			expectError: true,
			errorMsg:    "query uses SQL syntax instead of KQL",
		},
		{
			name:        "Query starting with invalid table",
			query:       "InvalidTable | where foo == 'bar'",
			expectError: true,
			errorMsg:    "query doesn't start with a recognized table name",
		},
		{
			name:        "Query with empty lines in middle",
			query:       "KubePodInventory\n\n\n| take 10",
			expectError: false, // This should actually pass since it trims properly
		},
		{
			name:        "Query with braces",
			query:       "KubePodInventory | where { someField == 'value' }",
			expectError: true,
			errorMsg:    "query contains JSON formatting",
		},
		{
			name:        "Valid KubeEvents query",
			query:       "KubeEvents | where TimeGenerated > ago(1h) | order by TimeGenerated desc",
			expectError: false,
		},
		{
			name:        "Valid InsightsMetrics query",
			query:       "InsightsMetrics | where Name == 'cpuUsageMillicores' | take 50",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ag.basicKQLValidation(tt.query)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && !containsIgnoreCase(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestExtractKQLFromResponse(t *testing.T) {
	ai := &AIQueryGenerator{}

	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name: "Valid JSON response",
			response: `{
  "kql": "KubePodInventory | where Namespace == 'default' | take 10",
  "tables_used": ["KubePodInventory"]
}`,
			expected: "KubePodInventory | where Namespace == 'default' | take 10",
		},
		{
			name:     "JSON with markdown code block",
			response: "```json\n{\n  \"kql\": \"ContainerLogV2 | take 5\",\n  \"tables_used\": [\"ContainerLogV2\"]\n}\n```",
			expected: "ContainerLogV2 | take 5",
		},
		{
			name: "JSON response with extra text",
			response: `Here's the query:
{
  "kql": "KubeEvents | order by TimeGenerated desc | take 20",
  "tables_used": ["KubeEvents"]
}`,
			expected: "KubeEvents | order by TimeGenerated desc | take 20",
		},
		{
			name:     "Plain KQL query",
			response: "KubePodInventory | where PodStatus == 'Running' | project Name, Namespace",
			expected: "KubePodInventory | where PodStatus == 'Running' | project Name, Namespace",
		},
		{
			name:     "KQL with markdown code block",
			response: "```kql\nKubePodInventory | take 100\n```",
			expected: "KubePodInventory | take 100",
		},
		{
			name: "Multi-line plain KQL",
			response: `KubePodInventory
| where Namespace == 'kube-system'
| project Name, PodStatus
| take 50`,
			expected: "KubePodInventory\n| where Namespace == 'kube-system'\n| project Name, PodStatus\n| take 50",
		},
		{
			name: "Response with comments",
			response: `// This query gets pod inventory
KubePodInventory | take 10
// End of query`,
			expected: "KubePodInventory | take 10",
		},
		{
			name:     "Empty response",
			response: "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			response: "   \n\t  \n  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ai.extractKQLFromResponse(tt.response)
			if result != tt.expected {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

// Helper function for case-insensitive string contains check
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
