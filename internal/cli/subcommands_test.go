package cli

import (
	"bytes"
	"testing"
)

func TestUnknownSubcommandsFailInsteadOfReportingSuccess(t *testing.T) {
	for _, args := range [][]string{{"chat", "plans", "list", "main"}, {"character", "dynamic", "set", "alice"}, {"worldbook", "nonexistent"}, {"nonexistent"}} {
		root := NewRoot()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("unknown command succeeded: %v", args)
		}
	}
	for _, args := range [][]string{{"chat"}, {"chat", "--help"}, {"character", "dynamic"}} {
		root := NewRoot()
		root.SetOut(&bytes.Buffer{})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("help failed for %v: %v", args, err)
		}
	}
}
