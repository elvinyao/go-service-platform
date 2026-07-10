package ruleengine

import "reflect"

// CloneRuleSet returns a deep copy suitable for immutable provider snapshots.
func CloneRuleSet(in RuleSet) RuleSet {
	out := in
	out.Rules = make([]Rule, len(in.Rules))
	for i, rule := range in.Rules {
		out.Rules[i] = cloneRule(rule)
	}
	return out
}

func cloneRule(in Rule) Rule {
	out := in
	out.Conditions = make([]Condition, len(in.Conditions))
	for i, condition := range in.Conditions {
		out.Conditions[i] = condition
		out.Conditions[i].Value = cloneRuleValue(condition.Value)
	}
	out.Actions = make([]Action, len(in.Actions))
	for i, action := range in.Actions {
		out.Actions[i] = cloneAction(action)
	}
	return out
}

func cloneAction(in Action) Action {
	out := in
	if in.Params != nil {
		out.Params = cloneRuleValue(in.Params).(map[string]interface{})
	}
	return out
}

func cloneRuleValue(value interface{}) interface{} {
	cloned := cloneReflectValue(reflect.ValueOf(value))
	if !cloned.IsValid() {
		return nil
	}
	return cloned.Interface()
}

func cloneReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloneReflectValue(value.Elem()))
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			out.SetMapIndex(iterator.Key(), cloneReflectValue(iterator.Value()))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return out
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneReflectValue(value.Elem()))
		return out
	default:
		return value
	}
}
