package rsc

import (
	"testing"
)

func TestFilterRSCFilesets(t *testing.T) {
	rows := []SLAAssignment{
		{ShareType: "NFS", TargetHost: "lif-a", Host: "other-host", Path: "/data", SVM: "svm1", Volume: "data", SLAID: "sla-1", SLAName: "Daily"},
		{ShareType: "SMB", TargetHost: "lif-b", Path: "/share", SVM: "svm2", Volume: "share", SLAID: "sla-2", SLAName: "Monthly"},
	}
	got := FilterFilesets(rows, []string{"nfs"}, "daily", "svm1", []string{"lif-a"}, "data", "/data", false)
	if len(got) != 1 || got[0].Volume != "data" {
		t.Fatalf("combined filters = %#v", got)
	}
	if got := FilterFilesets(rows, nil, "sla-2", "", nil, "", "", false); len(got) != 1 || got[0].SLAName != "Monthly" {
		t.Fatalf("SLA ID filter = %#v", got)
	}
}

func TestFilterRSCFilesetsHidesUnknownTypeAndHostByDefault(t *testing.T) {
	rows := []SLAAssignment{
		{ShareType: "", TargetHost: "", Host: "", Path: "uuid-1", SLAID: "sla-1", SLAName: "VMware"},
		{ShareType: "unknown", TargetHost: "unknown", Host: "unknown", Path: "uuid-2", SLAID: "sla-2", SLAName: "VMware"},
		{ShareType: "NFS", TargetHost: "ncl1-1-vs-21.eu.socionext.com", Path: "/fme_ba", SVM: "ncl1-1-vs-20", Volume: "fme_ba_dest", SLAID: "sla-3", SLAName: "NFS_SLA"},
		{ShareType: "NFS", TargetHost: "", Host: "", Path: "/fme_tools", Volume: "fme_tools_dest", SLAID: "sla-4", SLAName: "NFS_SLA"},
		{ShareType: "", TargetHost: "host-only.example.com", Host: "host-only.example.com", Path: "/share", SLAID: "sla-5", SLAName: "SMB_SLA"},
	}

	defaultFiltered := FilterFilesets(rows, nil, "", "", nil, "", "", false)
	if len(defaultFiltered) != 3 {
		t.Fatalf("expected 3 rows by default, got %d: %#v", len(defaultFiltered), defaultFiltered)
	}
	for _, r := range defaultFiltered {
		if r.Path == "uuid-1" || r.Path == "uuid-2" {
			t.Fatalf("row with unknown type and host should be hidden: %#v", r)
		}
	}

	allFiltered := FilterFilesets(rows, nil, "", "", nil, "", "", true)
	if len(allFiltered) != 5 {
		t.Fatalf("expected 5 rows with showUnknown=true, got %d", len(allFiltered))
	}
}

func TestSplitRSCFilesetPhysicalPath(t *testing.T) {
	include, host, targetPath := SplitFilesetPhysicalPath([]FilesetPath{{Name: "ALL_FILES_NFS"}, {Name: "/fme_app"}, {Name: "ncl1-3-vs-22.eu.socionext.com"}}, nil)
	if include != "ALL_FILES_NFS" || host != "ncl1-3-vs-22.eu.socionext.com" || targetPath != "/fme_app" {
		t.Fatalf("split fileset path = %q/%q/%q", include, host, targetPath)
	}
}

func TestSortRSCFilesetsByTarget(t *testing.T) {
	rows := []SLAAssignment{
		{TargetHost: "host-b", Path: "/z"},
		{TargetHost: "host-a", Path: "/z"},
		{TargetHost: "host-a", Path: "/a"},
		{Host: "host-c", Path: "/a"},
	}
	SortFilesetsByTarget(rows)
	got := []string{FilesetTargetHost(rows[0]) + rows[0].Path, FilesetTargetHost(rows[1]) + rows[1].Path, FilesetTargetHost(rows[2]) + rows[2].Path, FilesetTargetHost(rows[3]) + rows[3].Path}
	want := []string{"host-a/a", "host-a/z", "host-b/z", "host-c/a"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("sorted rows = %#v, want %#v", got, want)
		}
	}
}

func TestLooksLikeRSCSLAID(t *testing.T) {
	if !LooksLikeSLAID("D50661A3-B4AC-46C2-AA1A-2E102E9417F7", "d50661a3-b4ac-46c2-aa1a-2e102e9417f7") {
		t.Fatal("expected matching SLA ID to be recognized")
	}
	if LooksLikeSLAID("NFS_DIRECT_ARCHIVE_4d_1y", "d50661a3-b4ac-46c2-aa1a-2e102e9417f7") {
		t.Fatal("SLA name must not be recognized as an ID")
	}
}
