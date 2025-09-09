# KubeNodeInventory Table Schema

Source: https://learn.microsoft.com/en-us/azure/azure-monitor/reference/tables/kubenodeinventory

## Description
Kubernetes node inventory information including versions and status.

## Key Columns

### Identification
- **ClusterId** (string): Kubernetes cluster ID
- **ClusterName** (string): Kubernetes cluster name
- **Computer** (string): Computer/node name in cluster
- **KubernetesProviderID** (string): Provider ID for Kubernetes

### Versions
- **DockerVersion** (string): Container runtime version
- **KubeletVersion** (string): Kubelet version
- **KubeProxyVersion** (string): KubeProxy version

### Status & Timing
- **TimeGenerated** (datetime): Record creation timestamp
- **CreationTimeStamp** (datetime): Node creation time
- **Status** (string): Node status conditions
- **LastTransitionTimeReady** (datetime): Last node ready condition transition

### Configuration
- **OperatingSystem** (string): Node's host OS image
- **Labels** (string): Kubernetes node labels

### System
- **SourceSystem** (string): Agent collection type
- **_ResourceId** (string): Unique resource identifier
- **_SubscriptionId** (string): Subscription identifier

## Common KQL Examples
```kql
// Get node status overview
KubeNodeInventory
| project TimeGenerated, Computer, Status, KubeletVersion, OperatingSystem

// Find nodes with issues
KubeNodeInventory
| where Status !contains "Ready"
| project TimeGenerated, Computer, Status
```