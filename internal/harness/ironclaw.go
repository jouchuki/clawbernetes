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

// IronClawHarness implements the Harness interface for the IronClaw agent runtime.
type IronClawHarness struct{}

func (h *IronClawHarness) Name() string            { return "ironclaw" }
func (h *IronClawHarness) DefaultImage() string    { return "clawbernetes/ironclaw:latest" }
func (h *IronClawHarness) GatewayPort() int32      { return 9090 }
func (h *IronClawHarness) HomePath() string        { return "/home/ironclaw/.ironclaw" }
func (h *IronClawHarness) WorkspacePath() string   { return "/home/ironclaw/.ironclaw/workspace" }
func (h *IronClawHarness) ExtensionsPath() string  { return "/home/ironclaw/.ironclaw/extensions" }
func (h *IronClawHarness) ConfigFileName() string  { return "ironclaw.json" }
func (h *IronClawHarness) ConfigMapSuffix() string { return "-ironclaw-config" }
func (h *IronClawHarness) ReadinessPath() string   { return "/ready" }
func (h *IronClawHarness) LivenessPath() string    { return "/health" }
func (h *IronClawHarness) ContainerName() string   { return "ironclaw" }

func (h *IronClawHarness) CopyExtensionsCommands() []string {
	return DefaultCopyExtensionsCommands(h.HomePath())
}

func (h *IronClawHarness) SeedCommands() []string {
	return DefaultSeedCommands(h.ConfigFileName())
}

// BuildConfig generates a placeholder IronClaw config.
// TODO: implement real IronClaw config generation when the runtime is available.
func (h *IronClawHarness) BuildConfig(input ConfigInput) (string, error) {
	cfg := map[string]any{
		"_comment": "TODO: real IronClaw config — this is a placeholder",
		"agent":    input.Name,
		"port":     h.GatewayPort(),
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling ironclaw config: %w", err)
	}
	return string(b), nil
}
