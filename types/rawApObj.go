package types

import (
	"encoding/json"
	"strings"

	"github.com/totegamma/concurrent/util"
)

type RawApObj struct {
	data map[string]any
}

func LoadAsRawApObj(jsonBytes []byte) (*RawApObj, error) {
	var data map[string]any
	err := json.Unmarshal(jsonBytes, &data)
	return &RawApObj{data}, err
}

func (r *RawApObj) Print() {
	util.JsonPrint("RawApObj", r.data)
}

func (r *RawApObj) GetData() map[string]any {
	return r.data
}

func (r *RawApObj) get(key string) (any, bool) {
	keys := strings.Split(key, ".")
	var value any = r.data
	for _, k := range keys {
		if value == nil {
			return nil, false
		}
		var ok bool
		value, ok = value.(map[string]any)[k]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func (r *RawApObj) GetRaw(key string) (*RawApObj, bool) {
	value, ok := r.get(key)
	if !ok {
		return nil, false
	}

	if arr, ok := value.([]any); ok {
		scalar, ok := arr[0].(map[string]any)
		if !ok {
			util.JsonPrint("failed to convert raw to map", arr[0])
			return nil, false
		}
		return &RawApObj{scalar}, true
	}

	scalar, ok := value.(map[string]any)
	if !ok {
		util.JsonPrint("failed to convert raw to map", value)
		return nil, false
	}
	return &RawApObj{scalar}, true
}

func (r *RawApObj) MustGetRaw(key string) *RawApObj {
	raw, ok := r.GetRaw(key)
	if !ok {
		return nil
	}
	return raw
}

func (r *RawApObj) GetRawSlice(key string) ([]*RawApObj, bool) {
	value, ok := r.get(key)
	if !ok {
		return nil, false
	}

	arr, ok := value.([]any)
	if !ok {
		return nil, false
	}

	var result []*RawApObj
	for _, item := range arr {
		result = append(result, &RawApObj{item.(map[string]any)})
	}
	return result, true
}

func (r *RawApObj) MustGetRawSlice(key string) []*RawApObj {
	raws, ok := r.GetRawSlice(key)
	if !ok {
		return []*RawApObj{}
	}
	return raws
}

func (r *RawApObj) GetString(key string) (string, bool) {
	value, ok := r.get(key)
	if !ok {
		return "", false
	}

	if arr, ok := value.([]string); ok {
		return arr[0], true
	}

	str, ok := value.(string)
	return str, ok
}

func (r *RawApObj) MustGetString(key string) string {
	str, ok := r.GetString(key)
	if !ok {
		return ""
	}
	return str
}

func (r *RawApObj) GetStringSlice(key string) ([]string, bool) {
	value, ok := r.get(key)
	if !ok {
		return nil, false
	}

	arr, ok := value.([]any)
	if ok {
		var result []string
		for _, v := range arr {
			scalar, ok := v.(string)
			if !ok {
				return []string{}, false
			}
			result = append(result, scalar)
		}
		return result, true
	} else {
		scalar, ok := value.(string)
		if !ok {
			return []string{}, false
		}
		return []string{scalar}, true
	}
}

func (r *RawApObj) MustGetStringSlice(key string) []string {
	strs, ok := r.GetStringSlice(key)
	if !ok {
		return []string{}
	}
	return strs
}

func (r *RawApObj) GetBool(key string) (bool, bool) {
	value, ok := r.get(key)
	if !ok {
		return false, false
	}

	if arr, ok := value.([]bool); ok {
		return arr[0], true
	}

	b, ok := value.(bool)
	return b, ok
}

func (r *RawApObj) MustGetBool(key string) bool {
	b, ok := r.GetBool(key)
	if !ok {
		return false
	}
	return b
}
