package rsc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type BackupJobsOptions struct {
	First      int
	After      *string
	WorkloadID string
	ClusterID  string
	SearchTerm string
	Statuses   []string
}

type BackupJob struct {
	ActivitySeriesID string     `json:"activitySeriesId"`
	ClusterID        string     `json:"clusterUuid"`
	ClusterName      string     `json:"clusterName"`
	WorkloadID       string     `json:"fid"`
	ObjectID         string     `json:"objectId"`
	ObjectName       *string    `json:"objectName"`
	ObjectType       string     `json:"objectType"`
	Status           string     `json:"lastActivityStatus"`
	Message          *string    `json:"lastActivityMessage"`
	Progress         *string    `json:"progress"`
	StartTime        *time.Time `json:"startTime"`
	LastUpdated      time.Time  `json:"lastUpdated"`
	IsCancelable     *bool      `json:"isCancelable"`
	IsOnDemand       *bool      `json:"isOnDemand"`
}

type BackupJobPage struct {
	Jobs        []BackupJob
	EndCursor   *string
	HasNextPage bool
}

func (client *Client) BackupJobs(ctx context.Context, options BackupJobsOptions) (BackupJobPage, error) {
	return FetchBackupJobs(ctx, client.httpClient, client.endpoint, options)
}

func FetchBackupJobs(ctx context.Context, client *http.Client, endpoint string, options BackupJobsOptions) (BackupJobPage, error) {
	if options.First == 0 {
		options.First = 100
	}
	if options.First < 1 || options.First > 100 {
		return BackupJobPage{}, fmt.Errorf("RSC backup jobs page size must be between 1 and 100")
	}
	if options.After != nil && strings.TrimSpace(*options.After) == "" {
		return BackupJobPage{}, fmt.Errorf("RSC backup jobs cursor must not be empty")
	}
	filters := map[string]any{"lastActivityType": []string{"BACKUP"}}
	if options.WorkloadID != "" {
		filters["objectFid"] = []string{options.WorkloadID}
	}
	if options.ClusterID != "" {
		filters["clusterId"] = []string{options.ClusterID}
	}
	if options.SearchTerm != "" {
		filters["searchTerm"] = options.SearchTerm
	}
	if len(options.Statuses) > 0 {
		filters["lastActivityStatus"] = options.Statuses
	}
	const query = `query RSCBackupJobs($first: Int!, $after: String, $filters: ActivitySeriesFilter) {
		activitySeriesConnection(first: $first, after: $after, filters: $filters) {
			nodes { activitySeriesId clusterUuid clusterName fid objectId objectName objectType
				lastActivityStatus lastActivityMessage progress startTime lastUpdated isCancelable isOnDemand }
			pageInfo { endCursor hasNextPage }
		}
	}`
	var response struct {
		Data struct {
			Connection *struct {
				Nodes    []BackupJob `json:"nodes"`
				PageInfo *struct {
					EndCursor   *string `json:"endCursor"`
					HasNextPage *bool   `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"activitySeriesConnection"`
		} `json:"data"`
	}
	if _, _, err := request(ctx, client, endpoint, query, map[string]any{
		"first": options.First, "after": options.After, "filters": filters,
	}, &response, 1); err != nil {
		return BackupJobPage{}, err
	}
	connection := response.Data.Connection
	if connection == nil || connection.Nodes == nil || connection.PageInfo == nil || connection.PageInfo.HasNextPage == nil {
		return BackupJobPage{}, fmt.Errorf("RSC backup jobs response is incomplete")
	}
	page := connection.PageInfo
	if *page.HasNextPage && (page.EndCursor == nil || *page.EndCursor == "" || (options.After != nil && *page.EndCursor == *options.After)) {
		return BackupJobPage{}, fmt.Errorf("RSC backup jobs pagination did not advance")
	}
	for _, job := range connection.Nodes {
		if strings.TrimSpace(job.ActivitySeriesID) == "" || strings.TrimSpace(job.ClusterID) == "" || strings.TrimSpace(job.WorkloadID) == "" {
			return BackupJobPage{}, fmt.Errorf("RSC backup job response is missing activity, cluster, or workload identity")
		}
	}
	return BackupJobPage{Jobs: connection.Nodes, EndCursor: page.EndCursor, HasNextPage: *page.HasNextPage}, nil
}

type StartBackupInput struct {
	WorkloadIDs []string `json:"workloadIds"`
	SLAID       string   `json:"slaId"`
}

type BackupTaskchain struct {
	WorkloadID  string `json:"workloadId"`
	TaskchainID string `json:"taskchainUuid"`
}

type BackupWorkloadError struct {
	WorkloadID string `json:"workloadId"`
	Error      string `json:"error"`
}

type StartBackupResult struct {
	Taskchains []BackupTaskchain     `json:"taskchainUuids"`
	Errors     []BackupWorkloadError `json:"errors"`
}

func (client *Client) StartBackup(ctx context.Context, input StartBackupInput) (StartBackupResult, error) {
	return StartBackup(ctx, client.httpClient, client.endpoint, input)
}

func StartBackup(ctx context.Context, client *http.Client, endpoint string, input StartBackupInput) (StartBackupResult, error) {
	if len(input.WorkloadIDs) == 0 {
		return StartBackupResult{}, fmt.Errorf("RSC backup initiation requires at least one workload ID")
	}
	pending := make(map[string]bool, len(input.WorkloadIDs))
	for _, id := range input.WorkloadIDs {
		if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || pending[strings.ToLower(id)] {
			return StartBackupResult{}, fmt.Errorf("RSC backup initiation requires nonempty, unique workload IDs without surrounding whitespace")
		}
		pending[strings.ToLower(id)] = true
	}
	const query = `mutation RSCStartBackup($input: TakeOnDemandSnapshotInput!) {
		takeOnDemandSnapshot(input: $input) {
			taskchainUuids { workloadId taskchainUuid }
			errors { workloadId error }
		}
	}`
	var response struct {
		Data struct {
			Result *StartBackupResult `json:"takeOnDemandSnapshot"`
		} `json:"data"`
	}
	if _, _, err := request(ctx, client, endpoint, query, map[string]any{"input": input}, &response, 1); err != nil {
		return StartBackupResult{}, err
	}
	if response.Data.Result == nil {
		return StartBackupResult{}, fmt.Errorf("RSC backup initiation response is missing its result; outcome is unknown")
	}
	result := *response.Data.Result
	if result.Taskchains == nil || result.Errors == nil {
		return result, fmt.Errorf("RSC backup initiation response is incomplete; outcome may be partial")
	}
	for _, task := range result.Taskchains {
		key := strings.ToLower(task.WorkloadID)
		if !pending[key] || strings.TrimSpace(task.TaskchainID) == "" {
			return result, fmt.Errorf("RSC backup initiation returned an invalid taskchain result; outcome may be partial")
		}
		delete(pending, key)
	}
	for _, failure := range result.Errors {
		key := strings.ToLower(failure.WorkloadID)
		if !pending[key] || strings.TrimSpace(failure.Error) == "" {
			return result, fmt.Errorf("RSC backup initiation returned an invalid workload error; outcome may be partial")
		}
		delete(pending, key)
	}
	if len(pending) > 0 {
		return result, fmt.Errorf("RSC backup initiation omitted %d workload result(s); outcome may be partial", len(pending))
	}
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("RSC backup initiation failed for %d workload(s): %s", len(result.Errors), result.Errors[0].Error)
	}
	return result, nil
}

type CancelBackupInput struct {
	ActivitySeriesID string `json:"activitySeriesId"`
	ClusterID        string `json:"clusterUuid"`
}

func (client *Client) CancelBackup(ctx context.Context, input CancelBackupInput) (bool, error) {
	return CancelBackup(ctx, client.httpClient, client.endpoint, input)
}

func CancelBackup(ctx context.Context, client *http.Client, endpoint string, input CancelBackupInput) (bool, error) {
	if strings.TrimSpace(input.ActivitySeriesID) == "" || strings.TrimSpace(input.ClusterID) == "" {
		return false, fmt.Errorf("RSC backup cancellation requires activity series and cluster IDs")
	}
	const query = `mutation RSCCancelBackup($input: CancelActivitySeriesInput!) { cancelActivitySeries(input: $input) }`
	var response struct {
		Data struct {
			Accepted *bool `json:"cancelActivitySeries"`
		} `json:"data"`
	}
	if _, _, err := request(ctx, client, endpoint, query, map[string]any{"input": input}, &response, 1); err != nil {
		return false, err
	}
	if response.Data.Accepted == nil || !*response.Data.Accepted {
		return false, fmt.Errorf("RSC did not acknowledge activity series cancellation")
	}
	return true, nil
}

func (client *Client) CancelBackupTaskchain(ctx context.Context, taskchainID string) (bool, error) {
	return CancelBackupTaskchain(ctx, client.httpClient, client.endpoint, taskchainID)
}

func CancelBackupTaskchain(ctx context.Context, client *http.Client, endpoint, taskchainID string) (bool, error) {
	if strings.TrimSpace(taskchainID) == "" {
		return false, fmt.Errorf("RSC backup taskchain cancellation requires a taskchain ID")
	}
	const query = `mutation RSCCancelBackupTaskchain($taskchainId: String!) { cancelTaskchain(taskchainId: $taskchainId) { success } }`
	var response struct {
		Data struct {
			Result *struct {
				Success *bool `json:"success"`
			} `json:"cancelTaskchain"`
		} `json:"data"`
	}
	if _, _, err := request(ctx, client, endpoint, query, map[string]any{"taskchainId": taskchainID}, &response, 1); err != nil {
		return false, err
	}
	if response.Data.Result == nil || response.Data.Result.Success == nil || !*response.Data.Result.Success {
		return false, fmt.Errorf("RSC did not acknowledge taskchain cancellation")
	}
	return true, nil
}
