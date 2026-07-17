package scheduler

import (
	"testing"
)

func TestPrepareDeliveryForUpdatePartialFailOnError(t *testing.T) {
	t.Parallel()
	cur := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelTelegram,
		Targets: []DeliveryTarget{{Kind: DeliveryKindUser, UserID: "self"}},
	}
	args := map[string]interface{}{"fail_on_error": true}
	if err := PrepareDeliveryForUpdate(cur, args, nil); err != nil {
		t.Fatal(err)
	}
	d, ok := args["delivery"].(*TaskDelivery)
	if !ok || d == nil || !d.FailOnError {
		t.Fatalf("got %#v", args["delivery"])
	}
	if len(d.Targets) != 1 || d.Targets[0].UserID != "self" {
		t.Fatalf("targets wiped: %#v", d.Targets)
	}
}

func TestPrepareDeliveryForUpdateStringBool(t *testing.T) {
	t.Parallel()
	cur := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g1"}},
	}
	args := map[string]interface{}{"fail_on_error": "true"}
	if err := PrepareDeliveryForUpdate(cur, args, nil); err != nil {
		t.Fatal(err)
	}
	d := args["delivery"].(*TaskDelivery)
	if !d.FailOnError {
		t.Fatal("string true not coerced")
	}
}

func TestPrepareDeliveryForUpdateReplaceClear(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{"delivery": nil}
	if err := PrepareDeliveryForUpdate(nil, args, func(map[string]interface{}) (*TaskDelivery, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if v, ok := args["delivery"]; !ok || v != nil {
		t.Fatalf("want clear nil, got present=%v val=%#v", ok, v)
	}
}

func TestArgsReplaceDeliveryChannelAlone(t *testing.T) {
	t.Parallel()
	if ArgsReplaceDelivery(map[string]interface{}{"channel": "telegram", "name": "x"}) {
		t.Fatal("channel alone must not replace")
	}
	if !ArgsReplaceDelivery(map[string]interface{}{"user_id": "self"}) {
		t.Fatal("user_id replaces")
	}
	if ArgsReplaceDelivery(map[string]interface{}{"user_id": "", "group_id": "  "}) {
		t.Fatal("empty shorthand must not replace")
	}
	if !ArgsReplaceDelivery(map[string]interface{}{"delivery": nil}) {
		t.Fatal("delivery key (even null) replaces/clears")
	}
}
