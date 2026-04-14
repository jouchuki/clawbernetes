/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

// crudRequest is the JSON body for create and update operations.
type crudRequest struct {
	Name string `json:"name"`
}

// registerCRUDRoutes registers CRUD endpoints for all 6 CRD types.
func (s *Server) registerCRUDRoutes(mux *http.ServeMux) {
	s.registerCRUD(mux, "/api/agents",
		func() client.Object { return &clawv1.ClawAgent{} },
		func() client.ObjectList { return &clawv1.ClawAgentList{} },
	)
	s.registerCRUD(mux, "/api/channels",
		func() client.Object { return &clawv1.ClawChannel{} },
		func() client.ObjectList { return &clawv1.ClawChannelList{} },
	)
	s.registerCRUD(mux, "/api/policies",
		func() client.Object { return &clawv1.ClawPolicy{} },
		func() client.ObjectList { return &clawv1.ClawPolicyList{} },
	)
	s.registerCRUD(mux, "/api/skillsets",
		func() client.Object { return &clawv1.ClawSkillSet{} },
		func() client.ObjectList { return &clawv1.ClawSkillSetList{} },
	)
	s.registerCRUD(mux, "/api/gateways",
		func() client.Object { return &clawv1.ClawGateway{} },
		func() client.ObjectList { return &clawv1.ClawGatewayList{} },
	)
	s.registerCRUD(mux, "/api/observabilities",
		func() client.Object { return &clawv1.ClawObservability{} },
		func() client.ObjectList { return &clawv1.ClawObservabilityList{} },
	)
}

// registerCRUD registers a handler at path+"/" for single-resource CRUD operations
// (GET, PUT, DELETE by name) and a POST handler at the exact path for creation.
// The existing list handlers (handleAgents, handleChannels) remain registered at
// the exact path for GET list operations.
func (s *Server) registerCRUD(
	mux *http.ServeMux,
	path string,
	newObj func() client.Object,
	newList func() client.ObjectList,
) {
	// Handle /api/{resource}/{name} and sub-resources — GET single, PUT, DELETE
	mux.HandleFunc(path+"/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, path+"/")
		rest = strings.TrimSuffix(rest, "/")
		if rest == "" {
			http.Error(w, "resource name is required", http.StatusBadRequest)
			return
		}

		parts := strings.SplitN(rest, "/", 2)
		name := parts[0]

		// Check for sub-resources (e.g., /api/agents/eng-agent/logs)
		if len(parts) == 2 {
			sub := parts[1]
			if path == "/api/agents" {
				switch sub {
				case "logs":
					s.handleAgentLogs(w, r, name)
					return
				case "chat":
					s.handleAgentChat(w, r, name)
					return
				}
			}
			http.Error(w, "unknown sub-resource", http.StatusNotFound)
			return
		}

		switch r.Method {
		case http.MethodGet:
			s.handleGetOne(w, name, newObj)
		case http.MethodPut:
			s.handleUpdate(w, r, name, newObj)
		case http.MethodDelete:
			s.handleDelete(w, name, newObj)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Capture any previously registered handler for this path (e.g. handleAgents)
	// so we can delegate GET requests to it while adding POST support.
	existingHandler := getExistingHandler(mux, path)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			s.handleCreate(w, r, newObj)
		case http.MethodGet:
			if existingHandler != nil {
				existingHandler.ServeHTTP(w, r)
			} else {
				s.handleList(w, newList)
			}
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// getExistingHandler probes the mux for a handler already registered at an exact path.
func getExistingHandler(mux *http.ServeMux, path string) http.Handler {
	fakeReq, _ := http.NewRequest(http.MethodGet, path, nil)
	handler, pattern := mux.Handler(fakeReq)
	if pattern == path {
		return handler
	}
	return nil
}

// handleGetOne returns a single resource by name.
func (s *Server) handleGetOne(w http.ResponseWriter, name string, newObj func() client.Object) {
	obj := newObj()
	key := types.NamespacedName{Name: name, Namespace: s.Namespace}
	if err := s.Client.Get(context.Background(), key, obj); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, obj)
}

// handleList returns all resources of a given type.
func (s *Server) handleList(w http.ResponseWriter, newList func() client.ObjectList) {
	list := newList()
	if err := s.Client.List(context.Background(), list, client.InNamespace(s.Namespace)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

// handleCreate creates a new resource from the request body.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request, newObj func() client.Object) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req crudRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	obj := newObj()
	if err := json.Unmarshal(body, obj); err != nil {
		http.Error(w, "failed to parse object: "+err.Error(), http.StatusBadRequest)
		return
	}

	obj.SetName(req.Name)
	obj.SetNamespace(s.Namespace)

	if err := s.Client.Create(context.Background(), obj); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, obj)
}

// handleUpdate updates an existing resource.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request, name string, newObj func() client.Object) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	existing := newObj()
	key := types.NamespacedName{Name: name, Namespace: s.Namespace}
	if err := s.Client.Get(context.Background(), key, existing); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Save metadata that the client must not overwrite.
	rv := existing.GetResourceVersion()
	uid := existing.GetUID()

	if err := json.Unmarshal(body, existing); err != nil {
		http.Error(w, "failed to parse object: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Restore metadata to prevent conflicts or identity changes.
	existing.SetName(name)
	existing.SetNamespace(s.Namespace)
	existing.SetResourceVersion(rv)
	existing.SetUID(uid)

	if err := s.Client.Update(context.Background(), existing); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, existing)
}

// handleDelete deletes a resource by name.
func (s *Server) handleDelete(w http.ResponseWriter, name string, newObj func() client.Object) {
	obj := newObj()
	obj.SetName(name)
	obj.SetNamespace(s.Namespace)

	if err := s.Client.Delete(context.Background(), obj); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]string{"status": "deleted", "name": name})
}
