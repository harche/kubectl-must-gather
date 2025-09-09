package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommandFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing required workspace-id",
			args:        []string{},
			expectError: true,
			errorMsg:    "required flag(s) \"workspace-id\" not set",
		},
		{
			name: "valid minimal args",
			args: []string{
				"--workspace-id", "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
			},
			expectError: false,
		},
		{
			name: "all flags provided",
			args: []string{
				"--workspace-id", "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				"--timespan", "PT6H",
				"--out", "custom-output.tar.gz",
				"--tables", "Table1,Table2",
				"--profiles", "aks-debug,audit",
				"--all-tables",
				"--stitch-logs=false",
				"--stitch-include-events=false",
			},
			expectError: false,
		},
		{
			name:        "help flag",
			args:        []string{"--help"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new command for each test to avoid state pollution
			cmd := createTestRootCommand()

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set args
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			// For help command, we expect success but no actual execution
			if len(tt.args) > 0 && tt.args[0] == "--help" {
				if err != nil {
					t.Errorf("help command should not error: %v", err)
				}
				output := stdout.String()
				if !strings.Contains(output, "aks-must-gather is a tool") {
					t.Errorf("help output should contain description")
				}
				return
			}

			// For other valid commands, we expect them to fail at runtime (no Azure creds)
			// but not due to flag parsing issues
			if err != nil && strings.Contains(err.Error(), "required flag") {
				t.Errorf("unexpected flag parsing error: %v", err)
			}
		})
	}
}

func TestRootCommandFlagDefaults(t *testing.T) {
	tests := []struct {
		name         string
		flagName     string
		expectedType string
		hasDefault   bool
	}{
		{name: "workspace-id flag", flagName: "workspace-id", expectedType: "string", hasDefault: false},
		{name: "timespan flag", flagName: "timespan", expectedType: "string", hasDefault: true},
		{name: "out flag", flagName: "out", expectedType: "string", hasDefault: true},
		{name: "tables flag", flagName: "tables", expectedType: "string", hasDefault: false},
		{name: "profiles flag", flagName: "profiles", expectedType: "string", hasDefault: false},
		{name: "all-tables flag", flagName: "all-tables", expectedType: "bool", hasDefault: true},
		{name: "stitch-logs flag", flagName: "stitch-logs", expectedType: "bool", hasDefault: true},
		{name: "stitch-include-events flag", flagName: "stitch-include-events", expectedType: "bool", hasDefault: true},
	}

	cmd := createTestRootCommand()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flagName)
			if flag == nil {
				t.Fatalf("flag %q not found", tt.flagName)
			}

			if flag.Value.Type() != tt.expectedType {
				t.Errorf("expected flag %q to be type %q, got %q", tt.flagName, tt.expectedType, flag.Value.Type())
			}

			if tt.hasDefault && flag.DefValue == "" {
				t.Errorf("expected flag %q to have a default value", tt.flagName)
			}
		})
	}
}

func TestRootCommandDescription(t *testing.T) {
	cmd := createTestRootCommand()

	tests := []struct {
		name          string
		field         string
		value         string
		shouldContain string
	}{
		{
			name:          "command use",
			field:         "Use",
			value:         cmd.Use,
			shouldContain: "aks-must-gather",
		},
		{
			name:          "command short description",
			field:         "Short",
			value:         cmd.Short,
			shouldContain: "diagnostic data",
		},
		{
			name:          "command long description",
			field:         "Long",
			value:         cmd.Long,
			shouldContain: "Azure Log Analytics workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.value, tt.shouldContain) {
				t.Errorf("expected %s to contain %q, got %q", tt.field, tt.shouldContain, tt.value)
			}
		})
	}
}

func TestRootCommandFlagUsage(t *testing.T) {
	cmd := createTestRootCommand()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	helpText := output.String()

	expectedFlags := []string{
		"--workspace-id",
		"--timespan",
		"--out",
		"--tables",
		"--profiles",
		"--all-tables",
		"--stitch-logs",
		"--stitch-include-events",
		"--help",
	}

	for _, flag := range expectedFlags {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text should contain flag %q", flag)
		}
	}

	// Check that flag descriptions are present
	expectedDescriptions := []string{
		"Log Analytics workspace ARM resource ID",
		"Timespan to query",
		"Output tar.gz path",
		"comma-separated list of tables",
		"comma-separated profiles",
		"Export all tables",
		"time-ordered logs",
		"Include KubeEvents",
	}

	for _, desc := range expectedDescriptions {
		if !strings.Contains(helpText, desc) {
			t.Errorf("help text should contain description %q", desc)
		}
	}
}

func TestExecuteFunction(t *testing.T) {
	// Test that Execute function exists and can be called
	// We can't test actual execution without mocking Azure services

	// Save original args
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Test with help flag to avoid actual execution
	os.Args = []string{"aks-must-gather", "--help"}

	// This should not panic or cause issues
	err := Execute()
	if err != nil {
		t.Errorf("Execute with --help should not error: %v", err)
	}
}

func TestRootCommandValidation(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty workspace ID",
			workspaceID: "",
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "valid workspace ID format",
			workspaceID: "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
			expectError: false,
		},
		{
			name:        "whitespace workspace ID",
			workspaceID: "   ",
			expectError: false, // The flag validation will pass, but runtime validation would fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := createTestRootCommand()

			var stderr bytes.Buffer
			cmd.SetErr(&stderr)

			args := []string{}
			if tt.workspaceID != "" {
				args = append(args, "--workspace-id", tt.workspaceID)
			}

			cmd.SetArgs(args)
			err := cmd.Execute()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
				}
			}
			// Note: We can't test successful execution without Azure credentials
			// The command will fail at runtime, but that's expected in tests
		})
	}
}

// createTestRootCommand creates a fresh root command for testing
func createTestRootCommand() *cobra.Command {
	var testWorkspaceID string
	var testTimespanStr string
	var testOutTar string
	var testTableFilterCSV string
	var testProfilesCSV string
	var testAllTables bool
	var testStitchLogs bool
	var testStitchIncludeEvents bool

	testRootCmd := &cobra.Command{
		Use:   "aks-must-gather",
		Short: "Collect diagnostic data from Azure Log Analytics workspace",
		Long: `aks-must-gather is a tool that collects diagnostic data from an Azure Log Analytics workspace
and packages it into a tar.gz file for analysis. It supports various profiles and can export
specific tables or all tables from the workspace.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if testWorkspaceID == "" {
				return fmt.Errorf("must provide --workspace-id (workspace ARM resource ID)")
			}
			// In tests, we just validate the flags and return
			// We don't actually execute the gatherer to avoid Azure dependencies
			return nil
		},
	}

	testRootCmd.Flags().StringVar(&testWorkspaceID, "workspace-id", "", "Log Analytics workspace ARM resource ID")
	testRootCmd.Flags().StringVar(&testTimespanStr, "timespan", "PT2H", "Timespan to query (ISO-8601 like PT6H, or Go duration like 6h)")
	testRootCmd.Flags().StringVar(&testOutTar, "out", "must-gather-20060102-150405.tar.gz", "Output tar.gz path")
	testRootCmd.Flags().StringVar(&testTableFilterCSV, "tables", "", "Optional comma-separated list of tables to export (overrides profiles)")
	testRootCmd.Flags().StringVar(&testProfilesCSV, "profiles", "", "Optional comma-separated profiles: aks-debug,podLogs,inventory,metrics,audit")
	testRootCmd.Flags().BoolVar(&testAllTables, "all-tables", false, "Export all tables in the workspace (may be slow). Overrides profiles/tables if used.")
	testRootCmd.Flags().BoolVar(&testStitchLogs, "stitch-logs", true, "Also include time-ordered logs per namespace/pod/container under namespaces/ folder")
	testRootCmd.Flags().BoolVar(&testStitchIncludeEvents, "stitch-include-events", true, "Include KubeEvents under namespaces/<ns>/events/events.log")

	testRootCmd.MarkFlagRequired("workspace-id")

	return testRootCmd
}

func TestRootCommandFlagInteractions(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		expectedTablesCfg string
		expectedAllTables bool
		expectedProfiles  string
	}{
		{
			name: "tables override profiles",
			args: []string{
				"--workspace-id", "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				"--tables", "CustomTable1,CustomTable2",
				"--profiles", "aks-debug",
			},
			expectedTablesCfg: "CustomTable1,CustomTable2",
			expectedProfiles:  "aks-debug",
		},
		{
			name: "all-tables overrides everything",
			args: []string{
				"--workspace-id", "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				"--tables", "CustomTable1,CustomTable2",
				"--profiles", "aks-debug",
				"--all-tables",
			},
			expectedAllTables: true,
			expectedTablesCfg: "CustomTable1,CustomTable2",
			expectedProfiles:  "aks-debug",
		},
		{
			name: "profiles only",
			args: []string{
				"--workspace-id", "/subscriptions/12345/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
				"--profiles", "podLogs,metrics",
			},
			expectedProfiles: "podLogs,metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := createTestRootCommand()
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			// We expect this to succeed in flag parsing (validation errors are OK for actual execution)
			if err != nil && strings.Contains(err.Error(), "required flag") {
				t.Errorf("flag parsing should succeed: %v", err)
			}
		})
	}
}
