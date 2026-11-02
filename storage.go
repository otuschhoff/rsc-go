package rsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type TargetMappingInfo struct {
	ID            string   `json:"id" yaml:"id"`
	Name          string   `json:"name" yaml:"name"`
	TargetType    string   `json:"target_type" yaml:"target_type"`
	Targets       []string `json:"targets" yaml:"targets"`
	ConsumedBytes int64    `json:"consumed_bytes,omitempty" yaml:"consumed_bytes,omitempty"`
}

func DerefInt64(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

func FetchTargetMappings(ctx context.Context, client *http.Client, endpoint string) ([]TargetMappingInfo, error) {
	const query = `query OpsctlTargetMappings { allTargetMappings { name id targetType targets { id name } } }`
	body, _, err := request(ctx, client, endpoint, query, nil, nil, 1)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data struct {
			TargetMappings []struct {
				Name       string `json:"name"`
				ID         string `json:"id"`
				TargetType string `json:"targetType"`
				Targets    []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"targets"`
			} `json:"allTargetMappings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse target mappings response: %w", err)
	}

	var results []TargetMappingInfo
	seen := make(map[string]struct{})
	for _, tm := range response.Data.TargetMappings {
		info := TargetMappingInfo{
			ID:         tm.ID,
			Name:       tm.Name,
			TargetType: tm.TargetType,
		}
		for _, t := range tm.Targets {
			if t.Name != "" {
				info.Targets = append(info.Targets, t.Name)
			}
		}
		key := strings.ToLower(tm.TargetType + "|" + strings.Join(info.Targets, ","))
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			results = append(results, info)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	return results, nil
}
