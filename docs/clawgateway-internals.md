# ClawGateway Internals

## Section 1: Technical Summary

### What ClawGateway Is

ClawGateway is a Kubernetes Custom Resource that deploys an intelligent LLM proxy into the cluster. It sits between ClawAgent pods and upstream LLM providers (e.g., Anthropic), providing three core capabilities:

1. **Routing** -- an evaluator pipeline that inspects every LLM request and decides whether to block, redact, re-route, or proxy it.
2. **Anomaly detection** -- configurable thresholds that detect runaway agents (error loops, cost spikes, idle burn, token inflation).
3. **Webhook alerting** -- fires HTTP callbacks when evaluators or anomaly detectors trigger.

The gateway is a standalone Python FastAPI server (`server.py`) embedded in a ConfigMap and deployed as its own Deployment + Service. Agents never talk to the upstream LLM directly; all traffic flows through the gateway, which injects the API key server-side so agents never see credentials.

---

### Spec Fields

The Go types live in `api/v1/clawgateway_types.go`. The top-level spec struct:

```go
// api/v1/clawgateway_types.go:151-172
type ClawGatewaySpec struct {
    Topology string              `json:"topology,omitempty"`
    Port     int                 `json:"port,omitempty"`
    Routing  GatewayRoutingSpec  `json:"routing,omitempty"`
    Anomaly  GatewayAnomalySpec  `json:"anomaly,omitempty"`
    Webhooks []GatewayWebhookSpec `json:"webhooks,omitempty"`
}
```

#### `topology` (string, enum: `centralized` | `sidecar`)

Controls the deployment model:
- **`centralized`** -- a single gateway Deployment + Service shared by all agents in the namespace.
- **`sidecar`** -- (future) gateway injected as a sidecar container into each agent pod.

Validated by kubebuilder:

```go
// api/v1/clawgateway_types.go:153-155
// +kubebuilder:validation:Enum=centralized;sidecar
Topology string `json:"topology,omitempty"`
```

#### `port` (int)

The TCP port the gateway listens on. Defaults to `8443` if unset:

```go
// internal/controller/clawgateway_controller.go:62-64
port := gw.Spec.Port
if port == 0 {
    port = 8443
}
```

#### `routing` (GatewayRoutingSpec)

```go
// api/v1/clawgateway_types.go:112-125
type GatewayRoutingSpec struct {
    Enabled          bool                    `json:"enabled,omitempty"`
    LogEveryDecision bool                    `json:"logEveryDecision,omitempty"`
    Evaluators       []GatewayEvaluatorSpec  `json:"evaluators,omitempty"`
}
```

- **`enabled`** -- master switch for the routing pipeline.
- **`logEveryDecision`** -- when `true`, every routing decision is logged (mapped to `logRouting` in the observeclaw plugin config).
- **`evaluators`** -- ordered list of evaluator stages (see below).

#### `anomaly` (GatewayAnomalySpec)

```go
// api/v1/clawgateway_types.go:128-148
type GatewayAnomalySpec struct {
    SpendSpikeMultiplier     int `json:"spendSpikeMultiplier,omitempty"`
    IdleBurnMinutes          int `json:"idleBurnMinutes,omitempty"`
    ErrorLoopThreshold       int `json:"errorLoopThreshold,omitempty"`
    TokenInflationMultiplier int `json:"tokenInflationMultiplier,omitempty"`
    CheckIntervalSeconds     int `json:"checkIntervalSeconds,omitempty"`
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `spendSpikeMultiplier` | 3 | Alert when hourly spend exceeds this multiple of the 7-day average |
| `idleBurnMinutes` | 10 | Alert after N minutes of continuous LLM calls with no useful output |
| `errorLoopThreshold` | 10 | Auto-pause the agent after N consecutive errors |
| `tokenInflationMultiplier` | 2 | Alert when average input tokens exceed this multiple of baseline |
| `checkIntervalSeconds` | 30 | How often anomaly detectors run their checks |

Defaults are applied in `buildObserveclawConfig`:

```go
// internal/controller/clawagent_controller.go:1360-1366
anomalyCfg := map[string]any{
    "spendSpikeMultiplier":     3,
    "idleBurnMinutes":          10,
    "errorLoopThreshold":       10,
    "tokenInflationMultiplier": 2,
    "checkIntervalSeconds":     30,
}
```

#### `webhooks` ([]GatewayWebhookSpec)

```go
// api/v1/clawgateway_types.go:24-36
type GatewayWebhookSpec struct {
    URL         string            `json:"url,omitempty"`
    MinSeverity string            `json:"minSeverity,omitempty"`
    Headers     map[string]string `json:"headers,omitempty"`
}
```

- **`url`** -- the HTTP endpoint to POST alerts to.
- **`minSeverity`** -- minimum severity level to trigger this webhook (e.g., `warning`, `critical`).
- **`headers`** -- additional HTTP headers (e.g., authorization tokens).

Webhooks can be defined at two levels: top-level (`spec.webhooks`) for global alerting, and per-evaluator (`spec.routing.evaluators[].webhooks`) for evaluator-specific notifications.

---

### Full YAML Example with Annotations

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawGateway
metadata:
  name: main-gateway
  namespace: clawbernetes
spec:
  # Deployment model: one gateway shared by all agents in the namespace
  topology: centralized

  # TCP port for the proxy server (defaults to 8443 if omitted)
  port: 8443

  routing:
    # Master switch -- set to false to disable all evaluators
    enabled: true

    # Log every routing decision (maps to observeclaw's logRouting flag)
    logEveryDecision: true

    evaluators:
      # --- Evaluator 1: Regex-based prompt injection blocker ---
      # Runs first (priority 100 = highest). Zero-latency, no LLM call.
      # Matches dangerous patterns and blocks the request immediately.
      - name: exec-injection-blocker
        type: regex           # Pattern matching evaluator
        priority: 100         # Evaluated first (highest priority wins)
        action: block         # Reject the request entirely
        blockReply: "Blocked: command injection or prompt manipulation detected."
        emitEvent: true       # Fire a Kubernetes Event on match

      # --- Evaluator 2: Complexity-based model router ---
      # Runs second (priority 50). Uses a classifier model to determine
      # prompt complexity, then routes to the appropriate model.
      - name: complexity-router
        type: classifier      # Complexity classification evaluator
        priority: 50          # Evaluated after regex blockers
        classifierModel: query-complexity
        routes:
          simple:             # Simple prompts -> cheap model
            provider: anthropic
            model: claude-haiku-4-5
          complex:            # Complex prompts -> powerful model
            provider: anthropic
            model: claude-sonnet-4-6
        timeoutMs: 3000       # Fail open after 3 seconds

  # Anomaly detection thresholds (consumed by agents via observeclaw plugin)
  anomaly:
    spendSpikeMultiplier: 5        # Alert at 5x the 7-day hourly average
    idleBurnMinutes: 15            # Alert after 15 min of useless LLM calls
    errorLoopThreshold: 5          # Auto-pause after 5 consecutive errors
    tokenInflationMultiplier: 3    # Alert at 3x input token baseline
    checkIntervalSeconds: 60       # Run anomaly checks every 60 seconds

  # Global webhook targets for alerts
  webhooks:
    - url: https://hooks.slack.com/services/T.../B.../xxx
      minSeverity: warning
      headers:
        Authorization: "Bearer my-slack-token"
```

---

### Evaluator Types

#### Regex Evaluator (`type: regex`)

Pattern-matching evaluator that runs at zero latency (no LLM call). Each `patterns` entry is a regular expression tested against the user's message content.

| Field | Description |
|-------|-------------|
| `patterns` | List of regex strings to match against request content |
| `action` | `block` (reject request), `redact` (replace matched content), or `proxy` (forward to a different endpoint) |
| `blockReply` | Message returned to the agent when `action: block` fires |
| `redactReplacement` | Replacement text when `action: redact` fires (default: `[REDACTED]`) |
| `proxyUrl` | Target endpoint when `action: proxy` fires |
| `emitEvent` | If `true`, fire a Kubernetes Event on match |
| `webhooks` | Per-evaluator webhook targets |

The gateway's embedded Python server implements PII redaction using these patterns:

```python
# internal/controller/clawgateway_controller.go:416-423 (embedded server.py)
def _compile_patterns(raw: list[dict]) -> list[tuple[re.Pattern, str]]:
    compiled = []
    for entry in raw:
        try:
            compiled.append((re.compile(entry["pattern"]), entry.get("replacement", "[REDACTED]")))
        except re.error as e:
            print(f"[config] bad pattern {entry.get('pattern')!r}: {e}")
    return compiled
```

#### Classifier Evaluator (`type: classifier`)

Complexity-based routing evaluator that classifies prompt difficulty and routes to the appropriate model.

| Field | Description |
|-------|-------------|
| `classifierModel` | Model ID for classification (e.g., `query-complexity`) |
| `classifierEndpoint` | Custom endpoint for the classifier |
| `ollamaEndpoint` | Ollama endpoint for local LlamaGuard-based classification |
| `routes` | Map of complexity class to model route (e.g., `simple` -> `claude-haiku-4-5`) |
| `timeoutMs` | Timeout in milliseconds; fail open if exceeded |

The gateway controller detects classifier evaluators with Ollama endpoints and automatically deploys an Ollama sidecar:

```go
// internal/controller/clawgateway_controller.go:67-77
needsOllama := false
ollamaModel := "llama-guard3:1b"
for _, ev := range gw.Spec.Routing.Evaluators {
    if ev.Type == "classifier" && ev.OllamaEndpoint != "" {
        needsOllama = true
        if ev.ClassifierModel != "" {
            ollamaModel = ev.ClassifierModel
        }
        break
    }
}
```

Routes are injected as environment variables into the gateway Deployment:

```go
// internal/controller/clawgateway_controller.go:169-179
for _, ev := range gw.Spec.Routing.Evaluators {
    if ev.Type == "classifier" && ev.Routes != nil {
        if simple, ok := ev.Routes["simple"]; ok {
            env = append(env, corev1.EnvVar{Name: "ROUTE_SIMPLE_MODEL", Value: simple.Model})
        }
        if complex, ok := ev.Routes["complex"]; ok {
            env = append(env, corev1.EnvVar{Name: "ROUTE_COMPLEX_MODEL", Value: complex.Model})
        }
    }
}
```

---

### Anomaly Detection

The four anomaly detectors, configured via `spec.anomaly`, protect against runaway agent behavior:

| Detector | Field | What it catches |
|----------|-------|-----------------|
| **Error Loop** | `errorLoopThreshold` | Agent stuck in a retry loop, burning tokens on repeated failures. Auto-pauses after N consecutive errors. |
| **Spend Spike** | `spendSpikeMultiplier` | Sudden cost increase. Fires when hourly spend exceeds N times the rolling 7-day average. |
| **Idle Burn** | `idleBurnMinutes` | Agent making LLM calls but producing no useful output. Fires after N minutes of continuous wasteful calls. |
| **Token Inflation** | `tokenInflationMultiplier` | Prompt bloat. Fires when average input token count exceeds N times the baseline. |

The `checkIntervalSeconds` field controls the polling interval for all detectors. These thresholds are not enforced by the gateway server itself -- they flow into the observeclaw plugin configuration on each agent, where the plugin runtime enforces them.

---

### How the Gateway Deployment Is Built

The gateway controller creates a Deployment with an init container and main container:

```go
// internal/controller/clawgateway_controller.go:155-252
func (r *ClawGatewayReconciler) gatewayDeployment(gw *clawv1.ClawGateway, ns, name string, port int, ollamaEndpoint string) *appsv1.Deployment {
    // ...
    InitContainers: []corev1.Container{
        {
            Name:  "install-deps",
            Image: "python:3.11-slim",
            Command: []string{"pip", "install",
                "--target=/deps",
                "fastapi", "uvicorn[standard]", "httpx", "pydantic",
            },
            // ...
        },
    },
    Containers: []corev1.Container{
        {
            Name:  "gateway",
            Image: "python:3.11-slim",
            Command: []string{"python", "/app/server.py",
                "--port", fmt.Sprintf("%d", port),
                "--no-classifier",
            },
            // ...
        },
    },
```

The `install-deps` init container installs Python dependencies into a shared `emptyDir` volume. The main `gateway` container runs `server.py` (mounted from the ConfigMap) with `--no-classifier` for fast startup.

---

### How Agents Consume the Gateway

The agent reconciler resolves the gateway reference and constructs the proxy URL:

```go
// internal/controller/clawagent_controller.go:113-131
gatewayURL := ""
var gateway *clawv1.ClawGateway
if agent.Spec.Gateway != "" {
    gw := &clawv1.ClawGateway{}
    gwKey := types.NamespacedName{Name: agent.Spec.Gateway, Namespace: ns}
    if err := r.Get(ctx, gwKey, gw); err != nil {
        // ...
    } else {
        gateway = gw
        port := gw.Spec.Port
        if port == 0 {
            port = 8443
        }
        gatewayURL = fmt.Sprintf("http://%s-gateway.%s.svc.cluster.local:%d", agent.Spec.Gateway, ns, port)
    }
}
```

The gateway URL is then registered as a model provider in `openclaw.json`:

```go
// internal/controller/clawagent_controller.go:1081-1104
if gatewayURL != "" {
    providers["gateway-anthropic"] = map[string]any{
        "baseUrl": gatewayURL,
        "api":     "anthropic-messages",
        "apiKey":  "gateway-managed", // sentinel -- gateway injects the real key server-side
        "models": []map[string]any{
            {
                "id":            "claude-sonnet-4-6",
                "name":          "Claude Sonnet 4.6 (via gateway)",
                // ...
            },
            {
                "id":            "claude-haiku-4-5",
                "name":          "Claude Haiku 4.5 (via gateway)",
                // ...
            },
        },
    }
}
```

Note the `"apiKey": "gateway-managed"` sentinel value. The agent never receives the real API key. The gateway server injects it server-side from the `ANTHROPIC_API_KEY` environment variable (sourced from the `openclaw-api-keys` Secret):

```python
# internal/controller/clawgateway_controller.go:493 (embedded server.py)
_SERVER_API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
```

---

## Section 2: Kubernetes Internals Deep Dive

### The ClawGatewayReconciler

The gateway has its own dedicated reconciler, separate from the agent reconciler:

```go
// internal/controller/clawgateway_controller.go:39-42
type ClawGatewayReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}
```

#### Reconcile Loop

The `Reconcile` method follows this sequence:

1. **Fetch** the `ClawGateway` CR.
2. **Detect Ollama requirement** -- scan evaluators for `classifier` type with `OllamaEndpoint` set.
3. **Create Ollama Deployment + Service** (if needed).
4. **Create gateway script ConfigMap** -- embeds the `server.py` Python source.
5. **Create gateway Deployment** -- Python container with init container for pip installs.
6. **Create gateway Service** -- ClusterIP service exposing the gateway port.

```go
// internal/controller/clawgateway_controller.go:48-110
func (r *ClawGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ...
    // Check if any evaluator needs Ollama (LlamaGuard).
    needsOllama := false
    // ...
    if needsOllama {
        if err := r.ensureResource(ctx, gw, r.ollamaDeployment(ns, name, ollamaModel), "ollama-deployment"); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.ensureResource(ctx, gw, r.ollamaService(ns, name), "ollama-service"); err != nil {
            return ctrl.Result{}, err
        }
    }

    if err := r.ensureResource(ctx, gw, r.gatewayScriptConfigMap(gw, ns, name), "gateway-script-cm"); err != nil {
        return ctrl.Result{}, err
    }

    // ...gateway Deployment and Service...
}
```

#### Resources Created

For a `ClawGateway` named `main-gateway` on port `8443` in namespace `clawbernetes`:

| Resource | Name | Purpose |
|----------|------|---------|
| ConfigMap | `main-gateway-gateway-script` | Contains `server.py` (the FastAPI proxy) |
| Deployment | `main-gateway-gateway` | Runs the Python gateway server |
| Service | `main-gateway-gateway` | ClusterIP service on port 8443 |
| Deployment | `main-gateway-ollama` | Ollama server (only if classifier + ollamaEndpoint) |
| Service | `main-gateway-ollama` | Ollama service on port 11434 (only if needed) |

All resources carry these labels:

```go
// internal/controller/clawgateway_controller.go:361-367
func gatewayLabels(name string) map[string]string {
    return map[string]string{
        "app":                          name + "-gateway",
        "clawbernetes.io/gateway":      name,
        "app.kubernetes.io/managed-by": "clawbernetes",
    }
}
```

---

### How the Gateway Is Deployed as a Standalone Service

The gateway is a pure standalone service, not a sidecar. The reconciler creates a single-replica Deployment running `python:3.11-slim`.

**Volume layout:**

| Volume | Type | Purpose |
|--------|------|---------|
| `script` | ConfigMap (`<name>-gateway-script`) | Mounts `server.py` at `/app/` |
| `deps` | EmptyDir | Shared between init container (pip install) and main container |

**Environment variables injected into the gateway container:**

| Env Var | Source | Purpose |
|---------|--------|---------|
| `GATEWAY_PORT` | `spec.port` | Port the server binds to |
| `UPSTREAM_BASE_URL` | Hardcoded `https://api.anthropic.com` | Upstream LLM API |
| `PYTHONPATH` | `/deps` | Points to pip-installed packages |
| `OLLAMA_ENDPOINT` | Computed from gateway name | Ollama sidecar URL (if needed) |
| `ROUTE_SIMPLE_MODEL` | `spec.routing.evaluators[].routes.simple.model` | Simple complexity route |
| `ROUTE_COMPLEX_MODEL` | `spec.routing.evaluators[].routes.complex.model` | Complex complexity route |
| `ANTHROPIC_API_KEY` | Secret `openclaw-api-keys` (via `envFrom`) | Server-side API key injection |

The Secret mount uses `Optional: true` so the gateway starts even without the secret:

```go
// internal/controller/clawgateway_controller.go:218-225
EnvFrom: []corev1.EnvFromSource{
    {
        SecretRef: &corev1.SecretEnvSource{
            LocalObjectReference: corev1.LocalObjectReference{Name: "openclaw-api-keys"},
            Optional:             boolPtr(true),
        },
    },
},
```

---

### How Agents Reference and Consume the Gateway

The agent-to-gateway relationship flows through the `buildObserveclawConfig` function in `clawagent_controller.go`. This is where ClawGateway CRD fields become runtime configuration.

#### The buildObserveclawConfig Function

```go
// internal/controller/clawagent_controller.go:1281
func (r *ClawAgentReconciler) buildObserveclawConfig(agent *clawv1.ClawAgent, agentName, gatewayURL string, policy *clawv1.ClawPolicy, gateway *clawv1.ClawGateway) map[string]any {
```

This function takes both the `ClawPolicy` (for budgets and tool policy) and the `ClawGateway` (for anomaly thresholds, routing evaluators, and webhooks) and produces a unified observeclaw plugin config map. The output is embedded in `openclaw.json` under `plugins.entries.observeclaw.config`.

It is called from `openclawConfigMap`:

```go
// internal/controller/clawagent_controller.go:1157
observeclawCfg := r.buildObserveclawConfig(agent, name, gatewayURL, policy, gateway)

pluginEntries := map[string]any{
    "observeclaw": map[string]any{
        "enabled": true,
        "config":  observeclawCfg,
    },
}
```

---

### The Routing Pipeline

#### How Evaluators Are Ordered by Priority

Evaluators are defined in `spec.routing.evaluators` with a `priority` field. **Higher priority numbers run first.** The agent controller maps every CRD evaluator directly into the observeclaw plugin config, preserving priority values:

```go
// internal/controller/clawagent_controller.go:1392-1434
for _, ev := range gateway.Spec.Routing.Evaluators {
    entry := map[string]any{
        "name":     ev.Name,
        "type":     ev.Type,
        "priority": ev.Priority,
        "enabled":  true,
    }
    if ev.Action != "" {
        entry["action"] = ev.Action
    }
    if len(ev.Patterns) > 0 {
        entry["patterns"] = ev.Patterns
    }
    if ev.BlockReply != "" {
        entry["blockReply"] = ev.BlockReply
    }
    // ... all other fields mapped 1:1 ...
    if ev.Routes != nil {
        routes := map[string]any{}
        for k, v := range ev.Routes {
            routes[k] = map[string]any{
                "provider": v.Provider,
                "model":    v.Model,
            }
        }
        entry["routes"] = routes
    }
    evaluators = append(evaluators, entry)
}
```

#### The Catch-All Gateway Proxy

After all user-defined evaluators, the agent controller appends a catch-all evaluator with `priority: 0` that routes all remaining traffic through the gateway:

```go
// internal/controller/clawagent_controller.go:1440-1451
if gatewayURL != "" {
    evaluators = append(evaluators, map[string]any{
        "name":          "gateway-proxy",
        "type":          "regex",
        "priority":      0,
        "enabled":       true,
        "action":        "proxy",
        "patterns":      []string{"[\\s\\S]"},
        "proxyProvider": "gateway-anthropic",
        "proxyModel":    "claude-sonnet-4-6",
    })
}
```

The pattern `[\s\S]` matches any string (including empty), ensuring every request that was not blocked or routed by a higher-priority evaluator flows through the gateway proxy.

#### Decision Flow

For a request with evaluators at priorities 100 and 50, plus the catch-all at 0:

```
Request arrives
    |
    v
[Priority 100: exec-injection-blocker (regex)]
    |-- Match? --> BLOCK (return blockReply, emit Event)
    |-- No match --> continue
    v
[Priority 50: complexity-router (classifier)]
    |-- Classified "simple" --> Route to claude-haiku-4-5
    |-- Classified "complex" --> Route to claude-sonnet-4-6
    |-- Timeout/error --> fall through (fail open)
    v
[Priority 0: gateway-proxy (catch-all)]
    --> Proxy to gateway-anthropic provider (claude-sonnet-4-6)
```

The full routing config is assembled as:

```go
// internal/controller/clawagent_controller.go:1453-1457
cfg["routing"] = map[string]any{
    "enabled":    len(evaluators) > 0,
    "logRouting": gateway != nil && gateway.Spec.Routing.LogEveryDecision,
    "evaluators": evaluators,
}
```

---

### How Anomaly Thresholds Flow into the Observeclaw Plugin Config

The gateway's `spec.anomaly` fields are read by the agent controller (not the gateway controller) and injected into each agent's observeclaw config. Defaults are applied first, then CRD values override:

```go
// internal/controller/clawagent_controller.go:1360-1385
anomalyCfg := map[string]any{
    "spendSpikeMultiplier":     3,
    "idleBurnMinutes":          10,
    "errorLoopThreshold":       10,
    "tokenInflationMultiplier": 2,
    "checkIntervalSeconds":     30,
}
if gateway != nil {
    a := gateway.Spec.Anomaly
    if a.SpendSpikeMultiplier > 0 {
        anomalyCfg["spendSpikeMultiplier"] = a.SpendSpikeMultiplier
    }
    if a.IdleBurnMinutes > 0 {
        anomalyCfg["idleBurnMinutes"] = a.IdleBurnMinutes
    }
    if a.ErrorLoopThreshold > 0 {
        anomalyCfg["errorLoopThreshold"] = a.ErrorLoopThreshold
    }
    if a.TokenInflationMultiplier > 0 {
        anomalyCfg["tokenInflationMultiplier"] = a.TokenInflationMultiplier
    }
    if a.CheckIntervalSeconds > 0 {
        anomalyCfg["checkIntervalSeconds"] = a.CheckIntervalSeconds
    }
}
cfg["anomaly"] = anomalyCfg
```

This means anomaly detection is **enforced per-agent** by the observeclaw plugin, not centrally by the gateway server. The gateway CRD is the single source of truth for thresholds, but each agent gets its own copy in `openclaw.json`.

---

### Webhook Configuration

Top-level webhooks from `spec.webhooks` are mapped into the observeclaw plugin config:

```go
// internal/controller/clawagent_controller.go:1460-1475
webhooks := []map[string]any{}
if gateway != nil {
    for _, wh := range gateway.Spec.Webhooks {
        entry := map[string]any{
            "url": wh.URL,
        }
        if wh.MinSeverity != "" {
            entry["minSeverity"] = wh.MinSeverity
        }
        if len(wh.Headers) > 0 {
            entry["headers"] = wh.Headers
        }
        webhooks = append(webhooks, entry)
    }
}
cfg["webhooks"] = webhooks
```

Per-evaluator webhooks (defined in `spec.routing.evaluators[].webhooks`) are available in the CRD schema but are mapped through the evaluator config entries rather than the top-level webhooks array.

---

### Proxy URL Construction and Traffic Routing

The proxy URL follows a deterministic pattern based on the gateway name, namespace, and port:

```
http://<gateway-name>-gateway.<namespace>.svc.cluster.local:<port>
```

For example, a gateway named `main-gateway` in namespace `clawbernetes` on port 8443:

```
http://main-gateway-gateway.clawbernetes.svc.cluster.local:8443
```

This URL is constructed in the agent reconciler:

```go
// internal/controller/clawagent_controller.go:126-130
port := gw.Spec.Port
if port == 0 {
    port = 8443
}
gatewayURL = fmt.Sprintf("http://%s-gateway.%s.svc.cluster.local:%d", agent.Spec.Gateway, ns, port)
```

The URL is used in two places:
1. As the `baseUrl` for the `gateway-anthropic` model provider in `openclaw.json`.
2. As the target of the catch-all `gateway-proxy` evaluator in the routing pipeline.

When the gateway is configured, the agent skips injecting the provider API key secret since the gateway handles credentials server-side:

```go
// internal/controller/clawagent_controller.go:822-824
// Model provider API key (direct providers only, no gateway).
if agent.Spec.Model.Provider != "" && p.GatewayURL == "" {
    injectSecret(clawv1.DefaultProviderAPIKeysSecret)
}
```

The gateway Service (`main-gateway-gateway`) uses a label selector to route to the gateway Deployment pods:

```go
// internal/controller/clawgateway_controller.go:258-273
func (r *ClawGatewayReconciler) gatewayService(ns, name string, port int) *corev1.Service {
    labels := gatewayLabels(name)
    return &corev1.Service{
        // ...
        Spec: corev1.ServiceSpec{
            Selector: labels,
            Ports: []corev1.ServicePort{
                {Name: "http", Port: int32(port), TargetPort: intstr.FromInt(port), Protocol: corev1.ProtocolTCP},
            },
        },
    }
}
```

---

### Ollama Sidecar Deployment

When a classifier evaluator specifies `ollamaEndpoint`, the gateway controller deploys Ollama as a separate Deployment + Service (not as a literal sidecar container, but as a companion service):

```go
// internal/controller/clawgateway_controller.go:279-333
func (r *ClawGatewayReconciler) ollamaDeployment(ns, gwName, model string) *appsv1.Deployment {
    name := gwName + "-ollama"
    // ...
    Containers: []corev1.Container{
        {
            Name:  "ollama",
            Image: "ollama/ollama:latest",
            Ports: []corev1.ContainerPort{
                {Name: "http", ContainerPort: 11434, Protocol: corev1.ProtocolTCP},
            },
            Lifecycle: &corev1.Lifecycle{
                PostStart: &corev1.LifecycleHandler{
                    Exec: &corev1.ExecAction{
                        Command: []string{"sh", "-c",
                            fmt.Sprintf("sleep 5 && ollama pull %s", model),
                        },
                    },
                },
            },
            Resources: corev1.ResourceRequirements{
                Requests: corev1.ResourceList{
                    corev1.ResourceMemory: resource.MustParse("2Gi"),
                    corev1.ResourceCPU:    resource.MustParse("500m"),
                },
                Limits: corev1.ResourceList{
                    corev1.ResourceMemory: resource.MustParse("4Gi"),
                    corev1.ResourceCPU:    resource.MustParse("2"),
                },
            },
        },
    },
```

The Ollama endpoint is constructed and passed to the gateway Deployment:

```go
// internal/controller/clawgateway_controller.go:96-98
if needsOllama {
    ollamaEndpoint = fmt.Sprintf("http://%s-ollama.%s.svc.cluster.local:11434", name, ns)
}
```

Default model is `llama-guard3:1b` (a safety classifier), pulled via a PostStart lifecycle hook.

---

### The ensureResource Pattern

Both controllers use the same create-if-not-exists pattern. The gateway controller's version:

```go
// internal/controller/clawgateway_controller.go:112-129
func (r *ClawGatewayReconciler) ensureResource(ctx context.Context, owner *clawv1.ClawGateway, obj client.Object, desc string) error {
    log := logf.FromContext(ctx)

    if err := ctrl.SetControllerReference(owner, obj, r.Scheme); err != nil {
        return fmt.Errorf("setting owner reference on %s: %w", desc, err)
    }

    key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
    existing := obj.DeepCopyObject().(client.Object)
    if err := r.Get(ctx, key, existing); err != nil {
        if apierrors.IsNotFound(err) {
            log.Info("creating resource", "kind", desc, "name", key.Name)
            return r.Create(ctx, obj)
        }
        return err
    }
    return nil
}
```

Key behavior: this is **create-only, not update**. If the resource already exists, the function returns `nil` without updating. This means changes to the ClawGateway CR (e.g., new port, new evaluators) require deleting the existing child resources to pick up changes, or the controller needs a rewrite to support updates.

Owner references are set via `ctrl.SetControllerReference`, which means the Kubernetes garbage collector will delete all child resources when the ClawGateway CR is deleted.

---

### RBAC

#### Gateway Controller RBAC

```go
// internal/controller/clawgateway_controller.go:44-46
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways/finalizers,verbs=update
```

The gateway controller has full CRUD on its own CRD. It also implicitly needs `apps/deployments`, `core/services`, and `core/configmaps` permissions via the `Owns()` declarations in `SetupWithManager`.

#### Agent Controller's Gateway Access

```go
// internal/controller/clawagent_controller.go:60
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways,verbs=get;list;watch
```

The agent controller has **read-only** access to ClawGateway resources. It never modifies the gateway -- it only reads the spec to construct the observeclaw plugin config.

#### Controller Manager Registration

The gateway controller watches its own CR plus all owned resources:

```go
// internal/controller/clawgateway_controller.go:370-378
func (r *ClawGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&clawv1.ClawGateway{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.Service{}).
        Owns(&corev1.ConfigMap{}).
        Named("clawgateway").
        Complete(r)
}
```

This means changes to any owned Deployment, Service, or ConfigMap (including external modifications or deletions) will trigger a re-reconcile of the parent ClawGateway. Combined with the create-only `ensureResource` pattern, this provides self-healing: if someone deletes the gateway Service, the reconciler will recreate it on the next loop.

---

### File Reference

| File | Purpose |
|------|---------|
| `api/v1/clawgateway_types.go` | Go type definitions for ClawGateway CRD |
| `config/crd/bases/claw.clawbernetes.io_clawgateways.yaml` | Generated OpenAPI v3 CRD schema |
| `config/samples/claw_v1_clawgateway.yaml` | Example ClawGateway manifest |
| `internal/controller/clawgateway_controller.go` | Gateway reconciler (Deployment, Service, ConfigMap creation) |
| `internal/controller/clawagent_controller.go` | Agent reconciler (gateway consumption via `buildObserveclawConfig`) |
