package ruleengine

import "testing"

func TestCloneRuleValueHandlesNilAndCompositeValues(t *testing.T) {
	if got := cloneRuleValue(nil); got != nil {
		t.Fatalf("nil clone = %#v, want nil", got)
	}

	var nilMap map[string]interface{}
	if got := cloneRuleValue(nilMap).(map[string]interface{}); got != nil {
		t.Fatalf("nil map clone = %#v, want nil", got)
	}
	var nilSlice []interface{}
	if got := cloneRuleValue(nilSlice).([]interface{}); got != nil {
		t.Fatalf("nil slice clone = %#v, want nil", got)
	}
	var nilPointer *map[string]interface{}
	if got := cloneRuleValue(nilPointer).(*map[string]interface{}); got != nil {
		t.Fatalf("nil pointer clone = %#v, want nil", got)
	}

	array := [1]map[string]interface{}{{"value": "original"}}
	clonedArray := cloneRuleValue(array).([1]map[string]interface{})
	clonedArray[0]["value"] = "changed"
	if array[0]["value"] != "original" {
		t.Fatalf("array source was mutated: %#v", array)
	}

	pointed := map[string]interface{}{"nested": []interface{}{"original"}}
	clonedPointer := cloneRuleValue(&pointed).(*map[string]interface{})
	(*clonedPointer)["nested"].([]interface{})[0] = "changed"
	if pointed["nested"].([]interface{})[0] != "original" {
		t.Fatalf("pointer source was mutated: %#v", pointed)
	}
}
