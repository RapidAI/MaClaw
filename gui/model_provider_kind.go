package main

import "strings"

type modelProviderKind string

const (
	modelProviderUnknown      modelProviderKind = ""
	modelProviderKimi         modelProviderKind = "kimi"
	modelProviderGLM          modelProviderKind = "glm"
	modelProviderGLM47        modelProviderKind = "glm-4.7"
	modelProviderDoubao       modelProviderKind = "doubao"
	modelProviderXFYun        modelProviderKind = "xfyun"
	modelProviderMiniMax      modelProviderKind = "minimax"
	modelProviderDeepSeek     modelProviderKind = "deepseek"
	modelProviderGACCode      modelProviderKind = "gaccode"
	modelProviderTencent      modelProviderKind = "tencent"
	modelProviderTencentCloud modelProviderKind = "tencentcloud"
	modelProviderAliyun       modelProviderKind = "aliyun"
	modelProviderXiaomi       modelProviderKind = "xiaomi"
	modelProviderQianfan      modelProviderKind = "qianfan"
	modelProviderCodegen      modelProviderKind = "codegen"
)

func normalizeModelProviderKind(value string) modelProviderKind {
	switch modelProviderKind(strings.ToLower(strings.TrimSpace(value))) {
	case modelProviderKimi:
		return modelProviderKimi
	case modelProviderGLM, modelProviderGLM47:
		return modelProviderGLM
	case modelProviderDoubao:
		return modelProviderDoubao
	case modelProviderXFYun:
		return modelProviderXFYun
	case modelProviderMiniMax:
		return modelProviderMiniMax
	case modelProviderDeepSeek:
		return modelProviderDeepSeek
	case modelProviderGACCode:
		return modelProviderGACCode
	case modelProviderTencent, modelProviderTencentCloud:
		return modelProviderTencent
	case modelProviderAliyun:
		return modelProviderAliyun
	case modelProviderXiaomi:
		return modelProviderXiaomi
	case modelProviderQianfan:
		return modelProviderQianfan
	case modelProviderCodegen:
		return modelProviderCodegen
	default:
		return modelProviderUnknown
	}
}

func (kind modelProviderKind) IsDeepSeek() bool {
	return kind == modelProviderDeepSeek
}

func (kind modelProviderKind) IsQianfan() bool {
	return kind == modelProviderQianfan
}
