package health

import "reflect"

// CloneReport returns a deep copy of a report and its JSON-like detail values.
func CloneReport(report *Report) *Report {
	if report == nil {
		return nil
	}

	cloned := *report
	cloned.CheckResults = make([]CheckResult, len(report.CheckResults))
	for i, result := range report.CheckResults {
		cloned.CheckResults[i] = result
		cloned.CheckResults[i].Details = cloneHealthMap(result.Details)
	}
	cloned.Metadata = cloneHealthMap(report.Metadata)
	return &cloned
}

func cloneHealthMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}

	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = cloneHealthValue(value)
	}
	return cloned
}

func cloneHealthValue(value interface{}) interface{} {
	cloned := cloneHealthReflectValue(reflect.ValueOf(value))
	if !cloned.IsValid() {
		return nil
	}
	return cloned.Interface()
}

func cloneHealthReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type()).Elem()
		cloned.Set(cloneHealthReflectValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			cloned.SetMapIndex(iterator.Key(), cloneHealthReflectValue(iterator.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHealthReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHealthReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneHealthReflectValue(value.Elem()))
		return cloned
	default:
		return value
	}
}
