package rsc

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func rscObservedConfigFixture() ConfigDump {
	return ConfigDump{
		SchemaVersion: "1.0", GeneratedAt: "2026-11-02T10:00:00Z",
		Clusters: []ClusterConfig{{
			ID: "cluster-1", Name: "rubrik", Version: "9.0",
			Status: "OK", Connectivity: "CONNECTED",
			Metric: &ClusterMetricConfig{TotalCapacity: 100, UsedCapacity: 40, AvailableCapacity: 60, LastUpdateTime: "2026-11-02T10:00:00Z"},
			Nodes:  []ClusterNodeConfig{{ID: "node-1", Hostname: "node", Status: "OK", HardwareHealth: "HEALTHY"}},
			Disks:  []ClusterDiskConfig{{ID: "disk-1", NodeID: "node-1", Path: "/disk", Status: "OK", RaidStatus: "OK", DiskType: "SSD"}},
		}},
		SLAPolicies:    []SLAConfig{{ID: "sla", Name: "Gold", Schedule: &SLAScheduleConfig{Daily: "every 1: keep 30 DAYS"}}},
		TargetMappings: []TargetMappingConfig{{ID: "archive", Name: "archive", Targets: []string{"s3"}, ConsumedBytes: 15}},
	}
}

func TestRSCConfigYAMLIgnoresObservations(t *testing.T) {
	first := rscObservedConfigFixture()
	original := rscObservedConfigFixture()
	before, err := SerializeConfigYAML(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, original) {
		t.Fatal("serialization mutated observed source data")
	}
	second := rscObservedConfigFixture()
	second.GeneratedAt = "2026-11-03T11:00:00Z"
	cluster := &second.Clusters[0]
	cluster.Status, cluster.SubStatus, cluster.Connectivity = "WARNING", "DEGRADED", "DISCONNECTED"
	cluster.Metric = &ClusterMetricConfig{TotalCapacity: 100, UsedCapacity: 80, AvailableCapacity: 20, LastUpdateTime: second.GeneratedAt}
	cluster.Nodes[0].Status, cluster.Nodes[0].SubStatus, cluster.Nodes[0].HardwareHealth = "BAD", "BAD", "BAD"
	cluster.Disks[0].Status, cluster.Disks[0].RaidStatus = "BAD", "REBUILDING"
	second.TargetMappings[0].ConsumedBytes = 30
	after, err := SerializeConfigYAML(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("runtime-only changes changed YAML:\n%s\n%s", before, after)
	}
	for _, field := range []string{"generated_at:", "metric:", "used_capacity:", "available_capacity:", "last_update_time:", "status:", "hardware_health:", "connectivity:", "consumed_bytes:"} {
		if strings.Contains(string(after), field) {
			t.Fatalf("volatile field %q remains in YAML", field)
		}
	}
	second.SLAPolicies[0].Schedule.Daily = "every 1: keep 60 DAYS"
	changed, err := SerializeConfigYAML(second)
	if err != nil || bytes.Equal(after, changed) {
		t.Fatal("SLA configuration change was lost", err)
	}
}
