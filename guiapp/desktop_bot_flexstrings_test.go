package guiapp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFlexStringsAcceptsStringForms(t *testing.T) {
	cases := map[string][]string{
		`["a","b"]`:      {"a", "b"},
		`"[\"x\",\"y\"]"`: {"x", "y"},
		`"桌面截图"`:         {"桌面截图"},
		`""`:              nil,
		`null`:            nil,
		`[1,"z"]`:         {"1", "z"},
	}
	for in, want := range cases {
		var f FlexStrings
		if err := json.Unmarshal([]byte(in), &f); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if !reflect.DeepEqual([]string(f), want) {
			t.Fatalf("%s: got %#v want %#v", in, f, want)
		}
	}
}
