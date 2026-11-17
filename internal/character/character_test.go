package character_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/character"
)

const validCard = "---\nenabled: true\navatar: avatar.png\n---\n# 身份\n一位剑士。\n"

func tempWorkspace(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func write(t *testing.T, workspace, relative string, data []byte) {
	t.Helper()
	target := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func picture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func setup(t *testing.T) string {
	t.Helper()
	workspace := tempWorkspace(t)
	write(t, workspace, "characters/alice/card.md", []byte(validCard))
	write(t, workspace, "characters/alice/avatar.png", picture(t))
	return workspace
}

func TestParseStrictCard(t *testing.T) {
	cases := map[string]string{
		"duplicate":           strings.Replace(validCard, "enabled: true", "enabled: true\nenabled: false", 1),
		"unknown":             strings.Replace(validCard, "enabled: true", "enabled: true\nsecret: yes", 1),
		"retired id":          strings.Replace(validCard, "enabled: true", "id: alice\nenabled: true", 1),
		"retired name":        strings.Replace(validCard, "enabled: true", "name: Alice\nenabled: true", 1),
		"missing enabled":     strings.Replace(validCard, "enabled: true\n", "", 1),
		"quoted bool":         strings.Replace(validCard, "enabled: true", "enabled: \"true\"", 1),
		"null bool":           strings.Replace(validCard, "enabled: true", "enabled: null", 1),
		"sequence":            strings.Replace(validCard, "avatar: avatar.png", "avatar: [avatar.png]", 1),
		"missing frontmatter": "# Alice\n",
		"missing end":         "---\nenabled: true\n",
		"empty frontmatter":   "---\n---\n",
		"anchor":              strings.Replace(validCard, "avatar: avatar.png", "avatar: &a avatar.png", 1),
		"merge":               "---\nenabled: true\n<<: {avatar: avatar.png}\n---\n",
		"avatar traversal":    strings.Replace(validCard, "avatar.png", "../bob/avatar.png", 1),
		"avatar absolute":     strings.Replace(validCard, "avatar.png", "/tmp/avatar.png", 1),
		"avatar drive":        strings.Replace(validCard, "avatar.png", "C:/avatar.png", 1),
		"avatar separator":    strings.Replace(validCard, "avatar.png", `..\bob\avatar.png`, 1),
		"nul":                 validCard + "\x00", "invalid UTF8": validCard + string([]byte{0xff}),
		"oversized": validCard + strings.Repeat("x", 4<<20),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := character.Parse([]byte(content), "alice"); err == nil {
				t.Fatal("accepted invalid card")
			}
		})
	}
	for _, content := range []string{validCard, strings.ReplaceAll(validCard, "\n", "\r\n"), strings.Replace(validCard, "avatar: avatar.png\n", "", 1)} {
		card, err := character.Parse([]byte(content), "alice")
		if err != nil {
			t.Fatal(err)
		}
		if card.ID != "alice" || card.Name != "alice" || !card.Enabled || !strings.Contains(card.Body, "剑士") {
			t.Fatalf("bad card: %+v", card)
		}
	}
}

func TestReservedIDs(t *testing.T) {
	for _, id := range []string{"chat", "director", "story", "assistant", "narrator", "environment", "", "../alice", "human", "god", strings.Repeat("a", 181)} {
		if character.ValidID(id) {
			t.Errorf("accepted %q", id)
		}
	}
	for _, id := range []string{"Alice", "角色·甲", "alice", "alice-2", "a_b", "0", strings.Repeat("a", 64)} {
		if !character.ValidID(id) {
			t.Errorf("rejected %q", id)
		}
	}
}

func TestScanAssetsAndEffective(t *testing.T) {
	workspace := setup(t)
	write(t, workspace, "characters/alice/expressions/happy.png", picture(t))
	write(t, workspace, "characters/alice/audio/readme.txt", []byte("voice description"))
	write(t, workspace, "characters/bob/card.md", []byte(strings.ReplaceAll(strings.ReplaceAll(validCard, "alice", "bob"), "avatar: avatar.png\n", "")))
	docs, err := character.Scan(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Metadata.ID != "alice" || len(docs[0].Assets) != 3 {
		t.Fatalf("bad documents: %+v", docs)
	}
	sum := sha256.Sum256([]byte(validCard))
	if docs[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("card hash mismatch")
	}
	if docs[0].Path != "characters/alice/card.md" || docs[0].Content != validCard {
		t.Fatal("bad source location/content")
	}
	views := character.Effective(docs, map[string]bool{"alice": false, "bob": true})
	if views[0].Enabled || !views[1].Enabled || !docs[0].Metadata.Enabled {
		t.Fatal("override changed base or was ignored")
	}
	views[0].Assets[0].Name = "mutated"
	if docs[0].Assets[0].Name == "mutated" {
		t.Fatal("effective view aliases assets")
	}
	asset, err := character.ReadAsset(workspace, "alice", "expressions/happy.png")
	if err != nil {
		t.Fatal(err)
	}
	if asset.MIME != "image/png" || asset.Path != "characters/alice/expressions/happy.png" || asset.Size != int64(len(asset.Data)) || !bytes.Equal(asset.Data, picture(t)) {
		t.Fatalf("bad asset: %+v", asset.AssetInfo)
	}
	docs2, err := character.Scan(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(docs, docs2) {
		t.Fatal("scanning is nondeterministic")
	}
}

func TestCharacterDirectoryAndMediaValidation(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"id path mismatch": func(t *testing.T, w string) {
			write(t, w, "characters/alice/card.md", []byte(strings.Replace(validCard, "enabled: true", "id: bob\nenabled: true", 1)))
		},
		"missing avatar": func(t *testing.T, w string) {
			if err := os.Remove(filepath.Join(w, "characters/alice/avatar.png")); err != nil {
				t.Fatal(err)
			}
		},
		"non image avatar": func(t *testing.T, w string) {
			write(t, w, "characters/alice/avatar.png", []byte("<script>alert(1)</script>"))
		},
		"non image expression": func(t *testing.T, w string) {
			write(t, w, "characters/alice/expressions/happy.svg", []byte("<svg></svg>"))
		},
		"missing card": func(t *testing.T, w string) {
			if err := os.MkdirAll(filepath.Join(w, "characters/bob"), 0700); err != nil {
				t.Fatal(err)
			}
		},
		"invalid character dir": func(t *testing.T, w string) { write(t, w, "characters/Bad?/card.md", []byte(validCard)) },
		"unexpected root file":  func(t *testing.T, w string) { write(t, w, "characters/extra", []byte("x")) },
		"reserved directory":    func(t *testing.T, w string) { write(t, w, "characters/assistant/card.md", []byte(validCard)) },
		"asset trailing dot":    func(t *testing.T, w string) { write(t, w, "characters/alice/weird.", []byte("x")) },
		"asset reserved device": func(t *testing.T, w string) { write(t, w, "characters/alice/CON.txt", []byte("x")) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := setup(t)
			change(t, workspace)
			if _, err := character.Scan(workspace); err == nil {
				t.Fatal("accepted invalid workspace")
			}
		})
	}
	empty := tempWorkspace(t)
	if docs, err := character.Scan(empty); err != nil || len(docs) != 0 {
		t.Fatalf("empty workspace: %v %v", docs, err)
	}
}

func TestReadAssetRejectsTraversal(t *testing.T) {
	workspace := setup(t)
	for _, relative := range []string{"", "../alice/avatar.png", "/etc/passwd", "C:/file", "..\\alice\\avatar.png", "avatar.png/..", "./avatar.png", "avatar.png//x", "avatar.png:ads", "card.md", "NUL.png", "a\x00b", "COM¹.png"} {
		t.Run(relative, func(t *testing.T) {
			if _, err := character.ReadAsset(workspace, "alice", relative); err == nil {
				t.Fatalf("accepted %q", relative)
			}
		})
	}
	if _, err := character.ReadAsset(workspace, "../alice", "avatar.png"); err == nil {
		t.Fatal("accepted character traversal")
	}
}

func TestCharacterSymlinkEscapes(t *testing.T) {
	cases := []string{"card", "avatar", "expression", "character", "root", "asset-directory"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := setup(t)
			outside := tempWorkspace(t)
			write(t, outside, "card.md", []byte(validCard))
			write(t, outside, "image.png", picture(t))
			var target, link string
			switch name {
			case "card":
				target = filepath.Join(outside, "card.md")
				link = filepath.Join(workspace, "characters/alice/card.md")
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
			case "avatar":
				target = filepath.Join(outside, "image.png")
				link = filepath.Join(workspace, "characters/alice/avatar.png")
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
			case "expression":
				target = filepath.Join(outside, "image.png")
				link = filepath.Join(workspace, "characters/alice/expressions/happy.png")
				if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
					t.Fatal(err)
				}
			case "character":
				target = outside
				link = filepath.Join(workspace, "characters/bob")
			case "root":
				target = filepath.Join(workspace, "characters")
				link = filepath.Join(workspace, "linked")
			case "asset-directory":
				target = outside
				link = filepath.Join(workspace, "characters/alice/external")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if name == "root" {
				if _, err := character.Scan(link); err == nil {
					t.Fatal("followed workspace link")
				}
				return
			}
			if _, err := character.Scan(workspace); err == nil {
				t.Fatal("followed symlink")
			}
			if name == "avatar" {
				if _, err := character.ReadAsset(workspace, "alice", "avatar.png"); err == nil {
					t.Fatal("ReadAsset followed symlink")
				}
			}
			if name == "asset-directory" {
				if _, err := character.ReadAsset(workspace, "alice", "external/image.png"); err == nil {
					t.Fatal("ReadAsset followed directory symlink")
				}
			}
		})
	}
}

func TestScanDisabledCardStillValidates(t *testing.T) {
	workspace := setup(t)
	write(t, workspace, "characters/alice/card.md", []byte(strings.Replace(validCard, "enabled: true", "enabled: false", 1)))
	write(t, workspace, "characters/alice/avatar.png", []byte("not an image"))
	if _, err := character.Scan(workspace); err == nil {
		t.Fatal("disabled card bypassed validation")
	}
}

func TestOversizedAsset(t *testing.T) {
	workspace := setup(t)
	target := filepath.Join(workspace, "characters/alice/large.bin")
	file, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(character.MaxAssetSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, err := character.ReadAsset(workspace, "alice", "large.bin"); err == nil {
		t.Fatal("accepted oversized asset")
	}
	if _, err := character.Scan(workspace); err == nil {
		t.Fatal("scan accepted oversized asset")
	}
}

func TestValidateCardDirect(t *testing.T) {
	for _, card := range []character.Card{{ID: "chat", Name: "Chat"}, {ID: "alice", Name: ""}, {ID: "alice", Name: "A\nB"}, {ID: "alice", Name: "A", Body: string([]byte{0xff})}, {ID: "alice", Name: "A", Avatar: "../avatar.png"}} {
		if err := character.Validate(card); err == nil {
			t.Errorf("accepted %+v", card)
		}
	}
}

func FuzzParseCard(f *testing.F) {
	f.Add([]byte(validCard))
	f.Add([]byte("---\nenabled: false\n---\n"))
	f.Add([]byte("---\nid: a\nid: b\n---\n"))
	f.Fuzz(func(t *testing.T, content []byte) {
		card, err := character.Parse(content, "alice")
		if err == nil {
			if err := character.Validate(card); err != nil {
				t.Fatalf("Parse accepted invalid card: %v", err)
			}
		}
	})
}

func TestCharacterRootSymlinkRejected(t *testing.T) {
	workspace := tempWorkspace(t)
	source := setup(t)
	if err := os.Symlink(filepath.Join(source, "characters"), filepath.Join(workspace, "characters")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := character.Scan(workspace); err == nil {
		t.Fatal("followed characters symlink")
	}
	if _, err := character.ReadAsset(workspace, "alice", "avatar.png"); err == nil {
		t.Fatal("ReadAsset followed characters symlink")
	}
}
