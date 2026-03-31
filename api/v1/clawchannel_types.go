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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Supported channel types.
const (
	ChannelTypeTelegram = "telegram"
	ChannelTypeSlack    = "slack"
	ChannelTypeDiscord  = "discord"
	ChannelTypeWhatsApp = "whatsapp"
	ChannelTypeMatrix   = "matrix"
	ChannelTypeMSTeams  = "msteams"
)

// ChannelsBotToken maps channel types that use a botToken credential.
var ChannelsWithBotToken = map[string]bool{
	ChannelTypeTelegram: true,
	ChannelTypeSlack:    true,
	ChannelTypeDiscord:  true,
	ChannelTypeMSTeams:  true,
}

// ChannelsWithAppToken maps channel types that also require an appToken.
var ChannelsWithAppToken = map[string]bool{
	ChannelTypeSlack: true,
}

// ProviderAPIFormats maps known provider names to their OpenClaw API format string.
var ProviderAPIFormats = map[string]string{
	"anthropic": "anthropic-messages",
	"openai":    "openai-responses",
	"google":    "google-genai",
}

// ProviderBaseURLs maps known provider names to their default base URL.
var ProviderBaseURLs = map[string]string{
	"anthropic": "https://api.anthropic.com",
	"openai":    "https://api.openai.com/v1",
	"google":    "https://generativelanguage.googleapis.com/v1beta",
}

// DefaultProviderAPIKeysSecret is the conventional secret name for LLM provider API keys.
const DefaultProviderAPIKeysSecret = "openclaw-api-keys"

// ClawChannelSpec defines the desired state of ClawChannel.
type ClawChannelSpec struct {
	// type is the delivery channel type (telegram, slack, discord, etc.).
	// +required
	// +kubebuilder:validation:Enum=telegram;slack;discord;whatsapp;matrix;msteams;irc;signal;line;feishu;nostr;mattermost;googlechat
	Type string `json:"type"`

	// enabled controls whether this channel is active.
	// +optional
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// credentialsSecret is the name of the Kubernetes Secret containing channel
	// credentials (e.g. bot tokens). Secret keys must use the convention
	// <TYPE_UPPER>_BOT_TOKEN, <TYPE_UPPER>_APP_TOKEN, etc.
	// Example: TELEGRAM_BOT_TOKEN, SLACK_BOT_TOKEN, SLACK_APP_TOKEN.
	// These are injected as env vars and referenced via ${VAR} in openclaw.json.
	// +required
	CredentialsSecret string `json:"credentialsSecret"`

	// config holds channel-specific configuration. Keys map directly to
	// fields in openclaw.json's channels.<type> section.
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// IsEnabled returns whether the channel is enabled (defaults to true).
func (c ClawChannelSpec) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// ClawChannelStatus defines the observed state of ClawChannel.
type ClawChannelStatus struct {
	// conditions represent the current state of the ClawChannel resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`

// ClawChannel is the Schema for the clawchannels API.
// It declares a delivery channel (Telegram, Slack, etc.) for agent communication.
type ClawChannel struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ClawChannel
	// +required
	Spec ClawChannelSpec `json:"spec"`

	// status defines the observed state of ClawChannel
	// +optional
	Status ClawChannelStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClawChannelList contains a list of ClawChannel
type ClawChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ClawChannel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClawChannel{}, &ClawChannelList{})
}
