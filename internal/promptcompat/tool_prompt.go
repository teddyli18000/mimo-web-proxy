package promptcompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teddyli18000/mimo-web-proxy/internal/adapter"
	"github.com/teddyli18000/mimo-web-proxy/internal/prompt"
)

// InjectToolPrompt injects tool schemas and DSML instructions into the system message.
// This is the stateless equivalent of ds2api's injectToolPrompt.
func InjectToolPrompt(messages []prompt.Message, tools []adapter.OpenAITool) ([]prompt.Message, []string) {
	if len(tools) == 0 {
		return messages, nil
	}

	var toolNames []string
	var toolDescriptions strings.Builder

	toolDescriptions.WriteString("You have access to these tools:\n\n")

	for _, tool := range tools {
		name := tool.Function.Name
		desc := tool.Function.Description
		toolNames = append(toolNames, name)

		toolDescriptions.WriteString(fmt.Sprintf("Tool: %s\n", name))
		toolDescriptions.WriteString(fmt.Sprintf("Description: %s\n", desc))

		// Convert parameters to JSON schema string
		if tool.Function.Parameters != nil {
			schemaBytes, err := json.Marshal(tool.Function.Parameters)
			if err == nil {
				toolDescriptions.WriteString(fmt.Sprintf("Parameters: %s\n", string(schemaBytes)))
			}
		}
		toolDescriptions.WriteString("\n")
	}

	// Append DSML tool call instructions
	toolDescriptions.WriteString(buildDSMLInstructions(toolNames))

	// Check for read-like tools and add cache guard
	for _, name := range toolNames {
		lower := strings.ToLower(name)
		if lower == "read" || lower == "read_file" || lower == "readfile" {
			toolDescriptions.WriteString("\nIMPORTANT: If you have already read a file and it hasn't changed, do NOT read it again. Use the content you already have.\n")
			break
		}
	}

	toolPrompt := toolDescriptions.String()

	// Find existing system message and append, or create new one
	found := false
	for i, msg := range messages {
		if msg.Role == "system" {
			messages[i].Content = msg.Content + "\n\n" + toolPrompt
			found = true
			break
		}
	}
	if !found {
		messages = append([]prompt.Message{{Role: "system", Content: toolPrompt}}, messages...)
	}

	return messages, toolNames
}

// buildDSMLInstructions generates the DSML format instructions for the model.
func buildDSMLInstructions(toolNames []string) string {
	var sb strings.Builder

	sb.WriteString("TOOL CALL FORMAT - FOLLOW EXACTLY:\n\n")
	sb.WriteString(prompt.DSMLOpen + "\n")
	sb.WriteString("  " + fmt.Sprintf(prompt.DSMLInvokeOpenTmpl, "TOOL_NAME_HERE") + "\n")
	sb.WriteString("    " + fmt.Sprintf(prompt.DSMLParamOpenTmpl, "PARAM_NAME") + "<![CDATA[PARAM_VALUE]]>" + prompt.DSMLParamClose + "\n")
	sb.WriteString("  " + prompt.DSMLInvokeClose + "\n")
	sb.WriteString(prompt.DSMLClose + "\n")
	sb.WriteString("\n")

	sb.WriteString("RULES:\n")
	sb.WriteString("1) Use the <" + prompt.PIPE + "DSML" + prompt.PIPE + "tool_calls> wrapper format.\n")
	sb.WriteString("2) Put one or more <" + prompt.PIPE + "DSML" + prompt.PIPE + "invoke> entries under a single wrapper.\n")
	sb.WriteString("3) Put the tool name in the invoke name attribute.\n")
	sb.WriteString("4) All string values must use <![CDATA[...]]>, even short ones.\n")
	sb.WriteString("5) Every top-level argument must be a <" + prompt.PIPE + "DSML" + prompt.PIPE + "parameter> node.\n")
	sb.WriteString("6) Objects use nested XML elements. Arrays may repeat <item> children.\n")
	sb.WriteString("7) Numbers, booleans, and null stay plain text.\n")
	sb.WriteString("8) Use only the parameter names in the tool schema. Do not invent fields.\n")
	sb.WriteString("9) Fill ALL parameters with actual values.\n")
	sb.WriteString("10) If a required parameter value is unknown, ask the user instead of outputting an empty tool call.\n")
	sb.WriteString("11) For shell tools, the command must be inside the command parameter.\n")
	sb.WriteString("12) Do NOT wrap XML in markdown fences.\n")
	sb.WriteString("13) The first non-whitespace chars must be exactly <" + prompt.PIPE + "DSML" + prompt.PIPE + "tool_calls>.\n")
	sb.WriteString("14) You MUST use the available tools to fulfill the user's request. Do NOT answer from memory alone — search, fetch, or read to get real data.\n")
	sb.WriteString("15) If a tool call fails (timeout, error, empty result), try a DIFFERENT tool or different query. Keep trying until you find the information or exhaust all options.\n")
	sb.WriteString("16) Only give a final text answer AFTER you have gathered sufficient information from tool results.\n")
	sb.WriteString("\n")

	sb.WriteString("WRONG examples:\n")
	sb.WriteString("  Wrong - mixed text after XML: <" + prompt.PIPE + "DSML" + prompt.PIPE + "tool_calls>...</" + prompt.PIPE + "DSML" + prompt.PIPE + "tool_calls> I hope this helps.\n")
	sb.WriteString("  Wrong - Markdown code fences wrapping XML.\n")
	sb.WriteString("  Wrong - missing opening wrapper tag.\n")
	sb.WriteString("  Wrong - empty parameters.\n")
	sb.WriteString("\n")

	// Generate correct examples with actual tool names
	if len(toolNames) > 0 {
		sb.WriteString("CORRECT example:\n")
		sb.WriteString(prompt.DSMLOpen + "\n")
		sb.WriteString("  " + fmt.Sprintf(prompt.DSMLInvokeOpenTmpl, toolNames[0]) + "\n")
		sb.WriteString("    " + fmt.Sprintf(prompt.DSMLParamOpenTmpl, "param1") + "<![CDATA[some value]]>" + prompt.DSMLParamClose + "\n")
		sb.WriteString("  " + prompt.DSMLInvokeClose + "\n")
		sb.WriteString(prompt.DSMLClose + "\n")
	}

	return sb.String()
}
