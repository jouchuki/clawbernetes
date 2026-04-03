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
)

// HermesHarness implements the Harness interface for the Hermes agent runtime.
type HermesHarness struct{}

func (h *HermesHarness) Name() string            { return "hermes" }
func (h *HermesHarness) DefaultImage() string    { return "clawbernetes/hermes:latest" }
func (h *HermesHarness) GatewayPort() int32      { return 8080 }
func (h *HermesHarness) HomePath() string        { return "/home/hermes/.hermes" }
func (h *HermesHarness) WorkspacePath() string   { return "/home/hermes/.hermes/workspace" }
func (h *HermesHarness) ExtensionsPath() string  { return "/home/hermes/.hermes/extensions" }
func (h *HermesHarness) ConfigFileName() string  { return "hermes.yaml" }
func (h *HermesHarness) ConfigMapSuffix() string { return "-hermes-config" }
func (h *HermesHarness) ReadinessPath() string   { return "/healthz" }
func (h *HermesHarness) LivenessPath() string    { return "/healthz" }
func (h *HermesHarness) ContainerName() string   { return "hermes" }

func (h *HermesHarness) CopyExtensionsCommands() []string {
	return DefaultCopyExtensionsCommands(h.HomePath())
}

func (h *HermesHarness) SeedCommands() []string {
	return DefaultSeedCommands(h.ConfigFileName())
}

// BuildConfig generates a placeholder Hermes config.
// TODO: implement real Hermes config generation when the runtime is available.
func (h *HermesHarness) BuildConfig(input ConfigInput) (string, error) {
	cfg := map[string]any{
		"_comment": "TODO: real Hermes config — this is a placeholder",
		"agent":    input.Name,
		"port":     h.GatewayPort(),
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling hermes config: %w", err)
	}
	return string(b), nil
}
