package cli

import (
	"github.com/spf13/cobra"
	"net/http"
	"strconv"
)

func branchCommand(o *remoteOptions) *cobra.Command {
	var name string
	cmd := &cobra.Command{Use: "branch CHAT MESSAGE", Short: "Copy history through a 1-based message position into an independent story (host only)", Args: countArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		for _, id := range args {
			if err := validID(id); err != nil {
				return err
			}
		}
		if name == "" {
			return usage("--name is required")
		}
		body := map[string]any{"name": name}
		if pos, err := strconv.Atoi(args[1]); err == nil && pos > 0 {
			body["position"] = pos
		} else {
			body["event_id"] = args[1]
		}
		return o.call(cmd, http.MethodPost, "/chats/"+args[0]+"/branch", body)
	}}
	cmd.Flags().StringVar(&name, "name", "", "New story name")
	return cmd
}
func messageCommand(o *remoteOptions) *cobra.Command {
	group := &cobra.Command{Use: "message", Short: "Edit or delete history messages (host only)"}
	for _, verb := range []string{"edit", "delete"} {
		var text, file string
		var characters []string
		var clearCharacters bool
		cmd := &cobra.Command{Use: verb + " CHAT MESSAGE", Args: countArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			for _, id := range args {
				if err := validID(id); err != nil {
					return err
				}
			}
			body := map[string]any{}
			method := http.MethodDelete
			if verb == "edit" {
				if cmd.Flags().Changed("text") || cmd.Flags().Changed("file") {
					value, err := readText(cmd, file, text)
					if err != nil {
						return err
					}
					body["text"] = value
				}
				if clearCharacters && len(characters) > 0 {
					return usage("--character and --clear-characters conflict")
				}
				if clearCharacters {
					body["characters"] = []string{}
				} else if len(characters) > 0 {
					body["characters"] = characters
				}
				if len(body) == 0 {
					return usage("provide --text, --file, --character or --clear-characters")
				}
				method = http.MethodPatch
			}
			return o.call(cmd, method, "/chats/"+args[0]+"/events/"+args[1], body)
		}}
		if verb == "edit" {
			textFlags(cmd, &file, &text)
			cmd.Flags().StringArrayVar(&characters, "character", nil, "Output character folder name (repeatable; replaces the list)")
			cmd.Flags().BoolVar(&clearCharacters, "clear-characters", false, "Clear output character list")
		}
		group.AddCommand(cmd)
	}
	return group
}
