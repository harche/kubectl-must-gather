package mustgather

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type AIQueryGenerator struct{}

func NewAIQueryGenerator() (*AIQueryGenerator, error) {
	// Check if claude command is available
	if _, err := exec.LookPath("claude"); err != nil {
		return nil, fmt.Errorf("'claude' command not found in PATH. Please install Claude CLI: %w", err)
	}

	return &AIQueryGenerator{}, nil
}

func (ai *AIQueryGenerator) GenerateKQLQuery(ctx context.Context, userQuery string, availableTables []string) (string, error) {
	prompt := ai.buildKQLPrompt(userQuery, availableTables)

	// Stage 1: Generate KQL from natural language
	cmd := exec.CommandContext(ctx, "claude", prompt)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute claude command for KQL generation: %w", err)
	}

	kqlQuery := strings.TrimSpace(string(output))
	kqlQuery = ai.extractKQLFromResponse(kqlQuery)

	return kqlQuery, nil
}

func (ai *AIQueryGenerator) AnalyzeResults(ctx context.Context, userQuery, kqlQuery, tempDir string) (string, error) {
	prompt := ai.buildAnalysisPrompt(userQuery, kqlQuery, tempDir)

	// Stage 2: Analyze results and provide human-readable summary
	cmd := exec.CommandContext(ctx, "claude", prompt)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute claude command for result analysis: %w", err)
	}

	analysis := strings.TrimSpace(string(output))
	return analysis, nil
}

func (ai *AIQueryGenerator) FixKQLQuery(ctx context.Context, userQuery, brokenQuery, errorMessage string, availableTables []string) (string, error) {
	prompt := ai.buildFixPrompt(userQuery, brokenQuery, errorMessage, availableTables)

	// Stage 3: Fix broken KQL query
	cmd := exec.CommandContext(ctx, "claude", prompt)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute claude command for KQL fix: %w", err)
	}

	fixedResponse := strings.TrimSpace(string(output))
	fixedQuery := ai.extractKQLFromResponse(fixedResponse)

	return fixedQuery, nil
}

func (ai *AIQueryGenerator) buildKQLPrompt(userQuery string, availableTables []string) string {
	tablesList := strings.Join(availableTables, ", ")

	// Get table relevance suggestions based on query keywords
	relevantTables := ai.suggestRelevantTables(userQuery, availableTables)
	var relevanceGuidance string
	if len(relevantTables) > 0 {
		relevanceGuidance = fmt.Sprintf("\n\nRECOMMENDED TABLES for this query: %s\nThese tables are likely to contain the most relevant data for your specific query.", strings.Join(relevantTables, ", "))
	}

	return fmt.Sprintf(`You are a KQL (Kusto Query Language) expert helping to generate queries for Azure Log Analytics workspace data related to Kubernetes/AKS clusters.

User Query: "%s"

Available Tables: %s%s

IMPORTANT: Before generating the KQL query, you MUST look at the table schema documentation in @docs/tables/ for any tables you plan to use. Each table has a corresponding .md file with the exact column names and types.

Steps to follow:
1. First, identify which tables you need for the query
2. Look at the corresponding .md files in @docs/tables/ to get the exact schema and column names
3. Generate the KQL query using only the actual column names from the documentation

Generate a KQL query that answers the user's question. The query should:
1. Use appropriate tables from the available list
2. Include proper time filtering (use TimeGenerated column)
3. Be efficient and focused on the user's specific request
4. Include ONLY columns that actually exist in the table schemas (verify from docs/tables/)
5. Use proper KQL syntax and functions
6. Limit results appropriately (use 'take' or 'top' when needed)

CRITICAL: You must respond with a valid JSON object that conforms to this schema:

{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "kql": {
      "type": "string",
      "description": "The executable KQL query"
    },
    "tables_used": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "List of tables referenced in the query"
    }
  },
  "required": ["kql", "tables_used"],
  "additionalProperties": false
}

Example response:
{
  "kql": "KubePodInventory | where Namespace == 'default' | project TimeGenerated, Name, PodStatus",
  "tables_used": ["KubePodInventory"]
}

Return ONLY valid JSON. No other text before or after.`, userQuery, tablesList, relevanceGuidance)
}

func (ai *AIQueryGenerator) buildAnalysisPrompt(userQuery, kqlQuery, tempDir string) string {
	return fmt.Sprintf(`You are a Kubernetes troubleshooting expert. Analyze the query results in directory %s to answer this question: "%s"

The KQL query that was executed:
%s

Please:
1. Read the JSON files in the directory (especially ai-query-results/table_*.json)
2. Analyze the data to understand what's happening with the Kubernetes resources
3. Provide a clear, actionable summary of your findings
4. Focus on the specific question asked
5. Include relevant timestamps, pod names, error messages, and restart counts
6. Suggest next steps or solutions if applicable

Structure your response with clear headings and bullet points for easy reading.`, tempDir, userQuery, kqlQuery)
}

func (ai *AIQueryGenerator) buildFixPrompt(userQuery, brokenQuery, errorMessage string, availableTables []string) string {
	tablesList := strings.Join(availableTables, ", ")

	return fmt.Sprintf(`You are a KQL expert helping to fix a broken query. The query failed validation with the following error:

ERROR: %s

Original User Query: "%s"
Broken KQL Query:
%s

Available Tables: %s

Please fix the KQL query by:
1. Looking at the table schema documentation in @docs/tables/ for any tables you plan to use
2. Correcting syntax errors, invalid column names, or table references
3. Ensuring the query still answers the original user question
4. Using only columns that exist in the table schemas

CRITICAL: You must respond with a valid JSON object that conforms to this schema:

{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "kql": {
      "type": "string",
      "description": "The fixed executable KQL query"
    },
    "tables_used": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "List of tables referenced in the query"
    },
    "fix_explanation": {
      "type": "string",
      "description": "Brief explanation of what was fixed"
    }
  },
  "required": ["kql", "tables_used", "fix_explanation"],
  "additionalProperties": false
}

Return ONLY valid JSON. No other text before or after.`, errorMessage, userQuery, brokenQuery, tablesList)
}

type KQLResponse struct {
	KQL            string   `json:"kql"`
	TablesUsed     []string `json:"tables_used"`
	FixExplanation string   `json:"fix_explanation,omitempty"`
}

func (ai *AIQueryGenerator) extractKQLFromResponse(response string) string {
	response = strings.TrimSpace(response)

	// Remove markdown code block formatting
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```kql")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Try to parse as JSON first
	var kqlResp KQLResponse
	if err := json.Unmarshal([]byte(response), &kqlResp); err == nil {
		return strings.TrimSpace(kqlResp.KQL)
	}

	// Look for JSON block in the response
	lines := strings.Split(response, "\n")
	var jsonLines []string
	var inJSON bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, line)
		}
		if strings.HasSuffix(line, "}") && inJSON {
			break
		}
	}

	if len(jsonLines) > 0 {
		jsonStr := strings.Join(jsonLines, "\n")
		var kqlResp KQLResponse
		if err := json.Unmarshal([]byte(jsonStr), &kqlResp); err == nil {
			return strings.TrimSpace(kqlResp.KQL)
		}
	}

	// Fallback: treat the whole response as KQL and clean it up
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "}") && !strings.Contains(line, "json") {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// suggestRelevantTables analyzes the user query and suggests relevant tables based on keywords
func (ai *AIQueryGenerator) suggestRelevantTables(userQuery string, availableTables []string) []string {
	query := strings.ToLower(userQuery)
	var suggestions []string

	// Create a map for quick lookup of available tables
	tableMap := make(map[string]bool)
	for _, table := range availableTables {
		tableMap[table] = true
	}

	// Define keyword patterns and their associated tables
	patterns := map[string][]string{
		// Pod failure and troubleshooting keywords -> ContainerLogV2 + KubeEvents + KubePodInventory
		"troubleshooting": {"failed", "fail", "error", "crash", "restart", "restarting", "down", "broken", "issue", "problem", "why", "what happened", "not working", "stuck"},

		// Log-specific keywords -> ContainerLogV2
		"logs": {"log", "logs", "message", "output", "stdout", "stderr", "console"},

		// Event-specific keywords -> KubeEvents
		"events": {"event", "events", "warning", "backoff", "killing", "created", "started", "scheduled"},

		// Inventory and status keywords -> KubePodInventory, KubeNodeInventory
		"inventory": {"inventory", "status", "state", "running", "pending", "list", "show me", "get", "find"},

		// Metrics and performance keywords -> InsightsMetrics, Perf
		"metrics": {"metric", "performance", "cpu", "memory", "usage", "resource", "utilization"},

		// Node-specific keywords -> KubeNodeInventory
		"nodes": {"node", "nodes", "worker", "master", "cluster"},
	}

	// Table recommendations based on query patterns
	recommendations := map[string][]string{
		"troubleshooting": {"ContainerLogV2", "KubeEvents", "KubePodInventory"},
		"logs":            {"ContainerLogV2"},
		"events":          {"KubeEvents"},
		"inventory":       {"KubePodInventory", "KubeNodeInventory"},
		"metrics":         {"InsightsMetrics", "Perf"},
		"nodes":           {"KubeNodeInventory"},
	}

	// Score tables based on keyword matches
	tableScores := make(map[string]int)

	for category, keywords := range patterns {
		for _, keyword := range keywords {
			if strings.Contains(query, keyword) {
				// Add recommended tables for this category
				for _, table := range recommendations[category] {
					if tableMap[table] {
						tableScores[table]++
					}
				}
			}
		}
	}

	// Special handling for specific scenarios
	if strings.Contains(query, "pod") {
		if tableMap["KubePodInventory"] {
			tableScores["KubePodInventory"] += 2 // Higher weight for pod queries
		}
		// If it's a pod troubleshooting query, strongly recommend container logs
		if containsAny(query, []string{"failed", "error", "crash", "restart", "why", "problem"}) {
			if tableMap["ContainerLogV2"] {
				tableScores["ContainerLogV2"] += 3
			}
		}
	}

	if strings.Contains(query, "container") {
		if tableMap["ContainerLogV2"] {
			tableScores["ContainerLogV2"] += 2
		}
	}

	// Convert scores to suggestions (tables with score > 0)
	for table, score := range tableScores {
		if score > 0 {
			suggestions = append(suggestions, table)
		}
	}

	return suggestions
}

// containsAny checks if the string contains any of the given substrings
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
