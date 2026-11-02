package rsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type SLADuration struct {
	Duration int    `json:"duration"`
	Unit     string `json:"unit"`
}

type SLABackupWindow struct {
	DurationInHours int `json:"durationInHours"`
	Start           struct {
		Hour   int `json:"hour"`
		Minute int `json:"minute"`
	} `json:"startTimeAttributes"`
}

type SLADetailArchivalSpec struct {
	Threshold     int      `json:"threshold"`
	ThresholdUnit string   `json:"thresholdUnit"`
	Frequencies   []string `json:"frequencies"`
	LocationName  string   `json:"archivalLocationName"`
	LocationID    string   `json:"archivalLocationId"`
	Storage       struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		GroupType  string `json:"groupType"`
		TargetType string `json:"targetType"`
	} `json:"storageSetting"`
	Tier struct {
		ColdStorageClass            string `json:"coldStorageClass"`
		ShouldTierExistingSnapshots bool   `json:"shouldTierExistingSnapshots"`
		MinAccessibleDuration       int    `json:"minAccessibleDurationInSeconds"`
		InstantTiering              bool   `json:"isInstantTieringEnabled"`
	} `json:"archivalTieringSpec"`
}

type SLADetailReplication struct {
	Retention SLADuration `json:"retentionDuration"`
	Cluster   struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"cluster"`
	TargetMapping struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"targetMapping"`
	AWSTarget struct {
		AccountID   string `json:"accountId"`
		AccountName string `json:"accountName"`
		Region      string `json:"region"`
	} `json:"awsTarget"`
	AzureTarget struct {
		Region string `json:"region"`
	} `json:"azureTarget"`
}

type SLADetail struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	Type              string   `json:"__typename"`
	CDMID             string   `json:"cdmId"`
	Description       string   `json:"description"`
	IsDefault         bool     `json:"isDefault"`
	IsRetentionLocked bool     `json:"isRetentionLockedSla"`
	RetentionLockMode string   `json:"retentionLockMode"`
	ObjectTypes       []string `json:"objectTypes"`
	OwnerOrg          struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"ownerOrg"`
	Cluster struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"cluster"`
	BaseFrequency       SLADuration             `json:"baseFrequency"`
	SnapshotSchedule    SLASnapshotSchedule     `json:"snapshotSchedule"`
	LocalRetentionLimit SLADuration             `json:"localRetentionLimit"`
	BackupWindows       []SLABackupWindow       `json:"backupWindows"`
	FirstFullBackup     []SLABackupWindow       `json:"firstFullBackupWindows"`
	ArchivalSpecs       []SLADetailArchivalSpec `json:"archivalSpecs"`
	ArchivalSpec        SLADetailArchivalSpec   `json:"archivalSpec"`
	ReplicationSpecs    []SLADetailReplication  `json:"replicationSpecsV2"`
}

type SLAAssignment struct {
	Kind              string
	Template          string
	Instance          string
	Include           string
	TargetHost        string
	Path              string
	Host              string
	ShareType         string
	Cluster           string
	SLAID             string
	SLAName           string
	ConfiguredSLAName string
	EffectiveSLAName  string
	SVM               string
	Volume            string
}

type NASSource struct {
	Name        string
	HostAddress string
	ExportPoint string
	ShareType   string
}

type NASSourceMatch struct {
	source    NASSource
	ambiguous bool
}

func AddNASSource(index map[string]NASSourceMatch, source NASSource) {
	AddNASSourceKeys(index, source, NASSourceKeys(source.Name, source.ExportPoint))
}

func AddNASSourceKeys(index map[string]NASSourceMatch, source NASSource, keys []string) {
	for _, key := range keys {
		if existing, exists := index[key]; exists {
			if !strings.EqualFold(existing.source.HostAddress, source.HostAddress) || !strings.EqualFold(existing.source.ExportPoint, source.ExportPoint) || !strings.EqualFold(existing.source.ShareType, source.ShareType) {
				existing.ambiguous = true
				index[key] = existing
			}
		} else {
			index[key] = NASSourceMatch{source: source}
		}
	}
}

func LookupNASSource(index map[string]NASSourceMatch, keys []string) (NASSource, bool) {
	for _, key := range keys {
		if match, ok := index[key]; ok && !match.ambiguous {
			return match.source, true
		}
	}
	return NASSource{}, false
}

func FormatDuration(duration int, unit string) string {
	if duration == 0 && strings.TrimSpace(unit) == "" {
		return "unknown"
	}
	return fmt.Sprintf("%d %s", duration, strings.TrimSpace(unit))
}

func FetchSLADetail(ctx context.Context, client *http.Client, endpoint, id string) (SLADetail, error) {
	query := fmt.Sprintf(`query OpsctlSLAShow { slaDomain(id: %q) { id name version __typename ... on GlobalSlaReply { description isDefault isRetentionLockedSla retentionLockMode objectTypes ownerOrg { id name } baseFrequency { duration unit } snapshotSchedule { minute { basicSchedule { frequency retention retentionUnit } } hourly { basicSchedule { frequency retention retentionUnit } } daily { basicSchedule { frequency retention retentionUnit } } weekly { basicSchedule { frequency retention retentionUnit } } monthly { basicSchedule { frequency retention retentionUnit } } quarterly { basicSchedule { frequency retention retentionUnit } } yearly { basicSchedule { frequency retention retentionUnit } } } localRetentionLimit { duration unit } backupWindows { durationInHours startTimeAttributes { hour minute } } firstFullBackupWindows { durationInHours startTimeAttributes { hour minute } } archivalSpecs { threshold thresholdUnit frequencies storageSetting { id name groupType targetType } archivalTieringSpec { coldStorageClass shouldTierExistingSnapshots minAccessibleDurationInSeconds isInstantTieringEnabled } } replicationSpecsV2 { retentionDuration { duration unit } cluster { id name version } targetMapping { id name } awsTarget { accountId accountName region } azureTarget { region } } } ... on ClusterSlaDomain { cdmId cluster { id name version } ownerOrg { id name } isRetentionLockedSla retentionLockMode baseFrequency { duration unit } snapshotSchedule { minute { basicSchedule { frequency retention retentionUnit } } hourly { basicSchedule { frequency retention retentionUnit } } daily { basicSchedule { frequency retention retentionUnit } } weekly { basicSchedule { frequency retention retentionUnit } } monthly { basicSchedule { frequency retention retentionUnit } } quarterly { basicSchedule { frequency retention retentionUnit } } yearly { basicSchedule { frequency retention retentionUnit } } } localRetentionLimit { duration unit } backupWindows { durationInHours startTimeAttributes { hour minute } } firstFullBackupWindows { durationInHours startTimeAttributes { hour minute } } archivalSpec { archivalLocationName archivalLocationId threshold thresholdUnit archivalTieringSpec { coldStorageClass shouldTierExistingSnapshots minAccessibleDurationInSeconds isInstantTieringEnabled } } archivalSpecs { archivalLocationName archivalLocationId threshold thresholdUnit frequencies archivalTieringSpec { coldStorageClass shouldTierExistingSnapshots minAccessibleDurationInSeconds isInstantTieringEnabled } } replicationSpecsV2 { retentionDuration { duration unit } cluster { id name version } targetMapping { id name } awsTarget { accountId accountName region } azureTarget { region } } } } }`, id)
	var response struct {
		Data struct {
			SLADomain *SLADetail `json:"slaDomain"`
		} `json:"data"`
		Errors []GraphQLError `json:"errors"`
	}
	if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
		return SLADetail{}, err
	}
	if response.Data.SLADomain == nil {
		return SLADetail{}, fmt.Errorf("RSC SLA %q was not returned", id)
	}
	return *response.Data.SLADomain, nil
}

func ExecuteSLAGraphQL(ctx context.Context, client *http.Client, endpoint, query string, result any) error {
	_, _, err := request(ctx, client, endpoint, query, nil, result, 3)
	return err
}

func SLARetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func FetchSLAAssignments(ctx context.Context, client *http.Client, endpoint, slaID, slaName string) ([]SLAAssignment, error) {
	const queryTemplate = `query OpsctlSLAAssignments { filesetTemplates(hostRoot: %s, first: 100, after: %s) { nodes { id name includes excludes osType shareType cluster { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } descendantConnection { nodes { id name physicalPath { name fid } configuredSlaDomain { id name } effectiveSlaDomain { id name } cluster { name } } } } pageInfo { endCursor hasNextPage } } }`
	var assignments []SLAAssignment
	nasSources := make(map[string]NASSourceMatch)
	for _, root := range []string{"WINDOWS_HOST_ROOT", "LINUX_HOST_ROOT", "NAS_HOST_ROOT"} {
		var cursor string
		for {
			after := "null"
			if cursor != "" {
				cursorBytes, err := json.Marshal(cursor)
				if err != nil {
					return nil, fmt.Errorf("marshal RSC fileset cursor: %w", err)
				}
				after = string(cursorBytes)
			}
			query := fmt.Sprintf(queryTemplate, root, after)
			var response struct {
				Data struct {
					FilesetTemplates struct {
						Nodes []struct {
							ID           string   `json:"id"`
							Name         string   `json:"name"`
							Includes     []string `json:"includes"`
							EffectiveSLA struct {
								ID   string `json:"id"`
								Name string `json:"name"`
							} `json:"effectiveSlaDomain"`
							ConfiguredSLA struct {
								ID   string `json:"id"`
								Name string `json:"name"`
							} `json:"configuredSlaDomain"`
							DescendantConnection struct {
								Nodes []struct {
									ID           string `json:"id"`
									Name         string `json:"name"`
									PhysicalPath []struct {
										Name string `json:"name"`
										FID  string `json:"fid"`
									} `json:"physicalPath"`
									EffectiveSLA struct {
										ID   string `json:"id"`
										Name string `json:"name"`
									} `json:"effectiveSlaDomain"`
									ConfiguredSLA struct {
										ID   string `json:"id"`
										Name string `json:"name"`
									} `json:"configuredSlaDomain"`
									Cluster struct {
										Name string `json:"name"`
									} `json:"cluster"`
								} `json:"nodes"`
							} `json:"descendantConnection"`
							Cluster struct {
								Name string `json:"name"`
							} `json:"cluster"`
						} `json:"nodes"`
						PageInfo struct {
							EndCursor   string `json:"endCursor"`
							HasNextPage bool   `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"filesetTemplates"`
				} `json:"data"`
			}
			if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
				if IsNotFoundError(err) {
					logf(ctx, 2, "rsc sla: fileset source type %s not found: %v", root, err)
					break
				}
				return nil, err
			}
			for _, template := range response.Data.FilesetTemplates.Nodes {
				for _, instance := range template.DescendantConnection.Nodes {
					effectiveSLA := instance.EffectiveSLA
					if instance.ConfiguredSLA.ID == slaID {
						effectiveSLA = instance.ConfiguredSLA
					}
					if effectiveSLA.ID == "" || effectiveSLA.ID == "UNPROTECTED" {
						effectiveSLA = template.EffectiveSLA
					}
					if template.ConfiguredSLA.ID == slaID {
						effectiveSLA = template.ConfiguredSLA
					}
					if effectiveSLA.ID != slaID {
						continue
					}
					cluster := instance.Cluster.Name
					if cluster == "" {
						cluster = template.Cluster.Name
					}
					paths := make([]string, 0, len(instance.PhysicalPath))
					for _, path := range instance.PhysicalPath {
						if name := strings.TrimSpace(path.Name); name != "" {
							paths = append(paths, name)
						}
					}
					if len(paths) == 0 {
						paths = append(paths, template.Includes...)
					}
					assignments = append(assignments, SLAAssignment{Kind: root, Template: template.Name, Instance: instance.Name, Path: strings.Join(paths, ", "), ShareType: root, Cluster: cluster, SLAID: effectiveSLA.ID, SLAName: effectiveSLA.Name, ConfiguredSLAName: instance.ConfiguredSLA.Name, EffectiveSLAName: effectiveSLA.Name})
				}
			}
			if !response.Data.FilesetTemplates.PageInfo.HasNextPage || response.Data.FilesetTemplates.PageInfo.EndCursor == "" {
				break
			}
			cursor = response.Data.FilesetTemplates.PageInfo.EndCursor
		}
	}

	slaIDs, err := json.Marshal([]string{slaID})
	if err != nil {
		return nil, fmt.Errorf("marshal RSC SLA ID: %w", err)
	}
	const protectedObjectsQueryTemplate = `query OpsctlRSCProtectedObjects { protectedObjectsConnection(slaIds: %s, first: 100, after: %s) { nodes { id name objectType effectiveSlaFidOpt effectiveSlaOpt isArchived isUnprotected cluster { name } } pageInfo { endCursor hasNextPage } } }`
	var protectedCursor string
	for {
		after := "null"
		if protectedCursor != "" {
			cursorBytes, err := json.Marshal(protectedCursor)
			if err != nil {
				return nil, fmt.Errorf("marshal RSC protected-object cursor: %w", err)
			}
			after = string(cursorBytes)
		}
		query := fmt.Sprintf(protectedObjectsQueryTemplate, string(slaIDs), after)
		var response struct {
			Data struct {
				ProtectedObjects struct {
					Nodes []struct {
						ID             string `json:"id"`
						Name           string `json:"name"`
						ObjectType     string `json:"objectType"`
						EffectiveSLAID string `json:"effectiveSlaFidOpt"`
						EffectiveSLA   string `json:"effectiveSlaOpt"`
						IsArchived     bool   `json:"isArchived"`
						IsUnprotected  bool   `json:"isUnprotected"`
						Cluster        struct {
							Name string `json:"name"`
						} `json:"cluster"`
					} `json:"nodes"`
					PageInfo struct {
						EndCursor   string `json:"endCursor"`
						HasNextPage bool   `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"protectedObjectsConnection"`
			} `json:"data"`
		}
		if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &response); err != nil {
			if IsNotFoundError(err) {
				logf(ctx, 2, "rsc sla: protected objects unavailable: %v", err)
				break
			}
			return nil, err
		}
		for _, object := range response.Data.ProtectedObjects.Nodes {
			if object.IsArchived || object.IsUnprotected || object.EffectiveSLAID != slaID {
				continue
			}
			path := strings.TrimSpace(object.ID)
			if strings.Contains(strings.ToUpper(object.ObjectType), "NAS") {
				path = strings.TrimSpace(object.Name)
			}
			assignments = append(assignments, SLAAssignment{
				Kind:     strings.TrimSpace(object.ObjectType),
				Instance: strings.TrimSpace(object.Name),
				Path:     path,
				Cluster:  strings.TrimSpace(object.Cluster.Name),
				SLAID:    strings.TrimSpace(object.EffectiveSLAID),
				SLAName:  strings.TrimSpace(object.EffectiveSLA),
			})
		}
		if !response.Data.ProtectedObjects.PageInfo.HasNextPage || response.Data.ProtectedObjects.PageInfo.EndCursor == "" {
			break
		}
		protectedCursor = response.Data.ProtectedObjects.PageInfo.EndCursor
	}

	const nasQueryTemplate = `query OpsctlSLANASShares { nasShares(first: 100, after: %s) { nodes { name id exportPoint hostAddress shareType nasSystem { name } primaryFileset { name } cluster { name } configuredSlaDomain { id name } effectiveSlaDomain { id name } } pageInfo { endCursor hasNextPage } } }`
	var nasCursor string
	for {
		nasAfter := "null"
		if nasCursor != "" {
			nasAfterBytes, err := json.Marshal(nasCursor)
			if err != nil {
				return nil, fmt.Errorf("marshal RSC NAS cursor: %w", err)
			}
			nasAfter = string(nasAfterBytes)
		}
		query := fmt.Sprintf(nasQueryTemplate, nasAfter)
		var nasResponse struct {
			Data struct {
				NASShares struct {
					Nodes []struct {
						Name        string `json:"name"`
						ExportPoint string `json:"exportPoint"`
						HostAddress string `json:"hostAddress"`
						ShareType   string `json:"shareType"`
						NASSystem   struct {
							Name string `json:"name"`
						} `json:"nasSystem"`
						PrimaryFileset struct {
							Name string `json:"name"`
						} `json:"primaryFileset"`
						Cluster struct {
							Name string `json:"name"`
						} `json:"cluster"`
						EffectiveSLA struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"effectiveSlaDomain"`
						ConfiguredSLA struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"configuredSlaDomain"`
					} `json:"nodes"`
					PageInfo struct {
						EndCursor   string `json:"endCursor"`
						HasNextPage bool   `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"nasShares"`
			} `json:"data"`
		}
		if err := ExecuteSLAGraphQL(ctx, client, endpoint, query, &nasResponse); err != nil {
			if IsNotFoundError(err) {
				logf(ctx, 2, "rsc sla: NAS shares unavailable: %v", err)
				break
			}
			return nil, err
		}
		for _, share := range nasResponse.Data.NASShares.Nodes {
			source := NASSource{
				Name:        strings.TrimSpace(share.Name),
				HostAddress: strings.TrimSpace(share.HostAddress),
				ExportPoint: strings.TrimSpace(share.ExportPoint),
				ShareType:   strings.TrimSpace(share.ShareType),
			}
			AddNASSource(nasSources, source)
			if primaryFileset := strings.TrimSpace(share.PrimaryFileset.Name); primaryFileset != "" {
				AddNASSourceKeys(nasSources, source, NASSourceKeys(primaryFileset))
			}
			sla := share.EffectiveSLA
			if share.ConfiguredSLA.ID == slaID {
				sla = share.ConfiguredSLA
			}
			if !MatchesSLAReference(sla.ID, sla.Name, slaID, slaName) {
				continue
			}
			path := strings.TrimSpace(share.ExportPoint)
			if path == "" {
				path = strings.TrimSpace(share.HostAddress)
			}
			template := strings.TrimSpace(share.PrimaryFileset.Name)
			if template == "" {
				template = strings.TrimSpace(share.NASSystem.Name)
			}
			assignments = append(assignments, SLAAssignment{
				Kind:              "NAS_SHARE/" + strings.TrimSpace(share.ShareType),
				Template:          template,
				Instance:          strings.TrimSpace(share.Name),
				Path:              path,
				Host:              strings.TrimSpace(share.HostAddress),
				ShareType:         strings.TrimSpace(share.ShareType),
				Cluster:           strings.TrimSpace(share.Cluster.Name),
				SLAID:             strings.TrimSpace(sla.ID),
				SLAName:           strings.TrimSpace(sla.Name),
				ConfiguredSLAName: strings.TrimSpace(share.ConfiguredSLA.Name),
				EffectiveSLAName:  strings.TrimSpace(sla.Name),
			})
		}
		if !nasResponse.Data.NASShares.PageInfo.HasNextPage || nasResponse.Data.NASShares.PageInfo.EndCursor == "" {
			break
		}
		nasCursor = nasResponse.Data.NASShares.PageInfo.EndCursor
	}

	const nasObjectQuery = `query OpsctlSLANASObjects { nasSystems(first: 100) { nodes { name id vendorType shareCount namespaceCount cluster { name } effectiveSlaDomain { id name } } } nasNamespaces(first: 100) { nodes { name id vendorType cluster { name } effectiveSlaDomain { id name } } } }`
	var nasObjectResponse struct {
		Data struct {
			NASSystems struct {
				Nodes []struct {
					Name           string `json:"name"`
					VendorType     string `json:"vendorType"`
					ShareCount     int    `json:"shareCount"`
					NamespaceCount int    `json:"namespaceCount"`
					Cluster        struct {
						Name string `json:"name"`
					} `json:"cluster"`
					EffectiveSLA struct{ ID, Name string } `json:"effectiveSlaDomain"`
				} `json:"nodes"`
			} `json:"nasSystems"`
			NASNamespaces struct {
				Nodes []struct {
					Name       string `json:"name"`
					VendorType string `json:"vendorType"`
					Cluster    struct {
						Name string `json:"name"`
					} `json:"cluster"`
					EffectiveSLA struct{ ID, Name string } `json:"effectiveSlaDomain"`
				} `json:"nodes"`
			} `json:"nasNamespaces"`
		} `json:"data"`
	}
	if err := ExecuteSLAGraphQL(ctx, client, endpoint, nasObjectQuery, &nasObjectResponse); err != nil {
		if IsNotFoundError(err) {
			logf(ctx, 2, "rsc sla: NAS systems or namespaces unavailable: %v", err)
			return assignments, nil
		}
		return nil, err
	}
	for _, system := range nasObjectResponse.Data.NASSystems.Nodes {
		if MatchesSLAReference(system.EffectiveSLA.ID, system.EffectiveSLA.Name, slaID, slaName) {
			assignments = append(assignments, SLAAssignment{
				Kind:             "NAS_SYSTEM/" + strings.TrimSpace(system.VendorType),
				Instance:         strings.TrimSpace(system.Name),
				Path:             fmt.Sprintf("%d shares, %d namespaces", system.ShareCount, system.NamespaceCount),
				ShareType:        strings.TrimSpace(system.VendorType),
				Cluster:          strings.TrimSpace(system.Cluster.Name),
				SLAID:            strings.TrimSpace(system.EffectiveSLA.ID),
				SLAName:          strings.TrimSpace(system.EffectiveSLA.Name),
				EffectiveSLAName: strings.TrimSpace(system.EffectiveSLA.Name),
			})
		}
	}
	for _, namespace := range nasObjectResponse.Data.NASNamespaces.Nodes {
		if MatchesSLAReference(namespace.EffectiveSLA.ID, namespace.EffectiveSLA.Name, slaID, slaName) {
			assignments = append(assignments, SLAAssignment{
				Kind:             "NAS_NAMESPACE/" + strings.TrimSpace(namespace.VendorType),
				Instance:         strings.TrimSpace(namespace.Name),
				Path:             strings.TrimSpace(namespace.Name),
				ShareType:        strings.TrimSpace(namespace.VendorType),
				Cluster:          strings.TrimSpace(namespace.Cluster.Name),
				SLAID:            strings.TrimSpace(namespace.EffectiveSLA.ID),
				SLAName:          strings.TrimSpace(namespace.EffectiveSLA.Name),
				EffectiveSLAName: strings.TrimSpace(namespace.EffectiveSLA.Name),
			})
		}
	}
	for index := range assignments {
		if assignments[index].Host != "" || !strings.EqualFold(assignments[index].Kind, "NAS_FILESET") {
			continue
		}
		if source, ok := LookupNASSource(nasSources, NASSourceLookupKeys(assignments[index])); ok {
			assignments[index].Host = source.HostAddress
			assignments[index].ShareType = source.ShareType
			if assignments[index].Path == "" {
				assignments[index].Path = source.ExportPoint
			}
		}
	}
	return assignments, nil
}

func NASSourceLookupKeys(assignment SLAAssignment) []string {
	values := []string{assignment.Instance, assignment.Template, assignment.Path}
	return NASSourceKeys(values...)
}

func NASSourceKeys(values ...string) []string {
	keys := make([]string, 0, len(values)*2)
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, candidate := range []string{value, strings.TrimSpace(strings.SplitN(value, ":", 2)[0])} {
			candidate = strings.ToLower(strings.Trim(strings.TrimSpace(candidate), "/"))
			candidate = strings.TrimSpace(strings.Trim(candidate, "*"))
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				keys = append(keys, candidate)
			}
		}
	}
	return keys
}

func MatchesSLAReference(referenceID, referenceName, slaID, slaName string) bool {
	if strings.TrimSpace(referenceID) == strings.TrimSpace(slaID) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(referenceName), strings.TrimSpace(slaName))
}

func IsNotFoundError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "does not exist") || strings.Contains(message, "could not find")
}

func FilterSLAAssignments(assignments []SLAAssignment, clusterName string) []SLAAssignment {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" || clusterName == "%" {
		return assignments
	}
	filtered := make([]SLAAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		if strings.EqualFold(strings.TrimSpace(assignment.Cluster), clusterName) {
			filtered = append(filtered, assignment)
		}
	}
	return filtered
}
