package toolcall

import (
	"fmt"
	"regexp"
	"strings"
)

var strictFunctionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ValidateStrictFunctionTools(toolsRaw any) error {
	tools := normalizeToolList(toolsRaw)
	if len(tools) == 0 {
		return nil
	}
	functions := make([]map[string]any, 0, len(tools))
	anyStrict := false
	for _, tool := range tools {
		typ := strings.ToLower(strings.TrimSpace(asSchemaString(tool["type"])))
		if typ != "" && typ != "function" {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if len(fn) == 0 {
			fn = tool
		}
		name := strings.TrimSpace(asSchemaString(fn["name"]))
		if !strictFunctionNamePattern.MatchString(name) {
			return fmt.Errorf("function tool name %q must match ^[A-Za-z0-9_-]{1,64}$", name)
		}
		if schemaStrictBool(fn["strict"]) {
			anyStrict = true
		}
		functions = append(functions, fn)
	}
	if !anyStrict {
		return nil
	}
	for _, fn := range functions {
		name := strings.TrimSpace(asSchemaString(fn["name"]))
		if !schemaStrictBool(fn["strict"]) {
			return fmt.Errorf("strict mode requires every function tool to set strict=true")
		}
		schema, _ := fn["parameters"].(map[string]any)
		if len(schema) == 0 {
			continue
		}
		if err := validateStrictSchema(schema, "tools."+name+".parameters"); err != nil {
			return err
		}
	}
	return nil
}

func schemaStrictBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func validateStrictSchema(schema map[string]any, path string) error {
	if schema == nil {
		return nil
	}
	if _, ok := schema["oneOf"]; ok {
		return fmt.Errorf("%s.oneOf is not supported in strict mode; use anyOf", path)
	}
	if rawAnyOf, ok := schema["anyOf"].([]any); ok {
		if len(rawAnyOf) == 0 {
			return fmt.Errorf("%s.anyOf must not be empty", path)
		}
		for i, item := range rawAnyOf {
			child, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.anyOf[%d] must be an object schema", path, i)
			}
			if err := validateStrictSchema(child, fmt.Sprintf("%s.anyOf[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	}

	types := schemaTypes(schema)
	if len(types) == 0 {
		if _, ok := schema["enum"]; ok {
			return nil
		}
		if _, ok := schema["properties"]; ok {
			types = []string{"object"}
		} else if _, ok := schema["items"]; ok {
			types = []string{"array"}
		}
	}
	for _, typ := range types {
		if !strictSchemaTypeSupported(typ) {
			return fmt.Errorf("%s.type %q is not supported in strict mode", path, typ)
		}
	}
	for _, typ := range types {
		switch typ {
		case "object":
			if err := validateStrictObjectSchema(schema, path); err != nil {
				return err
			}
		case "array":
			if _, ok := schema["minItems"]; ok {
				return fmt.Errorf("%s.minItems is not supported in strict mode", path)
			}
			if _, ok := schema["maxItems"]; ok {
				return fmt.Errorf("%s.maxItems is not supported in strict mode", path)
			}
			if item, ok := schema["items"].(map[string]any); ok {
				if err := validateStrictSchema(item, path+".items"); err != nil {
					return err
				}
			}
		case "string":
			if _, ok := schema["minLength"]; ok {
				return fmt.Errorf("%s.minLength is not supported in strict mode", path)
			}
			if _, ok := schema["maxLength"]; ok {
				return fmt.Errorf("%s.maxLength is not supported in strict mode", path)
			}
		}
	}
	return nil
}

func strictSchemaTypeSupported(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "object", "string", "number", "integer", "boolean", "array":
		return true
	default:
		return false
	}
}

func validateStrictObjectSchema(schema map[string]any, path string) error {
	if v, ok := schema["additionalProperties"]; !ok || v != false {
		return fmt.Errorf("%s.additionalProperties must be false in strict mode", path)
	}
	properties := schemaProperties(schema)
	required := schemaRequired(schema)
	for name := range properties {
		if _, ok := required[name]; !ok {
			return fmt.Errorf("%s.required must include property %q in strict mode", path, name)
		}
	}
	for name, child := range properties {
		if err := validateStrictSchema(child, path+".properties."+name); err != nil {
			return err
		}
	}
	return nil
}
