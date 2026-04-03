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

// ObserveClawHarness implements the Harness interface for the orq-ai OpenClaw
// fork with observeclaw (budget enforcement, PII redaction, tool policy) and
// a2a-gateway plugins pre-installed. Same runtime as OpenClaw but with a
// custom-built image containing Clawbernetes plugins.
//
// Build with: make build-openclaw-image
type ObserveClawHarness struct {
	OpenClawHarness
}

func (h *ObserveClawHarness) Name() string            { return "observeclaw" }
func (h *ObserveClawHarness) DefaultImage() string    { return "clawbernetes/openclaw:latest" }
func (h *ObserveClawHarness) ConfigMapSuffix() string { return "-observeclaw-config" }
func (h *ObserveClawHarness) ContainerName() string   { return "observeclaw" }
