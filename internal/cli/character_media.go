package cli

import (
	"encoding/json"
	"go.yaml.in/yaml/v3"
	"net/http"
	"net/url"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/spf13/cobra"
)

func characterMediaCommands(o *remoteOptions) []*cobra.Command {
	list := &cobra.Command{Use: "media ID", Short: "List a character's avatar, expressions and attachments", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		return o.call(cmd, http.MethodGet, "/media?character="+url.QueryEscape(args[0]), nil)
	}}
	var avatarFile string
	var clear bool
	avatar := &cobra.Command{Use: "avatar ID [--file IMAGE | --clear]", Short: "Set or clear the base avatar through Server; no direct card writes", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		if (avatarFile == "") == !clear {
			return usage("provide exactly one of --file or --clear")
		}
		c, err := o.client()
		if err != nil {
			return err
		}

		if !clear {
			return characterAssetUpload(cmd, o, c, args[0], avatarFile, "", true)
		}
		raw, err := c.do(cmd, http.MethodGet, "/characters/"+args[0], nil)
		if err != nil {
			return err
		}
		var envelope struct {
			Data character.Document `json:"data"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return err
		}
		card := envelope.Data.Metadata
		card.Avatar = ""
		metadata, err := yaml.Marshal(card)
		if err != nil {
			return err
		}
		content := "---\n" + string(metadata) + "---\n" + card.Body
		raw, err = c.do(cmd, http.MethodPut, "/characters/"+args[0], map[string]any{"content": content})
		if err != nil {
			return err
		}
		return printResponse(cmd, raw, o.json)
	}}
	avatar.Flags().StringVar(&avatarFile, "file", "", "Image file; actual MIME validated by Server")
	avatar.Flags().BoolVar(&clear, "clear", false, "Clear avatar reference, keeping the asset until explicitly deleted")
	var expressionFile string
	expression := &cobra.Command{Use: "expression ID EXPRESSION --file IMAGE", Short: "Upload a named expression; inspect/delete assets with media commands", Args: countArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		for _, id := range args {
			if err := validID(id); err != nil {
				return err
			}
		}
		if expressionFile == "" {
			return usage("--file is required")
		}
		c, err := o.client()
		if err != nil {
			return err
		}

		return characterAssetUpload(cmd, o, c, args[0], expressionFile, args[1], false)
	}}
	expression.Flags().StringVar(&expressionFile, "file", "", "Image file (max 32MiB)")
	return []*cobra.Command{list, avatar, expression}
}
func characterAssetUpload(cmd *cobra.Command, o *remoteOptions, c *apiClient, id, file, expression string, avatar bool) error {
	fields := map[string]string{"character": id}
	if expression != "" {
		fields["expression"] = expression
	}
	if avatar {
		fields["avatar"] = "true"
	}
	raw, err := c.upload(cmd, "/media", file, fields, maxMediaBytes)
	if err != nil {
		return err
	}
	return printResponse(cmd, raw, o.json)
}
