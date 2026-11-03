package rsc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const monitoringFixtureWorkload = "00000000-0000-4000-8000-000000000001"
const monitoringFixtureCluster = "00000000-0000-4000-8000-000000000002"
const monitoringFixtureSeries = "00000000-0000-4000-8000-000000000003"
const monitoringFixtureTaskchain = "00000000-0000-4000-8000-000000000010"

func monitoringFixtureOptions() BackupActivityReadOptions {
	start := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	return BackupActivityReadOptions{
		Scope:  BackupActivityScope{WorkloadIDs: []string{monitoringFixtureWorkload}, ClusterIDs: []string{monitoringFixtureCluster}},
		Window: BackupActivityWindow{Start: start, End: start.Add(time.Hour), Field: BackupActivityUpdatedTime},
		Limits: BackupActivityReadLimits{PageSize: 2, MaxPages: 3, MaxResults: 3, MaxDuration: time.Second},
	}
}

func monitoringFixtureRow() map[string]any {
	return map[string]any{
		"activitySeriesId": monitoringFixtureSeries, "fid": monitoringFixtureWorkload, "clusterUuid": monitoringFixtureCluster,
		"lastActivityType": "Backup", "lastActivityStatus": "Running", "startTime": "2026-11-03T10:10:00Z",
		"lastUpdated": "2026-11-03T10:30:00Z", "lastEventAddedAt": "2026-11-03T10:29:00Z", "lastVerifiedAt": nil,
		"dataTransferred": 100, "logicalSize": 1000, "attemptNumber": 1, "isPolarisEventSeries": false,
	}
}

func monitoringFixtureConnection(rows []any, more bool, cursor any) map[string]any {
	return map[string]any{"data": map[string]any{"activitySeriesConnection": map[string]any{
		"nodes": rows, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor},
	}}}
}

func TestBackupMonitoringSourceStatesAndWindows(t *testing.T) {
	for _, status := range []string{"Running", "Queued", "Success", "Failure", "Canceled", "Canceling", "TaskSuccess", "TaskFailure", "PARTIAL_SUCCESS", "NewSourceStatus"} {
		t.Run(status, func(t *testing.T) {
			for _, field := range []BackupActivityTimeField{BackupActivityUpdatedTime, BackupActivityStartTime} {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
					writer.Header().Set("Content-Type", "application/json")
					var payload struct {
						Variables map[string]any `json:"variables"`
					}
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Error(err)
						return
					}
					filters := payload.Variables["filters"].(map[string]any)
					prefix := "lastUpdatedTime"
					if field == BackupActivityStartTime {
						prefix = "startTime"
					}
					if filters[prefix+"Gt"] != "2026-11-03T10:00:00Z" || filters[prefix+"Lt"] != "2026-11-03T11:00:00Z" {
						t.Error("wrong source-time filters")
					}
					row := monitoringFixtureRow()
					row["lastActivityStatus"] = status
					if field == BackupActivityStartTime {
						row["lastUpdated"] = nil
					}
					_ = json.NewEncoder(writer).Encode(monitoringFixtureConnection([]any{row}, false, nil))
				}))
				options := monitoringFixtureOptions()
				options.Window.Field = field
				read, err := ReadBackupActivityObservations(t.Context(), server.Client(), server.URL, options)
				server.Close()
				if err != nil || !read.Complete || read.Atomic || len(read.Observations) != 1 {
					t.Fatalf("source read failed: %v", err)
				}
				observation := read.Observations[0]
				if observation.LastActivityStatus() != status {
					t.Fatal("raw status was normalized or lost")
				}
				if _, known := observation.LastUpdated(); known != (field == BackupActivityUpdatedTime) {
					t.Fatal("null update time not preserved")
				}
				if sourceTime, known := observation.StartTime(); !known || !sourceTime.Equal(options.Window.Start.Add(10*time.Minute)) {
					t.Fatal("start time meaning changed")
				}
				if eventTime, known := observation.LastEventAddedAt(); !known || !eventTime.Equal(options.Window.Start.Add(29*time.Minute)) {
					t.Fatal("last event time meaning changed")
				}
			}
		})
	}
}

func TestBackupMonitoringIncompleteReads(t *testing.T) {
	for _, scenario := range []string{
		"empty", "missing connection", "missing nodes", "null node", "missing page info", "missing page status", "missing cursor", "long cursor",
		"invalid series", "invalid workload", "wrong workload", "wrong cluster", "duplicate", "missing numeric", "bad numeric", "missing time", "unrepresentable UTC start", "unrepresentable UTC verification",
		"lower boundary", "upper boundary", "null filter time", "wrong type", "missing status", "oversize", "result limit", "page limit", "cyclic cursor", "malformed",
	} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			options := monitoringFixtureOptions()
			if scenario == "result limit" {
				options.Limits.MaxResults = 1
			}
			if scenario == "page limit" {
				options.Limits.MaxPages = 1
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
				requests++
				writer.Header().Set("Content-Type", "application/json")
				row := monitoringFixtureRow()
				rows := []any{row}
				more, cursor := false, any(nil)
				switch scenario {
				case "empty":
					rows = []any{}
				case "null node":
					rows = []any{nil}
				case "invalid series":
					row["activitySeriesId"] = "private-invalid-id"
				case "invalid workload":
					row["fid"] = "not-a-uuid"
				case "wrong workload":
					row["fid"] = monitoringFixtureSeries
				case "wrong cluster":
					row["clusterUuid"] = monitoringFixtureSeries
				case "duplicate":
					rows = []any{row, row}
				case "missing numeric":
					delete(row, "dataTransferred")
				case "bad numeric":
					row["dataTransferred"] = "100"
				case "missing time":
					delete(row, "startTime")
				case "unrepresentable UTC start":
					row["startTime"] = "0000-01-01T00:00:00+01:00"
				case "unrepresentable UTC verification":
					row["lastVerifiedAt"] = "9999-12-31T23:59:00-01:00"
				case "lower boundary":
					row["lastUpdated"] = "2026-11-03T10:00:00Z"
				case "upper boundary":
					row["lastUpdated"] = "2026-11-03T11:00:00Z"
				case "null filter time":
					row["lastUpdated"] = nil
				case "wrong type":
					row["lastActivityType"] = "Recovery"
				case "missing status":
					delete(row, "lastActivityStatus")
				case "missing cursor":
					more = true
				case "long cursor":
					more, cursor = true, strings.Repeat("x", 4097)
				case "result limit":
					other := monitoringFixtureRow()
					other["activitySeriesId"] = "00000000-0000-4000-8000-000000000004"
					rows = append(rows, other)
				case "page limit":
					more, cursor = true, "next"
				case "cyclic cursor":
					rows = []any{}
					more, cursor = true, "one"
					if requests == 2 {
						cursor = "two"
					}
				case "malformed":
					_, _ = io.WriteString(writer, "{")
					return
				}
				response := monitoringFixtureConnection(rows, more, cursor)
				data := response["data"].(map[string]any)
				connection := data["activitySeriesConnection"].(map[string]any)
				switch scenario {
				case "missing connection":
					delete(data, "activitySeriesConnection")
				case "missing nodes":
					delete(connection, "nodes")
				case "missing page info":
					delete(connection, "pageInfo")
				case "missing page status":
					delete(connection["pageInfo"].(map[string]any), "hasNextPage")
				}
				_ = json.NewEncoder(writer).Encode(response)
				if scenario == "oversize" {
					_, _ = io.WriteString(writer, strings.Repeat(" ", 4<<20))
				}
			}))
			defer server.Close()
			read, err := ReadBackupActivityObservations(t.Context(), server.Client(), server.URL, options)
			if scenario == "empty" {
				if err != nil || !read.Complete || read.Observations == nil || len(read.Observations) != 0 {
					t.Fatal("complete empty read was not distinguished")
				}
				return
			}
			if err == nil || read.Complete || read.Observations != nil {
				t.Fatal("invalid or truncated read reported success or partial observations")
			}
			if read.CollectionFinishedAt.IsZero() || read.CollectionStartedAt.IsZero() {
				t.Fatal("failure collection metadata missing")
			}
			if scenario == "page limit" && (read.Pages != 1 || read.Continuation != "next" || requests != 1) {
				t.Fatal("page-limit diagnostic metadata missing")
			}
			if scenario == "page limit" || scenario == "result limit" || scenario == "oversize" {
				if !errors.Is(err, ErrBackupMonitoringLimit) {
					t.Fatalf("limit error code lost: %v", err)
				}
			}
		})
	}
}

func TestBackupMonitoringInvalidOptions(t *testing.T) {
	for _, scenario := range []string{"scope", "name identity", "duplicate scope", "zero time", "reversed time", "non UTC", "time field", "page size", "pages", "results", "duration", "status", "duplicate status"} {
		t.Run(scenario, func(t *testing.T) {
			options := monitoringFixtureOptions()
			switch scenario {
			case "scope":
				options.Scope = BackupActivityScope{}
			case "name identity":
				options.Scope.WorkloadIDs = []string{"display-name"}
			case "duplicate scope":
				options.Scope.WorkloadIDs = append(options.Scope.WorkloadIDs, monitoringFixtureWorkload)
			case "zero time":
				options.Window.Start = time.Time{}
			case "reversed time":
				options.Window.End = options.Window.Start
			case "non UTC":
				options.Window.Start = options.Window.Start.In(time.FixedZone("fixture", 3600))
			case "time field":
				options.Window.Field = "completion"
			case "page size":
				options.Limits.PageSize = 101
			case "pages":
				options.Limits.MaxPages = 0
			case "results":
				options.Limits.MaxResults = 10001
			case "duration":
				options.Limits.MaxDuration = 2 * time.Minute
			case "status":
				options.Statuses = []string{"Aborted"}
			case "duplicate status":
				options.Statuses = []string{"RUNNING", "RUNNING"}
			}
			client := &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid options reached network")
				return nil, nil
			})}
			read, err := ReadBackupActivityObservations(t.Context(), client, "https://fixture.invalid/api/graphql", options)
			if !errors.Is(err, ErrBackupMonitoringOptions) || read.Complete || read.Pages != 0 {
				t.Fatal("invalid options were accepted")
			}
		})
	}
}

func TestBackupMonitoringRetryResetAndLateRevisions(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		row := monitoringFixtureRow()
		if requests >= 2 {
			row["lastActivityStatus"], row["attemptNumber"], row["dataTransferred"] = "Failure", 2, 40
		}
		if requests >= 4 {
			row["lastUpdated"], row["dataTransferred"] = "2026-11-03T10:40:00Z", 60
		}
		_ = json.NewEncoder(writer).Encode(monitoringFixtureConnection([]any{row}, false, nil))
	}))
	defer server.Close()
	var observations []BackupActivityObservation
	for range 4 {
		read, err := ReadBackupActivityObservations(t.Context(), server.Client(), server.URL, monitoringFixtureOptions())
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, read.Observations[0])
	}
	if observations[0].ActivitySeriesID() != observations[1].ActivitySeriesID() || observations[0].RevisionFingerprint() == observations[1].RevisionFingerprint() ||
		observations[1].RevisionFingerprint() != observations[2].RevisionFingerprint() || observations[2].RevisionFingerprint() == observations[3].RevisionFingerprint() {
		t.Fatal("retry/replay/late revisions conflated")
	}
	if bytes, known := observations[0].DataTransferred(); !known || bytes != 100 {
		t.Fatal("later poll mutated earlier observation")
	}
	if bytes, known := observations[1].DataTransferred(); !known || bytes != 40 {
		t.Fatal("counter reset was hidden")
	}
	if attempts, known := observations[1].AttemptNumber(); !known || attempts != 2 {
		t.Fatal("retry metadata lost")
	}
	seenRevisions := make(map[string]bool)
	for _, observation := range observations {
		seenRevisions[observation.RevisionFingerprint()] = true
	}
	if len(seenRevisions) != 3 {
		t.Fatal("replay created a new source observation")
	}
}

func TestBackupMonitoringPrivacyAndIsolation(t *testing.T) {
	const privateMarker = "private-fixture-path-and-secret"
	t.Setenv("RUBRIK_SERVICEACCOUNT_CLIENTSECRET", privateMarker)
	t.Setenv("RUBRIK_SERVICEACCOUNT_FILE", "/nonexistent/private-fixture")
	t.Setenv("RUBRIK_TOKEN_CACHE", "true")
	for _, scenario := range []string{"401", "403", "GraphQL", "transport", "authorized"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			authorityErr := errors.New(privateMarker)
			client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Host != "fixture.invalid" || req.Header.Get("User-Agent") != "monitor-fixture/1" || req.Header.Get("Authorization") != "" {
					t.Error("endpoint/request config or credential isolation changed")
				}
				if scenario == "transport" {
					return nil, authorityErr
				}
				status := 200
				body := `{"errors":[{"message":"` + privateMarker + `"}]}`
				if scenario == "401" {
					status = 401
				}
				if scenario == "403" {
					status = 403
				}
				if scenario == "authorized" {
					req.Header.Set("Authorization", "Bearer authority-fixture-only")
					encoded, _ := json.Marshal(monitoringFixtureConnection([]any{}, false, nil))
					body = string(encoded)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			ctx := WithRequestOptions(t.Context(), RequestOptions{UserAgent: "monitor-fixture/1", Logf: func(_ int, format string, args ...any) {
				t.Error("one-attempt monitoring unexpectedly logged a source failure")
			}})
			read, err := ReadBackupActivityObservations(ctx, client, "https://fixture.invalid/api/graphql", monitoringFixtureOptions())
			if requests != 1 {
				t.Fatal("monitoring retried or discovered credentials")
			}
			if scenario == "authorized" {
				if err != nil || !read.Complete {
					t.Fatal("authorized injected transport was not reused")
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), privateMarker) || read.Complete {
				t.Fatal("failure was unsanitized or reported success")
			}
			if (scenario == "401" || scenario == "403") && !errors.Is(err, ErrBackupMonitoringDenied) {
				t.Fatal("denied source read not distinguished")
			}
			if scenario == "transport" && !errors.Is(err, authorityErr) {
				t.Fatal("authority failure identity lost")
			}
		})
	}
}

func TestBackupMonitoringCancellationAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	read, err := ReadBackupActivityObservations(ctx, http.DefaultClient, "https://fixture.invalid/api/graphql", monitoringFixtureOptions())
	if !errors.Is(err, context.Canceled) || read.Complete {
		t.Fatal("cancellation identity lost")
	}
	client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	options := monitoringFixtureOptions()
	options.Limits.MaxDuration = time.Millisecond
	read, err = ReadBackupActivityObservations(t.Context(), client, "https://fixture.invalid/api/graphql", options)
	if !errors.Is(err, context.DeadlineExceeded) || read.Complete || read.Observations != nil {
		t.Fatal("overall deadline did not fail closed")
	}
	options.Limits.MaxDuration = time.Second
	shortCtx, shortCancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer shortCancel()
	if _, err := ReadBackupActivityObservations(shortCtx, client, "https://fixture.invalid/api/graphql", options); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shorter caller deadline lost")
	}
}

func TestKnownBackupTaskchainReplayAndBoundaries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		attempts := 1
		if requests >= 3 {
			attempts = 2
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"taskchain": map[string]any{
			"taskchainUuid": monitoringFixtureTaskchain, "state": "SUCCEEDED", "parentTaskchainId": "", "jobType": "fixture-type",
			"startTime": "2026-11-03T10:00:00Z", "endTime": "2026-11-03T10:05:00Z", "progressedAt": nil, "currentTaskExecutionAttempts": attempts}}})
	}))
	defer server.Close()
	options := BackupTaskchainReadOptions{TaskchainIDs: []string{monitoringFixtureTaskchain}, MaxRequests: 1, MaxDuration: time.Second}
	completed := make(map[string]bool)
	revisions := make(map[string]bool)
	for range 3 {
		read, err := ReadKnownBackupTaskchains(t.Context(), server.Client(), server.URL, options)
		if err != nil {
			t.Fatal(err)
		}
		observation := read.Observations[0]
		if terminal, known := observation.Terminal(); known && terminal {
			completed[observation.TaskchainID()] = true
		}
		revisions[observation.RevisionFingerprint()] = true
	}
	if len(completed) != 1 || len(revisions) != 2 {
		t.Fatal("polls or task retries counted as new completed executions")
	}
	for _, scenario := range []string{"null start", "null end", "reversed", "zero", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			source := backupTaskchainObservationWire{State: "SUCCEEDED", StartTime: monitoringNullable[time.Time]{value: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), known: true}}
			source.EndTime = source.StartTime
			switch scenario {
			case "null start":
				source.StartTime.known = false
			case "null end":
				source.EndTime.known = false
			case "reversed":
				source.EndTime.value = source.StartTime.value.Add(-time.Second)
			case "overflow":
				source.EndTime.value = source.StartTime.value.AddDate(300, 0, 0)
			}
			duration, known := (BackupTaskchainObservation{source: source}).ElapsedDuration()
			if known != (scenario == "zero") || (known && duration != 0) {
				t.Fatal("invalid duration fabricated or true zero lost")
			}
		})
	}
}

func TestBackupMonitoringExactScopeAndNullableValues(t *testing.T) {
	for _, scope := range []BackupActivityScope{{WorkloadIDs: []string{monitoringFixtureWorkload}}, {ClusterIDs: []string{monitoringFixtureCluster}}} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			var payload struct {
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			filters := payload.Variables["filters"].(map[string]any)
			if filters["lastActivityStatus"].([]any)[0] != "QUEUED" || filters["lastActivityType"].([]any)[0] != "BACKUP" {
				t.Error("source status/type scope not explicit")
			}
			row := monitoringFixtureRow()
			row["lastActivityStatus"], row["startTime"], row["logicalSize"], row["attemptNumber"] = "Queued", nil, nil, 0
			row["lastUpdated"] = "2026-11-03T11:30:00+01:00"
			_ = json.NewEncoder(writer).Encode(monitoringFixtureConnection([]any{row}, false, nil))
		}))
		options := monitoringFixtureOptions()
		options.Scope = scope
		options.Statuses = []string{"QUEUED"}
		read, err := ReadBackupActivityObservations(t.Context(), server.Client(), server.URL, options)
		server.Close()
		if err != nil || len(read.Observations) != 1 {
			t.Fatalf("exact scope read failed: %v", err)
		}
		observation := read.Observations[0]
		if _, known := observation.LogicalSize(); known {
			t.Fatal("null logical bytes collapsed to zero")
		}
		if value, known := observation.AttemptNumber(); !known || value != 0 {
			t.Fatal("source zero attempt lost")
		}
		if value, known := observation.IsPolarisEventSeries(); !known || value {
			t.Fatal("source origin lost")
		}
		if value, known := observation.LastUpdated(); !known || value.Location() != time.UTC || !value.Equal(options.Window.Start.Add(30*time.Minute)) {
			t.Fatal("source time was not normalized to UTC")
		}
		if _, known := observation.LastVerifiedAt(); known {
			t.Fatal("null verification timestamp fabricated")
		}
	}
}

func TestKnownBackupTaskchainFailures(t *testing.T) {
	for _, scenario := range []string{"empty options", "duplicate IDs", "request budget", "wrong identity", "missing taskchain", "missing end field", "unrepresentable UTC start", "unrepresentable UTC progress", "malformed", "oversize", "denied", "canceled", "deadline", "partial batch"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			options := BackupTaskchainReadOptions{TaskchainIDs: []string{monitoringFixtureTaskchain}, MaxRequests: 2, MaxDuration: time.Second}
			ctx := t.Context()
			if scenario == "empty options" {
				options.TaskchainIDs = nil
			}
			if scenario == "duplicate IDs" {
				options.TaskchainIDs = append(options.TaskchainIDs, monitoringFixtureTaskchain)
			}
			if scenario == "request budget" {
				options.MaxRequests = 0
			}
			if scenario == "partial batch" {
				options.TaskchainIDs = append(options.TaskchainIDs, "00000000-0000-4000-8000-000000000011")
			}
			if scenario == "canceled" {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			}
			client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if scenario == "deadline" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				source := map[string]any{"taskchainUuid": monitoringFixtureTaskchain, "state": "RUNNING", "parentTaskchainId": "", "jobType": "fixture-type", "startTime": nil, "endTime": nil, "progressedAt": nil, "currentTaskExecutionAttempts": 1}
				if scenario == "wrong identity" {
					source["taskchainUuid"] = monitoringFixtureWorkload
				}
				if scenario == "missing end field" {
					delete(source, "endTime")
				}
				if scenario == "unrepresentable UTC start" {
					source["startTime"] = "0000-01-01T00:00:00+01:00"
				}
				if scenario == "unrepresentable UTC progress" {
					source["progressedAt"] = "9999-12-31T23:59:00-01:00"
				}
				var taskchain any = source
				if scenario == "missing taskchain" || (scenario == "partial batch" && requests == 2) {
					taskchain = nil
				}
				body, _ := json.Marshal(map[string]any{"data": map[string]any{"taskchain": taskchain}})
				status := 200
				if scenario == "malformed" {
					body = []byte("{")
				}
				if scenario == "oversize" {
					body = append(body, []byte(strings.Repeat(" ", 4<<20))...)
				}
				if scenario == "denied" {
					status = 403
					body = []byte(`{"message":"private-fixture"}`)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
			})}
			if scenario == "deadline" {
				options.MaxDuration = time.Millisecond
			}
			read, err := ReadKnownBackupTaskchains(ctx, client, "https://fixture.invalid/api/graphql", options)
			if err == nil || read.Complete || read.Observations != nil {
				t.Fatal("incomplete known-taskchain read returned success or partial observations")
			}
			if strings.Contains(err.Error(), "private-fixture") {
				t.Fatal("source failure exposed private message")
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("taskchain cancellation identity lost")
			}
			if scenario == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("taskchain deadline identity lost")
			}
			if scenario == "denied" && !errors.Is(err, ErrBackupMonitoringDenied) {
				t.Fatal("denied taskchain not distinguished")
			}
			if scenario == "empty options" || scenario == "duplicate IDs" || scenario == "request budget" || scenario == "canceled" {
				if requests != 0 {
					t.Fatal("invalid/canceled request reached network")
				}
			} else if scenario == "partial batch" {
				if requests != 2 || read.Requests != 2 {
					t.Fatal("partial batch metadata inaccurate")
				}
			} else if requests != 1 {
				t.Fatal("taskchain request unexpectedly retried")
			}
		})
	}
}
