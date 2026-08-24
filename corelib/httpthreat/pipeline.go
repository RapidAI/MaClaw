package httpthreat

import (
	"hash/fnv"
	"strings"
)

func CanarySelected(tenantID, siteID, sourceID string, siteTenant string) bool {
	key := strings.TrimSpace(siteID)
	if key != "" {
		if strings.TrimSpace(siteTenant) == "" || siteTenant != strings.TrimSpace(tenantID) {
			key = ""
		} else {
			key = strings.TrimSpace(tenantID) + "|" + key
		}
	}
	if key == "" {
		key = strings.TrimSpace(tenantID)
	}
	if key == "" {
		key = strings.TrimSpace(sourceID)
	}
	if key == "" {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()%100 < CanaryPercent
}

// MergeDisposition upgrades only. unknown keeps the rule action.
func MergeDisposition(ruleAction, headClass string, allowRewrite bool) (action string, used bool) {
	ruleAction = strings.TrimSpace(ruleAction)
	if ruleAction == "" {
		ruleAction = ActionAllow
	}
	if !allowRewrite {
		return ruleAction, false
	}
	headAction := ClassAction(headClass)
	if headAction == "" {
		return ruleAction, false
	}
	if ActionRank(headAction) <= ActionRank(ruleAction) {
		return ruleAction, false
	}
	return headAction, true
}

func applyPipeline(mode, tenant, site, sourceID, siteTenant, ruleClass, ruleSource, ruleAction string, pred Prediction) (class, src, action string, used bool) {
	mode = NormalizePipeline(mode)
	if mode == PipelineOff || mode == PipelineShadow || !HeadMayScore(ruleSource) {
		return ruleClass, ruleSource, ruleAction, false
	}
	if mode == PipelineCanary && !CanarySelected(tenant, site, sourceID, siteTenant) {
		return ruleClass, ruleSource, ruleAction, false
	}
	if pred.Class == ClassUnknown || pred.MaxP < DefaultTau {
		return ruleClass, ruleSource, ruleAction, false
	}
	action, used = MergeDisposition(ruleAction, pred.Class, true)
	if !used {
		return ruleClass, ruleSource, ruleAction, false
	}
	return pred.Class, SourceHead, action, true
}
