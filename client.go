// Package rsc provides Rubrik Security Cloud inventory, monitoring and
// configuration routines. Authentication is injected; no credential files,
// environment credentials or application caches are discovered by this package.
package rsc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/graphql"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/log"
	"github.com/rubrikinc/rubrik-polaris-sdk-for-go/pkg/polaris/token"
)

// Credentials is an in-memory service account. It is never persisted by rsc-go.
type Credentials struct {
	Name           string `json:"name"`
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	AccessTokenURI string `json:"access_token_uri"`
}

// Client holds application-selected authentication and endpoint information.
// Applications can use the same HTTP client with the package-level routines.
// The zero value is not usable; construct clients with the functions below.
type Client struct {
	httpClient *http.Client
	endpoint   string
}

// RequestError preserves status classification and errors.Is across SDK errors.
type RequestError struct {
	StatusCode int
	Err        error
}

func (err *RequestError) Error() string { return "RSC GraphQL request: " + err.Err.Error() }
func (err *RequestError) Unwrap() error { return err.Err }

// CheckCredentials validates authentication with a minimal read-only query.
func CheckCredentials(ctx context.Context, client *http.Client, endpoint string) error {
	const query = `query OpsctlCredentialCheck { clusterConnection(first: 1) { nodes { id } } }`
	_, _, err := request(ctx, client, endpoint, query, nil, nil, 1)
	if err == nil {
		return nil
	}
	var responseErr *RequestError
	if errors.As(err, &responseErr) && (responseErr.StatusCode == http.StatusUnauthorized || responseErr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("RSC authentication failed: %w", err)
	}
	var gqlErr graphql.GQLError
	if errors.As(err, &gqlErr) {
		return fmt.Errorf("RSC authentication failed: %w", err)
	}
	return fmt.Errorf("RSC connection attempt failed: %w", err)
}

// NewClient accepts an already-authorized HTTP client. It neither exchanges
// credentials nor adds Authorization headers; policy remains with the caller.
func NewClient(endpoint string, httpClient *http.Client) (*Client, error) {
	if err := ValidateEndpoint(endpoint); err != nil {
		return nil, err
	}
	if _, err := graphql.NewClientWithHTTPClient(endpoint, httpClient); err != nil {
		return nil, err
	}
	return &Client{httpClient: httpClient, endpoint: endpoint}, nil
}

// NewServiceAccountJSON accepts JSON bytes in memory, never a file path.
func NewServiceAccountJSON(content []byte, endpoint string) (*Client, error) {
	var credentials Credentials
	if err := json.Unmarshal(content, &credentials); err != nil {
		return nil, fmt.Errorf("decode RSC service account: %w", err)
	}
	return NewServiceAccount(credentials, endpoint)
}

// NewServiceAccount delegates token acquisition and in-memory refresh to the
// SDK, without its disk cache or credential environment overrides. An empty
// endpoint uses the API URL derived from the service account's token URI.
func NewServiceAccount(credentials Credentials, endpoint string) (*Client, error) {
	content, err := json.Marshal(credentials)
	if err != nil {
		return nil, err
	}
	account, err := polaris.ServiceAccountFromText(string(content), false)
	clear(content)
	if err != nil {
		return nil, fmt.Errorf("initialize RSC service account: %w", err)
	}
	if endpoint == "" {
		endpoint = account.APIURL() + "/graphql"
	}
	if err := ValidateEndpoint(account.TokenURL()); err != nil {
		return nil, fmt.Errorf("invalid RSC token endpoint: %w", err)
	}
	tokenClient := &http.Client{CheckRedirect: noRedirect}
	source := token.NewServiceAccountSourceWithLogger(tokenClient, account.TokenURL(),
		account.ClientID, account.ClientSecret, log.DiscardLogger{})
	client := &http.Client{
		Transport:     token.NewRoundTripper(http.DefaultTransport, source),
		CheckRedirect: noRedirect,
	}
	return NewClient(endpoint, client)
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// GraphQL executes a custom operation with the SDK, using one attempt.
// result receives the complete GraphQL envelope, including data/extensions.
func (client *Client) GraphQL(ctx context.Context, query string, variables any, result any) error {
	_, _, err := request(ctx, client.httpClient, client.endpoint, query, variables, result, 1)
	return err
}

func (client *Client) Clusters(ctx context.Context, filter string) ([]ClusterInfo, error) {
	return FetchClusterInfosForCluster(ctx, client.httpClient, client.endpoint, filter)
}

func (client *Client) Health(ctx context.Context, filter string) ([]HealthInfo, error) {
	return FetchHealthsForCluster(ctx, client.httpClient, client.endpoint, filter)
}

func (client *Client) Capacity(ctx context.Context, filter string) ([]DFInfo, error) {
	return FetchDFsForCluster(ctx, client.httpClient, client.endpoint, filter)
}

func (client *Client) SLAs(ctx context.Context) ([]SLAPolicy, error) {
	return FetchSLAPolicies(ctx, client.httpClient, client.endpoint)
}

func (client *Client) Snapshots(ctx context.Context, concurrency int) ([]SnapshotRow, error) {
	return FetchSnapshots(ctx, client.httpClient, client.endpoint, concurrency)
}

func (client *Client) SLADetail(ctx context.Context, id string) (SLADetail, error) {
	return FetchSLADetail(ctx, client.httpClient, client.endpoint, id)
}

func (client *Client) Assignments(ctx context.Context, slaID, slaName string) ([]SLAAssignment, error) {
	return FetchSLAAssignments(ctx, client.httpClient, client.endpoint, slaID, slaName)
}

func (client *Client) Filesets(ctx context.Context) ([]SLAAssignment, error) {
	return FetchFilesets(ctx, client.httpClient, client.endpoint)
}

func (client *Client) FilesetsForSLA(ctx context.Context, filter string) ([]SLAAssignment, error) {
	return FetchFilesetsForSLA(ctx, client.httpClient, client.endpoint, filter)
}

func (client *Client) FilesetTemplates(ctx context.Context) ([]FilesetTemplateConfig, error) {
	return FetchFilesetTemplatesDetailed(ctx, client.httpClient, client.endpoint)
}

func (client *Client) NASSources(ctx context.Context) ([]NASSource, error) {
	return FetchNASSources(ctx, client.httpClient, client.endpoint)
}

func (client *Client) NASShares(ctx context.Context) ([]NASShareConfig, error) {
	return FetchNASSharesDetailed(ctx, client.httpClient, client.endpoint)
}

func (client *Client) NASObjects(ctx context.Context) ([]NASSystemConfig, []NASNamespaceConfig, error) {
	return FetchNASObjectsDetailed(ctx, client.httpClient, client.endpoint)
}

func (client *Client) TargetMappings(ctx context.Context) ([]TargetMappingInfo, error) {
	return FetchTargetMappings(ctx, client.httpClient, client.endpoint)
}

func (client *Client) WorkloadSnapshots(ctx context.Context, workloadID string) ([]SnapshotGQLNode, error) {
	return FetchSnapshotsForWorkload(ctx, client.httpClient, client.endpoint, workloadID)
}

func (client *Client) BackupActivities(ctx context.Context, searchTerm, workloadID string, maxResults int) ([]ActivitySeriesGQLNode, error) {
	return FetchBackupActivitySeries(ctx, client.httpClient, client.endpoint, searchTerm, workloadID, maxResults)
}

func (client *Client) EnrichSnapshotActivities(ctx context.Context, rows []SnapshotRow, filters SnapshotFilters, maxResults, concurrency int) ([]SnapshotRow, error) {
	return EnrichSnapshotsWithBackupActivities(ctx, client.httpClient, client.endpoint, rows, filters, maxResults, concurrency)
}

// RequestOptions applies only to the request context, never global state.
type RequestOptions struct {
	UserAgent string
	Logf      func(level int, format string, args ...any)
}

type requestOptionsKey struct{}

func WithRequestOptions(ctx context.Context, options RequestOptions) context.Context {
	return context.WithValue(ctx, requestOptionsKey{}, options)
}

func logf(ctx context.Context, level int, format string, args ...any) {
	if options, ok := ctx.Value(requestOptionsKey{}).(RequestOptions); ok && options.Logf != nil {
		options.Logf(level, format, args...)
	}
}

type observedTransport struct {
	next      http.RoundTripper
	status    int
	header    http.Header
	err       error
	userAgent string
}

func (transport *observedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if transport.userAgent != "" {
		req.Header.Set("User-Agent", transport.userAgent)
	}
	response, err := transport.next.RoundTrip(req)
	transport.err = err
	if response != nil {
		transport.status = response.StatusCode
		transport.header = response.Header.Clone()
	}
	return response, err
}

func request(ctx context.Context, httpClient *http.Client, endpoint, query string, variables, result any, attempts int) ([]byte, http.Header, error) {
	if ctx == nil || httpClient == nil {
		return nil, nil, errors.New("RSC request requires context and HTTP client")
	}
	if attempts < 1 {
		return nil, nil, errors.New("RSC request requires at least one attempt")
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	options, _ := ctx.Value(requestOptionsKey{}).(RequestOptions)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		next := httpClient.Transport
		if next == nil {
			next = http.DefaultTransport
		}
		observed := &observedTransport{next: next, userAgent: options.UserAgent}
		bound := *httpClient
		bound.Transport = observed
		// Requests are scoped to the chosen endpoint; redirect handling must
		// never move an authorized RSC request to another path or audience.
		bound.CheckRedirect = noRedirect
		sdk, err := graphql.NewClientWithHTTPClient(endpoint, &bound)
		if err != nil {
			return nil, nil, err
		}
		body, err := sdk.RequestWithoutRetry(ctx, query, variables)
		if err == nil {
			if result != nil {
				if err := json.Unmarshal(body, result); err != nil {
					return nil, observed.header, fmt.Errorf("parse RSC GraphQL response: %w", err)
				}
			}
			return body, observed.header, nil
		}
		if ctx.Err() != nil {
			return nil, observed.header, ctx.Err()
		}
		if observed.err != nil {
			err = observed.err
		}
		lastErr = &RequestError{StatusCode: observed.status, Err: err}
		if observed.err == nil && !SLARetryableStatus(observed.status) {
			return nil, observed.header, lastErr
		}
		if attempt < attempts {
			logf(ctx, 2, "rsc: transient request failure (attempt %d/%d): %v", attempt, attempts, lastErr)
		}
	}
	return nil, nil, lastErr
}

func globMatch(pattern, value string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	value = strings.ToLower(strings.TrimSpace(value))
	if pattern == "" || pattern == "*" {
		return true
	}
	expression := strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*")
	return regexp.MustCompile("^" + expression + "$").MatchString(value)
}

func normalizeWildcardFilter(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "%", "*")
	if value == "" {
		return "*"
	}
	if !strings.Contains(value, "*") {
		return "*" + value + "*"
	}
	return value
}

// ValidateEndpoint verifies an explicit RSC URL before acquiring credentials.
func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1"))) {
		return errors.New("invalid RSC endpoint")
	}
	return nil
}
