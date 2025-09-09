## AKS Must-Gather (Log Analytics)

### Overview
- Exports AKS diagnostics from Azure Monitor Log Analytics into a tar.gz, similar to OpenShift must-gather.
- Queries selected tables over a time window, writes per‑table NDJSON parts and schemas, plus summary metadata.
- Focus areas: Kubernetes logs, pod/container logs, state/inventory, metrics, and optional control‑plane/audit logs.
- Requires only the Log Analytics workspace ARM resource ID (`--workspace-id`). The tool resolves other details (GUID, schema) automatically.
- **AI-powered mode** - Use natural language queries to generate KQL and get targeted results without tar files.  

### Prerequisites
- Azure CLI logged in (`az login`) with access to the target workspace/cluster.
- Go 1.22+ to build locally.

### Quick Start
1) Create AKS + Log Analytics (optional): `./hack/create-ask.sh`
2) Build: `go build -o ./bin/aks-must-gather ./cmd/aks-must-gather`
3) list and pick one workspace name:
     - In a resource group: `az monitor log-analytics workspace list -g <rg> --query "[].{name:name,id:id}" -o table`
     - In the subscription: `az monitor log-analytics workspace list --query "[].{name:name,resourceGroup:resourceGroup,id:id}" -o table`
4) Get the workspace ARM resource ID with az (replace values):
   - `RG=<your-resource-group>`
   - `LAW=<your-workspace-name>`
   - `WID=$(az monitor log-analytics workspace show -g "$RG" -n "$LAW" --query id -o tsv)`
5) Run the capture (recommended profile: aks-debug):
   - `./bin/aks-must-gather --workspace-id "$WID" --timespan PT15M --profiles aks-debug --out ./must-gather.tar.gz`

## AI-Powered Query Mode (Experimental)

The tool includes an experimental AI-powered mode that lets you ask natural language questions about your AKS cluster. Instead of generating tar files, it creates KQL queries from your questions and provides intelligent analysis of the results.

**Prerequisites for AI mode**: Claude CLI installed and authenticated (`claude` command available in PATH).

### How It Works
1. **Natural Language Input**: Ask questions in plain English
2. **KQL Generation**: Claude generates precise KQL queries using current table schemas
3. **Query Execution**: Runs the query against your Log Analytics workspace  
4. **AI Analysis**: Claude analyzes the results and provides insights
5. **Persistent Results**: Saves all data in timestamped directories for manual inspection

### Quick Start with AI Mode
```bash
# Deploy demo workloads (creates test-nginx and failing-pod)
./hack/deploy-demo-workloads.sh

# Wait a few minutes for data to appear in Log Analytics, then:
./bin/aks-must-gather --workspace-id "$WID" --ai-mode "Show me all pods in ai-test namespace"
./bin/aks-must-gather --workspace-id "$WID" --ai-mode "Why is my failing-pod not running?"
```

### Example Natural Language Queries
- `"Show me all pods in kube-system namespace"`
- `"Why are my pods failing?"`
- `"Show me container logs with errors from the last hour"`
- `"What nodes are having issues?"`
- `"Show me kubernetes events for failed pods"`
- `"Which containers have high restart counts?"`

### AI Mode Output
- **KQL Query**: Shows the generated query for transparency
- **AI Analysis**: Human-readable insights and recommendations
- **Persistent Data**: Results saved in `ai-results-YYYYMMDD-HHMMSS/` directories
- **Raw Data Access**: Full query results available for manual analysis

### Usage (Flags)
- `--workspace-id`: Log Analytics workspace ARM resource ID (required). The tool discovers the workspace GUID automatically.
- `--timespan`: ISO‑8601 (e.g., `PT30M`, `PT2H`, `P1D`) or Go style (`30m`, `2h`).
- `--ai-mode`: Enable AI-powered query mode. Prompts for natural language query and presents results directly (no tar file).
- `--profiles`: Comma‑separated profiles (see below). Supports alias `aks-debug` (podLogs+inventory+metrics). Defaults to that union if omitted.
- `--tables`: Comma‑separated table list. Overrides `--profiles`.
- `--all-tables`: Export every table in the workspace (can be slow). Overrides profiles/tables.
- `--out`: Output tar.gz path (defaults to `must-gather-<timestamp>.tar.gz`).
- `--stitch-logs`: Also include time‑ordered logs per namespace/pod/container under `namespaces/` (default true).
- `--stitch-include-events`: Include `KubeEvents` under `namespaces/<ns>/events/events.log` (default true).

### Profiles
- aks-debug (alias: podLogs + inventory + metrics)
  - Tables: union of the three profiles below
  - Why: Good default for debugging most AKS issues

- podLogs (container and K8s logs)
  - Tables: `ContainerLogV2`, `ContainerLog` (legacy), `KubeEvents`, `KubeMonAgentEvents`, `Syslog` (if collected via DCR)
  - Why: App stdout/stderr, cluster event stream, agent events, node syslog when enabled.

- inventory (state and inventory)
  - Tables: `KubePodInventory`, `KubeNodeInventory`, `KubeServices`, `KubePVInventory`, `ContainerInventory`, `ContainerImageInventory`, `ContainerNodeInventory`, `KubeHealth`
  - Why: Reconstruct pod/node/service/PV state and image topology.

- metrics (metrics and agent heartbeat)
  - Tables: `InsightsMetrics`, `Perf`, `Heartbeat`
  - Why: Kube‑state metrics (e.g., replicas ready), CPU/memory counters, agent/node liveness.

- audit (control‑plane/audit; requires AKS Diagnostic Settings)
  - Tables: `AKSControlPlane`, `AKSAudit`, `AKSAuditAdmin`
  - Enablement: https://learn.microsoft.com/azure/aks/monitor-aks#enable-resource-logs

### Table References
- Container Insights tables & queries: https://learn.microsoft.com/azure/azure-monitor/containers/container-insights-log-search
- Container Insights overview: https://learn.microsoft.com/azure/azure-monitor/containers/container-insights-overview
- AKS monitoring overview: https://learn.microsoft.com/azure/aks/monitor-aks
- Azure Monitor resource logs: https://learn.microsoft.com/azure/azure-monitor/essentials/resource-logs

### Performance Tips
- Narrow the timespan (e.g., `PT30M` to `PT1H`) for quicker captures.
- Use `--profiles` (recommended) instead of `--all-tables` (workspaces often have 600+ tables).
- The tool writes per‑time‑chunk NDJSON (15m chunks for ≤2h windows, else hourly) to keep memory stable.

### Limitations and Notes
- `ContainerLogV2` is the primary container log table on modern clusters; `ContainerLog` may be empty.
- `Syslog` in AKS via Container Insights is not enabled by default. The Syslog Data Collection Rule (DCR) for VMs/VMSS does not apply to the AKS AMA DaemonSet; a custom approach is required to ingest node syslog. If not configured, this table will be empty.
- Control‑plane/audit tables populate only if AKS Diagnostic Settings are configured to send those categories to Log Analytics.
- Schema export requires `--workspace-id` (management plane). The tool resolves the workspace GUID automatically for queries.

### Artifact Layout
- `metadata/workspace.json`: workspace GUID/ID, timespan, count of tables.
- `metadata/azure.json`: subscription, resource group, workspace name (when `--workspace-id` provided).
- `tables/<Table>/schema.json`: Log Analytics schema (management plane).
- `tables/<Table>/parts/<chunk>.ndjson`: Per‑chunk rows in NDJSON.
- `tables/<Table>/summary.json`: Per‑table row count and duration.
- `namespaces/<namespace>/pods/<pod>/<container>.log`: Stitched, time‑ordered container logs from `ContainerLogV2`.
- `namespaces/<namespace>/events/events.log`: Cluster events (when `--stitch-include-events=true`).
- `index.json`: List of exported tables.

### Examples

#### Traditional tar.gz export:
- Pod/container logs only (30 minutes):
  `./bin/aks-must-gather --workspace-id <ARM ID> --timespan PT30M --profiles podLogs --out logs.tar.gz`

- Inventory + metrics (1 hour):
  `./bin/aks-must-gather --workspace-id <ARM ID> --timespan PT1H --profiles inventory,metrics --out inv-metrics.tar.gz`

- All tables (use with care):
  `./bin/aks-must-gather --workspace-id <ARM ID> --timespan PT30M --all-tables --out all.tar.gz`

- Default AKS debug (alias):
  `./bin/aks-must-gather --workspace-id <ARM ID> --timespan PT30M --profiles aks-debug --out aks-debug.tar.gz`

#### AI-powered mode:
- Natural language queries with direct input:
  ```bash
  ./bin/aks-must-gather --workspace-id <ARM ID> --ai-mode "Show me all pods in ai-test namespace"
  ./bin/aks-must-gather --workspace-id <ARM ID> --ai-mode "Why is my failing-pod not running?"
  ./bin/aks-must-gather --workspace-id <ARM ID> --ai-mode "Show me kubernetes events for failed pods"
  ```

- **Deploy demo workloads for testing:**
  ```bash
  ./hack/deploy-demo-workloads.sh
  ```

- **Sample AI mode session:** [View complete troubleshooting session](https://gist.github.com/harche/9d8bd277973565effbfaefc8d88d37ce) - Shows AI-powered analysis of a crash simulator pod with OOM errors, including KQL generation, validation, and actionable recommendations.

### Notes
- `ContainerLogV2` is the primary container log table on modern clusters; `ContainerLog` may be empty.
- `Syslog` appears only if your Data Collection Rule (DCR) collects it for AKS nodes.
- Control‑plane/audit tables populate only if AKS Diagnostic Settings are configured to send those categories to Log Analytics.
- The tool writes per‑time‑chunk NDJSON parts to keep memory stable and performance predictable on large workspaces.
