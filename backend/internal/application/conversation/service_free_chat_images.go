package conversation

import "strings"

// textOnlyCallOptions 禁止免费聊天通过原生工具或输出模态重新进入图片生成路径。
// 图片理解所需的输入图片不受影响，只有输出图片能力被移除。
func (s *Service) textOnlyCallOptions(options map[string]any) map[string]any {
	if s.pointsSvc == nil {
		return options
	}
	result := make(map[string]any, len(options))
	for key, value := range options {
		result[key] = value
	}
	if tools, ok := result["tools"].([]any); ok {
		kept := make([]any, 0, len(tools))
		for _, tool := range tools {
			entry, ok := tool.(map[string]any)
			if !ok {
				kept = append(kept, tool)
				continue
			}
			kind, _ := entry["type"].(string)
			kind = strings.ToLower(kind)
			if strings.Contains(kind, "image_generation") || strings.Contains(kind, "generate_image") || kind == "image_edit" {
				continue
			}
			kept = append(kept, tool)
		}
		result["tools"] = kept
	}
	for _, key := range []string{"responseModalities", "response_modalities"} {
		if _, present := result[key]; present {
			result[key] = []string{"TEXT"}
		}
	}
	for _, key := range []string{"modalities", "output_modalities"} {
		if _, present := result[key]; present {
			result[key] = []string{"text"}
		}
	}
	delete(result, "image_generation")
	delete(result, "imageConfig")
	delete(result, "image_config")
	if format, ok := result["response_format"].(map[string]any); ok && format["type"] == "image" {
		delete(result, "response_format")
	}
	for _, key := range []string{"generationConfig", "generation_config"} {
		if original, ok := result[key].(map[string]any); ok {
			config := make(map[string]any, len(original))
			for name, value := range original {
				config[name] = value
			}
			result[key] = config
			delete(config, "imageConfig")
			delete(config, "image_config")
			if _, exists := config["responseModalities"]; exists {
				config["responseModalities"] = []string{"TEXT"}
			}
			if _, exists := config["response_modalities"]; exists {
				config["response_modalities"] = []string{"TEXT"}
			}
		}
	}
	return result
}
