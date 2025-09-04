#!/usr/bin/env bash
#
# Minimal setup for AKS + Log Analytics with Azure Monitor for containers enabled.
# Collects AKS container logs into the Log Analytics workspace.
#
# Requirements: Azure CLI (az), logged in (az login), subscription set (az account set)
#

set -Eeuo pipefail
trap 'echo "❌ Error on line $LINENO. Exiting." >&2' ERR

# -------------------------
# Config
# -------------------------
RANDOM_SUFFIX=$(openssl rand -hex 3)

RESOURCE_GROUP="harpatil-aks-law-rg-${RANDOM_SUFFIX}"
LOCATION="eastus"
AKS_CLUSTER_NAME="harpatil-aks-cluster-${RANDOM_SUFFIX}"
LOG_ANALYTICS_WORKSPACE_NAME="harpatil-law-${RANDOM_SUFFIX}"

# -------------------------
# (Optional) Provider registrations (safe if already registered)
# -------------------------
az provider register --namespace Microsoft.ContainerService >/dev/null 2>&1 || true
az provider register --namespace Microsoft.OperationalInsights >/dev/null 2>&1 || true

# -------------------------
# Resource group
# -------------------------
echo "--- Creating Resource Group: $RESOURCE_GROUP in $LOCATION ---"
az group create --name "$RESOURCE_GROUP" --location "$LOCATION" >/dev/null

# -------------------------
# Log Analytics workspace
# -------------------------
echo "--- Creating Log Analytics Workspace: $LOG_ANALYTICS_WORKSPACE_NAME ---"
az monitor log-analytics workspace create \
  --resource-group "$RESOURCE_GROUP" \
  --workspace-name "$LOG_ANALYTICS_WORKSPACE_NAME" \
  --location "$LOCATION" >/dev/null

# Capture both the ARM resource ID (for linking) and the workspace GUID (for queries)
LOG_ANALYTICS_WORKSPACE_RESOURCE_ID=$(az monitor log-analytics workspace show \
  --resource-group "$RESOURCE_GROUP" \
  --workspace-name "$LOG_ANALYTICS_WORKSPACE_NAME" \
  --query id -o tsv)

LOG_ANALYTICS_WORKSPACE_GUID=$(az monitor log-analytics workspace show \
  --resource-group "$RESOURCE_GROUP" \
  --workspace-name "$LOG_ANALYTICS_WORKSPACE_NAME" \
  --query customerId -o tsv)

# -------------------------
# AKS cluster (Azure Monitor enabled, linked to the workspace)
# -------------------------
echo "--- Creating AKS Cluster: $AKS_CLUSTER_NAME ---"
az aks create \
  --resource-group "$RESOURCE_GROUP" \
  --name "$AKS_CLUSTER_NAME" \
  --location "$LOCATION" \
  --node-count 1 \
  --enable-addons monitoring \
  --workspace-resource-id "$LOG_ANALYTICS_WORKSPACE_RESOURCE_ID" \
  --generate-ssh-keys \
  >/dev/null

# Wait explicitly (no-op if already DONE)
az aks wait -g "$RESOURCE_GROUP" -n "$AKS_CLUSTER_NAME" --created

# Get kubeconfig for quick validations
az aks get-credentials -g "$RESOURCE_GROUP" -n "$AKS_CLUSTER_NAME" --overwrite-existing >/dev/null

# -------------------------
# Verify ContainerLog table appears in LA (agent warm-up)
# -------------------------
echo "--- Verifying creation of Container log table in Log Analytics Workspace ---"
TABLE_CHECK_TIMEOUT_SECONDS=900 # 15 minutes
CHECK_INTERVAL_SECONDS=30
SECONDS_WAITED=0

while true; do
  # Newer AKS/Container Insights deployments use ContainerLogV2; older use ContainerLog.
  if az monitor log-analytics query \
      --workspace "$LOG_ANALYTICS_WORKSPACE_GUID" \
      --analytics-query "ContainerLogV2 | take 1" \
      --output none >/dev/null 2>&1; then
    echo "✅ 'ContainerLogV2' table found in Log Analytics Workspace."
    break
  fi

  if az monitor log-analytics query \
      --workspace "$LOG_ANALYTICS_WORKSPACE_GUID" \
      --analytics-query "ContainerLog | take 1" \
      --output none >/dev/null 2>&1; then
    echo "✅ 'ContainerLog' table found in Log Analytics Workspace."
    break
  fi
  if [ $SECONDS_WAITED -ge $TABLE_CHECK_TIMEOUT_SECONDS ]; then
    echo "❌ Timed out waiting for Container log table to be created in Log Analytics."
    echo "   Check the AKS monitoring addon status on cluster '$AKS_CLUSTER_NAME'."
    exit 1
  fi
  echo "Table not yet found. Waiting ${CHECK_INTERVAL_SECONDS}s before retrying... (${SECONDS_WAITED}/${TABLE_CHECK_TIMEOUT_SECONDS}s)"
  sleep $CHECK_INTERVAL_SECONDS
  SECONDS_WAITED=$((SECONDS_WAITED + CHECK_INTERVAL_SECONDS))

done

# -------------------------
# Done
# -------------------------
echo "--- ✅ Setup Complete! ---"
echo "AKS cluster '$AKS_CLUSTER_NAME' is linked to Log Analytics workspace '$LOG_ANALYTICS_WORKSPACE_NAME'."
echo "Container logs should begin flowing into 'ContainerLogV2' (or 'ContainerLog')."
