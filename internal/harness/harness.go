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

import "fmt"

// Harness defines the contract that each agent runtime must implement.
type Harness interface {
	Name() string
	DefaultImage() string
	GatewayPort() int32
	HomePath() string
	WorkspacePath() string
	ExtensionsPath() string
	ConfigFileName() string
	ConfigMapSuffix() string
	ReadinessPath() string
	LivenessPath() string
	ContainerName() string
	ContainerCommand() []string
	RunAsUser() *int64
	BuildConfig(input ConfigInput) (string, error)
	CopyExtensionsCommands() []string
	SeedCommands() []string
}

// DefaultCopyExtensionsCommands returns the standard copy-extensions init
// container commands, parameterized by the harness home path.
func DefaultCopyExtensionsCommands(homePath string) []string {
	return []string{
		fmt.Sprintf("cp -r %s/extensions /harness-home/extensions 2>/dev/null || true", homePath),
		fmt.Sprintf("cp -r %s/workspace/plugins /harness-home/workspace-plugins 2>/dev/null || true", homePath),
		"echo 'extensions and plugins copied'",
	}
}

// DefaultSeedCommands returns the standard seed-workspace init container
// commands, parameterized by the config file name.
func DefaultSeedCommands(configFileName string) []string {
	return []string{
		"mkdir -p /harness-home/workspace/skills /harness-home/workspace/plugins",
		"cp -r /harness-home/workspace-plugins/* /harness-home/workspace/plugins/ 2>/dev/null || true",
		fmt.Sprintf("cp /config-src/%s /harness-home/%s", configFileName, configFileName),
		"cp /config-src/HEARTBEAT.md /harness-home/workspace/HEARTBEAT.md",
		"cp /config-src/TOOLS.md /harness-home/workspace/TOOLS.md 2>/dev/null || true",
		"cp /identity-src/SOUL.md /harness-home/workspace/SOUL.md 2>/dev/null || true",
		"cp /identity-src/USER.md /harness-home/workspace/USER.md 2>/dev/null || true",
		"cp /identity-src/IDENTITY.md /harness-home/workspace/IDENTITY.md 2>/dev/null || true",
		`for f in /skills-src/*; do [ -f "$f" ] && skill=$(basename "$f") && mkdir -p /harness-home/workspace/skills/$skill && cp "$f" /harness-home/workspace/skills/$skill/SKILL.md; done || true`,
		"echo 'workspace seeded'",
	}
}
