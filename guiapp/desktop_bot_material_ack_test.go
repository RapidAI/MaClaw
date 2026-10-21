package guiapp

import "testing"

const ackPDF = "附件是文档。\n\n[素材]\n- ~/素材/Bot 2/2608.06216-回答.pdf"

func TestDesktopBotMaterialNames(t *testing.T) {
	names := desktopBotMaterialNames(ackPDF)
	if len(names) != 1 || names[0] != "2608.06216-回答.pdf" {
		t.Fatalf("names %v", names)
	}
	if desktopBotWordsBeforeMaterials(ackPDF) != "附件是文档。" || desktopBotMaterialNames("普通消息") != nil {
		t.Fatal("split")
	}
	if !desktopBotMentionsAny("用 2608.06216-回答 写", names) || desktopBotMentionsAny("写摘要", names) {
		t.Fatal("mentions")
	}
}
