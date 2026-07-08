package pelican

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	server, err := r.lookupServerByUUID(ctx, containerRef)
	if err != nil {
		return "", err
	}
	if server == nil {
		log.Printf("pelican api output: uuid=%s server=<nil>", containerRef)
		return "", nil
	}
	log.Printf("pelican api output: uuid=%s server=%q identifier=%q name=%q nest=%d egg=%d", containerRef, server.UUID, server.Identifier, server.Name, server.Nest, server.Egg)

	// Pelican uses the egg name as the authoritative game label for matching.
	game, err := r.lookupEggName(ctx, server.Nest, server.Egg)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(game) == "" {
		log.Printf("pelican api output: uuid=%s egg=%d/%d name=<empty>", containerRef, server.Nest, server.Egg)
		return "", nil
	}
	log.Printf("pelican api output: uuid=%s egg=%d/%d name=%q", containerRef, server.Nest, server.Egg, game)

	r.cache.Store(containerRef, game)

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

func (r *Resolver) lookupServerByUUID(ctx context.Context, uuid string) (*applicationServerAttributes, error) {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, nil
	}

	endpoint := *r.baseURL
	endpoint.Path = path.Join(strings.TrimRight(endpoint.Path, "/"), "api/application/servers")
	query := endpoint.Query()
	query.Set("per_page", "100")
	query.Set("filter[uuid]", uuid)
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
	log.Printf("pelican api request: method=GET path=%s status=%s", endpoint.Path, resp.Status)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pelican servers request failed: %s", resp.Status)
	}

	var payload applicationServersResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode pelican servers response: %w", err)
	}
	if len(payload.Data) == 0 {
		log.Printf("pelican api output: uuid=%s servers=[]", uuid)
		return nil, nil
	}

	if server := matchServer(uuid, payload.Data); server != nil {
		log.Printf("pelican api output: uuid=%s servers=%d matched=true", uuid, len(payload.Data))
		return server, nil
	}

	log.Printf("pelican api output: uuid=%s servers=%d matched=false using_first=true", uuid, len(payload.Data))

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
	log.Printf("pelican api request: method=GET path=%s status=%s", endpoint.Path, resp.Status)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("pelican egg request failed: %s", resp.Status)
	}

	var payload applicationEggResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode pelican egg response: %w", err)
	}

	name := strings.TrimSpace(payload.Attributes.Name)
	log.Printf("pelican api output: nest=%d egg=%d name=%q", nestID, eggID, name)
	return name, nil
}
