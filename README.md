kubectl-must-gather (ADX)

What this adds
- A small Go CLI that connects to Azure Data Explorer (Kusto) and gathers a curated set of ADX‑Mon metrics/logs for a given Cluster and time window.
- Outputs a must-gather style folder with NDJSON files, plus a .tar.gz archive for easy sharing.

Quick start
- Prereq: `az login` (DefaultAzureCredential is used).
- Env/flags:
  - `KUSTO_CLUSTER` or `--cluster-uri`: e.g. `https://<cluster>.<region>.kusto.windows.net`
  - `KUSTO_DATABASE` (default `Metrics`)
  - `ADXMON_CLUSTER` or `--cluster`: the Cluster label value used by ADX‑Mon ingestion

Setup (ADX‑Mon Quick Start)
- This repo focuses only on querying Kusto and producing a must‑gather artifact. It does not include deployment manifests for data collection or Grafana dashboards.
- To set up the ingestion pipeline (collector → ingestor → ADX) and optional dashboards, follow the ADX‑Mon quick start:
  - https://azure.github.io/adx-mon/quick-start/
  - The quick start provisions the Metrics and Logs databases and installs helper functions such as `prom_rate()` and `prom_delta()` that the queries here rely on.

Build
```
go build ./cmd/kusto-must-gather
```

Examples
```
export KUSTO_CLUSTER="https://<cluster>.eastus.kusto.windows.net"
export KUSTO_DATABASE="Metrics"
export ADXMON_CLUSTER="my-aks-cluster"

# Gather last 1h (default) and create must-gather-<timestamp>.tar.gz
./kusto-must-gather

# Gather last 30m into a custom folder (no tar)
./kusto-must-gather --since=30m --out=mg-quick --no-tar

# Specify explicit time range and 5m buckets
./kusto-must-gather --start=2025-09-05T12:00:00Z --end=2025-09-05T14:00:00Z --bucket=5m
```

What it collects (built-in)
- Metrics database:
  - Node capacity: CPU, memory (KubeNodeStatusCapacity)
  - Node allocatable: CPU, memory (KubeNodeStatusAllocatable)
  - API server request volume by status code (ApiserverRequestTotal via prom_delta)
  - Top API server resources by rate (prom_rate)
  - API server latency histogram buckets (ApiserverRequestDurationSecondsBucket via prom_delta)
  - API server average latency overall and by verb (Sum/Count via prom_delta)
  - Container CPU usage aggregated by namespace (ContainerCpuUsageSecondsTotal via prom_rate)
  - Container memory working set aggregated by namespace/pod (ContainerMemoryWorkingSetBytes)
  - Pod restarts timeseries and top restarts in window (KubePodContainerStatusRestartsTotal via prom_delta)
  - Pod phase counts over time (KubePodStatusPhase)
- Logs database:
  - Kubelet logs sample (up to 20k rows)
  - OOM-related Kubelet messages (filtered by substring)
- Node state:
  - Node conditions per node over time (KubeNodeStatusCondition)
  - NotReady node counts over time
  - Node info latest snapshot (KubeNodeInfo)

Output layout
```
must-gather-YYYYmmdd-HHMMSS/
  manifest.json        # run context + per-query status
  meta.json            # basic context (start/end/cluster)
  metrics/*.ndjson
  apiserver/*.ndjson
  workload/*.ndjson
  inventory/*.ndjson
  logs/*.ndjson
```

Auth
- Uses `WithDefaultAzureCredential()`: works with `az login`, managed identity, or service principal env vars.

Custom queries
- Provide a JSON file via `--queries` with entries:
```
[
  {
    "name": "custom-name",
    "database": "Metrics",
    "file": "custom/out.ndjson",
    "kql": "MyTable | where $__timeFilter(Timestamp) | where Cluster == $Cluster | take 1000",
    "maxRows": 1000
  }
]
```
- Supported placeholders:
  - `$Cluster` → quoted literal
  - `$__timeFilter(Timestamp)` → `Timestamp between (datetime(start)..datetime(end))`
  - `$__timeInterval` → the chosen bucket (e.g., `5m`)

Notes
- Queries assume ADX‑Mon functions like `prom_rate()`/`prom_delta()` are present (as installed by the quick start).
- Outputs NDJSON (one JSON object per line) for easy `jq` processing.
