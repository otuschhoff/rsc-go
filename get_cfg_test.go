package rsc

import (
	"testing"
)

func TestFilterRSCConfigDumpByCluster(t *testing.T) {
	dump := ConfigDump{
		SchemaVersion: "1.0",
		Clusters: []ClusterConfig{
			{Name: "Rubrikcluster01"},
			{Name: "Rubrikcluster02"},
		},
		SLAPolicies: []SLAConfig{
			{
				Name:    "Policy-Global",
				Kind:    "global",
				Cluster: "",
				Assignments: []SLAAssignmentConfig{
					{Cluster: "Rubrikcluster01", Path: "/data1"},
					{Cluster: "Rubrikcluster02", Path: "/data2"},
				},
			},
			{
				Name:    "Policy-Cluster2",
				Kind:    "cluster",
				Cluster: "Rubrikcluster02",
			},
		},
		Filesets: []FilesetTemplateConfig{
			{
				Name:    "Fileset-C1",
				Cluster: "Rubrikcluster01",
			},
			{
				Name:    "Fileset-C2",
				Cluster: "Rubrikcluster02",
			},
		},
		NASShares: []NASShareConfig{
			{Name: "share1", Cluster: "Rubrikcluster01"},
			{Name: "share2", Cluster: "Rubrikcluster02"},
		},
	}

	filtered := FilterConfigDumpByCluster(dump, "Rubrikcluster01")

	if len(filtered.Clusters) != 1 || filtered.Clusters[0].Name != "Rubrikcluster01" {
		t.Fatalf("filtered clusters = %+v, want 1 cluster Rubrikcluster01", filtered.Clusters)
	}
	if len(filtered.SLAPolicies) != 1 || filtered.SLAPolicies[0].Name != "Policy-Global" {
		t.Fatalf("filtered SLA policies = %+v, want only Policy-Global", filtered.SLAPolicies)
	}
	if len(filtered.SLAPolicies[0].Assignments) != 1 || filtered.SLAPolicies[0].Assignments[0].Path != "/data1" {
		t.Fatalf("filtered assignments = %+v, want 1 assignment /data1", filtered.SLAPolicies[0].Assignments)
	}
	if len(filtered.Filesets) != 1 || filtered.Filesets[0].Name != "Fileset-C1" {
		t.Fatalf("filtered filesets = %+v, want Fileset-C1", filtered.Filesets)
	}
	if len(filtered.NASShares) != 1 || filtered.NASShares[0].Name != "share1" {
		t.Fatalf("filtered NAS shares = %+v, want share1", filtered.NASShares)
	}
}
