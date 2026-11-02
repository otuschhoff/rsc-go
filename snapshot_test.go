package rsc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
