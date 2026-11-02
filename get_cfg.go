package rsc

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// rscSLAConfig represents the serialized declarative SLA policy configuration.
type SLAConfig struct {
	ID                string                  `yaml:"id"`
	Name              string                  `yaml:"name"`
	Kind              string                  `yaml:"kind"`
	Cluster           string                  `yaml:"cluster,omitempty"`
	Description       string                  `yaml:"description,omitempty"`
	IsDefault         bool                    `yaml:"is_default,omitempty"`
	RetentionLocked   bool                    `yaml:"retention_locked,omitempty"`
	RetentionLockMode string                  `yaml:"retention_lock_mode,omitempty"`
	ObjectTypes       []string                `yaml:"object_types,omitempty"`
	Schedule          *SLAScheduleConfig      `yaml:"schedule,omitempty"`
	ArchivalSpecs     []SLAArchivalSpecConfig `yaml:"archival_specs,omitempty"`
	ReplicationSpecs  []SLAReplicationConfig  `yaml:"replication_specs,omitempty"`
	Assignments       []SLAAssignmentConfig   `yaml:"assignments,omitempty"`
}

// rscSLAScheduleConfig represents scheduled frequencies and retentions.
type SLAScheduleConfig struct {
	Minute    string `yaml:"minute,omitempty"`
	Hourly    string `yaml:"hourly,omitempty"`
	Daily     string `yaml:"daily,omitempty"`
	Weekly    string `yaml:"weekly,omitempty"`
	Monthly   string `yaml:"monthly,omitempty"`
	Quarterly string `yaml:"quarterly,omitempty"`
	Yearly    string `yaml:"yearly,omitempty"`
}

// rscSLAArchivalSpecConfig represents an archival policy target and tiering spec.
type SLAArchivalSpecConfig struct {
	LocationName     string   `yaml:"location_name,omitempty"`
	Threshold        int      `yaml:"threshold,omitempty"`
	ThresholdUnit    string   `yaml:"threshold_unit,omitempty"`
	Frequencies      []string `yaml:"frequencies,omitempty"`
	StorageName      string   `yaml:"storage_name,omitempty"`
	StorageType      string   `yaml:"storage_type,omitempty"`
	ColdStorageClass string   `yaml:"cold_storage_class,omitempty"`
	InstantTiering   bool     `yaml:"instant_tiering,omitempty"`
}

// rscSLAReplicationConfig represents replication targets and duration.
type SLAReplicationConfig struct {
	ClusterName   string `yaml:"cluster_name,omitempty"`
	Retention     string `yaml:"retention,omitempty"`
	TargetMapping string `yaml:"target_mapping,omitempty"`
	AWSAccount    string `yaml:"aws_account,omitempty"`
	AWSRegion     string `yaml:"aws_region,omitempty"`
	AzureRegion   string `yaml:"azure_region,omitempty"`
}

// rscSLAAssignmentConfig represents a protected object or fileset assignment.
type SLAAssignmentConfig struct {
	Kind              string `yaml:"kind"`
	Template          string `yaml:"template,omitempty"`
	Instance          string `yaml:"instance,omitempty"`
	Include           string `yaml:"include,omitempty"`
	TargetHost        string `yaml:"target_host,omitempty"`
	Path              string `yaml:"path,omitempty"`
	Host              string `yaml:"host,omitempty"`
	ShareType         string `yaml:"share_type,omitempty"`
	Cluster           string `yaml:"cluster,omitempty"`
	ConfiguredSLAName string `yaml:"configured_sla,omitempty"`
	EffectiveSLAName  string `yaml:"effective_sla,omitempty"`
	SVM               string `yaml:"svm,omitempty"`
	Volume            string `yaml:"volume,omitempty"`
}

// rscFilesetTemplateConfig represents a fileset template and its protected instances.
type FilesetTemplateConfig struct {
	Name          string                  `yaml:"name"`
	ShareType     string                  `yaml:"share_type,omitempty"`
	OSType        string                  `yaml:"os_type,omitempty"`
	Cluster       string                  `yaml:"cluster,omitempty"`
	Includes      []string                `yaml:"includes,omitempty"`
	ConfiguredSLA string                  `yaml:"configured_sla,omitempty"`
	EffectiveSLA  string                  `yaml:"effective_sla,omitempty"`
	Instances     []FilesetInstanceConfig `yaml:"instances,omitempty"`
}

// rscFilesetInstanceConfig represents a specific host / share fileset instance.
type FilesetInstanceConfig struct {
	Name          string   `yaml:"name"`
	Cluster       string   `yaml:"cluster,omitempty"`
	PhysicalPaths []string `yaml:"physical_paths,omitempty"`
	ConfiguredSLA string   `yaml:"configured_sla,omitempty"`
	EffectiveSLA  string   `yaml:"effective_sla,omitempty"`
}

// rscNASShareConfig represents an export/share registered in RSC.
type NASShareConfig struct {
	Name           string `yaml:"name"`
	ID             string `yaml:"id,omitempty"`
	ExportPoint    string `yaml:"export_point,omitempty"`
	HostAddress    string `yaml:"host_address,omitempty"`
	ShareType      string `yaml:"share_type,omitempty"`
	NASSystem      string `yaml:"nas_system,omitempty"`
	PrimaryFileset string `yaml:"primary_fileset,omitempty"`
	Cluster        string `yaml:"cluster,omitempty"`
	ConfiguredSLA  string `yaml:"configured_sla,omitempty"`
	EffectiveSLA   string `yaml:"effective_sla,omitempty"`
}

// rscNASSystemConfig represents a storage system array or namespace registered in RSC.
type NASSystemConfig struct {
	Name           string `yaml:"name"`
	ID             string `yaml:"id,omitempty"`
	VendorType     string `yaml:"vendor_type,omitempty"`
	ShareCount     int    `yaml:"share_count,omitempty"`
	NamespaceCount int    `yaml:"namespace_count,omitempty"`
	Cluster        string `yaml:"cluster,omitempty"`
	EffectiveSLA   string `yaml:"effective_sla,omitempty"`
}

// rscNASNamespaceConfig represents a NAS namespace.
type NASNamespaceConfig struct {
	Name         string `yaml:"name"`
	ID           string `yaml:"id,omitempty"`
	VendorType   string `yaml:"vendor_type,omitempty"`
	Cluster      string `yaml:"cluster,omitempty"`
	EffectiveSLA string `yaml:"effective_sla,omitempty"`
}

// rscClusterNodeConfig represents an individual cluster node.
type ClusterNodeConfig struct {
	ID             string `yaml:"id,omitempty"`
	Hostname       string `yaml:"hostname,omitempty"`
	IPAddress      string `yaml:"ip_address,omitempty"`
	Status         string `yaml:"status,omitempty"`
	SubStatus      string `yaml:"sub_status,omitempty"`
	Classification string `yaml:"classification,omitempty"`
	Role           string `yaml:"role,omitempty"`
	HardwareHealth string `yaml:"hardware_health,omitempty"`
}

// rscClusterDiskConfig represents an individual disk.
type ClusterDiskConfig struct {
	ID         string `yaml:"id,omitempty"`
	NodeID     string `yaml:"node_id,omitempty"`
	Path       string `yaml:"path,omitempty"`
	Status     string `yaml:"status,omitempty"`
	RaidType   string `yaml:"raid_type,omitempty"`
	RaidStatus string `yaml:"raid_status,omitempty"`
	DiskMode   string `yaml:"disk_mode,omitempty"`
	DiskType   string `yaml:"disk_type,omitempty"`
}

// rscClusterMetricConfig represents storage capacity and data reduction metrics.
type ClusterMetricConfig struct {
	TotalCapacity           int64  `yaml:"total_capacity,omitempty"`
	UsedCapacity            int64  `yaml:"used_capacity,omitempty"`
	AvailableCapacity       int64  `yaml:"available_capacity,omitempty"`
	SnapshotCapacity        int64  `yaml:"snapshot_capacity,omitempty"`
	LiveMountCapacity       int64  `yaml:"live_mount_capacity,omitempty"`
	MiscellaneousCapacity   int64  `yaml:"miscellaneous_capacity,omitempty"`
	PendingSnapshotCapacity int64  `yaml:"pending_snapshot_capacity,omitempty"`
	CDPCapacity             int64  `yaml:"cdp_capacity,omitempty"`
	IngestedSnapshotStorage int64  `yaml:"ingested_snapshot_storage,omitempty"`
	PhysicalSnapshotStorage int64  `yaml:"physical_snapshot_storage,omitempty"`
	IngestedArchivalStorage int64  `yaml:"ingested_archival_storage,omitempty"`
	PhysicalArchivalStorage int64  `yaml:"physical_archival_storage,omitempty"`
	AverageDailyGrowth      int64  `yaml:"average_daily_growth,omitempty"`
	ImmutabilityOverhead    int64  `yaml:"immutability_overhead,omitempty"`
	LastUpdateTime          string `yaml:"last_update_time,omitempty"`
}

// rscClusterConfig represents a connected Rubrik cluster.
type ClusterConfig struct {
	ID           string               `yaml:"id"`
	Name         string               `yaml:"name"`
	Version      string               `yaml:"version,omitempty"`
	APIVersion   string               `yaml:"api_version,omitempty"`
	Status       string               `yaml:"status,omitempty"`
	SubStatus    string               `yaml:"sub_status,omitempty"`
	Connectivity string               `yaml:"connectivity,omitempty"`
	Metric       *ClusterMetricConfig `yaml:"metric,omitempty"`
	Nodes        []ClusterNodeConfig  `yaml:"nodes,omitempty"`
	Disks        []ClusterDiskConfig  `yaml:"disks,omitempty"`
}

// rscTargetMappingConfig represents an archival/replication storage target configuration.
type TargetMappingConfig struct {
	ID            string   `yaml:"id,omitempty"`
	Name          string   `yaml:"name"`
	TargetType    string   `yaml:"target_type,omitempty"`
	Targets       []string `yaml:"targets,omitempty"`
	ConsumedBytes int64    `yaml:"consumed_bytes,omitempty"`
}

// rscConfigDump is the root serialized YAML document containing full Rubrik configuration.
type ConfigDump struct {
	SchemaVersion  string                  `yaml:"schema_version"`
	GeneratedAt    string                  `yaml:"generated_at,omitempty"`
	Clusters       []ClusterConfig         `yaml:"clusters"`
	SLAPolicies    []SLAConfig             `yaml:"sla_policies"`
	Filesets       []FilesetTemplateConfig `yaml:"filesets,omitempty"`
	NASShares      []NASShareConfig        `yaml:"nas_shares,omitempty"`
	NASSystems     []NASSystemConfig       `yaml:"nas_systems,omitempty"`
	NASNamespaces  []NASNamespaceConfig    `yaml:"nas_namespaces,omitempty"`
	TargetMappings []TargetMappingConfig   `yaml:"target_mappings,omitempty"`
}

// filterRSCConfigDumpByCluster filters dump elements to those belonging to clusterName or global.
func FilterConfigDumpByCluster(dump ConfigDump, clusterName string) ConfigDump {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		return dump
	}

	filtered := ConfigDump{
		SchemaVersion: dump.SchemaVersion,
		GeneratedAt:   dump.GeneratedAt,
	}

	for _, c := range dump.Clusters {
		if strings.EqualFold(c.Name, clusterName) || (c.ID != "" && strings.EqualFold(c.ID, clusterName)) {
			filtered.Clusters = append(filtered.Clusters, c)
		}
	}

	for _, p := range dump.SLAPolicies {
		if p.Cluster == "" || strings.EqualFold(p.Cluster, clusterName) || strings.EqualFold(p.Kind, "global") {
			policyCopy := p
			var assignments []SLAAssignmentConfig
			for _, a := range p.Assignments {
				if a.Cluster == "" || strings.EqualFold(a.Cluster, clusterName) {
					assignments = append(assignments, a)
				}
			}
			policyCopy.Assignments = assignments
			filtered.SLAPolicies = append(filtered.SLAPolicies, policyCopy)
		}
	}

	for _, f := range dump.Filesets {
		if f.Cluster == "" || strings.EqualFold(f.Cluster, clusterName) {
			filesetCopy := f
			var instances []FilesetInstanceConfig
			for _, inst := range f.Instances {
				if inst.Cluster == "" || strings.EqualFold(inst.Cluster, clusterName) {
					instances = append(instances, inst)
				}
			}
			filesetCopy.Instances = instances
			filtered.Filesets = append(filtered.Filesets, filesetCopy)
		}
	}

	for _, s := range dump.NASShares {
		if s.Cluster == "" || strings.EqualFold(s.Cluster, clusterName) {
			filtered.NASShares = append(filtered.NASShares, s)
		}
	}

	for _, s := range dump.NASSystems {
		if s.Cluster == "" || strings.EqualFold(s.Cluster, clusterName) {
			filtered.NASSystems = append(filtered.NASSystems, s)
		}
	}

	for _, n := range dump.NASNamespaces {
		if n.Cluster == "" || strings.EqualFold(n.Cluster, clusterName) {
			filtered.NASNamespaces = append(filtered.NASNamespaces, n)
		}
	}

	for _, tm := range dump.TargetMappings {
		filtered.TargetMappings = append(filtered.TargetMappings, tm)
	}

	return filtered
}

func BuildClusterConfigs(infos []ClusterInfo, healths []HealthInfo, dfs []DFInfo) []ClusterConfig {
	healthMap := make(map[string]HealthInfo)
	for _, h := range healths {
		healthMap[strings.ToLower(h.ClusterName)] = h
		if h.ClusterID != "" {
			healthMap[strings.ToLower(h.ClusterID)] = h
		}
	}
	dfMap := make(map[string]DFInfo)
	for _, df := range dfs {
		dfMap[strings.ToLower(df.ClusterName)] = df
		if df.ClusterID != "" {
			dfMap[strings.ToLower(df.ClusterID)] = df
		}
	}

	var configs []ClusterConfig
	for _, info := range infos {
		cfg := ClusterConfig{
			ID:         info.ClusterID,
			Name:       info.ClusterName,
			Version:    info.SoftwareVersion,
			APIVersion: info.APIVersion,
		}
		h, ok := healthMap[strings.ToLower(info.ClusterName)]
		if !ok && info.ClusterID != "" {
			h, ok = healthMap[strings.ToLower(info.ClusterID)]
		}
		if ok {
			cfg.Status = h.OverallStatus
			cfg.Connectivity = h.Connectivity
			for _, n := range h.Nodes {
				cfg.Nodes = append(cfg.Nodes, ClusterNodeConfig{
					ID:             n.ID,
					Hostname:       n.Hostname,
					IPAddress:      n.IPAddress,
					Status:         n.Status,
					SubStatus:      n.SubStatus,
					Classification: n.Classification,
					Role:           n.Role,
					HardwareHealth: n.HardwareHealth,
				})
			}
			sort.Slice(cfg.Nodes, func(i, j int) bool {
				return strings.ToLower(cfg.Nodes[i].Hostname) < strings.ToLower(cfg.Nodes[j].Hostname)
			})
			for _, d := range h.Disks {
				cfg.Disks = append(cfg.Disks, ClusterDiskConfig{
					ID:         d.ID,
					NodeID:     d.NodeID,
					Path:       d.Path,
					Status:     d.Status,
					RaidType:   d.RaidType,
					RaidStatus: d.RaidStatus,
					DiskMode:   d.DiskMode,
					DiskType:   d.DiskType,
				})
			}
			sort.Slice(cfg.Disks, func(i, j int) bool {
				if cfg.Disks[i].NodeID != cfg.Disks[j].NodeID {
					return cfg.Disks[i].NodeID < cfg.Disks[j].NodeID
				}
				return cfg.Disks[i].Path < cfg.Disks[j].Path
			})
		}
		df, ok := dfMap[strings.ToLower(info.ClusterName)]
		if !ok && info.ClusterID != "" {
			df, ok = dfMap[strings.ToLower(info.ClusterID)]
		}
		if ok {
			cfg.Metric = &ClusterMetricConfig{
				TotalCapacity:           DerefInt64(df.Metric.TotalCapacity),
				UsedCapacity:            DerefInt64(df.Metric.UsedCapacity),
				AvailableCapacity:       DerefInt64(df.Metric.AvailableCapacity),
				SnapshotCapacity:        DerefInt64(df.Metric.SnapshotCapacity),
				LiveMountCapacity:       DerefInt64(df.Metric.LiveMountCapacity),
				MiscellaneousCapacity:   DerefInt64(df.Metric.MiscellaneousCapacity),
				PendingSnapshotCapacity: DerefInt64(df.Metric.PendingSnapshotCapacity),
				CDPCapacity:             DerefInt64(df.Metric.CDPCapacity),
				IngestedSnapshotStorage: DerefInt64(df.Metric.IngestedSnapshotStorage),
				PhysicalSnapshotStorage: DerefInt64(df.Metric.PhysicalSnapshotStorage),
				IngestedArchivalStorage: DerefInt64(df.Metric.IngestedArchivalStorage),
				PhysicalArchivalStorage: DerefInt64(df.Metric.PhysicalArchivalStorage),
				AverageDailyGrowth:      DerefInt64(df.Metric.AverageDailyGrowth),
				ImmutabilityOverhead:    DerefInt64(df.Metric.ImmutabilityOverhead),
				LastUpdateTime:          df.Metric.LastUpdateTime,
			}
		}
		configs = append(configs, cfg)
	}

	sort.Slice(configs, func(i, j int) bool {
		return strings.ToLower(configs[i].Name) < strings.ToLower(configs[j].Name)
	})
	return configs
}

func ConvertSLADetailToConfig(detail SLADetail, assignments []SLAAssignment) SLAConfig {
	kind := "global"
	if strings.TrimSpace(detail.CDMID) != "" {
		kind = "cluster"
	}
	cfg := SLAConfig{
		ID:                detail.ID,
		Name:              detail.Name,
		Kind:              kind,
		Cluster:           detail.Cluster.Name,
		Description:       detail.Description,
		IsDefault:         detail.IsDefault,
		RetentionLocked:   detail.IsRetentionLocked,
		RetentionLockMode: detail.RetentionLockMode,
		ObjectTypes:       detail.ObjectTypes,
	}
	sort.Strings(cfg.ObjectTypes)

	sched := ConvertSLASnapshotSchedule(detail.SnapshotSchedule)
	if sched != nil {
		cfg.Schedule = sched
	}

	specs := detail.ArchivalSpecs
	if len(specs) == 0 && (detail.ArchivalSpec.LocationName != "" || detail.ArchivalSpec.Threshold != 0) {
		specs = []SLADetailArchivalSpec{detail.ArchivalSpec}
	}
	for _, spec := range specs {
		if spec.LocationName == "" && spec.Threshold == 0 && spec.Storage.Name == "" {
			continue
		}
		freqs := append([]string{}, spec.Frequencies...)
		sort.Strings(freqs)
		cfg.ArchivalSpecs = append(cfg.ArchivalSpecs, SLAArchivalSpecConfig{
			LocationName:     spec.LocationName,
			Threshold:        spec.Threshold,
			ThresholdUnit:    spec.ThresholdUnit,
			Frequencies:      freqs,
			StorageName:      spec.Storage.Name,
			StorageType:      spec.Storage.TargetType,
			ColdStorageClass: spec.Tier.ColdStorageClass,
			InstantTiering:   spec.Tier.InstantTiering,
		})
	}
	sort.Slice(cfg.ArchivalSpecs, func(i, j int) bool {
		return cfg.ArchivalSpecs[i].LocationName < cfg.ArchivalSpecs[j].LocationName
	})

	for _, rep := range detail.ReplicationSpecs {
		cfg.ReplicationSpecs = append(cfg.ReplicationSpecs, SLAReplicationConfig{
			ClusterName:   rep.Cluster.Name,
			Retention:     FormatDuration(rep.Retention.Duration, rep.Retention.Unit),
			TargetMapping: rep.TargetMapping.Name,
			AWSAccount:    rep.AWSTarget.AccountName,
			AWSRegion:     rep.AWSTarget.Region,
			AzureRegion:   rep.AzureTarget.Region,
		})
	}
	sort.Slice(cfg.ReplicationSpecs, func(i, j int) bool {
		return cfg.ReplicationSpecs[i].ClusterName < cfg.ReplicationSpecs[j].ClusterName
	})

	for _, a := range assignments {
		cfg.Assignments = append(cfg.Assignments, SLAAssignmentConfig{
			Kind:              a.Kind,
			Template:          a.Template,
			Instance:          a.Instance,
			Include:           a.Include,
			TargetHost:        a.TargetHost,
			Path:              a.Path,
			Host:              a.Host,
			ShareType:         a.ShareType,
			Cluster:           a.Cluster,
			ConfiguredSLAName: a.ConfiguredSLAName,
			EffectiveSLAName:  a.EffectiveSLAName,
			SVM:               a.SVM,
			Volume:            a.Volume,
		})
	}
	sort.Slice(cfg.Assignments, func(i, j int) bool {
		keyI := cfg.Assignments[i].TargetHost + "|" + cfg.Assignments[i].Path + "|" + cfg.Assignments[i].Instance
		keyJ := cfg.Assignments[j].TargetHost + "|" + cfg.Assignments[j].Path + "|" + cfg.Assignments[j].Instance
		return strings.ToLower(keyI) < strings.ToLower(keyJ)
	})

	return cfg
}

func ConvertSLASnapshotSchedule(sched SLASnapshotSchedule) *SLAScheduleConfig {
	cfg := &SLAScheduleConfig{}
	hasAny := false

	checkPeriod := func(p SLASchedulePeriod) string {
		if p.BasicSchedule.Frequency != 0 || p.BasicSchedule.Retention != 0 {
			hasAny = true
			return fmt.Sprintf("every %d: keep %d %s", p.BasicSchedule.Frequency, p.BasicSchedule.Retention, p.BasicSchedule.RetentionUnit)
		}
		return ""
	}

	cfg.Minute = checkPeriod(sched.Minute)
	cfg.Hourly = checkPeriod(sched.Hourly)
	cfg.Daily = checkPeriod(sched.Daily)
	cfg.Weekly = checkPeriod(sched.Weekly)
	cfg.Monthly = checkPeriod(sched.Monthly)
	cfg.Quarterly = checkPeriod(sched.Quarterly)
	cfg.Yearly = checkPeriod(sched.Yearly)

	if !hasAny {
		return nil
	}
	return cfg
}

func FetchFilesetTemplatesDetailed(ctx context.Context, client *http.Client, endpoint string) ([]FilesetTemplateConfig, error) {
	const queryTemplate = `query OpsctlFilesetTemplatesDetailed { filesetTemplates(hostRoot: %s, first: 100, after: %s) { nodes { name includes shareType osType cluster { name } configuredSlaDomain { name } effectiveSlaDomain { name } descendantConnection(first: 100) { nodes { name physicalPath { name } configuredSlaDomain { name } effectiveSlaDomain { name } cluster { name } } pageInfo { hasNextPage } } } pageInfo { endCursor hasNextPage } } }`
	var templates []FilesetTemplateConfig

	for _, root := range []string{"WINDOWS_HOST_ROOT", "LINUX_HOST_ROOT", "NAS_HOST_ROOT"} {
		var after *string
		for {
			afterValue := "null"
			if after != nil {
				afterValue = fmt.Sprintf("%q", *after)
			}
			query := fmt.Sprintf(queryTemplate, root, afterValue)
			var response struct {
				Data struct {
					Filesets struct {
						Nodes []struct {
							Name          string                `json:"name"`
							Includes      []string              `json:"includes"`
							ShareType     string                `json:"shareType"`
							OSType        string                `json:"osType"`
							Cluster       struct{ Name string } `json:"cluster"`
							ConfiguredSLA struct{ Name string } `json:"configuredSlaDomain"`
							EffectiveSLA  struct{ Name string } `json:"effectiveSlaDomain"`
							Descendants   struct {
								Nodes []struct {
									Name          string                `json:"name"`
									PhysicalPath  []FilesetPath         `json:"physicalPath"`
									ConfiguredSLA struct{ Name string } `json:"configuredSlaDomain"`
									EffectiveSLA  struct{ Name string } `json:"effectiveSlaDomain"`
									Cluster       struct{ Name string } `json:"cluster"`
								} `json:"nodes"`
							} `json:"descendantConnection"`
						} `json:"nodes"`
						PageInfo struct {
							EndCursor   *string `json:"endCursor"`
							HasNextPage bool    `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"filesetTemplates"`
				} `json:"data"`
			}
			if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
				if IsNotFoundError(err) {
					break
				}
				return nil, err
			}
			for _, node := range response.Data.Filesets.Nodes {
				includes := append([]string{}, node.Includes...)
				sort.Strings(includes)
				t := FilesetTemplateConfig{
					Name:          node.Name,
					ShareType:     node.ShareType,
					OSType:        node.OSType,
					Cluster:       node.Cluster.Name,
					Includes:      includes,
					ConfiguredSLA: node.ConfiguredSLA.Name,
					EffectiveSLA:  node.EffectiveSLA.Name,
				}
				for _, d := range node.Descendants.Nodes {
					paths := make([]string, 0, len(d.PhysicalPath))
					for _, p := range d.PhysicalPath {
						if p.Name != "" {
							paths = append(paths, p.Name)
						}
					}
					sort.Strings(paths)
					t.Instances = append(t.Instances, FilesetInstanceConfig{
						Name:          d.Name,
						Cluster:       d.Cluster.Name,
						PhysicalPaths: paths,
						ConfiguredSLA: d.ConfiguredSLA.Name,
						EffectiveSLA:  d.EffectiveSLA.Name,
					})
				}
				sort.Slice(t.Instances, func(i, j int) bool {
					return strings.ToLower(t.Instances[i].Name) < strings.ToLower(t.Instances[j].Name)
				})
				templates = append(templates, t)
			}
			if !response.Data.Filesets.PageInfo.HasNextPage || response.Data.Filesets.PageInfo.EndCursor == nil {
				break
			}
			after = response.Data.Filesets.PageInfo.EndCursor
		}
	}

	sort.Slice(templates, func(i, j int) bool {
		return strings.ToLower(templates[i].Name) < strings.ToLower(templates[j].Name)
	})
	return templates, nil
}

func FetchNASSharesDetailed(ctx context.Context, client *http.Client, endpoint string) ([]NASShareConfig, error) {
	const queryTemplate = `query OpsctlNASSharesDetailed { nasShares(first: 100, after: %s) { nodes { id name exportPoint hostAddress shareType nasSystem { name } primaryFileset { name } cluster { name } configuredSlaDomain { name } effectiveSlaDomain { name } } pageInfo { endCursor hasNextPage } } }`
	var shares []NASShareConfig
	var after *string

	for {
		afterValue := "null"
		if after != nil {
			afterValue = fmt.Sprintf("%q", *after)
		}
		query := fmt.Sprintf(queryTemplate, afterValue)
		var response struct {
			Data struct {
				NASShares struct {
					Nodes []struct {
						ID             string                `json:"id"`
						Name           string                `json:"name"`
						ExportPoint    string                `json:"exportPoint"`
						HostAddress    string                `json:"hostAddress"`
						ShareType      string                `json:"shareType"`
						NASSystem      struct{ Name string } `json:"nasSystem"`
						PrimaryFileset struct{ Name string } `json:"primaryFileset"`
						Cluster        struct{ Name string } `json:"cluster"`
						ConfiguredSLA  struct{ Name string } `json:"configuredSlaDomain"`
						EffectiveSLA   struct{ Name string } `json:"effectiveSlaDomain"`
					} `json:"nodes"`
					PageInfo struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage bool    `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"nasShares"`
			} `json:"data"`
		}
		if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
			if IsNotFoundError(err) {
				break
			}
			return nil, err
		}
		for _, s := range response.Data.NASShares.Nodes {
			shares = append(shares, NASShareConfig{
				Name:           s.Name,
				ID:             s.ID,
				ExportPoint:    s.ExportPoint,
				HostAddress:    s.HostAddress,
				ShareType:      s.ShareType,
				NASSystem:      s.NASSystem.Name,
				PrimaryFileset: s.PrimaryFileset.Name,
				Cluster:        s.Cluster.Name,
				ConfiguredSLA:  s.ConfiguredSLA.Name,
				EffectiveSLA:   s.EffectiveSLA.Name,
			})
		}
		if !response.Data.NASShares.PageInfo.HasNextPage || response.Data.NASShares.PageInfo.EndCursor == nil {
			break
		}
		after = response.Data.NASShares.PageInfo.EndCursor
	}

	sort.Slice(shares, func(i, j int) bool {
		keyI := shares[i].HostAddress + "|" + shares[i].ExportPoint + "|" + shares[i].Name
		keyJ := shares[j].HostAddress + "|" + shares[j].ExportPoint + "|" + shares[j].Name
		return strings.ToLower(keyI) < strings.ToLower(keyJ)
	})
	return shares, nil
}

func FetchNASObjectsDetailed(ctx context.Context, client *http.Client, endpoint string) ([]NASSystemConfig, []NASNamespaceConfig, error) {
	const query = `query OpsctlNASObjectsDetailed { nasSystems(first: 100) { nodes { name id vendorType shareCount namespaceCount cluster { name } effectiveSlaDomain { name } } } nasNamespaces(first: 100) { nodes { name id vendorType cluster { name } effectiveSlaDomain { name } } } }`
	var response struct {
		Data struct {
			NASSystems struct {
				Nodes []struct {
					Name           string                `json:"name"`
					ID             string                `json:"id"`
					VendorType     string                `json:"vendorType"`
					ShareCount     int                   `json:"shareCount"`
					NamespaceCount int                   `json:"namespaceCount"`
					Cluster        struct{ Name string } `json:"cluster"`
					EffectiveSLA   struct{ Name string } `json:"effectiveSlaDomain"`
				} `json:"nodes"`
			} `json:"nasSystems"`
			NASNamespaces struct {
				Nodes []struct {
					Name         string                `json:"name"`
					ID           string                `json:"id"`
					VendorType   string                `json:"vendorType"`
					Cluster      struct{ Name string } `json:"cluster"`
					EffectiveSLA struct{ Name string } `json:"effectiveSlaDomain"`
				} `json:"nodes"`
			} `json:"nasNamespaces"`
		} `json:"data"`
	}
	if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
		if IsNotFoundError(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var systems []NASSystemConfig
	for _, sys := range response.Data.NASSystems.Nodes {
		systems = append(systems, NASSystemConfig{
			Name:           sys.Name,
			ID:             sys.ID,
			VendorType:     sys.VendorType,
			ShareCount:     sys.ShareCount,
			NamespaceCount: sys.NamespaceCount,
			Cluster:        sys.Cluster.Name,
			EffectiveSLA:   sys.EffectiveSLA.Name,
		})
	}
	sort.Slice(systems, func(i, j int) bool {
		return strings.ToLower(systems[i].Name) < strings.ToLower(systems[j].Name)
	})

	var namespaces []NASNamespaceConfig
	for _, ns := range response.Data.NASNamespaces.Nodes {
		namespaces = append(namespaces, NASNamespaceConfig{
			Name:         ns.Name,
			ID:           ns.ID,
			VendorType:   ns.VendorType,
			Cluster:      ns.Cluster.Name,
			EffectiveSLA: ns.EffectiveSLA.Name,
		})
	}
	sort.Slice(namespaces, func(i, j int) bool {
		return strings.ToLower(namespaces[i].Name) < strings.ToLower(namespaces[j].Name)
	})

	return systems, namespaces, nil
}

// serializeRSCConfigYAML serializes RSCConfigDump into git-diff friendly YAML format.
func SerializeConfigYAML(dump ConfigDump) ([]byte, error) {
	dump = StableConfig(dump)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(dump); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
