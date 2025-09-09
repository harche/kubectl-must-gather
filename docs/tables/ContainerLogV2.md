# ContainerLogV2 Table Schema

Source: https://learn.microsoft.com/en-us/azure/azure-monitor/reference/tables/containerlogv2

## Description
Container logs from stdout/stderr. Successor to ContainerLog with enhanced capabilities.

## Key Columns

### Identification
- **Computer** (string): Name of the Computer/Node generating the log
- **ContainerId** (string): Container ID of the log source
- **ContainerName** (string): Name of the Container generating the log
- **PodName** (string): Kubernetes Pod name
- **PodNamespace** (string): Kubernetes Namespace

### Log Details
- **TimeGenerated** (datetime): Log generation timestamp
- **LogMessage** (dynamic): Log message from stdout/stderr
- **LogLevel** (string): Log severity (CRITICAL, ERROR, WARNING, INFO, DEBUG, TRACE, UNKNOWN)
- **LogSource** (string): Source of log message (stdout/stderr)

### Metadata
- **KubernetesMetadata** (dynamic): Kubernetes metadata including pod details
- **SourceSystem** (string): Agent type collecting the event
- **_ResourceId** (string): Unique resource identifier
- **_SubscriptionId** (string): Subscription identifier
- **TenantId** (string): Log Analytics workspace ID

## Key Features
- Supports container log lines up to 64 KB
- Supports .NET and Go stack traces
- Kubernetes-specific log schema

## Common KQL Examples
```kql
// Get logs from specific pod
ContainerLogV2
| where PodName == "my-pod" and PodNamespace == "default"
| project TimeGenerated, LogMessage, LogLevel

// Find error logs
ContainerLogV2
| where LogLevel == "ERROR"
| project TimeGenerated, PodNamespace, PodName, LogMessage
```