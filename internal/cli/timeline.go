package cli

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

func timelineCommand() *cobra.Command {
	o := &remoteOptions{}
	group := &cobra.Command{Use: "chat", Short: "Manage independent stories and their messages"}
	o.flags(group, false)
	group.AddCommand(&cobra.Command{Use: "list", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return o.call(cmd, http.MethodGet, "/chats", nil) }})
	for _, verb := range []string{"get", "delete", "state"} {
		verb := verb
		group.AddCommand(&cobra.Command{Use: verb + " ID", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validID(args[0]); err != nil {
				return err
			}
			path := "/chats/" + args[0]
			method := http.MethodGet
			if verb == "state" {
				path += "/state"
			}
			if verb == "delete" {
				method = http.MethodDelete
			}
			return o.call(cmd, method, path, nil)
		}})
	}
	create := &cobra.Command{Use: "create NAME", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		return o.call(cmd, http.MethodPost, "/chats", map[string]any{"name": args[0]})
	}}
	group.AddCommand(create)
	group.AddCommand(&cobra.Command{Use: "rename ID NAME", Args: countArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		return o.call(cmd, http.MethodPatch, "/chats/"+args[0], map[string]any{"name": args[1]})
	}})
	var after string
	var limit int
	history := &cobra.Command{Use: "history ID", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		if limit < 1 || limit > 10000 {
			return usage("--limit must be 1..10000")
		}
		q := url.Values{"limit": {strconv.Itoa(limit)}}
		if after != "" {
			q.Set("after", after)
		}
		return o.call(cmd, http.MethodGet, "/chats/"+args[0]+"/events?"+q.Encode(), nil)
	}}
	history.Flags().StringVar(&after, "after", "", "Message cursor")
	history.Flags().IntVar(&limit, "limit", 200, "Page size")
	group.AddCommand(history, storySettingsCommand(o), branchCommand(o), messageCommand(o))
	group.AddCommand(archiveCommands(o, "chats")...)
	return group
}
func storySettingsCommand(o *remoteOptions) *cobra.Command {
	var file string
	command := &cobra.Command{Use: "settings ID [KEY JSON_VALUE]", Short: "Read or update this story's narrative, polishing, worldbook and characters", Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 && len(args) != 3 {
			return usage("requires a story ID and optional KEY JSON_VALUE or --file PATCH.json")
		}
		return validID(args[0])
	}, RunE: func(cmd *cobra.Command, args []string) error {
		path := "/chats/" + args[0] + "/settings"
		if len(args) == 1 && file == "" {
			return o.call(cmd, http.MethodGet, path, nil)
		}
		patch, err := patchFromArgs(args[1:], file)
		if err != nil {
			return err
		}
		return o.call(cmd, http.MethodPatch, path, patch)
	}}
	command.Flags().StringVar(&file, "file", "", "JSON story settings patch")
	return command
}
