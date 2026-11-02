package rsc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrIncompleteInventory = errors.New("RSC fileset inventory is incomplete or invalid")

type InventoryLimits struct {
	MaxPages   int
	MaxObjects int
}

type FilesetInventoryObject struct {
	ID             string
	ClusterID      string
	Root           string
	EffectiveSLAID *string
}

type inventoryPageInfo struct {
	HasNextPage *bool   `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type inventoryConnection[T any] struct {
	Nodes    *[]*T              `json:"nodes"`
	PageInfo *inventoryPageInfo `json:"pageInfo"`
}

type inventoryObject struct {
	ID      string `json:"id"`
	Cluster *struct {
		ID string `json:"id"`
	} `json:"cluster"`
	EffectiveSLA *struct {
		ID *string `json:"id"`
	} `json:"effectiveSlaDomain"`
}

type inventoryTemplate struct {
	ID          string                                `json:"id"`
	Descendants *inventoryConnection[inventoryObject] `json:"descendantConnection"`
}

type inventoryTransport struct{ next http.RoundTripper }
type inventoryBody struct {
	io.Reader
	io.Closer
}

func (transport inventoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := transport.next.RoundTrip(req)
	if err == nil && response != nil {
		response.Body = inventoryBody{Reader: io.LimitReader(response.Body, (4<<20)+1), Closer: response.Body}
	}
	return response, err
}

func inventoryID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && !strings.ContainsFunc(value, func(character rune) bool { return unicode.IsSpace(character) || unicode.IsControl(character) })
}

func inventoryComplete[T any](connection *inventoryConnection[T]) bool {
	return connection != nil && connection.Nodes != nil && connection.PageInfo != nil && connection.PageInfo.HasNextPage != nil && len(*connection.Nodes) <= 100
}

func (client *Client) FilesetInventory(ctx context.Context, limits InventoryLimits) ([]FilesetInventoryObject, error) {
	return FetchFilesetInventory(ctx, client.httpClient, client.endpoint, limits)
}

func FetchFilesetInventory(ctx context.Context, httpClient *http.Client, endpoint string, limits InventoryLimits) ([]FilesetInventoryObject, error) {
	if ctx == nil || httpClient == nil || limits.MaxPages < 3 || limits.MaxPages > 30 || limits.MaxObjects < 1 || limits.MaxObjects > 10000 {
		return nil, ErrIncompleteInventory
	}
	if _, exists := ctx.Deadline(); !exists {
		return nil, ErrIncompleteInventory
	}
	bound := *httpClient
	next := bound.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	bound.Transport = inventoryTransport{next: next}
	const queryTemplate = `query RscFilesetInventory { filesetTemplates(hostRoot: %s, first: 100, after: %s) { nodes { id descendantConnection(first: 100) { nodes { id cluster { id } effectiveSlaDomain { id } } pageInfo { hasNextPage } } } pageInfo { endCursor hasNextPage } } }`
	objects := make([]FilesetInventoryObject, 0)
	seenObjects, seenTemplates := make(map[string]bool), make(map[string]bool)
	pages := 0
	for _, root := range []string{"WINDOWS_HOST_ROOT", "LINUX_HOST_ROOT", "NAS_HOST_ROOT"} {
		cursor := "null"
		seenCursors := make(map[string]bool)
		for {
			if pages >= limits.MaxPages {
				return nil, ErrIncompleteInventory
			}
			pages++
			var response struct {
				Data *struct {
					Filesets *inventoryConnection[inventoryTemplate] `json:"filesetTemplates"`
				} `json:"data"`
			}
			body, _, err := request(ctx, &bound, endpoint, fmt.Sprintf(queryTemplate, root, cursor), nil, &response, 1)
			if err != nil {
				return nil, err
			}
			if len(body) > 4<<20 || response.Data == nil || !inventoryComplete(response.Data.Filesets) {
				return nil, ErrIncompleteInventory
			}
			connection := response.Data.Filesets
			for _, template := range *connection.Nodes {
				if template == nil || !inventoryID(template.ID) || seenTemplates[template.ID] || !inventoryComplete(template.Descendants) || *template.Descendants.PageInfo.HasNextPage {
					return nil, ErrIncompleteInventory
				}
				seenTemplates[template.ID] = true
				for _, object := range *template.Descendants.Nodes {
					if object == nil || !inventoryID(object.ID) || seenObjects[object.ID] || object.Cluster == nil || !inventoryID(object.Cluster.ID) || len(objects) >= limits.MaxObjects {
						return nil, ErrIncompleteInventory
					}
					seenObjects[object.ID] = true
					var policy *string
					if object.EffectiveSLA != nil && object.EffectiveSLA.ID != nil {
						if !inventoryID(*object.EffectiveSLA.ID) {
							return nil, ErrIncompleteInventory
						}
						value := *object.EffectiveSLA.ID
						policy = &value
					}
					objects = append(objects, FilesetInventoryObject{ID: object.ID, ClusterID: object.Cluster.ID, Root: root, EffectiveSLAID: policy})
				}
			}
			if !*connection.PageInfo.HasNextPage {
				break
			}
			value := connection.PageInfo.EndCursor
			if value == nil || strings.TrimSpace(*value) == "" || !utf8.ValidString(*value) || len(*value) > 4096 || seenCursors[*value] {
				return nil, ErrIncompleteInventory
			}
			seenCursors[*value] = true
			encoded, _ := json.Marshal(*value)
			cursor = string(encoded)
		}
	}
	return objects, nil
}
