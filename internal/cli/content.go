package cli

import (
	"net/http"

	"github.com/spf13/cobra"
)

func characterCommand() *cobra.Command {
	o := &remoteOptions{}
	group := &cobra.Command{Use: "character", Short: "Manage base character cards and timeline-specific dynamic state"}
	o.flags(group, true)
	group.AddCommand(&cobra.Command{Use: "list", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return o.call(cmd, http.MethodGet, "/characters", nil) }})
	for _, verb := range []string{"get", "delete"} {
		verb := verb
		group.AddCommand(&cobra.Command{Use: verb + " ID", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validID(args[0]); err != nil {
				return err
			}
			method := http.MethodGet
			if verb == "delete" {
				method = http.MethodDelete
			}
			return o.call(cmd, method, "/characters/"+args[0], nil)
		}})
	}
	for _, verb := range []string{"create", "update"} {
		verb := verb
		var file string
		command := &cobra.Command{Use: verb + " ID --file CARD.md", Short: "Write a complete Frontmatter Markdown card through Server", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validID(args[0]); err != nil {
				return err
			}
			if file == "" {
				return usage("--file is required")
			}
			content, err := readText(cmd, file, "")
			if err != nil {
				return err
			}
			method, path, body := http.MethodPost, "/characters", map[string]any{"id": args[0], "content": content}
			if verb == "update" {
				method = http.MethodPut
				path += "/" + args[0]
				delete(body, "id")
			}
			return o.call(cmd, method, path, body)
		}}
		command.Flags().StringVar(&file, "file", "", "Complete UTF-8 card with required Frontmatter")
		group.AddCommand(command)
	}
	group.AddCommand(&cobra.Command{Use: "state ID", Short: "Read effective dynamic state, separate from base card", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		p, err := o.linePath()
		if err != nil {
			return err
		}
		return o.call(cmd, http.MethodGet, p+"/characters/"+args[0]+"/state", nil)
	}})
	dynamic := &cobra.Command{Use: "dynamic", Short: "Manage the character's story-state document"}
	var dynamicFile string
	update := &cobra.Command{Use: "update NAME --file STATE.md", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		if dynamicFile == "" {
			return usage("--file is required")
		}
		document, err := readText(cmd, dynamicFile, "")
		if err != nil {
			return err
		}
		p, err := o.linePath()
		if err != nil {
			return err
		}
		return o.call(cmd, http.MethodPost, p+"/characters/"+args[0]+"/updates", map[string]any{"document": document})
	}}
	update.Flags().StringVar(&dynamicFile, "file", "", "Complete UTF-8 story-state document; empty content clears it")
	dynamic.AddCommand(update)
	dynamic.AddCommand(&cobra.Command{Use: "delete NAME", Short: "Clear the story-state document without changing the base card", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		p, err := o.linePath()
		if err != nil {
			return err
		}
		return o.call(cmd, http.MethodPost, p+"/characters/"+args[0]+"/updates", map[string]any{"document": ""})
	}})
	group.AddCommand(dynamic)
	group.AddCommand(characterMediaCommands(o)...)
	group.AddCommand(archiveCommands(o, "characters")...)
	return group
}
func worldbookCommand() *cobra.Command {
	o := &remoteOptions{}
	group := &cobra.Command{Use: "worldbook", Short: "Manage strict two-level Worldbook content and activation"}
	o.flags(group, true)
	group.AddCommand(&cobra.Command{Use: "list", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return o.call(cmd, http.MethodGet, "/worldbook", nil) }})
	group.AddCommand(&cobra.Command{Use: "refresh", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return o.call(cmd, http.MethodPost, "/worldbook/refresh", map[string]any{})
	}})
	for _, verb := range []string{"get", "delete"} {
		verb := verb
		group.AddCommand(&cobra.Command{Use: verb + " CATEGORY SLUG", Args: countArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			for _, s := range args {
				if err := validID(s); err != nil {
					return err
				}
			}
			method := http.MethodGet
			if verb == "delete" {
				method = http.MethodDelete
			}
			return o.call(cmd, method, "/worldbook/"+args[0]+"/"+args[1], nil)
		}})
	}
	for _, verb := range []string{"create", "update"} {
		verb := verb
		var file string
		command := &cobra.Command{Use: verb + " CATEGORY SLUG --file ENTRY.md", Args: countArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			for _, s := range args {
				if err := validID(s); err != nil {
					return err
				}
			}
			if file == "" {
				return usage("--file is required")
			}
			content, err := readText(cmd, file, "")
			if err != nil {
				return err
			}
			method, path, body := http.MethodPost, "/worldbook", map[string]any{"category": args[0], "slug": args[1], "content": content}
			if verb == "update" {
				method = http.MethodPut
				path += "/" + args[0] + "/" + args[1]
				delete(body, "category")
				delete(body, "slug")
			}
			return o.call(cmd, method, path, body)
		}}
		command.Flags().StringVar(&file, "file", "", "Complete UTF-8 entry with Frontmatter")
		group.AddCommand(command)
	}
	var enabled bool
	category := &cobra.Command{Use: "category CATEGORY", Short: "Enable/disable a category in this story", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		if !cmd.Flags().Changed("enabled") {
			return usage("--enabled=true|false is required")
		}
		path, err := o.linePath()
		if err != nil {
			return err
		}
		path += "/settings"
		var value any = enabled
		return o.call(cmd, http.MethodPatch, path, map[string]any{"worldbook": map[string]any{"categories": map[string]any{args[0]: value}}})
	}}
	category.Flags().BoolVar(&enabled, "enabled", true, "Explicit category enabled state")

	group.AddCommand(category)
	group.AddCommand(archiveCommands(o, "worldbook")...)
	return group
}
func configCommand() *cobra.Command {
	o := &remoteOptions{}
	group := &cobra.Command{Use: "config", Short: "Read or change the shared project configuration"}
	o.flags(group, false)
	group.AddCommand(&cobra.Command{Use: "get", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return o.call(cmd, http.MethodGet, "/project", nil) }})
	var file string
	set := &cobra.Command{Use: "set [KEY JSON_VALUE]", RunE: func(cmd *cobra.Command, args []string) error {
		patch, err := patchFromArgs(args, file)
		if err != nil {
			return err
		}
		return o.call(cmd, http.MethodPatch, "/project", patch)
	}}
	set.Flags().StringVar(&file, "file", "", "JSON configuration patch")
	group.AddCommand(set)
	return group
}
