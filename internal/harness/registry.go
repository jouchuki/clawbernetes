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

import clawv1 "github.com/clawbernetes/operator/api/v1"

// ForType returns the Harness implementation for the given type.
func ForType(t clawv1.HarnessType) Harness {
	switch t {
	case clawv1.HarnessObserveClaw:
		return &ObserveClawHarness{}
	case clawv1.HarnessHermes:
		return &HermesHarness{}
	default:
		return &OpenClawHarness{}
	}
}
