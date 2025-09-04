package main

import (
    "archive/tar"
    "compress/gzip"
    "context"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "io"
    "net/url"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	azquery "github.com/Azure/azure-sdk-for-go/sdk/monitor/azquery"
	armoperationalinsights "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"
)

// parseResourceID splits an Azure resource ID for workspace.
func parseResourceID(id string) (sub, rg, workspace string, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", "", errors.New("empty resource id")
	}
	parts := strings.Split(id, "/")
	// Expect: /subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.OperationalInsights/workspaces/<name>
	if len(parts) < 9 {
		return "", "", "", fmt.Errorf("invalid resource id: %s", id)
	}
	for i := 0; i < len(parts)-1; i++ {
		switch strings.ToLower(parts[i]) {
		case "subscriptions":
			if i+1 < len(parts) {
				sub = parts[i+1]
			}
		case "resourcegroups":
			if i+1 < len(parts) {
				rg = parts[i+1]
			}
		case "workspaces":
			if i+1 < len(parts) {
				workspace = parts[i+1]
			}
		}
	}
	if sub == "" || rg == "" || workspace == "" {
		return "", "", "", fmt.Errorf("failed to parse resource id: %s", id)
	}
	return
}

// iso8601Duration accepts either Go durations (e.g., 2h45m) or ISO-8601 (PT2H45M) and returns ISO-8601.
func iso8601Duration(dur string) (string, error) {
    dur = strings.TrimSpace(dur)
    if dur == "" {
        return "", errors.New("empty duration")
    }
    if strings.HasPrefix(strings.ToUpper(dur), "P") {
        // Assume already ISO-8601
        return dur, nil
    }
    d, err := time.ParseDuration(dur)
    if err != nil {
        return "", fmt.Errorf("parse duration: %w", err)
    }
    // Convert to ISO-8601 PT#H#M#S
    secs := int64(d.Seconds())
    if secs < 0 {
        secs = -secs
    }
    h := secs / 3600
    m := (secs % 3600) / 60
    secRem := secs % 60
    return fmt.Sprintf("PT%dH%dM%dS", h, m, secRem), nil
}

// safeFileName sanitizes table names for filesystem paths.
func safeFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "/", "_")
	re := regexp.MustCompile(`[^A-Za-z0-9_.\-]`)
	name = re.ReplaceAllString(name, "_")
	if name == "" { name = "unnamed" }
	return name
}

func writeFileToTar(tw *tar.Writer, path string, data []byte) error {
	hdr := &tar.Header{
		Name:    path,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil { return err }
	_, err := tw.Write(data)
	return err
}

func writeStreamToTar(tw *tar.Writer, path string, r io.Reader) error {
	// Stream to a temp buffer to get size? Tar needs size up-front; so we buffer in memory for now.
	// For large outputs, consider chunk files.
	buf, err := io.ReadAll(r)
	if err != nil { return err }
	return writeFileToTar(tw, path, buf)
}

func main() {
    var (
        workspaceID    string // ARM resource ID
        timespanStr    string
        outTar         string
        tableFilterCSV string
        profilesCSV    string
        allTables      bool
    )
    flag.StringVar(&workspaceID, "workspace-id", "", "Log Analytics workspace ARM resource ID")
    flag.StringVar(&timespanStr, "timespan", "PT2H", "Timespan to query (ISO-8601 like PT6H, or Go duration like 6h)")
    flag.StringVar(&outTar, "out", fmt.Sprintf("must-gather-%s.tar.gz", time.Now().Format("20060102-150405")), "Output tar.gz path")
    flag.StringVar(&tableFilterCSV, "tables", "", "Optional comma-separated list of tables to export (overrides profiles)")
    flag.StringVar(&profilesCSV, "profiles", "", "Optional comma-separated profiles: aks-debug,podLogs,inventory,metrics,audit")
    flag.BoolVar(&allTables, "all-tables", false, "Export all tables in the workspace (may be slow). Overrides profiles/tables if used.")
    // Stitched logs options
    stitchLogs := true
    stitchIncludeEvents := true
    flag.BoolVar(&stitchLogs, "stitch-logs", true, "Also include time-ordered logs per namespace/pod/container under namespaces/ folder")
    flag.BoolVar(&stitchIncludeEvents, "stitch-include-events", true, "Include KubeEvents under namespaces/<ns>/events/events.log")
    flag.Parse()

    if workspaceID == "" {
        fmt.Fprintln(os.Stderr, "must provide --workspace-id (workspace ARM resource ID)")
        os.Exit(2)
    }

	iso, err := iso8601Duration(timespanStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid timespan:", err)
		os.Exit(2)
	}

	ctx := context.Background()
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to init credential:", err)
		os.Exit(1)
	}

    // Resolve GUID and list of tables.
    var (
        subID string
        rg string
        wsName string
        tables []string
        workspaceGUID string // resolved customerId
    )

    if workspaceID != "" {
        subID, rg, wsName, err = parseResourceID(workspaceID)
        if err != nil {
            fmt.Fprintln(os.Stderr, "parse workspace-id:", err)
            os.Exit(2)
        }
        // Get workspace properties including customerId
        wcli, err := armoperationalinsights.NewWorkspacesClient(subID, cred, nil)
        if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
        w, err := wcli.Get(ctx, rg, wsName, nil)
        if err != nil { fmt.Fprintln(os.Stderr, "get workspace:", err); os.Exit(1) }
        if w.Properties != nil && w.Properties.CustomerID != nil {
            workspaceGUID = *w.Properties.CustomerID
        }
        if allTables {
            // List tables via management plane only when explicitly requested
            tcli, err := armoperationalinsights.NewTablesClient(subID, cred, nil)
            if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
            pager := tcli.NewListByWorkspacePager(rg, wsName, nil)
            for pager.More() {
                page, err := pager.NextPage(ctx)
                if err != nil { fmt.Fprintln(os.Stderr, "list tables:", err); os.Exit(1) }
                for _, t := range page.Value {
                    if t.Name != nil { tables = append(tables, *t.Name) }
                }
            }
        }
    }

    if workspaceGUID == "" {
        fmt.Fprintln(os.Stderr, "could not determine workspace GUID from workspace; check permissions or workspace-id")
        os.Exit(2)
    }

    if tableFilterCSV != "" {
        // override tables with filter list
        parts := strings.Split(tableFilterCSV, ",")
        tables = tables[:0]
        for _, p := range parts {
            p = strings.TrimSpace(p)
            if p != "" { tables = append(tables, p) }
        }
    }

    // Profiles mapping
    profileMap := map[string][]string{
        "podLogs":  {"ContainerLogV2", "ContainerLog", "KubeEvents", "KubeMonAgentEvents", "Syslog"},
        "inventory": {"KubePodInventory", "KubeNodeInventory", "KubeServices", "KubePVInventory", "ContainerInventory", "ContainerImageInventory", "ContainerNodeInventory", "KubeHealth"},
        "metrics":  {"InsightsMetrics", "Perf", "Heartbeat"},
        "audit":    {"AKSControlPlane", "AKSAudit", "AKSAuditAdmin"},
    }

    // Alias: aks-debug = podLogs + inventory + metrics
    {
        combined := make([]string, 0, 32)
        seen := map[string]struct{}{}
        for _, k := range []string{"podLogs", "inventory", "metrics"} {
            for _, t := range profileMap[k] {
                if _, ok := seen[t]; ok { continue }
                seen[t] = struct{}{}
                combined = append(combined, t)
            }
        }
        profileMap["aks-debug"] = combined
    }

    // If profiles provided, union their table lists (overridden by --tables if set earlier)
    if len(tables) == 0 && profilesCSV != "" && !allTables {
        profs := strings.Split(profilesCSV, ",")
        seen := map[string]struct{}{}
        for _, p := range profs {
            p = strings.TrimSpace(p)
            if p == "" { continue }
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
    if len(tables) == 0 && !allTables {
        def := append([]string{}, profileMap["aks-debug"]...)
        // dedupe
        seen := map[string]struct{}{}
        for _, t := range def { if _, ok := seen[t]; !ok { tables = append(tables, t); seen[t] = struct{}{} } }
    }

	// Prepare tar.gz writer
	outF, err := os.Create(outTar)
	if err != nil { fmt.Fprintln(os.Stderr, "create out:", err); os.Exit(1) }
	defer outF.Close()
	gz := gzip.NewWriter(outF)
	defer gz.Close()
	tarw := tar.NewWriter(gz)
	defer tarw.Close()

	// Write metadata
	meta := map[string]any{
		"generatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"workspaceGUID": workspaceGUID,
		"workspaceID": workspaceID,
		"timespan": iso,
		"tablesCount": len(tables),
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	_ = writeFileToTar(tarw, "metadata/workspace.json", metaBytes)

	// If we have management-plane info, persist it
	if subID != "" && rg != "" && wsName != "" {
		mp := map[string]string{"subscriptionId": subID, "resourceGroup": rg, "workspaceName": wsName}
		mpb, _ := json.MarshalIndent(mp, "", "  ")
		_ = writeFileToTar(tarw, "metadata/azure.json", mpb)
	}

    // Initialize logs client
    lcli, err := azquery.NewLogsClient(cred, nil)
    if err != nil { fmt.Fprintln(os.Stderr, "logs client:", err); os.Exit(1) }

	// Helper: fetch schema for a table if we can (management plane only)
	var tcli *armoperationalinsights.TablesClient
	if subID != "" {
		if tcli, err = armoperationalinsights.NewTablesClient(subID, cred, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

    // Export each table
    // Accumulators for stitched logs
    type ckey struct{ ns, pod, container string }
    stitchedLogs := map[ckey]*strings.Builder{}
    stitchedEvents := map[string]*strings.Builder{}
    // helpers
    getBuf := func(k ckey) *strings.Builder {
        if b, ok := stitchedLogs[k]; ok { return b }
        b := &strings.Builder{}
        stitchedLogs[k] = b
        return b
    }
    getEvt := func(ns string) *strings.Builder {
        if b, ok := stitchedEvents[ns]; ok { return b }
        b := &strings.Builder{}
        stitchedEvents[ns] = b
        return b
    }

    for _, table := range tables {
        fmt.Fprintf(os.Stderr, "Exporting %s...\n", table)
        safe := safeFileName(table)
        // Schema
        if tcli != nil {
			if resp, err := tcli.Get(ctx, rg, wsName, table, nil); err == nil {
				b, _ := json.MarshalIndent(resp.Table, "", "  ")
				_ = writeFileToTar(tarw, filepath.Join("tables", safe, "schema.json"), b)
			}
		}

		// Data: chunk queries by hour to avoid limits.
		// Determine time window now-iso to since.
		since := time.Now().UTC()
		// Parse iso timespan to duration for chunking
		dur := time.Duration(0)
		if d2, err := parseISO8601ToDuration(iso); err == nil { dur = d2 } else if d3, err := time.ParseDuration(timespanStr); err == nil { dur = d3 }
		start := since.Add(-dur)
		if dur == 0 { start = since.Add(-2 * time.Hour) }

		// chunk = 1h if dur>2h else 15m
		chunk := time.Hour
		if dur <= 2*time.Hour { chunk = 15 * time.Minute }

        rowsTotal := 0
        chunkIndex := 0
        for t0 := start; t0.Before(since); t0 = t0.Add(chunk) {
            t1 := t0.Add(chunk)
            if t1.After(since) { t1 = since }
            // Build time-bounded query via timespan
            q := table
            body := azquery.Body{Query: &q, Timespan: to.Ptr(azquery.NewTimeInterval(t0.UTC(), t1.UTC()))}
            // Increase server-side wait timeout
            res, err := lcli.QueryWorkspace(ctx, workspaceGUID, body, &azquery.LogsClientQueryWorkspaceOptions{Options: &azquery.LogsQueryOptions{Wait: to.Ptr(180)}})
            if err != nil {
                // Note: If the table doesn't exist, ignore.
                fmt.Fprintf(os.Stderr, "  warn: query chunk failed for %s: %v\n", table, err)
                continue
            }
            if res.Error != nil {
                fmt.Fprintf(os.Stderr, "  warn: partial/error for %s: %v\n", table, res.Error.Error())
            }
            if len(res.Tables) == 0 { continue }
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
                tm string
                ns string
                pod string
                cn string
                src string
                msg any
            }
            v2rows := make([]v2row, 0, len(tab.Rows))
            type evtrow struct {
                tm string
                ns string
                name string
                reason string
                message string
            }
            evrows := make([]evtrow, 0, len(tab.Rows))

            // Column index helpers
            idx := func(name string) int { for i, n := range colNames { if n == name { return i } }; return -1 }
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
                if stitchLogs && table == "ContainerLogV2" && timeIdx >= 0 && nsIdx >= 0 && podIdx >= 0 && cnIdx >= 0 && srcIdx >= 0 && msgIdx >= 0 {
                    toStr := func(v any) string { if v == nil { return "" }; switch t := v.(type) { case string: return t; default: return fmt.Sprint(t) } }
                    v2rows = append(v2rows, v2row{
                        tm: toStr(row[timeIdx]),
                        ns: toStr(row[nsIdx]),
                        pod: toStr(row[podIdx]),
                        cn: toStr(row[cnIdx]),
                        src: toStr(row[srcIdx]),
                        msg: row[msgIdx],
                    })
                }
                if stitchLogs && stitchIncludeEvents && table == "KubeEvents" && timeIdx >= 0 && evNsIdx >= 0 && evNameIdx >= 0 && evReasonIdx >= 0 && evMsgIdx >= 0 {
                    toStr := func(v any) string { if v == nil { return "" }; switch t := v.(type) { case string: return t; default: return fmt.Sprint(t) } }
                    evrows = append(evrows, evtrow{
                        tm: toStr(row[timeIdx]),
                        ns: toStr(row[evNsIdx]),
                        name: toStr(row[evNameIdx]),
                        reason: toStr(row[evReasonIdx]),
                        message: toStr(row[evMsgIdx]),
                    })
                }
            }
            if rowsChunk > 0 {
                partName := fmt.Sprintf("parts/%04d-%s_%s.ndjson", chunkIndex, t0.UTC().Format(time.RFC3339), t1.UTC().Format(time.RFC3339))
                _ = writeFileToTar(tarw, filepath.Join("tables", safe, partName), []byte(partBuilder.String()))
                chunkIndex++
                rowsTotal += rowsChunk
            }

            // After writing parts, write stitched chunk into builders in time order
            if stitchLogs && table == "ContainerLogV2" && len(v2rows) > 0 {
                sort.Slice(v2rows, func(i, j int) bool {
                    ti := parseTimeRFC3339(v2rows[i].tm)
                    tj := parseTimeRFC3339(v2rows[j].tm)
                    if ti.IsZero() || tj.IsZero() { return v2rows[i].tm < v2rows[j].tm }
                    return ti.Before(tj)
                })
                // marshal message
                for _, r := range v2rows {
                    if r.ns == "" && r.pod == "" && r.cn == "" { continue }
                    // format line
                    ts := parseTimeRFC3339(r.tm).Format(time.RFC3339Nano)
                    if ts == "0001-01-01T00:00:00Z" { ts = r.tm }
                    msg := ""
                    switch m := r.msg.(type) {
                    case string:
                        msg = m
                    case map[string]any, []any:
                        if bb, err := json.Marshal(m); err == nil { msg = string(bb) } else { msg = fmt.Sprint(m) }
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
            if stitchLogs && stitchIncludeEvents && table == "KubeEvents" && len(evrows) > 0 {
                sort.Slice(evrows, func(i, j int) bool {
                    ti := parseTimeRFC3339(evrows[i].tm)
                    tj := parseTimeRFC3339(evrows[j].tm)
                    if ti.IsZero() || tj.IsZero() { return evrows[i].tm < evrows[j].tm }
                    return ti.Before(tj)
                })
                for _, r := range evrows {
                    ns := r.ns
                    if ns == "" { ns = "default" }
                    ts := parseTimeRFC3339(r.tm).Format(time.RFC3339Nano)
                    if ts == "0001-01-01T00:00:00Z" { ts = r.tm }
                    line := fmt.Sprintf("%s %s/%s %s %s\n", ts, ns, r.name, r.reason, strings.ReplaceAll(r.message, "\n", " "))
                    buf := getEvt(ns)
                    buf.WriteString(line)
                }
            }
        }
        // Write summary
        sum := map[string]any{"table": table, "rows": rowsTotal, "duration": iso}
        b, _ := json.MarshalIndent(sum, "", "  ")
        _ = writeFileToTar(tarw, filepath.Join("tables", safe, "summary.json"), b)
    }

    // Write stitched logs into the tar
    if stitchLogs {
        for k, b := range stitchedLogs {
            if b.Len() == 0 { continue }
            ns := safeFileName(k.ns)
            pod := safeFileName(k.pod)
            cn := safeFileName(k.container)
            path := filepath.Join("namespaces", ns, "pods", pod, cn+".log")
            _ = writeFileToTar(tarw, path, []byte(b.String()))
        }
        if stitchIncludeEvents {
            for ns, b := range stitchedEvents {
                if b.Len() == 0 { continue }
                path := filepath.Join("namespaces", safeFileName(ns), "events", "events.log")
                _ = writeFileToTar(tarw, path, []byte(b.String()))
            }
        }
    }

    // Index file
    index := map[string]any{"tables": tables}
    idxb, _ := json.MarshalIndent(index, "", "  ")
    _ = writeFileToTar(tarw, "index.json", idxb)

	fmt.Fprintf(os.Stderr, "Wrote %s\n", outTar)
}

// parseISO8601ToDuration parses a subset of ISO8601 durations like PT6H, PT30M, PT1H30M.
func parseISO8601ToDuration(iso string) (time.Duration, error) {
	iso = strings.ToUpper(strings.TrimSpace(iso))
	if !strings.HasPrefix(iso, "P") { return 0, fmt.Errorf("not iso8601: %s", iso) }
	// Only support time part for now (PT..)
	i := strings.Index(iso, "T")
	if i == -1 { return 0, fmt.Errorf("only time components supported: %s", iso) }
	part := iso[i+1:]
	var total time.Duration
	re := regexp.MustCompile(`(?i)(\d+)H`)
	if m := re.FindStringSubmatch(part); len(m) == 2 { if v, _ := time.ParseDuration(m[1]+"h"); v > 0 { total += v } }
	re = regexp.MustCompile(`(?i)(\d+)M`)
	if m := re.FindStringSubmatch(part); len(m) == 2 { if v, _ := time.ParseDuration(m[1]+"m"); v > 0 { total += v } }
	re = regexp.MustCompile(`(?i)(\d+)S`)
	if m := re.FindStringSubmatch(part); len(m) == 2 { if v, _ := time.ParseDuration(m[1]+"s"); v > 0 { total += v } }
	return total, nil
}

// ensure imported packages are referenced
var _ = url.URL{}

// parseTimeRFC3339 parses RFC3339/RFC3339Nano, returns zero time on failure
func parseTimeRFC3339(s string) time.Time {
    s = strings.TrimSpace(s)
    if s == "" { return time.Time{} }
    if ts, err := time.Parse(time.RFC3339Nano, s); err == nil { return ts }
    if ts, err := time.Parse(time.RFC3339, s); err == nil { return ts }
    return time.Time{}
}
