package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// WriteError prints one machine-readable error envelope when --json is active;
// callers retain the operation's separate exit code. No connection secret
// is ever included in a CLI-generated diagnostic.
func WriteError(cmd *cobra.Command, err error) {
	if err == nil {
		return
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if !asJSON {
		fmt.Fprintln(cmd.ErrOrStderr(), "rps:", err)
		return
	}
	code := "operation_failed"
	message := err.Error()
	var details json.RawMessage
	var remote *remoteError
	if errors.As(err, &remote) {
		code = remote.Code
		message = remote.Message
		details = remote.Details
	} else if ExitCode(err) == 2 {
		code = "usage_error"
	}
	out := map[string]any{"code": code, "message": message}
	if len(details) > 0 {
		out["details"] = details
	}
	_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(map[string]any{"error": out})
}
func normalizeError(err error) error {
	if err != nil && strings.HasPrefix(err.Error(), "unknown command ") {
		return &UsageError{Err: err}
	}
	return err
}

// Execute is the process entry point and is also usable by black-box CLI tests.
func Execute(root *cobra.Command) int {
	cmd, err := root.ExecuteC()
	err = normalizeError(err)
	if cmd == nil {
		cmd = root
	}
	WriteError(cmd, err)
	return ExitCode(err)
}
