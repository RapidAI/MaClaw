package main

type pendingVisibleArtifacts struct {
	LocalPreviewPath      string
	LocalPreviewThumbnail string
	QRCodeURL             string
}

func (a *pendingVisibleArtifacts) Attach(resp *IMAgentResponse) {
	if a == nil {
		return
	}
	attachLocalPreview(resp, a.LocalPreviewPath, a.LocalPreviewThumbnail)
	if a.QRCodeURL != "" {
		appendVisibleNote(resp, "二维码登录链接："+a.QRCodeURL)
	}
}
