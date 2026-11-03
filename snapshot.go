package rsc

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SnapshotRow struct {
	ID                string        `json:"id"`
	WorkloadID        string        `json:"workload_id,omitempty"`
	Date              time.Time     `json:"date"`
	Size              int64         `json:"size"`
	Duration          time.Duration `json:"duration"`
	Storage           string        `json:"storage"`
	Path              string        `json:"path"`
	TargetHost        string        `json:"target_host"`
	SVM               string        `json:"svm"`
	Volume            string        `json:"volume"`
	SLAName           string        `json:"sla_name"`
	ConfiguredSLAName string        `json:"configured_sla_name"`
	EffectiveSLAName  string        `json:"effective_sla_name"`
	ShareType         string        `json:"share_type"`
	Template          string        `json:"template"`
	Instance          string        `json:"instance"`
	Cluster           string        `json:"cluster"`
	IsArchived        bool          `json:"is_archived"`
	IsLocal           bool          `json:"is_local"`
	IsOnDemand        bool          `json:"is_on_demand"`
	CloudState        string        `json:"cloud_state"`
	IndexState        string        `json:"index_state"`
	ArchivalLocations []string      `json:"archival_locations"`
}

type SnapshotFilters struct {
	SearchTerm string
	SLA        string
	Volume     string
	SVM        string
	Since      string
	Till       string
	Types      []string
	TargetPath string
	Storage    string
	Cluster    string
	OnlyBroken bool
}

func FilterBrokenSnapshots(rows []SnapshotRow) []SnapshotRow {
	broken := make([]SnapshotRow, 0, len(rows))
	for _, row := range rows {
		if IsUnknownSnapshotTarget(row.TargetHost) || IsUnknownSnapshotTarget(row.Volume) {
			broken = append(broken, row)
		}
	}
	return broken
}

func IsUnknownSnapshotTarget(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "-" || strings.EqualFold(value, "unknown")
}

type SnapshotGQLArchivalLocation struct {
	Name         string `json:"name"`
	LocationID   string `json:"locationId"`
	Status       string `json:"status"`
	TieringState string `json:"tieringState"`
}

type SnapshotDataLocation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ClusterID string `json:"clusterUuid"`
	Type      string `json:"type"`
	IsActive  *bool  `json:"isActive"`
}

type SnapshotGQLNode struct {
	SnapshotType         string                        `json:"__typename"`
	ID                   string                        `json:"id"`
	Date                 any                           `json:"date"`
	Size                 *int64                        `json:"size"`
	SnapshotDiskSize     *int64                        `json:"snapshotDiskSize"`
	ExpirationDate       any                           `json:"expirationDate"`
	IsOnDemand           bool                          `json:"isOnDemand"`
	IndexState           string                        `json:"indexState"`
	CloudState           string                        `json:"cloudState"`
	IsArchived           bool                          `json:"isArchived"`
	IsReplica            bool                          `json:"isReplica"`
	IsLocal              bool                          `json:"isLocal"`
	TimeTaken            any                           `json:"timeTaken"`
	BackupDuration       any                           `json:"backupDuration"`
	ArchivalLocationInfo []SnapshotGQLArchivalLocation `json:"archivalLocationInfo"`
	LocalLocations       []SnapshotDataLocation        `json:"localLocations"`
	ArchivalLocations    []SnapshotDataLocation        `json:"archivalLocations"`
	ReplicationLocations []SnapshotDataLocation        `json:"replicationLocations"`
	SLADomain            struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"slaDomain"`
}

type SnapshotGQLDescendant struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	PhysicalPath        []FilesetPath  `json:"physicalPath"`
	ConfiguredSLADomain FilesetSLA     `json:"configuredSlaDomain"`
	EffectiveSLADomain  FilesetSLA     `json:"effectiveSlaDomain"`
	Cluster             FilesetCluster `json:"cluster"`
	SnapshotConnection  struct {
		Nodes    []SnapshotGQLNode `json:"nodes"`
		PageInfo struct {
			EndCursor   *string `json:"endCursor"`
			HasNextPage bool    `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"snapshotConnection"`
}

type SnapshotGQLTemplate struct {
	Name                string         `json:"name"`
	Includes            []string       `json:"includes"`
	ShareType           string         `json:"shareType"`
	OSType              string         `json:"osType"`
	Cluster             FilesetCluster `json:"cluster"`
	ConfiguredSLADomain FilesetSLA     `json:"configuredSlaDomain"`
	EffectiveSLADomain  FilesetSLA     `json:"effectiveSlaDomain"`
	Descendants         struct {
		Nodes    []SnapshotGQLDescendant `json:"nodes"`
		PageInfo struct {
			EndCursor   *string `json:"endCursor"`
			HasNextPage bool    `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"descendantConnection"`
}

type WorkloadSnapshotTarget struct {
	WorkloadID   string
	TemplateName string
	InstanceName string
	Includes     []string
	PhysicalPath []FilesetPath
	Configured   FilesetSLA
	Effective    FilesetSLA
	Cluster      string
	ShareType    string
	HostAddress  string
	ExportPoint  string
	IsNASShare   bool
}

func FetchSnapshots(ctx context.Context, client *http.Client, endpoint string, concurrency int) ([]SnapshotRow, error) {
	const queryTemplatePrimary = `query OpsctlFilesetSnapshots { filesetTemplates(hostRoot: %s, first: 100, after: %s) { nodes { name includes shareType osType cluster { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } descendantConnection(first: 100) { nodes { id name physicalPath { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } cluster { name } } pageInfo { hasNextPage } } } pageInfo { endCursor hasNextPage } } }`
	const queryTemplateFallback = `query OpsctlFilesetSnapshotsFallback { filesetTemplates(hostRoot: %s, first: 100, after: %s) { nodes { name includes shareType osType cluster { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } descendantConnection(first: 100) { nodes { id name physicalPath { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } cluster { name } } pageInfo { hasNextPage } } } pageInfo { endCursor hasNextPage } } }`

	var targets []WorkloadSnapshotTarget
	for _, root := range []string{"WINDOWS_HOST_ROOT", "LINUX_HOST_ROOT", "NAS_HOST_ROOT"} {
		var after *string
		for {
			afterValue := "null"
			if after != nil {
				afterValue = fmt.Sprintf("%q", *after)
			}
			query := fmt.Sprintf(queryTemplatePrimary, root, afterValue)
			var response struct {
				Data struct {
					Filesets struct {
						Nodes    []SnapshotGQLTemplate `json:"nodes"`
						PageInfo struct {
							EndCursor   *string `json:"endCursor"`
							HasNextPage bool    `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"filesetTemplates"`
				} `json:"data"`
			}
			if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {

				fallbackQuery := fmt.Sprintf(queryTemplateFallback, root, afterValue)
				if fallbackErr := ExecuteSLAGraphQL(ctx, client, endpoint, fallbackQuery, &response); fallbackErr != nil {
					if IsNotFoundError(err) || IsNotFoundError(fallbackErr) {
						logf(ctx, 2, "rsc snapshot: fileset source type %s not found: %v", root, err)
						break
					}
					return nil, err
				}
			}

			for _, template := range response.Data.Filesets.Nodes {
				for _, descendant := range template.Descendants.Nodes {
					if strings.TrimSpace(descendant.ID) == "" {
						continue
					}
					configured, effective := descendant.ConfiguredSLADomain, descendant.EffectiveSLADomain
					if configured.ID == "" {
						configured = template.ConfiguredSLADomain
					}
					if effective.ID == "" {
						effective = template.EffectiveSLADomain
					}
					cluster := descendant.Cluster.Name
					if cluster == "" {
						cluster = template.Cluster.Name
					}
					shareType := template.ShareType
					if shareType == "" {
						shareType = template.OSType
					}

					targets = append(targets, WorkloadSnapshotTarget{
						WorkloadID:   descendant.ID,
						TemplateName: template.Name,
						InstanceName: descendant.Name,
						Includes:     template.Includes,
						PhysicalPath: descendant.PhysicalPath,
						Configured:   configured,
						Effective:    effective,
						Cluster:      cluster,
						ShareType:    shareType,
					})
				}
			}

			if !response.Data.Filesets.PageInfo.HasNextPage || response.Data.Filesets.PageInfo.EndCursor == nil {
				break
			}
			after = response.Data.Filesets.PageInfo.EndCursor
		}
	}

	nasTargets, nasErr := FetchNASSharesTargets(ctx, client, endpoint)
	if nasErr == nil && len(nasTargets) > 0 {
		targets = append(targets, nasTargets...)
	} else if nasErr != nil {
		logf(ctx, 2, "rsc snapshot: NAS shares target fetch unavailable: %v", nasErr)
	}

	logf(ctx, 2, "rsc snapshot: querying snapshots for %d workloads (concurrency=%d)", len(targets), concurrency)
	return QueryWorkloadSnapshotsParallel(ctx, client, endpoint, targets, concurrency)
}

func FetchNASSharesTargets(ctx context.Context, client *http.Client, endpoint string) ([]WorkloadSnapshotTarget, error) {
	const queryTemplate = `query OpsctlNASSharesSnapshots { nasShares(first: 100, after: %s) { nodes { id name exportPoint hostAddress shareType primaryFileset { id name } cluster { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } } pageInfo { endCursor hasNextPage } } }`
	var targets []WorkloadSnapshotTarget
	var after *string
	for {
		afterValue := "null"
		if after != nil {
			afterValue = fmt.Sprintf("%q", *after)
		}
		query := fmt.Sprintf(queryTemplate, afterValue)
		var response struct {
			Data struct {
				Shares struct {
					Nodes []struct {
						ID             string `json:"id"`
						Name           string `json:"name"`
						ExportPoint    string `json:"exportPoint"`
						HostAddress    string `json:"hostAddress"`
						ShareType      string `json:"shareType"`
						PrimaryFileset struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"primaryFileset"`
						Cluster struct {
							Name string `json:"name"`
						} `json:"cluster"`
						ConfiguredSLA FilesetSLA `json:"configuredSlaDomain"`
						EffectiveSLA  FilesetSLA `json:"effectiveSlaDomain"`
					} `json:"nodes"`
					PageInfo struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage bool    `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"nasShares"`
			} `json:"data"`
		}
		if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
			return nil, err
		}
		for _, share := range response.Data.Shares.Nodes {
			if strings.TrimSpace(share.PrimaryFileset.ID) == "" {
				continue
			}
			targets = append(targets, WorkloadSnapshotTarget{
				WorkloadID:   share.PrimaryFileset.ID,
				TemplateName: share.PrimaryFileset.Name,
				InstanceName: share.Name,
				HostAddress:  share.HostAddress,
				ExportPoint:  share.ExportPoint,
				ShareType:    share.ShareType,
				Cluster:      share.Cluster.Name,
				Configured:   share.ConfiguredSLA,
				Effective:    share.EffectiveSLA,
				IsNASShare:   true,
			})
		}
		if !response.Data.Shares.PageInfo.HasNextPage || response.Data.Shares.PageInfo.EndCursor == nil {
			break
		}
		after = response.Data.Shares.PageInfo.EndCursor
	}
	return targets, nil
}

func QueryWorkloadSnapshotsParallel(ctx context.Context, client *http.Client, endpoint string, targets []WorkloadSnapshotTarget, concurrency int) ([]SnapshotRow, error) {
	if concurrency <= 0 {
		concurrency = 10
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var allSnapshots []SnapshotRow

	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		t := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			snapshotNodes, err := FetchSnapshotsForWorkload(ctx, client, endpoint, t.WorkloadID)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			var targetHost, targetPath string
			if t.IsNASShare {
				targetPath = strings.TrimSpace(t.ExportPoint)
				if targetPath == "" {
					targetPath = strings.TrimSpace(t.InstanceName)
				}
				targetHost = strings.TrimSpace(t.HostAddress)
			} else {
				_, targetHost, targetPath = SplitFilesetPhysicalPath(t.PhysicalPath, t.Includes)
			}

			var rows []SnapshotRow
			for _, snapNode := range snapshotNodes {
				snapDate, _ := ParseSnapshotDate(snapNode.Date)
				snapSize := GetSnapshotSize(snapNode)
				snapDuration := ParseDuration(snapNode.BackupDuration)
				if snapDuration == 0 {
					snapDuration = ParseDuration(snapNode.TimeTaken)
				}
				storage := DetermineSnapshotStorage(snapNode)

				slaName := snapNode.SLADomain.Name
				if slaName == "" {
					slaName = t.Effective.Name
				}
				if slaName == "" {
					slaName = t.Configured.Name
				}

				var archLocs []string
				for _, loc := range snapNode.ArchivalLocationInfo {
					if loc.Name != "" {
						archLocs = append(archLocs, loc.Name)
					}
				}

				rows = append(rows, SnapshotRow{
					ID:                strings.TrimSpace(snapNode.ID),
					WorkloadID:        strings.TrimSpace(t.WorkloadID),
					Date:              snapDate,
					Size:              snapSize,
					Duration:          snapDuration,
					Storage:           storage,
					Path:              targetPath,
					TargetHost:        targetHost,
					SLAName:           strings.TrimSpace(slaName),
					ConfiguredSLAName: strings.TrimSpace(t.Configured.Name),
					EffectiveSLAName:  strings.TrimSpace(t.Effective.Name),
					ShareType:         strings.TrimSpace(t.ShareType),
					Template:          strings.TrimSpace(t.TemplateName),
					Instance:          strings.TrimSpace(t.InstanceName),
					Cluster:           strings.TrimSpace(t.Cluster),
					IsArchived:        snapNode.IsArchived,
					IsLocal:           snapNode.IsLocal,
					IsOnDemand:        snapNode.IsOnDemand,
					CloudState:        strings.TrimSpace(snapNode.CloudState),
					IndexState:        strings.TrimSpace(snapNode.IndexState),
					ArchivalLocations: archLocs,
				})
			}

			mu.Lock()
			allSnapshots = append(allSnapshots, rows...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return allSnapshots, nil
}

type ActivitySeriesGQLNode struct {
	ID                 any       `json:"id"`
	ActivitySeriesID   string    `json:"activitySeriesId"`
	ObjectID           string    `json:"objectId"`
	ObjectName         string    `json:"objectName"`
	ObjectType         string    `json:"objectType"`
	StartTime          time.Time `json:"startTime"`
	LastUpdated        time.Time `json:"lastUpdated"`
	LastActivityType   string    `json:"lastActivityType"`
	LastActivityStatus string    `json:"lastActivityStatus"`
	DataTransferred    int64     `json:"dataTransferred"`
	LogicalSize        int64     `json:"logicalSize"`
	WorkloadID         string    `json:"-"`
}

func EnrichSnapshotsWithBackupActivities(ctx context.Context, client *http.Client, endpoint string, rows []SnapshotRow, filters SnapshotFilters, maxResults, concurrency int) ([]SnapshotRow, error) {
	if len(rows) == 0 {
		return rows, nil
	}

	searchTerms := []string{}
	if filters.SearchTerm != "" {
		searchTerms = append(searchTerms, filters.SearchTerm)
	}
	if filters.Volume != "" && filters.Volume != filters.SearchTerm {
		searchTerms = append(searchTerms, filters.Volume)
	}
	if filters.TargetPath != "" && filters.TargetPath != filters.SearchTerm {
		searchTerms = append(searchTerms, path.Base(filters.TargetPath))
	}
	var allActivities []ActivitySeriesGQLNode
	seenIDs := make(map[string]struct{})

	if len(searchTerms) == 0 {
		workloadIDs := UniqueSnapshotWorkloadIDs(rows)
		if len(workloadIDs) > 0 {
			activities, err := FetchBackupActivitiesForWorkloads(ctx, client, endpoint, workloadIDs, maxResults, concurrency)
			if err != nil {
				return nil, err
			}
			allActivities = append(allActivities, activities...)
		} else {
			searchTerms = append(searchTerms, "")
		}
	}

	for _, term := range searchTerms {
		activities, err := FetchBackupActivitySeries(ctx, client, endpoint, term, "", maxResults)
		if err != nil {
			return nil, fmt.Errorf("query backup activities for %q: %w", term, err)
		}
		for _, act := range activities {
			actID := fmt.Sprintf("%v", act.ID)
			if actID == "" {
				actID = act.ActivitySeriesID
			}
			if _, exists := seenIDs[actID]; !exists {
				seenIDs[actID] = struct{}{}
				allActivities = append(allActivities, act)
			}
		}
	}

	logf(ctx, 2, "rsc snapshot: collected %d backup activity series for detailed enrichment", len(allActivities))

	for i := range rows {
		var bestAct *ActivitySeriesGQLNode
		var bestDiff time.Duration = 1 << 62

		for j := range allActivities {
			act := &allActivities[j]
			if MatchActivityToSnapshot(rows[i], *act) {
				diff := act.StartTime.Sub(rows[i].Date)
				if diff < 0 {
					diff = -diff
				}
				if diff < bestDiff {
					bestDiff = diff
					bestAct = act
				}
			}
		}

		if bestAct != nil {
			if bestAct.LastUpdated.After(bestAct.StartTime) {
				rows[i].Duration = bestAct.LastUpdated.Sub(bestAct.StartTime)
			}
			if bestAct.LogicalSize > 0 {
				rows[i].Size = bestAct.LogicalSize
			} else if bestAct.DataTransferred > 0 {
				rows[i].Size = bestAct.DataTransferred
			}
		}
	}

	return rows, nil
}

func UniqueSnapshotWorkloadIDs(rows []SnapshotRow) []string {
	seen := make(map[string]struct{})
	var workloadIDs []string
	for _, row := range rows {
		workloadID := strings.TrimSpace(row.WorkloadID)
		if workloadID == "" {
			continue
		}
		if _, ok := seen[workloadID]; ok {
			continue
		}
		seen[workloadID] = struct{}{}
		workloadIDs = append(workloadIDs, workloadID)
	}
	return workloadIDs
}

func FetchBackupActivitiesForWorkloads(ctx context.Context, client *http.Client, endpoint string, workloadIDs []string, maxResults, concurrency int) ([]ActivitySeriesGQLNode, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var activities []ActivitySeriesGQLNode
	var firstErr error

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for workloadID := range jobs {
				workloadActivities, err := FetchBackupActivitySeries(ctx, client, endpoint, "", workloadID, maxResults)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("query backup activities for workload %q: %w", workloadID, err)
						cancel()
					}
					mu.Unlock()
					continue
				}
				for i := range workloadActivities {
					workloadActivities[i].WorkloadID = workloadID
				}
				mu.Lock()
				activities = append(activities, workloadActivities...)
				mu.Unlock()
			}
		}()
	}

sendJobs:
	for _, workloadID := range workloadIDs {
		select {
		case jobs <- workloadID:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return activities, nil
}

func FetchBackupActivitySeries(ctx context.Context, client *http.Client, endpoint, searchTerm, workloadID string, maxResults int) ([]ActivitySeriesGQLNode, error) {
	const queryTemplate = `query OpsctlBackupActivities { activitySeriesConnection(first: 100, after: %s, filters: {%s}) { nodes { id activitySeriesId objectId objectName objectType startTime lastUpdated lastActivityType lastActivityStatus dataTransferred logicalSize } pageInfo { endCursor hasNextPage } } }`

	filterParts := []string{"lastActivityType: [BACKUP]"}
	if strings.TrimSpace(searchTerm) != "" {
		filterParts = append(filterParts, fmt.Sprintf(`searchTerm: %q`, strings.TrimSpace(searchTerm)))
	}
	if strings.TrimSpace(workloadID) != "" {
		filterParts = append(filterParts, fmt.Sprintf(`objectFid: [%q]`, strings.TrimSpace(workloadID)))
	}

	var activities []ActivitySeriesGQLNode
	var after *string
	for {
		afterValue := "null"
		if after != nil {
			afterValue = fmt.Sprintf("%q", *after)
		}
		query := fmt.Sprintf(queryTemplate, afterValue, strings.Join(filterParts, ", "))
		var response struct {
			Data struct {
				Activities struct {
					Nodes    []ActivitySeriesGQLNode `json:"nodes"`
					PageInfo struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage bool    `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"activitySeriesConnection"`
			} `json:"data"`
		}
		if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
			return nil, err
		}
		activities = append(activities, response.Data.Activities.Nodes...)
		if !response.Data.Activities.PageInfo.HasNextPage || response.Data.Activities.PageInfo.EndCursor == nil {
			break
		}
		if maxResults > 0 && len(activities) >= maxResults {
			break
		}
		after = response.Data.Activities.PageInfo.EndCursor
	}
	return activities, nil
}

func MatchActivityToSnapshot(snap SnapshotRow, act ActivitySeriesGQLNode) bool {
	if snap.Date.IsZero() || act.StartTime.IsZero() {
		return false
	}
	diff := act.StartTime.Sub(snap.Date)
	if diff < 0 {
		diff = -diff
	}
	if diff > 1800*time.Second {
		return false
	}

	if snap.WorkloadID != "" && act.WorkloadID == snap.WorkloadID {
		return true
	}
	if snap.WorkloadID != "" && strings.Contains(strings.ToLower(act.ObjectID), strings.ToLower(snap.WorkloadID)) {
		return true
	}
	if snap.Volume != "" && strings.Contains(strings.ToLower(act.ObjectName), strings.ToLower(snap.Volume)) {
		return true
	}
	if snap.Path != "" && strings.Contains(strings.ToLower(act.ObjectName), strings.ToLower(snap.Path)) {
		return true
	}
	if snap.Instance != "" && strings.Contains(strings.ToLower(act.ObjectName), strings.ToLower(snap.Instance)) {
		return true
	}
	if snap.Template != "" && strings.Contains(strings.ToLower(act.ObjectName), strings.ToLower(snap.Template)) {
		return true
	}
	if snap.ID != "" && strings.Contains(strings.ToLower(act.ObjectID), strings.ToLower(snap.ID)) {
		return true
	}
	return false
}

func FetchSnapshotsForWorkload(ctx context.Context, client *http.Client, endpoint, workloadID string) ([]SnapshotGQLNode, error) {
	const queryTemplate = `query OpsctlSnapshotsForWorkload { snapshotOfASnappableConnection(first: 100, after: %s, workloadId: %q) { nodes { __typename id date expirationDate isOnDemand: isOnDemandSnapshot ... on CdmSnapshot { cloudState slaDomain { id name } localLocations { id name clusterUuid type isActive } archivalLocations { id name clusterUuid type isActive } replicationLocations { id name clusterUuid type isActive } } } pageInfo { endCursor hasNextPage } } }`
	snapshots := make([]SnapshotGQLNode, 0)
	var after *string
	seen := make(map[string]bool)
	for {
		afterValue := "null"
		if after != nil {
			afterValue = fmt.Sprintf("%q", *after)
		}
		query := fmt.Sprintf(queryTemplate, afterValue, workloadID)
		var response struct {
			Data struct {
				Snapshots *struct {
					Nodes    []SnapshotGQLNode `json:"nodes"`
					PageInfo *struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage *bool   `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"snapshotOfASnappableConnection"`
			} `json:"data"`
		}
		if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
			return nil, err
		}
		if response.Data.Snapshots == nil || response.Data.Snapshots.Nodes == nil || response.Data.Snapshots.PageInfo == nil || response.Data.Snapshots.PageInfo.HasNextPage == nil {
			return nil, fmt.Errorf("RSC workload snapshot response is incomplete")
		}
		for _, node := range response.Data.Snapshots.Nodes {
			node.IsLocal = node.IsLocal || len(node.LocalLocations) > 0
			node.IsArchived = node.IsArchived || len(node.ArchivalLocations) > 0
			node.IsReplica = node.IsReplica || len(node.ReplicationLocations) > 0
			if len(node.ArchivalLocationInfo) == 0 {
				for _, location := range node.ArchivalLocations {
					node.ArchivalLocationInfo = append(node.ArchivalLocationInfo, SnapshotGQLArchivalLocation{
						Name: location.Name, LocationID: location.ID,
					})
				}
			}
			snapshots = append(snapshots, node)
		}
		if !*response.Data.Snapshots.PageInfo.HasNextPage {
			break
		}
		cursor := response.Data.Snapshots.PageInfo.EndCursor
		if cursor == nil || *cursor == "" || len(*cursor) > 4096 || seen[*cursor] {
			return nil, fmt.Errorf("RSC workload snapshot pagination did not advance")
		}
		seen[*cursor] = true
		after = cursor
	}
	return snapshots, nil
}

func GetSnapshotSize(node SnapshotGQLNode) int64 {
	if node.Size != nil && *node.Size > 0 {
		return *node.Size
	}
	if node.SnapshotDiskSize != nil && *node.SnapshotDiskSize > 0 {
		return *node.SnapshotDiskSize
	}
	if node.Size != nil {
		return *node.Size
	}
	if node.SnapshotDiskSize != nil {
		return *node.SnapshotDiskSize
	}
	return 0
}

func DetermineSnapshotStorage(node SnapshotGQLNode) string {
	cloudState := strings.ToUpper(strings.TrimSpace(node.CloudState))
	var locStatuses []string
	for _, loc := range node.ArchivalLocationInfo {
		if s := strings.TrimSpace(loc.Status); s != "" {
			locStatuses = append(locStatuses, strings.ToLower(s))
		}
		if s := strings.TrimSpace(loc.TieringState); s != "" {
			locStatuses = append(locStatuses, strings.ToLower(s))
		}
	}

	if cloudState == "MIGRATING" || cloudState == "TIERING" || cloudState == "TIER_IN_PROGRESS" {
		return "migrating"
	}
	for _, s := range locStatuses {
		if strings.Contains(s, "migrat") || strings.Contains(s, "tier") || strings.Contains(s, "progress") {
			return "migrating"
		}
	}

	isArchived := node.IsArchived || cloudState == "ARCHIVED" || cloudState == "TIERED" || len(node.ArchivalLocationInfo) > 0 || len(node.ArchivalLocations) > 0
	isLocal := node.IsLocal || len(node.LocalLocations) > 0

	var locations []string
	if isLocal {
		locations = append(locations, "rubrik")
	}
	if isArchived {
		locations = append(locations, "archiv")
	}
	if node.IsReplica || len(node.ReplicationLocations) > 0 {
		locations = append(locations, "replica")
	}
	if len(locations) > 0 {
		return strings.Join(locations, ", ")
	}
	if node.SnapshotType != "" || node.LocalLocations != nil || node.ArchivalLocations != nil || node.ReplicationLocations != nil {
		return "unknown"
	}
	return "rubrik"
}

func ParseSnapshotDate(val any) (time.Time, error) {
	if val == nil {
		return time.Time{}, nil
	}
	switch v := val.(type) {
	case time.Time:
		return v, nil
	case string:
		v = strings.TrimSpace(v)
		if v == "" || v == "-" {
			return time.Time{}, nil
		}
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05.999999999Z07:00",
			"2006-01-02T15:04:05.999999999",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02 15:04",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, v); err == nil {
				return t, nil
			}
			if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
				return t, nil
			}
		}
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			if sec > 1e11 {
				return time.UnixMilli(sec), nil
			}
			return time.Unix(sec, 0), nil
		}
		return time.Time{}, fmt.Errorf("unrecognized date format: %q", v)
	case float64:
		sec := int64(v)
		if sec > 1e11 {
			return time.UnixMilli(sec), nil
		}
		return time.Unix(sec, 0), nil
	case int64:
		if v > 1e11 {
			return time.UnixMilli(v), nil
		}
		return time.Unix(v, 0), nil
	case int:
		iv := int64(v)
		if iv > 1e11 {
			return time.UnixMilli(iv), nil
		}
		return time.Unix(iv, 0), nil
	default:
		return time.Time{}, nil
	}
}

func ParseDuration(val any) time.Duration {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case time.Duration:
		return v
	case float64:
		if v > 1e11 {
			return time.Duration(v)
		}
		if v > 100000 {
			return time.Duration(v * float64(time.Millisecond))
		}
		return time.Duration(v * float64(time.Second))
	case int64:
		if v > 1e11 {
			return time.Duration(v)
		}
		if v > 100000 {
			return time.Duration(v) * time.Millisecond
		}
		return time.Duration(v) * time.Second
	case int:
		iv := int64(v)
		if iv > 1e11 {
			return time.Duration(iv)
		}
		if iv > 100000 {
			return time.Duration(iv) * time.Millisecond
		}
		return time.Duration(iv) * time.Second
	case string:
		v = strings.TrimSpace(v)
		if v == "" || v == "-" {
			return 0
		}
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if sec, err := strconv.ParseFloat(v, 64); err == nil {
			return ParseDuration(sec)
		}
	}
	return 0
}

var TimeframeRegex = regexp.MustCompile(`^([+-]?\d+)\s*([a-zA-Z]+)?(?:\s+ago)?$`)

func ParseTimeframe(val string, now time.Time, isEnd bool) (time.Time, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return time.Time{}, nil
	}

	lower := strings.ToLower(val)
	if lower == "today" {
		if isEnd {
			return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, now.Location()), nil
		}
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	}
	if lower == "yesterday" {
		yesterday := now.AddDate(0, 0, -1)
		if isEnd {
			return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 999999999, now.Location()), nil
		}
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location()), nil
	}

	if match := TimeframeRegex.FindStringSubmatch(lower); len(match) == 3 {
		n, err := strconv.Atoi(match[1])
		if err == nil {
			if n < 0 {
				n = -n
			}
			unit := match[2]
			switch unit {
			case "d", "day", "days":
				return now.AddDate(0, 0, -n), nil
			case "w", "week", "weeks":
				return now.AddDate(0, 0, -n*7), nil
			case "mo", "month", "months":
				return now.AddDate(0, -n, 0), nil
			case "y", "yr", "year", "years":
				return now.AddDate(-n, 0, 0), nil
			case "h", "hr", "hour", "hours":
				return now.Add(-time.Duration(n) * time.Hour), nil
			case "m", "min", "mins", "minute", "minutes":
				return now.Add(-time.Duration(n) * time.Minute), nil
			case "s", "sec", "secs", "second", "seconds":
				return now.Add(-time.Duration(n) * time.Second), nil
			}
		}
	}

	cleanDur := strings.TrimSuffix(strings.TrimPrefix(lower, "-"), " ago")
	if dur, err := time.ParseDuration(cleanDur); err == nil {
		return now.Add(-dur), nil
	}

	dateOnlyLayouts := []string{
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
		"02-Jan-2006",
		"2006.01.02",
	}
	for _, layout := range dateOnlyLayouts {
		if t, err := time.Parse(layout, val); err == nil {
			if isEnd {
				return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC), nil
			}
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
		if t, err := time.ParseInLocation(layout, val, now.Location()); err == nil {
			if isEnd {
				return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, now.Location()), nil
			}
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location()), nil
		}
	}

	dateTimeLayouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
	}
	for _, layout := range dateTimeLayouts {
		if t, err := time.Parse(layout, val); err == nil {
			return t, nil
		}
		if t, err := time.ParseInLocation(layout, val, now.Location()); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timeframe %q (accepted formats: 7d, 24h, 2w, 30d, 2026-08-01, 2026-08-01 15:04:05)", val)
}

func EnrichSnapshotsNASSources(rows []SnapshotRow, sources []NASSource) []SnapshotRow {
	index := make(map[string]NASSourceMatch)
	for _, source := range sources {
		AddNASSource(index, source)
	}
	for i := range rows {
		if rows[i].TargetHost != "" && rows[i].Path != "" {
			continue
		}
		if source, ok := LookupNASSource(index, NASSourceKeys(rows[i].Instance, rows[i].Template, rows[i].Path)); ok {
			if rows[i].TargetHost == "" {
				rows[i].TargetHost = source.HostAddress
			}
			if rows[i].ShareType == "" {
				rows[i].ShareType = source.ShareType
			}
			if rows[i].Path == "" {
				rows[i].Path = source.ExportPoint
			}
		}
	}
	return rows
}

func MatchesSnapshotSearchTerm(row SnapshotRow, term string) bool {
	term = strings.TrimSpace(term)
	if term == "" || term == "*" || term == "%" {
		return true
	}
	pattern := normalizeWildcardFilter(term)
	fields := []string{
		row.Path,
		row.Volume,
		row.SVM,
		row.TargetHost,
		row.Template,
		row.Instance,
		row.SLAName,
		row.ConfiguredSLAName,
		row.EffectiveSLAName,
		row.ID,
		row.Storage,
		row.ShareType,
	}
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			continue
		}
		if globMatch(pattern, field) {
			return true
		}
	}
	if row.Path != "" {
		if globMatch(pattern, path.Base(row.Path)) {
			return true
		}
		for _, segment := range strings.Split(row.Path, "/") {
			if segment != "" && globMatch(pattern, segment) {
				return true
			}
		}
	}
	return false
}

func FilterSnapshots(
	rows []SnapshotRow,
	searchTerm string,
	slaFilter string,
	volumeFilter string,
	svmFilter string,
	since time.Time,
	till time.Time,
	typeFilters []string,
	pathFilter string,
	storageFilter string,
	clusterFilter string,
) []SnapshotRow {
	var typePatterns []string
	for _, t := range typeFilters {
		typePatterns = append(typePatterns, normalizeWildcardFilter(t))
	}
	slaFilter = strings.TrimSpace(slaFilter)
	volumeFilter = strings.TrimSpace(volumeFilter)
	svmFilter = strings.TrimSpace(svmFilter)
	pathFilter = strings.TrimSpace(pathFilter)
	storageFilter = strings.TrimSpace(storageFilter)
	clusterFilter = strings.TrimSpace(clusterFilter)

	result := make([]SnapshotRow, 0, len(rows))
	for _, row := range rows {
		if !MatchesSnapshotSearchTerm(row, searchTerm) {
			continue
		}
		if slaFilter != "" {
			pattern := normalizeWildcardFilter(slaFilter)
			if !globMatch(pattern, row.SLAName) &&
				!globMatch(pattern, row.EffectiveSLAName) &&
				!globMatch(pattern, row.ConfiguredSLAName) &&
				!globMatch(pattern, row.ID) {
				continue
			}
		}
		if volumeFilter != "" {
			pattern := normalizeWildcardFilter(volumeFilter)
			if !globMatch(pattern, row.Volume) &&
				!globMatch(pattern, path.Base(row.Path)) &&
				!strings.Contains(strings.ToLower(row.Path), strings.ToLower(volumeFilter)) {
				continue
			}
		}
		if svmFilter != "" {
			if !globMatch(normalizeWildcardFilter(svmFilter), row.SVM) {
				continue
			}
		}
		if len(typePatterns) > 0 {
			matched := false
			for _, pattern := range typePatterns {
				if globMatch(pattern, row.ShareType) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if pathFilter != "" {
			if !globMatch(normalizeWildcardFilter(pathFilter), row.Path) {
				continue
			}
		}
		if storageFilter != "" {
			if !globMatch(normalizeWildcardFilter(storageFilter), row.Storage) {
				continue
			}
		}
		if clusterFilter != "" && clusterFilter != "%" && clusterFilter != "*" {
			if !strings.EqualFold(strings.TrimSpace(row.Cluster), clusterFilter) {
				continue
			}
		}
		if !since.IsZero() && row.Date.Before(since) {
			continue
		}
		if !till.IsZero() && row.Date.After(till) {
			continue
		}
		result = append(result, row)
	}
	return result
}

func SortSnapshots(rows []SnapshotRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		pathI, pathJ := strings.ToLower(strings.TrimSpace(rows[i].Path)), strings.ToLower(strings.TrimSpace(rows[j].Path))
		if pathI != pathJ {
			return pathI < pathJ
		}
		if !rows[i].Date.Equal(rows[j].Date) {
			return rows[i].Date.After(rows[j].Date)
		}
		return rows[i].ID < rows[j].ID
	})
}
