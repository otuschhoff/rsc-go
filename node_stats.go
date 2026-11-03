package rsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type NodeStatsOptions struct {
	Start           time.Time
	End             time.Time
	Aggregation     string
	NodeID          string
	IncludeExtended bool
}

type NodeStats struct {
	ClusterID                     string `json:"clusterId"`
	NodeID                        string `json:"nodeId"`
	Time                          string `json:"time"`
	CPUStat                       *int64 `json:"cpuStat"`
	UsedMemoryPercent             *int   `json:"usedMemoryStat"`
	IOPSReadsPerSecond            *int64 `json:"iopsReadsPerSecond"`
	IOPSWritesPerSecond           *int64 `json:"iopsWritesPerSecond"`
	ReadThroughputBytesPerSecond  *int64 `json:"readThroughputBytesPerSecond"`
	WriteThroughputBytesPerSecond *int64 `json:"writeThroughputBytesPerSecond"`
	DiskUtilBasisPoints           *int64 `json:"diskUtilBasisPoints"`
	LoadAvg5MinMilli              *int64 `json:"loadAvg5MinMilli"`
}

type ClusterNodeStatsInfo struct {
	ClusterID   string
	ClusterName string
	Samples     []NodeStats
}

func (client *Client) NodeStats(ctx context.Context, filter string, options NodeStatsOptions) ([]ClusterNodeStatsInfo, error) {
	return FetchNodeStatsForCluster(ctx, client.httpClient, client.endpoint, filter, options)
}

func FetchNodeStatsForCluster(ctx context.Context, client *http.Client, endpoint, clusterName string, options NodeStatsOptions) ([]ClusterNodeStatsInfo, error) {
	if options.Start.IsZero() != options.End.IsZero() || (!options.Start.IsZero() && !options.Start.Before(options.End)) {
		return nil, fmt.Errorf("RSC node statistics require both start and end, with start before end")
	}
	switch options.Aggregation {
	case "", "AVERAGE", "MAX", "UNDEFINED":
	default:
		return nil, fmt.Errorf("invalid RSC node statistics aggregation %q", options.Aggregation)
	}
	variables := map[string]any{"after": nil, "timeRange": nil, "aggregation": nil, "nodeID": nil}
	if !options.Start.IsZero() {
		variables["timeRange"] = map[string]string{
			"start": options.Start.UTC().Format(time.RFC3339Nano),
			"end":   options.End.UTC().Format(time.RFC3339Nano),
		}
	}
	if options.Aggregation != "" {
		variables["aggregation"] = options.Aggregation
	}
	if options.NodeID != "" {
		variables["nodeID"] = options.NodeID
	}
	extended := ""
	if options.IncludeExtended {
		extended = "diskUtilBasisPoints loadAvg5MinMilli"
	}
	query := fmt.Sprintf(`query RSCNodeStats($after: String, $timeRange: TimeRangeInput, $aggregation: NodeStatsAggregationType, $nodeID: String) {
		clusterConnection(%sfirst: 100, after: $after) {
			nodes { id name clusterNodeStats(timeRange: $timeRange, aggregationType: $aggregation, cdmClusterNodeID: $nodeID) {
				clusterId nodeId time cpuStat usedMemoryStat iopsReadsPerSecond iopsWritesPerSecond
				readThroughputBytesPerSecond writeThroughputBytesPerSecond %s
			} }
			pageInfo { endCursor hasNextPage }
		}
	}`, ClusterFilterArgument(clusterName), extended)
	var result []ClusterNodeStatsInfo
	seen := make(map[string]bool)
	for {
		body, _, err := request(ctx, client, endpoint, query, variables, nil, 1)
		if err != nil {
			return nil, err
		}
		var response struct {
			Data struct {
				ClusterConnection struct {
					Nodes []struct {
						ID      string      `json:"id"`
						Name    string      `json:"name"`
						Samples []NodeStats `json:"clusterNodeStats"`
					} `json:"nodes"`
					PageInfo struct {
						EndCursor   *string `json:"endCursor"`
						HasNextPage bool    `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"clusterConnection"`
			} `json:"data"`
			Errors []GraphQLError `json:"errors"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse RSC node statistics response: %w", err)
		}
		if len(response.Errors) > 0 {
			return nil, fmt.Errorf("RSC GraphQL error: %s", response.Errors[0].Message)
		}
		connection := response.Data.ClusterConnection
		for _, cluster := range connection.Nodes {
			result = append(result, ClusterNodeStatsInfo{
				ClusterID: strings.TrimSpace(cluster.ID), ClusterName: strings.TrimSpace(cluster.Name), Samples: cluster.Samples,
			})
		}
		if !connection.PageInfo.HasNextPage {
			break
		}
		cursor := connection.PageInfo.EndCursor
		if cursor == nil || *cursor == "" || seen[*cursor] {
			return nil, fmt.Errorf("RSC node statistics pagination did not advance")
		}
		seen[*cursor] = true
		variables["after"] = *cursor
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("RSC node statistics query returned no clusters")
	}
	return result, nil
}
