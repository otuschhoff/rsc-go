package rsc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (transport fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return transport(req)
}

func TestServiceAccountInMemoryConcurrentRequests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("RUBRIK_TOKEN_CACHE", "true")
	t.Setenv("RUBRIK_TOKEN_CACHE_DIR", cache)
	t.Setenv("RUBRIK_SERVICEACCOUNT_NAME", "ignored")
	t.Setenv("RUBRIK_SERVICEACCOUNT_CLIENTID", "ignored")
	t.Setenv("RUBRIK_SERVICEACCOUNT_CLIENTSECRET", "ignored")
	t.Setenv("RUBRIK_SERVICEACCOUNT_ACCESSTOKENURI", "https://ignored.invalid/api/client_token")
	t.Setenv("RUBRIK_SERVICEACCOUNT_FILE", "/nonexistent/service-account.json")
	jwt := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, time.Now().Add(time.Hour).Unix())) + "."
	var tokens, queries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/api/client_token" {
			tokens.Add(1)
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["client_id"] != "client" || body["client_secret"] != "fixture-only-secret" || body["grant_type"] != "client_credentials" {
				t.Error("SDK did not receive the supplied in-memory credentials")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": jwt, "client_id": "client"})
			return
		}
		queries.Add(1)
		if req.URL.Path != "/api/graphql" || req.Header.Get("Authorization") != "Bearer "+jwt {
			t.Error("unexpected GraphQL endpoint or authentication")
		}
		_, _ = io.WriteString(w, `{"data":{"ok":true}}`)
	}))
	defer server.Close()
	content, err := json.Marshal(Credentials{
		Name: "test", ClientID: "client", ClientSecret: "fixture-only-secret",
		AccessTokenURI: server.URL + "/api/client_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewServiceAccountJSON(content, "")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 0 || queries.Load() != 0 {
		t.Fatal("client construction accessed the network")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			var response struct {
				Data struct{ OK bool }
			}
			if err := client.GraphQL(t.Context(), "query Check { ok }", nil, &response); err != nil || !response.Data.OK {
				t.Errorf("query failed: %v; response=%+v", err, response)
			}
		})
	}
	wg.Wait()
	if tokens.Load() != 1 || queries.Load() != 8 {
		t.Fatalf("token/API requests = %d/%d, want 1/8", tokens.Load(), queries.Load())
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 0 {
		t.Fatalf("SDK wrote a disk cache: %v; entries=%v", err, entries)
	}
}

func TestClientValidation(t *testing.T) {
	for _, endpoint := range []string{"", "not a URL", "ftp://example.com/graphql", "http://example.com/graphql", "https://user:secret@example.com/graphql", "https://example.com/graphql#fragment", "https://example.com/graphql?token=secret"} {
		if _, err := NewClient(endpoint, http.DefaultClient); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	if _, err := NewClient("https://example.com/graphql", nil); err == nil {
		t.Fatal("accepted a nil HTTP client")
	}
	for _, content := range []string{`invalid`, `{}`, `{"access_token":"legacy-token"}`} {
		if _, err := NewServiceAccountJSON([]byte(content), ""); err == nil {
			t.Fatalf("accepted invalid service account %s", content)
		}
	}
	_, _, err := request(t.Context(), http.DefaultClient, "https://example.com/graphql", "query { ok }", nil, nil, 0)
	if err == nil {
		t.Fatal("zero attempts returned success")
	}
}

func TestServiceAccountBlocksTokenRedirects(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		redirected.Add(1)
		http.Error(w, "must not receive credentials", http.StatusUnauthorized)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination.URL+"/api/client_token", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := NewServiceAccount(Credentials{
		Name: "test", ClientID: "client", ClientSecret: "fixture-only-secret",
		AccessTokenURI: source.URL + "/api/client_token",
	}, source.URL+"/api/graphql")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := client.GraphQL(ctx, "query Check { ok }", nil, nil); err == nil {
		t.Fatal("redirected token exchange unexpectedly succeeded")
	}
	if redirected.Load() != 0 {
		t.Fatal("token credentials were forwarded through a redirect")
	}
}

func TestRequestRetryClassificationWithJSONErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusForbidden, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				attempts++
				w.Header().Set("Content-Type", "application/json")
				if attempts < 3 {
					w.WriteHeader(status)
					if status == http.StatusOK {
						_, _ = io.WriteString(w, `{"errors":[{"message":"fixture permanent denial"}]}`)
					} else {
						_, _ = io.WriteString(w, `{"message":"fixture error"}`)
					}
					return
				}
				_, _ = io.WriteString(w, `{"data":{"ok":true}}`)
			}))
			defer server.Close()
			var response struct{ Data struct{ OK bool } }
			err := ExecuteSLAGraphQL(t.Context(), server.Client(), server.URL, "query Check { ok }", &response)
			if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
				if err != nil || attempts != 3 || !response.Data.OK {
					t.Fatalf("transient JSON failure was not retried: attempts=%d response=%v err=%v", attempts, response, err)
				}
			} else if err == nil || attempts != 1 {
				t.Fatalf("permanent failure was retried or hidden: attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestAuthorizedClientPreservesErrorsAndOptions(t *testing.T) {
	denied := errors.New("fixture authorization denied")
	var requests int
	httpClient := &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Header.Get("Authorization") != "" {
			t.Error("SDK added authentication to an application-owned transport")
		}
		if req.Header.Get("User-Agent") != "fixture/1" || req.URL.String() != "https://example.com/custom/graphql" {
			t.Error("SDK changed endpoint or discarded the request options")
		}
		return nil, denied
	})}
	client, err := NewClient("https://example.com/custom/graphql", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequestOptions(t.Context(), RequestOptions{UserAgent: "fixture/1"})
	if err := client.GraphQL(ctx, "query Check { ok }", nil, nil); !errors.Is(err, denied) {
		t.Fatalf("authorization error identity lost: %v", err)
	}
	if requests != 1 {
		t.Fatal("custom GraphQL request retried an authorization failure")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := client.GraphQL(ctx, "query Check { ok }", nil, nil); !errors.Is(err, context.Canceled) || requests != 1 {
		t.Fatalf("canceled request reached transport: requests=%d error=%v", requests, err)
	}
}

func TestClientBlocksRedirects(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		redirected.Add(1)
		_, _ = io.WriteString(w, `{"data":{"ok":true}}`)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination.URL+"/outside", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	httpClient := source.Client()
	var callerRedirects atomic.Int32
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { callerRedirects.Add(1); return nil }
	client, err := NewClient(source.URL+"/api/graphql", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	var response any
	err = client.GraphQL(t.Context(), "query Check { ok }", nil, &response)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != http.StatusTemporaryRedirect || redirected.Load() != 0 || callerRedirects.Load() != 0 {
		t.Fatalf("redirect was not blocked: %v; destination=%d", err, redirected.Load())
	}
}

func TestClustersPreserveHeaderVersionAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct{ Query, OperationName string }
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !strings.Contains(body.Query, "clusterConnection(") || body.OperationName != "OpsctlRSCInfo" {
			t.Error("SDK changed the inventory operation or scope")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Rubrik-API-Version", "fixture-version")
		_, _ = io.WriteString(w, `{"data":{"clusterConnection":{"nodes":[{"id":"cluster-id","name":"cluster","version":"9.0"}]}}}`)
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api/graphql", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.Clusters(t.Context(), "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("clusters=%v error=%v", rows, err)
	}
	if rows[0].ClusterID != "cluster-id" || rows[0].SoftwareVersion != "9.0" || rows[0].APIVersion != "fixture-version" {
		t.Fatalf("cluster or API version fields lost: %+v", rows[0])
	}
}

func TestParallelCollectorsDoNotSucceedOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rows, err := QueryWorkloadSnapshotsParallel(ctx, http.DefaultClient, "https://example.com/graphql",
		[]WorkloadSnapshotTarget{{WorkloadID: "workload"}}, 1)
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("snapshot collector returned canceled partial success: %v, %v", rows, err)
	}
	activities, err := FetchBackupActivitiesForWorkloads(ctx, http.DefaultClient, "https://example.com/graphql", []string{"workload"}, 1, 1)
	if !errors.Is(err, context.Canceled) || activities != nil {
		t.Fatalf("activity collector returned canceled partial success: %v, %v", activities, err)
	}
}

func TestClientGraphQLErrorAndDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			t.Error(err)
			return
		}
		if req.URL.Path == "/slow" {
			select {
			case <-req.Context().Done():
			case <-time.After(time.Second):
				t.Error("server did not observe canceled request")
			}
			return
		}
		_, _ = io.WriteString(w, `{"errors":[{"message":"fixture permission denied"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api/graphql", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.GraphQL(t.Context(), "query Check { ok }", nil, nil); err == nil || !strings.Contains(err.Error(), "fixture permission denied") {
		t.Fatalf("GraphQL failure was hidden: %v", err)
	}
	client, err = NewClient(server.URL+"/slow", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := client.GraphQL(ctx, "query Check { ok }", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity lost: %v", err)
	}
}
