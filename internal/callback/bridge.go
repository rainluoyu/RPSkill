package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Command is an explicitly supplied launcher inbox bridge, not an Agent command.
// The bridge must deliver stdin JSON to the already-running owner and exit after
// acceptance. No shell is used and failure never falls back to another session.
type Command struct {
	Executable string
	Args       []string
}

func (c Command) Deliver(ctx context.Context, n Notification) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.Executable, c.Args...)
	cmd.Stdin = bytes.NewReader(raw)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launcher inbox bridge: %w", err)
	}
	return nil
}
