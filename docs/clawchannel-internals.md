# ClawChannel CRD Internals

## Section 1: Technical Summary

### What is ClawChannel?

ClawChannel is a namespaced Custom Resource Definition (CRD) in the Clawbernetes operator that declares a **delivery channel** for agent communication. Each ClawChannel represents a single messaging platform integration -- Telegram, Slack, Discord, and so on -- that an agent can use to send and receive messages.

ClawChannel is a **passive data resource**. It has no controller of its own. The `ClawAgentReconciler` reads ClawChannel objects during its reconciliation loop, merges their configuration into the `openclaw.json` file, injects their credential secrets as environment variables, and enables the corresponding plugins in the agent pod.

**Group/Version/Kind:** `claw.clawbernetes.io/v1 ClawChannel`  
**Scope:** Namespaced  
**Source CRD:** `config/crd/bases/claw.clawbernetes.io_clawchannels.yaml`  
**Go types:** `api/v1/clawchannel_types.go`

### Spec Fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `type` | `string` (enum) | Yes | -- | The delivery channel platform. One of the 13 supported types listed below. |
| `credentialsSecret` | `string` | Yes | -- | Name of a Kubernetes Secret containing bot tokens and app tokens for this channel. Keys must follow the `<TYPE_UPPER>_BOT_TOKEN` / `<TYPE_UPPER>_APP_TOKEN` convention. |
| `enabled` | `*bool` | No | `true` | Controls whether this channel is active. When `false`, the agent reconciler skips it entirely. |
| `config` | `map[string]string` | No | `nil` | Arbitrary key-value pairs that map directly to fields in the `channels.<type>` section of `openclaw.json`. |

The `enabled` field uses a pointer to `bool` so that its zero value is distinguishable from an explicit `false`. The helper method handles this:

```go
// api/v1/clawchannel_types.go:90-92
func (c ClawChannelSpec) IsEnabled() bool {
    return c.Enabled == nil || *c.Enabled
}
```

### Full YAML Examples

**Telegram channel:**

```yaml
# config/samples/claw_v1_clawchannel_telegram.yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawChannel
metadata:
  name: eng-telegram          # Name referenced from ClawAgent.spec.channels
  namespace: clawbernetes
  labels:
    team: engineering
spec:
  type: telegram               # Must match one of the 13 enum values
  credentialsSecret: telegram-creds  # Secret must contain TELEGRAM_BOT_TOKEN key
  config:
    dmPolicy: "pairing"        # Merged verbatim into openclaw.json channels.telegram
    groupPolicy: "allowlist"
    streaming: "partial"
```

**Slack channel:**

```yaml
# config/samples/claw_v1_clawchannel_slack.yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawChannel
metadata:
  name: eng-slack
  namespace: clawbernetes
  labels:
    team: engineering
spec:
  type: slack                  # Slack requires both BOT_TOKEN and APP_TOKEN
  credentialsSecret: slack-creds  # Secret must contain SLACK_BOT_TOKEN and SLACK_APP_TOKEN
  config:
    mode: "socket"             # Socket mode vs. webhook mode
    webhookPath: "/slack/events"
    streaming: "partial"
    nativeStreaming: "true"
    groupPolicy: "allowlist"
```

### How an Agent References Channels

A `ClawAgent` resource lists channel names in its `spec.channels` string array. Each entry is the `.metadata.name` of a ClawChannel in the same namespace:

```go
// api/v1/clawagent_types.go:203-207
// channels is a list of ClawChannel resource names for delivery integrations
// (e.g. Telegram, Slack). The operator resolves each channel and generates
// the corresponding openclaw.json channels config with ${ENV_VAR} credential placeholders.
// +optional
Channels []string `json:"channels,omitempty"`
```

Example ClawAgent YAML fragment:

```yaml
spec:
  channels:
    - eng-telegram
    - eng-slack
```

### How Channels are Resolved

The reconciler iterates over the agent's channel names, fetches each ClawChannel by namespaced name, and filters out disabled channels:

```go
// internal/controller/clawagent_controller.go:148-161
// --- Resolve ClawChannels if referenced ---
var channels []clawv1.ClawChannel
for _, chName := range agent.Spec.Channels {
    ch := &clawv1.ClawChannel{}
    chKey := types.NamespacedName{Name: chName, Namespace: ns}
    if err := r.Get(ctx, chKey, ch); err != nil {
        if !apierrors.IsNotFound(err) {
            return ctrl.Result{}, err
        }
        log.Info("referenced ClawChannel not found", "name", chName)
    } else if ch.Spec.IsEnabled() {
        channels = append(channels, *ch)
    }
}
```

Key behaviors:
- If a ClawChannel is not found, the reconciler logs a warning and continues (does not fail).
- If the channel exists but `enabled` is `false`, it is silently skipped.
- Only enabled, existing channels make it into the `channels` slice.

### How Channels are Injected into openclaw.json

The resolved channels slice flows into `buildOpenclawConfig`, which generates the `channels` section of `openclaw.json`:

```go
// internal/controller/clawagent_controller.go:1132-1154
// --- Channels: generate channels config from ClawChannel CRs ---
if len(channels) > 0 {
    channelsCfg := map[string]any{}
    for _, ch := range channels {
        chCfg := map[string]any{
            "enabled": true,
        }
        // Map credential secret keys to ${ENV_VAR} placeholders.
        chType := strings.ToUpper(ch.Spec.Type)
        if clawv1.ChannelsWithBotToken[ch.Spec.Type] {
            chCfg["botToken"] = fmt.Sprintf("${%s_BOT_TOKEN}", chType)
        }
        if clawv1.ChannelsWithAppToken[ch.Spec.Type] {
            chCfg["appToken"] = fmt.Sprintf("${%s_APP_TOKEN}", chType)
        }
        // Merge user-provided config (dmPolicy, groupPolicy, streaming, etc.)
        for k, v := range ch.Spec.Config {
            chCfg[k] = v
        }
        channelsCfg[ch.Spec.Type] = chCfg
    }
    cfg["channels"] = channelsCfg
}
```

For example, given the Slack sample above, this produces:

```json
{
  "channels": {
    "slack": {
      "enabled": true,
      "botToken": "${SLACK_BOT_TOKEN}",
      "appToken": "${SLACK_APP_TOKEN}",
      "mode": "socket",
      "webhookPath": "/slack/events",
      "streaming": "partial",
      "nativeStreaming": "true",
      "groupPolicy": "allowlist"
    }
  }
}
```

---

## Section 2: Kubernetes Internals Deep Dive

### No Dedicated Controller

ClawChannel has **no controller**. There is no `ClawChannelReconciler`. The CRD exists purely as a passive storage resource consumed by the `ClawAgentReconciler`.

When you look at `SetupWithManager`, only ClawAgent events trigger reconciliation:

```go
// internal/controller/clawagent_controller.go:1537-1545
func (r *ClawAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&clawv1.ClawAgent{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.ConfigMap{}).
        Owns(&corev1.Service{}).
        Named("clawagent").
        Complete(r)
}
```

There is no `Watches(&clawv1.ClawChannel{}, ...)` call. This means:
- If you update a ClawChannel CR, the agent pod **will not automatically reconcile**. The change takes effect only on the next ClawAgent reconciliation (e.g., triggered by editing the ClawAgent CR or restarting the controller).
- This is a deliberate design choice to keep the controller simple.

### Channel Resolution Flow

The full data flow from ClawChannel CR to running agent pod is:

1. **ClawAgent reconcile starts** -- the reconciler reads `agent.Spec.Channels`, a `[]string` of ClawChannel names.
2. **Fetch each ClawChannel** -- `r.Get(ctx, chKey, ch)` with the same namespace as the agent (line 153).
3. **Filter disabled** -- `ch.Spec.IsEnabled()` returns false when `enabled` is explicitly `false` (line 158).
4. **Generate openclaw.json** -- the `channels` slice is passed to `openclawConfigMap` (line 176), which calls `buildOpenclawConfig` (line 630).
5. **Inject credential secrets** -- the `channels` slice is also passed to `agentDeployment` via `deploymentParams.Channels` (line 205).
6. **Create ConfigMap** -- the generated `openclaw.json` is stored in ConfigMap `<agent-name>-openclaw-config` (line 625).
7. **Create Deployment** -- the deployment mounts the ConfigMap and injects the secrets.

### Credential Secret Convention and Env Var Injection

Channel credentials follow a strict naming convention. Given a channel type (e.g., `slack`), the operator expects the referenced Secret to contain keys like:

| Channel Type | Required Secret Keys |
|---|---|
| `telegram` | `TELEGRAM_BOT_TOKEN` |
| `slack` | `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN` |
| `discord` | `DISCORD_BOT_TOKEN` |
| `msteams` | `MSTEAMS_BOT_TOKEN` |
| Other types | No automatic token mapping (config only) |

This is determined by two lookup maps in `api/v1/clawchannel_types.go`:

```go
// api/v1/clawchannel_types.go:34-44
var ChannelsWithBotToken = map[string]bool{
    ChannelTypeTelegram: true,
    ChannelTypeSlack:    true,
    ChannelTypeDiscord:  true,
    ChannelTypeMSTeams:  true,
}

var ChannelsWithAppToken = map[string]bool{
    ChannelTypeSlack: true,
}
```

The secret itself is injected into the pod as an `envFrom` source, so every key in the Secret becomes an environment variable:

```go
// internal/controller/clawagent_controller.go:805-829
// Inject secrets as env vars for ${VAR} substitution in openclaw.json.
// Deduplicate to avoid mounting the same secret twice.
injectedSecrets := map[string]bool{}
injectSecret := func(name string) {
    if name == "" || injectedSecrets[name] {
        return
    }
    injectedSecrets[name] = true
    mainContainer.EnvFrom = append(mainContainer.EnvFrom, corev1.EnvFromSource{
        SecretRef: &corev1.SecretEnvSource{
            LocalObjectReference: corev1.LocalObjectReference{Name: name},
            Optional:             boolPtr(true),
        },
    })
}

// ...

// Channel credential secrets.
for _, ch := range p.Channels {
    injectSecret(ch.Spec.CredentialsSecret)
}
```

Key details:
- Secrets are marked `Optional: true`, so a missing secret will not prevent pod startup.
- Deduplication prevents the same secret from being mounted twice (e.g., if two channels share a credentials secret).
- The `envFrom` approach injects **all** keys from the secret, not just the expected token keys.

### The ${VAR} Placeholder Mechanism

The `openclaw.json` file is a static JSON blob stored in a ConfigMap. It cannot directly contain secret values. Instead, the operator writes `${VAR}` placeholders:

```go
// internal/controller/clawagent_controller.go:1140-1146
chType := strings.ToUpper(ch.Spec.Type)
if clawv1.ChannelsWithBotToken[ch.Spec.Type] {
    chCfg["botToken"] = fmt.Sprintf("${%s_BOT_TOKEN}", chType)
}
if clawv1.ChannelsWithAppToken[ch.Spec.Type] {
    chCfg["appToken"] = fmt.Sprintf("${%s_APP_TOKEN}", chType)
}
```

At runtime, the OpenClaw process resolves these placeholders by reading environment variables from the container. Since the operator mounts the credential Secret via `envFrom`, the variables are available. For example:

- `openclaw.json` contains `"botToken": "${TELEGRAM_BOT_TOKEN}"`
- The pod has env var `TELEGRAM_BOT_TOKEN=<value from Secret telegram-creds>`
- OpenClaw substitutes `${TELEGRAM_BOT_TOKEN}` with the actual token at startup

This means secrets never appear in ConfigMaps or any resource visible via `kubectl get configmap -o yaml`.

### How Enabling/Disabling a Channel Affects the Agent Pod

When a channel's `enabled` field is set to `false`:

1. **Resolution skips it** -- `ch.Spec.IsEnabled()` returns false, so the channel is not added to the `channels` slice (line 158).
2. **No openclaw.json entry** -- because the channel is not in the slice, `buildOpenclawConfig` does not generate a `channels.<type>` block for it.
3. **No secret injection** -- the channel's `credentialsSecret` is not injected as an `envFrom` source.
4. **No plugin enabled** -- the channel plugin is not added to the plugins section:

```go
// internal/controller/clawagent_controller.go:1173-1178
// Auto-enable channel plugins.
for _, ch := range channels {
    pluginEntries[ch.Spec.Type] = map[string]any{
        "enabled": true,
    }
}
```

5. **Not in plugin allow list** -- the channel is excluded from the `plugins.allow` list:

```go
// internal/controller/clawagent_controller.go:1182-1184
pluginAllow := []string{"observeclaw"}
for _, ch := range channels {
    pluginAllow = append(pluginAllow, ch.Spec.Type)
}
```

In short, a disabled channel is completely invisible to the agent -- as if it were never referenced.

### All 13 Supported Channel Types

The CRD validation enum (from `api/v1/clawchannel_types.go:67`) defines exactly 13 channel types:

| Type | Platform | Bot Token | App Token |
|---|---|---|---|
| `telegram` | Telegram Bot API | Yes | No |
| `slack` | Slack (Socket Mode / Events API) | Yes | Yes |
| `discord` | Discord Bot | Yes | No |
| `whatsapp` | WhatsApp Business API | No* | No |
| `matrix` | Matrix (Element, etc.) | No* | No |
| `msteams` | Microsoft Teams | Yes | No |
| `irc` | IRC | No* | No |
| `signal` | Signal Messenger | No* | No |
| `line` | LINE Messaging API | No* | No |
| `feishu` | Feishu / Lark | No* | No |
| `nostr` | Nostr protocol | No* | No |
| `mattermost` | Mattermost | No* | No |
| `googlechat` | Google Chat | No* | No |

\* "No" in the Bot Token / App Token columns means the operator does not automatically generate `${TYPE_BOT_TOKEN}` or `${TYPE_APP_TOKEN}` placeholders. These channel types receive credentials through their `config` map or through custom keys in the referenced secret that the OpenClaw plugin reads directly.

Only the four types in `ChannelsWithBotToken` (telegram, slack, discord, msteams) get automatic `botToken` placeholder generation. Only slack gets `appToken`.

### RBAC Requirements

The agent controller requires read access to ClawChannel resources. This is declared via the kubebuilder RBAC marker:

```go
// internal/controller/clawagent_controller.go:61
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawchannels,verbs=get;list;watch
```

This generates a ClusterRole rule granting `get`, `list`, and `watch` on `clawchannels.claw.clawbernetes.io`. The controller also needs read access to Secrets (line 62):

```go
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
```

Without these permissions, the reconciler would fail when calling `r.Get(ctx, chKey, ch)` to resolve channel references and when Kubernetes attempts to mount the credential Secrets into the pod.

### Status Subresource

ClawChannel defines a status subresource with standard `metav1.Condition` entries:

```go
// api/v1/clawchannel_types.go:95-101
type ClawChannelStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

However, since there is no ClawChannel controller, these conditions are never written to by the operator today. The status subresource exists as a forward-compatibility hook -- a future dedicated controller could use it to report channel health (e.g., "bot token valid", "webhook registered").
