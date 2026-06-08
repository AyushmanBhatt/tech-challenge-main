package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go/v2"
)

func TestSunTimesTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			json.NewEncoder(w).Encode(map[string]any{
				"results": []any{map[string]any{
					"name":      "Barcelona",
					"country":   "Spain",
					"latitude":  41.3851,
					"longitude": 2.1734,
				}},
			})
		case "/json":
			json.NewEncoder(w).Encode(map[string]any{
				"status": "OK",
				"results": map[string]any{
					"sunrise":    "2026-06-08T04:58:00+00:00",
					"sunset":     "2026-06-08T20:47:00+00:00",
					"day_length": "15:49:00",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	os.Setenv("GEOCODING_API_BASE_URL", server.URL+"/v1/search")
	os.Setenv("SUN_TIMES_API_BASE_URL", server.URL+"/json")
	defer os.Unsetenv("GEOCODING_API_BASE_URL")
	defer os.Unsetenv("SUN_TIMES_API_BASE_URL")

	tool := newSunTimesTool()
	result := tool.Handle(context.Background(), openai.ChatCompletionMessageToolCallUnion{
		Function: openai.ChatCompletionMessageFunctionToolCallFunction{Arguments: `{"location":"Barcelona"}`},
		ID:       "test-id",
	})

	if result.Output == "" {
		t.Fatal("expected non-empty tool output")
	}

	if !contains(result.Output, "Sunrise") || !contains(result.Output, "Sunset") {
		t.Fatalf("unexpected tool output: %s", result.Output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (strings.Contains(s, substr))
}
