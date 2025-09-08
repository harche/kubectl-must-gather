package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"kubectl-must-gather/pkg/mustgather"
)

var (
	workspaceID         string
	timespanStr         string
	outTar              string
	tableFilterCSV      string
	profilesCSV         string
	allTables           bool
	stitchLogs          bool
	stitchIncludeEvents bool
)

var rootCmd = &cobra.Command{
	Use:   "aks-must-gather",
	Short: "Collect diagnostic data from Azure Log Analytics workspace",
	Long: `aks-must-gather is a tool that collects diagnostic data from an Azure Log Analytics workspace
and packages it into a tar.gz file for analysis. It supports various profiles and can export
specific tables or all tables from the workspace.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if workspaceID == "" {
			return fmt.Errorf("must provide --workspace-id (workspace ARM resource ID)")
		}

		config := &mustgather.Config{
			WorkspaceID:         workspaceID,
			Timespan:            timespanStr,
			OutputFile:          outTar,
			TableFilter:         tableFilterCSV,
			Profiles:            profilesCSV,
			AllTables:           allTables,
			StitchLogs:          stitchLogs,
			StitchIncludeEvents: stitchIncludeEvents,
		}

		ctx := context.Background()
		gatherer, err := mustgather.NewGatherer(ctx, config)
		if err != nil {
			return err
		}

		return gatherer.Run()
	},
}

func init() {
	rootCmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Log Analytics workspace ARM resource ID")
	rootCmd.Flags().StringVar(&timespanStr, "timespan", "PT2H", "Timespan to query (ISO-8601 like PT6H, or Go duration like 6h)")
	rootCmd.Flags().StringVar(&outTar, "out", fmt.Sprintf("must-gather-%s.tar.gz", time.Now().Format("20060102-150405")), "Output tar.gz path")
	rootCmd.Flags().StringVar(&tableFilterCSV, "tables", "", "Optional comma-separated list of tables to export (overrides profiles)")
	rootCmd.Flags().StringVar(&profilesCSV, "profiles", "", "Optional comma-separated profiles: aks-debug,podLogs,inventory,metrics,audit")
	rootCmd.Flags().BoolVar(&allTables, "all-tables", false, "Export all tables in the workspace (may be slow). Overrides profiles/tables if used.")
	rootCmd.Flags().BoolVar(&stitchLogs, "stitch-logs", true, "Also include time-ordered logs per namespace/pod/container under namespaces/ folder")
	rootCmd.Flags().BoolVar(&stitchIncludeEvents, "stitch-include-events", true, "Include KubeEvents under namespaces/<ns>/events/events.log")

	rootCmd.MarkFlagRequired("workspace-id")
}

func Execute() error {
	return rootCmd.Execute()
}