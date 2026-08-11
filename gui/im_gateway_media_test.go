package main

import "testing"

func TestGuessMimeFromMediaRecognizesAllSixOfficeFormats(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "legacy.doc", want: "application/msword"},
		{name: "modern.docx", want: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{name: "legacy.xls", want: "application/vnd.ms-excel"},
		{name: "modern.xlsx", want: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{name: "legacy.ppt", want: "application/vnd.ms-powerpoint"},
		{name: "modern.pptx", want: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessMimeFromMedia(imMediaFile.String(), tt.name); got != tt.want {
				t.Fatalf("guessMimeFromMedia(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
