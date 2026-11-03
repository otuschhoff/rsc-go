package rsc

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// NASFilesetSource retains both share and workload identities. SLA references
// belong to the primary fileset, not the parent share.
type NASFilesetSource struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	ExportPoint    string             `json:"exportPoint"`
	HostAddress    string             `json:"hostAddress"`
	ShareType      string             `json:"shareType"`
	PrimaryFileset *NASPrimaryFileset `json:"primaryFileset"`
	Cluster        FilesetCluster     `json:"cluster"`
}

type NASPrimaryFileset struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	ConfiguredSLA FilesetSLA `json:"configuredSlaDomain"`
	EffectiveSLA  FilesetSLA `json:"effectiveSlaDomain"`
	PendingSLA    FilesetSLA `json:"pendingSla"`
}

// FetchNASFilesetSources returns the complete inventory or an error, never a
// partial inventory. Pending SLA is metadata, not evidence of active protection.
func FetchNASFilesetSources(ctx context.Context, client *http.Client, endpoint string) ([]NASFilesetSource, error) {
	const query = `query RSCNASFilesetSources($after: String) {
  nasShares(first: 100, after: $after) {
    nodes { id name exportPoint hostAddress shareType primaryFileset { id name configuredSlaDomain { id name } effectiveSlaDomain { id name } pendingSla { id name } } cluster { name } }
    pageInfo { endCursor hasNextPage }
  }
}`
	sources := []NASFilesetSource{}
	var after *string
	seen := map[string]bool{}
	for {
		var response struct {
			Data struct {
				Shares *struct {
					Nodes *[]NASFilesetSource `json:"nodes"`
					Page  *struct {
						EndCursor   string `json:"endCursor"`
						HasNextPage *bool  `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"nasShares"`
			} `json:"data"`
		}
		if _, _, err := request(ctx, client, endpoint, query, map[string]any{"after": after}, &response, 3); err != nil {
			return nil, err
		}
		shares := response.Data.Shares
		if shares == nil || shares.Nodes == nil || shares.Page == nil || shares.Page.HasNextPage == nil {
			return nil, errors.New("RSC NAS fileset source response is incomplete")
		}
		sources = append(sources, (*shares.Nodes)...)
		if !*shares.Page.HasNextPage {
			return sources, nil
		}
		next := shares.Page.EndCursor
		if strings.TrimSpace(next) == "" || seen[next] || after != nil && next == *after {
			return nil, errors.New("RSC NAS fileset source pagination did not advance")
		}
		seen[next] = true
		after = &next
	}
}

func nasFilesetSLAMatches(reference FilesetSLA, id, name string) bool {
	if strings.TrimSpace(reference.ID) != "" {
		return strings.TrimSpace(reference.ID) == strings.TrimSpace(id)
	}
	return strings.TrimSpace(reference.Name) != "" && strings.EqualFold(strings.TrimSpace(reference.Name), strings.TrimSpace(name))
}

func nasFilesetSLAName(reference FilesetSLA, id, name string) string {
	reference.ID = strings.TrimSpace(reference.ID)
	reference.Name = strings.TrimSpace(reference.Name)
	if strings.EqualFold(reference.ID, "DO_NOT_PROTECT") || strings.EqualFold(reference.ID, "UNPROTECTED") {
		return reference.ID
	}
	if reference.ID != "" && reference.ID != id && strings.EqualFold(reference.Name, name) {
		return reference.ID
	}
	if reference.Name != "" {
		return reference.Name
	}
	if reference.ID != "" && reference.ID == id {
		return name
	}
	if reference.ID != "" {
		return reference.ID
	}
	return "Unknown"
}
