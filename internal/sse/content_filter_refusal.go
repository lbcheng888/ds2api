package sse

import "strings"

func hasDeepSeekContentFilterRefusal(v any) bool {
	switch x := v.(type) {
	case string:
		return isDeepSeekContentFilterRefusalText(x)
	case []any:
		for _, item := range x {
			if hasDeepSeekContentFilterRefusal(item) {
				return true
			}
		}
	case map[string]any:
		for _, value := range x {
			if hasDeepSeekContentFilterRefusal(value) {
				return true
			}
		}
	}
	return false
}

func isDeepSeekContentFilterRefusalText(text string) bool {
	normalized := strings.Join(strings.Fields(text), "")
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "系统检测到您当前输入的信息存在敏感内容") &&
		strings.Contains(normalized, "无法响应您的请求") &&
		strings.Contains(normalized, "更换其他合规话题")
}
