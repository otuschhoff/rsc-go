package rsc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRSCGraphQLEndpoint(t *testing.T) {
	if got := NormalizeGraphQLEndpoint("https://tenant.my.rubrik.com"); got != "https://tenant.my.rubrik.com/api/graphql" {
		t.Fatalf("unexpected normalized endpoint: %q", got)
	}
	if got := NormalizeGraphQLEndpoint("https://graphql.rubrik.com/api/graphql"); got != "https://graphql.rubrik.com/api/graphql" {
		t.Fatalf("expected unchanged endpoint, got %q", got)
	}
}

func TestFetchRSCSLAPolicies(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		requestCount++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read SLA request: %v", err)
		}
		if requestCount == 2 && !strings.Contains(string(body), `"after":"cursor-1"`) {
			t.Fatalf("expected second page cursor in request: %s", body)
		}
		if requestCount == 1 {
			_, _ = w.Write([]byte(`{
  "data": {"slaDomains": {
    "nodes": [{
      "id": "global-1",
      "name": "Gold",
      "isDefault": true,
      "objectTypes": ["VMWARE_VIRTUAL_MACHINE"],
      "snapshotSchedule": {"daily": {"basicSchedule": {"frequency": 1, "retention": 30, "retentionUnit": "DAYS"}}},
      "archivalSpecs": [{"threshold": 30, "thresholdUnit": "DAYS", "storageSetting": {"name": "s3-archive"}}]
    }],
    "pageInfo": {"endCursor": "cursor-1", "hasNextPage": true}
  }}
}`))
			return
		}
		_, _ = w.Write([]byte(`{
  "data": {"slaDomains": {
    "nodes": [{
      "id": "cluster-1",
      "name": "Local Policy",
      "cdmId": "cdm-1",
      "cluster": {"name": "lab-cluster"},
      "snapshotSchedule": {"weekly": {"basicSchedule": {"frequency": 1, "retention": 8, "retentionUnit": "WEEKS"}}},
      "archivalSpec": {"threshold": 8, "thresholdUnit": "WEEKS", "archivalLocationName": "vault"}
    }],
    "pageInfo": {"endCursor": null, "hasNextPage": false}
  }}
}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	policies, err := FetchSLAPolicies(ctx, server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchRSCSLAPolicies returned error: %v", err)
	}
	if requestCount != 2 || len(policies) != 2 {
		t.Fatalf("expected two pages and policies, requests=%d policies=%d", requestCount, len(policies))
	}
	if policies[0].Kind != "global" || policies[0].Schedule != "daily every 1" || policies[0].Retention != "30 DAYS" || policies[0].RetentionDays != 30 || policies[0].Archive != "after 30 DAYS: s3-archive" || !policies[0].Default {
		t.Fatalf("unexpected global policy: %+v", policies[0])
	}
	if policies[1].Kind != "cluster" || policies[1].ID != "cluster-1" || policies[1].Cluster != "lab-cluster" || policies[1].Schedule != "weekly every 1" || policies[1].RetentionDays != 56 || policies[1].Archive != "after 8 WEEKS: vault" {
		t.Fatalf("unexpected cluster policy: %+v", policies[1])
	}
}

func TestExecuteRSCSLAGraphQLRetriesTransientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")

		attempts++
		if attempts < 3 {
			http.Error(writer, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer server.Close()

	var response struct {
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	if err := ExecuteSLAGraphQL(context.Background(), server.Client(), server.URL, "query { ok }", &response); err != nil {
		t.Fatalf("executeRSCSLAGraphQL: %v", err)
	}
	if attempts != 3 || !response.Data.OK {
		t.Fatalf("attempts/result = %d/%#v, want 3/success", attempts, response)
	}
}

func TestRSCRetentionDays(t *testing.T) {
	for _, test := range []struct {
		unit      string
		retention int
		want      int
	}{
		{unit: "MINUTES", retention: 2880, want: 2},
		{unit: "HOURS", retention: 48, want: 2},
		{unit: "DAYS", retention: 3, want: 3},
		{unit: "WEEKS", retention: 4, want: 28},
		{unit: "MONTHS", retention: 5, want: 150},
		{unit: "QUARTERS", retention: 2, want: 180},
		{unit: "YEARS", retention: 2, want: 730},
	} {
		schedule := SLASnapshotSchedule{}
		schedule.Daily.BasicSchedule.Retention = test.retention
		schedule.Daily.BasicSchedule.RetentionUnit = test.unit
		if got := RetentionDays(schedule); got != test.want {
			t.Fatalf("rscRetentionDays(%s) = %d, want %d", test.unit, got, test.want)
		}
	}
}

func TestFilterRSCSLAPoliciesByName(t *testing.T) {
	policies := []SLAPolicy{
		{Name: "Gold"},
		{Name: "Gold_Rubrikcluster01"},
		{Name: "Bronze"},
	}
	if got := FilterSLAPoliciesByName(policies, "Gold*"); len(got) != 2 {
		t.Fatalf("Gold* matched %d policies, want 2", len(got))
	}
	if got := FilterSLAPoliciesByName(policies, "bronze"); len(got) != 1 || got[0].Name != "Bronze" {
		t.Fatalf("bronze matched %+v, want Bronze", got)
	}
	if got := FilterSLAPoliciesByName(policies, "NoSuch%"); len(got) != 0 {
		t.Fatalf("NoSuch%% matched %d policies, want 0", len(got))
	}
}

func TestFilterRSCSLAPoliciesBySourceTypes(t *testing.T) {
	policies := []SLAPolicy{
		{Name: "NAS policy", ObjectTypes: []string{"NAS_OBJECT_TYPE"}},
		{Name: "VM policy", ObjectTypes: []string{"VMWARE_VIRTUAL_MACHINE"}},
		{Name: "Mixed policy", ObjectTypes: []string{"NAS", "VMWARE_VIRTUAL_MACHINE"}},
	}
	if got := FilterSLAPoliciesBySourceTypes(policies, []string{"nas"}); len(got) != 2 || got[0].Name != "NAS policy" || got[1].Name != "Mixed policy" {
		t.Fatalf("NAS filter returned %+v, want both NAS policies", got)
	}
	if got := FilterSLAPoliciesBySourceTypes(policies, []string{"VMWARE_VIRTUAL_MACHINE", "other"}); len(got) != 2 {
		t.Fatalf("multi-source filter returned %d policies, want 2", len(got))
	}
}

func TestFilterRSCSLAAssignments(t *testing.T) {
	assignments := []SLAAssignment{
		{Instance: "share-a", Cluster: "Rubrikcluster01"},
		{Instance: "share-b", Cluster: "other-cluster"},
	}
	if got := FilterSLAAssignments(assignments, "Rubrikcluster01"); len(got) != 1 || got[0].Instance != "share-a" {
		t.Fatalf("unexpected named-cluster assignments: %+v", got)
	}
	if got := FilterSLAAssignments(assignments, "%"); len(got) != 2 {
		t.Fatalf("wildcard cluster returned %d assignments, want 2", len(got))
	}
}

func TestMatchesRSCSLAReferenceByIDOrName(t *testing.T) {
	if !MatchesSLAReference("policy-id", "other", "policy-id", "policy") {
		t.Fatal("expected matching SLA ID to be accepted")
	}
	if !MatchesSLAReference("effective-fid", "SNEU_DFS_14d_3m", "policy-id", "sneu_dfs_14d_3m") {
		t.Fatal("expected matching SLA name to be accepted")
	}
	if MatchesSLAReference("effective-fid", "other", "policy-id", "policy") {
		t.Fatal("did not expect unrelated SLA reference to match")
	}
}

func TestRSCNASSourceLookupKeysHandlesFilesetPaths(t *testing.T) {
	keys := NASSourceLookupKeys(SLAAssignment{
		Instance: "/dabu_25: **",
		Path:     "/dabu_25: **",
	})
	for _, want := range []string{"dabu_25"} {
		found := false
		for _, key := range keys {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("lookup keys = %#v, want %q", keys, want)
		}
	}
}

func TestNormalizeRSCDiskMedia(t *testing.T) {
	if got := NormalizeDiskMedia("SSD", "HDD"); got != "FLASH" {
		t.Fatalf("expected SSD to normalize to FLASH, got %q", got)
	}
	if got := NormalizeDiskMedia("HDD", "SATA"); got != "HDD" {
		t.Fatalf("expected HDD to stay HDD, got %q", got)
	}
	if got := NormalizeDiskMedia("", "NVME"); got != "NVME" {
		t.Fatalf("expected mode-based media to be preserved, got %q", got)
	}
	if got := NormalizeDiskMedia("", ""); got != "UNKNOWN" {
		t.Fatalf("expected empty media to normalize to UNKNOWN, got %q", got)
	}
}
