package types

import (
	"encoding/json"
	"strings"

	"github.com/totegamma/concurrent/core"
)

type RawApObj struct {
	data map[string]any
}

func LoadAsRawApObj(jsonStr string) (*RawApObj, error) {
	var data map[string]any
	err := json.Unmarshal([]byte(jsonStr), &data)
	return &RawApObj{data}, err
}

func (r *RawApObj) Print() {
	core.JsonPrint("RawApObj", r.data)
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
	return &RawApObj{value.(map[string]any)}, true
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
