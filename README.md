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
  harness:
    type: openclaw          # or: observeclaw, hermes
  identity:
    soul: "You are a senior software engineer..."
  model:
    provider: anthropic
    name: claude-sonnet-4-6
  channels: [eng-telegram]
  workspace:
    mode: persistent
    storageSize: "10Gi"
```

`kubectl apply` and the operator:
1. Selects the agent runtime (harness) and pulls the container image
2. Creates a Pod with identity files (SOUL.md, USER.md, IDENTITY.md)
3. Connects to delivery channels (Telegram, Slack, Discord) via ClawChannel CRDs
4. Persists agent state across restarts via PVC
5. Routes LLM traffic through a centralized ClawGateway proxy
6. Exports traces to Tempo via OpenTelemetry, visualizes in Grafana

## Harnesses

Clawbernetes supports multiple agent runtimes through its harness abstraction:

| Harness | Image | Description |
|---------|-------|-------------|
| `openclaw` (default) | `ghcr.io/openclaw/openclaw:latest` | Vanilla upstream OpenClaw |
| `observeclaw` | `clawbernetes/openclaw:latest` | orq-ai fork with observeclaw (budget/policy) + a2a-gateway plugins |
| `hermes` | `nousresearch/hermes-agent:latest` | NousResearch Hermes Agent with multi-platform messaging |

Each harness handles its own config format, directory layout, ports, and probes. Use `spec.harness.image` to override the default image for any harness.

## Architecture

```
                       kubectl apply
                            |
                    +-------v--------+
                    |   Operator     |---> API Server (:9090)
                    | (Go, ctrl-rt)  |---> Dashboard  (:5173)
                    +---+---+---+----+
                        |   |   |
          +-------------+   |   +-------------+
          |                 |                 |
    ClawAgent          ClawGateway      ClawObservability
    (per-agent pod)    (FastAPI proxy)  (Tempo + Grafana)
          |                 |
    +-----+------+         |
    | Harness:   |         |
    | openclaw   |   LLM providers
    | hermes     |   (Anthropic, OpenAI, etc.)
    | observeclaw|         |
    +------------+         |
          |                |
    PVC (persistent     api.anthropic.com
     agent state)       api.openai.com
```

## CRDs

| CRD | Purpose |
|-----|---------|
| **ClawAgent** | Declares an autonomous agent with harness, identity, model, channels, workspace, and lifecycle |
| **ClawChannel** | Delivery channel integration (Telegram, Slack, Discord) with credential management |
| **ClawGateway** | Centralized LLM proxy with routing evaluators and PII redaction |
| **ClawPolicy** | Budget limits (daily/monthly), tool allow/deny lists, model downgrade thresholds |
| **ClawSkillSet** | Reusable skill packages (SKILL.md files) mounted into agent workspaces |
| **ClawObservability** | Deploys Tempo + Grafana for distributed tracing and fleet visualization |

## Quick Start

```bash
# Prerequisites: Docker, kind, kubectl, Go 1.25+, Node.js 20+

# 1. Create cluster + install CRDs + start operator + dashboard
make dev

# 2. Open the dashboard
open http://localhost:5173
```

The dashboard lets you create, edit, and delete agents and all other resources. It includes live log streaming and a chat interface for each agent.

## Dashboard

The control plane dashboard at `http://localhost:5173` provides:

- **Fleet overview** -- agent count, running status, channels, A2A links
- **Agent management** -- create/edit/delete with harness picker, identity editor, model config
- **Agent detail** -- live log streaming (SSE), chat relay, config viewer
- **Full CRD management** -- CRUD for channels, policies, skill sets, gateways, observability
- **API** at `http://localhost:9090` -- REST endpoints for all resources

## Deploy Your Own Fleet

### Step 1: Create secrets

```bash
cp .env.example .env
# Edit .env with your API keys

make create-secrets
```

Or manually:

```bash
kubectl create secret generic openclaw-api-keys \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-your-key \
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
    dmPolicy: "open"
```

### Step 3: Create your agent

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: my-agent
  namespace: clawbernetes
spec:
  harness:
    type: hermes             # or: openclaw, observeclaw
  identity:
    soul: |
      You are a helpful assistant specialized in data analysis.
      You are thorough, precise, and always cite your sources.
  model:
    provider: anthropic
    name: claude-sonnet-4-6
  channels:
    - my-telegram
  workspace:
    mode: persistent
    storageSize: "5Gi"
  lifecycle:
    restartPolicy: Always
```

```bash
kubectl apply -f my-agent.yaml
```

Or use the dashboard at `http://localhost:5173/agents/new`.

### Step 4: Add observability (optional)

Use the `observeclaw` harness for budget enforcement, tool policy, and OTEL tracing:

```yaml
spec:
  harness:
    type: observeclaw        # orq-ai fork with plugins
  policy: my-policy          # references a ClawPolicy
  observability: fleet-obs   # references a ClawObservability
```

Build the ObserveClaw image first: `make build-openclaw-image`

## Development

```bash
make dev            # Start everything (kind + operator + dashboard)
make dev-down       # Stop everything
make dashboard      # Start only the UI dev server
make test           # Run unit tests
make build          # Build operator binary
```

## Project Structure

```
api/v1/                  # CRD type definitions
internal/controller/     # Reconcilers
internal/harness/        # Harness abstraction (openclaw, hermes, observeclaw)
internal/api/            # Control plane API server (CRUD, logs, chat)
ui/                      # React + Tailwind dashboard
config/crd/              # Generated CRD manifests
config/samples/          # Sample CRs
build/openclaw/          # ObserveClaw image build
```

## Built With

- [Kubebuilder](https://kubebuilder.io/) -- operator scaffolding
- [OpenClaw](https://openclaw.com/) -- agent runtime (default harness)
- [Hermes Agent](https://github.com/NousResearch/hermes-agent) -- multi-platform agent runtime
- [observeclaw](https://github.com/ai-trust-layer/observeclaw) -- budget/policy/anomaly plugin
- [Grafana Tempo](https://grafana.com/oss/tempo/) -- distributed tracing
- React + Tailwind CSS -- dashboard UI
