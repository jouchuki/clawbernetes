# Clawbernetes

https://clawbernetes.org/

**Kubernetes-native orchestration for autonomous AI agent fleets. For founders who want to go off-cloud.**

Clawbernetes turns AI agent deployment into a `kubectl apply` workflow. Declare your agents, policies, and observability stack as Custom Resources -- the operator handles the rest.

## What It Does

One YAML, full fleet:

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: eng-agent
spec:
  identity:
    soul: "You are a senior software engineer..."
  model:
    provider: anthropic
    name: claude-sonnet-4-6
  policy: engineering-policy
  gateway: main-gateway
  observability: fleet-observability
```

`kubectl apply` and the operator:
1. Creates a Pod running [OpenClaw](https://github.com/orq-ai/openclaw) with identity files (SOUL.md, USER.md, IDENTITY.md)
2. Installs the [observeclaw](https://github.com/ai-trust-layer/observeclaw) plugin for budget enforcement, tool policy, anomaly detection, and routing
3. Routes all LLM traffic through a centralized ClawGateway proxy for LLM routing (for example, per budget constraints or to local LLMs)
4. Exports traces, metrics, and logs to Tempo via OpenTelemetry
5. Visualizes everything in a pre-provisioned Grafana dashboard

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
          |                 |                 |
    observeclaw plugin      |            OTLP traces
    - budget enforcement    |                 |
    - tool allow/deny       |           +-----v------+
    - anomaly detection     |           |   Tempo    |
    - routing evaluators    |           +-----+------+
          |                 |                 |
          +-----> LLM calls >----+      +-----v------+
                                 |      |  Grafana   |
                   api.anthropic.com    +------------+
```

## 6 CRDs

| CRD | Purpose |
|-----|---------|
| **ClawAgent** | Declares an autonomous agent with identity, skills, model, and lifecycle settings |
| **ClawGateway** | Centralized LLM proxy with routing evaluators and PII redaction |
| **ClawPolicy** | Budget limits (daily/monthly), tool allow/deny lists, model downgrade thresholds |
| **ClawSkillSet** | Reusable skill packages (SKILL.md files) mounted into agent workspaces |
| **ClawConnector** | External service connections (databases, HTTP APIs) with rate limiting |
| **ClawObservability** | Deploys Tempo + Grafana for distributed tracing and visualization |

## Quick Start

```bash
# Prerequisites: Docker, kind, kubectl, Go 1.25+

# 1. Create cluster, install CRDs, build images, deploy everything
make demo-up

# 2. Deploy nginx proxy for *.local hostnames
make demo-proxy

# 3. Add to /etc/hosts
echo '127.0.0.1 eng-agent.local sales-agent.local grafana.local' | sudo tee -a /etc/hosts

# 4. Port-forward
kubectl port-forward svc/agent-proxy 8080:80 -n clawbernetes

# 5. Access
# http://localhost:8080                          - Fleet config dashboard
# http://eng-agent.local:8080                   - Engineering agent UI
# http://sales-agent.local:8080                 - Sales agent UI
# http://grafana.local:8080/d/clawbernetes-fleet - Grafana fleet dashboard
```

Skip image rebuild on subsequent runs: `make demo-up SKIP_IMAGE=1`

## Key Features

**Budget Enforcement** -- Set daily/monthly USD limits per agent. The observeclaw plugin tracks spend in real-time and automatically downgrades to a cheaper model (e.g. Haiku) when thresholds are hit.

**Tool Policy** -- Deny dangerous tools (`rm_rf`, `drop_database`, `kubectl_delete_namespace`) at the plugin level. Allowlists supported.

**Anomaly Detection** -- Five detectors running on configurable intervals: spend spikes, idle burn, error loops, token inflation, budget warnings. Alerts dispatch to configured webhooks.

**Routing Evaluators** -- Chain regex blockers (prompt injection), classifiers (complexity routing), and proxy evaluators in priority order. All configured declaratively in the ClawGateway CRD.

**Full Observability** -- Every LLM call is traced with OpenTelemetry (model, tokens, duration, session, channel). Traces flow to Tempo. Grafana dashboard shows fleet overview, per-agent activity, and performance stats.

**Gateway Proxy** -- Centralized proxy between agents and LLM providers. Supports PII redaction via configurable regex patterns and optional HuggingFace classifier for complexity-based routing.

## Tear Down

```bash
make demo-down          # Stop operator, delete CRs
make kind-teardown      # Delete the kind cluster
```

## Project Structure

```
api/v1/                  # CRD type definitions
internal/controller/     # Reconcilers for all 6 CRDs
config/crd/              # Generated CRD manifests
config/samples/          # Sample CRs for demo
config/proxy/            # Nginx reverse proxy
config/grafana/          # Dashboard JSON source
```

## Built With

- [Kubebuilder](https://kubebuilder.io/) -- operator scaffolding
- [OpenClaw](https://github.com/orq-ai/openclaw) (orq-ai fork) -- agent runtime
- [observeclaw](https://github.com/ai-trust-layer/observeclaw) -- budget/policy/anomaly plugin
- [Grafana Tempo](https://grafana.com/oss/tempo/) -- distributed tracing
- [Grafana](https://grafana.com/oss/grafana/) -- visualization
