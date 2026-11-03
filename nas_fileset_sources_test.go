package rsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNASFilesetAssignmentsAcrossPages(t *testing.T) {
	const id, name = "9c95193d-5592-5347-8e5b-5db17f728332", "SNEU_DFS_14d_3m"
	paths := []string{"Group", "Project", "Group", "Org", "Project", "SNEU", "Temp", "/filesrv", "Group", "Project", "root_gcc_filesrv$", "FS", "/gcc_filesrv"}
	sources := []NASFilesetSource{}
	for i, path := range paths {
		sla := FilesetSLA{ID: id, Name: name}
		effective := sla
		if i >= 11 {
			effective = FilesetSLA{ID: "DO_NOT_PROTECT"}
		}
		protocol := "SMB"
		if strings.HasPrefix(path, "/") {
			protocol = "NFS"
		}
		sources = append(sources, NASFilesetSource{
			ID: fmt.Sprintf("share-%d", i), Name: path, ExportPoint: path,
			HostAddress: fmt.Sprintf("vs-%d.example.test", i/3), ShareType: protocol,
			Cluster:        FilesetCluster{Name: "Rubrikcluster01"},
			PrimaryFileset: &NASPrimaryFileset{ID: fmt.Sprintf("fileset-%d", i), Name: path, ConfiguredSLA: sla, EffectiveSLA: effective},
		})
	}
	nasCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var op struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		data := map[string]any{
			"filesetTemplates": map[string]any{"nodes": []any{}},
			"protectedObjectsConnection": map[string]any{"nodes": []any{
				map[string]any{"objectType": "VMWARE_VM", "name": "vm", "effectiveSlaFidOpt": id},
				map[string]any{"objectType": "NAS_FILESET", "name": "reporting-duplicate", "effectiveSlaFidOpt": id},
			}},
			"nasSystems":    map[string]any{"nodes": []any{}},
			"nasNamespaces": map[string]any{"nodes": []any{}},
		}
		if strings.Contains(op.Query, "RSCNASFilesetSources") {
			nasCalls++
			if !strings.Contains(op.Query, "primaryFileset { id name configuredSlaDomain") || !strings.Contains(op.Query, "pendingSla") {
				t.Error("missing primary-fileset selections")
			}
			nodes, next := sources[:10], "next"
			if op.Variables["after"] != nil {
				nodes, next = sources[10:], ""
			}
			data["nasShares"] = map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": next != "", "endCursor": next}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.Assignments(t.Context(), id, name)
	if err != nil || len(rows) != 14 || nasCalls != 2 {
		t.Fatalf("assignments=%+v calls=%d err=%v", rows, nasCalls, err)
	}
	protected, unprotected, cifs := 0, 0, 0
	for _, row := range rows {
		if row.Kind != "NAS_FILESET" {
			continue
		}
		if row.ConfiguredSLAName != name || row.Host == "" || row.TargetHost == "" || row.Path == "" {
			t.Fatalf("missing source metadata: %+v", row)
		}
		if row.ShareType == "CIFS" {
			cifs++
		}
		if row.EffectiveSLAName == name {
			protected++
		} else if row.EffectiveSLAName == "DO_NOT_PROTECT" {
			unprotected++
		}
	}
	if protected != 11 || unprotected != 2 || cifs != 11 {
		t.Fatalf("protected=%d unprotected=%d CIFS=%d", protected, unprotected, cifs)
	}
	config := ConvertSLADetailToConfig(SLADetail{ID: id, Name: name}, rows)
	for _, row := range config.Assignments {
		if (row.Path == "FS" || row.Path == "/gcc_filesrv") && row.EffectiveSLAName != "DO_NOT_PROTECT" {
			t.Fatalf("configuration erased unprotection: %+v", row)
		}
	}
	if _, err := client.AssignmentsWithNAS(t.Context(), id, name, sources); err != nil || nasCalls != 2 {
		t.Fatalf("shared inventory refetched: %d %v", nasCalls, err)
	}
	if empty, err := client.AssignmentsWithNAS(t.Context(), id, name, []NASFilesetSource{}); err != nil || len(empty) != 1 || nasCalls != 2 {
		t.Fatalf("authoritative empty inventory lost: %+v %v", empty, err)
	}
	for _, test := range []struct {
		name    string
		fileset NASPrimaryFileset
		want    string
	}{
		{"effective-only", NASPrimaryFileset{EffectiveSLA: FilesetSLA{ID: id, Name: name}}, name},
		{"unknown-effective", NASPrimaryFileset{ConfiguredSLA: FilesetSLA{ID: id, Name: name}}, "Unknown"},
		{"different-effective-ID", NASPrimaryFileset{ConfiguredSLA: FilesetSLA{ID: id, Name: name}, EffectiveSLA: FilesetSLA{ID: "other", Name: name}}, "other"},
		{"pending-only", NASPrimaryFileset{PendingSLA: FilesetSLA{ID: id, Name: name}}, ""},
		{"different-configured-ID", NASPrimaryFileset{ConfiguredSLA: FilesetSLA{ID: "other", Name: name}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := client.AssignmentsWithNAS(t.Context(), id, name, []NASFilesetSource{{PrimaryFileset: &test.fileset}})
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if len(rows) != 1 {
					t.Fatalf("unexpected NAS match: %+v", rows)
				}
			} else if len(rows) != 2 || rows[1].EffectiveSLAName != test.want {
				t.Fatalf("incorrect effective state: %+v", rows)
			}
		})
	}
}

func TestNASFilesetSLAIdentityAndEffectiveState(t *testing.T) {
	if nasFilesetSLAMatches(FilesetSLA{ID: "other", Name: "Gold"}, "gold", "Gold") {
		t.Fatal("same name overrode a different policy ID")
	}
	for _, test := range []struct {
		sla  FilesetSLA
		want string
	}{
		{FilesetSLA{ID: "other", Name: "Gold"}, "other"},
		{FilesetSLA{}, "Unknown"},
		{FilesetSLA{ID: "DO_NOT_PROTECT", Name: "Gold"}, "DO_NOT_PROTECT"},
		{FilesetSLA{ID: "UNPROTECTED"}, "UNPROTECTED"},
		{FilesetSLA{ID: "gold"}, "Gold"},
	} {
		if got := nasFilesetSLAName(test.sla, "gold", "Gold"); got != test.want {
			t.Fatalf("%+v: %q != %q", test.sla, got, test.want)
		}
	}
}

func TestNASFilesetSourcesRejectIncompleteInventory(t *testing.T) {
	for _, scenario := range []string{"missing", "nodes", "page", "status", "empty-cursor", "cycle", "failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if scenario == "failure" && calls > 1 {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"error":"denied"}`)
					return
				}
				page := map[string]any{"hasNextPage": true, "endCursor": "same"}
				share := map[string]any{"nodes": []any{}, "pageInfo": page}
				data := map[string]any{"nasShares": share}
				switch scenario {
				case "missing":
					delete(data, "nasShares")
				case "nodes":
					delete(share, "nodes")
				case "page":
					delete(share, "pageInfo")
				case "status":
					delete(page, "hasNextPage")
				case "empty-cursor":
					page["endCursor"] = ""
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			ctx := t.Context()
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if rows, err := FetchNASFilesetSources(ctx, server.Client(), server.URL); err == nil || rows != nil {
				t.Fatalf("incomplete inventory accepted: %+v %v", rows, err)
			}
		})
	}
}
