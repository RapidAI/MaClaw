package desktop

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Materials are files a person uploads to a bot for a task (pictures, video,
// documents). They land under MaterialsRoot/<bot folder>/ in the owner's
// desktop container, where Thunar and the bot's Chromium file picker see
// them. The folder is a tidy convention, not a security boundary: every bot
// of one person shares that person's container and Linux user.
const (
	MaterialsRoot = ContainerHome + "/素材"
	// UploadMax is the largest single material file.
	UploadMax = 200 << 20
	// UploadChunkMax is the largest decoded chunk one upload call carries.
	UploadChunkMax  = 4 << 20
	materialNameMax = 120
)

// SafeMaterialName turns a display name into one path element: no
// separators, control characters, colons or leading dots, at most 120 runes.
// It returns "" when nothing usable is left.
func SafeMaterialName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == 0 || unicode.IsControl(r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	out = strings.TrimSpace(out)
	if utf8.RuneCountInString(out) > materialNameMax {
		ext := path.Ext(out)
		if utf8.RuneCountInString(ext) > 16 {
			ext = ""
		}
		runes := []rune(strings.TrimSuffix(out, ext))
		out = string(runes[:materialNameMax-utf8.RuneCountInString(ext)]) + ext
	}
	if out == "" || out == "." || out == ".." || strings.Trim(out, "_ ") == "" {
		return ""
	}
	return out
}

// MaterialsFolder picks the folder name of one bot: its sanitized name, or
// its id when the name is unusable or another bot of the same owner would
// get the same folder. others are the names of that owner's other bots.
func MaterialsFolder(botID, botName string, others []string) string {
	id := SafeMaterialName(botID)
	name := SafeMaterialName(botName)
	if name == "" || name == id {
		return id
	}
	for _, other := range others {
		if strings.EqualFold(SafeMaterialName(other), name) {
			return id
		}
	}
	return name
}

// MaterialPath is the container path of one uploaded file.
func MaterialPath(folder, fileName string) (string, error) {
	folder = SafeMaterialName(folder)
	fileName = SafeMaterialName(fileName)
	if folder == "" || fileName == "" {
		return "", fmt.Errorf("%w: file name is invalid", ErrInvalid)
	}
	return ContainerPath(MaterialsRoot + "/" + folder + "/" + fileName)
}

// MaterialDisplayPath is the ~/ form the person and the model are shown.
func MaterialDisplayPath(containerPath string) string {
	if strings.HasPrefix(containerPath, ContainerHome+"/") {
		return "~/" + strings.TrimPrefix(containerPath, ContainerHome+"/")
	}
	return containerPath
}
