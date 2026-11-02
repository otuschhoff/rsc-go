package graphql

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/log"
)

// NewClientWithHTTPClient uses an application-owned authenticated HTTP client.
// endpoint is the complete GraphQL URL. No token source, credential discovery,
// token cache, environment overrides or additional transport is installed.
// The caller must not mutate httpClient while requests are in progress.
func NewClientWithHTTPClient(endpoint string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" ||
		(u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid GraphQL endpoint")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("GraphQL HTTP client is required")
	}
	return &Client{
		gqlURL: endpoint,
		client: httpClient,
		log:    log.DiscardLogger{},
	}, nil
}
