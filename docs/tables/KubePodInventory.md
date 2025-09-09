# KubePodInventory Table Schema

Source: https://learn.microsoft.com/en-us/azure/azure-monitor/reference/tables/kubepodinventory

## Description
Kubernetes pod and container inventory information for monitoring and analysis.

## Key Columns

### Identification
- **ClusterId** (string): Kubernetes cluster ID
- **ClusterName** (string): Kubernetes cluster name  
- **Name** (string): Kubernetes Pod Name
- **Namespace** (string): Kubernetes Namespace
- **PodUid** (string): Unique Pod Identifier

### Metadata
- **TimeGenerated** (datetime): Record creation timestamp
- **ContainerName** (string): Container name
- **PodStatus** (string): Last observed Pod Status
- **PodCreationTimeStamp** (datetime): Pod creation time

### Performance/State
- **PodRestartCount** (int): Total pod restart count
- **ContainerRestartCount** (int): Container restart count
- **ContainerStatus** (string): Container's last observed current state

### Networking
- **PodIp** (string): Pod's IP address
- **Computer** (string): Node name in cluster
- **ServiceName** (string): Kubernetes service association

## Common KQL Examples
```kql
// Get all pods in a specific namespace
KubePodInventory
| where Namespace == "kube-system"
| project TimeGenerated, Name, PodStatus, Computer

// Find failed pods
KubePodInventory  
| where PodStatus == "Failed"
| project TimeGenerated, Namespace, Name, PodStatus
```