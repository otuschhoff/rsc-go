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

func TestActiveBackupActivityMetadata(t *testing.T) {
	for _, scenario := range []string{"present", "null", "absent", "empty", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: fixtureTransport(func(request *http.Request) (*http.Response, error) {
				requests++
				var operation struct{ Query string }
				if json.NewDecoder(request.Body).Decode(&operation) != nil {
					t.Fatal("cannot decode metadata query")
				}
				for _, field := range []string{"objectName", "objectType", "slaDomainName"} {
					if !strings.Contains(operation.Query, field) {
						t.Fatalf("metadata query omitted %s", field)
					}
				}
				row := monitoringFixtureRow()
				for field, value := range map[string]string{"objectName": `fixture "source"`, "objectType": "VmwareVm", "slaDomainName": "Daily fixture"} {
					switch scenario {
					case "present":
						row[field] = value
					case "null":
						row[field] = nil
					case "empty":
						row[field] = ""
					case "malformed":
						row[field] = 123
					}
				}
				body, _ := json.Marshal(monitoringFixtureConnection([]any{row}, false, nil))
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: request}, nil
			})}
			options := BackupActiveActivityReadOptions{Scope: monitoringFixtureOptions().Scope, Statuses: []string{"RUNNING"}, Limits: monitoringFixtureOptions().Limits}
			read, err := ReadActiveBackupActivityObservations(t.Context(), client, "https://fixture.invalid/api/graphql", options)
			if scenario == "malformed" {
				if err == nil || read.Complete || read.Observations != nil {
					t.Fatal("malformed metadata returned a complete read")
				}
				return
			}
			if err != nil || len(read.Observations) != 1 || requests != 1 {
				t.Fatalf("metadata read changed bounds: %v", err)
			}
			observation := read.Observations[0]
			for _, metadata := range []struct {
				get  func() (string, bool)
				want string
			}{{observation.ObjectName, `fixture "source"`}, {observation.ObjectType, "VmwareVm"}, {observation.SLADomainName, "Daily fixture"}} {
				value, known := metadata.get()
				wantKnown := scenario == "present" || scenario == "empty"
				if scenario != "present" {
					metadata.want = ""
				}
				if known != wantKnown || value != metadata.want {
					t.Fatal("metadata presence or raw value changed")
				}
			}
			encoded, _ := json.Marshal(observation)
			if string(encoded) != "{}" {
				t.Fatal("private metadata escaped opaque observation")
			}
		})
	}
}

func TestActiveBackupActivityReadFailuresAndBounds(t *testing.T) {
	for _, scenario := range []string{"empty", "missing connection", "missing nodes", "missing page info", "missing page status", "missing time", "missing bytes",
		"null row", "out of scope", "duplicate", "unknown state", "terminal state", "missing cursor", "long cursor", "cycle", "page limit", "result limit", "oversize", "malformed", "denied", "transport", "canceled", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			options := BackupActiveActivityReadOptions{Scope: monitoringFixtureOptions().Scope, Statuses: []string{"RUNNING"}, Limits: monitoringFixtureOptions().Limits}
			if scenario == "page limit" {
				options.Limits.MaxPages = 1
			}
			if scenario == "result limit" {
				options.Limits.MaxResults = 1
			}
			if scenario == "deadline" {
				options.Limits.MaxDuration = time.Millisecond
			}
			ctx := WithRequestOptions(t.Context(), RequestOptions{UserAgent: "active-fixture/1", Logf: func(int, string, ...any) { t.Error("source error logged") }})
			if scenario == "canceled" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			requests := 0
			authorityErr := errors.New("private-active-fixture")
			client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Host != "fixture.invalid" || req.Header.Get("User-Agent") != "active-fixture/1" || req.Header.Get("Authorization") != "" {
					t.Error("request authority/config changed")
				}
				if scenario == "transport" {
					return nil, authorityErr
				}
				if scenario == "deadline" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				row := monitoringFixtureRow()
				rows, more, cursor := []any{row}, false, any(nil)
				switch scenario {
				case "empty":
					rows = []any{}
				case "missing time":
					delete(row, "lastUpdated")
				case "missing bytes":
					delete(row, "dataTransferred")
				case "null row":
					rows = []any{nil}
				case "out of scope":
					row["fid"] = monitoringFixtureSeries
				case "duplicate":
					rows = append(rows, row)
				case "unknown state":
					row["lastActivityStatus"] = "NEW_STATE"
				case "terminal state":
					row["lastActivityStatus"] = "Canceled"
				case "missing cursor":
					more = true
				case "long cursor":
					more, cursor = true, strings.Repeat("x", 4097)
				case "cycle":
					row["activitySeriesId"] = []string{monitoringFixtureSeries, monitoringFixtureTaskchain}[requests-1]
					more, cursor = true, "same"
				case "page limit":
					more, cursor = true, "next"
				case "result limit":
					other := monitoringFixtureRow()
					other["activitySeriesId"] = monitoringFixtureTaskchain
					rows = append(rows, other)
				}
				response := monitoringFixtureConnection(rows, more, cursor)
				connection := response["data"].(map[string]any)["activitySeriesConnection"].(map[string]any)
				switch scenario {
				case "missing connection":
					delete(response["data"].(map[string]any), "activitySeriesConnection")
				case "missing nodes":
					delete(connection, "nodes")
				case "missing page info":
					delete(connection, "pageInfo")
				case "missing page status":
					delete(connection["pageInfo"].(map[string]any), "hasNextPage")
				}
				body, _ := json.Marshal(response)
				status := 200
				if scenario == "oversize" {
					body = append(body, []byte(strings.Repeat(" ", 4<<20))...)
				}
				if scenario == "malformed" {
					body = []byte("{")
				}
				if scenario == "denied" {
					status = 403
					body = []byte(`{"message":"private-active-fixture"}`)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
			})}
			read, err := ReadActiveBackupActivityObservations(ctx, client, "https://fixture.invalid/api/graphql", options)
			if scenario == "empty" {
				if err != nil || !read.Complete || read.Observations == nil || read.Results != 0 {
					t.Fatal("complete empty candidates not distinguished")
				}
				return
			}
			if err == nil || read.Complete || read.Observations != nil || strings.Contains(err.Error(), "private-") {
				t.Fatal("failure returned partial or unsanitized source evidence")
			}
			if scenario == "transport" && !errors.Is(err, authorityErr) {
				t.Fatal("authority error identity lost")
			}
			if scenario == "denied" && !errors.Is(err, ErrBackupMonitoringDenied) {
				t.Fatal("denial error identity lost")
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation error identity lost")
			}
			if scenario == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("deadline error identity lost")
			}
			if scenario == "page limit" || scenario == "result limit" || scenario == "oversize" {
				if !errors.Is(err, ErrBackupMonitoringLimit) {
					t.Fatal("limit error identity lost")
				}
			}
			if scenario == "cycle" {
				if requests != 2 {
					t.Fatal("unbounded cursor cycle")
				}
			} else if scenario == "canceled" {
				if requests != 0 {
					t.Fatal("canceled read reached network")
				}
			} else if requests != 1 {
				t.Fatal("extra request/retry/enrichment")
			}
		})
	}
}

func TestActiveBackupActivityInvalidOptions(t *testing.T) {
	for _, scenario := range []string{"no scope", "invalid scope", "scope count", "empty statuses", "status count", "terminal status", "unknown status", "duplicate status", "duration", "pages", "results", "page size"} {
		t.Run(scenario, func(t *testing.T) {
			options := BackupActiveActivityReadOptions{Scope: monitoringFixtureOptions().Scope, Statuses: []string{"RUNNING"}, Limits: monitoringFixtureOptions().Limits}
			switch scenario {
			case "no scope":
				options.Scope = BackupActivityScope{}
			case "invalid scope":
				options.Scope.ClusterIDs = []string{"display-name"}
			case "scope count":
				options.Scope.WorkloadIDs = make([]string, 101)
			case "empty statuses":
				options.Statuses = nil
			case "status count":
				options.Statuses = []string{"RUNNING", "QUEUED", "CANCELING", "SUCCESS"}
			case "terminal status":
				options.Statuses = []string{"CANCELED"}
			case "unknown status":
				options.Statuses = []string{"NEW_STATE"}
			case "duplicate status":
				options.Statuses = []string{"RUNNING", "RUNNING"}
			case "duration":
				options.Limits.MaxDuration = 2 * time.Minute
			case "pages":
				options.Limits.MaxPages = 101
			case "results":
				options.Limits.MaxResults = 10001
			case "page size":
				options.Limits.PageSize = 101
			}
			client := &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid read reached network"); return nil, nil })}
			read, err := ReadActiveBackupActivityObservations(t.Context(), client, "https://fixture.invalid/api/graphql", options)
			if !errors.Is(err, ErrBackupMonitoringOptions) || read.Complete {
				t.Fatal("invalid active selection accepted")
			}
		})
	}
}

func TestActiveBackupActivityRevisionsAndChildLifecycle(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		row := monitoringFixtureRow()
		if requests >= 2 {
			row["dataTransferred"], row["attemptNumber"] = 40, 2
		}
		if requests >= 4 {
			row["lastUpdated"] = "2026-11-03T10:40:00Z"
		}
		body, _ := json.Marshal(monitoringFixtureConnection([]any{row}, false, nil))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}
	options := BackupActiveActivityReadOptions{Scope: monitoringFixtureOptions().Scope, Statuses: []string{"RUNNING"}, Limits: monitoringFixtureOptions().Limits}
	var observations []BackupActivityObservation
	for range 4 {
		read, err := ReadActiveBackupActivityObservations(t.Context(), client, "https://fixture.invalid/api/graphql", options)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, read.Observations[0])
	}
	if observations[0].RevisionFingerprint() == observations[1].RevisionFingerprint() || observations[1].RevisionFingerprint() != observations[2].RevisionFingerprint() || observations[2].RevisionFingerprint() == observations[3].RevisionFingerprint() {
		t.Fatal("retry/replay/late update conflated")
	}
	if value, known := observations[0].DataTransferred(); !known || value != 100 {
		t.Fatal("immutable earlier source value changed")
	}
	if observations[1].DataTransferredContract().CumulativeDeltaSupported {
		t.Fatal("decrease or attempt became a delta baseline")
	}
	windowed, err := ReadBackupActivityObservations(t.Context(), client, "https://fixture.invalid/api/graphql", monitoringFixtureOptions())
	if err != nil || windowed.Observations[0].RevisionFingerprint() != observations[3].RevisionFingerprint() {
		t.Fatal("existing fingerprint contract changed for identical source values")
	}
	terminalIDs := make(map[string]bool)
	var firstTerminalFingerprint string
	for index, state := range []string{"RUNNING", "CANCELING", "CANCELED", "CANCELED", "FUTURE_STATE"} {
		childClient := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
			var end any
			if state == "CANCELED" {
				end = "2026-11-03T10:05:00Z"
			}
			if index == 3 {
				end = "2026-11-03T10:06:00Z"
			}
			body, _ := json.Marshal(map[string]any{"data": map[string]any{"taskchain": map[string]any{
				"taskchainUuid": monitoringFixtureTaskchain, "state": state, "parentTaskchainId": monitoringFixtureSeries,
				"jobType": "opaque-child-type", "startTime": "2026-11-03T10:00:00Z", "endTime": end,
				"progressedAt": nil, "currentTaskExecutionAttempts": 2}}})
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
		})}
		read, err := ReadKnownBackupTaskchains(t.Context(), childClient, "https://fixture.invalid/api/graphql", BackupTaskchainReadOptions{TaskchainIDs: []string{monitoringFixtureTaskchain}, MaxRequests: 1, MaxDuration: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		child := read.Observations[0]
		if terminal, known := child.Terminal(); terminal != (state == "CANCELED") || known != (state != "FUTURE_STATE") {
			t.Fatal("child cancellation state reinterpreted")
		}
		if _, known := child.EndTime(); known != (state == "CANCELED") {
			t.Fatal("child end boundary invented")
		}
		if parent, known := child.ParentTaskchainID(); !known || parent != monitoringFixtureSeries {
			t.Fatal("raw parent changed")
		}
		if state == "CANCELED" {
			terminalIDs[child.TaskchainID()] = true
			if index == 2 {
				firstTerminalFingerprint = child.RevisionFingerprint()
			}
			if index == 3 && child.RevisionFingerprint() == firstTerminalFingerprint {
				t.Fatal("late child end-time correction lost")
			}
			if value, known := child.ElapsedDuration(); !known || value != time.Duration(index+3)*time.Minute {
				t.Fatal("child elapsed boundary correction reinterpreted")
			}
		}
		if BackupActiveActivitySourceCapabilities().RootTaskchainBinding || BackupTaskReportSourceCapabilities().RootTaskchainBinding {
			t.Fatal("child/report promoted into backup root")
		}
	}
	if len(terminalIDs) != 1 {
		t.Fatal("late child revision became another job-instance completion")
	}
}

func TestActiveBackupActivityObservationsWithoutTimeWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var payload struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		filters := payload.Variables["filters"].(map[string]any)
		if len(filters) != 4 || filters["objectFid"].([]any)[0] != monitoringFixtureWorkload ||
			filters["clusterId"].([]any)[0] != monitoringFixtureCluster || filters["lastActivityType"].([]any)[0] != "BACKUP" {
			t.Error("scope broadened or unsupported/time filters added")
		}
		rows := []any{}
		for index, status := range []string{"Running", "Queued", "Canceling"} {
			row := monitoringFixtureRow()
			row["activitySeriesId"] = []string{monitoringFixtureSeries, monitoringFixtureTaskchain, "00000000-0000-4000-8000-000000000011"}[index]
			row["lastActivityStatus"], row["startTime"], row["lastUpdated"] = status, nil, "2020-01-01T00:00:00Z"
			row["dataTransferred"] = nil
			if index == 0 {
				row["dataTransferred"] = 0
			}
			rows = append(rows, row)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(monitoringFixtureConnection(rows, false, nil))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	options := BackupActiveActivityReadOptions{Scope: monitoringFixtureOptions().Scope, Statuses: []string{"RUNNING", "QUEUED", "CANCELING"},
		Limits: BackupActivityReadLimits{PageSize: 3, MaxPages: 2, MaxResults: 3, MaxDuration: time.Second}}
	read, err := client.ReadActiveBackupActivityObservations(t.Context(), options)
	if err != nil || !read.Complete || read.Atomic || read.CompleteEventHistory || read.Results != 3 || read.ScannedResults != 3 {
		t.Fatalf("windowless candidates: %v", err)
	}
	for index, observation := range read.Observations {
		if _, known := observation.StartTime(); known {
			t.Fatal("no-end queued candidate assigned lifecycle boundaries")
		}
		if value, known := observation.DataTransferred(); known != (index == 0) || (known && value != 0) {
			t.Fatal("null/zero counter changed")
		}
		if observation.WorkloadID() != monitoringFixtureWorkload {
			t.Fatal("concurrent same-workload series lost")
		}
	}
	if BackupActiveActivitySourceCapabilities() != (BackupActiveActivityCapabilities{TimeUnrestrictedSeriesCandidates: true, ExactSourceScope: true}) {
		t.Fatal("candidate series promoted into execution/root/history/counter guarantees")
	}
	options.Scope.WorkloadIDs[0], options.Statuses[0] = monitoringFixtureCluster, "changed"
	if read.Scope.WorkloadIDs[0] != monitoringFixtureWorkload || read.SourceScope.WorkloadIDs[0] != monitoringFixtureWorkload || read.Statuses[0] != "RUNNING" {
		t.Fatal("options mutated coverage metadata")
	}
}

func monitoringTaskReportFixtureOptions() BackupTaskReportReadOptions {
	baseline := monitoringFixtureOptions()
	return BackupTaskReportReadOptions{Scope: baseline.Scope, Start: baseline.Window.Start, End: baseline.Window.End,
		Statuses: []string{"fixture-terminal"}, TaskTypes: []string{"fixture-backup"}, Limits: baseline.Limits}
}

func monitoringTaskReportFixtureRow() map[string]any {
	return map[string]any{"id": 1, "reportJobInstanceId": "opaque-report-column", "objectFid": monitoringFixtureWorkload,
		"clusterUuid": monitoringFixtureCluster, "taskCategory": "Protection", "taskType": "fixture-backup", "status": "fixture-terminal",
		"startTime": "2026-11-03T10:10:00Z", "endTime": "2026-11-03T10:30:00Z", "duration": 123,
		"dataTransferred": 100, "logicalBytes": 1000, "physicalBytes": nil}
}

func monitoringTaskReportFixtureConnection(rows []any, more bool, cursor any) map[string]any {
	return map[string]any{"data": map[string]any{"taskDetailConnection": map[string]any{
		"nodes": rows, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor}}}}
}

func TestProtectionTaskReportsFailures(t *testing.T) {
	for _, scenario := range []string{"empty", "missing connection", "missing nodes", "missing page info", "missing page status", "null row",
		"wrong cluster", "invalid workload", "wrong category", "wrong type", "wrong status", "missing bytes", "invalid bytes", "missing row ID",
		"null end", "missing end", "lower boundary", "upper boundary", "invalid UTC", "duplicate", "changed duplicate", "changed duplicate pages", "long report ID",
		"missing cursor", "long cursor", "cyclic cursor", "page limit", "result limit", "filtered result limit", "page size", "oversize", "malformed", "denied", "GraphQL", "transport", "canceled", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			options := monitoringTaskReportFixtureOptions()
			ctx := t.Context()
			if scenario == "page limit" {
				options.Limits.MaxPages = 1
			}
			if scenario == "result limit" || scenario == "filtered result limit" {
				options.Limits.MaxResults = 1
			}
			if scenario == "page size" {
				options.Limits.PageSize = 1
			}
			if scenario == "deadline" {
				options.Limits.MaxDuration = time.Millisecond
			}
			if scenario == "canceled" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			requests := 0
			authorityErr := errors.New("private-task-report-fixture")
			client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if scenario == "transport" {
					return nil, authorityErr
				}
				if scenario == "deadline" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				row := monitoringTaskReportFixtureRow()
				rows := []any{row}
				more, cursor := false, any(nil)
				switch scenario {
				case "empty":
					rows = []any{}
				case "null row":
					rows = []any{nil}
				case "wrong cluster":
					row["clusterUuid"] = monitoringFixtureSeries
				case "invalid workload":
					row["objectFid"] = "private-not-a-uuid"
				case "wrong category":
					row["taskCategory"] = "Recovery"
				case "wrong type":
					row["taskType"] = "fixture-replication"
				case "wrong status":
					row["status"] = "unexpected"
				case "missing bytes":
					delete(row, "dataTransferred")
				case "invalid bytes":
					row["dataTransferred"] = "private-bad-counter"
				case "missing row ID":
					delete(row, "id")
				case "null end":
					row["endTime"] = nil
				case "missing end":
					delete(row, "endTime")
				case "lower boundary":
					row["endTime"] = "2026-11-03T10:00:00Z"
				case "upper boundary":
					row["endTime"] = "2026-11-03T11:00:00Z"
				case "invalid UTC":
					row["startTime"] = "0000-01-01T00:00:00+01:00"
				case "duplicate":
					rows = append(rows, row)
				case "changed duplicate":
					changed := monitoringTaskReportFixtureRow()
					changed["dataTransferred"] = 40
					rows = append(rows, changed)
				case "changed duplicate pages":
					more, cursor = requests == 1, "next"
					if requests == 2 {
						row["dataTransferred"] = 40
					}
				case "long report ID":
					row["reportJobInstanceId"] = strings.Repeat("x", 4097)
				case "missing cursor":
					more = true
				case "long cursor":
					more, cursor = true, strings.Repeat("x", 4097)
				case "cyclic cursor":
					row["id"] = requests
					more, cursor = true, "same"
				case "page limit":
					more, cursor = true, "next"
				case "result limit", "filtered result limit", "page size":
					other := monitoringTaskReportFixtureRow()
					other["id"] = 2
					rows = append(rows, other)
					if scenario == "filtered result limit" {
						row["objectFid"], other["objectFid"] = monitoringFixtureSeries, monitoringFixtureSeries
					}
				}
				response := monitoringTaskReportFixtureConnection(rows, more, cursor)
				data := response["data"].(map[string]any)
				connection := data["taskDetailConnection"].(map[string]any)
				switch scenario {
				case "missing connection":
					delete(data, "taskDetailConnection")
				case "missing nodes":
					delete(connection, "nodes")
				case "missing page info":
					delete(connection, "pageInfo")
				case "missing page status":
					delete(connection["pageInfo"].(map[string]any), "hasNextPage")
				}
				body, _ := json.Marshal(response)
				status := 200
				if scenario == "oversize" {
					body = append(body, []byte(strings.Repeat(" ", 4<<20))...)
				}
				if scenario == "malformed" {
					body = []byte("{")
				}
				if scenario == "denied" {
					status = 403
					body = []byte(`{"message":"private-task-report-fixture"}`)
				}
				if scenario == "GraphQL" {
					body = []byte(`{"errors":[{"message":"private-task-report-fixture"}]}`)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
			})}
			read, err := ReadProtectionTaskReports(ctx, client, "https://fixture.invalid/api/graphql", options)
			if scenario == "empty" {
				if err != nil || !read.Complete || read.Observations == nil || read.Results != 0 {
					t.Fatal("empty source read not distinguished")
				}
				return
			}
			if err == nil || read.Complete || read.Observations != nil || strings.Contains(err.Error(), "private-") {
				t.Fatal("failure leaked or returned partial success")
			}
			if scenario == "transport" && !errors.Is(err, authorityErr) {
				t.Fatal("authority identity lost")
			}
			if scenario == "denied" && !errors.Is(err, ErrBackupMonitoringDenied) {
				t.Fatal("denial identity lost")
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation identity lost")
			}
			if scenario == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("deadline identity lost")
			}
			if scenario == "page limit" || scenario == "result limit" || scenario == "filtered result limit" || scenario == "oversize" {
				if !errors.Is(err, ErrBackupMonitoringLimit) {
					t.Fatal("limit not classified")
				}
			}
			if scenario == "cyclic cursor" || scenario == "changed duplicate pages" {
				if requests != 2 {
					t.Fatal("cycle not bounded")
				}
			} else if scenario == "canceled" {
				if requests != 0 {
					t.Fatal("canceled read reached source")
				}
			} else if requests != 1 {
				t.Fatal("unexpected retry or extra request")
			}
		})
	}
}

func TestProtectionTaskReportsInvalidOptions(t *testing.T) {
	for _, scenario := range []string{"scope", "workload only", "invalid UUID", "scope limit", "duplicate scope", "zero time", "reversed", "non UTC", "year", "statuses", "duplicate status", "long status", "status count", "types", "type count", "pages", "page size", "results", "duration", "endpoint", "nil context", "nil client"} {
		t.Run(scenario, func(t *testing.T) {
			options, ctx, endpoint := monitoringTaskReportFixtureOptions(), t.Context(), "https://fixture.invalid/api/graphql"
			client := &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid options reached network")
				return nil, nil
			})}
			switch scenario {
			case "scope":
				options.Scope = BackupActivityScope{}
			case "workload only":
				options.Scope.ClusterIDs = nil
			case "invalid UUID":
				options.Scope.ClusterIDs = []string{"name"}
			case "scope limit":
				options.Scope.ClusterIDs = make([]string, 101)
			case "duplicate scope":
				options.Scope.ClusterIDs = append(options.Scope.ClusterIDs, monitoringFixtureCluster)
			case "zero time":
				options.Start = time.Time{}
			case "reversed":
				options.End = options.Start
			case "non UTC":
				options.Start = options.Start.In(time.FixedZone("fixture", 3600))
			case "year":
				options.End = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "statuses":
				options.Statuses = nil
			case "duplicate status":
				options.Statuses = append(options.Statuses, options.Statuses[0])
			case "long status":
				options.Statuses = []string{strings.Repeat("x", 129)}
			case "status count", "type count":
				values := make([]string, 101)
				for index := range values {
					values[index] = strings.Repeat("x", index+1)
				}
				if scenario == "status count" {
					options.Statuses = values
				} else {
					options.TaskTypes = values
				}
			case "types":
				options.TaskTypes = nil
			case "pages":
				options.Limits.MaxPages = 101
			case "page size":
				options.Limits.PageSize = 0
			case "results":
				options.Limits.MaxResults = 10001
			case "duration":
				options.Limits.MaxDuration = 2 * time.Minute
			case "endpoint":
				endpoint = "http://not-local.invalid/api/graphql"
			case "nil context":
				ctx = nil
			case "nil client":
				client = nil
			}
			read, err := ReadProtectionTaskReports(ctx, client, endpoint, options)
			if !errors.Is(err, ErrBackupMonitoringOptions) || read.Complete {
				t.Fatal("invalid options accepted")
			}
		})
	}
}

func TestProtectionTaskReportRevisionsAndUnknownExecutionSemantics(t *testing.T) {
	capabilities := BackupTaskReportSourceCapabilities()
	if capabilities != (BackupTaskReportCapabilities{ProtectionTaskEnumeration: true, TaskWorkloadClusterBinding: true,
		EndTimeWindows: true, TaskBoundaryElapsedTime: true, TaskTransferredByteTotals: true}) {
		t.Fatal("unsupported execution, history, root/child, retry or transfer guarantee promoted")
	}
	eventContract := (BackupActivityObservation{}).DataTransferredContract()
	if eventContract.Scope != "event-associated" || eventContract.Unit != "bytes" || eventContract.TotalForScope {
		t.Fatal("event-associated bytes reinterpreted as task totals")
	}
	requests := 0
	client := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		row, concurrent := monitoringTaskReportFixtureRow(), monitoringTaskReportFixtureRow()
		concurrent["id"], concurrent["reportJobInstanceId"] = 2, monitoringFixtureTaskchain
		row["reportJobInstanceId"] = monitoringFixtureTaskchain
		if requests >= 2 {
			row["dataTransferred"] = 40
		}
		if requests >= 4 {
			row["dataTransferred"] = nil
		}
		body, _ := json.Marshal(monitoringTaskReportFixtureConnection([]any{row, concurrent}, false, nil))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}
	var observations []BackupTaskReportObservation
	for range 4 {
		read, err := ReadProtectionTaskReports(t.Context(), client, "https://fixture.invalid/api/graphql", monitoringTaskReportFixtureOptions())
		if err != nil || len(read.Observations) != 2 {
			t.Fatalf("concurrent report rows collapsed: %v", err)
		}
		observations = append(observations, read.Observations[0])
	}
	if observations[0].RevisionFingerprint() == observations[1].RevisionFingerprint() || observations[1].RevisionFingerprint() != observations[2].RevisionFingerprint() || observations[2].RevisionFingerprint() == observations[3].RevisionFingerprint() {
		t.Fatal("late correction/replay/null revision lost")
	}
	if value, known := observations[0].DataTransferred(); !known || value != 100 {
		t.Fatal("earlier observation mutated")
	}
	if value, known := observations[1].DataTransferred(); !known || value != 40 {
		t.Fatal("decreasing raw total hidden")
	}
	if _, known := observations[3].DataTransferred(); known {
		t.Fatal("unknown total became zero")
	}
	if value, known := observations[0].SourceDuration(); !known || value != 123 {
		t.Fatal("unknown-unit duration was reinterpreted")
	}
	for _, contract := range []BackupTransferredBytesContract{observations[0].DataTransferredContract(), (BackupActivityObservation{}).DataTransferredContract()} {
		if contract.CounterMode != BackupTransferCounterUnknown || contract.ExecutionBindingVerified || contract.RetryResetBehaviorVerified || contract.BaselineCompatibilityVerified || contract.CumulativeDeltaSupported || contract.CompressionVerified || contract.PhysicalMeaningVerified {
			t.Fatal("unverified delta/counter semantics advertised")
		}
	}
	for _, scenario := range []string{"null", "zero", "reversed", "overflow"} {
		source := backupTaskReportWire{StartTime: monitoringNullable[time.Time]{value: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), known: true}}
		source.EndTime = source.StartTime
		switch scenario {
		case "null":
			source.StartTime.known = false
		case "reversed":
			source.EndTime.value = source.StartTime.value.Add(-time.Second)
		case "overflow":
			source.EndTime.value = source.StartTime.value.AddDate(300, 0, 0)
		}
		value, known := (BackupTaskReportObservation{source: source}).BoundaryElapsedDuration()
		if known != (scenario == "zero") || value != 0 {
			t.Fatal("invalid task interval or true zero lost")
		}
	}
}

func TestProtectionTaskReportsScopedEvidence(t *testing.T) {
	options := BackupTaskReportReadOptions{Scope: monitoringFixtureOptions().Scope, Start: monitoringFixtureOptions().Window.Start,
		End: monitoringFixtureOptions().Window.End, Statuses: []string{"fixture-terminal"}, TaskTypes: []string{"fixture-backup"}, Limits: monitoringFixtureOptions().Limits}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		requests++
		var payload struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		filter := payload.Variables["filter"].(map[string]any)
		if filter["time_gt"] != "2026-11-03T10:00:00Z" || filter["time_lt"] != "2026-11-03T11:00:00Z" ||
			filter["clusterUuid"].([]any)[0] != monitoringFixtureCluster || filter["taskCategory"].([]any)[0] != "Protection" || filter["searchTerm"] != nil {
			t.Error("incorrect source scope/window or fuzzy identity selection")
		}
		workload, rowID := monitoringFixtureWorkload, 1
		if requests == 1 {
			workload = monitoringFixtureSeries
		} else {
			rowID = 2
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"taskDetailConnection": map[string]any{
			"nodes": []any{map[string]any{"id": rowID, "reportJobInstanceId": "opaque-report-column", "objectFid": workload,
				"clusterUuid": monitoringFixtureCluster, "taskCategory": "Protection", "taskType": "fixture-backup", "status": "fixture-terminal",
				"startTime": "2026-11-03T10:10:00Z", "endTime": "2026-11-03T10:30:00Z", "duration": 123,
				"dataTransferred": 0, "logicalBytes": 1000, "physicalBytes": nil}},
			"pageInfo": map[string]any{"hasNextPage": requests == 1, "endCursor": "next"}}}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	read, err := client.ReadProtectionTaskReports(t.Context(), options)
	if err != nil || !read.Complete || read.Atomic || read.CompleteEventHistory || read.Pages != 2 || read.ScannedResults != 2 || read.Results != 1 {
		t.Fatalf("bounded report read: %v", err)
	}
	observation := read.Observations[0]
	if value, known := observation.DataTransferred(); !known || value != 0 {
		t.Fatal("zero task total lost")
	}
	if _, known := observation.PhysicalBytes(); known {
		t.Fatal("null physical bytes became zero")
	}
	if value, known := observation.ReportJobInstanceID(); !known || value != "opaque-report-column" {
		t.Fatal("opaque report column relabeled")
	}
	if value, known := observation.BoundaryElapsedDuration(); !known || value != 20*time.Minute {
		t.Fatal("task boundary interval lost")
	}
	contract := observation.DataTransferredContract()
	if !contract.TotalForScope || contract.Scope != "task" || contract.CounterMode != BackupTransferCounterUnknown || contract.CumulativeDeltaSupported || contract.BaselineCompatibilityVerified {
		t.Fatal("task total reinterpreted as cumulative execution bytes")
	}
	if read.SourceScope.WorkloadIDs != nil || len(read.SourceScope.ClusterIDs) != 1 {
		t.Fatal("source workload filtering invented")
	}
	capabilities := BackupTaskReportSourceCapabilities()
	if !capabilities.ProtectionTaskEnumeration || capabilities.StableExecutionUUID || capabilities.ScheduledExecutionDiscovery || capabilities.RootTaskchainBinding || capabilities.WholeExecutionStatus {
		t.Fatal("unsupported scheduled execution contract advertised")
	}
	fingerprint := observation.RevisionFingerprint()
	options.Scope.WorkloadIDs[0], options.Scope.ClusterIDs[0] = monitoringFixtureSeries, monitoringFixtureSeries
	options.Statuses[0], options.TaskTypes[0] = "changed", "changed"
	if read.Scope.WorkloadIDs[0] != monitoringFixtureWorkload || read.Scope.ClusterIDs[0] != monitoringFixtureCluster ||
		read.SourceScope.ClusterIDs[0] != monitoringFixtureCluster || read.Statuses[0] != "fixture-terminal" ||
		read.TaskTypes[0] != "fixture-backup" || observation.RevisionFingerprint() != fingerprint {
		t.Fatal("caller options mutated coverage or immutable source evidence")
	}
}

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
