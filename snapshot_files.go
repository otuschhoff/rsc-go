package rsc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SnapshotFile struct {
	AbsolutePath  string     `json:"absolutePath"`
	DisplayPath   string     `json:"displayPath"`
	Path          string     `json:"path"`
	Filename      string     `json:"filename"`
	FileMode      string     `json:"fileMode"`
	Size          *int64     `json:"size"`
	LastModified  *time.Time `json:"lastModified"`
	StatusMessage string     `json:"statusMessage"`
}

type SnapshotFilesOptions struct {
	Path           string
	First          int
	After          *string
	SearchPrefix   *string
	IsPrefixSearch *bool
}

type SnapshotFilePage struct {
	SnapshotID  string
	Path        string
	Entries     []SnapshotFile
	EndCursor   *string
	HasNextPage bool
}

func (client *Client) SnapshotFiles(ctx context.Context, snapshotID string, options SnapshotFilesOptions) (SnapshotFilePage, error) {
	return FetchSnapshotFiles(ctx, client.httpClient, client.endpoint, snapshotID, options)
}

func FetchSnapshotFiles(ctx context.Context, client *http.Client, endpoint, snapshotID string, options SnapshotFilesOptions) (SnapshotFilePage, error) {
	if strings.TrimSpace(snapshotID) == "" {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot files require a snapshot UUID")
	}
	if options.Path == "" {
		options.Path = "/"
	}
	if options.First == 0 {
		options.First = 100
	}
	if options.First < 1 || options.First > 100 || (options.After != nil && (strings.TrimSpace(*options.After) == "" || len(*options.After) > 4096)) {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot files require a page size of 1-100 and a nonempty continuation cursor")
	}
	if ctx == nil || client == nil {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot files require context and HTTP client")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	bound := *client
	next := bound.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	bound.Transport = inventoryTransport{next: next}
	const query = `query RSCSnapshotFiles($snapshotID: UUID!, $path: String!, $first: Int!, $after: String, $searchPrefix: String, $isPrefixSearch: Boolean) {
		browseSnapshotFileConnection(snapshotFid: $snapshotID, path: $path, first: $first, after: $after, searchPrefix: $searchPrefix, isPrefixSearch: $isPrefixSearch) {
			nodes { absolutePath displayPath path filename fileMode size lastModified statusMessage }
			pageInfo { endCursor hasNextPage }
		}
	}`
	var response struct {
		Data struct {
			Connection *struct {
				Nodes    []*SnapshotFile `json:"nodes"`
				PageInfo *struct {
					EndCursor   *string `json:"endCursor"`
					HasNextPage *bool   `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"browseSnapshotFileConnection"`
		} `json:"data"`
	}
	body, _, err := request(ctx, &bound, endpoint, query, map[string]any{
		"snapshotID": snapshotID, "path": options.Path, "first": options.First, "after": options.After,
		"searchPrefix": options.SearchPrefix, "isPrefixSearch": options.IsPrefixSearch,
	}, &response, 1)
	if err != nil {
		return SnapshotFilePage{}, err
	}
	if len(body) > 4<<20 {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file response exceeded 4 MiB")
	}
	connection := response.Data.Connection
	if connection == nil || connection.Nodes == nil || connection.PageInfo == nil || connection.PageInfo.HasNextPage == nil {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file response is incomplete")
	}
	pageInfo := connection.PageInfo
	if pageInfo.EndCursor != nil && len(*pageInfo.EndCursor) > 4096 {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file cursor exceeded 4096 bytes")
	}
	if *pageInfo.HasNextPage && (pageInfo.EndCursor == nil || *pageInfo.EndCursor == "" || (options.After != nil && *options.After == *pageInfo.EndCursor)) {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file pagination did not advance")
	}
	if len(connection.Nodes) > options.First {
		return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file response exceeded the requested page size")
	}
	entries := make([]SnapshotFile, 0, len(connection.Nodes))
	seen := make(map[string]bool)
	for _, entry := range connection.Nodes {
		if entry == nil || entry.AbsolutePath == "" || entry.Filename == "" || entry.FileMode == "" || entry.Size == nil {
			return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file entry is incomplete")
		}
		if seen[entry.AbsolutePath] {
			return SnapshotFilePage{}, fmt.Errorf("RSC snapshot file response contains duplicate paths")
		}
		seen[entry.AbsolutePath] = true
		entries = append(entries, *entry)
	}
	return SnapshotFilePage{SnapshotID: snapshotID, Path: options.Path, Entries: entries, EndCursor: pageInfo.EndCursor, HasNextPage: *pageInfo.HasNextPage}, nil
}

type SnapshotManifestLimits struct {
	MaxPages   int
	MaxEntries int
}

type SnapshotManifest struct {
	SnapshotID string
	RootPath   string
	Entries    []SnapshotFile
	Pages      int
}

func (client *Client) SnapshotFileManifest(ctx context.Context, snapshotID, rootPath string, limits SnapshotManifestLimits) (SnapshotManifest, error) {
	return FetchSnapshotFileManifest(ctx, client.httpClient, client.endpoint, snapshotID, rootPath, limits)
}

func FetchSnapshotFileManifest(ctx context.Context, client *http.Client, endpoint, snapshotID, rootPath string, limits SnapshotManifestLimits) (SnapshotManifest, error) {
	if ctx == nil {
		return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest requires context")
	}
	if limits.MaxPages == 0 {
		limits.MaxPages = 100
	}
	if limits.MaxEntries == 0 {
		limits.MaxEntries = 10000
	}
	if limits.MaxPages < 1 || limits.MaxPages > 100 || limits.MaxEntries < 1 || limits.MaxEntries > 10000 {
		return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest limits must be 1-100 pages and 1-10000 entries")
	}
	if rootPath == "" {
		rootPath = "/"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	manifest := SnapshotManifest{SnapshotID: snapshotID, RootPath: rootPath, Entries: make([]SnapshotFile, 0)}
	queue := []string{rootPath}
	directories := map[string]bool{rootPath: true}
	seenEntries := make(map[string]bool)
	for len(queue) > 0 {
		directory := queue[0]
		queue = queue[1:]
		var after *string
		cursors := make(map[string]bool)
		for {
			if manifest.Pages >= limits.MaxPages {
				return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest exceeded its page limit; listing is incomplete")
			}
			page, err := FetchSnapshotFiles(ctx, client, endpoint, snapshotID, SnapshotFilesOptions{Path: directory, After: after})
			if err != nil {
				return SnapshotManifest{}, fmt.Errorf("browse RSC snapshot directory %q: %w", directory, err)
			}
			manifest.Pages++
			for _, entry := range page.Entries {
				if seenEntries[entry.AbsolutePath] {
					return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest encountered duplicate paths; listing is incomplete")
				}
				seenEntries[entry.AbsolutePath] = true
				if len(manifest.Entries) >= limits.MaxEntries {
					return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest exceeded its entry limit; listing is incomplete")
				}
				manifest.Entries = append(manifest.Entries, entry)
				switch entry.FileMode {
				case "DIRECTORY":
					if directories[entry.AbsolutePath] {
						return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest encountered a directory cycle")
					}
					directories[entry.AbsolutePath] = true
					queue = append(queue, entry.AbsolutePath)
				case "FILE", "SYMLINK", "SHARE":
				default:
					return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest encountered unsupported file mode %q", entry.FileMode)
				}
			}
			if !page.HasNextPage {
				break
			}
			if cursors[*page.EndCursor] {
				return SnapshotManifest{}, fmt.Errorf("RSC snapshot manifest encountered a cursor cycle")
			}
			cursors[*page.EndCursor] = true
			after = page.EndCursor
		}
	}
	return manifest, nil
}
