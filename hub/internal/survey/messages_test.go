package survey

import (
	"sort"
	"strings"
	"testing"
)

// formatVerbs extracts printf verbs (s/d/f/...) from a format string, treating
// "%%" as an escaped percent. Used to guard zh/en tables against arg-shape drift.
func formatVerbs(s string) []string {
	var verbs []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if s[i] == '%' {
			continue
		}
		// Skip flags, width, precision until the verb letter.
		for i < len(s) && !strings.ContainsRune("vTtbcdoOqxXUeEfFgGspw", rune(s[i])) {
			i++
		}
		if i < len(s) {
			verbs = append(verbs, string(s[i]))
		}
	}
	sort.Strings(verbs)
	return verbs
}

// TestSurveyMessagesParity guards the IM message tables: zh is the fallback, so
// a missing en key silently leaks Chinese; a verb mismatch corrupts Sprintf at
// runtime ("%!d(MISSING)"). Both must be caught here, not in a group chat.
func TestSurveyMessagesParity(t *testing.T) {
	zh := surveyMessages["zh"]
	if len(zh) == 0 {
		t.Fatal("zh message table missing")
	}
	for lang, table := range surveyMessages {
		for key, s := range table {
			if strings.TrimSpace(s) == "" {
				t.Errorf("%s[%s] is empty", lang, key)
			}
		}
	}
	for key := range zh {
		if _, ok := surveyMessages["en"][key]; !ok {
			t.Errorf("key %q present in zh but missing in en", key)
		}
	}
	for key := range surveyMessages["en"] {
		if _, ok := zh[key]; !ok {
			t.Errorf("key %q present in en but missing in zh", key)
		}
	}
	for key, zhText := range zh {
		enText, ok := surveyMessages["en"][key]
		if !ok {
			continue
		}
		zv, ev := formatVerbs(zhText), formatVerbs(enText)
		if strings.Join(zv, ",") != strings.Join(ev, ",") {
			t.Errorf("key %q verb mismatch: zh %v en %v", key, zv, ev)
		}
	}
}

// TestSurveyMessagesKnownKeys ensures every msg* constant resolves in the zh
// fallback table — a typo'd constant would otherwise surface the raw key to users.
func TestSurveyMessagesKnownKeys(t *testing.T) {
	keys := []string{
		msgHelp, msgCancelDone, msgNoActiveSurvey, msgListP2P, msgListNoGroup,
		msgListEmpty, msgListHeader, msgListItem, msgListItemMeta, msgListFooter,
		msgSessionExpired, msgStatusSubmitted, msgStatusProgress, msgNeedCode,
		msgCodeNotFound, msgCodeInvalid, msgNotCollecting, msgDeadlinePassed,
		msgGroupOnly, msgNotBoundGroup, msgBusyOtherSurvey, msgNoQuestions,
		msgSubmittedEditable, msgRequiredNoSkip, msgSubmitOK, msgInvalidAnswer,
		msgStartIntro, msgResumeIntro, msgAlreadySubmitted, msgStoppedCollecting,
		msgCancelled, msgSurveyUnavailable, msgNoModifyAllowed, msgSurveyGoneEnded,
		msgBusyAnswering, msgPromptProgress, msgPromptOptional, msgPromptMulti,
		msgPromptSingle, msgPromptRating, msgPromptText, msgPromptSkipHint,
		msgPromptTailFirst, msgPromptTailPrev, msgMetaDeadline, msgMetaTarget,
		msgErrEmptyAnswer, msgErrOptionRange, msgErrAmbiguous, msgErrUnknownOption,
		msgErrRatingInt, msgErrRatingRange, msgErrRequired, msgErrTextTooLong,
		msgErrUnsupportedType,
	}
	for _, k := range keys {
		if _, ok := surveyMessages["zh"][k]; !ok {
			t.Errorf("msg constant %q has no zh entry", k)
		}
	}
}
