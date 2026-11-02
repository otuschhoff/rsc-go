package graphql

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type injectedHTTPTransport func(*http.Request) (*http.Response, error)

func (transport injectedHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return transport(req)
}

func TestNewClientWithHTTPClient(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: injectedHTTPTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		calls++
		if req.URL.String() != "https://example.invalid/custom/graphql" || req.Method != http.MethodPost || req.Header.Get("Authorization") != "" {
			t.Error("injected client endpoint or authentication changed")
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]int `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Query != "query Test($id: Int) { node(id: $id) { id } }" || body.Variables["id"] != 7 {
			t.Errorf("request payload changed: %+v; %v", body, err)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK",
			Header: http.Header{"Content-Type": []string{"application/json"}},
			Body:   io.NopCloser(strings.NewReader(`{"data":{"node":{"id":7}}}`)),
		}, nil
	})}
	client, err := NewClientWithHTTPClient("https://example.invalid/custom/graphql", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("constructor accessed the network")
	}
	response, err := client.RequestWithoutRetry(context.Background(), "query Test($id: Int) { node(id: $id) { id } }", map[string]int{"id": 7})
	if err != nil || string(response) != `{"data":{"node":{"id":7}}}` || calls != 1 {
		t.Fatalf("injected request failed: response=%s calls=%d err=%v", response, calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.RequestWithoutRetry(ctx, "query Test { ok }", nil); err == nil || calls != 1 {
		t.Fatalf("canceled request reached injected transport: calls=%d err=%v", calls, err)
	}
}

func TestNewClientWithHTTPClientValidation(t *testing.T) {
	for _, endpoint := range []string{"", "graphql", "ftp://example.invalid", "https://user:secret@example.invalid/graphql", "https://example.invalid/graphql#fragment"} {
		if _, err := NewClientWithHTTPClient(endpoint, http.DefaultClient); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	if _, err := NewClientWithHTTPClient("https://example.invalid/graphql", nil); err == nil {
		t.Fatal("accepted a nil HTTP client")
	}
}
