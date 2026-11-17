package server

import (
	"encoding/json"
	"net/http/httptest"

	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
)

func chatResult(t *testing.T, w *httptest.ResponseRecorder) timeline.Config {
	t.Helper()
	requireStatus(t, w, 201)
	var reply struct {
		Data timeline.Config `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply.Data
}
