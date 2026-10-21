package desktop

import (
	"fmt"
	"path"
	"sort"
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

// MaterialPeer is another bot of the same owner. The folder is resolved from
// both the id and the name, so a bot whose name sanitizes away still occupies
// its id folder.
type MaterialPeer struct {
	ID   string
	Name string
}

// MaterialsFolder picks one bot's directory under ~/素材. A usable name that
// no other bot wants is kept. A shared name, or a name that is another bot's
// id folder, falls back to this bot's id. If that id is taken too, a stable
// suffix is added. Two bots with the same display name both get their ids.
func MaterialsFolder(botID, botName string, others []MaterialPeer) string {
	all := make([]MaterialPeer, 0, 1+len(others))
	all = append(all, MaterialPeer{ID: botID, Name: botName})
	all = append(all, others...)
	preferred := make(map[string]string, len(all))
	counts := map[string]int{}
	for _, peer := range all {
		folder := preferredMaterialFolder(peer.ID, peer.Name)
		preferred[peer.ID] = folder
		counts[strings.ToLower(folder)]++
	}
	taken := map[string]struct{}{}
	assigned := map[string]string{}
	for _, peer := range all {
		folder := preferred[peer.ID]
		if counts[strings.ToLower(folder)] != 1 {
			continue
		}
		assigned[peer.ID] = folder
		taken[strings.ToLower(folder)] = struct{}{}
	}
	rest := make([]MaterialPeer, 0, len(all))
	for _, peer := range all {
		if _, ok := assigned[peer.ID]; !ok {
			rest = append(rest, peer)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].ID < rest[j].ID })
	for _, peer := range rest {
		folder := safeMaterialID(peer.ID)
		if _, ok := taken[strings.ToLower(folder)]; ok {
			folder = suffixedMaterialFolder(folder, peer.ID, taken)
		}
		assigned[peer.ID] = folder
		taken[strings.ToLower(folder)] = struct{}{}
	}
	if folder := assigned[botID]; folder != "" {
		return folder
	}
	return safeMaterialID(botID)
}

func preferredMaterialFolder(botID, botName string) string {
	id := safeMaterialID(botID)
	name := SafeMaterialName(botName)
	if name == "" || strings.EqualFold(name, id) {
		return id
	}
	return name
}

func safeMaterialID(botID string) string {
	if id := SafeMaterialName(botID); id != "" {
		return id
	}
	return "bot"
}

func suffixedMaterialFolder(base, raw string, taken map[string]struct{}) string {
	extra := SafeMaterialName(raw)
	if extra == "" || strings.EqualFold(extra, base) {
		extra = "2"
	}
	candidate := fitMaterialName(base+"-"+extra, "")
	if candidate != "" && !materialFolderTaken(candidate, taken) {
		return candidate
	}
	for n := 2; n < 100; n++ {
		candidate = fitMaterialName(fmt.Sprintf("%s-%d", base, n), "")
		if candidate != "" && !materialFolderTaken(candidate, taken) {
			return candidate
		}
	}
	return fitMaterialName(base+"-x", "")
}

func materialFolderTaken(folder string, taken map[string]struct{}) bool {
	_, ok := taken[strings.ToLower(folder)]
	return ok
}

// MaterialStoredName keeps a sanitized display name from colliding with
// another file in the same folder. The upload id is unique per attachment,
// so two screenshots that both arrive as image.png stay two files.
func MaterialStoredName(name, uploadID string) string {
	name = SafeMaterialName(name)
	uploadID = SafeMaterialName(uploadID)
	if name == "" || uploadID == "" {
		return ""
	}
	ext := path.Ext(name)
	if utf8.RuneCountInString(ext) > 16 {
		ext = ""
	}
	suffix := "-" + uploadID
	room := materialNameMax - utf8.RuneCountInString(ext) - utf8.RuneCountInString(suffix)
	if room < 1 {
		return fitMaterialName(uploadID, ext)
	}
	stem := []rune(strings.TrimSuffix(name, ext))
	if len(stem) > room {
		stem = stem[:room]
	}
	return SafeMaterialName(string(stem) + suffix + ext)
}

func fitMaterialName(stem, ext string) string {
	room := materialNameMax - utf8.RuneCountInString(ext)
	if room < 1 {
		return SafeMaterialName(ext)
	}
	runes := []rune(stem)
	if len(runes) > room {
		runes = runes[:room]
	}
	return SafeMaterialName(string(runes) + ext)
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
