# Clawbernetes

https://clawbernetes.org/

**Kubernetes-native orchestration for autonomous AI agent fleets. For founders who want to go off-cloud.**

Clawbernetes turns AI agent deployment into a `kubectl apply` workflow. Declare your agents, policies, channels, and observability stack as Custom Resources -- the operator handles the rest.

## What It Does

One YAML, full agent:

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: eng-agent
  namespace: clawbernetes
spec:
  identity:
    soul: "You are a senior software engineer..."
  model:
    provider: openai
    name: gpt-4.1
  channels: [eng-telegram]
  workspace:
    mode: persistent
    storageSize: "10Gi"
  a2a:
    enabled: true
    skills: [kubernetes, debugging]
```

`kubectl apply` and the operator:
1. Creates a Pod running [OpenClaw](https://github.com/orq-ai/openclaw) with identity files (SOUL.md, USER.md, IDENTITY.md)
2. Installs [observeclaw](https://github.com/ai-trust-layer/observeclaw) for budget enforcement, tool policy, anomaly detection, and routing
3. Connects to delivery channels (Telegram, Slack, Discord) via [ClawChannel](#clawchannel) CRDs
4. Enables agent-to-agent communication via the [A2A v0.3.0 protocol](#a2a-agent-to-agent)
5. Persists agent state (memory, cron, delivery queue) across restarts via PVC
6. Routes LLM traffic through a centralized ClawGateway proxy
7. Exports traces to Tempo via OpenTelemetry, visualizes in Grafana

## Architecture

```
                       kubectl apply
                            |
                    +-------v--------+
                    |   Operator     |
                    | (Go, ctrl-rt)  |
                    +---+---+---+----+
                        |   |   |
          +-------------+   |   +-------------+
          |                 |                 |
    ClawAgent          ClawGateway      ClawObservability
    (per-agent pod)    (FastAPI proxy)  (Tempo + Grafana)
          |                 |
    +-----+------+         |
    | Plugins:   |         |
    | observeclaw|   LLM providers
    | a2a-gateway|   (Anthropic, OpenAI, etc.)
    | telegram   |         |
    | slack      |         |
    +------------+         |
          |                |
    PVC (persistent     api.anthropic.com
     agent state)       api.openai.com
```

## CRDs

| CRD | Purpose |
|-----|---------|
| **ClawAgent** | Declares an autonomous agent with identity, model, channels, A2A, workspace, and lifecycle |
| **ClawChannel** | Delivery channel integration (Telegram, Slack, Discord) with credential management |
| **ClawGateway** | Centralized LLM proxy with routing evaluators and PII redaction |
| **ClawPolicy** | Budget limits (daily/monthly), tool allow/deny lists, model downgrade thresholds |
| **ClawSkillSet** | Reusable skill packages (SKILL.md files) mounted into agent workspaces |
| **ClawObservability** | Deploys Tempo + Grafana for distributed tracing and fleet visualization |

## Quick Start

```bash
# Prerequisites: Docker, kind, kubectl, Go 1.25+

# 1. Create cluster + build images + deploy everything
make demo-up

# 2. Access the fleet
kubectl port-forward svc/agent-proxy 8080:80 -n clawbernetes
# http://localhost:8080 -- Fleet dashboard
```

Skip image rebuild on subsequent runs: `make demo-up SKIP_IMAGE=1`

## Deploy Your Own Fleet

### Step 1: Create secrets

```bash
# LLM provider API key
kubectl create secret generic openclaw-api-keys \
  --from-literal=OPENAI_API_KEY=sk-your-key \
  -n clawbernetes

# Telegram bot token (optional)
kubectl create secret generic telegram-creds \
  --from-literal=TELEGRAM_BOT_TOKEN=your-bot-token \
  -n clawbernetes

# A2A auth tokens (one per agent)
kubectl create secret generic infra-a2a-token \
  --from-literal=A2A_TOKEN=$(openssl rand -hex 24) \
  -n clawbernetes
```

### Step 2: Create a delivery channel (optional)

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawChannel
metadata:
  name: my-telegram
  namespace: clawbernetes
spec:
  type: telegram
  credentialsSecret: telegram-creds
  config:
    dmPolicy: "pairing"
    groupPolicy: "allowlist"
    streaming: "partial"
```

### Step 3: Create your agent

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: my-agent
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are a helpful assistant specialized in data analysis.
      You are thorough, precise, and always cite your sources.
    user: |
      Your operator prefers concise answers with actionable next steps.

  model:
    provider: openai
    name: gpt-4.1

  channels:
    - my-telegram

  workspace:
    mode: persistent
    storageSize: "5Gi"
    reclaimPolicy: retain    # PVC survives agent deletion

  lifecycle:
    restartPolicy: Always
```

```bash
kubectl apply -f my-agent.yaml
```

Your agent is now running, connected to Telegram, with persistent state.

### Step 4: Add A2A communication (optional)

Enable agents to discover and talk to each other via the A2A v0.3.0 protocol:

```yaml
spec:
  a2a:
    enabled: true
    agentCardName: "My Agent"
    skills: [data-analysis, reporting]
    securityTokenSecret: my-agent-a2a-token
    peers:
      - name: other-agent
        agentCardUrl: http://other-agent.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: other-agent-a2a-token
```

Each agent serves an Agent Card at `/.well-known/agent-card.json` and can send/receive messages via JSON-RPC, REST, or gRPC.

## Features

### Persistent Workspace
Agent state (memory, cron, delivery queue, logs) persists across pod restarts via PVC. Delete the agent, recreate with the same name -- state is restored.

```yaml
workspace:
  mode: persistent       # or "ephemeral" for stateless agents
  storageSize: "10Gi"
  reclaimPolicy: retain  # PVC survives agent deletion
```

### Credential Security
LLM API keys never touch agent pods. The gateway injects auth server-side. Channel tokens use `${ENV_VAR}` substitution -- tokens are in Secrets, not ConfigMaps.

### Delivery Channels
Connect agents to Telegram, Slack, Discord, and more via ClawChannel CRDs. Each channel's credentials are injected securely as env vars.

### A2A (Agent-to-Agent)
Powered by [openclaw-a2a-gateway](https://github.com/win4r/openclaw-a2a-gateway) -- a production A2A v0.3.0 implementation with JSON-RPC/REST/gRPC transports, SSE streaming, circuit breakers, and DNS-SD discovery.

### Budget Enforcement
Set daily/monthly USD limits per agent. The observeclaw plugin tracks spend and automatically downgrades to a cheaper model when thresholds are hit.

### Tool Policy
Deny dangerous tools (`rm_rf`, `drop_database`) at the plugin level. Allowlists supported.

### Routing Evaluators
Chain regex blockers (prompt injection), classifiers (complexity routing), and proxy evaluators in priority order.

### Observability
Every LLM call is traced with OpenTelemetry. Traces flow to Tempo. Grafana dashboard shows fleet overview.

## Tear Down

```bash
make demo-down          # Stop operator, delete CRs
make kind-teardown      # Delete the kind cluster
```

## Project Structure

```
api/v1/                  # CRD type definitions (7 CRDs)
internal/controller/     # Reconcilers
config/crd/              # Generated CRD manifests
config/samples/          # Sample CRs
config/proxy/            # Nginx reverse proxy
config/grafana/          # Dashboard JSON
```

## Built With

- [Kubebuilder](https://kubebuilder.io/) -- operator scaffolding
- [OpenClaw](https://github.com/orq-ai/openclaw) (orq-ai fork) -- agent runtime
- [observeclaw](https://github.com/ai-trust-layer/observeclaw) -- budget/policy/anomaly plugin
- [openclaw-a2a-gateway](https://github.com/win4r/openclaw-a2a-gateway) -- A2A v0.3.0 protocol
- [Grafana Tempo](https://grafana.com/oss/tempo/) -- distributed tracing
- [Grafana](https://grafana.com/oss/grafana/) -- visualization
