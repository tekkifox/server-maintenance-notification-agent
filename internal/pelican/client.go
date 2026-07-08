package pelican

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type Resolver struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
	cache      sync.Map
}

func NewResolver(baseURL, token string) (*Resolver, error) {
	baseURL = strings.TrimSpace(baseURL)
	token = strings.TrimSpace(token)
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("pelican api url and token are required")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pelican api url: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")

	return &Resolver{
		baseURL: parsed,
		token:   token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (r *Resolver) ResolveGame(ctx context.Context, containerRef string) (string, error) {
	containerRef = strings.TrimSpace(containerRef)
	if containerRef == "" {
		return "", nil
	}
	if value, ok := r.cache.Load(containerRef); ok {
		return value.(string), nil
	}

	server, err := r.lookupServer(ctx, containerRef)
	if err != nil {
		return "", err
	}
	if server == nil {
		return "", nil
	}

	// Pelican uses the egg name as the authoritative game label for matching.
	game, err := r.lookupEggName(ctx, server.Nest, server.Egg)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(game) == "" {
		return "", nil
	}

	r.cache.Store(containerRef, game)
	if server.Name != "" {
		r.cache.Store(server.Name, game)
	}
	if server.Identifier != "" {
		r.cache.Store(server.Identifier, game)
	}
	if server.UUID != "" {
		r.cache.Store(server.UUID, game)
	}

	return game, nil
}

type applicationServersResponse struct {
	Data []applicationServerItem `json:"data"`
}

type applicationServerItem struct {
	Attributes applicationServerAttributes `json:"attributes"`
}

type applicationServerAttributes struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	UUID       string `json:"uuid"`
	Nest       int    `json:"nest"`
	Egg        int    `json:"egg"`
}

type applicationEggResponse struct {
	Attributes applicationEggAttributes `json:"attributes"`
}

type applicationEggAttributes struct {
	Name string `json:"name"`
}

func (r *Resolver) lookupServer(ctx context.Context, ref string) (*applicationServerAttributes, error) {
	endpoint := *r.baseURL
	endpoint.Path = path.Join(strings.TrimRight(endpoint.Path, "/"), "api/application/servers")
	query := endpoint.Query()
	query.Set("per_page", "100")
	query.Set("filter[name]", ref)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request pelican servers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pelican servers request failed: %s", resp.Status)
	}

	var payload applicationServersResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode pelican servers response: %w", err)
	}
	if len(payload.Data) == 0 {
		return nil, nil
	}

	if server := matchServer(ref, payload.Data); server != nil {
		return server, nil
	}

	return &payload.Data[0].Attributes, nil
}

func matchServer(ref string, items []applicationServerItem) *applicationServerAttributes {
	for i := range items {
		attrs := &items[i].Attributes
		if strings.EqualFold(strings.TrimSpace(attrs.Name), ref) ||
			strings.EqualFold(strings.TrimSpace(attrs.Identifier), ref) ||
			strings.EqualFold(strings.TrimSpace(attrs.UUID), ref) {
			return attrs
		}
	}
	return nil
}

func (r *Resolver) lookupEggName(ctx context.Context, nestID, eggID int) (string, error) {
	if nestID <= 0 || eggID <= 0 {
		return "", nil
	}

	endpoint := *r.baseURL
	endpoint.Path = path.Join(strings.TrimRight(endpoint.Path, "/"), fmt.Sprintf("api/application/nests/%d/eggs/%d", nestID, eggID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request pelican egg: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("pelican egg request failed: %s", resp.Status)
	}

	var payload applicationEggResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode pelican egg response: %w", err)
	}

	return strings.TrimSpace(payload.Attributes.Name), nil
}
