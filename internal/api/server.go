/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

var log = logf.Log.WithName("control-plane-api")

// Server serves the control plane API and embedded UI.
type Server struct {
	Client    client.Client
	Clientset kubernetes.Interface
	Namespace string
	Port      int
	UIAssets  fs.FS // embedded React build
}

// NewServer creates a Server with a Kubernetes clientset for pod log access.
func NewServer(c client.Client, ns string, port int) (*Server, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig for local dev
		config, err = rest.InClusterConfig()
		if err != nil {
			// If neither works, run without log access
			return &Server{Client: c, Namespace: ns, Port: port}, nil
		}
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return &Server{Client: c, Namespace: ns, Port: port}, nil
	}
	return &Server{Client: c, Clientset: cs, Namespace: ns, Port: port}, nil
}

// AgentResponse is the JSON response for a single agent.
type AgentResponse struct {
	Name          string            `json:"name"`
	Phase         string            `json:"phase"`
	PodName       string            `json:"podName,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	Model         string            `json:"model,omitempty"`
	Soul          string            `json:"soul,omitempty"`
	Channels      []string          `json:"channels,omitempty"`
	WorkspacePVC  string            `json:"workspacePVC,omitempty"`
	WorkspaceMode string            `json:"workspaceMode,omitempty"`
	ReclaimPolicy string            `json:"reclaimPolicy,omitempty"`
	A2AEnabled    bool              `json:"a2aEnabled"`
	A2APeers      []string          `json:"a2aPeers,omitempty"`
	A2ASkills     []string          `json:"a2aSkills,omitempty"`
	BudgetDaily   int               `json:"budgetDaily,omitempty"`
	BudgetMonthly int               `json:"budgetMonthly,omitempty"`
	ToolDeny      []string          `json:"toolDeny,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// ChannelResponse is the JSON response for a single channel.
type ChannelResponse struct {
	Name              string            `json:"name"`
	Type              string            `json:"type"`
	Enabled           bool              `json:"enabled"`
	CredentialsSecret string            `json:"credentialsSecret,omitempty"`
	Config            map[string]string `json:"config,omitempty"`
}

// FleetSummary is the top-level fleet stats.
type FleetSummary struct {
	TotalAgents    int `json:"totalAgents"`
	RunningAgents  int `json:"runningAgents"`
	TotalChannels  int `json:"totalChannels"`
	A2AConnections int `json:"a2aConnections"`
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/activity", s.handleActivity)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})

	// CRUD routes for all 6 CRD types
	s.registerCRUDRoutes(mux)


	// Serve embedded React UI at root
	if s.UIAssets != nil {
		fileServer := http.FileServer(http.FS(s.UIAssets))
		mux.Handle("/", fileServer)
	}

	addr := fmt.Sprintf(":%d", s.Port)
	server := &http.Server{Addr: addr, Handler: withCORS(mux)}

	log.Info("starting control plane API", "port", s.Port)

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	return server.ListenAndServe()
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	agents := &clawv1.ClawAgentList{}
	if err := s.Client.List(context.Background(), agents, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var resp []AgentResponse
	for _, a := range agents.Items {
		agent := AgentResponse{
			Name:          a.Name,
			Phase:         a.Status.Phase,
			PodName:       a.Status.PodName,
			Provider:      a.Spec.Model.Provider,
			Model:         a.Spec.Model.Name,
			Channels:      a.Spec.Channels,
			WorkspacePVC:  a.Status.WorkspacePVC,
			WorkspaceMode: a.Spec.Workspace.Mode,
			ReclaimPolicy: a.Spec.Workspace.ResolvedReclaimPolicy(),
			A2AEnabled:    a.Spec.A2A.Enabled,
			Labels:        a.Labels,
		}

		// Soul snippet
		soul := a.Spec.Identity.Soul
		if len(soul) > 150 {
			soul = soul[:150] + "..."
		}
		agent.Soul = soul

		// A2A peers
		for _, p := range a.Spec.A2A.Peers {
			agent.A2APeers = append(agent.A2APeers, p.Name)
		}
		agent.A2ASkills = a.Spec.A2A.Skills

		// Resolve policy for budget info
		if a.Spec.Policy != "" {
			pol := &clawv1.ClawPolicy{}
			if err := s.Client.Get(context.Background(), types.NamespacedName{Name: a.Spec.Policy, Namespace: s.Namespace}, pol); err == nil {
				agent.BudgetDaily = pol.Spec.Budget.Daily
				agent.BudgetMonthly = pol.Spec.Budget.Monthly
				agent.ToolDeny = pol.Spec.ToolPolicy.Deny
			}
		}

		resp = append(resp, agent)
	}

	writeJSON(w, resp)
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	channels := &clawv1.ClawChannelList{}
	if err := s.Client.List(context.Background(), channels, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var resp []ChannelResponse
	for _, ch := range channels.Items {
		resp = append(resp, ChannelResponse{
			Name:              ch.Name,
			Type:              ch.Spec.Type,
			Enabled:           ch.Spec.IsEnabled(),
			CredentialsSecret: ch.Spec.CredentialsSecret,
			Config:            ch.Spec.Config,
		})
	}

	writeJSON(w, resp)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	agents := &clawv1.ClawAgentList{}
	if err := s.Client.List(context.Background(), agents, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	channels := &clawv1.ClawChannelList{}
	if err := s.Client.List(context.Background(), channels, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	summary := FleetSummary{
		TotalAgents:   len(agents.Items),
		TotalChannels: len(channels.Items),
	}

	a2aConns := 0
	for _, a := range agents.Items {
		if a.Status.Phase == "Running" {
			summary.RunningAgents++
		}
		a2aConns += len(a.Spec.A2A.Peers)
	}
	summary.A2AConnections = a2aConns

	writeJSON(w, summary)
}

// ActivityEvent represents a single A2A or agent event for the activity feed.
type ActivityEvent struct {
	Timestamp string `json:"ts"`
	Agent     string `json:"agent"`
	Direction string `json:"direction,omitempty"`
	Type      string `json:"type,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
	Status    string `json:"status,omitempty"`
	Duration  int    `json:"durationMs,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Read pod logs for each agent — look for A2A gateway structured JSON events
	// and observeclaw alerts.
	agents := &clawv1.ClawAgentList{}
	if err := s.Client.List(ctx, agents, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	const maxEvents = 50
	var events []ActivityEvent

	for _, a := range agents.Items {
		if len(events) >= maxEvents {
			break
		}

		// Get recent pod logs
		podList := &corev1.PodList{}
		if err := s.Client.List(ctx, podList, client.InNamespace(s.Namespace), client.MatchingLabels{"app": a.Name}); err != nil || len(podList.Items) == 0 {
			continue
		}

		pod := podList.Items[0]
		// Read logs via the REST client
		logLines := s.getPodLogs(ctx, pod.Name, 50)
		for _, line := range logLines {
			// Parse structured A2A events: {"ts":"...","component":"a2a-gateway","event":"task.finished",...}
			if strings.Contains(line, `"component":"a2a-gateway"`) && strings.Contains(line, `"event"`) {
				// Extract the JSON part
				jsonStart := strings.Index(line, "{")
				if jsonStart < 0 {
					continue
				}
				var evt map[string]any
				if err := json.Unmarshal([]byte(line[jsonStart:]), &evt); err != nil {
					continue
				}
				event := ActivityEvent{
					Agent: a.Name,
					Type:  "a2a",
				}
				if ts, ok := evt["ts"].(string); ok {
					event.Timestamp = ts
				}
				if e, ok := evt["event"].(string); ok {
					event.Status = e
				}
				if tid, ok := evt["task_id"].(string); ok {
					event.TaskID = tid
				}
				if dur, ok := evt["duration_ms"].(float64); ok {
					event.Duration = int(dur)
				}
				if state, ok := evt["state"].(string); ok {
					event.Message = state
				}
				events = append(events, event)
			}

			// Parse observeclaw alerts: [observeclaw] alert: ...
			if strings.Contains(line, "[observeclaw] alert:") {
				ts := ""
				if len(line) > 23 {
					ts = line[:23]
				}
				alertStart := strings.Index(line, "alert:")
				msg := ""
				if alertStart > 0 {
					msg = strings.TrimSpace(line[alertStart+6:])
				}
				events = append(events, ActivityEvent{
					Timestamp: ts,
					Agent:     a.Name,
					Type:      "alert",
					Message:   msg,
				})
			}

			// Parse agent runs: [agent/embedded] embedded run agent end: ...
			if strings.Contains(line, "embedded run agent end") {
				ts := ""
				if len(line) > 23 {
					ts = line[:23]
				}
				events = append(events, ActivityEvent{
					Timestamp: ts,
					Agent:     a.Name,
					Type:      "llm-call",
					Message:   "Agent LLM call completed",
				})
			}
		}
	}

	// Sort by timestamp descending, limit to 50
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp > events[j].Timestamp
	})
	if len(events) > 50 {
		events = events[:50]
	}

	writeJSON(w, events)
}

// getPodLogs reads the last N lines of logs from a pod's openclaw container.
func (s *Server) getPodLogs(ctx context.Context, podName string, lines int) []string {
	if s.Clientset == nil {
		return nil
	}
	tailLines := int64(lines)
	container := "openclaw"
	opts := &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
	}
	req := s.Clientset.CoreV1().Pods(s.Namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil
	}
	defer stream.Close()

	var result []string
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}
	return result
}

// PVC info helper — resolves PVC status for an agent
func (s *Server) getPVCInfo(agentName string) map[string]string {
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: s.Namespace}
	if err := s.Client.Get(context.Background(), key, pvc); err != nil {
		return nil
	}
	return map[string]string{
		"name":   pvc.Name,
		"status": string(pvc.Status.Phase),
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
