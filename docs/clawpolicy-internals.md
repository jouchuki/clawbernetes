# ClawPolicy CRD Internals

## Section 1: Technical Summary

### What ClawPolicy Is

ClawPolicy is a namespaced Custom Resource Definition in the Clawbernetes operator that defines **budget limits** and **tool guardrails** for AI agents. It acts as a governance primitive: you declare spending thresholds and tool allow/deny lists in a ClawPolicy resource, then reference that policy from one or more ClawAgent resources. The operator reads the policy at reconciliation time and injects the constraints into the agent's runtime configuration.

ClawPolicy itself has **no controller** -- it is a passive data resource. The `ClawAgentReconciler` consumes it.

**CRD metadata:**

| Field | Value |
|---|---|
| Group | `claw.clawbernetes.io` |
| Version | `v1` |
| Kind | `ClawPolicy` |
| Plural | `clawpolicies` |
| Scope | `Namespaced` |
| Subresources | `status` |

### Spec Fields Reference

The Go types live in `api/v1/clawpolicy_types.go`.

#### `spec.budget` (type: `BudgetSpec`)

| Field | Go Type | JSON Key | Description |
|---|---|---|---|
| `daily` | `int` | `daily` | USD daily spend limit. When total spend for the day reaches this value, the gateway blocks further requests. |
| `monthly` | `int` | `monthly` | USD monthly spend limit. Same enforcement as daily but over a calendar month. |
| `warnAt` | `string` | `warnAt` | Fraction of the budget (e.g. `"0.9"`) at which the system triggers a model downgrade instead of a hard cutoff. Parsed to `float64` at reconciliation time. |
| `downgradeModel` | `string` | `downgradeModel` | The model ID to switch to when the `warnAt` threshold is breached (e.g. `claude-haiku-4-5`). |
| `downgradeProvider` | `string` | `downgradeProvider` | The provider for the downgrade model (e.g. `anthropic`). |

All fields are optional. When omitted, the operator applies defaults: daily=100, monthly=2000, warnAt=0.8, downgradeModel=`claude-haiku-4-5`, downgradeProvider=`anthropic`.

Go struct (`api/v1/clawpolicy_types.go:35-55`):

```go
type BudgetSpec struct {
	Daily             int    `json:"daily,omitempty"`
	Monthly           int    `json:"monthly,omitempty"`
	WarnAt            string `json:"warnAt,omitempty"`
	DowngradeModel    string `json:"downgradeModel,omitempty"`
	DowngradeProvider string `json:"downgradeProvider,omitempty"`
}
```

#### `spec.toolPolicy` (type: `ToolPolicySpec`)

| Field | Go Type | JSON Key | Description |
|---|---|---|---|
| `allow` | `[]string` | `allow` | Whitelist of permitted tool names. If non-empty, only these tools may be invoked. |
| `deny` | `[]string` | `deny` | Blacklist of denied tool names. These tools are blocked regardless of the allow list. |

Go struct (`api/v1/clawpolicy_types.go:24-32`):

```go
type ToolPolicySpec struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}
```

#### `status.conditions`

Standard Kubernetes `[]metav1.Condition` with list-map semantics keyed on `type`. Currently no controller sets conditions on ClawPolicy -- this field exists for future use or external tooling.

### Full YAML Example

```yaml
# A policy that caps daily spend at $10, monthly at $200,
# downgrades to Haiku at 90% budget consumption,
# and blocks three dangerous tool patterns.
apiVersion: claw.clawbernetes.io/v1
kind: ClawPolicy
metadata:
  name: engineering-policy          # <-- referenced by ClawAgent spec.policy
  namespace: clawbernetes
  labels:
    team: engineering
spec:
  toolPolicy:
    deny:
      - rm_rf                       # block recursive delete
      - drop_database               # block database destruction
      - kubectl_delete_namespace    # block namespace deletion
    # allow: []                     # omitted = all tools allowed (minus deny list)
  budget:
    daily: 10                       # USD per day
    monthly: 200                    # USD per month
    warnAt: "0.9"                   # at 90% of budget, downgrade model
    downgradeModel: claude-haiku-4-5
    downgradeProvider: anthropic
```

### How an Agent References a Policy

On the ClawAgent spec, the `policy` field is a string containing the **name** of a ClawPolicy in the **same namespace**.

From `api/v1/clawagent_types.go:199-201`:

```go
	// policy references a ClawPolicy resource by name.
	// +optional
	Policy string `json:"policy,omitempty"`
```

Example ClawAgent YAML (excerpt):

```yaml
apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: code-reviewer
  namespace: clawbernetes
spec:
  policy: engineering-policy      # <-- same namespace, same name as the ClawPolicy above
  gateway: main-gateway
  model:
    provider: anthropic
    name: claude-sonnet-4-6
```

### How Budget Fields Flow into the Observeclaw Plugin Config

The agent reconciler calls `buildObserveclawConfig` which produces a `map[string]any` that becomes the `config` value under the `observeclaw` plugin entry in `openclaw.json`.

From `internal/controller/clawagent_controller.go:1156-1163`:

```go
	// --- Build observeclaw plugin config from ClawPolicy + ClawGateway ---
	observeclawCfg := r.buildObserveclawConfig(agent, name, gatewayURL, policy, gateway)

	pluginEntries := map[string]any{
		"observeclaw": map[string]any{
			"enabled": true,
			"config":  observeclawCfg,
		},
	}
```

Inside `buildObserveclawConfig`, budget fields are mapped (`internal/controller/clawagent_controller.go:1288-1322`):

```go
	budgetDefaults := map[string]any{
		"daily":   100,
		"monthly": 2000,
		"warnAt":  0.8,
	}
	downgradeModel := "claude-haiku-4-5"
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

### How Tool Allow/Deny Lists Flow into the Plugin Config

From `internal/controller/clawagent_controller.go:1324-1357`:

```go
	// --- Tool policy from ClawPolicy ---
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
	// Auto-deny credential file access to prevent the LLM from
	// exfiltrating mounted integration secrets via file tools.
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

	cfg["toolPolicy"] = map[string]any{
		"defaults": toolDefaults,
		"agents":   map[string]any{},
	}
```

---

## Section 2: Kubernetes Internals Deep Dive

### ClawPolicy Has No Dedicated Controller

ClawPolicy is registered with the scheme (`api/v1/clawpolicy_types.go:106-108`):

```go
func init() {
	SchemeBuilder.Register(&ClawPolicy{}, &ClawPolicyList{})
}
```

But there is **no** `ClawPolicyReconciler`. No controller watches for changes to ClawPolicy resources. It is a purely passive data resource -- a configuration object that is read by the `ClawAgentReconciler` during its reconciliation loop.

This means that if you update a ClawPolicy, the change is **not** automatically propagated to agents. The agent reconciler must be triggered independently (e.g., by modifying the ClawAgent, restarting the controller, or through a future enhancement that watches ClawPolicy changes).

Looking at `SetupWithManager` (`internal/controller/clawagent_controller.go:1537-1545`):

```go
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

The controller watches ClawAgent, Deployment, ConfigMap, and Service. It does **not** `Watches(&clawv1.ClawPolicy{}, ...)` -- so changes to a ClawPolicy alone do not trigger re-reconciliation.

### How the Agent Reconciler Resolves the Policy by Name

The reconciler fetches the policy during the main `Reconcile` function using a simple `Get` with a `NamespacedName` constructed from the agent's `spec.policy` field and the agent's own namespace.

From `internal/controller/clawagent_controller.go:133-146`:

```go
	// --- Resolve ClawPolicy if referenced ---
	var policy *clawv1.ClawPolicy
	if agent.Spec.Policy != "" {
		pol := &clawv1.ClawPolicy{}
		polKey := types.NamespacedName{Name: agent.Spec.Policy, Namespace: ns}
		if err := r.Get(ctx, polKey, pol); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("referenced ClawPolicy not found", "name", agent.Spec.Policy)
		} else {
			policy = pol
		}
	}
```

Key behaviors:
- If `spec.policy` is empty, `policy` stays `nil` and defaults apply everywhere downstream.
- If the named ClawPolicy does not exist, the reconciler logs a warning and continues with `policy = nil` (soft failure, not a hard error).
- If the API server returns a non-NotFound error (e.g. RBAC denied, network failure), reconciliation is retried.

The resolved `policy` pointer is then threaded through `openclawConfigMap` -> `buildOpenclawConfig` -> `buildObserveclawConfig`.

### The buildObserveclawConfig Function

This is the core function that translates CRD fields into the observeclaw plugin's JSON configuration schema. It lives at `internal/controller/clawagent_controller.go:1281`.

**Signature:**

```go
func (r *ClawAgentReconciler) buildObserveclawConfig(
    agent *clawv1.ClawAgent,
    agentName, gatewayURL string,
    policy *clawv1.ClawPolicy,
    gateway *clawv1.ClawGateway,
) map[string]any
```

**What it produces (output structure):**

```json
{
  "enabled": true,
  "currency": "USD",
  "budgets": {
    "defaults": { "daily": 10, "monthly": 200, "warnAt": 0.9 },
    "agents": {}
  },
  "downgradeModel": "claude-haiku-4-5",
  "downgradeProvider": "anthropic",
  "toolPolicy": {
    "defaults": { "allow": [], "deny": ["rm_rf", "drop_database", ...] },
    "agents": {}
  },
  "anomaly": { ... }
}
```

The function is `nil`-safe on both `policy` and `gateway` -- it always produces a valid config with sensible defaults.

### How Budget Fields Map to Observeclaw Budget Config

The mapping is a direct translation with default fallbacks. From `internal/controller/clawagent_controller.go:1288-1322`:

| CRD Field (`spec.budget.*`) | Default Value | Observeclaw Config Key |
|---|---|---|
| `daily` | `100` | `budgets.defaults.daily` |
| `monthly` | `2000` | `budgets.defaults.monthly` |
| `warnAt` | `0.8` | `budgets.defaults.warnAt` |
| `downgradeModel` | `"claude-haiku-4-5"` | `downgradeModel` (top-level) |
| `downgradeProvider` | `"anthropic"` | `downgradeProvider` (top-level) |

The `warnAt` field is stored as a string in the CRD (to avoid floating-point issues in Kubernetes API validation) but is parsed to `float64` at reconciliation time via `strconv.ParseFloat`. If parsing fails, the default of `0.8` is preserved silently.

The `budgets.agents` key is always an empty map -- per-agent budget overrides within a single policy are not yet implemented; the `defaults` block applies uniformly.

### How Tool Allow/Deny Lists Are Serialized

The tool policy goes through a two-stage process (`internal/controller/clawagent_controller.go:1324-1357`):

**Stage 1: Copy from ClawPolicy.** If `policy` is non-nil and the allow/deny slices are non-empty, they replace the empty defaults.

**Stage 2: Auto-deny credential patterns.** If the agent has a `credentialsSecret` configured, the controller appends glob and regex patterns to the deny list that block the LLM from reading mounted credential files:

```go
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

This means the final deny list in the observeclaw config is the **union** of whatever the ClawPolicy specifies plus these auto-injected credential protection patterns. The allow list is passed through as-is.

Like budgets, `toolPolicy.agents` is always an empty map -- per-agent overrides within a policy are not implemented.

### How warnAt + downgradeModel Triggers Automatic Model Switching

The operator itself does not perform the model switch at runtime. Instead, it **configures** the observeclaw plugin with the `downgradeModel`, `downgradeProvider`, and `warnAt` values. The observeclaw plugin (running as an OpenClaw extension inside the agent container) monitors spend in real time and performs the switch when the threshold is breached.

The flow:

1. The operator writes `openclaw.json` with the observeclaw plugin config containing `budgets.defaults.warnAt: 0.9`, `downgradeModel: "claude-haiku-4-5"`, and `downgradeProvider: "anthropic"`.
2. At runtime, the observeclaw plugin tracks cumulative spend against the `daily` and `monthly` limits.
3. When spend reaches `warnAt` fraction of either limit (e.g. $9 out of $10 daily), the plugin switches the active model to the downgrade model.
4. This is a "soft" enforcement -- the agent continues operating but on a cheaper model. Hard enforcement (blocking requests entirely) happens when the actual limit is reached.

The operator ensures the downgrade model is registered as a provider. Looking at `internal/controller/clawagent_controller.go:1082-1104`, the `gateway-anthropic` provider always registers both `claude-sonnet-4-6` and `claude-haiku-4-5`, so the downgrade target is available for the observeclaw plugin to switch to.

### How Budget Limits Interact with Gateway Anomaly Detection

The observeclaw config also includes an `anomaly` section derived from the ClawGateway (not ClawPolicy). From `internal/controller/clawagent_controller.go:1359-1385`:

```go
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
		// ... remaining fields ...
	}
	cfg["anomaly"] = anomalyCfg
```

The anomaly detection and budget limits are **complementary**:

- **Budget limits** (from ClawPolicy) define absolute spend ceilings and the warning threshold for model downgrade.
- **Anomaly detection** (from ClawGateway) detects abnormal patterns relative to baseline: a sudden 3x spike in spend rate (`spendSpikeMultiplier`), continued billing while the agent is idle (`idleBurnMinutes`), rapid error loops (`errorLoopThreshold`), or unexpected token count inflation (`tokenInflationMultiplier`).

Both are serialized into the same observeclaw plugin config and enforced by the same runtime plugin, but they trigger different actions: budget limits trigger model downgrade or hard block, while anomaly detection triggers alerts or circuit-breaker responses.

### How the Policy Is Also Used in the Fleet Dashboard

Beyond the agent config, the reconciler also reads ClawPolicy when building the fleet dashboard HTML. From `internal/controller/clawagent_controller.go:278-287`:

```go
		// Resolve policy for budget/tool info
		if a.Spec.Policy != "" {
			pol := &clawv1.ClawPolicy{}
			if err := r.Get(ctx, types.NamespacedName{Name: a.Spec.Policy, Namespace: ns}, pol); err == nil {
				info.BudgetDaily = pol.Spec.Budget.Daily
				info.BudgetMonthly = pol.Spec.Budget.Monthly
				info.DowngradeModel = pol.Spec.Budget.DowngradeModel
				info.ToolDeny = pol.Spec.ToolPolicy.Deny
			}
		}
```

This reads `Budget.Daily`, `Budget.Monthly`, `Budget.DowngradeModel`, and `ToolPolicy.Deny` from the policy and displays them in the fleet overview dashboard.

### RBAC

The agent controller declares its RBAC requirements via kubebuilder markers. From `internal/controller/clawagent_controller.go:59`:

```go
// +kubebuilder:rbac:groups=claw.clawbernetes.io,resources=clawpolicies,verbs=get;list;watch
```

This generates an entry in `config/rbac/role.yaml` that grants the controller's service account read-only access to ClawPolicy resources:

```yaml
- apiGroups:
  - claw.clawbernetes.io
  resources:
  - clawchannels
  - clawpolicies
  - clawskillsets
  verbs:
  - get
  - list
  - watch
```

The controller only needs `get` (for resolving a single policy by name), `list` (for fleet dashboard iteration), and `watch` (for cache population). It never mutates ClawPolicy resources.

Additionally, Kubebuilder generates three aggregate ClusterRoles in `config/rbac/`:

| File | Role | Permissions |
|---|---|---|
| `clawpolicy_viewer_role.yaml` | `clawpolicy-viewer-role` | `get`, `list`, `watch` |
| `clawpolicy_editor_role.yaml` | `clawpolicy-editor-role` | `create`, `delete`, `get`, `list`, `patch`, `update`, `watch` |
| `clawpolicy_admin_role.yaml` | `clawpolicy-admin-role` | Same as editor + status subresource access |

### Architecture Implication: No Watch Means No Auto-Propagation

Because `SetupWithManager` does not include a `Watches` call for ClawPolicy, editing a ClawPolicy after initial agent deployment will **not** automatically update the agent's openclaw.json ConfigMap. The ConfigMap is only regenerated when the ClawAgent itself is reconciled. To force propagation of policy changes, you must either:

1. Touch the ClawAgent resource (e.g. add an annotation).
2. Restart the controller pod.
3. Add a `Watches(&clawv1.ClawPolicy{}, ...)` with an `EnqueueRequestsFromMapFunc` that maps policy changes to the agents that reference them (future enhancement).
