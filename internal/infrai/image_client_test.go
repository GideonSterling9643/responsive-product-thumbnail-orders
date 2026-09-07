package infrai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProcessUsesContractFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		for _, field := range []string{"image", "ops", "format", "store"} {
			if _, ok := body[field]; !ok {
				t.Errorf("missing field %q", field)
			}
			delete(body, field)
		}
		if len(body) != 0 {
			t.Errorf("unsupported fields: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"url":"https://cdn.example/thumb.webp"}}`))
	}))
	defer server.Close()

	client := New("test-key")
	client.BaseURL = server.URL
	if _, err := client.Process(context.Background(), "image-123", 480, 480, "webp", "operation-key"); err != nil {
		t.Fatalf("Process: %v", err)
	}
}
