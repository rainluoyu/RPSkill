package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"testing"

	"github.com/luoyu/rpskill/internal/media"
)

func mediaPNG(t *testing.T) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func multipartCall(t *testing.T, s *Server, url, key, token, filename string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	file, err := m.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	file.Write(data)
	for k, v := range fields {
		m.WriteField(k, v)
	}
	m.Close()
	r := httptest.NewRequest("POST", "http://127.0.0.1:8717/api/v1"+url, &b)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func decodeMedia(t *testing.T, w *httptest.ResponseRecorder) media.Record {
	t.Helper()
	var v struct {
		Data media.Record `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Data
}
func getMediaHTTP(s *Server, id, token, rangeHeader string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://127.0.0.1:8717/api/v1/media/"+id, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if rangeHeader != "" {
		r.Header.Set("Range", rangeHeader)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestMediaHTTPMultipartStrict(t *testing.T) {
	s := newTestServer(t)
	requireStatus(t, multipartCall(t, s, "/media", "unknown", testToken, "scene.png", mediaPNG(t), map[string]string{"timeline": "main", "unexpected": "bad"}), 400)
	requireStatus(t, multipartCall(t, s, "/media", "invalidname", testToken, "../escape.png", mediaPNG(t), map[string]string{"timeline": "main"}), 422)
	requireStatus(t, businessRequest(t, s, "POST", "/media", "json", map[string]string{"file": "x"}), 415)
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8717/api/v1/media", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 401)
}
