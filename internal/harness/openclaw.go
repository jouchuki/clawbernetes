/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package harness

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

// OpenClawHarness implements the Harness interface for the OpenClaw agent runtime.
type OpenClawHarness struct{}

func (h *OpenClawHarness) Name() string            { return "openclaw" }
func (h *OpenClawHarness) DefaultImage() string    { return "clawbernetes/openclaw:latest" }
func (h *OpenClawHarness) GatewayPort() int32      { return 18789 }
func (h *OpenClawHarness) HomePath() string        { return "/home/node/.openclaw" }
func (h *OpenClawHarness) WorkspacePath() string   { return "/home/node/.openclaw/workspace" }
func (h *OpenClawHarness) ExtensionsPath() string  { return "/home/node/.openclaw/extensions" }
func (h *OpenClawHarness) ConfigFileName() string  { return "openclaw.json" }
func (h *OpenClawHarness) ConfigMapSuffix() string { return "-openclaw-config" }
func (h *OpenClawHarness) ReadinessPath() string   { return "/ready" }
func (h *OpenClawHarness) LivenessPath() string    { return "/health" }
func (h *OpenClawHarness) ContainerName() string   { return "openclaw" }

func (h *OpenClawHarness) CopyExtensionsCommands() []string {
	return DefaultCopyExtensionsCommands(h.HomePath())
}

func (h *OpenClawHarness) SeedCommands() []string {
	return DefaultSeedCommands(h.ConfigFileName())
}

// BuildConfig generates the full openclaw.json for the agent.
func (h *OpenClawHarness) BuildConfig(input ConfigInput) (string, error) {
	agent := input.Agent
	name := input.Name
	ns := input.Namespace
	gatewayURL := input.GatewayURL
	otlpEndpoint := input.OTLPEndpoint
	policy := input.Policy
	gateway := input.Gateway
	channels := input.Channels

	gatewayPort := h.GatewayPort()

	cfg := map[string]any{
		"gateway": map[string]any{
			"port": gatewayPort,
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
					fmt.Sprintf("http://localhost:%d", gatewayPort),
					fmt.Sprintf("http://127.0.0.1:%d", gatewayPort),
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{
				"workspace": h.WorkspacePath(),
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

	// Set default model if provider and name are specified.
	if agent.Spec.Model.Provider != "" && agent.Spec.Model.Name != "" {
		defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
		defaults["model"] = map[string]any{
			"primary": fmt.Sprintf("%s/%s", agent.Spec.Model.Provider, agent.Spec.Model.Name),
		}
	}

	// --- diagnostics-otel: built-in extension in the orq-ai/openclaw fork ---
	if otlpEndpoint != "" {
		otelCfg := map[string]any{
			"enabled":     true,
			"endpoint":    otlpEndpoint,
			"protocol":    "http/protobuf",
			"serviceName": name,
			"traces":      true,
			"metrics":     true,
			"logs":        true,
			"sampleRate":  1.0,
		}

		// Wire TelemetryCaptureSpec — default everything on, let spec override.
		tc := agent.Spec.TelemetryCapture
		captureContent := map[string]any{
			"inputMessages":      true,
			"outputMessages":     true,
			"systemInstructions": true,
			"toolDefinitions":    true,
			"toolContent":        true,
		}
		// If any field is explicitly set on the spec, use those values instead.
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

		cfg["diagnostics"] = map[string]any{
			"enabled": true,
			"otel":    otelCfg,
		}
	}

	// --- Register model providers ---
	providers := map[string]any{}

	// Register a gateway-proxied Anthropic provider if gateway is configured.
	if gatewayURL != "" {
		providers["gateway-anthropic"] = map[string]any{
			"baseUrl": gatewayURL,
			"api":     "anthropic-messages",
			"apiKey":  "gateway-managed", // sentinel — gateway injects the real key server-side
			"models": []map[string]any{
				{
					"id":            "claude-sonnet-4-6",
					"name":          "Claude Sonnet 4.6 (via gateway)",
					"reasoning":     true,
					"input":         []string{"text"},
					"contextWindow": 200000,
					"maxTokens":     16384,
				},
				{
					"id":            "claude-haiku-4-5",
					"name":          "Claude Haiku 4.5 (via gateway)",
					"reasoning":     false,
					"input":         []string{"text"},
					"contextWindow": 200000,
					"maxTokens":     8192,
				},
			},
		}
	}

	// Register direct providers based on agent model spec.
	// Uses ${<PROVIDER_UPPER>_API_KEY} env var for credentials.
	if agent.Spec.Model.Provider != "" {
		providerName := agent.Spec.Model.Provider
		apiFormat := clawv1.ProviderAPIFormats[providerName]
		if apiFormat == "" {
			apiFormat = "openai-responses" // safe default for unknown providers
		}
		baseURL := clawv1.ProviderBaseURLs[providerName]
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		envVar := fmt.Sprintf("${%s_API_KEY}", strings.ToUpper(strings.ReplaceAll(providerName, "-", "_")))
		providers[providerName] = map[string]any{
			"baseUrl": baseURL,
			"api":     apiFormat,
			"apiKey":  envVar,
			"models":  []map[string]any{},
		}
	}

	if len(providers) > 0 {
		cfg["models"] = map[string]any{"providers": providers}
	}

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

	// --- Build observeclaw plugin config from ClawPolicy + ClawGateway ---
	observeclawCfg := h.buildObserveclawConfig(agent, name, gatewayURL, policy, gateway)

	pluginEntries := map[string]any{
		"observeclaw": map[string]any{
			"enabled": true,
			"config":  observeclawCfg,
		},
	}

	// Enable the bundled diagnostics-otel extension.
	if otlpEndpoint != "" {
		pluginEntries["diagnostics-otel"] = map[string]any{
			"enabled": true,
		}
	}

	// Auto-enable channel plugins.
	for _, ch := range channels {
		pluginEntries[ch.Spec.Type] = map[string]any{
			"enabled": true,
		}
	}

	// --- A2A gateway plugin configuration ---
	// Build plugins.allow — must include ALL active plugins or OpenClaw blocks them.
	pluginAllow := []string{"observeclaw"}
	for _, ch := range channels {
		pluginAllow = append(pluginAllow, ch.Spec.Type)
	}
	if agent.Spec.A2A.Enabled {
		a2aPort := agent.Spec.A2A.ResolvedPort()
		cardName := agent.Spec.A2A.AgentCardName
		if cardName == "" {
			cardName = name
		}
		cardDesc := agent.Spec.A2A.AgentCardDescription
		if cardDesc == "" {
			cardDesc = fmt.Sprintf("Clawbernetes agent: %s", name)
		}

		// Build skills list for Agent Card.
		a2aSkills := []map[string]any{}
		for _, s := range agent.Spec.A2A.Skills {
			a2aSkills = append(a2aSkills, map[string]any{
				"id": s, "name": s, "description": s,
			})
		}
		if len(a2aSkills) == 0 {
			a2aSkills = append(a2aSkills, map[string]any{
				"id": "chat", "name": "chat", "description": "Chat bridge",
			})
		}

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

		// Security: use ${A2A_TOKEN} from env var if security secret is configured.
		if agent.Spec.A2A.SecurityTokenSecret != "" {
			a2aCfg["security"] = map[string]any{
				"inboundAuth": "bearer",
				"token":       "${A2A_TOKEN}",
			}
		}

		// Build peers list with ${PEER_<NAME>_TOKEN} env var placeholders.
		if len(agent.Spec.A2A.Peers) > 0 {
			peers := []map[string]any{}
			for _, p := range agent.Spec.A2A.Peers {
				peer := map[string]any{
					"name":         p.Name,
					"agentCardUrl": p.AgentCardURL,
				}
				if p.CredentialsSecret != "" {
					envVar := fmt.Sprintf("${PEER_%s_TOKEN}", strings.ToUpper(strings.ReplaceAll(p.Name, "-", "_")))
					peer["auth"] = map[string]any{
						"type":  "bearer",
						"token": envVar,
					}
				}
				peers = append(peers, peer)
			}
			a2aCfg["peers"] = peers
		}

		pluginEntries["a2a-gateway"] = map[string]any{
			"enabled": true,
			"config":  a2aCfg,
		}
		pluginAllow = append(pluginAllow, "a2a-gateway")
	}

	pluginsCfg := map[string]any{
		"enabled": true,
		"entries": pluginEntries,
		"allow":   pluginAllow,
	}
	// Tell OpenClaw where to find the a2a-gateway plugin if A2A is enabled.
	if agent.Spec.A2A.Enabled {
		pluginsCfg["load"] = map[string]any{
			"paths": []string{h.WorkspacePath() + "/plugins/a2a-gateway"},
		}
	}
	cfg["plugins"] = pluginsCfg

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling openclaw config: %w", err)
	}
	return string(b), nil
}

// buildObserveclawConfig maps ClawPolicy + ClawGateway CRD fields to the
// observeclaw plugin configSchema.
func (h *OpenClawHarness) buildObserveclawConfig(agent *clawv1.ClawAgent, agentName, gatewayURL string, policy *clawv1.ClawPolicy, gateway *clawv1.ClawGateway) map[string]any {
	cfg := map[string]any{
		"enabled":  true,
		"currency": "USD",
	}

	// --- Budgets from ClawPolicy ---
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
			h.HomePath()+"/credentials/*",
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

	// --- Anomaly detection from ClawGateway ---
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

	// --- Routing: proxy all LLM traffic through ClawGateway ---
	evaluators := []map[string]any{}

	if gateway != nil {
		// Map CRD evaluators to observeclaw evaluator config.
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
			if ev.EmitEvent {
				entry["emitEvent"] = true
			}
			if ev.ClassifierModel != "" {
				entry["classifierModel"] = ev.ClassifierModel
			}
			if ev.TimeoutMs > 0 {
				entry["timeoutMs"] = ev.TimeoutMs
			}
			if ev.RedactReplacement != "" {
				entry["redactReplacement"] = ev.RedactReplacement
			}
			if ev.ProxyURL != "" {
				entry["proxyUrl"] = ev.ProxyURL
			}
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
	}

	// Catch-all proxy: route all LLM traffic through the ClawGateway.
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

	cfg["routing"] = map[string]any{
		"enabled":    len(evaluators) > 0,
		"logRouting": gateway != nil && gateway.Spec.Routing.LogEveryDecision,
		"evaluators": evaluators,
	}

	// --- Webhooks from ClawGateway ---
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

	return cfg
}
