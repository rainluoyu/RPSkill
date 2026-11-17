package server

import (
	"bytes"

	"net/http/httptest"
	"testing"
)

func TestArchiveHTTPRejectsNonMultipartWithoutMutation(t *testing.T) {
	s := newTestServer(t)
	before := businessRequest(t, s, "POST", "/project/export", "", nil)
	requireStatus(t, before, 200)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8717/api/v1/project/import", bytes.NewReader([]byte("bad")))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 415)
	after := businessRequest(t, s, "POST", "/project/export", "", nil)
	if !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("bad import mutated project")
	}
}
