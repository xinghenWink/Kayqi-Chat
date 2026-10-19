package channel

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ImageParameterPolicy 是管理员实测确认的 Images 参数边界。
type ImageParameterPolicy struct {
	Qualities          []string
	Sizes              []string
	MaxCount           int
	MaxReferenceImages int
}

// ParseImageParameterPolicy 保留只配置 image.stream 的旧模型，只有新参数声明才参与校验。
func ParseImageParameterPolicy(raw string) (*ImageParameterPolicy, error) {
	root, ok := parseCapabilities(raw)
	if !ok {
		return nil, nil
	}
	image, ok := root["image"].(map[string]any)
	if !ok {
		return nil, nil
	}
	value, present := image["parameters"]
	if !present {
		return nil, nil
	}
	parameters, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidModelCapsOverride
	}
	stringsAt := func(key string) ([]string, bool) {
		values, ok := parameters[key].([]any)
		if !ok || len(values) == 0 || len(values) > 32 {
			return nil, false
		}
		seen := map[string]bool{}
		result := make([]string, 0, len(values))
		for _, v := range values {
			s, ok := v.(string)
			if !ok || s == "" || len(s) > 32 || strings.TrimSpace(s) != s || seen[s] {
				return nil, false
			}
			seen[s] = true
			result = append(result, s)
		}
		return result, true
	}
	qualities, ok := stringsAt("qualities")
	if !ok {
		return nil, ErrInvalidModelCapsOverride
	}
	sizes, ok := stringsAt("sizes")
	if !ok {
		return nil, ErrInvalidModelCapsOverride
	}
	for _, size := range sizes {
		if size == "auto" {
			continue
		}
		parts := strings.Split(size, "x")
		if len(parts) != 2 {
			return nil, ErrInvalidModelCapsOverride
		}
		width, e1 := strconv.Atoi(parts[0])
		height, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil || width < 64 || height < 64 || width > 8192 || height > 8192 || int64(width)*int64(height) > 16777216 {
			return nil, ErrInvalidModelCapsOverride
		}
	}
	integerAt := func(key string, min, max int) (int, bool) {
		n, ok := parameters[key].(json.Number)
		if !ok {
			return 0, false
		}
		v, err := n.Int64()
		return int(v), err == nil && v >= int64(min) && v <= int64(max)
	}
	count, ok := integerAt("maxCount", 1, 4)
	if !ok {
		return nil, ErrInvalidModelCapsOverride
	}
	references, ok := integerAt("maxReferenceImages", 0, 16)
	if !ok {
		return nil, ErrInvalidModelCapsOverride
	}
	return &ImageParameterPolicy{Qualities: qualities, Sizes: sizes, MaxCount: count, MaxReferenceImages: references}, nil
}
