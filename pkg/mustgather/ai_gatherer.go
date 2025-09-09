package mustgather

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	azquery "github.com/Azure/azure-sdk-for-go/sdk/monitor/azquery"
	armoperationalinsights "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"

	"kubectl-must-gather/pkg/utils"
)

// LogsClientInterface defines the interface for Azure Logs Client to enable mocking
type LogsClientInterface interface {
	QueryWorkspace(ctx context.Context, workspaceID string, body azquery.Body, options *azquery.LogsClientQueryWorkspaceOptions) (azquery.LogsClientQueryWorkspaceResponse, error)
}

// AIQueryGeneratorInterface defines the interface for AI query generation to enable mocking
type AIQueryGeneratorInterface interface {
	FixKQLQuery(ctx context.Context, userQuery, brokenQuery, errorMessage string, availableTables []string) (string, error)
}

type AIGatherer struct {
	config *Config
	ctx    context.Context
	cred   *azidentity.DefaultAzureCredential
}

func (ag *AIGatherer) Run() error {
	fmt.Printf("Running in AI mode with query: %s\n", ag.config.AIQuery)

	iso, err := utils.ISO8601Duration(ag.config.Timespan)
	if err != nil {
		return fmt.Errorf("invalid timespan: %w", err)
	}

	// Resolve workspace information
	var (
		subID         string
		rg            string
		wsName        string
		workspaceGUID string
	)

	if ag.config.WorkspaceID != "" {
		subID, rg, wsName, err = utils.ParseResourceID(ag.config.WorkspaceID)
		if err != nil {
			return fmt.Errorf("parse workspace-id: %w", err)
		}

		// Get workspace properties including customerId
		wcli, err := armoperationalinsights.NewWorkspacesClient(subID, ag.cred, nil)
		if err != nil {
			return err
		}
		w, err := wcli.Get(ag.ctx, rg, wsName, nil)
		if err != nil {
			return fmt.Errorf("get workspace: %w", err)
		}
		if w.Properties != nil && w.Properties.CustomerID != nil {
			workspaceGUID = *w.Properties.CustomerID
		}
	}

	if workspaceGUID == "" {
		return fmt.Errorf("could not determine workspace GUID from workspace; check permissions or workspace-id")
	}

	// Get available tables
	availableTables := ag.getAvailableTablesForAI()

	// Initialize AI query generator
	aiGen, err := NewAIQueryGenerator()
	if err != nil {
		return fmt.Errorf("failed to initialize AI query generator: %w", err)
	}

	// Generate KQL query
	fmt.Printf("Generating KQL query from natural language...\n")
	kqlQuery, err := aiGen.GenerateKQLQuery(ag.ctx, ag.config.AIQuery, availableTables)
	if err != nil {
		return fmt.Errorf("failed to generate KQL query: %w", err)
	}

	fmt.Printf("Generated KQL query:\n%s\n\n", kqlQuery)

	// Initialize logs client for validation
	lcli, err := azquery.NewLogsClient(ag.cred, nil)
	if err != nil {
		return fmt.Errorf("logs client: %w", err)
	}

	// Basic client-side validation first
	fmt.Printf("Validating KQL syntax...\n")
	if err := ag.basicKQLValidation(kqlQuery); err != nil {
		fmt.Printf("❌ Basic validation failed: %v\n", err)
		return fmt.Errorf("KQL basic validation failed: %w", err)
	}

	// Server-side validation with retry
	validatedQuery, err := ag.validateAndFixKQLQuery(aiGen, lcli, kqlQuery, workspaceGUID, availableTables)
	if err != nil {
		return fmt.Errorf("KQL validation failed: %w", err)
	}
	kqlQuery = validatedQuery
	fmt.Printf("✅ KQL syntax is valid\n\n")

	// Execute the AI-generated query
	fmt.Printf("Executing query...\n")
	result, err := ag.executeAIQuery(lcli, kqlQuery, workspaceGUID, iso)
	if err != nil {
		return fmt.Errorf("failed to execute AI query: %w", err)
	}

	// Create timestamped results directory in current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	timestamp := time.Now().Format("20060102-150405")
	resultsDir := filepath.Join(cwd, fmt.Sprintf("ai-results-%s", timestamp))
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results directory: %w", err)
	}
	// Don't clean up - keep results for user inspection

	fmt.Printf("Writing results to directory: %s\n", resultsDir)

	// Write query results to files (similar to tar structure but in results dir)
	err = ag.writeResultsToFiles(resultsDir, kqlQuery, result, workspaceGUID, subID, rg, wsName, iso)
	if err != nil {
		return fmt.Errorf("failed to write results to files: %w", err)
	}

	// Stage 2: Analyze results with Claude
	fmt.Printf("Analyzing results with AI...\n")
	analysis, err := aiGen.AnalyzeResults(ag.ctx, ag.config.AIQuery, kqlQuery, resultsDir)
	if err != nil {
		fmt.Printf("Warning: Failed to analyze results with AI: %v\n", err)
		fmt.Printf("Falling back to raw results display...\n")
		ag.displayAIResults(result)
	} else if strings.TrimSpace(analysis) == "" {
		fmt.Printf("Warning: AI analysis returned empty result\n")
		fmt.Printf("Falling back to raw results display...\n")
		ag.displayAIResults(result)
	} else {
		// Display the AI analysis
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("AI ANALYSIS")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Println(analysis)
		fmt.Println(strings.Repeat("=", 80))
	}

	fmt.Printf("\nQuery results saved to: %s\n", resultsDir)
	fmt.Printf("You can inspect the raw data, KQL query, and metadata in this directory.\n")

	return nil
}

func (ag *AIGatherer) getAvailableTablesForAI() []string {
	// Return commonly available tables for AKS/Kubernetes workloads
	return []string{
		"ContainerLogV2", "ContainerLog", "KubeEvents", "KubePodInventory",
		"KubeNodeInventory", "KubeServices", "KubePVInventory", "ContainerInventory",
		"ContainerImageInventory", "ContainerNodeInventory", "KubeHealth",
		"InsightsMetrics", "Perf", "Heartbeat", "AKSControlPlane", "AKSAudit",
		"AKSAuditAdmin", "KubeMonAgentEvents", "Syslog",
	}
}

func (ag *AIGatherer) executeAIQuery(lcli *azquery.LogsClient, kqlQuery, workspaceGUID, iso string) (*azquery.LogsClientQueryWorkspaceResponse, error) {
	// Parse the ISO8601 duration to get time range
	duration, err := utils.ParseISO8601ToDuration(iso)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timespan: %w", err)
	}

	t1 := time.Now().UTC()
	t0 := t1.Add(-duration)

	body := azquery.Body{
		Query:    &kqlQuery,
		Timespan: to.Ptr(azquery.NewTimeInterval(t0, t1)),
	}

	options := &azquery.LogsClientQueryWorkspaceOptions{
		Options: &azquery.LogsQueryOptions{Wait: to.Ptr(180)},
	}

	result, err := lcli.QueryWorkspace(ag.ctx, workspaceGUID, body, options)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (ag *AIGatherer) writeResultsToFiles(tempDir, kqlQuery string, result *azquery.LogsClientQueryWorkspaceResponse, workspaceGUID, subID, rg, wsName, iso string) error {
	// Write metadata similar to regular gatherer
	meta := map[string]any{
		"generatedAt":   time.Now().UTC().Format(time.RFC3339Nano),
		"workspaceGUID": workspaceGUID,
		"workspaceID":   ag.config.WorkspaceID,
		"timespan":      iso,
		"aiMode":        true,
		"userQuery":     ag.config.AIQuery,
		"kqlQuery":      kqlQuery,
	}

	// Create metadata directory
	metaDir := filepath.Join(tempDir, "metadata")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return err
	}

	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), metaBytes, 0644); err != nil {
		return err
	}

	// Write Azure metadata if available
	if subID != "" && rg != "" && wsName != "" {
		mp := map[string]string{"subscriptionId": subID, "resourceGroup": rg, "workspaceName": wsName}
		mpb, _ := json.MarshalIndent(mp, "", "  ")
		if err := os.WriteFile(filepath.Join(metaDir, "azure.json"), mpb, 0644); err != nil {
			return err
		}
	}

	// Write query results
	if result.Tables != nil && len(result.Tables) > 0 {
		resultsDir := filepath.Join(tempDir, "ai-query-results")
		if err := os.MkdirAll(resultsDir, 0755); err != nil {
			return err
		}

		// Write the KQL query
		if err := os.WriteFile(filepath.Join(resultsDir, "query.kql"), []byte(kqlQuery), 0644); err != nil {
			return err
		}

		// Write each table result
		for i, table := range result.Tables {
			tableFile := filepath.Join(resultsDir, fmt.Sprintf("table_%d.json", i))
			tableBytes, _ := json.MarshalIndent(table, "", "  ")
			if err := os.WriteFile(tableFile, tableBytes, 0644); err != nil {
				return err
			}
		}

		// Write summary
		summary := map[string]any{
			"tableCount": len(result.Tables),
			"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
		}
		summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
		if err := os.WriteFile(filepath.Join(resultsDir, "summary.json"), summaryBytes, 0644); err != nil {
			return err
		}
	}

	return nil
}

func (ag *AIGatherer) displayAIResults(result *azquery.LogsClientQueryWorkspaceResponse) {
	if result.Tables == nil || len(result.Tables) == 0 {
		fmt.Println("No results found.")
		return
	}

	for i, table := range result.Tables {
		if i > 0 {
			fmt.Println("\n" + strings.Repeat("=", 80))
		}

		fmt.Printf("Results (Table %d):\n", i+1)
		fmt.Println(strings.Repeat("-", 40))

		if table.Columns == nil || table.Rows == nil {
			fmt.Println("No data in this table.")
			continue
		}

		// Print column headers
		var headers []string
		for _, col := range table.Columns {
			if col.Name != nil {
				headers = append(headers, *col.Name)
			}
		}
		fmt.Println(strings.Join(headers, " | "))
		fmt.Println(strings.Repeat("-", len(strings.Join(headers, " | "))))

		// Print rows (limit to first 50 rows for readability)
		maxRows := 50
		rowCount := len(table.Rows)
		if rowCount > maxRows {
			fmt.Printf("Showing first %d of %d rows:\n", maxRows, rowCount)
		}

		for i, row := range table.Rows {
			if i >= maxRows {
				break
			}

			var rowData []string
			for _, cell := range row {
				if cell == nil {
					rowData = append(rowData, "<null>")
				} else {
					// Convert cell to string, truncating if too long
					cellStr := fmt.Sprintf("%v", cell)
					if len(cellStr) > 100 {
						cellStr = cellStr[:97] + "..."
					}
					rowData = append(rowData, cellStr)
				}
			}
			fmt.Println(strings.Join(rowData, " | "))
		}

		if rowCount > maxRows {
			fmt.Printf("\n... and %d more rows\n", rowCount-maxRows)
		}
	}
}

// validateAndFixKQLQuery validates KQL syntax and attempts to fix errors using AI
func (ag *AIGatherer) validateAndFixKQLQuery(aiGen *AIQueryGenerator, lcli *azquery.LogsClient, kqlQuery, workspaceGUID string, availableTables []string) (string, error) {
	maxRetries := 2
	currentQuery := kqlQuery

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "Retrying validation (attempt %d/%d)...\n", attempt+1, maxRetries+1)
		}

		err := ag.validateKQLQuery(lcli, currentQuery, workspaceGUID)
		if err == nil {
			return currentQuery, nil
		}

		// If this is not the last attempt, try to fix the query with AI
		if attempt < maxRetries {
			fmt.Fprintf(os.Stderr, "❌ Validation failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "🔧 Asking Claude to fix the KQL query...\n")

			fixedQuery, fixErr := aiGen.FixKQLQuery(ag.ctx, ag.config.AIQuery, currentQuery, err.Error(), availableTables)
			if fixErr != nil {
				fmt.Fprintf(os.Stderr, "⚠️ Failed to fix query with AI: %v\n", fixErr)
				continue
			}

			fmt.Fprintf(os.Stderr, "🔄 Fixed KQL query:\n%s\n\n", fixedQuery)
			currentQuery = fixedQuery
		} else {
			return "", fmt.Errorf("failed to validate KQL after %d attempts: %v", maxRetries+1, err)
		}
	}

	return currentQuery, nil
}

// basicKQLValidation performs simple client-side checks
func (ag *AIGatherer) basicKQLValidation(kqlQuery string) error {
	query := strings.TrimSpace(kqlQuery)

	// Check for empty query
	if query == "" {
		return fmt.Errorf("query is empty")
	}

	// Check for obvious JSON formatting issues
	if strings.Contains(query, "{") || strings.Contains(query, "}") {
		return fmt.Errorf("query contains JSON formatting (should be plain KQL)")
	}

	// Check for SQL syntax instead of KQL
	if strings.Contains(strings.ToUpper(query), "SELECT ") {
		return fmt.Errorf("query uses SQL syntax instead of KQL")
	}

	// Check that it starts with a table name or valid KQL command
	lines := strings.Split(query, "\n")

	// Find the first non-comment, non-empty line
	var firstLine string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "//") {
			firstLine = line
			break
		}
	}

	if firstLine == "" {
		return fmt.Errorf("no valid KQL found after removing comments")
	}

	// Check for valid KQL constructs (table names or KQL commands)
	validTables := []string{
		"KubePodInventory", "KubeNodeInventory", "KubeEvents", "ContainerLogV2",
		"ContainerLog", "InsightsMetrics", "Perf", "Heartbeat", "KubeServices",
		"ContainerInventory", "AKSControlPlane", "AKSAudit", "Syslog",
	}

	validKQLCommands := []string{
		"let ", "with ", "union", "print", "datatable",
	}

	startsWithValidConstruct := false

	// Check if it starts with a table name
	for _, table := range validTables {
		if strings.HasPrefix(firstLine, table) {
			startsWithValidConstruct = true
			break
		}
	}

	// Check if it starts with a valid KQL command
	if !startsWithValidConstruct {
		for _, cmd := range validKQLCommands {
			if strings.HasPrefix(firstLine, cmd) {
				startsWithValidConstruct = true
				break
			}
		}
	}

	if !startsWithValidConstruct {
		return fmt.Errorf("query doesn't start with a recognized table name or KQL command")
	}

	return nil
}

// validateKQLQuery validates the syntax of a KQL query by running it with limit 0
func (ag *AIGatherer) validateKQLQuery(lcli *azquery.LogsClient, kqlQuery, workspaceGUID string) error {
	// Create a validation query by appending "| limit 0" to check syntax without returning data
	validationQuery := strings.TrimSpace(kqlQuery)
	if !strings.HasSuffix(strings.ToLower(validationQuery), "| limit 0") {
		validationQuery += " | limit 0"
	}

	// Use a minimal time range for validation (just last minute)
	t1 := time.Now().UTC()
	t0 := t1.Add(-time.Minute)

	body := azquery.Body{
		Query:    &validationQuery,
		Timespan: to.Ptr(azquery.NewTimeInterval(t0, t1)),
	}

	options := &azquery.LogsClientQueryWorkspaceOptions{
		Options: &azquery.LogsQueryOptions{Wait: to.Ptr(30)}, // Short timeout for validation
	}

	_, err := lcli.QueryWorkspace(ag.ctx, workspaceGUID, body, options)
	if err != nil {
		// Parse Azure error to provide more helpful validation messages
		errStr := err.Error()
		if strings.Contains(errStr, "SyntaxError") {
			return fmt.Errorf("KQL syntax error: %v", err)
		}
		if strings.Contains(errStr, "SemanticError") {
			return fmt.Errorf("KQL semantic error (invalid table/column names): %v", err)
		}
		if strings.Contains(errStr, "PartialError") {
			// Partial errors might be acceptable (e.g., some tables don't exist)
			fmt.Fprintf(os.Stderr, "⚠️ KQL validation warning (partial error): %v\n", err)
			return nil
		}
		return fmt.Errorf("KQL validation error: %v", err)
	}

	return nil
}

// validateKQLQueryWithClient is a testable version that accepts a client interface
func (ag *AIGatherer) validateKQLQueryWithClient(lcli LogsClientInterface, kqlQuery, workspaceGUID string) error {
	// Create a validation query by appending "| limit 0" to check syntax without returning data
	validationQuery := strings.TrimSpace(kqlQuery)
	if !strings.HasSuffix(strings.ToLower(validationQuery), "| limit 0") {
		validationQuery += " | limit 0"
	}

	// Use a minimal time range for validation (just last minute)
	t1 := time.Now().UTC()
	t0 := t1.Add(-time.Minute)

	body := azquery.Body{
		Query:    &validationQuery,
		Timespan: to.Ptr(azquery.NewTimeInterval(t0, t1)),
	}

	options := &azquery.LogsClientQueryWorkspaceOptions{
		Options: &azquery.LogsQueryOptions{Wait: to.Ptr(30)}, // Short timeout for validation
	}

	_, err := lcli.QueryWorkspace(ag.ctx, workspaceGUID, body, options)
	if err != nil {
		// Parse Azure error to provide more helpful validation messages
		errStr := err.Error()
		if strings.Contains(errStr, "SyntaxError") {
			return fmt.Errorf("KQL syntax error: %v", err)
		}
		if strings.Contains(errStr, "SemanticError") {
			return fmt.Errorf("KQL semantic error (invalid table/column names): %v", err)
		}
		if strings.Contains(errStr, "PartialError") {
			// Partial errors might be acceptable (e.g., some tables don't exist)
			fmt.Fprintf(os.Stderr, "⚠️ KQL validation warning (partial error): %v\n", err)
			return nil
		}
		return fmt.Errorf("KQL validation error: %v", err)
	}

	return nil
}

// validateAndFixKQLQueryWithClient is a testable version that accepts client and AI interfaces
func (ag *AIGatherer) validateAndFixKQLQueryWithClient(aiGen AIQueryGeneratorInterface, lcli LogsClientInterface, kqlQuery, workspaceGUID string, availableTables []string) (string, error) {
	maxRetries := 2
	currentQuery := kqlQuery

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "Retrying validation (attempt %d/%d)...\n", attempt+1, maxRetries+1)
		}

		err := ag.validateKQLQueryWithClient(lcli, currentQuery, workspaceGUID)
		if err == nil {
			return currentQuery, nil
		}

		// If this is not the last attempt, try to fix the query with AI
		if attempt < maxRetries {
			fmt.Fprintf(os.Stderr, "❌ Validation failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "🔧 Asking Claude to fix the KQL query...\n")

			fixedQuery, fixErr := aiGen.FixKQLQuery(ag.ctx, ag.config.AIQuery, currentQuery, err.Error(), availableTables)
			if fixErr != nil {
				fmt.Fprintf(os.Stderr, "⚠️ Failed to fix query with AI: %v\n", fixErr)
				continue
			}

			fmt.Fprintf(os.Stderr, "🔄 Fixed KQL query:\n%s\n\n", fixedQuery)
			currentQuery = fixedQuery
		} else {
			return "", fmt.Errorf("failed to validate KQL after %d attempts: %v", maxRetries+1, err)
		}
	}

	return currentQuery, nil
}
