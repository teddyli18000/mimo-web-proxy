package prompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DSML tag constants
const (
	DSMLOpen  = "<" + PIPE + "DSML" + PIPE + "tool_calls>"
	DSMLClose = "</" + PIPE + "DSML" + PIPE + "tool_calls>"
	DSMLInvokeOpenTmpl = "<" + PIPE + "DSML" + PIPE + "invoke name=\"%s\">"
	DSMLInvokeClose    = "</" + PIPE + "DSML" + PIPE + "invoke>"
	DSMLParamOpenTmpl  = "<" + PIPE + "DSML" + PIPE + "parameter name=\"%s\">"
	DSMLParamClose     = "</" + PIPE + "DSML" + PIPE + "parameter>"
)

// FormatToolCallsForPrompt renders OpenAI tool_calls as DSML XML for prompt history.
// Handles both OpenAI format (function.name/arguments) and Anthropic format (name/input).
func FormatToolCallsForPrompt(toolCalls interface{}) string {
	calls, ok := toolCalls.([]interface{})
	if !ok || len(calls) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(DSMLOpen)
	sb.WriteString("\n")

	for _, call := range calls {
		cm, ok := call.(map[string]interface{})
		if !ok {
			continue
		}
		sb.WriteString(formatSingleToolCall(cm))
	}

	sb.WriteString(DSMLClose)
	return sb.String()
}

func formatSingleToolCall(call map[string]interface{}) string {
	// Extract name: call.name or call.function.name
	name := extractString(call, "name")
	if name == "" {
		if fn, ok := call["function"].(map[string]interface{}); ok {
			name = extractString(fn, "name")
		}
	}

	// Extract arguments: call.arguments, call.input, or call.function.arguments
	var args interface{}
	if a, ok := call["arguments"]; ok {
		args = a
	} else if a, ok := call["input"]; ok {
		args = a
	} else if fn, ok := call["function"].(map[string]interface{}); ok {
		if a, ok := fn["arguments"]; ok {
			args = a
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  "+DSMLInvokeOpenTmpl+"\n", name))

	// Parse arguments if string (JSON)
	if argsStr, ok := args.(string); ok {
		var parsed interface{}
		if json.Unmarshal([]byte(argsStr), &parsed) == nil {
			args = parsed
		}
	}

	// Render parameters
	switch v := args.(type) {
	case map[string]interface{}:
		sb.WriteString(renderParameters(v, "    "))
	case nil:
		// no parameters
	default:
		// Wrap as single content parameter
		sb.WriteString(fmt.Sprintf("    "+DSMLParamOpenTmpl+"<![CDATA[%v]]>"+DSMLParamClose+"\n", "content", v))
	}

	sb.WriteString("  " + DSMLInvokeClose + "\n")
	return sb.String()
}

func renderParameters(params map[string]interface{}, indent string) string {
	var sb strings.Builder
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := params[k]
		renderParam(&sb, k, v, indent)
	}
	return sb.String()
}

func renderParam(sb *strings.Builder, name string, value interface{}, indent string) {
	switch v := value.(type) {
	case map[string]interface{}:
		sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"\n", name))
		sb.WriteString(renderParameters(v, indent+"  "))
		sb.WriteString(indent + DSMLParamClose + "\n")
	case []interface{}:
		sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"\n", name))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				sb.WriteString(fmt.Sprintf(indent+"  "+DSMLParamOpenTmpl+"\n", "item"))
				sb.WriteString(renderParameters(m, indent+"    "))
				sb.WriteString(indent + "  " + DSMLParamClose + "\n")
			} else {
				sb.WriteString(fmt.Sprintf(indent+"  "+DSMLParamOpenTmpl+"<![CDATA[%v]]>"+DSMLParamClose+"\n", "item", item))
			}
		}
		sb.WriteString(indent + DSMLParamClose + "\n")
	case string:
		sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"<![CDATA[%s]]>"+DSMLParamClose+"\n", name, v))
	case float64:
		if v == float64(int(v)) {
			sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"%d"+DSMLParamClose+"\n", name, int(v)))
		} else {
			sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"%g"+DSMLParamClose+"\n", name, v))
		}
	case bool:
		sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"%t"+DSMLParamClose+"\n", name, v))
	case nil:
		sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"null"+DSMLParamClose+"\n", name))
	default:
		sb.WriteString(fmt.Sprintf(indent+DSMLParamOpenTmpl+"<![CDATA[%v]]>"+DSMLParamClose+"\n", name, v))
	}
}

func extractString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
