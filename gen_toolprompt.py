#!/usr/bin/env python3
"""Patch toolprompt.go template string.
Replaces TEMPLATE_PLACEHOLDER with properly escaped Go string.
Key: the template uses actual \\n (backslash+n) between lines so Go
interprets them as newlines at runtime. We must NOT double-escape.
"""
import os

PIPE = chr(0xFF5C)  # fullwidth pipe

lines = [
    'TOOL CALL FORMAT - FOLLOW EXACTLY:',
    '',
    '<' + PIPE + 'DSML' + PIPE + 'tool_calls>',
    '  <' + PIPE + 'DSML' + PIPE + 'invoke name="TOOL_NAME_HERE">',
    '    <' + PIPE + 'DSML' + PIPE + 'parameter name="PARAM_NAME"><![CDATA[PARAM_VALUE]]></' + PIPE + 'DSML' + PIPE + 'parameter>',
    '  </' + PIPE + 'DSML' + PIPE + 'invoke>',
    '</' + PIPE + 'DSML' + PIPE + 'tool_calls>',
    '',
    'RULES:',
    '1) Use the <' + PIPE + 'DSML' + PIPE + 'tool_calls> wrapper format.',
    '2) Put one or more <' + PIPE + 'DSML' + PIPE + 'invoke> entries under a single <' + PIPE + 'DSML' + PIPE + 'tool_calls> root.',
    '3) Put the tool name in the invoke name attribute: <' + PIPE + 'DSML' + PIPE + 'invoke name="TOOL_NAME">.',
    '4) All string values must use <![CDATA[...]]>, even short ones. This includes code, scripts, file contents, prompts, paths, names, and queries.',
    '5) Every top-level argument must be a <' + PIPE + 'DSML' + PIPE + 'parameter name="ARG_NAME">...</' + PIPE + 'DSML' + PIPE + 'parameter> node.',
    '6) Objects use nested XML elements inside the parameter body. Arrays may repeat <item> children.',
    '7) Numbers, booleans, and null stay plain text.',
    '8) Use only the parameter names in the tool schema. Do not invent fields.',
    '9) Fill ALL parameters with actual values. Do not emit placeholder, blank, or whitespace-only parameters.',
    '10) If a required parameter value is unknown, ask the user or answer normally instead of outputting an empty tool call.',
    '11) For shell tools (Bash/execute_command), the command must be inside the command parameter. Never call them with an empty command.',
    '12) Do NOT wrap XML in markdown fences. Do NOT output explanations after tool calls.',
    '13) The first non-whitespace chars must be exactly <' + PIPE + 'DSML' + PIPE + 'tool_calls>.',
    '14) Never omit the opening <' + PIPE + 'DSML' + PIPE + 'tool_calls> tag.',
    '15) Legacy <tool_calls>/<invoke>/<parameter> tags also accepted, but prefer DSML form.',
    '16) NEVER use percent-sign tool calls like % ToolName args. This format is INVALID.',
    '17) NEVER use dollar-sign commands like $ command. This format is INVALID.',
    '18) NEVER use angle-bracket tool calls like <tool_call><skill>...</skill></tool_call>. This format is INVALID.',
    '19) For the Bash tool, you MUST include the "description" parameter explaining what the command does.',
    '20) CRITICAL - WHEN TO STOP CALLING TOOLS: After 1-3 tool calls, you MUST synthesize the results and provide a final text answer to the user. Do NOT keep calling tools repeatedly. If you searched and got results, ANSWER THE USER with what you found. If a search returned no useful results, say so and offer alternatives. NEVER enter an infinite loop of tool calls.',
    '21) CRITICAL - TOOL CALLS ARE NOT YOUR FINAL ANSWER: A tool call is an intermediate step. After getting tool results, your NEXT response MUST be plain text answering the user\'s question. Do NOT respond to tool results with another tool call unless absolutely necessary (max 3 tool calls per question).',
    '',
    'PARAMETER SHAPES:',
    '- string => <' + PIPE + 'DSML' + PIPE + 'parameter name="x"><![CDATA[value]]></' + PIPE + 'DSML' + PIPE + 'parameter>',
    '- object => <' + PIPE + 'DSML' + PIPE + 'parameter name="x"><field>...</field></' + PIPE + 'DSML' + PIPE + 'parameter>',
    '- array => <' + PIPE + 'DSML' + PIPE + 'parameter name="x"><item>...</item></' + PIPE + 'DSML' + PIPE + 'parameter>',
    '- number/bool/null => <' + PIPE + 'DSML' + PIPE + 'parameter name="x">plain_text</' + PIPE + 'DSML' + PIPE + 'parameter>',
    '',
    'WRONG - Do NOT do these:',
    '',
    'Wrong 1 - mixed text after XML:',
    '  <' + PIPE + 'DSML' + PIPE + 'tool_calls>...</' + PIPE + 'DSML' + PIPE + 'tool_calls> I hope this helps.',
    '',
    'Wrong 2 - Markdown code fences:',
    '  ```xml',
    '  <' + PIPE + 'DSML' + PIPE + 'tool_calls>...</' + PIPE + 'DSML' + PIPE + 'tool_calls>',
    '  ```',
    '',
    'Wrong 3 - missing opening wrapper:',
    '  <' + PIPE + 'DSML' + PIPE + 'invoke name="TOOL_NAME">...</' + PIPE + 'DSML' + PIPE + 'invoke>',
    '  </' + PIPE + 'DSML' + PIPE + 'tool_calls>',
    '',
    'Wrong 4 - empty parameters:',
    '  <' + PIPE + 'DSML' + PIPE + 'tool_calls>',
    '    <' + PIPE + 'DSML' + PIPE + 'invoke name="Bash">',
    '      <' + PIPE + 'DSML' + PIPE + 'parameter name="command"></' + PIPE + 'DSML' + PIPE + 'parameter>',
    '    </' + PIPE + 'DSML' + PIPE + 'invoke>',
    '  </' + PIPE + 'DSML' + PIPE + 'tool_calls>',
    '',
    'Wrong 5 - percent-sign tool call (INVALID):',
    '  % WebFetch https://example.com',
    '',
    'Wrong 6 - dollar-sign command (INVALID):',
    '  $ curl https://example.com',
    '',
    'Wrong 7 - angle-bracket tool call (INVALID):',
    '  <tool_call><skill><name>fetch-page</name></skill></tool_call>',
    '',
    'Remember: The ONLY valid way to use tools is the <' + PIPE + 'DSML' + PIPE + 'tool_calls>...</' + PIPE + 'DSML' + PIPE + 'tool_calls> block at the end of your response.',
]

# Join with actual newline characters
full_text = "\n".join(lines)

# Escape for Go double-quoted string:
# - backslash -> \\  (Go needs \\ for literal backslash)
# - double-quote -> \"
# - actual newline -> \\n  (Go escape sequence for newline)
# BUT: the fullwidth pipe character ｜ (U+FF5C) is fine in Go strings
def escape_for_go(s):
    result = []
    for ch in s:
        if ch == '\\':
            result.append('\\\\')
        elif ch == '"':
            result.append('\\"')
        elif ch == '\n':
            result.append('\\n')
        else:
            result.append(ch)
    return ''.join(result)

# Split on backtick since Go double-quoted strings can't contain literal backtick
# Actually, Go strings CAN contain backtick. Only raw strings (backtick-delimited) can't.
# Since we're using double-quoted strings, backtick is fine.
escaped = escape_for_go(full_text)

# Read and patch Go file
go_path = os.path.join('internal', 'toolcall', 'toolprompt.go')
with open(go_path, 'r', encoding='utf-8') as f:
    go_code = f.read()

old_marker = 'TEMPLATE_PLACEHOLDER'
# Replace the backtick-delimited placeholder with a double-quoted Go string
go_code = go_code.replace('`' + old_marker + '`', '"' + escaped + '"')

with open(go_path, 'w', encoding='utf-8') as f:
    f.write(go_code)

print(f"Patched {go_path} ({len(full_text)} chars, {len(lines)} lines)")
