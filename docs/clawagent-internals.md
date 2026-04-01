# ClawAgent CRD Internals

---

## Section 1: Technical Summary

### What ClawAgent Is

ClawAgent is a namespaced Custom Resource Definition (CRD) in the `claw.clawbernetes.io/v1` API group that declares a single autonomous OpenClaw AI agent as a first-class Kubernetes resource. When you apply a ClawAgent CR, the Clawbernetes operator reconciles it into a running Pod backed by a Deployment, along with ConfigMaps for identity/skills/configuration, an optional PVC for persistent workspace storage, and a Service for network access (including A2A agent-to-agent communication).

The operator translates declarative agent specifications -- personality, model selection, tool policies, budget controls, observability, delivery channels, and A2A networking -- into concrete Kubernetes resources and an `openclaw.json` configuration file that the OpenClaw runtime consumes.

### Spec Fields Reference

#### `identity` (AgentIdentitySpec)

Defines the agent's personality and context. Each sub-field maps to a Markdown file mounted into the agent's workspace.

| Sub-field | Type | Default | Purpose | Maps to |
|-----------|------|---------|---------|---------|
| `soul` | string | `""` | Core personality and behavioral instructions | `SOUL.md` |
| `user` | string | `""` | Description of the human operator | `USER.md` |
| `agentIdentity` | string | `""` | Agent's persistent self-concept | `IDENTITY.md` |

The controller builds these into a ConfigMap named `<agent-name>-identity`:

```go
// File: internal/controller/clawagent_controller.go:574-594
func (r *ClawAgentReconciler) identityConfigMap(agent *clawv1.ClawAgent, ns, name string) *corev1.ConfigMap {
    data := map[string]string{}
    if agent.Spec.Identity.Soul != "" {
        data["SOUL.md"] = agent.Spec.Identity.Soul
    }
    if agent.Spec.Identity.User != "" {
        data["USER.md"] = agent.Spec.Identity.User
    }
    if agent.Spec.Identity.AgentIdentity != "" {
        data["IDENTITY.md"] = agent.Spec.Identity.AgentIdentity
    }

    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name + "-identity",
            Namespace: ns,
            Labels:    agentLabels(name),
        },
        Data: data,
    }
}
```

#### `skillSet` (string)

References a `ClawSkillSet` CR by name in the same namespace. The controller resolves it and extracts the `skills` list. Each skill entry becomes a key in the `<agent-name>-skills` ConfigMap, then gets mounted as `skills/<skill-name>/SKILL.md` in the workspace via the seed-workspace init container.

```go
// File: internal/controller/clawagent_controller.go:600-616
func (r *ClawAgentReconciler) skillsConfigMap(ns, name string, skills []clawv1.SkillEntry) *corev1.ConfigMap {
    data := map[string]string{}
    for _, s := range skills {
        data[s.Name] = s.Content
    }
    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name + "-skills",
            Namespace: ns,
            Labels:    agentLabels(name),
        },
        Data: data,
    }
}
```

#### `policy` (string)

References a `ClawPolicy` CR by name. The policy is resolved during reconciliation and feeds into two parts of the generated `openclaw.json`:

1. **Budgets** -- `policy.Spec.Budget.Daily`, `Monthly`, `WarnAt`, `DowngradeModel`, `DowngradeProvider` map to the observeclaw plugin's budget config.
2. **Tool policy** -- `policy.Spec.ToolPolicy.Allow` and `Deny` map to the observeclaw plugin's tool allow/deny lists.

If the agent has a `credentialsSecret`, the controller automatically appends credential-path deny patterns to prevent the LLM from reading mounted secrets:

```go
// File: internal/controller/clawagent_controller.go:1340-1352
if agent.Spec.CredentialsSecret != "" {
    denyList, _ := toolDefaults["deny"].([]string)
    denyList = append(denyList,
        "/home/node/.openclaw/credentials/*",
        "cat.*credentials",
        "grep.*credentials",
        "head.*credentials",
        "tail.*credentials",
        "less.*credentials",
        "base64.*credentials",
    )
    toolDefaults["deny"] = denyList
}
```

#### `channels` ([]string)

A list of `ClawChannel` CR names. The operator resolves each one and, if `ch.Spec.IsEnabled()` returns true, generates channel config in `openclaw.json` with `${ENV_VAR}` credential placeholders:

```go
// File: internal/controller/clawagent_controller.go:1133-1154
if len(channels) > 0 {
    channelsCfg := map[string]any{}
    for _, ch := range channels {
        chCfg := map[string]any{
            "enabled": true,
        }
        chType := strings.ToUpper(ch.Spec.Type)
        if clawv1.ChannelsWithBotToken[ch.Spec.Type] {
            chCfg["botToken"] = fmt.Sprintf("${%s_BOT_TOKEN}", chType)
        }
        if clawv1.ChannelsWithAppToken[ch.Spec.Type] {
            chCfg["appToken"] = fmt.Sprintf("${%s_APP_TOKEN}", chType)
        }
        for k, v := range ch.Spec.Config {
            chCfg[k] = v
        }
        channelsCfg[ch.Spec.Type] = chCfg
    }
    cfg["channels"] = channelsCfg
}
```

Each channel's `CredentialsSecret` is also injected as an `envFrom` secret ref so the `${VAR}` placeholders resolve at runtime.

#### `gateway` (string)

References a `ClawGateway` CR. The resolved gateway provides:

1. **Gateway URL** -- computed as `http://<gateway-name>-gateway.<namespace>.svc.cluster.local:<port>` (port defaults to 8443).
2. **Routing evaluators** -- mapped to observeclaw routing config.
3. **Anomaly detection** -- thresholds for spend spikes, idle burns, error loops.
4. **Webhooks** -- alert webhook destinations.

```go
// File: internal/controller/clawagent_controller.go:113-131
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

#### `observability` (string)

References a `ClawObservability` CR. The controller extracts `obs.Spec.OTLPEndpoint` and uses it to:

1. Set `OTEL_EXPORTER_OTLP_ENDPOINT` env var on the agent pod.
2. Configure the `diagnostics-otel` extension in `openclaw.json`.

#### `telemetryCapture` (TelemetryCaptureSpec)

Controls what content appears in OTEL spans. All fields default to `true` when any one field is explicitly set.

| Sub-field | Type | Default | Purpose |
|-----------|------|---------|---------|
| `inputMessages` | bool | true | LLM input messages in spans |
| `outputMessages` | bool | true | LLM output messages in spans |
| `systemInstructions` | bool | true | System instructions in spans |
| `toolDefinitions` | bool | true | Tool schemas in spans |
| `toolContent` | bool | true | Tool call content in spans |
| `sampleRate` | string | `"1.0"` | Trace sampling rate (0.0 to 1.0) |

```go
// File: internal/controller/clawagent_controller.go:1047-1069
tc := agent.Spec.TelemetryCapture
captureContent := map[string]any{
    "inputMessages":      true,
    "outputMessages":     true,
    "systemInstructions": true,
    "toolDefinitions":    true,
    "toolContent":        true,
}
if tc.InputMessages || tc.OutputMessages || tc.SystemInstructions || tc.ToolDefinitions || tc.ToolContent {
    captureContent["inputMessages"] = tc.InputMessages
    captureContent["outputMessages"] = tc.OutputMessages
    captureContent["systemInstructions"] = tc.SystemInstructions
    captureContent["toolDefinitions"] = tc.ToolDefinitions
    captureContent["toolContent"] = tc.ToolContent
}
otelCfg["captureContent"] = captureContent

if tc.SampleRate != "" {
    if sr, err := strconv.ParseFloat(tc.SampleRate, 64); err == nil {
        otelCfg["sampleRate"] = sr
    }
}
```

#### `model` (AgentModelSpec)

| Sub-field | Type | Default | Purpose |
|-----------|------|---------|---------|
| `provider` | string | `""` | Model provider name (e.g. `"anthropic"`) |
| `name` | string | `""` | Model ID (e.g. `"claude-sonnet-4-6"`) |
| `fallback.provider` | string | `""` | Fallback provider |
| `fallback.name` | string | `""` | Fallback model ID |

When both provider and name are set, the controller adds a `model.primary` key to the agent defaults in openclaw.json:

```go
// File: internal/controller/clawagent_controller.go:1026-1031
if agent.Spec.Model.Provider != "" && agent.Spec.Model.Name != "" {
    defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
    defaults["model"] = map[string]any{
        "primary": fmt.Sprintf("%s/%s", agent.Spec.Model.Provider, agent.Spec.Model.Name),
    }
}
```

Direct provider registration uses `${PROVIDER_API_KEY}` env var placeholders and consults lookup tables (`ProviderAPIFormats`, `ProviderBaseURLs`) defined in `api/v1/clawchannel_types.go`.

#### `resources` (AgentResourceRequirements)

Standard Kubernetes resource requests and limits, applied directly to the main `openclaw` container:

```go
// File: internal/controller/clawagent_controller.go:748-753
if agent.Spec.Resources.Requests != nil || agent.Spec.Resources.Limits != nil {
    mainContainer.Resources = corev1.ResourceRequirements{
        Requests: agent.Spec.Resources.Requests,
        Limits:   agent.Spec.Resources.Limits,
    }
}
```

#### `lifecycle` (AgentLifecycleSpec)

| Sub-field | Type | Default | Purpose |
|-----------|------|---------|---------|
| `restartPolicy` | string | `"Always"` | Pod restart behavior (`Always`, `OnFailure`, `Never`) |
| `hibernateAfterIdleMinutes` | *int | nil | Scale to zero after N minutes idle |
| `maxRuntime` | string | `""` | Hard cap on agent runtime (e.g. `"24h"`) |

The restart policy maps directly to the Deployment's pod template:

```go
// File: internal/controller/clawagent_controller.go:860-863
restartPolicy := corev1.RestartPolicyAlways
if agent.Spec.Lifecycle.RestartPolicy != "" {
    restartPolicy = corev1.RestartPolicy(agent.Spec.Lifecycle.RestartPolicy)
}
```

#### `workspace` (WorkspaceSpec)

| Sub-field | Type | Default | Purpose |
|-----------|------|---------|---------|
| `mode` | string | `"ephemeral"` | `ephemeral` (emptyDir) or `persistent` (PVC) |
| `storageSize` | string | `"5Gi"` | PVC size when mode=persistent |
| `storageClassName` | *string | nil | Override default StorageClass |
| `reclaimPolicy` | string | `"retain"` | `retain` keeps PVC on CR deletion; `delete` GCs it |

The `openclaw-home` volume is determined by mode:

```go
// File: internal/controller/clawagent_controller.go:928-945
func openclawHomeVolume(pvcName, mode string) corev1.Volume {
    if mode == clawv1.WorkspaceModePersistent && pvcName != "" {
        return corev1.Volume{
            Name: "openclaw-home",
            VolumeSource: corev1.VolumeSource{
                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                    ClaimName: pvcName,
                },
            },
        }
    }
    return corev1.Volume{
        Name: "openclaw-home",
        VolumeSource: corev1.VolumeSource{
            EmptyDir: &corev1.EmptyDirVolumeSource{},
        },
    }
}
```

#### `credentialsSecret` (string)

Names a Kubernetes Secret mounted read-only at `/home/node/.openclaw/credentials/` with file mode `0400`. The controller also auto-injects tool deny rules to prevent the LLM from reading these files.

```go
// File: internal/controller/clawagent_controller.go:786-803
if agent.Spec.CredentialsSecret != "" {
    credMode := int32(0400)
    volumes = append(volumes, corev1.Volume{
        Name: "credentials-secret",
        VolumeSource: corev1.VolumeSource{
            Secret: &corev1.SecretVolumeSource{
                SecretName:  agent.Spec.CredentialsSecret,
                DefaultMode: &credMode,
                Optional:    boolPtr(true),
            },
        },
    })
    mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
        Name:      "credentials-secret",
        MountPath: "/home/node/.openclaw/credentials",
        ReadOnly:  true,
    })
}
```

#### `a2a` (A2ASpec)

Controls agent-to-agent communication via the A2A v0.3.0 protocol.

| Sub-field | Type | Default | Purpose |
|-----------|------|---------|---------|
| `enabled` | bool | false | Activates the A2A gateway plugin |
| `agentCardName` | string | `metadata.name` | Display name in the A2A Agent Card |
| `agentCardDescription` | string | `"Clawbernetes agent: <name>"` | Human-readable Agent Card description |
| `skills` | []string | `["chat"]` | Skill IDs advertised in the Agent Card |
| `port` | int | 18800 | A2A server port inside the pod |
| `peers` | []A2APeer | `[]` | Remote agents this agent can talk to |
| `securityTokenSecret` | string | `""` | Secret containing inbound A2A_TOKEN |

Each peer has:

| Peer field | Type | Required | Purpose |
|------------|------|----------|---------|
| `name` | string | yes | Peer display name |
| `agentCardUrl` | string | yes | URL to peer's Agent Card |
| `credentialsSecret` | string | no | Secret with key `A2A_TOKEN` for this peer |

### Full YAML Examples

#### Engineering Agent (persistent workspace, A2A enabled, full configuration)

```yaml
# File: config/samples/claw_v1_clawagent.yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: eng-agent                    # Becomes the Deployment, Service, and agent ID
  namespace: clawbernetes
  labels:
    team: engineering                # User-defined labels, not consumed by the operator
    tier: senior
spec:
  identity:
    soul: |                          # --> SOUL.md in workspace
      You are a senior software engineer with deep expertise in distributed
      systems and Kubernetes. You are methodical, cautious with production
      systems, and always verify before you modify.
    user: |                          # --> USER.md in workspace
      Your operator is an infrastructure engineer who prefers concise,
      technical responses. Skip the pleasantries. Show your work.
    agentIdentity: |                 # --> IDENTITY.md in workspace
      I am eng-agent, the infrastructure specialist for this cluster.

  skillSet: engineering-skills       # References ClawSkillSet CR
  policy: engineering-policy         # References ClawPolicy CR (budgets + tool deny)
  channels:                          # References ClawChannel CRs
    - eng-telegram
  gateway: main-gateway              # References ClawGateway CR (LLM proxy + anomaly)
  observability: fleet-observability # References ClawObservability CR (OTLP endpoint)

  telemetryCapture:
    toolDefinitions: true            # Only toolDefinitions enabled; others implicitly false
    sampleRate: "1.0"                # 100% trace sampling

  model:
    provider: anthropic
    name: claude-sonnet-4-6          # Primary model
    fallback:
      provider: anthropic
      name: claude-haiku-4-5         # Fallback if primary unavailable

  resources:
    requests:
      memory: "1Gi"
      cpu: "1"
    limits:
      memory: "4Gi"
      cpu: "3"

  lifecycle:
    restartPolicy: Always
    hibernateAfterIdleMinutes: 30    # Scale to 0 after 30 min idle
    maxRuntime: "24h"                # Hard cap

  workspace:
    mode: persistent                 # PVC-backed storage at /home/node/.openclaw
    storageSize: "10Gi"              # PVC size (default would be 5Gi)
    # reclaimPolicy defaults to "retain" -- PVC survives agent deletion

  credentialsSecret: eng-agent-credentials  # Mounted at /home/node/.openclaw/credentials/

  a2a:
    enabled: true
    agentCardName: "Engineering Agent"
    agentCardDescription: "Senior infrastructure specialist for the cluster"
    skills: [kubernetes, debugging, code-review]  # Advertised in Agent Card
    # port defaults to 18800
    securityTokenSecret: eng-a2a-token            # Inbound auth
    peers:
      - name: sales-agent
        agentCardUrl: http://sales-agent.clawbernetes.svc.cluster.local:18800/.well-known/agent-card.json
        credentialsSecret: sales-a2a-token        # Outbound auth for this peer
```

#### Sales Agent (ephemeral workspace, no A2A, lighter model)

```yaml
# File: config/samples/claw_v1_clawagent_sales.yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: sales-agent
  namespace: clawbernetes
  labels:
    team: sales
    tier: junior
spec:
  identity:
    soul: |
      You are a sales operations assistant. You help the sales team with
      CRM data analysis, pipeline reporting, and outreach drafting. You
      are upbeat, detail-oriented, and always back claims with data.
    user: |
      Your operator is a sales manager who values clear summaries and
      actionable next steps. Keep responses concise and business-focused.
    agentIdentity: |
      I am sales-agent, the sales operations specialist for this org.

  skillSet: engineering-skills
  policy: engineering-policy
  gateway: main-gateway
  observability: fleet-observability

  telemetryCapture:
    inputMessages: true              # Capture LLM inputs
    outputMessages: true             # Capture LLM outputs
    sampleRate: "0.5"                # 50% sampling

  model:
    provider: anthropic
    name: claude-haiku-4-5           # Lighter model, no fallback

  resources:
    requests:
      memory: "1Gi"
      cpu: "1"
    limits:
      memory: "4Gi"
      cpu: "3"

  lifecycle:
    restartPolicy: Always
    hibernateAfterIdleMinutes: 15
    maxRuntime: "8h"

  workspace:
    mode: ephemeral                  # emptyDir -- state lost on pod restart
    # No A2A, no credentialsSecret, no channels
```

### Status Fields

| Field | Type | Purpose |
|-------|------|---------|
| `phase` | string | Lifecycle phase: `Running`, `Pending`, `Progressing`, `Error` |
| `podName` | string | Pod identification string (format: `<name> (replicas: N)`) |
| `workspacePVC` | string | Name of the PVC backing the workspace (if persistent) |
| `conditions` | []metav1.Condition | Standard Kubernetes conditions |

Additional printer columns show `Phase` and `Pod` in `kubectl get clawagents` output.

---

## Section 2: Kubernetes Internals Deep Dive

### The Full Reconciliation Loop

When you run `kubectl apply -f clawagent.yaml`, here is every step that occurs:

1. **API server validates and persists the CR** -- The CRD schema (`claw.clawbernetes.io_clawagents.yaml`) defines validation rules. The API server stores the object in etcd.

2. **controller-runtime watch triggers Reconcile** -- The controller registers watches in `SetupWithManager`:

```go
// File: internal/controller/clawagent_controller.go:1537-1545
func (r *ClawAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&clawv1.ClawAgent{}).       // Watch ClawAgent CRs
        Owns(&appsv1.Deployment{}).      // Watch owned Deployments
        Owns(&corev1.ConfigMap{}).       // Watch owned ConfigMaps
        Owns(&corev1.Service{}).         // Watch owned Services
        Named("clawagent").
        Complete(r)
}
```

`For(&clawv1.ClawAgent{})` sets up a watch on all ClawAgent resources. When one is created, updated, or deleted, controller-runtime enqueues a reconcile request with the object's `NamespacedName`. `Owns(...)` sets up secondary watches: if an owned Deployment, ConfigMap, or Service changes (including pod status updates propagated to the Deployment), controller-runtime traces the owner reference back to the parent ClawAgent and enqueues a reconcile for it.

3. **Fetch the ClawAgent** -- The reconciler starts by getting the CR. If it is gone (deleted), it returns nil (owned resources with owner references are garbage-collected automatically):

```go
// File: internal/controller/clawagent_controller.go:67-77
func (r *ClawAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := logf.FromContext(ctx)
    agent := &clawv1.ClawAgent{}
    if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }
```

4. **Resolve all referenced CRs** -- The controller fetches each referenced resource in sequence:
   - `ClawObservability` (line 84-95) -- extracts `otlpEndpoint`
   - `ClawSkillSet` (line 98-110) -- extracts skill entries
   - `ClawGateway` (line 113-131) -- extracts gateway URL, anomaly config, routing evaluators
   - `ClawPolicy` (line 134-146) -- extracts budget and tool policy
   - `ClawChannel` list (line 149-161) -- resolves each channel by name, filters by `IsEnabled()`

   All "not found" errors are logged and skipped (degraded mode). Hard API errors propagate and cause a retry.

5. **Create/update identity ConfigMap** (`<name>-identity`)
6. **Create/update skills ConfigMap** (`<name>-skills`)
7. **Create/update openclaw config ConfigMap** (`<name>-openclaw-config`) containing `openclaw.json`, `HEARTBEAT.md`, and `TOOLS.md`
8. **Ensure workspace storage** -- PVC if persistent, orphan cleanup if ephemeral
9. **Create/update Deployment**
10. **Create/update Service**
11. **Update CR status** (phase, podName, workspacePVC)
12. **Regenerate fleet dashboard** (non-fatal if it fails)

### How controller-runtime Watches Work

controller-runtime uses the Kubernetes informer/watch mechanism underneath. When you call `For(&clawv1.ClawAgent{})`, it:

1. Creates a **SharedInformer** for the ClawAgent GVR.
2. The informer opens a long-lived HTTP watch connection to the API server.
3. On each watch event (ADDED, MODIFIED, DELETED), the informer calls the controller's event handler.
4. The event handler extracts the object's `NamespacedName` and enqueues it into a **work queue**.
5. The controller's worker goroutine dequeues items and calls `Reconcile(ctx, req)`.

For `Owns(...)` watches, the event handler inspects the changed object's `metadata.ownerReferences`. If it finds an owner of the watched kind (ClawAgent), it enqueues the **owner's** NamespacedName instead. This is why a Deployment status change (e.g., pods becoming ready) triggers a reconcile of the parent ClawAgent.

The work queue provides:
- **Deduplication** -- if the same key is already queued, it is not added again.
- **Rate limiting** -- exponential backoff on errors (default: 5ms base, 1000s max).
- **Requeue** -- returning `ctrl.Result{RequeueAfter: duration}` re-enqueues after the specified delay.

### Sub-Resource Creation: ConfigMaps

#### Identity ConfigMap

Name: `<agent-name>-identity`

Contains up to three keys: `SOUL.md`, `USER.md`, `IDENTITY.md`. Only non-empty identity fields produce keys. Built by `identityConfigMap()` at line 574.

#### Skills ConfigMap

Name: `<agent-name>-skills`

Each skill from the resolved ClawSkillSet becomes a flat key (skill name) with the skill content as value. The seed-workspace init container iterates these keys and creates `skills/<name>/SKILL.md` directories. Built by `skillsConfigMap()` at line 600.

#### OpenClaw Config ConfigMap

Name: `<agent-name>-openclaw-config`

Contains three keys:
- `openclaw.json` -- the full runtime configuration (see detailed breakdown below)
- `HEARTBEAT.md` -- a lightweight checklist the agent checks on each heartbeat cycle
- `TOOLS.md` -- documents available A2A peers and how to communicate with them

Built by `openclawConfigMap()` at line 622.

### The `ensureResource` Helper

All sub-resources (ConfigMaps, Deployment, Service) go through a single helper that sets owner references and does create-if-not-exists:

```go
// File: internal/controller/clawagent_controller.go:321-338
func (r *ClawAgentReconciler) ensureResource(ctx context.Context, owner *clawv1.ClawAgent, obj client.Object, desc string) error {
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

Note: this is a create-only pattern. If the resource already exists, it is **not updated**. The controller relies on the initial creation being correct and on Kubernetes garbage collection for cleanup on CR deletion.

### openclaw.json Generation

The `buildOpenclawConfig` function (line 989-1276) assembles the entire runtime configuration from all resolved CRs. Here is the structure it builds:

**Base structure (always present):**

```go
// File: internal/controller/clawagent_controller.go:991-1023
cfg := map[string]any{
    "gateway": map[string]any{
        "port": openclawGatewayPort,  // 18789
        "bind": "lan",
        "http": map[string]any{
            "endpoints": map[string]any{
                "chatCompletions": map[string]any{"enabled": true},
                "responses":       map[string]any{"enabled": true},
            },
        },
        "controlUi": map[string]any{
            "allowedOrigins": []string{
                fmt.Sprintf("http://%s.local", name),
                fmt.Sprintf("http://%s.local:8080", name),
                fmt.Sprintf("http://localhost:%d", openclawGatewayPort),
                fmt.Sprintf("http://127.0.0.1:%d", openclawGatewayPort),
            },
        },
    },
    "agents": map[string]any{
        "defaults": map[string]any{
            "workspace": "/home/node/.openclaw/workspace",
            "heartbeat": map[string]any{
                "every":           "5m",
                "lightContext":    true,
                "isolatedSession": true,
                "ackMaxChars":     300,
            },
        },
        "list": []map[string]any{
            {"id": name, "default": true},
        },
    },
}
```

**Diagnostics/OTEL section (when observability is configured):**

Added at line 1034-1075. Configures the diagnostics-otel extension with endpoint, protocol (`http/protobuf`), service name, and capture content settings derived from `telemetryCapture`.

**Model providers section:**

Two provider types can be registered:

1. **Gateway-proxied provider** (`gateway-anthropic`) -- when a ClawGateway is referenced. Uses the gateway URL as `baseUrl` with a sentinel API key (`"gateway-managed"`). Pre-registers claude-sonnet-4-6 and claude-haiku-4-5 model definitions. (Lines 1081-1105)

2. **Direct provider** -- when `model.provider` is set. Uses `ProviderBaseURLs` and `ProviderAPIFormats` lookup tables for configuration. Uses `${PROVIDER_API_KEY}` env var placeholder. (Lines 1109-1126)

**Channels section:**

Built from resolved ClawChannel CRs. Each channel type gets `enabled: true`, credential placeholders (`${TYPE_BOT_TOKEN}`, `${TYPE_APP_TOKEN}`), and user-provided config merged in. (Lines 1133-1154)

**Plugins section:**

```go
// File: internal/controller/clawagent_controller.go:1159-1272
pluginEntries := map[string]any{
    "observeclaw": map[string]any{
        "enabled": true,
        "config":  observeclawCfg,
    },
}
// diagnostics-otel if OTLP configured
// channel plugins auto-enabled
// a2a-gateway if A2A enabled
```

The `plugins.allow` list is critical -- OpenClaw blocks any plugin not in this list. The controller builds it dynamically:

```go
// File: internal/controller/clawagent_controller.go:1182-1184
pluginAllow := []string{"observeclaw"}
for _, ch := range channels {
    pluginAllow = append(pluginAllow, ch.Spec.Type)
}
```

If A2A is enabled, `"a2a-gateway"` is appended, and a `plugins.load.paths` entry tells OpenClaw where to find the plugin binary.

### Observeclaw Plugin Configuration

The `buildObserveclawConfig` function (line 1281-1478) generates the config for the observeclaw sidecar plugin. It has four major sections:

#### Budgets

```go
// File: internal/controller/clawagent_controller.go:1288-1322
budgetDefaults := map[string]any{
    "daily":   100,          // $100/day default
    "monthly": 2000,         // $2000/month default
    "warnAt":  0.8,          // Warn at 80% budget
}
downgradeModel := "claude-haiku-4-5"     // Default downgrade target
downgradeProvider := "anthropic"

if policy != nil {
    b := policy.Spec.Budget
    if b.Daily > 0 {
        budgetDefaults["daily"] = b.Daily
    }
    if b.Monthly > 0 {
        budgetDefaults["monthly"] = b.Monthly
    }
    if b.WarnAt != "" {
        if warnAt, err := strconv.ParseFloat(b.WarnAt, 64); err == nil {
            budgetDefaults["warnAt"] = warnAt
        }
    }
    if b.DowngradeModel != "" {
        downgradeModel = b.DowngradeModel
    }
    if b.DowngradeProvider != "" {
        downgradeProvider = b.DowngradeProvider
    }
}

cfg["budgets"] = map[string]any{
    "defaults": budgetDefaults,
    "agents":   map[string]any{},
}
cfg["downgradeModel"] = downgradeModel
cfg["downgradeProvider"] = downgradeProvider
```

#### Tool Policy

```go
// File: internal/controller/clawagent_controller.go:1325-1357
toolDefaults := map[string]any{
    "allow": []string{},
    "deny":  []string{},
}
if policy != nil {
    tp := policy.Spec.ToolPolicy
    if len(tp.Allow) > 0 {
        toolDefaults["allow"] = tp.Allow
    }
    if len(tp.Deny) > 0 {
        toolDefaults["deny"] = tp.Deny
    }
}
// Auto-deny credential path access (shown earlier)
```

#### Anomaly Detection

```go
// File: internal/controller/clawagent_controller.go:1360-1385
anomalyCfg := map[string]any{
    "spendSpikeMultiplier":     3,   // Alert if spend is 3x normal
    "idleBurnMinutes":          10,  // Alert if burning tokens while idle for 10min
    "errorLoopThreshold":       10,  // Alert after 10 consecutive errors
    "tokenInflationMultiplier": 2,   // Alert if token usage is 2x normal
    "checkIntervalSeconds":     30,  // Check every 30 seconds
}
if gateway != nil {
    a := gateway.Spec.Anomaly
    // Override each threshold if set in the ClawGateway CR
    if a.SpendSpikeMultiplier > 0 { anomalyCfg["spendSpikeMultiplier"] = a.SpendSpikeMultiplier }
    if a.IdleBurnMinutes > 0 { anomalyCfg["idleBurnMinutes"] = a.IdleBurnMinutes }
    if a.ErrorLoopThreshold > 0 { anomalyCfg["errorLoopThreshold"] = a.ErrorLoopThreshold }
    if a.TokenInflationMultiplier > 0 { anomalyCfg["tokenInflationMultiplier"] = a.TokenInflationMultiplier }
    if a.CheckIntervalSeconds > 0 { anomalyCfg["checkIntervalSeconds"] = a.CheckIntervalSeconds }
}
```

#### Routing

Evaluators from the ClawGateway are mapped one-to-one to observeclaw evaluator entries. Each evaluator supports: `name`, `type`, `priority`, `enabled`, `action`, `patterns`, `blockReply`, `emitEvent`, `classifierModel`, `timeoutMs`, `redactReplacement`, `proxyUrl`, and `routes`. (Lines 1389-1435)

A catch-all `gateway-proxy` evaluator is always appended when a gateway URL exists:

```go
// File: internal/controller/clawagent_controller.go:1441-1451
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

This regex (`[\s\S]`) matches all traffic and routes it through the gateway.

### A2A Gateway Plugin Configuration

When `a2a.enabled` is true, the controller builds a full A2A gateway plugin config (lines 1186-1271):

**Agent Card:**

```go
// File: internal/controller/clawagent_controller.go:1210-1224
a2aCfg := map[string]any{
    "agentCard": map[string]any{
        "name":        cardName,
        "description": cardDesc,
        "url":         fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/a2a/jsonrpc", name, ns, a2aPort),
        "skills":      a2aSkills,
    },
    "server": map[string]any{
        "host": "0.0.0.0",
        "port": a2aPort,
    },
    "routing": map[string]any{
        "defaultAgentId": name,
    },
}
```

**Security (inbound auth):**

If `securityTokenSecret` is set, inbound requests require bearer auth with `${A2A_TOKEN}`:

```go
// File: internal/controller/clawagent_controller.go:1227-1232
if agent.Spec.A2A.SecurityTokenSecret != "" {
    a2aCfg["security"] = map[string]any{
        "inboundAuth": "bearer",
        "token":       "${A2A_TOKEN}",
    }
}
```

**Peers (outbound auth):**

Each peer gets a `${PEER_<NAME_UPPER>_TOKEN}` env var placeholder. The env var is injected via a `SecretKeyRef` that reads the `A2A_TOKEN` key from the peer's credentials secret:

```go
// File: internal/controller/clawagent_controller.go:842-856
for _, peer := range agent.Spec.A2A.Peers {
    if peer.CredentialsSecret != "" {
        envName := fmt.Sprintf("PEER_%s_TOKEN", strings.ToUpper(strings.ReplaceAll(peer.Name, "-", "_")))
        mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{
            Name: envName,
            ValueFrom: &corev1.EnvVarSource{
                SecretKeyRef: &corev1.SecretKeySelector{
                    LocalObjectReference: corev1.LocalObjectReference{Name: peer.CredentialsSecret},
                    Key:                  "A2A_TOKEN",
                    Optional:             boolPtr(true),
                },
            },
        })
    }
}
```

The A2A port is also added to the container's port list and to the Service.

### Deployment Construction

The `agentDeployment` function (line 653-890) builds a complete Deployment with:

**Pod Security Context:**

```go
// File: internal/controller/clawagent_controller.go:877-881
SecurityContext: &corev1.PodSecurityContext{
    RunAsUser:  int64Ptr(1000),
    RunAsGroup: int64Ptr(1000),
    FSGroup:    int64Ptr(1000),
},
```

**Two init containers:**

1. `copy-extensions` -- runs the openclaw image (`clawbernetes/openclaw:latest`) to copy baked-in extensions and plugins from `/home/node/.openclaw/extensions` and `/home/node/.openclaw/workspace/plugins` into the shared `openclaw-home` emptyDir volume. (Lines 664-676)

2. `seed-workspace` -- runs `busybox:1.36` to copy config files from the three ConfigMaps into the correct workspace directory structure. Creates `skills/<name>/SKILL.md` directories by iterating the skills ConfigMap keys. (Lines 679-700)

**Main container (`openclaw`):**

- Image: `clawbernetes/openclaw:latest`
- Ports: `gateway` (18789) and optionally `a2a` (18800)
- Readiness probe: `GET /ready` on port 18789, initial delay 5s, period 10s
- Liveness probe: `GET /health` on port 18789, initial delay 10s, period 30s
- Volume mount: `openclaw-home` at `/home/node/.openclaw`

**Environment variables:**

- `OTEL_SERVICE_NAME` -- always set to the agent name
- `OTEL_EXPORTER_OTLP_ENDPOINT` -- set when observability is configured
- `OTEL_EXPORTER_OTLP_HEADERS` -- empty string (set alongside endpoint)
- `PEER_<NAME>_TOKEN` -- one per A2A peer with a credentials secret

**EnvFrom secret refs (deduplicated):**

- `openclaw-api-keys` -- when model provider is set and no gateway is used
- Channel credential secrets -- one per enabled channel
- A2A security token secret -- when A2A is enabled

**Volumes (4 base, 1 optional):**

1. `openclaw-home` -- emptyDir or PVC depending on workspace mode
2. `config-src` -- ConfigMap `<name>-openclaw-config`
3. `identity-src` -- ConfigMap `<name>-identity`
4. `skills-src` -- ConfigMap `<name>-skills` (optional: true)
5. `credentials-secret` -- Secret volume (only if `credentialsSecret` is set)

### Agent Service

The controller creates a ClusterIP Service with the same name as the agent:

```go
// File: internal/controller/clawagent_controller.go:959-981
func (r *ClawAgentReconciler) agentService(agent *clawv1.ClawAgent, ns, name string) *corev1.Service {
    labels := agentLabels(name)
    ports := []corev1.ServicePort{
        {Name: "gateway", Port: int32(openclawGatewayPort), TargetPort: intstr.FromInt(openclawGatewayPort), Protocol: corev1.ProtocolTCP},
    }
    if agent.Spec.A2A.Enabled {
        a2aPort := agent.Spec.A2A.ResolvedPort()
        ports = append(ports, corev1.ServicePort{
            Name: "a2a", Port: int32(a2aPort), TargetPort: intstr.FromInt(a2aPort), Protocol: corev1.ProtocolTCP,
        })
    }
    return &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: ns,
            Labels:    labels,
        },
        Spec: corev1.ServiceSpec{
            Selector: labels,
            Ports:    ports,
        },
    }
}
```

The Service selector uses the agent labels: `app: <name>`, `clawbernetes.io/agent: <name>`, `app.kubernetes.io/managed-by: clawbernetes`.

### PVC Lifecycle

#### Creation

When `workspace.mode` is `persistent`, `ensurePVC` (line 408-446) creates a PVC:

```go
// File: internal/controller/clawagent_controller.go:351-369
func buildPVC(pvcName, ns, agentName string, size resource.Quantity, ws clawv1.WorkspaceSpec) *corev1.PersistentVolumeClaim {
    pvc := &corev1.PersistentVolumeClaim{
        ObjectMeta: metav1.ObjectMeta{
            Name:      pvcName,
            Namespace: ns,
            Labels:    agentLabels(agentName),
        },
        Spec: corev1.PersistentVolumeClaimSpec{
            AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
            Resources: corev1.VolumeResourceRequirements{
                Requests: corev1.ResourceList{corev1.ResourceStorage: size},
            },
        },
    }
    if ws.StorageClassName != nil {
        pvc.Spec.StorageClassName = ws.StorageClassName
    }
    return pvc
}
```

The PVC name is `<agent-name>-home` by default (constant `PVCSuffix = "-home"`).

#### Owner Reference and Reclaim Policy

- **`reclaimPolicy: delete`** -- the PVC gets an owner reference pointing to the ClawAgent CR. When the CR is deleted, Kubernetes garbage-collects the PVC.
- **`reclaimPolicy: retain`** (default) -- no owner reference. The PVC survives CR deletion, allowing state recovery by recreating the agent with the same name.

The `syncPVCOwnerRefs` function (line 448-468) handles transitions between policies on an existing PVC -- adding or removing owner references as needed.

#### Resize

PVC resize uses a multi-step state machine in `resizePVC` (line 481-546). Since Kubernetes PVCs may not support in-place expansion, the controller does an offline migration:

1. **Scale deployment to 0** -- releases the RWO PVC. Returns `errRequeueNeeded`.
2. **Create new PVC** (`<name>-home-v2`) with the requested size.
3. **Create migration pod** -- a `busybox:1.36` pod that mounts old PVC read-only at `/src` and new PVC at `/dst`, runs `cp -a /src/. /dst/`.
4. **On success** -- delete old PVC, delete migration pod, update `status.workspacePVC` to the new name.
5. **On failure** -- delete migration pod, return error (will retry on next reconcile).
6. **Still running** -- return `errRequeueNeeded` (requeues after 5 seconds).

Shrinking is not supported -- the controller logs a warning and takes no action.

#### Orphan Cleanup

When workspace mode is changed from `persistent` to `ephemeral`, `cleanupOrphanedPVC` (line 548-568) deletes any PVCs with either the `-home` or `-home-v2` suffix:

```go
// File: internal/controller/clawagent_controller.go:548-568
func (r *ClawAgentReconciler) cleanupOrphanedPVC(ctx context.Context, ns, name string) error {
    log := logf.FromContext(ctx)
    for _, suffix := range []string{clawv1.PVCSuffix, clawv1.PVCResizeSuffix} {
        pvcName := name + suffix
        existing := &corev1.PersistentVolumeClaim{}
        if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: ns}, existing); err != nil {
            if apierrors.IsNotFound(err) {
                continue
            }
            return err
        }
        log.Info("deleting orphaned PVC from previous persistent workspace", "pvc", pvcName)
        if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
            return err
        }
    }
    return nil
}
```

### Status Resolution

```go
// File: internal/controller/clawagent_controller.go:896-914
func (r *ClawAgentReconciler) resolveAgentStatus(ctx context.Context, ns, name string) (phase, podName string) {
    dep := &appsv1.Deployment{}
    if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, dep); err != nil {
        return "Pending", ""
    }

    for _, c := range dep.Status.Conditions {
        if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionTrue {
            podName = fmt.Sprintf("%s (replicas: %d)", name, dep.Status.ReadyReplicas)
            return "Running", podName
        }
    }

    if dep.Status.UnavailableReplicas > 0 {
        return "Progressing", ""
    }
    return "Pending", ""
}
```

The logic:
- If the Deployment cannot be fetched: `Pending`
- If the Deployment has `Available=True` condition: `Running` with pod name info
- If there are unavailable replicas: `Progressing`
- Otherwise: `Pending`

The status is written back to the CR:

```go
// File: internal/controller/clawagent_controller.go:218-225
phase, podName := r.resolveAgentStatus(ctx, ns, name)
agent.Status.Phase = phase
agent.Status.PodName = podName
agent.Status.WorkspacePVC = activePVC
if err := r.Status().Update(ctx, agent); err != nil {
    log.Error(err, "unable to update ClawAgent status")
    return ctrl.Result{}, err
}
```

### Fleet Dashboard Regeneration

After every successful reconcile, `updateFleetDashboard` (line 240-315) lists ALL ClawAgents in the namespace, resolves each one's ClawPolicy for budget/tool info, and generates an HTML dashboard stored in a ConfigMap named `fleet-dashboard`:

```go
// File: internal/controller/clawagent_controller.go:294-314
cm := &corev1.ConfigMap{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "fleet-dashboard",
        Namespace: ns,
        Labels:    map[string]string{"app": "fleet-dashboard", "app.kubernetes.io/managed-by": "clawbernetes"},
    },
    Data: map[string]string{
        "index.html": html,
    },
}

key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
existing := &corev1.ConfigMap{}
if err := r.Get(ctx, key, existing); err != nil {
    if apierrors.IsNotFound(err) {
        return r.Create(ctx, cm)
    }
    return err
}
existing.Data = cm.Data
return r.Update(ctx, existing)
```

This ConfigMap is **not** owned by any single ClawAgent (no owner reference), so it is not garbage-collected when an agent is deleted. It is always overwritten on each reconcile. The HTML generation is in `internal/controller/fleet_dashboard.go` using the `generateFleetDashboardHTML` function.

Dashboard failures are non-fatal -- they are logged but do not block reconciliation.

### Owner References and Garbage Collection

The controller uses Kubernetes owner references for automatic cleanup. When a ClawAgent CR is deleted, all resources with an owner reference pointing to it are garbage-collected by the Kubernetes garbage collector.

**Resources WITH owner references (always garbage-collected on CR deletion):**
- `<name>-identity` ConfigMap
- `<name>-skills` ConfigMap
- `<name>-openclaw-config` ConfigMap
- `<name>` Deployment
- `<name>` Service

**Resources with CONDITIONAL owner references:**
- PVC (`<name>-home` or `<name>-home-v2`):
  - `reclaimPolicy: delete` -- has owner reference, garbage-collected on CR deletion
  - `reclaimPolicy: retain` -- NO owner reference, survives CR deletion

**Resources WITHOUT owner references (never garbage-collected):**
- `fleet-dashboard` ConfigMap -- shared across all agents

The owner reference is set by `ensureResource` (line 324) via `ctrl.SetControllerReference`. For PVCs, it is handled explicitly in `ensurePVC` (line 415-418) based on the reclaim policy.

### RBAC Permissions Required

The controller declares its RBAC needs via kubebuilder markers (lines 54-65):

```
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawagents/finalizers,verbs=update
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawobservabilities,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawskillsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawchannels,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
```

Summary table:

| Resource | API Group | Verbs |
|----------|-----------|-------|
| clawagents | claw.clawbernetes.io | full CRUD |
| clawagents/status | claw.clawbernetes.io | get, update, patch |
| clawagents/finalizers | claw.clawbernetes.io | update |
| clawobservabilities | claw.clawbernetes.io | get, list, watch |
| clawskillsets | claw.clawbernetes.io | get, list, watch |
| clawpolicies | claw.clawbernetes.io | get, list, watch |
| clawgateways | claw.clawbernetes.io | get, list, watch |
| clawchannels | claw.clawbernetes.io | get, list, watch |
| secrets | core | get, list, watch |
| deployments | apps | full CRUD |
| configmaps | core | full CRUD |
| services | core | full CRUD |

Note that Pods and PersistentVolumeClaims are managed directly in the controller code (`r.Create`, `r.Delete`) but are **not** declared in the RBAC markers. The PVC and Pod operations (used during resize migration) would require additional RBAC grants in practice:

- `""` (core) / `persistentvolumeclaims` -- get, list, watch, create, update, delete
- `""` (core) / `pods` -- get, list, watch, create, delete
