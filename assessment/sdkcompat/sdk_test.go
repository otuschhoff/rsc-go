package sdkcompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/graphql"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/graphql/cluster"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/graphql/event"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/graphql/sla"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/log"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/token"
)

const fixtureID = "00000000-0000-4000-8000-000000000001"

type fixture struct {
	client        *polaris.Client
	tokenRequests atomic.Int64
	apiRequests   atomic.Int64
	cacheDir      string
}

func newFixture(t *testing.T, handler http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{cacheDir: t.TempDir()}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUBRIK_SERVICEACCOUNT_NAME", "environment-must-not-win")
	t.Setenv("RUBRIK_SERVICEACCOUNT_CLIENTID", "environment-must-not-win")
	t.Setenv("RUBRIK_SERVICEACCOUNT_CLIENTSECRET", "environment-must-not-win")
	t.Setenv("RUBRIK_SERVICEACCOUNT_ACCESSTOKENURI", "http://environment-must-not-win.invalid/api/client_token")
	t.Setenv("RUBRIK_SERVICEACCOUNT_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	t.Setenv("RUBRIK_TOKEN_CACHE", "true")
	t.Setenv("RUBRIK_TOKEN_CACHE_DIR", f.cacheDir)

	token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix()))) + "."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/client_token" {
			f.tokenRequests.Add(1)
			var body struct {
				ClientID     string `json:"client_id"`
				ClientSecret string `json:"client_secret"`
				GrantType    string `json:"grant_type"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				http.Error(w, "bad fixture request", http.StatusBadRequest)
				return
			}
			if body.ClientID != "assessment-client" || body.ClientSecret != "assessment-only-not-a-real-secret" ||
				body.GrantType != "client_credentials" {
				t.Error("SDK did not use the explicitly supplied in-memory credentials")
				http.Error(w, "wrong fixture credentials", http.StatusUnauthorized)
				return
			}
			if err := json.NewEncoder(w).Encode(map[string]string{
				"client_id": "assessment-client", "access_token": token,
			}); err != nil {
				t.Error(err)
			}
			return
		}
		f.apiRequests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("SDK did not authenticate the fixture API request")
			http.Error(w, "missing fixture token", http.StatusUnauthorized)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	content, err := json.Marshal(map[string]string{
		"name": "assessment", "client_id": "assessment-client",
		"client_secret":    "assessment-only-not-a-real-secret",
		"access_token_uri": server.URL + "/api/client_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := polaris.ServiceAccountFromText(string(content), false)
	if err != nil {
		t.Fatal(err)
	}
	f.client, err = polaris.NewClientWithCacheParams(account, polaris.CacheParams{Enable: false})
	if err != nil {
		t.Fatal(err)
	}
	if account.APIURL() != server.URL+"/api" {
		t.Fatal("SDK derived an unexpected API URL")
	}
	if f.tokenRequests.Load() != 0 || f.apiRequests.Load() != 0 {
		t.Fatal("construction unexpectedly contacted the network")
	}
	return f
}

func query(t *testing.T, r *http.Request) string {
	t.Helper()
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	return body.Query
}

func TestInMemoryCredentialsNoOverridesOrDiskCache(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graphql" {
			t.Errorf("unexpected API path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":{"clusterConnection":{"nodes":[]}},"extensions":{"apiVersion":"fixture-v1"}}`)
	})
	for range 2 {
		body, err := f.client.GQL.RequestWithoutRetry(context.Background(), `query Assessment { clusterConnection { nodes { id } } }`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"apiVersion":"fixture-v1"`) {
			t.Fatal("raw GraphQL response lost extension metadata")
		}
	}
	if f.tokenRequests.Load() != 1 || f.apiRequests.Load() != 2 {
		t.Fatalf("unexpected token/API request counts: %d/%d", f.tokenRequests.Load(), f.apiRequests.Load())
	}
	files, err := os.ReadDir(f.cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("disabled SDK token cache wrote files")
	}
}

func TestTypedClusterAPIReturnsPageCursor(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(query(t, r), "allClusterConnection") {
			t.Error("unexpected cluster query")
		}
		fmt.Fprintf(w, `{"data":{"result":{"edges":[{"node":{"id":%q,"name":"fixture","version":"9.0.1"}}],"pageInfo":{"hasNextPage":true,"endCursor":"cluster-page-2"},"count":2}}}`, fixtureID)
	})
	page, err := cluster.AllClusters(context.Background(), f.client.GQL, 100, "", cluster.SearchFilter{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Clusters) != 1 || !page.PageInfo.HasNextPage || page.PageInfo.EndCursor != "cluster-page-2" {
		t.Fatalf("cluster pagination metadata was not preserved: %+v", page)
	}
}

func TestLowLevelSDKAllowsIndependentTokenAndGraphQLEndpoints(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("API request incorrectly used the token server")
		http.Error(w, "wrong fixture API", http.StatusNotFound)
	})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/separate-api/graphql" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("SDK did not authenticate against the explicitly selected API endpoint")
		}
		fmt.Fprint(w, `{"data":{"fixture":true}}`)
	}))
	defer api.Close()
	tokenClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	source := token.NewServiceAccountSourceWithLogger(tokenClient, f.client.Account.TokenURL(),
		"assessment-client", "assessment-only-not-a-real-secret", log.DiscardLogger{})
	client := graphql.NewClient(api.URL+"/separate-api", source)
	if _, err := client.RequestWithoutRetry(context.Background(), `query Assessment { fixture }`, nil); err != nil {
		t.Fatal(err)
	}
	if f.tokenRequests.Load() != 1 || f.apiRequests.Load() != 0 {
		t.Fatal("SDK did not keep API and token endpoint selection independent")
	}
}

func TestTypedGlobalSLAOmitsRequiredFields(t *testing.T) {
	var domain sla.Domain
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":%q,"name":"fixture","isDefault":true,"ownerOrg":{"name":"org"},"baseFrequency":{"duration":1,"unit":"DAYS"}}`, fixtureID)), &domain); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"IsDefault", "OwnerOrg", "BaseFrequency"} {
		if _, exists := reflect.TypeOf(domain).FieldByName(field); exists {
			t.Fatalf("SDK changed: reassess missing SLA field %s", field)
		}
	}
}

func TestTypedGlobalSLAPagination(t *testing.T) {
	var pages atomic.Int64
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Variables struct {
				After string `json:"after"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		page := pages.Add(1)
		if page == 1 {
			if request.Variables.After != "" {
				t.Error("first page unexpectedly had a cursor")
			}
			fmt.Fprintf(w, `{"data":{"result":{"nodes":[{"id":%q,"name":"first"}],"pageInfo":{"hasNextPage":true,"endCursor":"page-2"}}}}`, fixtureID)
			return
		}
		if page != 2 || request.Variables.After != "page-2" {
			t.Error("SDK did not request the second page correctly")
		}
		fmt.Fprintf(w, `{"data":{"result":{"nodes":[{"id":%q,"name":"second"}],"pageInfo":{"hasNextPage":false}}}}`, fixtureID)
	})
	domains, err := sla.ListDomains(context.Background(), f.client.GQL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 2 || pages.Load() != 2 {
		t.Fatalf("unexpected pagination result: %d domains, %d pages", len(domains), pages.Load())
	}
}

func TestTypedActivityDropsStartTimeAndListDoesNotExposeCursor(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := query(t, r)
		if strings.Contains(q, "activitySeriesConnection") {
			if strings.Contains(q, "startTime") {
				t.Error("SDK changed: reassess activity list query fields")
			}
			fmt.Fprint(w, `{"data":{"result":{"edges":[{"cursor":"event-1","node":{"id":1,"objectId":"workload-1","lastUpdated":"2026-01-01T01:00:00Z"}}],"pageInfo":{"hasNextPage":true,"endCursor":"event-2"}}}}`)
			return
		}
		if !strings.Contains(q, "startTime") {
			t.Error("expected SDK activity detail query to request startTime")
		}
		fmt.Fprint(w, `{"data":{"result":{"id":1,"objectId":"workload-1","startTime":"2026-01-01T00:00:00Z","lastUpdated":"2026-01-01T01:00:00Z","logicalSize":12345}}}`)
	})
	events := event.Wrap(f.client.GQL)
	detail, err := events.ActivitySeries(context.Background(), fixtureID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reflect.TypeOf(detail).FieldByName("StartTime"); exists {
		t.Fatal("SDK changed: reassess missing activity start time")
	}
	if detail.LogicalSize != 12345 {
		t.Fatal("SDK activity detail did not decode available metrics")
	}
	rows, err := events.EventSeries(context.Background(), "", event.EventSeriesFilter{}, 100, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || reflect.TypeOf(rows).Kind() != reflect.Slice {
		t.Fatal("SDK activity list did not return the expected slice-only result")
	}
}

func TestGenericRequestsRejectGraphQLErrorsAndHonorCancellation(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"errors":[{"message":"fixture refused","extensions":{"code":403}}]}`)
	})
	_, err := f.client.GQL.RequestWithoutRetry(context.Background(), `query Assessment { clusterConnection { nodes { id } } }`, nil)
	var gqlErr graphql.GQLError
	if !errors.As(err, &gqlErr) || gqlErr.Code() != 403 {
		t.Fatalf("expected typed GraphQL rejection, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.client.GQL.RequestWithoutRetry(ctx, `query Assessment { clusterConnection { nodes { id } } }`, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SDK did not preserve cancellation: %v", err)
	}
	if f.apiRequests.Load() != 1 {
		t.Fatal("canceled operation unexpectedly reached the API")
	}
}

func TestRetryDelayRespectsOperationDeadline(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"data":null}`)
	})
	// Prime the in-memory token cache before starting the deadline probe.
	if _, err := f.client.GQL.RequestWithoutRetry(context.Background(), `query Assessment { clusterConnection { nodes { id } } }`, nil); err == nil {
		t.Fatal("expected the fixture's initial unavailable response")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := f.client.GQL.RequestWithoutLogging(ctx, `query Assessment { clusterConnection { nodes { id } } }`, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SDK retry wait did not respect the deadline: %v", err)
	}
	if f.apiRequests.Load() != 2 {
		t.Fatalf("unexpected retry count within short deadline: %d", f.apiRequests.Load())
	}
}

func TestJSONErrorBodyBypassesHTTP503Retry(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"message":"fixture unavailable"}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := f.client.GQL.RequestWithoutLogging(ctx, `query Assessment { fixture }`, nil)
	if err == nil || !strings.Contains(err.Error(), "fixture unavailable") {
		t.Fatalf("unexpected SDK unavailable-response error: %v", err)
	}
	if f.apiRequests.Load() != 1 {
		t.Fatal("SDK changed: reassess JSON-error response retry classification")
	}
}

func TestDefaultSDKClientFollowsRedirectOutsideConfiguredAPIPath(t *testing.T) {
	var followed atomic.Bool
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/graphql" {
			http.Redirect(w, r, "/outside-configured-api-path", http.StatusTemporaryRedirect)
			return
		}
		followed.Store(true)
		fmt.Fprint(w, `{"data":{"fixture":true}}`)
	})
	_, err := f.client.GQL.RequestWithoutRetry(context.Background(), `query Assessment { fixture }`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !followed.Load() {
		t.Fatal("SDK changed: reassess default redirect behavior")
	}
}

func TestTokenOnlyContentIsNotAServiceAccount(t *testing.T) {
	if _, err := polaris.ServiceAccountFromText(`{"access_token":"assessment-only-token"}`, false); err == nil {
		t.Fatal("SDK unexpectedly accepted token-only material as a service account")
	}
}
