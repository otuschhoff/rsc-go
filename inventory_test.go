package rsc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBoundedFilesetInventory(t *testing.T) {
	for _, test := range []struct {
		name, connection string
		wantError        bool
	}{
		{"empty", `{"nodes":[],"pageInfo":{"hasNextPage":false}}`, false},
		{"null", `null`, true},
		{"missing_nodes", `{"pageInfo":{"hasNextPage":false}}`, true},
		{"null_nodes", `{"nodes":null,"pageInfo":{"hasNextPage":false}}`, true},
		{"missing_page_info", `{"nodes":[]}`, true},
		{"missing_next", `{"nodes":[],"pageInfo":{}}`, true},
		{"missing_cursor", `{"nodes":[],"pageInfo":{"hasNextPage":true}}`, true},
		{"cycle", `{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"same"}}`, true},
		{"null_template", `{"nodes":[null],"pageInfo":{"hasNextPage":false}}`, true},
		{"nested_truncation", `{"nodes":[{"id":"template","descendantConnection":{"nodes":[],"pageInfo":{"hasNextPage":true}}}],"pageInfo":{"hasNextPage":false}}`, true},
		{"missing_nested_nodes", `{"nodes":[{"id":"template","descendantConnection":{"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}`, true},
		{"null_object", `{"nodes":[{"id":"template","descendantConnection":{"nodes":[null],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}`, true},
		{"missing_scope", `{"nodes":[{"id":"template","descendantConnection":{"nodes":[{"id":"object"}],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				requests++
				_, _ = io.WriteString(writer, `{"data":{"filesetTemplates":`+test.connection+`}}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			rows, err := FetchFilesetInventory(ctx, server.Client(), server.URL, InventoryLimits{MaxPages: 6, MaxObjects: 100})
			if (err != nil) != test.wantError || (test.wantError && !errors.Is(err, ErrIncompleteInventory)) || len(rows) != 0 || requests > 3 {
				t.Fatalf("rows=%d requests=%d error=%v", len(rows), requests, err)
			}
		})
	}
}

func TestFilesetInventoryPresenceIdentityAndBudgets(t *testing.T) {
	for _, mode := range []string{"valid", "duplicate", "page_limit", "object_limit", "denied", "late_transient", "large_response"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				requests++
				var body struct{ Query string }
				_ = json.NewDecoder(req.Body).Decode(&body)
				if mode == "large_response" {
					_, _ = io.WriteString(writer, strings.Repeat(" ", (4<<20)+2))
					return
				}
				if mode == "denied" && requests == 2 {
					writer.WriteHeader(http.StatusForbidden)
					return
				}
				if mode == "late_transient" && requests == 2 {
					writer.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if !strings.Contains(body.Query, "WINDOWS_HOST_ROOT") {
					_, _ = io.WriteString(writer, `{"data":{"filesetTemplates":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`)
					return
				}
				next := !strings.Contains(body.Query, `after: "next"`)
				id := "second"
				if next || mode == "duplicate" {
					id = "first"
				}
				policy := any(nil)
				if next {
					policy = map[string]any{"id": "UNPROTECTED"}
				}
				object := map[string]any{"id": id, "cluster": map[string]any{"id": "cluster"}, "effectiveSlaDomain": policy}
				template := map[string]any{"id": "template-" + id, "descendantConnection": map[string]any{"nodes": []any{object}, "pageInfo": map[string]any{"hasNextPage": false}}}
				_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"filesetTemplates": map[string]any{"nodes": []any{template}, "pageInfo": map[string]any{"hasNextPage": next, "endCursor": "next"}}}})
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			limits := InventoryLimits{MaxPages: 6, MaxObjects: 100}
			if mode == "page_limit" {
				limits.MaxPages = 3
			}
			if mode == "object_limit" {
				limits.MaxObjects = 1
			}
			rows, err := FetchFilesetInventory(ctx, server.Client(), server.URL, limits)
			if mode != "valid" {
				if err == nil || rows != nil {
					t.Fatalf("partial inventory escaped: %+v %v", rows, err)
				}
				if (mode == "denied" || mode == "late_transient") && requests != 2 {
					t.Fatal("page failure retried or returned partial results")
				}
				return
			}
			if err != nil || len(rows) != 2 || requests != 4 || rows[0].EffectiveSLAID == nil || *rows[0].EffectiveSLAID != "UNPROTECTED" || rows[1].EffectiveSLAID != nil || rows[1].ClusterID != "cluster" {
				t.Fatalf("presence/identity lost: %+v %v", rows, err)
			}
			cancel()
			if _, err := FetchFilesetInventory(ctx, server.Client(), server.URL, limits); !errors.Is(err, context.Canceled) || requests != 4 {
				t.Fatal("cancelled inventory performed requests")
			}
		})
	}
}
