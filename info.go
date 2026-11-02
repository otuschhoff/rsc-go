package rsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const DefaultGraphQLEndpoint = "https://graphql.rubrik.com/api/graphql"

type ClusterInfo struct {
	ClusterID       string
	ClusterName     string
	SoftwareVersion string
	APIVersion      string
}

type HealthNode struct {
	ID              string
	Hostname        string
	IPAddress       string
	Status          string
	SubStatus       string
	Classification  string
	Role            string
	HardwareHealth  string
	NeedsInspection bool
}

type HealthDisk struct {
	ID         string
	NodeID     string
	Status     string
	Path       string
	RaidType   string
	RaidStatus string
	DiskMode   string
	DiskType   string
	Capacity   string
}

type HealthInfo struct {
	ClusterID     string
	ClusterName   string
	OverallStatus string
	Connectivity  string
	Nodes         []HealthNode
	Disks         []HealthDisk
}

type DFMetric struct {
	TotalCapacity           *int64 `json:"totalCapacity"`
	AvailableCapacity       *int64 `json:"availableCapacity"`
	UsedCapacity            *int64 `json:"usedCapacity"`
	SnapshotCapacity        *int64 `json:"snapshotCapacity"`
	LiveMountCapacity       *int64 `json:"liveMountCapacity"`
	MiscellaneousCapacity   *int64 `json:"miscellaneousCapacity"`
	PendingSnapshotCapacity *int64 `json:"pendingSnapshotCapacity"`
	CDPCapacity             *int64 `json:"cdpCapacity"`
	IngestedSnapshotStorage *int64 `json:"ingestedSnapshotStorage"`
	PhysicalSnapshotStorage *int64 `json:"physicalSnapshotStorage"`
	IngestedArchivalStorage *int64 `json:"ingestedArchivalStorage"`
	PhysicalArchivalStorage *int64 `json:"physicalArchivalStorage"`
	ImmutabilityOverhead    *int64 `json:"immutabilityOverhead"`
	LastUpdateTime          string `json:"lastUpdateTime"`
	AverageDailyGrowth      *int64 `json:"averageDailyGrowth"`
}

type DFEntitlement struct {
	Capacity     *float64 `json:"capacity"`
	Bundle       string   `json:"bundle"`
	Tier         string   `json:"tier"`
	Redundancy   string   `json:"redundancy"`
	UsedCapacity *float64 `json:"usedCapacity"`
}

type DFInfo struct {
	ClusterID          string
	ClusterName        string
	Metric             DFMetric
	BackupEntitlement  DFEntitlement
	ArchiveEntitlement DFEntitlement
	Entitlements       []DFEntitlement
}

type SLAPolicy struct {
	ID            string
	Name          string
	Kind          string
	ObjectTypes   []string
	Schedule      string
	Retention     string
	RetentionDays int
	Archive       string
	Default       bool
	Cluster       string
}

type SLABasicSchedule struct {
	Frequency     int    `json:"frequency"`
	Retention     int    `json:"retention"`
	RetentionUnit string `json:"retentionUnit"`
}

type SLASchedulePeriod struct {
	BasicSchedule SLABasicSchedule `json:"basicSchedule"`
}

type SLASnapshotSchedule struct {
	Minute    SLASchedulePeriod `json:"minute"`
	Hourly    SLASchedulePeriod `json:"hourly"`
	Daily     SLASchedulePeriod `json:"daily"`
	Weekly    SLASchedulePeriod `json:"weekly"`
	Monthly   SLASchedulePeriod `json:"monthly"`
	Quarterly SLASchedulePeriod `json:"quarterly"`
	Yearly    SLASchedulePeriod `json:"yearly"`
}

type SLAStorageSetting struct {
	Name string `json:"name"`
}

type SLAArchivalSpec struct {
	Threshold      int               `json:"threshold"`
	ThresholdUnit  string            `json:"thresholdUnit"`
	StorageSetting SLAStorageSetting `json:"storageSetting"`
}

type SLAClusterArchivalSpec struct {
	Threshold            int    `json:"threshold"`
	ThresholdUnit        string `json:"thresholdUnit"`
	ArchivalLocationName string `json:"archivalLocationName"`
}

type GraphQLRequest struct {
	Query string `json:"query"`
}

type GraphQLError struct {
	Message string `json:"message"`
}

type GraphQLResponse struct {
	Data struct {
		ClusterConnection struct {
			Nodes []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"nodes"`
		} `json:"clusterConnection"`
	} `json:"data"`
	Errors     []GraphQLError `json:"errors"`
	Extensions map[string]any `json:"extensions"`
}

func NormalizeSourceTypes(sourceTypes []string) []string {
	seen := make(map[string]struct{}, len(sourceTypes))
	result := make([]string, 0, len(sourceTypes))
	for _, sourceType := range sourceTypes {
		normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(sourceType), "_OBJECT_TYPE"))
		if normalized != "" {
			if _, ok := seen[normalized]; !ok {
				seen[normalized] = struct{}{}
				result = append(result, normalized)
			}
		}
	}
	return result
}

func NormalizeGraphQLEndpoint(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return DefaultGraphQLEndpoint
	}
	if strings.Contains(base, "/api/graphql") {
		return base
	}

	if strings.Contains(base, "/api/client_token") {
		base = strings.Replace(base, "/api/client_token", "/api/graphql", 1)
		return base
	}
	if strings.Contains(base, "/api/client-token") {
		base = strings.Replace(base, "/api/client-token", "/api/graphql", 1)
		return base
	}
	if strings.Contains(base, "/api/") {
		base = strings.Replace(base, "/api/", "/api/", 1)
	}

	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return strings.TrimRight(base, "/") + "/api/graphql"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/graphql"
	return u.String()
}

func FetchClusterInfosForCluster(ctx context.Context, client *http.Client, endpoint string, clusterName string) ([]ClusterInfo, error) {
	query := fmt.Sprintf(`query OpsctlRSCInfo { clusterConnection(%sfirst: 100) { nodes { id name version } } }`, ClusterFilterArgument(clusterName))
	body, headers, err := request(ctx, client, endpoint, query, nil, nil, 1)
	if err != nil {
		return nil, err
	}

	var gqlResp GraphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse RSC GraphQL response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("RSC GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if len(gqlResp.Data.ClusterConnection.Nodes) == 0 {
		return nil, fmt.Errorf("RSC GraphQL response did not include any clusters")
	}

	apiVersion := ExtractAPIVersion(headers, gqlResp.Extensions)
	infos := make([]ClusterInfo, 0, len(gqlResp.Data.ClusterConnection.Nodes))
	for _, node := range gqlResp.Data.ClusterConnection.Nodes {
		infos = append(infos, ClusterInfo{
			ClusterID:       strings.TrimSpace(node.ID),
			ClusterName:     strings.TrimSpace(node.Name),
			SoftwareVersion: strings.TrimSpace(node.Version),
			APIVersion:      apiVersion,
		})
	}
	return infos, nil
}

func FetchSLAPolicies(ctx context.Context, client *http.Client, endpoint string) ([]SLAPolicy, error) {
	const query = `query OpsctlListSLA($after: String) { slaDomains(first: 100, after: $after) { nodes { id name ... on GlobalSlaReply { isDefault objectTypes snapshotSchedule { minute { basicSchedule { frequency retention retentionUnit } } hourly { basicSchedule { frequency retention retentionUnit } } daily { basicSchedule { frequency retention retentionUnit } } weekly { basicSchedule { frequency retention retentionUnit } } monthly { basicSchedule { frequency retention retentionUnit } } quarterly { basicSchedule { frequency retention retentionUnit } } yearly { basicSchedule { frequency retention retentionUnit } } } archivalSpecs { threshold thresholdUnit storageSetting { name } } } ... on ClusterSlaDomain { cdmId cluster { name } snapshotSchedule { minute { basicSchedule { frequency retention retentionUnit } } hourly { basicSchedule { frequency retention retentionUnit } } daily { basicSchedule { frequency retention retentionUnit } } weekly { basicSchedule { frequency retention retentionUnit } } monthly { basicSchedule { frequency retention retentionUnit } } quarterly { basicSchedule { frequency retention retentionUnit } } yearly { basicSchedule { frequency retention retentionUnit } } } archivalSpec { threshold thresholdUnit archivalLocationName } } } pageInfo { endCursor hasNextPage } } }`

	var policies []SLAPolicy
	var cursor *string
	for {
		body, _, err := request(ctx, client, endpoint, query, map[string]any{"after": cursor}, nil, 1)
		if err != nil {
			return nil, err
		}

		var gqlResp struct {
			Data struct {
				SLADomains struct {
					Nodes []struct {
						ID               string              `json:"id"`
						Name             string              `json:"name"`
						IsDefault        bool                `json:"isDefault"`
						ObjectTypes      []string            `json:"objectTypes"`
						SnapshotSchedule SLASnapshotSchedule `json:"snapshotSchedule"`
						ArchivalSpecs    []SLAArchivalSpec   `json:"archivalSpecs"`
						CDMID            string              `json:"cdmId"`
						Cluster          struct {
							Name string `json:"name"`
						} `json:"cluster"`
						ArchivalSpec SLAClusterArchivalSpec `json:"archivalSpec"`
					} `json:"nodes"`
					PageInfo struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage bool    `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"slaDomains"`
			} `json:"data"`
			Errors []GraphQLError `json:"errors"`
		}
		if err := json.Unmarshal(body, &gqlResp); err != nil {
			return nil, fmt.Errorf("parse RSC SLA response: %w", err)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("RSC GraphQL error: %s", gqlResp.Errors[0].Message)
		}

		for _, node := range gqlResp.Data.SLADomains.Nodes {
			policy := SLAPolicy{ID: strings.TrimSpace(node.ID), Name: strings.TrimSpace(node.Name)}
			if strings.TrimSpace(node.CDMID) != "" {
				policy.Kind = "cluster"
				if policy.ID == "" {
					policy.ID = strings.TrimSpace(node.CDMID)
				}
				policy.Cluster = strings.TrimSpace(node.Cluster.Name)
				policy.Schedule = SummarizeSchedule(node.SnapshotSchedule)
				policy.Retention = SummarizeRetention(node.SnapshotSchedule)
				policy.RetentionDays = RetentionDays(node.SnapshotSchedule)
				if spec := node.ArchivalSpec; spec.ArchivalLocationName != "" || spec.Threshold != 0 {
					policy.Archive = fmt.Sprintf("after %d %s: %s", spec.Threshold, strings.TrimSpace(spec.ThresholdUnit), UnknownIfEmpty(spec.ArchivalLocationName))
				}
			} else {
				policy.Kind = "global"
				policy.ObjectTypes = node.ObjectTypes
				policy.Schedule = SummarizeSchedule(node.SnapshotSchedule)
				policy.Retention = SummarizeRetention(node.SnapshotSchedule)
				policy.RetentionDays = RetentionDays(node.SnapshotSchedule)
				policy.Archive = SummarizeArchival(node.ArchivalSpecs)
				policy.Default = node.IsDefault
			}
			policies = append(policies, policy)
		}

		pageInfo := gqlResp.Data.SLADomains.PageInfo
		if !pageInfo.HasNextPage || pageInfo.EndCursor == nil || strings.TrimSpace(*pageInfo.EndCursor) == "" {
			break
		}
		cursor = pageInfo.EndCursor
	}
	return policies, nil
}

func SummarizeSchedule(schedule SLASnapshotSchedule) string {
	name, basic, ok := FirstSchedulePeriod(schedule)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s every %d", name, basic.Frequency)
}

func SummarizeRetention(schedule SLASnapshotSchedule) string {
	_, basic, ok := FirstSchedulePeriod(schedule)
	if !ok || basic.Retention == 0 {
		return ""
	}
	return fmt.Sprintf("%d %s", basic.Retention, strings.TrimSpace(basic.RetentionUnit))
}

func RetentionDays(schedule SLASnapshotSchedule) int {
	_, basic, ok := FirstSchedulePeriod(schedule)
	if !ok || basic.Retention == 0 {
		return 0
	}
	switch strings.ToUpper(strings.TrimSpace(basic.RetentionUnit)) {
	case "MINUTES":
		return basic.Retention / (24 * 60)
	case "HOURS":
		return basic.Retention / 24
	case "DAYS":
		return basic.Retention
	case "WEEKS":
		return basic.Retention * 7
	case "MONTHS":
		return basic.Retention * 30
	case "QUARTERS":
		return basic.Retention * 90
	case "YEARS":
		return basic.Retention * 365
	default:
		return 0
	}
}

func FirstSchedulePeriod(schedule SLASnapshotSchedule) (string, SLABasicSchedule, bool) {
	periods := []struct {
		name string
		data SLASchedulePeriod
	}{
		{"minute", schedule.Minute},
		{"hourly", schedule.Hourly},
		{"daily", schedule.Daily},
		{"weekly", schedule.Weekly},
		{"monthly", schedule.Monthly},
		{"quarterly", schedule.Quarterly},
		{"yearly", schedule.Yearly},
	}
	for _, period := range periods {
		basic := period.data.BasicSchedule
		if basic.Frequency != 0 || basic.Retention != 0 {
			return period.name, basic, true
		}
	}
	return "", SLABasicSchedule{}, false
}

func SummarizeArchival(specs []SLAArchivalSpec) string {
	if len(specs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(specs))
	for _, spec := range specs {
		location := strings.TrimSpace(spec.StorageSetting.Name)
		if location == "" {
			location = "unknown"
		}
		location = ShortenText(location, 36)
		if spec.Threshold == 0 {
			parts = append(parts, location)
			continue
		}
		parts = append(parts, fmt.Sprintf("after %d %s: %s", spec.Threshold, strings.TrimSpace(spec.ThresholdUnit), location))
	}
	return strings.Join(parts, "; ")
}

func ClusterFilterArgument(cluster string) string {
	clusterName := strings.TrimSpace(cluster)
	if clusterName == "" || clusterName == "%" {
		return ""
	}
	return fmt.Sprintf("filter: {name: %q}, ", clusterName)
}

func FilterClusterInfos(infos []ClusterInfo, clusterName string) []ClusterInfo {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" || clusterName == "%" {
		return infos
	}
	filtered := make([]ClusterInfo, 0, len(infos))
	for _, info := range infos {
		if strings.EqualFold(strings.TrimSpace(info.ClusterName), clusterName) {
			filtered = append(filtered, info)
		}
	}
	return filtered
}

func FilterSLAPolicies(policies []SLAPolicy, clusterName string) []SLAPolicy {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" || clusterName == "%" {
		return policies
	}
	filtered := make([]SLAPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy.Kind == "global" || strings.EqualFold(strings.TrimSpace(policy.Cluster), clusterName) {
			filtered = append(filtered, policy)
		}
	}
	return filtered
}

func FilterSLAPoliciesByName(policies []SLAPolicy, pattern string) []SLAPolicy {
	pattern = normalizeWildcardFilter(pattern)
	filtered := make([]SLAPolicy, 0, len(policies))
	for _, policy := range policies {
		if globMatch(pattern, policy.Name) {
			filtered = append(filtered, policy)
		}
	}
	return filtered
}

func FilterSLAPoliciesBySourceTypes(policies []SLAPolicy, sourceTypes []string) []SLAPolicy {
	filters := make(map[string]struct{}, len(sourceTypes))
	for _, sourceType := range NormalizeSourceTypes(sourceTypes) {
		filters[sourceType] = struct{}{}
	}
	if len(filters) == 0 {
		return policies
	}
	filtered := make([]SLAPolicy, 0, len(policies))
	for _, policy := range policies {
		for _, objectType := range policy.ObjectTypes {
			normalizedTypes := NormalizeSourceTypes([]string{objectType})
			if len(normalizedTypes) == 1 {
				if _, ok := filters[normalizedTypes[0]]; ok {
					filtered = append(filtered, policy)
					break
				}
			}
		}
	}
	return filtered
}

func ShortenText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum-3] + "..."
}

func FetchDFsForCluster(ctx context.Context, client *http.Client, endpoint string, clusterName string) ([]DFInfo, error) {
	query := fmt.Sprintf(`query OpsctlRSCDF { clusterConnection(%sfirst: 100) { nodes { id name metric { totalCapacity availableCapacity usedCapacity snapshotCapacity liveMountCapacity miscellaneousCapacity pendingSnapshotCapacity cdpCapacity ingestedSnapshotStorage physicalSnapshotStorage ingestedArchivalStorage physicalArchivalStorage averageDailyGrowth immutabilityOverhead lastUpdateTime } } } rcvAccountEntitlement { backupEntitlement { capacity bundle tier redundancy } archiveEntitlement { capacity bundle tier redundancy } entitlements { entitlement { capacity bundle tier redundancy } usedCapacity } } }`, ClusterFilterArgument(clusterName))
	body, _, err := request(ctx, client, endpoint, query, nil, nil, 1)
	if err != nil {
		return nil, err
	}

	var gqlResp struct {
		Data struct {
			ClusterConnection struct {
				Nodes []struct {
					ID     string   `json:"id"`
					Name   string   `json:"name"`
					Metric DFMetric `json:"metric"`
				} `json:"nodes"`
			} `json:"clusterConnection"`
			RCVAccountEntitlement struct {
				BackupEntitlement  DFEntitlement `json:"backupEntitlement"`
				ArchiveEntitlement DFEntitlement `json:"archiveEntitlement"`
				Entitlements       []struct {
					Entitlement  DFEntitlement `json:"entitlement"`
					UsedCapacity *float64      `json:"usedCapacity"`
				} `json:"entitlements"`
			} `json:"rcvAccountEntitlement"`
		} `json:"data"`
		Errors []GraphQLError `json:"errors"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse RSC df response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("RSC GraphQL error: %s", gqlResp.Errors[0].Message)
	}
	if len(gqlResp.Data.ClusterConnection.Nodes) == 0 {
		return nil, fmt.Errorf("RSC df query returned no clusters")
	}

	dfs := make([]DFInfo, 0, len(gqlResp.Data.ClusterConnection.Nodes))
	for _, cluster := range gqlResp.Data.ClusterConnection.Nodes {
		df := DFInfo{
			ClusterID:          strings.TrimSpace(cluster.ID),
			ClusterName:        strings.TrimSpace(cluster.Name),
			Metric:             cluster.Metric,
			BackupEntitlement:  gqlResp.Data.RCVAccountEntitlement.BackupEntitlement,
			ArchiveEntitlement: gqlResp.Data.RCVAccountEntitlement.ArchiveEntitlement,
		}
		for _, entitlement := range gqlResp.Data.RCVAccountEntitlement.Entitlements {
			usage := entitlement.Entitlement
			usage.UsedCapacity = entitlement.UsedCapacity
			df.Entitlements = append(df.Entitlements, usage)
		}
		dfs = append(dfs, df)
	}
	return dfs, nil
}

func FetchHealthsForCluster(ctx context.Context, client *http.Client, endpoint string, clusterName string) ([]HealthInfo, error) {
	query := fmt.Sprintf(`query OpsctlRSCHealth { clusterConnection(%sfirst: 100) { nodes { id name status subStatus passesConnectivityCheck clusterNodeConnection(first: 100) { nodes { id hostname ipAddress status subStatus platformType role hardwareHealth { isHealthy message policyName } needsInspection } } clusterDiskConnection(first: 100) { nodes { diskId nodeId status path raidStatus raidType diskMode diskType } } } } }`, ClusterFilterArgument(clusterName))
	body, _, err := request(ctx, client, endpoint, query, nil, nil, 1)
	if err != nil {
		return nil, err
	}

	var gqlResp struct {
		Data struct {
			ClusterConnection struct {
				Nodes []struct {
					ID                              string `json:"id"`
					Name                            string `json:"name"`
					Status                          string `json:"status"`
					SubStatus                       string `json:"subStatus"`
					PassesConnectivityCheck         bool   `json:"passesConnectivityCheck"`
					GlobalManagerConnectivityStatus struct {
						URLs []string `json:"urls"`
					} `json:"globalManagerConnectivityStatus"`
					ClusterNodeConnection struct {
						Nodes []struct {
							ID             string `json:"id"`
							Hostname       string `json:"hostname"`
							IPAddress      string `json:"ipAddress"`
							Status         string `json:"status"`
							SubStatus      string `json:"subStatus"`
							PlatformType   string `json:"platformType"`
							Role           string `json:"role"`
							HardwareHealth []struct {
								IsHealthy  bool   `json:"isHealthy"`
								Message    string `json:"message"`
								PolicyName string `json:"policyName"`
							} `json:"hardwareHealth"`
							NeedsInspection bool `json:"needsInspection"`
						} `json:"nodes"`
					} `json:"clusterNodeConnection"`
					ClusterDiskConnection struct {
						Nodes []struct {
							ID         string `json:"diskId"`
							NodeID     string `json:"nodeId"`
							Status     string `json:"status"`
							Path       string `json:"path"`
							RaidStatus string `json:"raidStatus"`
							RaidType   string `json:"raidType"`
							DiskMode   string `json:"diskMode"`
							DiskType   string `json:"diskType"`
						} `json:"nodes"`
					} `json:"clusterDiskConnection"`
				} `json:"nodes"`
			} `json:"clusterConnection"`
		} `json:"data"`
		Errors []GraphQLError `json:"errors"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse RSC health response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("RSC GraphQL error: %s", gqlResp.Errors[0].Message)
	}
	if len(gqlResp.Data.ClusterConnection.Nodes) == 0 {
		return nil, fmt.Errorf("RSC health query returned no clusters")
	}

	healths := make([]HealthInfo, 0, len(gqlResp.Data.ClusterConnection.Nodes))
	for _, cluster := range gqlResp.Data.ClusterConnection.Nodes {
		health := HealthInfo{
			ClusterID:     strings.TrimSpace(cluster.ID),
			ClusterName:   strings.TrimSpace(cluster.Name),
			OverallStatus: strings.TrimSpace(cluster.Status),
		}
		if cluster.PassesConnectivityCheck {
			health.Connectivity = "CONNECTED"
		} else {
			health.Connectivity = "DISCONNECTED"
		}

		for _, node := range cluster.ClusterNodeConnection.Nodes {
			healthNode := HealthNode{
				ID:              strings.TrimSpace(node.ID),
				Hostname:        strings.TrimSpace(node.Hostname),
				IPAddress:       strings.TrimSpace(node.IPAddress),
				Status:          strings.TrimSpace(node.Status),
				SubStatus:       strings.TrimSpace(node.SubStatus),
				Classification:  strings.ToLower(strings.TrimSpace(node.PlatformType)),
				Role:            strings.TrimSpace(node.Role),
				NeedsInspection: node.NeedsInspection,
			}
			if len(node.HardwareHealth) > 0 {
				parts := make([]string, 0, len(node.HardwareHealth))
				for _, healthPolicy := range node.HardwareHealth {
					if policy := strings.TrimSpace(healthPolicy.PolicyName); policy != "" {
						parts = append(parts, policy)
					} else if message := strings.TrimSpace(healthPolicy.Message); message != "" {
						parts = append(parts, message)
					}
				}
				if len(parts) > 0 {
					healthNode.HardwareHealth = strings.Join(parts, "; ")
				}
			}
			health.Nodes = append(health.Nodes, healthNode)
		}
		for _, disk := range cluster.ClusterDiskConnection.Nodes {
			health.Disks = append(health.Disks, HealthDisk{
				ID:         strings.TrimSpace(disk.ID),
				NodeID:     strings.TrimSpace(disk.NodeID),
				Status:     strings.TrimSpace(disk.Status),
				Path:       strings.TrimSpace(disk.Path),
				RaidStatus: strings.TrimSpace(disk.RaidStatus),
				RaidType:   strings.TrimSpace(disk.RaidType),
				DiskMode:   strings.TrimSpace(disk.DiskMode),
				DiskType:   strings.TrimSpace(disk.DiskType),
			})
		}
		healths = append(healths, health)
	}
	return healths, nil
}

func ExtractAPIVersion(header http.Header, extensions map[string]any) string {
	for _, key := range []string{"x-rubrik-api-version", "x-api-version", "api-version"} {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return value
		}
	}
	for _, key := range []string{"apiVersion", "api_version"} {
		if raw, ok := extensions[key]; ok {
			if str, ok := raw.(string); ok {
				if str = strings.TrimSpace(str); str != "" {
					return str
				}
			}
		}
	}
	return ""
}

func NormalizeDiskMedia(diskType string, diskMode string) string {
	diskType = strings.TrimSpace(strings.ToUpper(diskType))
	diskMode = strings.TrimSpace(strings.ToUpper(diskMode))

	switch diskType {
	case "FLASH", "SSD", "NVME", "SASFLASH", "SAS-SSD":
		return "FLASH"
	case "HDD", "SATA", "SAS", "SAS-HDD":
		return "HDD"
	case "UNKNOWN", "", "N/A":
		if diskMode != "" && diskMode != "UNKNOWN" {
			return diskMode
		}
		return "UNKNOWN"
	default:
		if diskType != "" {
			return diskType
		}
		return "UNKNOWN"
	}
}

func UnknownIfEmpty(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
