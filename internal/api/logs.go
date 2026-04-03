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
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

// handleAgentLogs streams pod logs as Server-Sent Events.
//
// Query parameters:
//   - follow (bool, default false): whether to follow the log stream
//   - tailLines (int, default 100): number of trailing lines to return
func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request, agentName string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	follow := r.URL.Query().Get("follow") == "true"
	tailLines := int64(100)
	if tl := r.URL.Query().Get("tailLines"); tl != "" {
		if v, err := strconv.ParseInt(tl, 10, 64); err == nil && v > 0 {
			tailLines = v
		}
	}

	podName, containerName, err := s.resolveAgentPod(ctx, agentName)
	if err != nil {
		http.Error(w, fmt.Sprintf("resolving agent pod: %v", err), http.StatusNotFound)
		return
	}

	opts := &corev1.PodLogOptions{
		Container: containerName,
		Follow:    follow,
		TailLines: &tailLines,
	}

	stream, err := s.Clientset.CoreV1().Pods(s.Namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("opening log stream: %v", err), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()

		// Stop if the client disconnected.
		if ctx.Err() != nil {
			return
		}
	}
}

// resolveAgentPod finds the pod name and main container name for a ClawAgent.
// It first checks the agent status for a stored pod name, then falls back to
// listing pods by the app={name} label selector.
func (s *Server) resolveAgentPod(ctx context.Context, agentName string) (string, string, error) {
	agent := &clawv1.ClawAgent{}
	if err := s.Client.Get(ctx, agentKey(agentName, s.Namespace), agent); err != nil {
		return "", "", fmt.Errorf("getting agent %q: %w", agentName, err)
	}

	// Try the status-reported pod name first, then fall back to label selector.
	if agent.Status.PodName != "" {
		pod, err := s.Clientset.CoreV1().Pods(s.Namespace).Get(ctx, agent.Status.PodName, metav1.GetOptions{})
		if err == nil {
			return pod.Name, firstContainerName(pod), nil
		}
		// Status pod name may contain extra text (e.g. "name (replicas: 1)") — fall through to label selector.
	}

	pods, err := s.Clientset.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", agentName),
		Limit:         1,
	})
	if err != nil {
		return "", "", fmt.Errorf("listing pods for agent %q: %w", agentName, err)
	}
	if len(pods.Items) == 0 {
		return "", "", fmt.Errorf("no pods found for agent %q", agentName)
	}
	pod := &pods.Items[0]
	return pod.Name, firstContainerName(pod), nil
}

// firstContainerName returns the name of the first non-init container, or ""
// if the pod has no containers.
func firstContainerName(pod *corev1.Pod) string {
	if len(pod.Spec.Containers) > 0 {
		return pod.Spec.Containers[0].Name
	}
	return ""
}
