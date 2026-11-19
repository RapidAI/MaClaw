package guiapp

import (
	"math"
	"reflect"
	"testing"
)

func TestFlattenSQLiteValueFieldsRejectsCyclesAndNonFiniteNumbers(t *testing.T) {
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic
	if _, err := flattenSQLiteValueFields(cyclic); err == nil {
		t.Fatal("cyclic map should return an error instead of overflowing the stack")
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := flattenSQLiteValueFields(value); err == nil {
			t.Fatalf("non-finite number %v should be rejected", value)
		}
		if _, ok := sqliteMessageNumber(value); ok {
			t.Fatalf("non-finite timestamp %v should be rejected", value)
		}
	}
}

func TestSQLiteValueFieldsRoundTripComplexValue(t *testing.T) {
	value := map[string]interface{}{
		"0": "numeric map key",
		"items": []interface{}{
			map[string]interface{}{"name": "first", "done": true},
			float64(2),
			nil,
		},
		"emptyMap":   map[string]interface{}{},
		"emptyArray": []interface{}{},
	}
	fields, err := flattenSQLiteValueFields(value)
	if err != nil {
		t.Fatal(err)
	}
	for index := range fields {
		fields[index].Path = sqliteMapFieldPath("", "root") + fields[index].Path
	}
	restored, err := unflattenSQLiteValueFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored["root"]; !reflect.DeepEqual(got, value) {
		t.Fatalf("round trip = %#v, want %#v", got, value)
	}
}
