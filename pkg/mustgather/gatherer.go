package mustgather

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	azquery "github.com/Azure/azure-sdk-for-go/sdk/monitor/azquery"
	armoperationalinsights "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"

	"kubectl-must-gather/pkg/utils"
)

type ckey struct{ ns, pod, container string }

type GathererInterface interface {
	Run() error
}

type Gatherer struct {
	config *Config
	ctx    context.Context
	cred   *azidentity.DefaultAzureCredential
}

func NewGatherer(ctx context.Context, config *Config) (GathererInterface, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to init credential: %w", err)
	}

	if config.AIMode {
		return &AIGatherer{
			config: config,
			ctx:    ctx,
			cred:   cred,
		}, nil
	}

	return &Gatherer{
		config: config,
		ctx:    ctx,
		cred:   cred,
	}, nil
}

func (g *Gatherer) Run() error {
	iso, err := utils.ISO8601Duration(g.config.Timespan)
	if err != nil {
		return fmt.Errorf("invalid timespan: %w", err)
	}

	// Resolve GUID and list of tables
	var (
		subID         string
		rg            string
		wsName        string
		tables        []string
		workspaceGUID string
	)

	if g.config.WorkspaceID != "" {
		subID, rg, wsName, err = utils.ParseResourceID(g.config.WorkspaceID)
		if err != nil {
			return fmt.Errorf("parse workspace-id: %w", err)
		}

		// Get workspace properties including customerId
		wcli, err := armoperationalinsights.NewWorkspacesClient(subID, g.cred, nil)
		if err != nil {
			return err
		}
		w, err := wcli.Get(g.ctx, rg, wsName, nil)
		if err != nil {
			return fmt.Errorf("get workspace: %w", err)
		}
		if w.Properties != nil && w.Properties.CustomerID != nil {
			workspaceGUID = *w.Properties.CustomerID
		}

		if g.config.AllTables {
			// List tables via management plane only when explicitly requested
			tcli, err := armoperationalinsights.NewTablesClient(subID, g.cred, nil)
			if err != nil {
				return err
			}
			pager := tcli.NewListByWorkspacePager(rg, wsName, nil)
			for pager.More() {
				page, err := pager.NextPage(g.ctx)
				if err != nil {
					return fmt.Errorf("list tables: %w", err)
				}
				for _, t := range page.Value {
					if t.Name != nil {
						tables = append(tables, *t.Name)
					}
				}
			}
		}
	}

	if workspaceGUID == "" {
		return fmt.Errorf("could not determine workspace GUID from workspace; check permissions or workspace-id")
	}

	tables = g.resolveTables(tables)

	// Prepare tar.gz writer
	outFile := g.config.GenerateDefaultOutputName()
	outF, err := os.Create(outFile)
	if err != nil {
		return fmt.Errorf("create out: %w", err)
	}
	defer outF.Close()
	gz := gzip.NewWriter(outF)
	defer gz.Close()
	tarw := tar.NewWriter(gz)
	defer tarw.Close()

	// Write metadata
	meta := map[string]any{
		"generatedAt":   time.Now().UTC().Format(time.RFC3339Nano),
		"workspaceGUID": workspaceGUID,
		"workspaceID":   g.config.WorkspaceID,
		"timespan":      iso,
		"tablesCount":   len(tables),
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	_ = utils.WriteFileToTar(tarw, "metadata/workspace.json", metaBytes)

	// If we have management-plane info, persist it
	if subID != "" && rg != "" && wsName != "" {
		mp := map[string]string{"subscriptionId": subID, "resourceGroup": rg, "workspaceName": wsName}
		mpb, _ := json.MarshalIndent(mp, "", "  ")
		_ = utils.WriteFileToTar(tarw, "metadata/azure.json", mpb)
	}

	// Initialize logs client
	lcli, err := azquery.NewLogsClient(g.cred, nil)
	if err != nil {
		return fmt.Errorf("logs client: %w", err)
	}

	// Helper: fetch schema for a table if we can (management plane only)
	var tcli *armoperationalinsights.TablesClient
	if subID != "" {
		if tcli, err = armoperationalinsights.NewTablesClient(subID, g.cred, nil); err != nil {
			return err
		}
	}

	err = g.exportTables(tarw, lcli, tcli, tables, workspaceGUID, subID, rg, wsName, iso)
	if err != nil {
		return err
	}

	// Index file
	index := map[string]any{"tables": tables}
	idxb, _ := json.MarshalIndent(index, "", "  ")
	_ = utils.WriteFileToTar(tarw, "index.json", idxb)

	fmt.Fprintf(os.Stderr, "Wrote %s\n", outFile)
	return nil
}

func (g *Gatherer) resolveTables(tables []string) []string {
	if g.config.TableFilter != "" {
		// override tables with filter list
		parts := strings.Split(g.config.TableFilter, ",")
		tables = tables[:0]
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				tables = append(tables, p)
			}
		}
	}

	profileMap := GetDefaultProfiles()

	// If profiles provided, union their table lists (overridden by --tables if set earlier)
	if len(tables) == 0 && g.config.Profiles != "" && !g.config.AllTables {
		profs := strings.Split(g.config.Profiles, ",")
		seen := map[string]struct{}{}
		for _, p := range profs {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if lst, ok := profileMap[p]; ok {
				for _, t := range lst {
					if _, ok := seen[t]; !ok {
						tables = append(tables, t)
						seen[t] = struct{}{}
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "warning: unknown profile '%s'\n", p)
			}
		}
	}

	// If still empty, default to union of podLogs+inventory+metrics (same as aks-debug)
	if len(tables) == 0 && !g.config.AllTables {
		def := append([]string{}, profileMap["aks-debug"]...)
		// dedupe
		seen := map[string]struct{}{}
		for _, t := range def {
			if _, ok := seen[t]; !ok {
				tables = append(tables, t)
				seen[t] = struct{}{}
			}
		}
	}

	return tables
}

func (g *Gatherer) exportTables(tarw *tar.Writer, lcli *azquery.LogsClient, tcli *armoperationalinsights.TablesClient, tables []string, workspaceGUID, subID, rg, wsName, iso string) error {
	// Accumulators for stitched logs
	stitchedLogs := map[ckey]*strings.Builder{}
	stitchedEvents := map[string]*strings.Builder{}

	for _, table := range tables {
		fmt.Fprintf(os.Stderr, "Exporting %s...\n", table)
		safe := utils.SafeFileName(table)

		// Schema
		if tcli != nil {
			if resp, err := tcli.Get(g.ctx, rg, wsName, table, nil); err == nil {
				b, _ := json.MarshalIndent(resp.Table, "", "  ")
				_ = utils.WriteFileToTar(tarw, filepath.Join("tables", safe, "schema.json"), b)
			}
		}

		err := g.exportTableData(tarw, lcli, table, safe, workspaceGUID, iso, stitchedLogs, stitchedEvents)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error exporting table %s: %v\n", table, err)
			continue
		}
	}

	// Write stitched logs into the tar
	if g.config.StitchLogs {
		for k, b := range stitchedLogs {
			if b.Len() == 0 {
				continue
			}
			ns := utils.SafeFileName(k.ns)
			pod := utils.SafeFileName(k.pod)
			cn := utils.SafeFileName(k.container)
			path := filepath.Join("namespaces", ns, "pods", pod, cn+".log")
			_ = utils.WriteFileToTar(tarw, path, []byte(b.String()))
		}
		if g.config.StitchIncludeEvents {
			for ns, b := range stitchedEvents {
				if b.Len() == 0 {
					continue
				}
				path := filepath.Join("namespaces", utils.SafeFileName(ns), "events", "events.log")
				_ = utils.WriteFileToTar(tarw, path, []byte(b.String()))
			}
		}
	}

	return nil
}

func (g *Gatherer) exportTableData(tarw *tar.Writer, lcli *azquery.LogsClient, table, safe, workspaceGUID, iso string, stitchedLogs map[ckey]*strings.Builder, stitchedEvents map[string]*strings.Builder) error {
	// Data: chunk queries by hour to avoid limits.
	// Determine time window now-iso to since.
	since := time.Now().UTC()
	// Parse iso timespan to duration for chunking
	dur := time.Duration(0)
	if d2, err := utils.ParseISO8601ToDuration(iso); err == nil {
		dur = d2
	} else if d3, err := time.ParseDuration(g.config.Timespan); err == nil {
		dur = d3
	}
	start := since.Add(-dur)
	if dur == 0 {
		start = since.Add(-2 * time.Hour)
	}

	// chunk = 1h if dur>2h else 15m
	chunk := time.Hour
	if dur <= 2*time.Hour {
		chunk = 15 * time.Minute
	}

	// helpers
	getBuf := func(k ckey) *strings.Builder {
		if b, ok := stitchedLogs[k]; ok {
			return b
		}
		b := &strings.Builder{}
		stitchedLogs[k] = b
		return b
	}
	getEvt := func(ns string) *strings.Builder {
		if b, ok := stitchedEvents[ns]; ok {
			return b
		}
		b := &strings.Builder{}
		stitchedEvents[ns] = b
		return b
	}

	rowsTotal := 0
	chunkIndex := 0

	for t0 := start; t0.Before(since); t0 = t0.Add(chunk) {
		t1 := t0.Add(chunk)
		if t1.After(since) {
			t1 = since
		}
		// Build time-bounded query via timespan
		q := table
		body := azquery.Body{Query: &q, Timespan: to.Ptr(azquery.NewTimeInterval(t0.UTC(), t1.UTC()))}
		// Increase server-side wait timeout
		res, err := lcli.QueryWorkspace(g.ctx, workspaceGUID, body, &azquery.LogsClientQueryWorkspaceOptions{Options: &azquery.LogsQueryOptions{Wait: to.Ptr(180)}})
		if err != nil {
			// Note: If the table doesn't exist, ignore.
			fmt.Fprintf(os.Stderr, "  warn: query chunk failed for %s: %v\n", table, err)
			continue
		}
		if res.Error != nil {
			fmt.Fprintf(os.Stderr, "  warn: partial/error for %s: %v\n", table, res.Error.Error())
		}
		if len(res.Tables) == 0 {
			continue
		}
		tab := res.Tables[0]
		// Create a mapping col index -> name
		colNames := make([]string, len(tab.Columns))
		for i, c := range tab.Columns {
			colNames[i] = *c.Name
		}
		// Build NDJSON for this chunk only and write as a separate part file
		var partBuilder strings.Builder
		rowsChunk := 0

		// If stitching enabled and relevant table, collect rows for sorting
		type v2row struct {
			tm  string
			ns  string
			pod string
			cn  string
			src string
			msg any
		}
		v2rows := make([]v2row, 0, len(tab.Rows))
		type evtrow struct {
			tm      string
			ns      string
			name    string
			reason  string
			message string
		}
		evrows := make([]evtrow, 0, len(tab.Rows))

		// Column index helpers
		idx := func(name string) int {
			for i, n := range colNames {
				if n == name {
					return i
				}
			}
			return -1
		}
		timeIdx := idx("TimeGenerated")
		// For ContainerLogV2
		nsIdx := idx("PodNamespace")
		podIdx := idx("PodName")
		cnIdx := idx("ContainerName")
		srcIdx := idx("LogSource")
		msgIdx := idx("LogMessage")
		// For KubeEvents
		evNsIdx := idx("Namespace")
		evNameIdx := idx("Name")
		evReasonIdx := idx("Reason")
		evMsgIdx := idx("Message")

		for _, row := range tab.Rows {
			obj := map[string]any{}
			for i, v := range row {
				var val any = v
				obj[colNames[i]] = val
			}
			b, _ := json.Marshal(obj)
			partBuilder.Write(b)
			partBuilder.WriteByte('\n')
			rowsChunk++

			// Stitch accumulation
			if g.config.StitchLogs && table == "ContainerLogV2" && timeIdx >= 0 && nsIdx >= 0 && podIdx >= 0 && cnIdx >= 0 && srcIdx >= 0 && msgIdx >= 0 {
				toStr := func(v any) string {
					if v == nil {
						return ""
					}
					switch t := v.(type) {
					case string:
						return t
					default:
						return fmt.Sprint(t)
					}
				}
				v2rows = append(v2rows, v2row{
					tm:  toStr(row[timeIdx]),
					ns:  toStr(row[nsIdx]),
					pod: toStr(row[podIdx]),
					cn:  toStr(row[cnIdx]),
					src: toStr(row[srcIdx]),
					msg: row[msgIdx],
				})
			}
			if g.config.StitchLogs && g.config.StitchIncludeEvents && table == "KubeEvents" && timeIdx >= 0 && evNsIdx >= 0 && evNameIdx >= 0 && evReasonIdx >= 0 && evMsgIdx >= 0 {
				toStr := func(v any) string {
					if v == nil {
						return ""
					}
					switch t := v.(type) {
					case string:
						return t
					default:
						return fmt.Sprint(t)
					}
				}
				evrows = append(evrows, evtrow{
					tm:      toStr(row[timeIdx]),
					ns:      toStr(row[evNsIdx]),
					name:    toStr(row[evNameIdx]),
					reason:  toStr(row[evReasonIdx]),
					message: toStr(row[evMsgIdx]),
				})
			}
		}
		if rowsChunk > 0 {
			partName := fmt.Sprintf("parts/%04d-%s_%s.ndjson", chunkIndex, t0.UTC().Format(time.RFC3339), t1.UTC().Format(time.RFC3339))
			_ = utils.WriteFileToTar(tarw, filepath.Join("tables", safe, partName), []byte(partBuilder.String()))
			chunkIndex++
			rowsTotal += rowsChunk
		}

		// After writing parts, write stitched chunk into builders in time order
		if g.config.StitchLogs && table == "ContainerLogV2" && len(v2rows) > 0 {
			sort.Slice(v2rows, func(i, j int) bool {
				ti := utils.ParseTimeRFC3339(v2rows[i].tm)
				tj := utils.ParseTimeRFC3339(v2rows[j].tm)
				if ti.IsZero() || tj.IsZero() {
					return v2rows[i].tm < v2rows[j].tm
				}
				return ti.Before(tj)
			})
			// marshal message
			for _, r := range v2rows {
				if r.ns == "" && r.pod == "" && r.cn == "" {
					continue
				}
				// format line
				ts := utils.ParseTimeRFC3339(r.tm).Format(time.RFC3339Nano)
				if ts == "0001-01-01T00:00:00Z" {
					ts = r.tm
				}
				msg := ""
				switch m := r.msg.(type) {
				case string:
					msg = m
				case map[string]any, []any:
					if bb, err := json.Marshal(m); err == nil {
						msg = string(bb)
					} else {
						msg = fmt.Sprint(m)
					}
				default:
					msg = fmt.Sprint(m)
				}
				msg = strings.ReplaceAll(msg, "\r", "")
				msg = strings.ReplaceAll(msg, "\n", "\\n")
				line := fmt.Sprintf("%s [%s] %s\n", ts, r.src, msg)
				buf := getBuf(ckey{ns: r.ns, pod: r.pod, container: r.cn})
				buf.WriteString(line)
			}
		}
		if g.config.StitchLogs && g.config.StitchIncludeEvents && table == "KubeEvents" && len(evrows) > 0 {
			sort.Slice(evrows, func(i, j int) bool {
				ti := utils.ParseTimeRFC3339(evrows[i].tm)
				tj := utils.ParseTimeRFC3339(evrows[j].tm)
				if ti.IsZero() || tj.IsZero() {
					return evrows[i].tm < evrows[j].tm
				}
				return ti.Before(tj)
			})
			for _, r := range evrows {
				ns := r.ns
				if ns == "" {
					ns = "default"
				}
				ts := utils.ParseTimeRFC3339(r.tm).Format(time.RFC3339Nano)
				if ts == "0001-01-01T00:00:00Z" {
					ts = r.tm
				}
				line := fmt.Sprintf("%s %s/%s %s %s\n", ts, ns, r.name, r.reason, strings.ReplaceAll(r.message, "\n", " "))
				buf := getEvt(ns)
				buf.WriteString(line)
			}
		}
	}
	// Write summary
	sum := map[string]any{"table": table, "rows": rowsTotal, "duration": iso}
	b, _ := json.MarshalIndent(sum, "", "  ")
	_ = utils.WriteFileToTar(tarw, filepath.Join("tables", safe, "summary.json"), b)

	return nil
}
