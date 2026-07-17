package scheduler

import (
	"fmt"
	"strings"
)

// ArgsReplaceDelivery reports whether args fully set or clear delivery
// (delivery object or routing shorthand). channel alone is not enough.
// Empty-string shorthand keys are ignored (do not wipe delivery by accident).
func ArgsReplaceDelivery(args map[string]interface{}) bool {
	if args == nil {
		return false
	}
	if _, ok := args["delivery"]; ok {
		return true
	}
	for _, k := range []string{
		"group_id", "delivery_group_id",
		"group_name", "delivery_group_name",
		"user_id", "delivery_user_id",
	} {
		if argHasNonEmptyValue(args, k) {
			return true
		}
	}
	return false
}

func argHasNonEmptyValue(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	// Non-string presence (e.g. numbers) counts as set.
	return true
}

// ArgsPartialDeliveryPatch reports fail_on_error / mention-only updates
// that must merge into existing delivery instead of wiping it.
func ArgsPartialDeliveryPatch(args map[string]interface{}) bool {
	if args == nil {
		return false
	}
	for _, k := range []string{"fail_on_error", "mention_all", "mention_user_ids"} {
		if _, ok := args[k]; ok {
			return true
		}
	}
	return false
}

// ArgsTouchDelivery is true when any delivery-related field is present.
func ArgsTouchDelivery(args map[string]interface{}) bool {
	if ArgsReplaceDelivery(args) || ArgsPartialDeliveryPatch(args) {
		return true
	}
	if args == nil {
		return false
	}
	if _, ok := args["delivery_channel"]; ok {
		return true
	}
	return false
}

// PrepareDeliveryForUpdate mutates args so Manager.Update receives a coherent
// delivery field. parseReplace builds/resolves a full delivery when replacing
// (may return nil to clear). current is the task's existing delivery (may be nil).
func PrepareDeliveryForUpdate(current *TaskDelivery, args map[string]interface{}, parseReplace func(map[string]interface{}) (*TaskDelivery, error)) error {
	if args == nil {
		return nil
	}
	if ArgsReplaceDelivery(args) {
		if parseReplace == nil {
			return fmt.Errorf("scheduler: delivery replace requires parseReplace")
		}
		d, err := parseReplace(args)
		if err != nil {
			return err
		}
		// Explicit delivery:null / disabled / empty shorthand → clear.
		// Use untyped nil so Manager.Update sees raw==nil (not (*TaskDelivery)(nil)).
		if d == nil {
			args["delivery"] = nil
		} else {
			args["delivery"] = d
		}
		for _, k := range []string{
			"group_id", "delivery_group_id", "group_name", "delivery_group_name",
			"user_id", "delivery_user_id", "mention_user_ids", "mention_all",
			"fail_on_error", "delivery_channel",
		} {
			delete(args, k)
		}
		return nil
	}
	if !ArgsPartialDeliveryPatch(args) {
		return nil
	}
	if current == nil || !current.Enabled {
		// No delivery to patch — drop partial flags (do not invent empty delivery).
		delete(args, "fail_on_error")
		delete(args, "mention_all")
		delete(args, "mention_user_ids")
		return nil
	}
	cp := *current
	cp.Targets = append([]DeliveryTarget(nil), current.Targets...)
	// Deep-copy mention slices so we never mutate the live task under callers.
	for i := range cp.Targets {
		if len(cp.Targets[i].MentionUserIDs) > 0 {
			cp.Targets[i].MentionUserIDs = append([]string(nil), cp.Targets[i].MentionUserIDs...)
		}
	}
	if fo, ok := coerceBool(args["fail_on_error"]); ok {
		cp.FailOnError = fo
	}
	if ma, ok := coerceBool(args["mention_all"]); ok {
		for i := range cp.Targets {
			if cp.Targets[i].Kind == DeliveryKindGroup {
				cp.Targets[i].MentionAll = ma
				if ma {
					cp.Targets[i].MentionUserIDs = nil
				}
			}
		}
	}
	if _, ok := args["mention_user_ids"]; ok {
		ids := coerceStringList(args["mention_user_ids"])
		for i := range cp.Targets {
			if cp.Targets[i].Kind == DeliveryKindGroup && !cp.Targets[i].MentionAll {
				cp.Targets[i].MentionUserIDs = append([]string(nil), ids...)
			}
		}
	}
	cp.Normalize()
	if err := cp.Validate(); err != nil {
		return err
	}
	args["delivery"] = &cp
	delete(args, "fail_on_error")
	delete(args, "mention_all")
	delete(args, "mention_user_ids")
	return nil
}

func coerceBool(v interface{}) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	}
	return false, false
}

func coerceStringList(v interface{}) []string {
	var ids []string
	switch x := v.(type) {
	case string:
		for _, p := range strings.FieldsFunc(x, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\n' || r == '\t'
		}) {
			p = strings.TrimSpace(p)
			if p != "" {
				ids = append(ids, p)
			}
		}
	case []interface{}:
		for _, el := range x {
			if s, ok := el.(string); ok && strings.TrimSpace(s) != "" {
				ids = append(ids, strings.TrimSpace(s))
			}
		}
	case []string:
		for _, s := range x {
			if strings.TrimSpace(s) != "" {
				ids = append(ids, strings.TrimSpace(s))
			}
		}
	}
	return ids
}
