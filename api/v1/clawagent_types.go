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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentIdentitySpec defines the agent's soul, user context, and self-concept.
type AgentIdentitySpec struct {
	// soul is the agent's core personality and behavior (maps to SOUL.md).
	// +optional
	Soul string `json:"soul,omitempty"`

	// user describes the human operator the agent works with (maps to USER.md).
	// +optional
	User string `json:"user,omitempty"`

	// agentIdentity is the agent's persistent self-concept (maps to IDENTITY.md).
	// +optional
	AgentIdentity string `json:"agentIdentity,omitempty"`
}

// TelemetryCaptureSpec controls what content is captured in OTEL spans.
type TelemetryCaptureSpec struct {
	// inputMessages controls whether LLM input messages appear in spans.
	// +optional
	InputMessages bool `json:"inputMessages,omitempty"`

	// outputMessages controls whether LLM output messages appear in spans.
	// +optional
	OutputMessages bool `json:"outputMessages,omitempty"`

	// systemInstructions controls whether system instructions appear in spans.
	// +optional
	SystemInstructions bool `json:"systemInstructions,omitempty"`

	// toolDefinitions controls whether tool schemas appear in spans.
	// +optional
	ToolDefinitions bool `json:"toolDefinitions,omitempty"`

	// toolContent controls whether tool call content appears in spans.
	// +optional
	ToolContent bool `json:"toolContent,omitempty"`

	// sampleRate is the trace sampling rate (0.0 to 1.0), serialized as string.
	// +optional
	SampleRate string `json:"sampleRate,omitempty"`
}

// ModelFallbackSpec defines a fallback model configuration.
type ModelFallbackSpec struct {
	// provider is the model provider (e.g. "anthropic").
	// +optional
	Provider string `json:"provider,omitempty"`

	// name is the model name (e.g. "claude-haiku-4-5").
	// +optional
	Name string `json:"name,omitempty"`
}

// AgentModelSpec defines the default LLM model for the agent.
type AgentModelSpec struct {
	// provider is the model provider (e.g. "anthropic").
	// +optional
	Provider string `json:"provider,omitempty"`

	// name is the model name (e.g. "claude-sonnet-4-6").
	// +optional
	Name string `json:"name,omitempty"`

	// fallback defines the fallback model if the primary is unavailable.
	// +optional
	Fallback *ModelFallbackSpec `json:"fallback,omitempty"`
}

// AgentResourceRequirements defines resource requests and limits for the agent pod.
type AgentResourceRequirements struct {
	// requests describes the minimum resources required.
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`

	// limits describes the maximum resources allowed.
	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`
}

// AgentLifecycleSpec defines lifecycle management for the agent.
type AgentLifecycleSpec struct {
	// restartPolicy controls pod restart behavior.
	// +optional
	// +kubebuilder:validation:Enum=Always;OnFailure;Never
	RestartPolicy string `json:"restartPolicy,omitempty"`

	// hibernateAfterIdleMinutes scales the agent to zero after this many minutes idle.
	// +optional
	HibernateAfterIdleMinutes *int `json:"hibernateAfterIdleMinutes,omitempty"`

	// maxRuntime is the hard cap on agent runtime (e.g. "24h").
	// +optional
	MaxRuntime string `json:"maxRuntime,omitempty"`
}

// WorkspaceSpec controls storage for the agent's .openclaw home directory.
type WorkspaceSpec struct {
	// mode selects ephemeral (emptyDir) or persistent (PVC) storage.
	// +optional
	// +kubebuilder:default=ephemeral
	// +kubebuilder:validation:Enum=ephemeral;persistent
	Mode string `json:"mode,omitempty"`

	// storageSize is the PVC size when mode=persistent (e.g. "10Gi").
	// Ignored when mode=ephemeral.
	// +optional
	StorageSize string `json:"storageSize,omitempty"`

	// storageClassName overrides the default StorageClass for the PVC.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// ClawAgentSpec defines the desired state of ClawAgent.
type ClawAgentSpec struct {
	// identity defines the agent's soul, user context, and self-concept.
	// +optional
	Identity AgentIdentitySpec `json:"identity,omitempty"`

	// skillSet references a ClawSkillSet resource by name.
	// +optional
	SkillSet string `json:"skillSet,omitempty"`

	// policy references a ClawPolicy resource by name.
	// +optional
	Policy string `json:"policy,omitempty"`

	// connectors is a list of ClawConnector resource names this agent can reach.
	// +optional
	Connectors []string `json:"connectors,omitempty"`

	// gateway references the ClawGateway resource name for LLM routing.
	// +optional
	Gateway string `json:"gateway,omitempty"`

	// observability references the ClawObservability resource name.
	// +optional
	Observability string `json:"observability,omitempty"`

	// telemetryCapture controls what content is captured in OTEL spans.
	// +optional
	TelemetryCapture TelemetryCaptureSpec `json:"telemetryCapture,omitempty"`

	// model defines the default LLM model for this agent.
	// +optional
	Model AgentModelSpec `json:"model,omitempty"`

	// resources defines Kubernetes resource requests and limits for the agent pod.
	// +optional
	Resources AgentResourceRequirements `json:"resources,omitempty"`

	// lifecycle defines lifecycle management for the agent.
	// +optional
	Lifecycle AgentLifecycleSpec `json:"lifecycle,omitempty"`

	// workspace controls storage for the agent's .openclaw home directory.
	// +optional
	Workspace WorkspaceSpec `json:"workspace,omitempty"`

	// credentialsSecret names a Kubernetes Secret containing integration credentials
	// (e.g. telegram tokens). Mounted read-only at /home/node/.openclaw/credentials/.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

// ClawAgentStatus defines the observed state of ClawAgent.
type ClawAgentStatus struct {
	// conditions represent the current state of the ClawAgent resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phase is the current lifecycle phase of the agent (e.g. Running, Hibernating, Error).
	// +optional
	Phase string `json:"phase,omitempty"`

	// podName is the name of the Pod running this agent.
	// +optional
	PodName string `json:"podName,omitempty"`

	// workspacePVC is the name of the PVC backing the agent's home directory (if persistent).
	// +optional
	WorkspacePVC string `json:"workspacePVC,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`

// ClawAgent is the Schema for the clawagents API.
// It declares a single autonomous OpenClaw agent as a first-class Kubernetes resource.
type ClawAgent struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ClawAgent
	// +required
	Spec ClawAgentSpec `json:"spec"`

	// status defines the observed state of ClawAgent
	// +optional
	Status ClawAgentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClawAgentList contains a list of ClawAgent
type ClawAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ClawAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClawAgent{}, &ClawAgentList{})
}
