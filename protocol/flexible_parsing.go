package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// Security limits to prevent DoS attacks
const (
	MaxStringLength = 10 * 1024 * 1024 // 10MB max string
	MaxSliceLength  = 100000           // 100k max slice elements
	MaxMapSize      = 50000            // 50k max map entries
	MaxDepth        = 100              // Max nesting depth
)

// setFieldValue sets a reflect.Value with type conversion
func setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}

	sourceValue := reflect.ValueOf(value)
	fieldType := field.Type()

	// Handle direct assignment if types match
	if sourceValue.Type().AssignableTo(fieldType) {
		field.Set(sourceValue)
		return nil
	}

	// Handle type conversions
	switch fieldType.Kind() {
	case reflect.String:
		if str, ok := value.(string); ok {
			// Security: Enforce string length limits
			if err := validateStringLength(str); err != nil {
				return err
			}
			field.SetString(str)
		} else {
			// Convert to string representation with size limit
			str := fmt.Sprintf("%v", value)
			if err := validateStringLength(str); err != nil {
				return err
			}
			field.SetString(str)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if num, ok := convertToInt64(value); ok {
			field.SetInt(num)
		} else {
			return fmt.Errorf("cannot convert %T to %s", value, fieldType.Kind())
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if num, ok := convertToUint64(value); ok {
			field.SetUint(num)
		} else {
			return fmt.Errorf("cannot convert %T to %s", value, fieldType.Kind())
		}
	case reflect.Float32, reflect.Float64:
		if num, ok := convertToFloat64(value); ok {
			field.SetFloat(num)
		} else {
			return fmt.Errorf("cannot convert %T to %s", value, fieldType.Kind())
		}
	case reflect.Bool:
		if b, ok := value.(bool); ok {
			field.SetBool(b)
		} else {
			return fmt.Errorf("cannot convert %T to bool", value)
		}
	case reflect.Interface:
		field.Set(sourceValue)
	case reflect.Map:
		// Security: Validate map size before processing
		if err := validateMapSize(value); err != nil {
			return err
		}
		if sourceValue.Type().AssignableTo(fieldType) {
			field.Set(sourceValue)
		} else {
			// Try to convert map types
			return convertMap(field, value)
		}
	case reflect.Slice:
		// Security: Validate slice size before processing
		if err := validateSliceSize(value); err != nil {
			return err
		}
		return convertSlice(field, value)
	default:
		// For complex types, try JSON marshal/unmarshal as fallback
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("cannot marshal value for field conversion: %w", err)
		}

		newValue := reflect.New(fieldType)
		if err := json.Unmarshal(jsonBytes, newValue.Interface()); err != nil {
			return fmt.Errorf("cannot unmarshal value for field conversion: %w", err)
		}

		field.Set(newValue.Elem())
	}

	return nil
}

// Helper functions for type conversion

func equalFold(s1, s2 string) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i := 0; i < len(s1); i++ {
		c1, c2 := s1[i], s2[i]
		if c1 != c2 {
			// Convert to lowercase
			if 'A' <= c1 && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if 'A' <= c2 && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				return false
			}
		}
	}
	return true
}

func convertToInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		// Check for overflow before conversion
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		// Check for overflow before conversion
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case float32:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

func convertToUint64(value interface{}) (uint64, bool) {
	switch v := value.(type) {
	case int:
		if v >= 0 {
			return uint64(v), true
		}
	case int8:
		if v >= 0 {
			return uint64(v), true
		}
	case int16:
		if v >= 0 {
			return uint64(v), true
		}
	case int32:
		if v >= 0 {
			return uint64(v), true
		}
	case int64:
		if v >= 0 {
			return uint64(v), true
		}
	case uint:
		return uint64(v), true
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case float32:
		if v >= 0 {
			return uint64(v), true
		}
	case float64:
		if v >= 0 {
			return uint64(v), true
		}
	}
	return 0, false
}

func convertToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func convertMap(field reflect.Value, value interface{}) error {
	sourceMap, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected map[string]interface{}, got %T", value)
	}

	mapType := field.Type()
	newMap := reflect.MakeMap(mapType)

	for k, v := range sourceMap {
		keyValue := reflect.ValueOf(k)
		valueValue := reflect.ValueOf(v)

		if keyValue.Type().AssignableTo(mapType.Key()) &&
			valueValue.Type().AssignableTo(mapType.Elem()) {
			newMap.SetMapIndex(keyValue, valueValue)
		}
	}

	field.Set(newMap)
	return nil
}

func convertSlice(field reflect.Value, value interface{}) error {
	sourceSlice := reflect.ValueOf(value)
	if sourceSlice.Kind() != reflect.Slice {
		return fmt.Errorf("expected slice, got %T", value)
	}

	sliceType := field.Type()
	newSlice := reflect.MakeSlice(sliceType, sourceSlice.Len(), sourceSlice.Len())

	for i := 0; i < sourceSlice.Len(); i++ {
		sourceElem := sourceSlice.Index(i)
		targetElem := newSlice.Index(i)

		if sourceElem.Type().AssignableTo(sliceType.Elem()) {
			targetElem.Set(sourceElem)
		} else {
			// Handle struct conversion in slices
			elemType := sliceType.Elem()
			if elemType.Kind() == reflect.Struct {
				// Create a new instance of the target type
				newElem := reflect.New(elemType)

				// Convert using mapToStruct if source is a map
				if sourceMap, ok := sourceElem.Interface().(map[string]interface{}); ok {
					if err := mapToStruct(sourceMap, newElem.Interface()); err != nil {
						return fmt.Errorf("failed to convert slice element %d: %w", i, err)
					}
				} else {
					// Fallback to JSON marshal/unmarshal for other types
					if err := setFieldValue(newElem.Elem(), sourceElem.Interface()); err != nil {
						return fmt.Errorf("failed to convert slice element %d: %w", i, err)
					}
				}

				targetElem.Set(newElem.Elem())
			} else {
				// For non-struct types, use the existing setFieldValue
				if err := setFieldValue(targetElem, sourceElem.Interface()); err != nil {
					return fmt.Errorf("failed to convert slice element %d: %w", i, err)
				}
			}
		}
	}

	field.Set(newSlice)
	return nil
}

// ValidateAndParseParams validates required fields and then flexibly parses parameters
func ValidateAndParseParams(params interface{}, target interface{}, required []string) error {
	if params == nil {
		if len(required) > 0 {
			return fmt.Errorf("missing required parameters: %v", required)
		}
		return nil
	}

	// First extract the raw map to check required fields
	var raw map[string]interface{}
	switch p := params.(type) {
	case map[string]interface{}:
		raw = p
	case json.RawMessage:
		if err := json.Unmarshal(p, &raw); err != nil {
			return fmt.Errorf("invalid JSON parameters: %w", err)
		}
	case []byte:
		if err := json.Unmarshal(p, &raw); err != nil {
			return fmt.Errorf("invalid JSON parameters: %w", err)
		}
	case string:
		if err := json.Unmarshal([]byte(p), &raw); err != nil {
			return fmt.Errorf("invalid JSON parameters: %w", err)
		}
	default:
		// For other types, try to marshal first
		jsonBytes, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("cannot process parameters of type %T: %w", params, err)
		}
		if err := json.Unmarshal(jsonBytes, &raw); err != nil {
			return fmt.Errorf("invalid parameter structure: %w", err)
		}
	}

	// Check required fields with case-insensitive matching
	for _, requiredField := range required {
		found := false
		for key := range raw {
			if equalFold(key, requiredField) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing required parameter: %s", requiredField)
		}
	}

	// Now parse with flexible handling
	return FlexibleParseParams(params, target)
}

// Security validation functions

// validateStringLength enforces maximum string length limits
func validateStringLength(s string) error {
	if len(s) > MaxStringLength {
		return fmt.Errorf("string length %d exceeds maximum %d", len(s), MaxStringLength)
	}
	return nil
}

// validateSliceSize enforces maximum slice length limits
func validateSliceSize(value interface{}) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice {
		return nil // Not a slice, skip validation
	}

	if rv.Len() > MaxSliceLength {
		return fmt.Errorf("slice length %d exceeds maximum %d", rv.Len(), MaxSliceLength)
	}
	return nil
}

// validateMapSize enforces maximum map size limits
func validateMapSize(value interface{}) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map {
		return nil // Not a map, skip validation
	}

	if rv.Len() > MaxMapSize {
		return fmt.Errorf("map size %d exceeds maximum %d", rv.Len(), MaxMapSize)
	}
	return nil
}
