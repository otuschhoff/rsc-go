package rsc

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

func FilesetValueIsUnknown(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "unknown")
}

func FetchFilesetsForSLA(ctx context.Context, client *http.Client, endpoint, filter string) ([]SLAAssignment, error) {
	policies, err := FetchSLAPolicies(ctx, client, endpoint)
	if err != nil {
		return nil, err
	}
	var assignments []SLAAssignment
	for _, policy := range policies {
		if !globMatch(filter, policy.Name) && !globMatch(filter, policy.ID) {
			continue
		}
		policyAssignments, err := FetchSLAAssignments(ctx, client, endpoint, policy.ID, policy.Name)
		if err != nil {
			return nil, err
		}
		for index := range policyAssignments {
			if strings.TrimSpace(policyAssignments[index].ConfiguredSLAName) == "" {
				policyAssignments[index].ConfiguredSLAName = policy.Name
			}
			if strings.TrimSpace(policyAssignments[index].EffectiveSLAName) == "" || LooksLikeSLAID(policyAssignments[index].EffectiveSLAName, policy.ID) {
				policyAssignments[index].EffectiveSLAName = policy.Name
			}
			policyAssignments[index].SLAName = policy.Name
		}
		assignments = append(assignments, policyAssignments...)
	}
	return assignments, nil
}

func LooksLikeSLAID(value, policyID string) bool {
	return strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(policyID))
}

func SortFilesetsByTarget(rows []SLAAssignment) {
	sort.SliceStable(rows, func(i, j int) bool {
		leftHost, rightHost := FilesetTargetHost(rows[i]), FilesetTargetHost(rows[j])
		if !strings.EqualFold(leftHost, rightHost) {
			return strings.ToLower(leftHost) < strings.ToLower(rightHost)
		}
		return strings.ToLower(strings.TrimSpace(rows[i].Path)) < strings.ToLower(strings.TrimSpace(rows[j].Path))
	})
}

func FilesetTargetHost(row SLAAssignment) string {
	if host := strings.TrimSpace(row.TargetHost); host != "" {
		return host
	}
	return strings.TrimSpace(row.Host)
}

type FilesetPath struct {
	Name string `json:"name"`
}
type FilesetSLA struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type FilesetCluster struct {
	Name string `json:"name"`
}
type FilesetDescendant struct {
	Name       string         `json:"name"`
	Paths      []FilesetPath  `json:"physicalPath"`
	Configured FilesetSLA     `json:"configuredSlaDomain"`
	Effective  FilesetSLA     `json:"effectiveSlaDomain"`
	Cluster    FilesetCluster `json:"cluster"`
}
type FilesetTemplate struct {
	Name        string         `json:"name"`
	Includes    []string       `json:"includes"`
	ShareType   string         `json:"shareType"`
	OSType      string         `json:"osType"`
	Cluster     FilesetCluster `json:"cluster"`
	Configured  FilesetSLA     `json:"configuredSlaDomain"`
	Effective   FilesetSLA     `json:"effectiveSlaDomain"`
	Descendants struct {
		Nodes    []FilesetDescendant `json:"nodes"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"descendantConnection"`
}

func FetchFilesets(ctx context.Context, client *http.Client, endpoint string) ([]SLAAssignment, error) {
	const queryTemplate = `query OpsctlFilesets { filesetTemplates(hostRoot: %s, first: 100, after: %s) { nodes { name includes shareType osType cluster { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } descendantConnection(first: 100) { nodes { name physicalPath { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } cluster { name } } pageInfo { hasNextPage } } } pageInfo { endCursor hasNextPage } } }`
	all := []SLAAssignment{}
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
						Nodes    []FilesetTemplate `json:"nodes"`
						PageInfo struct {
							EndCursor   *string `json:"endCursor"`
							HasNextPage bool    `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"filesetTemplates"`
				} `json:"data"`
			}
			if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
				return nil, err
			}
			for _, template := range response.Data.Filesets.Nodes {
				if template.Descendants.PageInfo.HasNextPage {
					return nil, fmt.Errorf("RSC fileset template %q has more than 100 descendants; pagination is required", template.Name)
				}
				for _, child := range template.Descendants.Nodes {
					include, targetHost, targetPath := SplitFilesetPhysicalPath(child.Paths, template.Includes)
					configured, effective := child.Configured, child.Effective
					if configured.ID == "" {
						configured = template.Configured
					}
					if effective.ID == "" {
						effective = template.Effective
					}
					cluster := child.Cluster.Name
					if cluster == "" {
						cluster = template.Cluster.Name
					}
					kind := template.ShareType
					if kind == "" {
						kind = template.OSType
					}
					all = append(all, SLAAssignment{Kind: root, Template: template.Name, Instance: child.Name, Include: include, TargetHost: targetHost, Path: targetPath, ShareType: kind, Cluster: cluster, SLAID: configured.ID, SLAName: effective.Name, ConfiguredSLAName: configured.Name, EffectiveSLAName: effective.Name})
				}
			}
			if !response.Data.Filesets.PageInfo.HasNextPage || response.Data.Filesets.PageInfo.EndCursor == nil {
				break
			}
			after = response.Data.Filesets.PageInfo.EndCursor
		}
	}
	return all, nil
}

func SplitFilesetPhysicalPath(paths []FilesetPath, includes []string) (string, string, string) {
	include := strings.Join(includes, ", ")
	targetHost, targetPath := "", ""
	for _, physicalPath := range paths {
		value := strings.TrimSpace(physicalPath.Name)
		if value == "" {
			continue
		}
		switch {
		case strings.HasPrefix(value, "/"):
			if targetPath == "" {
				targetPath = value
			}
		case strings.Contains(value, "."):
			if targetHost == "" {
				targetHost = value
			}
		default:
			if include == "" {
				include = value
			}
		}
	}
	return include, targetHost, targetPath
}

func FilterFilesets(rows []SLAAssignment, types []string, sla, svm string, lifs []string, volume, targetPath string, showUnknown bool) []SLAAssignment {
	patterns := make([]string, 0, len(types))
	for _, value := range types {
		patterns = append(patterns, normalizeWildcardFilter(value))
	}
	sla, svm, volume, targetPath = normalizeWildcardFilter(sla), normalizeWildcardFilter(svm), normalizeWildcardFilter(volume), normalizeWildcardFilter(targetPath)
	lifPatterns := make([]string, 0, len(lifs))
	for _, lif := range lifs {
		lifPatterns = append(lifPatterns, normalizeWildcardFilter(lif))
	}
	result := make([]SLAAssignment, 0, len(rows))
	for _, row := range rows {
		if !showUnknown && FilesetValueIsUnknown(row.ShareType) && FilesetValueIsUnknown(FilesetTargetHost(row)) {
			continue
		}
		if len(patterns) > 0 {
			matched := false
			for _, pattern := range patterns {
				if globMatch(pattern, row.ShareType) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if sla != "" && !globMatch(sla, row.SLAName) && !globMatch(sla, row.SLAID) {
			continue
		}
		if svm != "" && !globMatch(svm, row.SVM) {
			continue
		}
		if len(lifPatterns) > 0 {
			matched := false
			for _, pattern := range lifPatterns {
				if globMatch(pattern, row.TargetHost) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if volume != "" && !globMatch(volume, row.Volume) {
			continue
		}
		if targetPath != "" && !globMatch(targetPath, row.Path) {
			continue
		}
		result = append(result, row)
	}
	return result
}

func FetchNASSources(ctx context.Context, client *http.Client, endpoint string) ([]NASSource, error) {
	const queryTemplate = `query OpsctlFilesetNASShares { nasShares(first: 100, after: %s) { nodes { name exportPoint hostAddress shareType primaryFileset { name } } pageInfo { endCursor hasNextPage } } }`
	sources := []NASSource{}
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
						Name        string `json:"name"`
						ExportPoint string `json:"exportPoint"`
						HostAddress string `json:"hostAddress"`
						ShareType   string `json:"shareType"`
						Primary     struct {
							Name string `json:"name"`
						} `json:"primaryFileset"`
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
			source := NASSource{Name: strings.TrimSpace(share.Name), HostAddress: strings.TrimSpace(share.HostAddress), ExportPoint: strings.TrimSpace(share.ExportPoint), ShareType: strings.TrimSpace(share.ShareType)}
			sources = append(sources, source)
			if name := strings.TrimSpace(share.Primary.Name); name != "" {
				source.Name = name
				sources = append(sources, source)
			}
		}
		if !response.Data.Shares.PageInfo.HasNextPage || response.Data.Shares.PageInfo.EndCursor == nil {
			break
		}
		after = response.Data.Shares.PageInfo.EndCursor
	}
	return sources, nil
}

func EnrichFilesetsNASSources(rows []SLAAssignment, sources []NASSource) []SLAAssignment {
	index := make(map[string]NASSource)
	for _, source := range sources {
		for _, key := range NASSourceKeys(source.Name, source.ExportPoint) {
			if _, exists := index[key]; !exists {
				index[key] = source
			}
		}
	}
	for rowIndex := range rows {
		for _, key := range NASSourceLookupKeys(rows[rowIndex]) {
			if source, ok := index[key]; ok {
				rows[rowIndex].Host = source.HostAddress
				rows[rowIndex].ShareType = source.ShareType
				break
			}
		}
	}
	return rows
}
