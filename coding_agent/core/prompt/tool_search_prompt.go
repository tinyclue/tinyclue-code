package prompt

const TOOL_SEARCH_PROMPT = `Fetches full schema definitions for deferred tools so they can be called.
Deferred tools appear by name in <available-deferred-tools> messages.
Until fetched, only the name is known — there is no parameter schema, so the tool cannot be invoked. This tool takes a query, searches the deferred tool pool, and returns a comma-separated list of matching tool names.

Result format: "Matched tools: <name1>, <name2>, ..." — just the tool names, no schema. If no tools match, returns "No matching deferred tools found."

Once a tool name appears in the result, its full schema will be available in your tools list on the next turn.

Query forms:
- "select:Read,Edit,Grep" — fetch these exact tools by name
- "notebook jupyter" — keyword search, up to max_results best matches
- "+slack send" — require "slack" in the name, rank by remaining terms
`
