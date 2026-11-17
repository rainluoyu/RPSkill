package character

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/luoyu/rpskill/internal/character/contentio"
)

const MaxAssetSize int64 = 32 << 20

type AssetInfo struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Name         string `json:"name"`
	MIME         string `json:"mime"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type Asset struct {
	AssetInfo
	Data []byte `json:"-"`
}

func imageMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// ReadAsset accepts only portable paths under this character's pinned directory.
// MIME is detected from bytes, never trusted from a filename or caller header.
// Unsupported/active MIME types can be downloaded by the server, not rendered.
func ReadAsset(workspace, characterID, relativePath string) (Asset, error) {
	if !ValidID(characterID) {
		return Asset{}, fmt.Errorf("invalid character ID %q", characterID)
	}
	if err := contentio.Relative(relativePath); err != nil {
		return Asset{}, err
	}
	if relativePath == "card.md" {
		return Asset{}, errors.New("card.md is not a media asset")
	}
	root, err := contentio.Open(workspace)
	if err != nil {
		return Asset{}, err
	}
	defer root.Close()
	folder, err := contentio.Directory(root, path.Join("characters", characterID))
	if err != nil {
		return Asset{}, err
	}
	defer folder.Close()
	asset, err := readAsset(folder, relativePath)
	if err != nil {
		return Asset{}, err
	}
	asset.Path = path.Join("characters", characterID, relativePath)
	return asset, nil
}

func readAsset(folder *os.Root, relativePath string) (Asset, error) {
	data, err := contentio.ReadFile(folder, relativePath, MaxAssetSize)
	if err != nil {
		return Asset{}, err
	}
	mime := http.DetectContentType(data)
	if strings.HasPrefix(relativePath, "expressions/") && !imageMIME(mime) {
		return Asset{}, fmt.Errorf("expression %q must be a supported raster image", relativePath)
	}
	return Asset{AssetInfo: AssetInfo{RelativePath: relativePath, Name: path.Base(relativePath), MIME: mime, Size: int64(len(data)), SHA256: contentio.Hash(data)}, Data: data}, nil
}

func scanAssets(folder *os.Root, prefix string, depth int) ([]AssetInfo, error) {
	if depth > 16 {
		return nil, errors.New("character assets exceed 16 directory levels")
	}
	entries, err := contentio.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	result := []AssetInfo{}
	for _, entry := range entries {
		relative := path.Join(prefix, entry.Name())
		if err := contentio.Relative(relative); err != nil {
			return nil, err
		}
		if prefix == "" && entry.Name() == "card.md" {
			continue
		}
		info, err := folder.Lstat(entry.Name())
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("character asset is a symbolic link: %s", relative)
		}
		if info.IsDir() {
			sub, err := contentio.Directory(folder, entry.Name())
			if err != nil {
				return nil, err
			}
			assets, readErr := scanAssets(sub, relative, depth+1)
			sub.Close()
			if readErr != nil {
				return nil, readErr
			}
			result = append(result, assets...)
		} else {
			asset, err := readAsset(folder, entry.Name())
			if err != nil {
				return nil, fmt.Errorf("asset %s: %w", relative, err)
			}
			asset.RelativePath = relative
			if strings.HasPrefix(relative, "expressions/") && !imageMIME(asset.MIME) {
				return nil, fmt.Errorf("expression %q must be a supported raster image", relative)
			}
			result = append(result, asset.AssetInfo)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result, nil
}
