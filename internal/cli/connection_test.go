package cli

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func (b *lockedBuffer) Reset()         { b.mu.Lock(); defer b.mu.Unlock(); b.b.Reset() }
func readLog(path string) string       { b, _ := os.ReadFile(path); return string(b) }
func connectionFromLog(t *testing.T, text string) (string, error) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "Open: ") {
			continue
		}
		u, err := url.Parse(strings.TrimPrefix(line, "Open: "))
		if err != nil {
			return "", err
		}
		values, err := url.ParseQuery(u.Fragment)
		if err != nil {
			return "", err
		}
		endpoint := u.Scheme + "://" + u.Host
		t.Setenv("RPS_SERVER_URL", endpoint)
		t.Setenv("RPS_SERVER_TOKEN", values.Get("token"))
		return endpoint, nil
	}
	return "", errors.New("startup not yet announced")
}
func skillFromLog(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Skill: ") {
			return strings.TrimPrefix(line, "Skill: ")
		}
	}
	t.Fatal("no skill location")
	return ""
}
