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
	"strings"
	"testing"

	clawv1 "github.com/clawbernetes/operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestForType verifies the registry returns the correct harness for each type.
func TestForType(t *testing.T) {
	tests := []struct {
		htype    clawv1.HarnessType
		wantName string
	}{
		{clawv1.HarnessOpenClaw, "openclaw"},
		{clawv1.HarnessObserveClaw, "observeclaw"},
		{clawv1.HarnessHermes, "hermes"},
		{"", "openclaw"},        // empty defaults to openclaw
		{"unknown", "openclaw"}, // unknown defaults to openclaw
	}
	for _, tt := range tests {
		h := ForType(tt.htype)
		if h.Name() != tt.wantName {
			t.Errorf("ForType(%q).Name() = %q, want %q", tt.htype, h.Name(), tt.wantName)
		}
	}
}

// TestHarnessProperties verifies each harness returns consistent, non-empty values.
func TestHarnessProperties(t *testing.T) {
	harnesses := []Harness{
		&OpenClawHarness{},
		&ObserveClawHarness{},
		&HermesHarness{},
	}

	for _, h := range harnesses {
		t.Run(h.Name(), func(t *testing.T) {
			if h.DefaultImage() == "" {
				t.Error("DefaultImage() is empty")
			}
			if h.GatewayPort() <= 0 {
				t.Errorf("GatewayPort() = %d, want > 0", h.GatewayPort())
			}
			if h.HomePath() == "" {
				t.Error("HomePath() is empty")
			}
			if !strings.HasPrefix(h.WorkspacePath(), h.HomePath()) {
				t.Errorf("WorkspacePath() %q should be under HomePath() %q", h.WorkspacePath(), h.HomePath())
			}
			if !strings.HasPrefix(h.ExtensionsPath(), h.HomePath()) {
				t.Errorf("ExtensionsPath() %q should be under HomePath() %q", h.ExtensionsPath(), h.HomePath())
			}
			if h.ConfigFileName() == "" {
				t.Error("ConfigFileName() is empty")
			}
			if h.ConfigMapSuffix() == "" {
				t.Error("ConfigMapSuffix() is empty")
			}
			if !strings.HasPrefix(h.ConfigMapSuffix(), "-") {
				t.Errorf("ConfigMapSuffix() %q should start with '-'", h.ConfigMapSuffix())
			}
			// ReadinessPath/LivenessPath may be empty for harnesses that use
			// exec probes instead of HTTP (e.g. Hermes has no HTTP health endpoint).
			if h.ContainerName() == "" {
				t.Error("ContainerName() is empty")
			}
			if cmds := h.CopyExtensionsCommands(); len(cmds) == 0 {
				t.Error("CopyExtensionsCommands() returned empty slice")
			}
			if cmds := h.SeedCommands(); len(cmds) == 0 {
				t.Error("SeedCommands() returned empty slice")
			}
		})
	}
}

// TestOpenClawDefaultImage verifies OpenClaw points to the published ghcr.io image.
func TestOpenClawDefaultImage(t *testing.T) {
	h := &OpenClawHarness{}
	if got := h.DefaultImage(); got != "ghcr.io/openclaw/openclaw:latest" {
		t.Errorf("DefaultImage() = %q, want ghcr.io/openclaw/openclaw:latest", got)
	}
}

// TestHermesDefaultImage verifies Hermes points to the published Docker Hub image.
func TestHermesDefaultImage(t *testing.T) {
	h := &HermesHarness{}
	if got := h.DefaultImage(); got != "nousresearch/hermes-agent:latest" {
		t.Errorf("DefaultImage() = %q, want nousresearch/hermes-agent:latest", got)
	}
	if got := h.GatewayPort(); got != 8642 {
		t.Errorf("GatewayPort() = %d, want 8642", got)
	}
	if got := h.HomePath(); got != "/opt/data" {
		t.Errorf("HomePath() = %q, want /opt/data", got)
	}
	if got := h.ReadinessPath(); got != "" {
		t.Errorf("ReadinessPath() = %q, want empty (exec probe)", got)
	}
	if got := h.ConfigFileName(); got != "config.yaml" {
		t.Errorf("ConfigFileName() = %q, want config.yaml", got)
	}
}

// TestObserveClawDefaults verifies ObserveClaw uses the custom-built image and inherits OpenClaw behavior.
func TestObserveClawDefaults(t *testing.T) {
	h := &ObserveClawHarness{}
	if got := h.DefaultImage(); got != "clawbernetes/openclaw:latest" {
		t.Errorf("DefaultImage() = %q, want clawbernetes/openclaw:latest", got)
	}
	if got := h.Name(); got != "observeclaw" {
		t.Errorf("Name() = %q, want observeclaw", got)
	}
	if got := h.ContainerName(); got != "observeclaw" {
		t.Errorf("ContainerName() = %q, want observeclaw", got)
	}
	// Should inherit OpenClaw's port, paths, and probes
	if got := h.GatewayPort(); got != 18789 {
		t.Errorf("GatewayPort() = %d, want 18789 (inherited from OpenClaw)", got)
	}
	if got := h.ReadinessPath(); got != "/ready" {
		t.Errorf("ReadinessPath() = %q, want /ready (inherited from OpenClaw)", got)
	}
	if got := h.HomePath(); got != "/home/node/.openclaw" {
		t.Errorf("HomePath() = %q, want /home/node/.openclaw (inherited from OpenClaw)", got)
	}
}

// TestOpenClawBuildConfig verifies the generated openclaw.json has the expected structure.
func TestOpenClawBuildConfig(t *testing.T) {
	h := &OpenClawHarness{}
	input := ConfigInput{
		Agent: &clawv1.ClawAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-agent", Namespace: "default"},
			Spec: clawv1.ClawAgentSpec{
				Model: clawv1.AgentModelSpec{
					Provider: "anthropic",
					Name:     "claude-sonnet-4-6",
				},
			},
		},
		Name:      "test-agent",
		Namespace: "default",
	}

	raw, err := h.BuildConfig(input)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("BuildConfig() produced invalid JSON: %v", err)
	}

	// Verify gateway section exists with correct port.
	gw, ok := cfg["gateway"].(map[string]any)
	if !ok {
		t.Fatal("missing 'gateway' section in config")
	}
	if port, ok := gw["port"].(float64); !ok || int32(port) != h.GatewayPort() {
		t.Errorf("gateway.port = %v, want %d", gw["port"], h.GatewayPort())
	}

	// Verify agents section exists with correct agent ID.
	agents, ok := cfg["agents"].(map[string]any)
	if !ok {
		t.Fatal("missing 'agents' section in config")
	}
	list, ok := agents["list"].([]any)
	if !ok || len(list) == 0 {
		t.Fatal("missing 'agents.list' in config")
	}
	first := list[0].(map[string]any)
	if first["id"] != "test-agent" {
		t.Errorf("agents.list[0].id = %q, want 'test-agent'", first["id"])
	}

	// Verify model provider was registered.
	models, ok := cfg["models"].(map[string]any)
	if !ok {
		t.Fatal("missing 'models' section in config")
	}
	providers, ok := models["providers"].(map[string]any)
	if !ok {
		t.Fatal("missing 'models.providers' in config")
	}
	if _, ok := providers["anthropic"]; !ok {
		t.Error("expected 'anthropic' provider in models.providers")
	}

	// Verify plugins section exists.
	if _, ok := cfg["plugins"]; !ok {
		t.Error("missing 'plugins' section in config")
	}
}

// TestOpenClawBuildConfigWithOTLP verifies diagnostics are injected when OTLP is set.
func TestOpenClawBuildConfigWithOTLP(t *testing.T) {
	h := &OpenClawHarness{}
	input := ConfigInput{
		Agent: &clawv1.ClawAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "otel-agent", Namespace: "default"},
			Spec:       clawv1.ClawAgentSpec{},
		},
		Name:         "otel-agent",
		Namespace:    "default",
		OTLPEndpoint: "http://tempo.observability:4318",
	}

	raw, err := h.BuildConfig(input)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	diag, ok := cfg["diagnostics"].(map[string]any)
	if !ok {
		t.Fatal("missing 'diagnostics' section when OTLPEndpoint is set")
	}
	otel, ok := diag["otel"].(map[string]any)
	if !ok {
		t.Fatal("missing 'diagnostics.otel' section")
	}
	if otel["endpoint"] != "http://tempo.observability:4318" {
		t.Errorf("otel.endpoint = %q, want 'http://tempo.observability:4318'", otel["endpoint"])
	}
}

// TestHermesBuildConfig verifies Hermes config.yaml has the expected structure.
func TestHermesBuildConfig(t *testing.T) {
	h := &HermesHarness{}
	input := ConfigInput{
		Agent: &clawv1.ClawAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "hermes-test", Namespace: "default"},
			Spec: clawv1.ClawAgentSpec{
				Model: clawv1.AgentModelSpec{
					Provider: "anthropic",
					Name:     "claude-sonnet-4-6",
				},
			},
		},
		Name:      "hermes-test",
		Namespace: "default",
	}

	raw, err := h.BuildConfig(input)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	// Verify YAML config contains expected sections
	if !strings.Contains(raw, "provider: anthropic") {
		t.Error("missing 'provider: anthropic' in Hermes config")
	}
	if !strings.Contains(raw, "model: claude-sonnet-4-6") {
		t.Error("missing 'model: claude-sonnet-4-6' in Hermes config")
	}
	if !strings.Contains(raw, "port: 8642") {
		t.Error("missing 'port: 8642' in Hermes config")
	}
	if !strings.Contains(raw, "backend: local") {
		t.Error("missing 'backend: local' terminal setting")
	}
	if !strings.Contains(raw, "${ANTHROPIC_API_KEY}") {
		t.Error("missing API key env var reference")
	}
}

// TestCopyExtensionsCommandsNotEmpty verifies each harness has copy-extensions commands.
func TestCopyExtensionsCommandsNotEmpty(t *testing.T) {
	harnesses := []Harness{&OpenClawHarness{}, &ObserveClawHarness{}, &HermesHarness{}}
	for _, h := range harnesses {
		t.Run(h.Name(), func(t *testing.T) {
			cmds := h.CopyExtensionsCommands()
			if len(cmds) == 0 {
				t.Error("CopyExtensionsCommands() returned empty slice")
			}
		})
	}
}

// TestOpenClawCopyExtensionsReferencesHome verifies OpenClaw specifically copies from its home path.
func TestOpenClawCopyExtensionsReferencesHome(t *testing.T) {
	h := &OpenClawHarness{}
	joined := strings.Join(h.CopyExtensionsCommands(), "\n")
	if !strings.Contains(joined, h.HomePath()) {
		t.Errorf("OpenClaw CopyExtensionsCommands() should reference HomePath %q", h.HomePath())
	}
}

// TestSeedCommandsContainConfigFile verifies seed commands reference the config file name.
func TestSeedCommandsContainConfigFile(t *testing.T) {
	harnesses := []Harness{&OpenClawHarness{}, &ObserveClawHarness{}, &HermesHarness{}}
	for _, h := range harnesses {
		t.Run(h.Name(), func(t *testing.T) {
			cmds := h.SeedCommands()
			joined := strings.Join(cmds, "\n")
			if !strings.Contains(joined, h.ConfigFileName()) {
				t.Errorf("SeedCommands() should reference ConfigFileName %q", h.ConfigFileName())
			}
		})
	}
}

// TestDistinctRuntimePorts verifies harnesses with different runtimes use distinct ports.
// ObserveClaw intentionally shares OpenClaw's port since it's the same runtime.
func TestDistinctRuntimePorts(t *testing.T) {
	openclaw := &OpenClawHarness{}
	hermes := &HermesHarness{}
	if openclaw.GatewayPort() == hermes.GatewayPort() {
		t.Errorf("openclaw and hermes should use different ports, both use %d", openclaw.GatewayPort())
	}
	observeclaw := &ObserveClawHarness{}
	if observeclaw.GatewayPort() != openclaw.GatewayPort() {
		t.Errorf("observeclaw should share openclaw's port, got %d vs %d", observeclaw.GatewayPort(), openclaw.GatewayPort())
	}
}
