package pelican

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveGameFromApplicationAPI(t *testing.T) {
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}

		switch r.URL.Path {
		case "/api/application/servers":
			if got := r.URL.Query().Get("filter[name]"); got != "minecraft-server" {
				t.Fatalf("unexpected filter name: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{
					map[string]any{
						"attributes": map[string]any{
							"name":       "minecraft-server",
							"identifier": "minecraft-server",
							"uuid":       "uuid-1",
							"nest":       2,
							"egg":        5,
						},
					},
				},
			})
		case "/api/application/nests/2/eggs/5":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attributes": map[string]any{
					"name": "Minecraft Dedicated Server",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver, err := NewResolver(server.URL, "token")
	if err != nil {
		t.Fatalf("unexpected resolver error: %v", err)
	}

	game, err := resolver.ResolveGame(context.Background(), "minecraft-server")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if game != "Minecraft Dedicated Server" {
		t.Fatalf("unexpected game: %q", game)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(requests))
	}

	game, err = resolver.ResolveGame(context.Background(), "minecraft-server")
	if err != nil {
		t.Fatalf("unexpected cached resolve error: %v", err)
	}
	if game != "Minecraft Dedicated Server" {
		t.Fatalf("unexpected cached game: %q", game)
	}
	if len(requests) != 2 {
		t.Fatalf("expected cached lookup to avoid extra requests, got %d", len(requests))
	}
}
