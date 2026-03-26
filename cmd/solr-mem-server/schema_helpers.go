package main

func NewObjectSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func prop(typ, desc string) map[string]any {
	return map[string]any{
		"type":        typ,
		"description": desc,
	}
}

func arrayPropSchema(items map[string]any, desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": desc,
		"items":       items,
	}
}

func integerProp(desc string, min, max *int) map[string]any {
	p := prop("integer", desc)
	if min != nil {
		p["minimum"] = *min
	}
	if max != nil {
		p["maximum"] = *max
	}
	return p
}

func numberProp(desc string, min, max *float64) map[string]any {
	p := prop("number", desc)
	if min != nil {
		p["minimum"] = *min
	}
	if max != nil {
		p["maximum"] = *max
	}
	return p
}

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}
