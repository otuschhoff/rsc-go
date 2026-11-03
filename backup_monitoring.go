package rsc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type BackupMonitoringErrorCode string

const (
	ErrBackupMonitoringOptions    BackupMonitoringErrorCode = "invalid options"
	ErrBackupMonitoringIncomplete BackupMonitoringErrorCode = "incomplete or invalid source read"
	ErrBackupMonitoringLimit      BackupMonitoringErrorCode = "read limit exceeded"
	ErrBackupMonitoringDenied     BackupMonitoringErrorCode = "access denied"
	ErrBackupMonitoringRequest    BackupMonitoringErrorCode = "source request failed"
)

func (code BackupMonitoringErrorCode) Error() string { return "RSC backup monitoring: " + string(code) }

type BackupMonitoringError struct {
	Code       BackupMonitoringErrorCode
	StatusCode int
	cause      error
}

func (err *BackupMonitoringError) Error() string {
	if err.StatusCode != 0 {
		return fmt.Sprintf("%s (HTTP %d)", err.Code.Error(), err.StatusCode)
	}
	return err.Code.Error()
}

func (err *BackupMonitoringError) Unwrap() error        { return err.cause }
func (err *BackupMonitoringError) Is(target error) bool { return target == err.Code }

func backupMonitoringFailure(code BackupMonitoringErrorCode, cause error) error {
	failure := &BackupMonitoringError{Code: code, cause: cause}
	var requestErr *RequestError
	if errors.As(cause, &requestErr) {
		failure.StatusCode = requestErr.StatusCode
		if requestErr.StatusCode == 401 || requestErr.StatusCode == 403 {
			failure.Code = ErrBackupMonitoringDenied
		}
	}
	return failure
}

type BackupActivityScope struct {
	WorkloadIDs []string
	ClusterIDs  []string
}

type BackupActivityTimeField string

const (
	BackupActivityUpdatedTime BackupActivityTimeField = "lastUpdated"
	BackupActivityStartTime   BackupActivityTimeField = "startTime"
)

type BackupActivityWindow struct {
	Start time.Time
	End   time.Time
	Field BackupActivityTimeField
}

type BackupActivityReadLimits struct {
	PageSize    int
	MaxPages    int
	MaxResults  int
	MaxDuration time.Duration
}

type BackupActivityReadOptions struct {
	Scope    BackupActivityScope
	Window   BackupActivityWindow
	Statuses []string
	Limits   BackupActivityReadLimits
}

type BackupActivityRead struct {
	Scope                BackupActivityScope
	Window               BackupActivityWindow
	Observations         []BackupActivityObservation
	Complete             bool
	Atomic               bool
	Pages                int
	Results              int
	ResponseBytes        int
	Continuation         string
	CollectionStartedAt  time.Time
	CollectionFinishedAt time.Time
}

type monitoringNullable[T any] struct {
	value   T
	known   bool
	present bool
}

func (value *monitoringNullable[T]) UnmarshalJSON(body []byte) error {
	*value = monitoringNullable[T]{present: true}
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(body, &value.value); err != nil {
		return err
	}
	value.known = true
	return nil
}

func (value monitoringNullable[T]) MarshalJSON() ([]byte, error) {
	if !value.known {
		return []byte("null"), nil
	}
	return json.Marshal(value.value)
}

type backupActivityObservationWire struct {
	SeriesID           string                        `json:"activitySeriesId"`
	WorkloadID         string                        `json:"fid"`
	ClusterID          string                        `json:"clusterUuid"`
	LastActivityType   string                        `json:"lastActivityType"`
	LastActivityStatus string                        `json:"lastActivityStatus"`
	StartTime          monitoringNullable[time.Time] `json:"startTime"`
	LastUpdated        monitoringNullable[time.Time] `json:"lastUpdated"`
	LastEventAddedAt   monitoringNullable[time.Time] `json:"lastEventAddedAt"`
	LastVerifiedAt     monitoringNullable[time.Time] `json:"lastVerifiedAt"`
	DataTransferred    monitoringNullable[int64]     `json:"dataTransferred"`
	LogicalSize        monitoringNullable[int64]     `json:"logicalSize"`
	AttemptNumber      monitoringNullable[int]       `json:"attemptNumber"`
	IsPolaris          monitoringNullable[bool]      `json:"isPolarisEventSeries"`
}

type BackupActivityObservation struct {
	source      backupActivityObservationWire
	collectedAt time.Time
}

func (observation BackupActivityObservation) ActivitySeriesID() string {
	return observation.source.SeriesID
}
func (observation BackupActivityObservation) WorkloadID() string {
	return observation.source.WorkloadID
}
func (observation BackupActivityObservation) ClusterID() string { return observation.source.ClusterID }
func (observation BackupActivityObservation) LastActivityType() string {
	return observation.source.LastActivityType
}
func (observation BackupActivityObservation) LastActivityStatus() string {
	return observation.source.LastActivityStatus
}
func (observation BackupActivityObservation) CollectedAt() time.Time { return observation.collectedAt }
func (observation BackupActivityObservation) StartTime() (time.Time, bool) {
	return observation.source.StartTime.value, observation.source.StartTime.known
}
func (observation BackupActivityObservation) LastUpdated() (time.Time, bool) {
	return observation.source.LastUpdated.value, observation.source.LastUpdated.known
}
func (observation BackupActivityObservation) LastEventAddedAt() (time.Time, bool) {
	return observation.source.LastEventAddedAt.value, observation.source.LastEventAddedAt.known
}
func (observation BackupActivityObservation) LastVerifiedAt() (time.Time, bool) {
	return observation.source.LastVerifiedAt.value, observation.source.LastVerifiedAt.known
}
func (observation BackupActivityObservation) DataTransferred() (int64, bool) {
	return observation.source.DataTransferred.value, observation.source.DataTransferred.known
}
func (observation BackupActivityObservation) LogicalSize() (int64, bool) {
	return observation.source.LogicalSize.value, observation.source.LogicalSize.known
}
func (observation BackupActivityObservation) AttemptNumber() (int, bool) {
	return observation.source.AttemptNumber.value, observation.source.AttemptNumber.known
}
func (observation BackupActivityObservation) IsPolarisEventSeries() (bool, bool) {
	return observation.source.IsPolaris.value, observation.source.IsPolaris.known
}

func (observation BackupActivityObservation) RevisionFingerprint() string {
	body, _ := json.Marshal(observation.source)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type BackupActivityCapabilities struct {
	ScopedSeriesObservations bool
	SourceTimeWindows        bool
	ExecutionIdentity        bool
	WholeRunStatus           bool
	CompletionTime           bool
	CumulativeByteCounter    bool
	JobDuration              bool
	TransferPhaseDuration    bool
	AtomicEnumeration        bool
}

func BackupActivitySourceCapabilities() BackupActivityCapabilities {
	return BackupActivityCapabilities{ScopedSeriesObservations: true, SourceTimeWindows: true}
}

func monitoringUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16 && value != "00000000-0000-0000-0000-000000000000"
}

func monitoringScopeIDs(values []string) ([]string, map[string]bool, bool) {
	if len(values) > 100 {
		return nil, nil, false
	}
	copyValues := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.ToLower(value)
		if !monitoringUUID(value) || seen[value] {
			return nil, nil, false
		}
		copyValues = append(copyValues, value)
		seen[value] = true
	}
	return copyValues, seen, true
}

func monitoringEventStatus(raw string) string {
	switch raw {
	case "Running":
		return "RUNNING"
	case "Queued":
		return "QUEUED"
	case "Success":
		return "SUCCESS"
	case "Failure":
		return "FAILURE"
	case "Canceled":
		return "CANCELED"
	case "Canceling":
		return "CANCELING"
	case "Info":
		return "INFO"
	case "Warning":
		return "WARNING"
	case "TaskSuccess":
		return "TASK_SUCCESS"
	case "TaskFailure":
		return "TASK_FAILURE"
	case "PARTIAL_SUCCESS":
		return "PARTIAL_SUCCESS"
	default:
		return "UNKNOWN_EVENT_STATUS"
	}
}

func (client *Client) ReadBackupActivityObservations(ctx context.Context, options BackupActivityReadOptions) (BackupActivityRead, error) {
	return ReadBackupActivityObservations(ctx, client.httpClient, client.endpoint, options)
}

func ReadBackupActivityObservations(ctx context.Context, client *http.Client, endpoint string, options BackupActivityReadOptions) (BackupActivityRead, error) {
	read := BackupActivityRead{Window: options.Window}
	fail := func(code BackupMonitoringErrorCode, cause error) (BackupActivityRead, error) {
		read.Observations = nil
		read.Complete = false
		if !read.CollectionStartedAt.IsZero() {
			read.CollectionFinishedAt = time.Now().UTC()
		}
		return read, backupMonitoringFailure(code, cause)
	}
	workloads, workloadSet, validWorkloads := monitoringScopeIDs(options.Scope.WorkloadIDs)
	clusters, clusterSet, validClusters := monitoringScopeIDs(options.Scope.ClusterIDs)
	read.Scope = BackupActivityScope{WorkloadIDs: workloads, ClusterIDs: clusters}
	limits, window := options.Limits, options.Window
	_, startOffset := window.Start.Zone()
	_, endOffset := window.End.Zone()
	if ctx == nil || client == nil || !validWorkloads || !validClusters || len(workloads)+len(clusters) == 0 ||
		window.Start.IsZero() || window.End.IsZero() || !window.Start.Before(window.End) || startOffset != 0 || endOffset != 0 ||
		(window.Field != BackupActivityUpdatedTime && window.Field != BackupActivityStartTime) ||
		limits.PageSize < 1 || limits.PageSize > 100 || limits.MaxPages < 1 || limits.MaxPages > 100 ||
		limits.MaxResults < 1 || limits.MaxResults > 10000 || limits.MaxDuration <= 0 || limits.MaxDuration > time.Minute {
		return fail(ErrBackupMonitoringOptions, nil)
	}
	statuses := make(map[string]bool)
	for _, status := range options.Statuses {
		switch status {
		case "RUNNING", "QUEUED", "SUCCESS", "FAILURE", "CANCELED", "CANCELING", "INFO", "WARNING", "TASK_SUCCESS", "TASK_FAILURE", "PARTIAL_SUCCESS", "UNKNOWN_EVENT_STATUS":
		default:
			return fail(ErrBackupMonitoringOptions, nil)
		}
		if statuses[status] {
			return fail(ErrBackupMonitoringOptions, nil)
		}
		statuses[status] = true
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return fail(ErrBackupMonitoringOptions, err)
	}
	filters := map[string]any{"lastActivityType": []string{"BACKUP"}}
	if len(workloads) > 0 {
		filters["objectFid"] = workloads
	}
	if len(clusters) > 0 {
		filters["clusterId"] = clusters
	}
	if len(statuses) > 0 {
		filters["lastActivityStatus"] = append([]string(nil), options.Statuses...)
	}
	prefix := "lastUpdatedTime"
	if window.Field == BackupActivityStartTime {
		prefix = "startTime"
	}
	filters[prefix+"Gt"] = window.Start.UTC().Format(time.RFC3339Nano)
	filters[prefix+"Lt"] = window.End.UTC().Format(time.RFC3339Nano)
	const query = `query RSCBackupActivityObservations($first: Int!, $after: String, $filters: ActivitySeriesFilter) {
		activitySeriesConnection(first: $first, after: $after, filters: $filters) {
			nodes { activitySeriesId fid clusterUuid lastActivityType lastActivityStatus startTime lastUpdated
				lastEventAddedAt lastVerifiedAt dataTransferred logicalSize attemptNumber isPolarisEventSeries }
			pageInfo { endCursor hasNextPage }
		}
	}`
	ctx, cancel := context.WithTimeout(ctx, limits.MaxDuration)
	defer cancel()
	bound := *client
	next := bound.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	bound.Transport = inventoryTransport{next: next}
	read.CollectionStartedAt = time.Now().UTC()
	read.Observations = make([]BackupActivityObservation, 0)
	var after *string
	seenCursors, seenSeries := make(map[string]bool), make(map[string]bool)
	for {
		if read.Pages >= limits.MaxPages {
			return fail(ErrBackupMonitoringLimit, nil)
		}
		body, _, err := request(ctx, &bound, endpoint, query, map[string]any{"first": limits.PageSize, "after": after, "filters": filters}, nil, 1)
		if err != nil {
			return fail(ErrBackupMonitoringRequest, err)
		}
		read.Pages++
		read.ResponseBytes += len(body)
		if len(body) > 4<<20 {
			return fail(ErrBackupMonitoringLimit, nil)
		}
		if !utf8.Valid(body) {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		var response struct {
			Data *struct {
				Connection *inventoryConnection[backupActivityObservationWire] `json:"activitySeriesConnection"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return fail(ErrBackupMonitoringIncomplete, err)
		}
		if response.Data == nil || !inventoryComplete(response.Data.Connection) {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		connection := response.Data.Connection
		if len(*connection.Nodes) > limits.PageSize {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		collectedAt := time.Now().UTC()
		for _, source := range *connection.Nodes {
			if source == nil {
				return fail(ErrBackupMonitoringIncomplete, nil)
			}
			source.SeriesID, source.WorkloadID, source.ClusterID = strings.ToLower(source.SeriesID), strings.ToLower(source.WorkloadID), strings.ToLower(source.ClusterID)
			if !monitoringUUID(source.SeriesID) || !monitoringUUID(source.WorkloadID) || !monitoringUUID(source.ClusterID) || seenSeries[source.SeriesID] ||
				(len(workloadSet) > 0 && !workloadSet[source.WorkloadID]) || (len(clusterSet) > 0 && !clusterSet[source.ClusterID]) ||
				source.LastActivityType != "Backup" || source.LastActivityStatus == "" ||
				!source.StartTime.present || !source.LastUpdated.present || !source.LastEventAddedAt.present || !source.LastVerifiedAt.present ||
				!source.DataTransferred.present || !source.LogicalSize.present || !source.AttemptNumber.present || !source.IsPolaris.present ||
				(len(statuses) > 0 && !statuses[monitoringEventStatus(source.LastActivityStatus)]) {
				return fail(ErrBackupMonitoringIncomplete, nil)
			}
			for _, timestamp := range []*monitoringNullable[time.Time]{&source.StartTime, &source.LastUpdated, &source.LastEventAddedAt, &source.LastVerifiedAt} {
				if timestamp.known {
					timestamp.value = timestamp.value.UTC()
					if timestamp.value.Year() < 0 || timestamp.value.Year() > 9999 {
						return fail(ErrBackupMonitoringIncomplete, nil)
					}
				}
			}
			sourceTime := source.LastUpdated
			if window.Field == BackupActivityStartTime {
				sourceTime = source.StartTime
			}
			if !sourceTime.known || !sourceTime.value.After(window.Start) || !sourceTime.value.Before(window.End) {
				return fail(ErrBackupMonitoringIncomplete, nil)
			}
			if read.Results >= limits.MaxResults {
				return fail(ErrBackupMonitoringLimit, nil)
			}
			seenSeries[source.SeriesID] = true
			read.Results++
			read.Observations = append(read.Observations, BackupActivityObservation{source: *source, collectedAt: collectedAt})
		}
		pageInfo := connection.PageInfo
		read.Continuation = ""
		if pageInfo.EndCursor != nil {
			if len(*pageInfo.EndCursor) > 4096 || !utf8.ValidString(*pageInfo.EndCursor) {
				return fail(ErrBackupMonitoringIncomplete, nil)
			}
		}
		if !*pageInfo.HasNextPage {
			break
		}
		if pageInfo.EndCursor == nil || strings.TrimSpace(*pageInfo.EndCursor) == "" || seenCursors[*pageInfo.EndCursor] {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		seenCursors[*pageInfo.EndCursor] = true
		read.Continuation = *pageInfo.EndCursor
		after = pageInfo.EndCursor
	}
	if err := ctx.Err(); err != nil {
		return fail(ErrBackupMonitoringRequest, err)
	}
	read.Complete = true
	read.CollectionFinishedAt = time.Now().UTC()
	return read, nil
}

type backupTaskchainObservationWire struct {
	ID           string                        `json:"taskchainUuid"`
	State        string                        `json:"state"`
	ParentID     monitoringNullable[string]    `json:"parentTaskchainId"`
	JobType      monitoringNullable[string]    `json:"jobType"`
	StartTime    monitoringNullable[time.Time] `json:"startTime"`
	EndTime      monitoringNullable[time.Time] `json:"endTime"`
	ProgressedAt monitoringNullable[time.Time] `json:"progressedAt"`
	TaskAttempts monitoringNullable[int]       `json:"currentTaskExecutionAttempts"`
}

type BackupTaskchainObservation struct {
	source      backupTaskchainObservationWire
	collectedAt time.Time
}

func (observation BackupTaskchainObservation) TaskchainID() string    { return observation.source.ID }
func (observation BackupTaskchainObservation) State() string          { return observation.source.State }
func (observation BackupTaskchainObservation) CollectedAt() time.Time { return observation.collectedAt }
func (observation BackupTaskchainObservation) ParentTaskchainID() (string, bool) {
	return observation.source.ParentID.value, observation.source.ParentID.known
}
func (observation BackupTaskchainObservation) JobType() (string, bool) {
	return observation.source.JobType.value, observation.source.JobType.known
}
func (observation BackupTaskchainObservation) StartTime() (time.Time, bool) {
	return observation.source.StartTime.value, observation.source.StartTime.known
}
func (observation BackupTaskchainObservation) EndTime() (time.Time, bool) {
	return observation.source.EndTime.value, observation.source.EndTime.known
}
func (observation BackupTaskchainObservation) ProgressedAt() (time.Time, bool) {
	return observation.source.ProgressedAt.value, observation.source.ProgressedAt.known
}
func (observation BackupTaskchainObservation) CurrentTaskExecutionAttempts() (int, bool) {
	return observation.source.TaskAttempts.value, observation.source.TaskAttempts.known
}

func (observation BackupTaskchainObservation) Terminal() (bool, bool) {
	switch observation.source.State {
	case "SUCCEEDED", "FAILED", "CANCELED":
		return true, true
	case "READY", "RUNNING", "CANCELING", "UNDOING":
		return false, true
	default:
		return false, false
	}
}

func (observation BackupTaskchainObservation) ElapsedDuration() (time.Duration, bool) {
	terminal, known := observation.Terminal()
	start, hasStart := observation.StartTime()
	end, hasEnd := observation.EndTime()
	if !known || !terminal || !hasStart || !hasEnd || end.Before(start) {
		return 0, false
	}
	duration := end.Sub(start)
	if !start.Add(duration).Equal(end) {
		return 0, false
	}
	return duration, true
}

func (observation BackupTaskchainObservation) RevisionFingerprint() string {
	body, _ := json.Marshal(observation.source)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type BackupTaskchainReadOptions struct {
	TaskchainIDs []string
	MaxRequests  int
	MaxDuration  time.Duration
}

type BackupTaskchainRead struct {
	Observations         []BackupTaskchainObservation
	Complete             bool
	Atomic               bool
	Requests             int
	CollectionStartedAt  time.Time
	CollectionFinishedAt time.Time
}

func (client *Client) ReadKnownBackupTaskchains(ctx context.Context, options BackupTaskchainReadOptions) (BackupTaskchainRead, error) {
	return ReadKnownBackupTaskchains(ctx, client.httpClient, client.endpoint, options)
}

func ReadKnownBackupTaskchains(ctx context.Context, client *http.Client, endpoint string, options BackupTaskchainReadOptions) (BackupTaskchainRead, error) {
	read := BackupTaskchainRead{}
	fail := func(code BackupMonitoringErrorCode, cause error) (BackupTaskchainRead, error) {
		read.Observations = nil
		if !read.CollectionStartedAt.IsZero() {
			read.CollectionFinishedAt = time.Now().UTC()
		}
		return read, backupMonitoringFailure(code, cause)
	}
	ids, _, valid := monitoringScopeIDs(options.TaskchainIDs)
	if ctx == nil || client == nil || !valid || len(ids) == 0 || options.MaxRequests < 1 || options.MaxRequests > 100 ||
		len(ids) > options.MaxRequests || options.MaxDuration <= 0 || options.MaxDuration > time.Minute {
		return fail(ErrBackupMonitoringOptions, nil)
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return fail(ErrBackupMonitoringOptions, err)
	}
	ctx, cancel := context.WithTimeout(ctx, options.MaxDuration)
	defer cancel()
	bound := *client
	next := bound.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	bound.Transport = inventoryTransport{next: next}
	read.CollectionStartedAt = time.Now().UTC()
	read.Observations = make([]BackupTaskchainObservation, 0, len(ids))
	const query = `query RSCKnownBackupTaskchain($id: String!) {
		taskchain(taskchainId: $id) { taskchainUuid state parentTaskchainId jobType startTime endTime progressedAt currentTaskExecutionAttempts }
	}`
	for _, id := range ids {
		body, _, err := request(ctx, &bound, endpoint, query, map[string]any{"id": id}, nil, 1)
		if err != nil {
			return fail(ErrBackupMonitoringRequest, err)
		}
		read.Requests++
		if len(body) > 4<<20 {
			return fail(ErrBackupMonitoringLimit, nil)
		}
		if !utf8.Valid(body) {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		var response struct {
			Data *struct {
				Taskchain *backupTaskchainObservationWire `json:"taskchain"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return fail(ErrBackupMonitoringIncomplete, err)
		}
		if response.Data == nil || response.Data.Taskchain == nil {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		source := *response.Data.Taskchain
		source.ID = strings.ToLower(source.ID)
		if source.ID != id || !monitoringUUID(source.ID) || source.State == "" || !source.ParentID.present || !source.JobType.present ||
			!source.StartTime.present || !source.EndTime.present || !source.ProgressedAt.present || !source.TaskAttempts.present {
			return fail(ErrBackupMonitoringIncomplete, nil)
		}
		for _, timestamp := range []*monitoringNullable[time.Time]{&source.StartTime, &source.EndTime, &source.ProgressedAt} {
			if timestamp.known {
				timestamp.value = timestamp.value.UTC()
				if timestamp.value.Year() < 0 || timestamp.value.Year() > 9999 {
					return fail(ErrBackupMonitoringIncomplete, nil)
				}
			}
		}
		read.Observations = append(read.Observations, BackupTaskchainObservation{source: source, collectedAt: time.Now().UTC()})
	}
	if err := ctx.Err(); err != nil {
		return fail(ErrBackupMonitoringRequest, err)
	}
	read.Complete = true
	read.CollectionFinishedAt = time.Now().UTC()
	return read, nil
}
