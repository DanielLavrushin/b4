package config

import (
	"reflect"
)

func ApplyDefaults(target, defaults interface{}) {
	applyDefaultsValue(reflect.ValueOf(target), reflect.ValueOf(defaults))
}

func applyDefaultsValue(target, defaults reflect.Value) {
	if target.Kind() == reflect.Ptr {
		if target.IsNil() {
			return
		}
		target = target.Elem()
	}
	if defaults.Kind() == reflect.Ptr {
		if defaults.IsNil() {
			return
		}
		defaults = defaults.Elem()
	}

	if target.Kind() != reflect.Struct || defaults.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < target.NumField(); i++ {
		targetField := target.Field(i)
		defaultField := defaults.Field(i)

		if !targetField.CanSet() {
			continue
		}

		switch targetField.Kind() {
		case reflect.Struct:
			applyDefaultsValue(targetField.Addr(), defaultField.Addr())

		case reflect.Slice:
			if targetField.IsNil() && !defaultField.IsNil() {
				newSlice := reflect.MakeSlice(targetField.Type(), defaultField.Len(), defaultField.Cap())
				reflect.Copy(newSlice, defaultField)
				targetField.Set(newSlice)
			}

		case reflect.Map:
			if targetField.IsNil() && !defaultField.IsNil() {
				newMap := reflect.MakeMap(targetField.Type())
				for _, key := range defaultField.MapKeys() {
					newMap.SetMapIndex(key, defaultField.MapIndex(key))
				}
				targetField.Set(newMap)
			}

		case reflect.String:
			if targetField.String() == "" && defaultField.String() != "" {
				targetField.Set(defaultField)
			}

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if targetField.Int() == 0 && defaultField.Int() != 0 {
				targetField.Set(defaultField)
			}

		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if targetField.Uint() == 0 && defaultField.Uint() != 0 {
				targetField.Set(defaultField)
			}

		case reflect.Float32, reflect.Float64:
			if targetField.Float() == 0 && defaultField.Float() != 0 {
				targetField.Set(defaultField)
			}

		case reflect.Bool:

		case reflect.Ptr:
			if targetField.IsNil() && !defaultField.IsNil() {
				newVal := reflect.New(targetField.Type().Elem())
				newVal.Elem().Set(defaultField.Elem())
				targetField.Set(newVal)
			}
		}
	}
}

func NewSetConfigWithDefaults() SetConfig {
	return NewSetConfig()
}

func ApplySetDefaults(set *SetConfig) {
	ApplyDefaults(set, &DefaultSetConfig)
}

func ApplySetDefaultsKeepingFakeType(set *SetConfig) {
	fakeType := set.Faking.SNIType
	ApplySetDefaults(set)
	set.Faking.SNIType = fakeType
}

func ApplyConfigDefaults(cfg *Config) {
	ApplyDefaults(cfg, &DefaultConfig)
}
