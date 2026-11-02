package rsc

func StableConfig(dump ConfigDump) ConfigDump {
	dump.GeneratedAt = ""
	dump.Clusters = append([]ClusterConfig(nil), dump.Clusters...)
	for index := range dump.Clusters {
		cluster := &dump.Clusters[index]
		cluster.Status, cluster.SubStatus, cluster.Connectivity, cluster.Metric = "", "", "", nil
		cluster.Nodes = append([]ClusterNodeConfig(nil), cluster.Nodes...)
		for nodeIndex := range cluster.Nodes {
			node := &cluster.Nodes[nodeIndex]
			node.Status, node.SubStatus, node.HardwareHealth = "", "", ""
		}
		cluster.Disks = append([]ClusterDiskConfig(nil), cluster.Disks...)
		for diskIndex := range cluster.Disks {
			disk := &cluster.Disks[diskIndex]
			disk.Status, disk.RaidStatus = "", ""
		}
	}
	dump.TargetMappings = append([]TargetMappingConfig(nil), dump.TargetMappings...)
	for index := range dump.TargetMappings {
		dump.TargetMappings[index].ConsumedBytes = 0
	}
	return dump
}
