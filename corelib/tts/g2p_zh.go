package tts

import (
	"strings"
)

// charToPinyin converts a Chinese character to pinyin with tone number.
func charToPinyin(r rune) string {
	// Try full table first (21000+ entries)
	if py, ok := zhPinyinTableFull[r]; ok {
		return py
	}
	// Fallback to small table
	if py, ok := zhPinyinTable[r]; ok {
		return py
	}
	return ""
}

func charToPinyinInText(runes []rune, i int) string {
	if i < 0 || i >= len(runes) {
		return ""
	}
	if py := contextualPinyinOverride(runes, i); py != "" {
		return py
	}
	return charToPinyin(runes[i])
}

func contextualPinyinOverride(runes []rune, i int) string {
	bestPinyin := ""
	bestLen := 0
	for _, rule := range contextualPinyinRules {
		if i < rule.index || i-rule.index+len(rule.phrase) > len(runes) {
			continue
		}
		start := i - rule.index
		matched := true
		for j, r := range rule.phrase {
			if runes[start+j] != r {
				matched = false
				break
			}
		}
		if matched && len(rule.phrase) > bestLen {
			bestPinyin = rule.pinyin
			bestLen = len(rule.phrase)
		}
	}
	return bestPinyin
}

type contextualPinyinRule struct {
	phrase []rune
	index  int
	pinyin string
}

func pinyinRule(phrase string, index int, pinyin string) contextualPinyinRule {
	runes := []rune(phrase)
	if index < 0 || index >= len(runes) {
		panic("tts: contextual pinyin rule index out of range")
	}
	return contextualPinyinRule{phrase: runes, index: index, pinyin: pinyin}
}

var contextualPinyinRules = []contextualPinyinRule{
	pinyinRule("\u7761\u89c9", 1, "jiao4"),  // sleep
	pinyinRule("\u5348\u89c9", 1, "jiao4"),  // nap
	pinyinRule("\u61d2\u89c9", 1, "jiao4"),  // sleep in
	pinyinRule("\u97f3\u4e50", 1, "yue4"),   // music
	pinyinRule("\u94f6\u884c", 1, "hang2"),  // bank
	pinyinRule("\u884c\u4e1a", 0, "hang2"),  // industry
	pinyinRule("\u91cd\u5e86", 0, "chong2"), // Chongqing
	pinyinRule("\u91cd\u65b0", 0, "chong2"), // again
	pinyinRule("\u91cd\u590d", 0, "chong2"), // repeat
	pinyinRule("\u957f\u5927", 0, "zhang3"), // grow up
	pinyinRule("\u6821\u957f", 1, "zhang3"), // principal
}

// pinyinToPhonemes converts a pinyin syllable (e.g. "zhong1") to MeloTTS phonemes.
// Returns phoneme list and tone number.
func pinyinToPhonemes(pinyin string) (phonemes []string, tone int) {
	if pinyin == "" {
		return nil, 0
	}

	// Extract tone number from last character
	tone = 0
	py := pinyin
	if len(py) > 0 {
		last := py[len(py)-1]
		if last >= '1' && last <= '5' {
			tone = int(last - '0')
			py = py[:len(py)-1]
		}
	}

	// Look up in initials/finals table
	initial, final := splitPinyin(py)

	if initial != "" {
		phonemes = append(phonemes, initial)
	}
	if final != "" {
		// Map compound finals to phoneme sequences
		fps := finalToPhonemes(final)
		phonemes = append(phonemes, fps...)
	}

	if len(phonemes) == 0 {
		// Fallback: treat each character as a phoneme
		phonemes = append(phonemes, py)
	}
	return phonemes, tone
}

// splitPinyin splits a pinyin syllable into initial and final.
func splitPinyin(py string) (initial, final string) {
	py = strings.ToLower(py)

	// Two-character initials first
	if len(py) >= 2 {
		prefix2 := py[:2]
		switch prefix2 {
		case "zh", "ch", "sh":
			rest := py[2:]
			// zhi/chi/shi → special final "ir" (not "i")
			if rest == "i" {
				return prefix2, "ir"
			}
			return prefix2, rest
		}
	}

	// Single-character initials
	if len(py) >= 1 {
		c := py[0]
		switch c {
		case 'b', 'p', 'm', 'f', 'd', 't', 'n', 'l',
			'g', 'k', 'h', 'j', 'q', 'x',
			'w', 'y':
			initial = string(c)
			return initial, normalizePinyinFinal(initial, py[1:])
		case 'r':
			rest := py[1:]
			// ri → special final "ir"
			if rest == "i" {
				return "r", "ir"
			}
			return "r", rest
		case 'z', 'c', 's':
			rest := py[1:]
			// zi/ci/si → special final "i0"
			if rest == "i" {
				return string(c), "i0"
			}
			return string(c), rest
		}
	}

	// No initial (zero initial)
	return "", py
}

func normalizePinyinFinal(initial, final string) string {
	if initial != "j" && initial != "q" && initial != "x" {
		return final
	}
	switch final {
	case "u":
		return "v"
	case "uan":
		return "van"
	case "un":
		return "vn"
	case "ue":
		return "ve"
	}
	return final
}

// finalToPhonemes maps a pinyin final to MeloTTS phoneme(s).
func finalToPhonemes(final string) []string {
	// Direct mapping for common finals
	if phs, ok := zhFinalMap[final]; ok {
		return phs
	}
	// Fallback: return as single phoneme
	if final != "" {
		return []string{final}
	}
	return nil
}

// zhFinalMap maps pinyin finals to MeloTTS phoneme sequences.
var zhFinalMap = map[string][]string{
	"a":    {"a"},
	"o":    {"o"},
	"e":    {"e"},
	"i":    {"i"},
	"u":    {"u"},
	"v":    {"v"}, // ü
	"ai":   {"ai"},
	"ei":   {"ei"},
	"ao":   {"ao"},
	"ou":   {"ou"},
	"an":   {"an"},
	"en":   {"en"},
	"ang":  {"ang"},
	"eng":  {"eng"},
	"ong":  {"ong"},
	"er":   {"er"},
	"ia":   {"ia"},
	"ie":   {"ie"},
	"iao":  {"iao"},
	"iu":   {"iu"},
	"ian":  {"ian"},
	"in":   {"in"},
	"iang": {"iang"},
	"ing":  {"ing"},
	"iong": {"iong"},
	"ua":   {"ua"},
	"uo":   {"uo"},
	"uai":  {"uai"},
	"ui":   {"ui"},
	"uan":  {"uan"},
	"un":   {"un"},
	"uang": {"uang"},
	"ve":   {"ve"},
	"van":  {"van"},
	"vn":   {"vn"},
	"ir":   {"ir"}, // zhi/chi/shi/ri 的韵母
	"i0":   {"i0"}, // zi/ci/si 的韵母
}

// zhPinyinTable maps common Chinese characters to pinyin with tone.
// This is a minimal subset (~3000 most frequent characters).
// Format: character → "pinyin+tone" e.g. '你' → "ni3"
var zhPinyinTable = map[rune]string{
	// Top frequency characters (partial list — will be expanded)
	'的': "de5", '一': "yi1", '是': "shi4", '不': "bu4", '了': "le5",
	'人': "ren2", '我': "wo3", '在': "zai4", '有': "you3", '他': "ta1",
	'这': "zhe4", '中': "zhong1", '大': "da4", '来': "lai2", '上': "shang4",
	'国': "guo2", '个': "ge4", '到': "dao4", '说': "shuo1", '们': "men5",
	'为': "wei4", '子': "zi3", '和': "he2", '你': "ni3", '地': "di4",
	'出': "chu1", '会': "hui4", '时': "shi2", '要': "yao4", '也': "ye3",
	'以': "yi3", '生': "sheng1", '能': "neng2", '对': "dui4", '里': "li3",
	'就': "jiu4", '学': "xue2", '下': "xia4", '自': "zi4", '心': "xin1",
	'后': "hou4", '然': "ran2", '家': "jia1", '多': "duo1", '天': "tian1",
	'而': "er2", '本': "ben3", '去': "qu4", '行': "xing2", '前': "qian2",
	'年': "nian2", '日': "ri4", '如': "ru2", '都': "dou1", '方': "fang1",
	'成': "cheng2", '事': "shi4", '只': "zhi3", '作': "zuo4", '当': "dang1",
	'没': "mei2", '动': "dong4", '面': "mian4", '起': "qi3", '看': "kan4",
	'定': "ding4", '开': "kai1", '好': "hao3", '小': "xiao3", '部': "bu4",
	'其': "qi2", '些': "xie1", '主': "zhu3", '样': "yang4", '理': "li3",
	'新': "xin1", '明': "ming2", '实': "shi2", '意': "yi4", '正': "zheng4",
	'长': "chang2", '把': "ba3", '机': "ji1", '十': "shi2", '从': "cong2",
	'无': "wu2", '进': "jin4", '使': "shi3", '所': "suo3", '两': "liang3",
	'很': "hen3", '经': "jing1", '公': "gong1", '此': "ci3", '已': "yi3",
	'工': "gong1", '同': "tong2", '体': "ti3", '高': "gao1", '老': "lao3",
	'问': "wen4", '最': "zui4", '力': "li4",
	'三': "san1", '但': "dan4", '现': "xian4", '被': "bei4",
	'关': "guan1", '点': "dian3", '业': "ye4", '外': "wai4",
	'将': "jiang1", '与': "yu3", '想': "xiang3", '她': "ta1", '它': "ta1",
	'过': "guo4", '用': "yong4", '可': "ke3",
	'发': "fa1", '那': "na4", '什': "shen2", '么': "me5",
	'等': "deng3", '头': "tou2", '给': "gei3", '法': "fa3",
	'白': "bai2", '回': "hui2", '果': "guo3", '话': "hua4", '活': "huo2",
	'打': "da3", '呢': "ne5", '真': "zhen1", '山': "shan1", '水': "shui3",
	'笑': "xiao4", '让': "rang4", '走': "zou3",
	'吃': "chi1", '喝': "he1", '睡': "shui4", '觉': "jue2", '听': "ting1",
	'写': "xie3", '读': "du2", '买': "mai3", '卖': "mai4", '飞': "fei1",
	'跑': "pao3", '坐': "zuo4", '站': "zhan4", '住': "zhu4", '死': "si3",
	'爱': "ai4", '怕': "pa4", '快': "kuai4", '慢': "man4", '早': "zao3",
	'晚': "wan3", '左': "zuo3", '右': "you4", '东': "dong1", '西': "xi1",
	'南': "nan2", '北': "bei3", '红': "hong2", '黄': "huang2", '蓝': "lan2",
	'绿': "lv4", '黑': "hei1", '花': "hua1", '鸟': "niao3", '鱼': "yu2",
	'猫': "mao1", '狗': "gou3", '马': "ma3", '牛': "niu2", '羊': "yang2",
	'月': "yue4", '星': "xing1", '风': "feng1", '雨': "yu3", '雪': "xue3",
	'电': "dian4", '车': "che1", '船': "chuan2", '门': "men2", '窗': "chuang1",
	'桌': "zhuo1", '椅': "yi3", '书': "shu1", '笔': "bi3", '纸': "zhi3",
	'钱': "qian2", '路': "lu4", '城': "cheng2", '市': "shi4", '村': "cun1",
	'河': "he2", '海': "hai3", '湖': "hu2", '春': "chun1", '夏': "xia4",
	'秋': "qiu1", '冬': "dong1", '男': "nan2", '女': "nv3", '父': "fu4",
	'母': "mu3", '兄': "xiong1", '弟': "di4", '姐': "jie3", '妹': "mei4",
	'朋': "peng2", '友': "you3", '师': "shi1", '医': "yi1", '病': "bing4",
	'药': "yao4", '饭': "fan4", '菜': "cai4", '茶': "cha2", '酒': "jiu3",
	'米': "mi3", '肉': "rou4", '蛋': "dan4", '奶': "nai3", '糖': "tang2",
	'盐': "yan2", '油': "you2", '衣': "yi1", '裤': "ku4", '鞋': "xie2",
	'帽': "mao4", '包': "bao1", '伞': "san3", '钟': "zhong1", '表': "biao3",
	'世': "shi4", '界': "jie4", '今': "jin1", '昨': "zuo2",
	'每': "mei3", '次': "ci4", '先': "xian1",
	'还': "hai2", '又': "you4", '才': "cai2", '刚': "gang1",
	'常': "chang2", '总': "zong3", '别': "bie2", '请': "qing3", '谢': "xie4",
	'错': "cuo4", '难': "nan2", '易': "yi4", '忙': "mang2",
	'闲': "xian2", '累': "lei4", '饿': "e4", '渴': "ke3", '冷': "leng3",
	'热': "re4", '干': "gan1", '湿': "shi1", '轻': "qing1", '重': "zhong4",
	'远': "yuan3", '近': "jin4", '深': "shen1", '浅': "qian3", '厚': "hou4",
	'薄': "bao2", '宽': "kuan1", '窄': "zhai3", '胖': "pang4", '瘦': "shou4",
	'美': "mei3", '丑': "chou3", '香': "xiang1", '臭': "chou4", '甜': "tian2",
	'苦': "ku3", '酸': "suan1", '辣': "la4", '咸': "xian2",
	// 数字
	'零': "ling2", '二': "er4", '四': "si4", '五': "wu3",
	'六': "liu4", '七': "qi1", '八': "ba1", '九': "jiu3", '百': "bai3",
	'千': "qian1", '万': "wan4", '亿': "yi4",
}
