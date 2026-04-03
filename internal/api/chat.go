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

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

// Default ports for agent harness types.
const (
	defaultOpenClawGatewayPort = 18789
	defaultGatewayPort         = 8443
)

// upstreamClient is used for chat relay requests with a reasonable timeout.
var upstreamClient = &http.Client{Timeout: 60 * time.Second}

// chatRequest is the JSON body for the POST /api/agents/{name}/chat endpoint.
type chatRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleAgentChat relays a chat message to the agent's gateway and returns the
// response. It resolves the agent's in-cluster service URL and forwards the
// request as an OpenClaw /v1/chat/completions call.
func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request, agentName string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	gatewayURL, err := s.resolveGatewayURL(ctx, agentName)
	if err != nil {
		http.Error(w, fmt.Sprintf("resolving gateway: %v", err), http.StatusNotFound)
		return
	}

	chatBody := map[string]any{
		"model": "openclaw/default",
		"messages": []map[string]string{
			{"role": req.Role, "content": req.Content},
		},
	}
	bodyBytes, err := json.Marshal(chatBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshalling request: %v", err), http.StatusInternalServerError)
		return
	}

	upstream := fmt.Sprintf("%s/v1/chat/completions", gatewayURL)
	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("building upstream request: %v", err), http.StatusInternalServerError)
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")

	resp, err := upstreamClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("calling agent gateway: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// resolveGatewayURL returns the in-cluster URL for the agent's gateway service.
// It reads the agent's service to find the gateway port.
func (s *Server) resolveGatewayURL(ctx context.Context, agentName string) (string, error) {
	agent := &clawv1.ClawAgent{}
	if err := s.Client.Get(ctx, agentKey(agentName, s.Namespace), agent); err != nil {
		return "", fmt.Errorf("getting agent %q: %w", agentName, err)
	}

	svc := &corev1.Service{}
	svcKey := client.ObjectKey{Name: agentName, Namespace: s.Namespace}
	if err := s.Client.Get(ctx, svcKey, svc); err != nil {
		// Fall back: if the agent references a gateway CR, use that service.
		if agent.Spec.Gateway != "" {
			return s.resolveGatewayFromCR(ctx, agent.Spec.Gateway)
		}
		return "", fmt.Errorf("getting service %q: %w", agentName, err)
	}

	port := findServicePort(svc, "gateway")
	if port == 0 {
		port = defaultOpenClawGatewayPort
	}

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", agentName, s.Namespace, port), nil
}

// resolveGatewayFromCR resolves the gateway URL from a ClawGateway CR.
func (s *Server) resolveGatewayFromCR(ctx context.Context, gatewayName string) (string, error) {
	gw := &clawv1.ClawGateway{}
	key := client.ObjectKey{Name: gatewayName, Namespace: s.Namespace}
	if err := s.Client.Get(ctx, key, gw); err != nil {
		return "", fmt.Errorf("getting gateway %q: %w", gatewayName, err)
	}

	port := gw.Spec.Port
	if port == 0 {
		port = defaultGatewayPort
	}
	svcName := fmt.Sprintf("%s-gateway", gatewayName)
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svcName, s.Namespace, port), nil
}

// findServicePort returns the port number for a named port on a service.
func findServicePort(svc *corev1.Service, portName string) int {
	for _, p := range svc.Spec.Ports {
		if p.Name == portName {
			return int(p.Port)
		}
	}
	return 0
}

// agentKey builds a client.ObjectKey for a ClawAgent in the given namespace.
func agentKey(name, namespace string) client.ObjectKey {
	return client.ObjectKey{Name: name, Namespace: namespace}
}

