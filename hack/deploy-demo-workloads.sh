#!/bin/bash
set -e

echo "🚀 Deploying demo workloads for AI mode testing..."

# Create demo namespace
echo "📦 Creating ai-test namespace..."
kubectl create namespace ai-test --dry-run=client -o yaml | kubectl apply -f -

# Deploy a working nginx pod
echo "✅ Deploying test-nginx (working pod)..."
kubectl delete pod test-nginx -n ai-test --ignore-not-found=true
kubectl run test-nginx --image=nginx --namespace=ai-test

# Deploy a failing pod (invalid image)
echo "❌ Deploying failing-pod (broken pod for testing)..."
kubectl delete pod failing-pod -n ai-test --ignore-not-found=true
kubectl run failing-pod --image=nonexistent:invalid --namespace=ai-test

# Deploy a crashing workload that logs before failing
echo "💥 Deploying crash-simulator (logs then crashes)..."
kubectl delete pod crash-simulator -n ai-test --ignore-not-found=true
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: crash-simulator
  namespace: ai-test
  labels:
    app: crash-simulator
spec:
  containers:
  - name: crash-simulator
    image: busybox
    command: ["/bin/sh"]
    args:
    - -c
    - |
      echo "🚀 Application starting up..."
      echo "📊 Initializing components..."
      echo "🔗 Connecting to database..."
      echo "✅ Database connection established"
      echo "🌐 Starting web server on port 8080..."
      echo "📡 Health check endpoint ready"
      echo "🎯 Application ready to serve traffic"
      sleep 10
      echo "⚠️  High memory usage detected: 85%"
      echo "⚠️  Memory usage increasing: 92%"
      echo "🔥 CRITICAL: Memory usage at 98%"
      echo "💀 ERROR: Out of memory - application will terminate"
      echo "💥 FATAL: Segmentation fault in memory allocator"
      exit 1
    resources:
      requests:
        memory: "64Mi"
        cpu: "100m"
      limits:
        memory: "128Mi"
        cpu: "200m"
  restartPolicy: Always
EOF

# Wait a moment for pods to be created
sleep 5

# Show current pod status
echo "📋 Current pod status in ai-test namespace:"
kubectl get pods -n ai-test

echo ""
echo "🎯 Demo workloads deployed! You can now test AI mode with queries like:"
echo ""
echo "Pod Status & Inventory:"
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"Show me all pods in ai-test namespace\""
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"Why is my failing-pod not running?\""
echo ""
echo "Crash Analysis & Logs:"
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"Why did crash-simulator keep restarting?\""
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"Show me error logs from crash-simulator\""
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"What caused the crash in ai-test namespace?\""
echo ""
echo "Events & Troubleshooting:"
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"Show me kubernetes events for ai-test namespace\""
echo "  ./aks-must-gather --workspace-id \"\$WID\" --ai-mode \"What issues are happening in ai-test namespace?\""
echo ""
echo "💡 Note: It may take a few minutes for data to appear in Log Analytics workspace."
echo "🔄 The crash-simulator will restart automatically - wait ~30 seconds and try the crash analysis queries!"