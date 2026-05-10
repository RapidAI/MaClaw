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
		appendVisibleNote(resp, "娴滃瞼娣惍浣烘瑜版洟鎽奸幒銉窗"+a.QRCodeURL)
	}
}
