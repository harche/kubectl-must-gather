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
    "log"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/Azure/azure-kusto-go/azkustodata"
    "github.com/Azure/azure-kusto-go/azkustodata/kql"
    "github.com/Azure/azure-kusto-go/azkustodata/types"
)

type QueryDef struct {
    Name     string `json:"name"`
    Database string `json:"database"`
    File     string `json:"file"`
    KQL      string `json:"kql"`
    Format   string `json:"format"` // json|ndjson; default ndjson
    MaxRows  int    `json:"maxRows"`
}

type Manifest struct {
    Cluster     string            `json:"cluster"`
    ClusterURI  string            `json:"clusterUri"`
    TimeStart   time.Time         `json:"timeStart"`
    TimeEnd     time.Time         `json:"timeEnd"`
    TimeWindow  string            `json:"timeWindow"`
    TimeBucket  string            `json:"timeBucket"`
    Queries     []QueryStatus     `json:"queries"`
    Env         map[string]string `json:"env,omitempty"`
    Errors      []string          `json:"errors,omitempty"`
    Version     string            `json:"version"`
}

type QueryStatus struct {
    Name     string `json:"name"`
    Database string `json:"database"`
    File     string `json:"file"`
    Rows     int    `json:"rows"`
    Error    string `json:"error,omitempty"`
    Duration string `json:"duration"`
}

type RunOpts struct {
    ClusterURI string
    Database   string
    Cluster    string
    Since      time.Duration
    Start      time.Time
    End        time.Time
    Bucket     string
    OutDir     string
    Tar        bool
    Timeout    time.Duration
}

func main() {
    var (
        clusterURI = flag.String("cluster-uri", getenv("KUSTO_CLUSTER", ""), "Kusto cluster URI, e.g. https://<cluster>.<region>.kusto.windows.net (env KUSTO_CLUSTER)")
        database   = flag.String("database", getenv("KUSTO_DATABASE", "Metrics"), "Default database to query (env KUSTO_DATABASE)")
        cluster    = flag.String("cluster", getenv("ADXMON_CLUSTER", ""), "Cluster label value to filter on (ADX-Mon 'Cluster' column)")
        since      = flag.Duration("since", time.Hour, "How far back to gather (e.g. 30m, 2h)")
        startStr   = flag.String("start", "", "Start time RFC3339 (overrides --since)")
        endStr     = flag.String("end", "", "End time RFC3339 (defaults now)")
        bucket     = flag.String("bucket", "", "Time bucket for aggregations like $__timeInterval (e.g. 1m,5m,15m). Empty = auto")
        outDir     = flag.String("out", "", "Output folder (default: must-gather-<timestamp>)")
        noTar      = flag.Bool("no-tar", false, "Do not create .tar.gz, leave folder only")
        timeout    = flag.Duration("timeout", 2*time.Minute, "Per-query timeout")
        queries    = flag.String("queries", "", "Path to JSON file with query definitions (optional). If empty, uses built-in set.")
        verbose    = flag.Bool("v", false, "Verbose logging")
    )
    flag.Parse()

    if *clusterURI == "" {
        log.Fatalf("--cluster-uri or KUSTO_CLUSTER is required")
    }
    if *cluster == "" {
        log.Fatalf("--cluster (or ADXMON_CLUSTER) is required to avoid ambiguous results")
    }

    // Time range
    var start, end time.Time
    if strings.TrimSpace(*startStr) != "" {
        s, err := time.Parse(time.RFC3339, *startStr)
        if err != nil {
            log.Fatalf("invalid --start: %v", err)
        }
        start = s.UTC()
        if strings.TrimSpace(*endStr) != "" {
            e, err := time.Parse(time.RFC3339, *endStr)
            if err != nil {
                log.Fatalf("invalid --end: %v", err)
            }
            end = e.UTC()
        } else {
            end = time.Now().UTC()
        }
    } else {
        end = time.Now().UTC()
        start = end.Add(-*since)
    }

    // Bucket auto
    bucketVal := *bucket
    if bucketVal == "" {
        d := end.Sub(start)
        switch {
        case d <= 30*time.Minute:
            bucketVal = "1m"
        case d <= 2*time.Hour:
            bucketVal = "5m"
        case d <= 12*time.Hour:
            bucketVal = "15m"
        case d <= 48*time.Hour:
            bucketVal = "30m"
        default:
            bucketVal = "1h"
        }
    }

    // Output dir
    out := *outDir
    if out == "" {
        out = fmt.Sprintf("must-gather-%s", time.Now().UTC().Format("20060102-150405"))
    }
    if err := os.MkdirAll(out, 0o755); err != nil {
        log.Fatalf("failed to create out dir: %v", err)
    }

    // Prepare queries
    var qdefs []QueryDef
    var err error
    if strings.TrimSpace(*queries) != "" {
        qdefs, err = loadQueriesFromJSON(*queries)
        if err != nil {
            log.Fatalf("failed to load queries: %v", err)
        }
    } else {
        qdefs = builtinQueries()
    }

    // Build client
    kcsb := azkustodata.NewConnectionStringBuilder(*clusterURI).WithDefaultAzureCredential()
    client, err := azkustodata.New(kcsb)
    if err != nil {
        log.Fatalf("creating kusto client: %v", err)
    }
    defer client.Close()

    // Run
    opts := RunOpts{
        ClusterURI: *clusterURI,
        Database:   *database,
        Cluster:    *cluster,
        Since:      end.Sub(start),
        Start:      start,
        End:        end,
        Bucket:     bucketVal,
        OutDir:     out,
        Tar:        !*noTar,
        Timeout:    *timeout,
    }
    if *verbose {
        log.Printf("Gather: clusterUri=%s cluster=%s db=%s start=%s end=%s bucket=%s out=%s",
            opts.ClusterURI, safePrint(opts.Cluster), opts.Database, opts.Start.Format(time.RFC3339), opts.End.Format(time.RFC3339), opts.Bucket, opts.OutDir)
    }

    manifest := Manifest{
        Cluster:    opts.Cluster,
        ClusterURI: opts.ClusterURI,
        TimeStart:  opts.Start,
        TimeEnd:    opts.End,
        TimeWindow: opts.End.Sub(opts.Start).String(),
        TimeBucket: opts.Bucket,
        Version:    "0.1.0",
        Env:        map[string]string{},
    }

    // Save context file
    if err := writeJSON(filepath.Join(out, "meta.json"), manifest); err != nil {
        log.Printf("WARN: failed writing meta.json: %v", err)
    }

    // Execute queries
    var statuses []QueryStatus
    for _, qd := range qdefs {
        st := QueryStatus{Name: qd.Name, Database: firstNonEmpty(qd.Database, opts.Database), File: qd.File}
        started := time.Now()
        rows, qerr := runOneQuery(client, opts, qd)
        st.Duration = time.Since(started).String()
        st.Rows = rows
        if qerr != nil {
            st.Error = qerr.Error()
            manifest.Errors = append(manifest.Errors, fmt.Sprintf("%s: %v", qd.Name, qerr))
            log.Printf("ERROR: %s: %v", qd.Name, qerr)
        } else {
            log.Printf("OK: %s -> %s (%d rows)", qd.Name, qd.File, rows)
        }
        statuses = append(statuses, st)
    }
    manifest.Queries = statuses
    // Write final manifest
    if err := writeJSON(filepath.Join(out, "manifest.json"), manifest); err != nil {
        log.Printf("WARN: failed writing manifest.json: %v", err)
    }

    if opts.Tar {
        tarPath := out + ".tar.gz"
        if err := tarGzDir(out, tarPath); err != nil {
            log.Printf("WARN: failed to create archive: %v", err)
        } else {
            log.Printf("Archive: %s", tarPath)
        }
    }
}

func runOneQuery(client *azkustodata.Client, opts RunOpts, qd QueryDef) (int, error) {
    db := firstNonEmpty(qd.Database, opts.Database)
    // Prepare KQL with substitutions
    kqlText := substitutePlaceholders(qd.KQL, opts)
    // Build KQL builder (unsafe because string is runtime)
    qb := (&kql.Builder{}).AddUnsafe(kqlText)

    ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
    defer cancel()
    ds, err := client.IterativeQuery(ctx, db, qb)
    if err != nil {
        return 0, err
    }
    defer ds.Close()

    // Ensure parent dirs exist
    outPath := filepath.Join(opts.OutDir, qd.File)
    if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
        return 0, err
    }
    f, err := os.Create(outPath)
    if err != nil {
        return 0, err
    }
    defer f.Close()

    // Stream rows as NDJSON, only PrimaryResult table
    rowsWritten := 0
    for tr := range ds.Tables() {
        if tr.Err() != nil {
            return rowsWritten, tr.Err()
        }
        t := tr.Table()
        if t.Name() != "PrimaryResult" {
            // Skip metadata tables to keep output concise
            continue
        }
        cols := t.Columns()
        for rr := range t.Rows() {
            if rr.Err() != nil {
                return rowsWritten, rr.Err()
            }
            row := rr.Row()
            vals := row.Values()
            obj := make(map[string]interface{}, len(cols)+2)
            obj["_rowIndex"] = row.Index()
            for i, c := range cols {
                if i >= len(vals) {
                    continue
                }
                v := vals[i]
                if v == nil {
                    obj[c.Name()] = nil
                    continue
                }
                if c.Type() == types.Dynamic {
                    if b, ok := v.GetValue().([]byte); ok {
                        var any interface{}
                        if err := json.Unmarshal(b, &any); err == nil {
                            obj[c.Name()] = any
                            continue
                        }
                        obj[c.Name()] = string(b)
                        continue
                    }
                    if pb, ok := v.GetValue().(*[]byte); ok {
                        if pb == nil {
                            obj[c.Name()] = nil
                            continue
                        }
                        var any interface{}
                        if err := json.Unmarshal(*pb, &any); err == nil {
                            obj[c.Name()] = any
                            continue
                        }
                        obj[c.Name()] = string(*pb)
                        continue
                    }
                }
                obj[c.Name()] = v.GetValue()
            }
            enc, err := json.Marshal(obj)
            if err != nil {
                return rowsWritten, err
            }
            if _, err := f.Write(enc); err != nil {
                return rowsWritten, err
            }
            if _, err := f.Write([]byte("\n")); err != nil {
                return rowsWritten, err
            }
            rowsWritten++
            if qd.MaxRows > 0 && rowsWritten >= qd.MaxRows {
                break
            }
        }
    }
    return rowsWritten, nil
}

func substitutePlaceholders(q string, opts RunOpts) string {
    // Replace $Cluster with a safe literal
    if opts.Cluster != "" {
        q = strings.ReplaceAll(q, "$Cluster", fmt.Sprintf("'%s'", kqlQuote(opts.Cluster)))
    }
    // $__timeFilter(Timestamp)
    tf := fmt.Sprintf("Timestamp between (datetime(%s)..datetime(%s))", opts.Start.Format(time.RFC3339), opts.End.Format(time.RFC3339))
    q = strings.ReplaceAll(q, "$__timeFilter(Timestamp)", tf)
    // $__timeInterval
    q = strings.ReplaceAll(q, "$__timeInterval", opts.Bucket)
    return q
}

func builtinQueries() []QueryDef {
    return []QueryDef{
        {
            Name:     "node-capacity-cpu",
            Database: "Metrics",
            File:     filepath.Join("metrics", "node-capacity-cpu.ndjson"),
            KQL: `KubeNodeStatusCapacity
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels.resource == "cpu" and Labels.unit == "core"
| extend Node=tostring(Labels.node)
| project Timestamp, Cluster, Node, Value
| order by Timestamp asc`,
        },
        {
            Name:     "node-allocatable-cpu",
            Database: "Metrics",
            File:     filepath.Join("metrics", "node-allocatable-cpu.ndjson"),
            KQL: `KubeNodeStatusAllocatable
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels.resource == "cpu" and Labels.unit == "core"
| extend Node=tostring(Labels.node)
| project Timestamp, Cluster, Node, Value
| order by Timestamp asc`,
        },
        {
            Name:     "node-allocatable-mem",
            Database: "Metrics",
            File:     filepath.Join("metrics", "node-allocatable-mem.ndjson"),
            KQL: `KubeNodeStatusAllocatable
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels.resource == "memory" and Labels.unit == "byte"
| extend Node=tostring(Labels.node)
| project Timestamp, Cluster, Node, Value
| order by Timestamp asc`,
        },
        {
            Name:     "node-capacity-mem",
            Database: "Metrics",
            File:     filepath.Join("metrics", "node-capacity-mem.ndjson"),
            KQL: `KubeNodeStatusCapacity
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels.resource == "memory" and Labels.unit == "byte"
| extend Node=tostring(Labels.node)
| project Timestamp, Cluster, Node, Value
| order by Timestamp asc`,
        },
        {
            Name:     "apiserver-requests-by-code",
            Database: "Metrics",
            File:     filepath.Join("apiserver", "requests-by-code.ndjson"),
            KQL: `ApiserverRequestTotal
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| invoke prom_delta()
| extend Code=tostring(Labels.code)
| summarize Value=sum(Value) by bin(Timestamp, $__timeInterval), Code
| project Timestamp, Code, Value
| order by Timestamp asc`,
        },
        {
            Name:     "apiserver-requests-top-resources",
            Database: "Metrics",
            File:     filepath.Join("apiserver", "requests-top-resources.ndjson"),
            KQL: `ApiserverRequestTotal
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels.resource != ""
| invoke prom_rate()
| extend Resource=tostring(Labels.resource)
| summarize Value=sum(Value) by Resource
| top 50 by Value desc`,
        },
        {
            Name:     "apiserver-latency-histogram",
            Database: "Metrics",
            File:     filepath.Join("apiserver", "latency-histogram.ndjson"),
            KQL: `ApiserverRequestDurationSecondsBucket
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Series=toreal(Labels.le)
| where Labels.le != "+Inf"
| invoke prom_delta()
| summarize Value=sum(Value) by bin(Timestamp, $__timeInterval), Series
| order by Timestamp desc, Series asc
| extend Value=case(prev(Series) < Series, iff(Value-prev(Value) > 0, Value-prev(Value), toreal(0)), Value)
| project Timestamp, tostring(Series), Value
| order by Timestamp asc`,
        },
        {
            Name:     "apiserver-latency-avg",
            Database: "Metrics",
            File:     filepath.Join("apiserver", "latency-avg.ndjson"),
            KQL: `let sums = ApiserverRequestDurationSecondsSum
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| invoke prom_delta()
| summarize Sum=sum(Value) by bin(Timestamp, $__timeInterval);
let counts = ApiserverRequestDurationSecondsCount
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| invoke prom_delta()
| summarize Cnt=sum(Value) by bin(Timestamp, $__timeInterval);
sums | join kind=inner counts on Timestamp
| extend Avg = iif(Cnt > 0, Sum / Cnt, real(null))
| project Timestamp, Avg
| order by Timestamp asc`,
        },
        {
            Name:     "apiserver-latency-avg-by-verb",
            Database: "Metrics",
            File:     filepath.Join("apiserver", "latency-avg-by-verb.ndjson"),
            KQL: `let sums = ApiserverRequestDurationSecondsSum
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Verb=tostring(Labels.verb)
| invoke prom_delta()
| summarize Sum=sum(Value) by bin(Timestamp, $__timeInterval), Verb;
let counts = ApiserverRequestDurationSecondsCount
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Verb=tostring(Labels.verb)
| invoke prom_delta()
| summarize Cnt=sum(Value) by bin(Timestamp, $__timeInterval), Verb;
sums | join kind=inner counts on Timestamp, Verb
| extend Avg = iif(Cnt > 0, Sum / Cnt, real(null))
| project Timestamp, Verb, Avg
| order by Timestamp asc`,
        },
        {
            Name:     "container-cpu-usage",
            Database: "Metrics",
            File:     filepath.Join("workload", "container-cpu-usage.ndjson"),
            KQL: `ContainerCpuUsageSecondsTotal
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels.cpu == "total"
| where Container == "cadvisor"
| extend Namespace=tostring(Labels.namespace), Pod=tostring(Labels.pod), Container=tostring(Labels.container)
| where Container == ""
| extend Id = tostring(Labels.id)
| where Id endswith ".slice"
| invoke prom_rate()
| summarize Value=avg(Value) by bin(Timestamp, $__timeInterval), Namespace, Id
| summarize Value=sum(Value) by Timestamp, Namespace
| order by Timestamp asc`,
        },
        {
            Name:     "container-memory-workingset",
            Database: "Metrics",
            File:     filepath.Join("workload", "container-memory-workingset.ndjson"),
            KQL: `ContainerMemoryWorkingSetBytes
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where Labels !has "id"
| extend Namespace=tostring(Labels.namespace), Pod=tostring(Labels.pod), Container=tostring(Labels.container)
| where Container != ""
| summarize Value=avg(Value) by bin(Timestamp, $__timeInterval), Namespace, Pod
| order by Timestamp asc`,
        },
        {
            Name:     "pod-restarts-timeseries",
            Database: "Metrics",
            File:     filepath.Join("workload", "pod-restarts-timeseries.ndjson"),
            KQL: `KubePodContainerStatusRestartsTotal
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Namespace=tostring(Labels['namespace']), Pod=tostring(Labels['pod']), Container=tostring(Labels['container'])
| invoke prom_delta()
| summarize Restarts=sum(Value) by bin(Timestamp, $__timeInterval), Namespace, Pod, Container
| order by Timestamp asc`,
        },
        {
            Name:     "pod-restarts-top",
            Database: "Metrics",
            File:     filepath.Join("workload", "pod-restarts-top.ndjson"),
            KQL: `KubePodContainerStatusRestartsTotal
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Namespace=tostring(Labels['namespace']), Pod=tostring(Labels['pod']), Container=tostring(Labels['container'])
| invoke prom_delta()
| summarize Restarts=sum(Value) by Namespace, Pod, Container
| top 50 by Restarts desc`,
        },
        {
            Name:     "pod-phase-counts",
            Database: "Metrics",
            File:     filepath.Join("workload", "pod-phase-counts.ndjson"),
            KQL: `KubePodStatusPhase
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Phase=tostring(Labels['phase'])
| summarize Pods=sum(Value) by bin(Timestamp, $__timeInterval), Phase
| order by Timestamp asc`,
        },
        {
            Name:     "kube-pod-info",
            Database: "Metrics",
            File:     filepath.Join("inventory", "kube-pod-info.ndjson"),
            KQL: `KubePodInfo
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Namespace=tostring(Labels.namespace), Pod=tostring(Labels.pod)
| project Timestamp, Namespace, Pod, Value
| order by Timestamp asc`,
        },
        {
            Name:     "node-conditions",
            Database: "Metrics",
            File:     filepath.Join("nodes", "node-conditions.ndjson"),
            KQL: `KubeNodeStatusCondition
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Node=tostring(Labels['node']), Condition=tostring(Labels['condition']), Status=tostring(Labels['status'])
| summarize Value=avg(Value) by bin(Timestamp, $__timeInterval), Node, Condition, Status
| order by Timestamp asc`,
        },
        {
            Name:     "node-notready-count",
            Database: "Metrics",
            File:     filepath.Join("nodes", "node-notready-count.ndjson"),
            KQL: `KubeNodeStatusCondition
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where tostring(Labels['condition']) == "Ready" and tostring(Labels['status']) == "false"
| summarize NotReady=sum(Value) by bin(Timestamp, $__timeInterval)
| order by Timestamp asc`,
        },
        {
            Name:     "node-info-latest",
            Database: "Metrics",
            File:     filepath.Join("nodes", "node-info.ndjson"),
            KQL: `KubeNodeInfo
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| extend Node=tostring(Labels['node']), InternalIP=tostring(Labels['internal_ip']), OSImage=tostring(Labels['os_image']), KubeletVersion=tostring(Labels['kubelet_version']), ContainerRuntime=tostring(Labels['container_runtime_version'])
| summarize arg_max(Timestamp, InternalIP, OSImage, KubeletVersion, ContainerRuntime) by Node
| project Node, Timestamp, InternalIP, OSImage, KubeletVersion, ContainerRuntime
| order by Node asc`,
        },
        {
            Name:     "logs-kubelet-sample",
            Database: "Logs",
            File:     filepath.Join("logs", "kubelet.ndjson"),
            KQL: `Kubelet
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| project Timestamp, SeverityText, Message=tostring(Body.message), Cluster, Host, Namespace, Pod
| order by Timestamp asc`,
            MaxRows: 20000,
        },
        {
            Name:     "logs-oom-messages",
            Database: "Logs",
            File:     filepath.Join("logs", "oom.ndjson"),
            KQL: `Kubelet
| where $__timeFilter(Timestamp)
| where Cluster == $Cluster
| where tolower(tostring(Body.message)) has "oom"
| project Timestamp, Message=tostring(Body.message), Cluster, Host, Namespace, Pod
| order by Timestamp asc`,
            MaxRows: 20000,
        },
    }
}

func kqlQuote(s string) string { // escape single quotes for KQL string literal
    return strings.ReplaceAll(s, "'", "''")
}

func safePrint(s string) string {
    if s == "" {
        return s
    }
    if len(s) > 80 {
        return s[:80] + "…"
    }
    return s
}

func firstNonEmpty(a, b string) string {
    if strings.TrimSpace(a) != "" {
        return a
    }
    return b
}

func writeJSON(path string, v interface{}) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    f, err := os.Create(path)
    if err != nil {
        return err
    }
    defer f.Close()
    enc := json.NewEncoder(f)
    enc.SetIndent("", "  ")
    return enc.Encode(v)
}

func tarGzDir(srcDir, dstPath string) error {
    out, err := os.Create(dstPath)
    if err != nil {
        return err
    }
    defer out.Close()
    gz := gzip.NewWriter(out)
    defer gz.Close()
    tw := tar.NewWriter(gz)
    defer tw.Close()

    return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if info.IsDir() {
            return nil
        }
        rel, err := filepath.Rel(filepath.Dir(srcDir), path)
        if err != nil {
            return err
        }
        hdr, err := tar.FileInfoHeader(info, "")
        if err != nil {
            return err
        }
        hdr.Name = rel
        if err := tw.WriteHeader(hdr); err != nil {
            return err
        }
        f, err := os.Open(path)
        if err != nil {
            return err
        }
        defer f.Close()
        if _, err := io.Copy(tw, f); err != nil {
            return err
        }
        return nil
    })
}

func loadQueriesFromJSON(path string) ([]QueryDef, error) {
    b, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var q []QueryDef
    if err := json.Unmarshal(b, &q); err != nil {
        return nil, err
    }
    if len(q) == 0 {
        return nil, errors.New("empty queries file")
    }
    // Basic validation
    for i := range q {
        if q[i].Name == "" || q[i].File == "" || q[i].KQL == "" {
            return nil, fmt.Errorf("query[%d] missing name/file/kql", i)
        }
    }
    return q, nil
}

func getenv(key, def string) string {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    return v
}
