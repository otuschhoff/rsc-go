package rsc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWorkloadSnapshotsPagination(t *testing.T) {
	for _, scenario := range []string{"valid", "empty", "missing connection", "missing status", "missing cursor", "repeated cursor"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				requests++
				writer.Header().Set("Content-Type", "application/json")
				switch scenario {
				case "empty":
					_, _ = writer.Write([]byte(`{"data":{"snapshotOfASnappableConnection":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`))
				case "missing connection":
					_, _ = writer.Write([]byte(`{"data":{}}`))
				case "missing status":
					_, _ = writer.Write([]byte(`{"data":{"snapshotOfASnappableConnection":{"nodes":[],"pageInfo":{}}}}`))
				case "missing cursor":
					_, _ = writer.Write([]byte(`{"data":{"snapshotOfASnappableConnection":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`))
				default:
					if requests == 1 || scenario == "repeated cursor" {
						_, _ = writer.Write([]byte(`{"data":{"snapshotOfASnappableConnection":{"nodes":[{"id":"first"}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}`))
					} else {
						var payload struct {
							Query string `json:"query"`
						}
						if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || !strings.Contains(payload.Query, `after: "next"`) {
							t.Errorf("missing continuation cursor: %s / %v", payload.Query, err)
						}
						_, _ = writer.Write([]byte(`{"data":{"snapshotOfASnappableConnection":{"nodes":[{"id":"second"}],"pageInfo":{"hasNextPage":false}}}}`))
					}
				}
			}))
			defer server.Close()
			nodes, err := FetchSnapshotsForWorkload(context.Background(), server.Client(), server.URL, "workload")
			if scenario == "valid" {
				if err != nil || len(nodes) != 2 || nodes[1].ID != "second" || requests != 2 {
					t.Fatalf("pages = %#v / %v", nodes, err)
				}
			} else if scenario == "empty" {
				if err != nil || nodes == nil || len(nodes) != 0 {
					t.Fatalf("empty = %#v / %v", nodes, err)
				}
			} else if err == nil || nodes != nil {
				t.Fatalf("incomplete snapshots = %#v / %v", nodes, err)
			}
		})
	}
}

func TestSnapshotStorageCopies(t *testing.T) {
	for _, test := range []struct {
		node SnapshotGQLNode
		want string
	}{
		{node: SnapshotGQLNode{IsLocal: true, IsReplica: true}, want: "rubrik, replica"},
		{node: SnapshotGQLNode{IsArchived: true, IsReplica: true}, want: "archiv, replica"},
		{node: SnapshotGQLNode{IsLocal: true, IsArchived: true, IsReplica: true}, want: "rubrik, archiv, replica"},
		{node: SnapshotGQLNode{SnapshotType: "CloudNativeSnapshot"}, want: "unknown"},
	} {
		if got := DetermineSnapshotStorage(test.node); got != test.want {
			t.Errorf("storage = %q, want %q", got, test.want)
		}
	}
}

func TestSnapshotFilesOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		var payload struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Variables["path"] != `C:\data\"folder` || payload.Variables["after"] != "next" || payload.Variables["searchPrefix"] != `file"` || payload.Variables["isPrefixSearch"] != true || payload.Variables["first"] != float64(7) {
			t.Errorf("browse options lost: %#v", payload.Variables)
		}
		_, _ = writer.Write([]byte(`{"data":{"browseSnapshotFileConnection":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	cursor, prefix, prefixSearch := "next", `file"`, true
	if _, err := client.SnapshotFiles(context.Background(), "snapshot", SnapshotFilesOptions{Path: `C:\data\"folder`, First: 7, After: &cursor, SearchPrefix: &prefix, IsPrefixSearch: &prefixSearch}); err != nil {
		t.Fatal(err)
	}
	for _, options := range []SnapshotFilesOptions{{First: -1}, {First: 101}} {
		if _, err := FetchSnapshotFiles(context.Background(), server.Client(), server.URL, "snapshot", options); err == nil {
			t.Fatal("invalid page size accepted")
		}
	}
	if _, err := FetchSnapshotFiles(context.Background(), server.Client(), server.URL, "", SnapshotFilesOptions{}); err == nil {
		t.Fatal("empty snapshot UUID accepted")
	}
}

func TestSnapshotFilesFailures(t *testing.T) {
	valid := `{"absolutePath":"/file","filename":"file","fileMode":"FILE","size":0}`
	for _, test := range []struct {
		name      string
		body      string
		status    int
		wantError bool
	}{
		{name: "empty", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`},
		{name: "missing", body: `{"data":{}}`, wantError: true},
		{name: "null entry", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[null],"pageInfo":{"hasNextPage":false}}}}`, wantError: true},
		{name: "missing metadata", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[{"absolutePath":"/file"}],"pageInfo":{"hasNextPage":false}}}}`, wantError: true},
		{name: "missing status", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[],"pageInfo":{}}}}`, wantError: true},
		{name: "missing cursor", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`, wantError: true},
		{name: "duplicate", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[` + valid + `,` + valid + `],"pageInfo":{"hasNextPage":false}}}}`, wantError: true},
		{name: "oversized", body: `{"data":{"browseSnapshotFileConnection":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}` + strings.Repeat(" ", 4<<20), wantError: true},
		{name: "GraphQL", body: `{"errors":[{"message":"snapshot is not indexed"}]}`, wantError: true},
		{name: "HTTP", body: `{"message":"unavailable"}`, status: 503, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				requests++
				writer.Header().Set("Content-Type", "application/json")
				if test.status != 0 {
					writer.WriteHeader(test.status)
				}
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			page, err := FetchSnapshotFiles(context.Background(), server.Client(), server.URL, "snapshot", SnapshotFilesOptions{})
			if (err != nil) != test.wantError || requests != 1 {
				t.Fatalf("error/requests = %v/%d", err, requests)
			}
			if test.wantError && page.Entries != nil {
				t.Fatal("failed browse returned a partial page")
			}
			if !test.wantError && page.Entries == nil {
				t.Fatal("empty browse must have non-nil entries")
			}
		})
	}
}

func TestSnapshotManifestLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		limits SnapshotManifestLimits
		mode   string
	}{
		{name: "pages", limits: SnapshotManifestLimits{MaxPages: 1}, mode: "DIRECTORY"},
		{name: "entries", limits: SnapshotManifestLimits{MaxEntries: 1}, mode: "FILE"},
		{name: "directory cycle", mode: "DIRECTORY"},
		{name: "unknown mode", mode: "OTHER"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				entryPath := "/entry"
				if test.name == "directory cycle" {
					entryPath = "/"
				}
				entries := []map[string]any{{"absolutePath": entryPath, "filename": "entry", "fileMode": test.mode, "size": 0}}
				if test.name == "entries" {
					entries = append(entries, map[string]any{"absolutePath": "/other", "filename": "other", "fileMode": "FILE", "size": 0})
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"browseSnapshotFileConnection": map[string]any{"nodes": entries, "pageInfo": map[string]any{"hasNextPage": false}}}})
			}))
			defer server.Close()
			manifest, err := FetchSnapshotFileManifest(context.Background(), server.Client(), server.URL, "snapshot", "", test.limits)
			if err == nil || manifest.Entries != nil {
				t.Fatalf("incomplete manifest returned %+v / %v", manifest, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchSnapshotFiles(ctx, http.DefaultClient, "https://example.invalid/api/graphql", "snapshot", SnapshotFilesOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
}

func TestSnapshotFileManifest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		var payload struct {
			Query     string `json:"query"`
			Variables struct {
				SnapshotID string  `json:"snapshotID"`
				Path       string  `json:"path"`
				After      *string `json:"after"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload.Variables.SnapshotID != `snapshot-"1` || !strings.Contains(payload.Query, "browseSnapshotFileConnection(snapshotFid: $snapshotID") {
			t.Errorf("unexpected browse request: %+v", payload)
		}
		if payload.Variables.Path == "/" && payload.Variables.After == nil {
			_, _ = writer.Write([]byte(`{"data":{"browseSnapshotFileConnection":{"nodes":[{"absolutePath":"/dir","path":"dir","displayPath":"/dir","filename":"dir","fileMode":"DIRECTORY","size":0,"lastModified":null,"statusMessage":""}],"pageInfo":{"hasNextPage":true,"endCursor":"page-2"}}}}`))
		} else if payload.Variables.Path == "/" && payload.Variables.After != nil && *payload.Variables.After == "page-2" {
			_, _ = writer.Write([]byte(`{"data":{"browseSnapshotFileConnection":{"nodes":[{"absolutePath":"/link","path":"link","filename":"link","fileMode":"SYMLINK","size":4,"lastModified":null,"statusMessage":""}],"pageInfo":{"hasNextPage":false}}}}`))
		} else if payload.Variables.Path == "/dir" {
			_, _ = writer.Write([]byte(`{"data":{"browseSnapshotFileConnection":{"nodes":[{"absolutePath":"/dir/file","path":"dir/file","filename":"file","fileMode":"FILE","size":123,"lastModified":"2026-11-03T10:00:00Z","statusMessage":"indexed"}],"pageInfo":{"hasNextPage":false}}}}`))
		} else {
			t.Errorf("unexpected directory/cursor: %+v", payload.Variables)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := client.SnapshotFileManifest(context.Background(), `snapshot-"1`, "", SnapshotManifestLimits{})
	if err != nil || len(manifest.Entries) != 3 || manifest.Pages != 3 || requests != 3 || manifest.RootPath != "/" {
		t.Fatalf("manifest = %+v / %v, requests=%d", manifest, err, requests)
	}
	if manifest.Entries[0].LastModified != nil || manifest.Entries[1].FileMode != "SYMLINK" ||
		manifest.Entries[2].Size == nil || *manifest.Entries[2].Size != 123 || manifest.Entries[2].LastModified == nil || manifest.Entries[2].StatusMessage != "indexed" {
		t.Fatalf("file metadata lost: %+v", manifest.Entries)
	}
}

func TestWorkloadSnapshotLocations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		for _, field := range []string{"localLocations", "archivalLocations", "replicationLocations", "clusterUuid", "isActive"} {
			if !strings.Contains(payload.Query, field) {
				t.Errorf("snapshot query missing %s", field)
			}
		}
		_, _ = writer.Write([]byte(`{"data":{"snapshotOfASnappableConnection":{"nodes":[
			{"id":"both","localLocations":[{"id":"local","name":"lab","clusterUuid":"cluster","type":"LOCAL","isActive":true,"isArchived":false}],"archivalLocations":[{"id":"archive","name":"vault","clusterUuid":"cluster","type":"ARCHIVAL","isActive":true,"isArchived":true}],"replicationLocations":[]},
			{"id":"archive-only","localLocations":[],"archivalLocations":[{"id":"archive","name":"vault"}],"replicationLocations":[]},
			{"id":"local-only","localLocations":[{"id":"local"}],"archivalLocations":[],"replicationLocations":[]},
			{"id":"replica-only","localLocations":[],"archivalLocations":[],"replicationLocations":[{"id":"replica"}]},
			{"id":"unknown","localLocations":[],"archivalLocations":[],"replicationLocations":[]}
		],"pageInfo":{"hasNextPage":false}}}}`))
	}))
	defer server.Close()
	nodes, err := FetchSnapshotsForWorkload(context.Background(), server.Client(), server.URL, "workload")
	if err != nil || len(nodes) != 5 {
		t.Fatalf("snapshots = %#v / %v", nodes, err)
	}
	for index, want := range []string{"rubrik, archiv", "archiv", "rubrik", "replica", "unknown"} {
		if got := DetermineSnapshotStorage(nodes[index]); got != want {
			t.Errorf("%s storage = %q, want %q", nodes[index].ID, got, want)
		}
	}
	if !nodes[0].IsLocal || !nodes[0].IsArchived || len(nodes[0].ArchivalLocationInfo) != 1 || nodes[0].ArchivalLocationInfo[0].Name != "vault" ||
		nodes[0].LocalLocations[0].IsActive == nil || !*nodes[0].LocalLocations[0].IsActive {
		t.Fatalf("location metadata lost: %+v", nodes[0])
	}
}

func TestBackupJobLifecycle(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		var payload struct {
			Query     string                     `json:"query"`
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		switch {
		case strings.Contains(payload.Query, "query RSCBackupJobs"):
			var filters map[string]any
			if err := json.Unmarshal(payload.Variables["filters"], &filters); err != nil {
				t.Error(err)
			}
			if string(payload.Variables["first"]) != "10" || string(payload.Variables["after"]) != `"cursor-1"` || filters["searchTerm"] != `host-"one` ||
				!strings.Contains(string(payload.Variables["filters"]), `"lastActivityType":["BACKUP"]`) ||
				!strings.Contains(string(payload.Variables["filters"]), `"objectFid":["workload-1"]`) ||
				!strings.Contains(string(payload.Variables["filters"]), `"clusterId":["cluster-1"]`) ||
				!strings.Contains(string(payload.Variables["filters"]), `"lastActivityStatus":["RUNNING"]`) {
				t.Errorf("unexpected listing variables: %#v", payload.Variables)
			}
			_, _ = writer.Write([]byte(`{"data":{"activitySeriesConnection":{"nodes":[{"activitySeriesId":"activity-1","clusterUuid":"cluster-1","clusterName":"lab","fid":"workload-1","objectId":"cdm-object","objectName":"host-one","objectType":"Fileset","lastActivityStatus":"Running","progress":"25%","startTime":null,"lastUpdated":"2026-11-03T10:00:00Z","isCancelable":true,"isOnDemand":false}],"pageInfo":{"endCursor":"cursor-2","hasNextPage":true}}}}`))
		case strings.Contains(payload.Query, "mutation RSCStartBackup"):
			var input StartBackupInput
			if err := json.Unmarshal(payload.Variables["input"], &input); err != nil {
				t.Error(err)
			}
			if len(input.WorkloadIDs) != 1 || input.WorkloadIDs[0] != "workload-1" || input.SLAID != "" || !strings.Contains(string(payload.Variables["input"]), `"slaId":""`) {
				t.Errorf("unexpected start input: %+v", input)
			}
			_, _ = writer.Write([]byte(`{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[{"workloadId":"workload-1","taskchainUuid":"task-1"}],"errors":[]}}}`))
		case strings.Contains(payload.Query, "mutation RSCCancelBackupTaskchain"):
			if string(payload.Variables["taskchainId"]) != `"task-1"` {
				t.Errorf("unexpected taskchain: %s", payload.Variables["taskchainId"])
			}
			_, _ = writer.Write([]byte(`{"data":{"cancelTaskchain":{"success":true}}}`))
		case strings.Contains(payload.Query, "mutation RSCCancelBackup"):
			var input CancelBackupInput
			if err := json.Unmarshal(payload.Variables["input"], &input); err != nil {
				t.Error(err)
			}
			if input.ActivitySeriesID != "activity-1" || input.ClusterID != "cluster-1" {
				t.Errorf("unexpected cancellation input: %+v", input)
			}
			_, _ = writer.Write([]byte(`{"data":{"cancelActivitySeries":true}}`))
		default:
			t.Errorf("unexpected operation: %s", payload.Query)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	cursor := "cursor-1"
	page, err := client.BackupJobs(context.Background(), BackupJobsOptions{
		First: 10, After: &cursor, WorkloadID: "workload-1", ClusterID: "cluster-1", SearchTerm: `host-"one`, Statuses: []string{"RUNNING"},
	})
	if err != nil || len(page.Jobs) != 1 || !page.HasNextPage || page.EndCursor == nil || *page.EndCursor != "cursor-2" {
		t.Fatalf("unexpected job page: %+v / %v", page, err)
	}
	job := page.Jobs[0]
	if job.IsCancelable == nil || !*job.IsCancelable || job.IsOnDemand == nil || *job.IsOnDemand || job.StartTime != nil ||
		job.Progress == nil || *job.Progress != "25%" || job.WorkloadID != "workload-1" || job.ObjectID != "cdm-object" || job.Status != "Running" {
		t.Fatalf("unexpected job: %+v", job)
	}
	started, err := client.StartBackup(context.Background(), StartBackupInput{WorkloadIDs: []string{job.WorkloadID}})
	if err != nil || len(started.Taskchains) != 1 || started.Taskchains[0].TaskchainID != "task-1" {
		t.Fatalf("unexpected initiation: %+v / %v", started, err)
	}
	if accepted, err := client.CancelBackupTaskchain(context.Background(), started.Taskchains[0].TaskchainID); err != nil || !accepted {
		t.Fatalf("taskchain cancellation: %v / %v", accepted, err)
	}
	if accepted, err := client.CancelBackup(context.Background(), CancelBackupInput{ActivitySeriesID: job.ActivitySeriesID, ClusterID: job.ClusterID}); err != nil || !accepted {
		t.Fatalf("activity cancellation: %v / %v", accepted, err)
	}
	if requests != 4 {
		t.Fatalf("requests = %d, want 4", requests)
	}
}

func TestBackupJobResponses(t *testing.T) {
	for _, test := range []struct {
		name         string
		operation    string
		body         string
		wantError    bool
		wantTasks    int
		wantFailures int
	}{
		{name: "empty listing", operation: "list", body: `{"data":{"activitySeriesConnection":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`},
		{name: "missing listing", operation: "list", body: `{"data":{}}`, wantError: true},
		{name: "missing nodes", operation: "list", body: `{"data":{"activitySeriesConnection":{"pageInfo":{"hasNextPage":false}}}}`, wantError: true},
		{name: "missing page status", operation: "list", body: `{"data":{"activitySeriesConnection":{"nodes":[],"pageInfo":{}}}}`, wantError: true},
		{name: "missing cursor", operation: "list", body: `{"data":{"activitySeriesConnection":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`, wantError: true},
		{name: "nonadvancing cursor", operation: "list", body: `{"data":{"activitySeriesConnection":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"same"}}}}`, wantError: true},
		{name: "missing identity", operation: "list", body: `{"data":{"activitySeriesConnection":{"nodes":[{"activitySeriesId":"activity"}],"pageInfo":{"hasNextPage":false}}}}`, wantError: true},
		{name: "partial start", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[{"workloadId":"workload-1","taskchainUuid":"task-1"}],"errors":[{"workloadId":"workload-2","error":"permission denied"}]}}}`, wantError: true, wantTasks: 1, wantFailures: 1},
		{name: "failed start", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[],"errors":[{"workloadId":"workload-1","error":"unsupported"},{"workloadId":"workload-2","error":"unsupported"}]}}}`, wantError: true, wantFailures: 2},
		{name: "missing start", operation: "start", body: `{"data":{"takeOnDemandSnapshot":null}}`, wantError: true},
		{name: "missing start outcomes", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{}}}`, wantError: true},
		{name: "omitted workload", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[{"workloadId":"workload-1","taskchainUuid":"task-1"}],"errors":[]}}}`, wantError: true, wantTasks: 1},
		{name: "duplicate outcome", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[{"workloadId":"workload-1","taskchainUuid":"task-1"},{"workloadId":"workload-1","taskchainUuid":"task-1"}],"errors":[]}}}`, wantError: true, wantTasks: 2},
		{name: "unexpected workload", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[{"workloadId":"other","taskchainUuid":"task-1"}],"errors":[]}}}`, wantError: true, wantTasks: 1},
		{name: "missing task ID", operation: "start", body: `{"data":{"takeOnDemandSnapshot":{"taskchainUuids":[{"workloadId":"workload-1"}],"errors":[]}}}`, wantError: true, wantTasks: 1},
		{name: "cancel rejected", operation: "cancel", body: `{"data":{"cancelActivitySeries":false}}`, wantError: true},
		{name: "cancel missing", operation: "cancel", body: `{"data":{}}`, wantError: true},
		{name: "task cancel rejected", operation: "task", body: `{"data":{"cancelTaskchain":{"success":false}}}`, wantError: true},
		{name: "task cancel missing", operation: "task", body: `{"data":{"cancelTaskchain":null}}`, wantError: true},
		{name: "task cancel status missing", operation: "task", body: `{"data":{"cancelTaskchain":{}}}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				requests++
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			var err error
			switch test.operation {
			case "list":
				options := BackupJobsOptions{}
				if test.name == "nonadvancing cursor" {
					cursor := "same"
					options.After = &cursor
				}
				page, listErr := FetchBackupJobs(context.Background(), server.Client(), server.URL, options)
				err = listErr
				if !test.wantError && (page.Jobs == nil || len(page.Jobs) != 0 || page.HasNextPage) {
					t.Fatalf("unexpected empty page: %+v", page)
				}
				if test.wantError && page.Jobs != nil {
					t.Fatal("listing failure returned partial jobs")
				}
			case "start":
				result, startErr := StartBackup(context.Background(), server.Client(), server.URL, StartBackupInput{WorkloadIDs: []string{"workload-1", "workload-2"}, SLAID: "sla-1"})
				err = startErr
				if len(result.Taskchains) != test.wantTasks || len(result.Errors) != test.wantFailures {
					t.Fatalf("lost mutation outcomes: %+v", result)
				}
			case "cancel":
				accepted, cancelErr := CancelBackup(context.Background(), server.Client(), server.URL, CancelBackupInput{ActivitySeriesID: "activity-1", ClusterID: "cluster-1"})
				err = cancelErr
				if accepted {
					t.Fatal("unacknowledged cancellation reported success")
				}
			case "task":
				accepted, cancelErr := CancelBackupTaskchain(context.Background(), server.Client(), server.URL, "task-1")
				err = cancelErr
				if accepted {
					t.Fatal("unacknowledged taskchain cancellation reported success")
				}
			}
			if (err != nil) != test.wantError || requests != 1 {
				t.Fatalf("error/requests = %v/%d, want error=%v and one request", err, requests, test.wantError)
			}
		})
	}
}

func TestBackupJobRequestsNotReplayed(t *testing.T) {
	for _, operation := range []string{"list", "start", "cancel", "task"} {
		for _, failure := range []string{"HTTP", "GraphQL", "canceled"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
					requests++
					writer.Header().Set("Content-Type", "application/json")
					if failure == "HTTP" {
						writer.WriteHeader(http.StatusServiceUnavailable)
						_, _ = writer.Write([]byte(`{"message":"unavailable"}`))
					} else {
						_, _ = writer.Write([]byte(`{"errors":[{"message":"not authorized"}]}`))
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if failure == "canceled" {
					cancel()
				}
				var err error
				switch operation {
				case "list":
					_, err = FetchBackupJobs(ctx, server.Client(), server.URL, BackupJobsOptions{})
				case "start":
					_, err = StartBackup(ctx, server.Client(), server.URL, StartBackupInput{WorkloadIDs: []string{"workload-1"}})
				case "cancel":
					_, err = CancelBackup(ctx, server.Client(), server.URL, CancelBackupInput{ActivitySeriesID: "activity-1", ClusterID: "cluster-1"})
				case "task":
					_, err = CancelBackupTaskchain(ctx, server.Client(), server.URL, "task-1")
				}
				wantRequests := 1
				if failure == "canceled" {
					wantRequests = 0
					if !errors.Is(err, context.Canceled) {
						t.Errorf("cancellation identity lost: %v", err)
					}
				}
				if err == nil || requests != wantRequests {
					t.Fatalf("error/requests = %v/%d, want error and %d requests", err, requests, wantRequests)
				}
			})
		}
	}
}

func TestBackupJobInvalidInputs(t *testing.T) {
	for _, input := range []StartBackupInput{
		{}, {WorkloadIDs: []string{""}}, {WorkloadIDs: []string{" workload-1"}},
		{WorkloadIDs: []string{"workload-1", "WORKLOAD-1"}},
	} {
		if _, err := StartBackup(context.Background(), nil, "", input); err == nil || !strings.Contains(err.Error(), "backup initiation") {
			t.Errorf("invalid start input %+v: %v", input, err)
		}
	}
	for _, input := range []CancelBackupInput{{}, {ActivitySeriesID: "activity"}, {ClusterID: "cluster"}} {
		if _, err := CancelBackup(context.Background(), nil, "", input); err == nil || !strings.Contains(err.Error(), "backup cancellation") {
			t.Errorf("invalid cancel input %+v: %v", input, err)
		}
	}
	if _, err := CancelBackupTaskchain(context.Background(), nil, "", " "); err == nil || !strings.Contains(err.Error(), "taskchain ID") {
		t.Errorf("empty taskchain input: %v", err)
	}
	cursor := " "
	for _, options := range []BackupJobsOptions{{First: -1}, {First: 101}, {After: &cursor}} {
		if _, err := FetchBackupJobs(context.Background(), nil, "", options); err == nil || !strings.Contains(err.Error(), "backup jobs") {
			t.Errorf("invalid listing options %+v: %v", options, err)
		}
	}
}

func TestParseRSCTimeframe(t *testing.T) {
	baseTime := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	if got, err := ParseTimeframe("", baseTime, false); err != nil || !got.IsZero() {
		t.Fatalf("parseRSCTimeframe empty = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("7d", baseTime, false); err != nil || !got.Equal(time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 7d = %v, want %v, err=%v", got, time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), err)
	}
	if got, err := ParseTimeframe("14 days", baseTime, false); err != nil || !got.Equal(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 14 days = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("7d ago", baseTime, false); err != nil || !got.Equal(time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 7d ago = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("2w", baseTime, false); err != nil || !got.Equal(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 2w = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("2 weeks", baseTime, false); err != nil || !got.Equal(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 2 weeks = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("24h", baseTime, false); err != nil || !got.Equal(baseTime.Add(-24*time.Hour)) {
		t.Fatalf("parseRSCTimeframe 24h = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("3 hours", baseTime, false); err != nil || !got.Equal(baseTime.Add(-3*time.Hour)) {
		t.Fatalf("parseRSCTimeframe 3 hours = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("30m", baseTime, false); err != nil || !got.Equal(baseTime.Add(-30*time.Minute)) {
		t.Fatalf("parseRSCTimeframe 30m = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("1mo", baseTime, false); err != nil || !got.Equal(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 1mo = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("1 month", baseTime, false); err != nil || !got.Equal(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 1 month = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("1y", baseTime, false); err != nil || !got.Equal(time.Date(2025, 8, 15, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe 1y = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("today", baseTime, false); err != nil || got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("parseRSCTimeframe today start = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("today", baseTime, true); err != nil || got.Hour() != 23 || got.Minute() != 59 {
		t.Fatalf("parseRSCTimeframe today end = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("yesterday", baseTime, false); err != nil || got.Day() != 14 || got.Hour() != 0 {
		t.Fatalf("parseRSCTimeframe yesterday start = %v, err=%v", got, err)
	}

	if got, err := ParseTimeframe("2026-08-01", baseTime, false); err != nil || !got.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("parseRSCTimeframe date start = %v, want %v, err=%v", got, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), err)
	}
	if got, err := ParseTimeframe("2026-08-01", baseTime, true); err != nil || got.Hour() != 23 || got.Minute() != 59 {
		t.Fatalf("parseRSCTimeframe date end = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("2026-08-01 15:04:05", baseTime, false); err != nil || got.Hour() != 15 || got.Minute() != 4 || got.Second() != 5 {
		t.Fatalf("parseRSCTimeframe datetime = %v, err=%v", got, err)
	}
	if got, err := ParseTimeframe("2026-08-01T15:04:05Z", baseTime, false); err != nil || got.Hour() != 15 || got.Minute() != 4 {
		t.Fatalf("parseRSCTimeframe RFC3339 = %v, err=%v", got, err)
	}

	if _, err := ParseTimeframe("invalid-timeframe-xyz", baseTime, false); err == nil {
		t.Fatal("expected error on invalid timeframe")
	}
}

func TestParseRSCSnapshotDate(t *testing.T) {

	d, err := ParseSnapshotDate("2026-08-15T02:00:14.000Z")
	if err != nil || d.Year() != 2026 || d.Month() != 8 || d.Day() != 15 || d.Hour() != 2 || d.Minute() != 0 || d.Second() != 14 {
		t.Fatalf("parseRSCSnapshotDate RFC3339 = %v, err=%v", d, err)
	}

	d2, err := ParseSnapshotDate(int64(1786759214000))
	if err != nil || d2.Year() != 2026 {
		t.Fatalf("parseRSCSnapshotDate int64 ms = %v, err=%v", d2, err)
	}

	d3, err := ParseSnapshotDate(float64(1786759214000))
	if err != nil || d3.Year() != 2026 {
		t.Fatalf("parseRSCSnapshotDate float64 ms = %v, err=%v", d3, err)
	}

	dEmpty, err := ParseSnapshotDate("")
	if err != nil || !dEmpty.IsZero() {
		t.Fatalf("parseRSCSnapshotDate empty = %v, err=%v", dEmpty, err)
	}
}

func TestEnrichRSCSnapshotsNASSourcesRejectsAmbiguousSourceName(t *testing.T) {
	rows := []SnapshotRow{{Instance: "temp", Path: "temp", ShareType: "SMB"}}
	sources := []NASSource{
		{Name: "temp", HostAddress: "ncl1-1-vs-46.eu.socionext.com", ExportPoint: "temp", ShareType: "SMB"},
		{Name: "temp", HostAddress: "ncl1-1-vs-86.eu.socionext.com", ExportPoint: "temp", ShareType: "SMB"},
	}

	got := EnrichSnapshotsNASSources(rows, sources)
	if got[0].TargetHost != "" {
		t.Fatalf("ambiguous NAS source assigned target host %q", got[0].TargetHost)
	}
}

func TestMatchesRSCSnapshotSearchTerm(t *testing.T) {
	snap := SnapshotRow{
		ID:         "snap-123",
		Path:       "/vol/vol_projects/dept",
		Volume:     "vol_projects",
		SVM:        "pssvm01",
		TargetHost: "192.168.1.50",
		SLAName:    "Gold-Daily",
		ShareType:  "NFS",
		Storage:    "rubrik",
	}

	if !MatchesSnapshotSearchTerm(snap, "vol_projects") {
		t.Fatal("expected match on volume name")
	}

	if !MatchesSnapshotSearchTerm(snap, "dept") {
		t.Fatal("expected match on path segment")
	}

	if !MatchesSnapshotSearchTerm(snap, "/vol/vol_projects/dept") {
		t.Fatal("expected match on full path")
	}

	if !MatchesSnapshotSearchTerm(snap, "pssvm01") {
		t.Fatal("expected match on SVM")
	}

	if !MatchesSnapshotSearchTerm(snap, "Gold") {
		t.Fatal("expected match on SLA")
	}

	if !MatchesSnapshotSearchTerm(snap, "*proj*") {
		t.Fatal("expected match on wildcard *proj*")
	}

	if MatchesSnapshotSearchTerm(snap, "nonexistent_term") {
		t.Fatal("expected no match on nonexistent_term")
	}
}

func TestFilterRSCSnapshots(t *testing.T) {
	t1 := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)

	rows := []SnapshotRow{
		{ID: "s1", Path: "/vol/data1", Volume: "data1", SVM: "svm1", SLAName: "Daily-Gold", ShareType: "NFS", Date: t1, Storage: "rubrik"},
		{ID: "s2", Path: "/vol/data2", Volume: "data2", SVM: "svm2", SLAName: "Weekly-Silver", ShareType: "SMB", Date: t2, Storage: "archiv"},
		{ID: "s3", Path: "/vol/data3", Volume: "data3", SVM: "svm1", SLAName: "Daily-Gold", ShareType: "NFS", Date: t3, Storage: "migrating"},
	}

	got := FilterSnapshots(rows, "data2", "", "", "", time.Time{}, time.Time{}, nil, "", "", "")
	if len(got) != 1 || got[0].ID != "s2" {
		t.Fatalf("filter by search term data2 = %#v", got)
	}

	got = FilterSnapshots(rows, "", "Daily-Gold", "", "", time.Time{}, time.Time{}, nil, "", "", "")
	if len(got) != 2 {
		t.Fatalf("filter by SLA Daily-Gold = %#v", got)
	}

	got = FilterSnapshots(rows, "", "", "data3", "", time.Time{}, time.Time{}, nil, "", "", "")
	if len(got) != 1 || got[0].ID != "s3" {
		t.Fatalf("filter by Volume data3 = %#v", got)
	}

	got = FilterSnapshots(rows, "", "", "", "svm1", time.Time{}, time.Time{}, nil, "", "", "")
	if len(got) != 2 {
		t.Fatalf("filter by SVM svm1 = %#v", got)
	}

	got = FilterSnapshots(rows, "", "", "", "", t2, time.Time{}, nil, "", "", "")
	if len(got) != 2 || got[0].ID != "s2" || got[1].ID != "s3" {
		t.Fatalf("filter by since t2 = %#v", got)
	}

	got = FilterSnapshots(rows, "", "", "", "", time.Time{}, t2, nil, "", "", "")
	if len(got) != 2 || got[0].ID != "s1" || got[1].ID != "s2" {
		t.Fatalf("filter by till t2 = %#v", got)
	}

	got = FilterSnapshots(rows, "", "", "", "", time.Time{}, time.Time{}, []string{"SMB"}, "", "", "")
	if len(got) != 1 || got[0].ID != "s2" {
		t.Fatalf("filter by Type SMB = %#v", got)
	}

	got = FilterSnapshots(rows, "", "", "", "", time.Time{}, time.Time{}, nil, "", "archiv", "")
	if len(got) != 1 || got[0].ID != "s2" {
		t.Fatalf("filter by Storage archiv = %#v", got)
	}
}

func TestSortRSCSnapshots(t *testing.T) {
	t1 := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	rows := []SnapshotRow{
		{ID: "s1", Path: "/vol/b", Date: t1},
		{ID: "s2", Path: "/vol/a", Date: t1},
		{ID: "s3", Path: "/vol/a", Date: t2},
	}
	SortSnapshots(rows)

	if rows[0].ID != "s3" || rows[1].ID != "s2" || rows[2].ID != "s1" {
		t.Fatalf("sorted rows order = [%s, %s, %s], want [s3, s2, s1]", rows[0].ID, rows[1].ID, rows[2].ID)
	}
}

func TestFetchRSCSnapshotsGraphQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.Contains(req.Query, "snapshotOfASnappableConnection") {
			for _, field := range []string{"size", "snapshotDiskSize", "indexState", "isArchived", "isReplica", "isLocal", "timeTaken", "backupDuration", "archivalLocationInfo"} {
				if strings.Contains(req.Query, " "+field+" ") {
					http.Error(w, "unsupported CdmSnapshot field: "+field, http.StatusBadRequest)
					return
				}
			}
			if !strings.Contains(req.Query, "isOnDemand: isOnDemandSnapshot") {
				http.Error(w, "isOnDemandSnapshot alias missing", http.StatusBadRequest)
				return
			}
			if !strings.Contains(req.Query, `workloadId: "fileset-child-1"`) {
				http.Error(w, "unexpected workload ID", http.StatusBadRequest)
				return
			}
			resp := map[string]any{"data": map[string]any{
				"snapshotOfASnappableConnection": map[string]any{
					"nodes": []map[string]any{{
						"id":         "snap-001",
						"date":       "2026-08-15T02:00:14.000Z",
						"isOnDemand": true,
						"cloudState": "",
						"slaDomain":  map[string]any{"id": "sla-1", "name": "Daily-Gold"},
					}},
					"pageInfo": map[string]any{"hasNextPage": false},
				},
			}}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(req.Query, "filesetTemplates") {
			if strings.Contains(req.Query, "snapshotConnection") {
				http.Error(w, "snapshotConnection must not be nested in fileset discovery", http.StatusBadRequest)
				return
			}
			if strings.Contains(req.Query, "NAS_HOST_ROOT") {
				resp := map[string]any{
					"data": map[string]any{
						"filesetTemplates": map[string]any{
							"nodes": []map[string]any{
								{
									"name":      "NAS_ALL_NFS",
									"includes":  []string{"/vol/projects"},
									"shareType": "NFS",
									"cluster":   map[string]any{"name": "Rubrikcluster01"},
									"configuredSlaDomain": map[string]any{
										"id":   "sla-1",
										"name": "Daily-Gold",
									},
									"effectiveSlaDomain": map[string]any{
										"id":   "sla-1",
										"name": "Daily-Gold",
									},
									"descendantConnection": map[string]any{
										"nodes": []map[string]any{
											{
												"id":   "fileset-child-1",
												"name": "projects_fileset",
												"physicalPath": []map[string]any{
													{"name": "/vol/projects"},
													{"name": "192.168.1.10"},
												},
												"configuredSlaDomain": map[string]any{"id": "sla-1", "name": "Daily-Gold"},
												"effectiveSlaDomain":  map[string]any{"id": "sla-1", "name": "Daily-Gold"},
												"cluster":             map[string]any{"name": "Rubrikcluster01"},
											},
										},
										"pageInfo": map[string]any{"hasNextPage": false},
									},
								},
							},
							"pageInfo": map[string]any{"hasNextPage": false},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}

			resp := map[string]any{
				"data": map[string]any{
					"filesetTemplates": map[string]any{
						"nodes":    []any{},
						"pageInfo": map[string]any{"hasNextPage": false},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(req.Query, "nasShares") {
			resp := map[string]any{
				"data": map[string]any{
					"nasShares": map[string]any{
						"nodes":    []any{},
						"pageInfo": map[string]any{"hasNextPage": false},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "unknown query", http.StatusBadRequest)
	}))
	defer server.Close()

	ctx := context.Background()
	client := server.Client()

	snapshots, err := FetchSnapshots(ctx, client, server.URL, 5)
	if err != nil {
		t.Fatalf("fetchRSCSnapshots failed: %v", err)
	}

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}

	snap := snapshots[0]
	if snap.ID != "snap-001" {
		t.Errorf("snap ID = %q, want snap-001", snap.ID)
	}
	if snap.Path != "/vol/projects" {
		t.Errorf("snap Path = %q, want /vol/projects", snap.Path)
	}
	if snap.SLAName != "Daily-Gold" {
		t.Errorf("snap SLA = %q, want Daily-Gold", snap.SLAName)
	}
	if snap.Size != 0 {
		t.Errorf("snap Size = %d, want 0", snap.Size)
	}
	if snap.Duration != 0 {
		t.Errorf("snap Duration = %v, want 0", snap.Duration)
	}
	if !snap.IsOnDemand {
		t.Error("snap IsOnDemand = false, want true")
	}
	if snap.Storage != "rubrik" {
		t.Errorf("snap Storage = %q, want rubrik", snap.Storage)
	}
}

func TestFilterBrokenRSCSnapshots(t *testing.T) {
	rows := []SnapshotRow{
		{ID: "healthy", TargetHost: "ncl1-1-vs-21.example.com", Volume: "data"},
		{ID: "missing-host", Volume: "data"},
		{ID: "missing-volume", TargetHost: "ncl1-1-vs-21.example.com"},
		{ID: "unknown-values", TargetHost: "unknown", Volume: "-"},
	}

	got := FilterBrokenSnapshots(rows)
	if len(got) != 3 || got[0].ID != "missing-host" || got[1].ID != "missing-volume" || got[2].ID != "unknown-values" {
		t.Fatalf("broken snapshots = %#v, want all rows missing host or volume", got)
	}
}

func TestMatchActivityToSnapshot(t *testing.T) {
	snapTime := time.Date(2026, 8, 29, 22, 0, 2, 0, time.UTC)
	snap := SnapshotRow{
		Date:     snapTime,
		Volume:   "gcc_proj3",
		Path:     "/gcc_proj3",
		Instance: "projects_fileset",
	}

	actMatching := ActivitySeriesGQLNode{
		ObjectName:      "/gcc_proj3: ** from ncl1-1-vs-86.eu.socionext.com:/gcc_proj3",
		StartTime:       snapTime.Add(-1 * time.Second),
		LastUpdated:     snapTime.Add(2 * time.Hour),
		LogicalSize:     14271762213049,
		DataTransferred: 365739209193,
	}

	if !MatchActivityToSnapshot(snap, actMatching) {
		t.Fatal("expected activity to match snapshot")
	}

	actOld := actMatching
	actOld.StartTime = snapTime.Add(-2 * time.Hour)
	if MatchActivityToSnapshot(snap, actOld) {
		t.Fatal("expected old activity to not match snapshot")
	}

	actUnrelated := actMatching
	actUnrelated.ObjectName = "/other_vol: **"
	if MatchActivityToSnapshot(snap, actUnrelated) {
		t.Fatal("expected unrelated activity to not match snapshot")
	}
}

func TestEnrichRSCSnapshotsWithBackupActivitiesMock(t *testing.T) {
	snapTime := time.Date(2026, 8, 29, 22, 0, 2, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Query, "activitySeriesConnection") {
			resp := map[string]any{
				"data": map[string]any{
					"activitySeriesConnection": map[string]any{
						"nodes": []map[string]any{
							{
								"id":                 1001,
								"activitySeriesId":   "series-1",
								"objectId":           "Fileset:::04cf26ae",
								"objectName":         "/gcc_proj3: **",
								"objectType":         "NAS_FILESET",
								"startTime":          snapTime.Add(-1 * time.Second).Format(time.RFC3339Nano),
								"lastUpdated":        snapTime.Add(2 * time.Hour).Format(time.RFC3339Nano),
								"lastActivityType":   "Backup",
								"lastActivityStatus": "Success",
								"dataTransferred":    1000000,
								"logicalSize":        5000000,
							},
						},
						"pageInfo": map[string]any{"hasNextPage": false},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "unknown query", http.StatusBadRequest)
	}))
	defer server.Close()

	ctx := context.Background()
	client := server.Client()

	rows := []SnapshotRow{
		{Date: snapTime, Volume: "gcc_proj3", Path: "/gcc_proj3"},
	}
	filters := SnapshotFilters{Volume: "gcc_proj3"}

	enriched, err := EnrichSnapshotsWithBackupActivities(ctx, client, server.URL, rows, filters, 0, 1)
	if err != nil {
		t.Fatalf("enrich snapshots: %v", err)
	}
	if len(enriched) != 1 {
		t.Fatalf("expected 1 enriched row, got %d", len(enriched))
	}
	if enriched[0].Size != 5000000 {
		t.Errorf("enriched Size = %d, want 5000000", enriched[0].Size)
	}
	if enriched[0].Duration != 2*time.Hour+1*time.Second {
		t.Errorf("enriched Duration = %v, want 2h0m1s", enriched[0].Duration)
	}
}

func TestEnrichRSCSnapshotsQueriesExactWorkload(t *testing.T) {
	snapTime := time.Date(2026, 8, 29, 22, 0, 2, 0, time.UTC)
	const workloadID = "04cf26ae-46cb-4d05-8cbf-efc83cb01d70"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req.Query, `objectFid: ["`+workloadID+`"]`) {
			t.Errorf("activity query does not filter workload ID: %s", req.Query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"activitySeriesConnection": map[string]any{
					"nodes": []map[string]any{{
						"id":          1001,
						"objectId":    "Fileset:::different-id",
						"objectName":  "unrelated name",
						"startTime":   snapTime.Format(time.RFC3339Nano),
						"lastUpdated": snapTime.Add(time.Hour).Format(time.RFC3339Nano),
						"logicalSize": 5000000,
					}},
					"pageInfo": map[string]any{"hasNextPage": false},
				},
			},
		})
	}))
	defer server.Close()

	rows := []SnapshotRow{{WorkloadID: workloadID, Date: snapTime}}
	enriched, err := EnrichSnapshotsWithBackupActivities(context.Background(), server.Client(), server.URL, rows, SnapshotFilters{}, 0, 1)
	if err != nil {
		t.Fatalf("enrich snapshots: %v", err)
	}
	if enriched[0].Size != 5000000 || enriched[0].Duration != time.Hour {
		t.Fatalf("enriched snapshot = %+v, want workload activity size and duration", enriched[0])
	}
}
