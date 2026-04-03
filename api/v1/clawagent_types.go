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

// Workspace mode constants.
const (
	WorkspaceModeEphemeral  = "ephemeral"
	WorkspaceModePersistent = "persistent"
)

// Reclaim policy constants.
const (
	ReclaimPolicyRetain = "retain"
	ReclaimPolicyDelete = "delete"
)

// Default storage size for persistent workspaces.
const DefaultWorkspaceStorageSize = "5Gi"

// PVC naming constants.
const (
	PVCSuffix       = "-home"
	PVCResizeSuffix = "-home-v2"
)

// WorkspaceSpec controls storage for the agent home directory.
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

	// reclaimPolicy controls whether the PVC is deleted or retained when the
	// ClawAgent CR is removed. Only meaningful when mode=persistent.
	// "retain" (default for persistent): PVC survives agent deletion, allowing
	// state recovery by recreating the agent with the same name.
	// "delete": PVC is garbage-collected with the agent CR.
	// +optional
	// +kubebuilder:default=retain
	// +kubebuilder:validation:Enum=retain;delete
	ReclaimPolicy string `json:"reclaimPolicy,omitempty"`
}

// IsPersistent returns true if the workspace mode is persistent.
func (w WorkspaceSpec) IsPersistent() bool {
	return w.Mode == WorkspaceModePersistent
}

// ResolvedReclaimPolicy returns the effective reclaim policy, defaulting to retain.
func (w WorkspaceSpec) ResolvedReclaimPolicy() string {
	if w.ReclaimPolicy == "" {
		return ReclaimPolicyRetain
	}
	return w.ReclaimPolicy
}

// ResolvedStorageSize returns the effective storage size, defaulting to 5Gi.
func (w WorkspaceSpec) ResolvedStorageSize() string {
	if w.StorageSize == "" {
		return DefaultWorkspaceStorageSize
	}
	return w.StorageSize
}

// HarnessType is the type of agent harness runtime.
// +kubebuilder:validation:Enum=openclaw;hermes;ironclaw
type HarnessType string

const (
	HarnessOpenClaw HarnessType = "openclaw"
	HarnessHermes   HarnessType = "hermes"
	HarnessIronClaw HarnessType = "ironclaw"
)

// HarnessSpec selects and configures the agent harness runtime.
type HarnessSpec struct {
	// type selects the harness runtime. Default: "openclaw".
	// +optional
	// +kubebuilder:default=openclaw
	Type HarnessType `json:"type,omitempty"`

	// image overrides the default container image for this harness.
	// +optional
	Image string `json:"image,omitempty"`
}

// ClawAgentSpec defines the desired state of ClawAgent.
type ClawAgentSpec struct {
	// harness selects the agent harness runtime (openclaw, hermes, ironclaw).
	// +optional
	Harness HarnessSpec `json:"harness,omitempty"`

	// identity defines the agent's soul, user context, and self-concept.
	// +optional
	Identity AgentIdentitySpec `json:"identity,omitempty"`

	// skillSet references a ClawSkillSet resource by name.
	// +optional
	SkillSet string `json:"skillSet,omitempty"`

	// policy references a ClawPolicy resource by name.
	// +optional
	Policy string `json:"policy,omitempty"`

	// channels is a list of ClawChannel resource names for delivery integrations
	// (e.g. Telegram, Slack). The operator resolves each channel and generates
	// the corresponding openclaw.json channels config with ${ENV_VAR} credential placeholders.
	// +optional
	Channels []string `json:"channels,omitempty"`

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

	// workspace controls storage for the agent home directory.
	// +optional
	Workspace WorkspaceSpec `json:"workspace,omitempty"`

	// credentialsSecret names a Kubernetes Secret containing integration credentials
	// (e.g. telegram tokens). Mounted read-only at the harness credentials directory.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// a2a controls agent-to-agent communication via the A2A v0.3.0 protocol.
	// When enabled, the openclaw-a2a-gateway plugin is configured with an Agent Card,
	// and the agent can discover and communicate with peers.
	// +optional
	A2A A2ASpec `json:"a2a,omitempty"`
}

// A2ASpec controls agent-to-agent communication via the openclaw-a2a-gateway plugin.
type A2ASpec struct {
	// enabled controls whether the A2A gateway plugin is active for this agent.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// agentCardName is the display name in the A2A Agent Card.
	// Defaults to the agent's metadata.name.
	// +optional
	AgentCardName string `json:"agentCardName,omitempty"`

	// agentCardDescription is the human-readable description in the Agent Card.
	// +optional
	AgentCardDescription string `json:"agentCardDescription,omitempty"`

	// skills lists the A2A skill IDs this agent advertises in its Agent Card.
	// +optional
	Skills []string `json:"skills,omitempty"`

	// port is the A2A server port inside the pod. Default 18800.
	// +optional
	// +kubebuilder:default=18800
	Port int `json:"port,omitempty"`

	// peers lists other A2A agents this agent can communicate with.
	// +optional
	Peers []A2APeer `json:"peers,omitempty"`

	// securityTokenSecret is a K8s Secret name containing the inbound auth token.
	// The Secret must have a key named A2A_TOKEN.
	// +optional
	SecurityTokenSecret string `json:"securityTokenSecret,omitempty"`
}

// A2APeer defines a remote A2A agent peer.
type A2APeer struct {
	// name is the peer display name.
	// +required
	Name string `json:"name"`

	// agentCardURL is the URL to the peer's Agent Card.
	// For in-cluster peers: http://<agent-name>.<namespace>.svc.cluster.local:18800/.well-known/agent-card.json
	// +required
	AgentCardURL string `json:"agentCardUrl"`

	// credentialsSecret is a K8s Secret name containing the peer's auth token.
	// The Secret must have a key named A2A_TOKEN.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

// ResolvedPort returns the A2A port, defaulting to 18800.
func (a A2ASpec) ResolvedPort() int {
	if a.Port == 0 {
		return 18800
	}
	return a.Port
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
