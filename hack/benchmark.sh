#!/usr/bin/env bash
# Benchmark script for Clawbernetes operator scalability.
# Usage: ./hack/benchmark.sh [num_agents]
#
# This script:
# 1. Cleans up any existing benchmark agents
# 2. Installs CRDs
# 3. Creates N benchmark agents
# 4. Starts the operator, measures reconcile times from logs
# 5. Updates an agent and measures re-reconcile time
# 6. Hits API endpoints and measures response times
# 7. Cleans up

set -euo pipefail

NUM_AGENTS=${1:-50}
NS="clawbernetes"
OPERATOR_PID=""
LOG_FILE="/tmp/clawbernetes-bench-$(date +%s).log"
RESULTS_FILE="/tmp/clawbernetes-bench-results.txt"

cleanup() {
    echo "--- Cleaning up ---"
    if [[ -n "$OPERATOR_PID" ]] && kill -0 "$OPERATOR_PID" 2>/dev/null; then
        kill "$OPERATOR_PID" 2>/dev/null || true
        wait "$OPERATOR_PID" 2>/dev/null || true
    fi
    # Delete benchmark agents
    kubectl delete clawagents -n "$NS" -l benchmark=true --ignore-not-found=true 2>/dev/null || true
    # Delete benchmark configmaps
    kubectl delete configmaps -n "$NS" -l app.kubernetes.io/managed-by=clawbernetes,benchmark=true --ignore-not-found=true 2>/dev/null || true
}
trap cleanup EXIT

echo "=== Clawbernetes Operator Benchmark ==="
echo "Agents: $NUM_AGENTS"
echo "Log: $LOG_FILE"
echo "Results: $RESULTS_FILE"
echo ""

# Ensure namespace exists
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - 2>/dev/null

# Install CRDs
echo "--- Installing CRDs ---"
make install 2>&1 | tail -2

# Clean any leftover benchmark agents
kubectl delete clawagents -n "$NS" -l benchmark=true --ignore-not-found=true 2>/dev/null || true
sleep 1

# Create N benchmark agents
echo "--- Creating $NUM_AGENTS benchmark agents ---"
CREATE_START=$(date +%s%N)
for i in $(seq 1 "$NUM_AGENTS"); do
    cat <<YAML | kubectl apply -f - 2>/dev/null
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: bench-agent-$(printf "%04d" "$i")
  namespace: $NS
  labels:
    benchmark: "true"
spec:
  identity:
    soul: "Benchmark agent $i for scalability testing."
  model:
    provider: anthropic
    name: claude-haiku-4-5
  workspace:
    mode: ephemeral
YAML
done
CREATE_END=$(date +%s%N)
CREATE_MS=$(( (CREATE_END - CREATE_START) / 1000000 ))
echo "Agent creation: ${CREATE_MS}ms"

# Verify agents exist
ACTUAL=$(kubectl get clawagents -n "$NS" -l benchmark=true --no-headers 2>/dev/null | wc -l)
echo "Agents created: $ACTUAL"

# Start the operator and capture logs
echo "--- Starting operator ---"
go run ./cmd/main.go 2>&1 | tee "$LOG_FILE" &
OPERATOR_PID=$!

# Wait for the operator to start and reconcile all agents
echo "--- Waiting for reconciliation ---"
RECONCILE_START=$(date +%s%N)

# Poll until all benchmark agents have been reconciled (have a phase set)
MAX_WAIT=120
WAITED=0
while true; do
    RECONCILED=$(kubectl get clawagents -n "$NS" -l benchmark=true -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null | grep -c "." || true)
    if [[ "$RECONCILED" -ge "$NUM_AGENTS" ]]; then
        break
    fi
    if [[ "$WAITED" -ge "$MAX_WAIT" ]]; then
        echo "TIMEOUT: only $RECONCILED/$NUM_AGENTS reconciled after ${MAX_WAIT}s"
        break
    fi
    sleep 1
    WAITED=$((WAITED + 1))
done

RECONCILE_END=$(date +%s%N)
RECONCILE_MS=$(( (RECONCILE_END - RECONCILE_START) / 1000000 ))
echo "Full fleet reconciliation ($ACTUAL agents): ${RECONCILE_MS}ms"

# Count reconcile log lines for per-agent timing
RECONCILE_COUNT=$(grep -c "reconciled ClawAgent" "$LOG_FILE" 2>/dev/null || echo 0)
echo "Reconcile log entries: $RECONCILE_COUNT"

# Extract individual reconcile times from logs if available
echo ""
echo "--- Per-agent reconcile latency (from log timestamps) ---"
# Get first and last reconcile timestamps
FIRST_RECONCILE=$(grep "reconciled ClawAgent" "$LOG_FILE" | head -1 | grep -oP '\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+' || echo "N/A")
LAST_RECONCILE=$(grep "reconciled ClawAgent" "$LOG_FILE" | tail -1 | grep -oP '\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+' || echo "N/A")
echo "First reconcile: $FIRST_RECONCILE"
echo "Last reconcile:  $LAST_RECONCILE"

# Measure: update one agent and time the re-reconcile
echo ""
echo "--- Single agent update benchmark ---"
# Clear log for this measurement
MARK_LINE=$(wc -l < "$LOG_FILE")

UPDATE_START=$(date +%s%N)
kubectl patch clawagent bench-agent-0001 -n "$NS" --type merge -p '{"spec":{"identity":{"soul":"Updated soul for benchmark"}}}' 2>/dev/null
# Wait for re-reconcile
for i in $(seq 1 30); do
    if tail -n +$((MARK_LINE + 1)) "$LOG_FILE" | grep -q "reconciled ClawAgent.*bench-agent-0001"; then
        break
    fi
    sleep 0.2
done
UPDATE_END=$(date +%s%N)
UPDATE_MS=$(( (UPDATE_END - UPDATE_START) / 1000000 ))
echo "Single agent update + reconcile: ${UPDATE_MS}ms"

# API endpoint benchmarks
echo ""
echo "--- API endpoint benchmarks ---"

# /api/agents
AGENTS_START=$(date +%s%N)
AGENTS_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/agents 2>/dev/null || echo "ERR")
AGENTS_END=$(date +%s%N)
AGENTS_MS=$(( (AGENTS_END - AGENTS_START) / 1000000 ))
echo "/api/agents:   ${AGENTS_MS}ms (status: $AGENTS_STATUS)"

# /api/summary
SUMMARY_START=$(date +%s%N)
SUMMARY_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/summary 2>/dev/null || echo "ERR")
SUMMARY_END=$(date +%s%N)
SUMMARY_MS=$(( (SUMMARY_END - SUMMARY_START) / 1000000 ))
echo "/api/summary:  ${SUMMARY_MS}ms (status: $SUMMARY_STATUS)"

# /api/activity
ACTIVITY_START=$(date +%s%N)
ACTIVITY_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/activity 2>/dev/null || echo "ERR")
ACTIVITY_END=$(date +%s%N)
ACTIVITY_MS=$(( (ACTIVITY_END - ACTIVITY_START) / 1000000 ))
echo "/api/activity: ${ACTIVITY_MS}ms (status: $ACTIVITY_STATUS)"

# /api/health
HEALTH_START=$(date +%s%N)
HEALTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/health 2>/dev/null || echo "ERR")
HEALTH_END=$(date +%s%N)
HEALTH_MS=$(( (HEALTH_END - HEALTH_START) / 1000000 ))
echo "/api/health:   ${HEALTH_MS}ms (status: $HEALTH_STATUS)"

# Write results to file
cat > "$RESULTS_FILE" <<RESULTS
=== Clawbernetes Benchmark Results ===
Branch: $(git rev-parse --abbrev-ref HEAD)
Commit: $(git rev-parse --short HEAD)
Date:   $(date -Iseconds)
Agents: $NUM_AGENTS

--- Timings ---
Agent creation (kubectl):         ${CREATE_MS}ms
Full fleet reconciliation:        ${RECONCILE_MS}ms
Reconcile log entries:            $RECONCILE_COUNT
Single agent update + reconcile:  ${UPDATE_MS}ms

--- API Endpoints ---
/api/agents:   ${AGENTS_MS}ms (status: $AGENTS_STATUS)
/api/summary:  ${SUMMARY_MS}ms (status: $SUMMARY_STATUS)
/api/activity: ${ACTIVITY_MS}ms (status: $ACTIVITY_STATUS)
/api/health:   ${HEALTH_MS}ms (status: $HEALTH_STATUS)

--- Log Timestamps ---
First reconcile: $FIRST_RECONCILE
Last reconcile:  $LAST_RECONCILE
RESULTS

echo ""
echo "--- Results saved to $RESULTS_FILE ---"
cat "$RESULTS_FILE"
