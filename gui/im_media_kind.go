package main

import "strings"

type imMediaKind string

const (
	imMediaUnknown imMediaKind = ""
	imMediaImage   imMediaKind = "image"
	imMediaVoice   imMediaKind = "voice"
	imMediaVideo   imMediaKind = "video"
	imMediaAudio   imMediaKind = "audio"
	imMediaFile    imMediaKind = "file"
)

func normalizeIMMediaKind(value string) imMediaKind {
	switch imMediaKind(strings.TrimSpace(value)) {
	case imMediaImage:
		return imMediaImage
	case imMediaVoice:
		return imMediaVoice
	case imMediaVideo:
		return imMediaVideo
	case imMediaAudio:
		return imMediaAudio
	case imMediaFile:
		return imMediaFile
	default:
		return imMediaUnknown
	}
}

func (kind imMediaKind) String() string {
	return string(kind)
}

func (kind imMediaKind) IsImage() bool {
	return kind == imMediaImage
}

func (kind imMediaKind) IsVoice() bool {
	return kind == imMediaVoice
}

func (kind imMediaKind) IsAudio() bool {
	return kind == imMediaAudio
}

type imContentBlockKind string

const (
	imContentBlockUnknown  imContentBlockKind = ""
	imContentBlockText     imContentBlockKind = "text"
	imContentBlockImage    imContentBlockKind = "image"
	imContentBlockImageURL imContentBlockKind = "image_url"
)

func normalizeIMContentBlockKind(value string) imContentBlockKind {
	switch imContentBlockKind(strings.TrimSpace(value)) {
	case imContentBlockText:
		return imContentBlockText
	case imContentBlockImage:
		return imContentBlockImage
	case imContentBlockImageURL:
		return imContentBlockImageURL
	default:
		return imContentBlockUnknown
	}
}

func (kind imContentBlockKind) IsImageBlock() bool {
	return kind == imContentBlockImage || kind == imContentBlockImageURL
}

func (kind imContentBlockKind) IsText() bool {
	return kind == imContentBlockText
}
