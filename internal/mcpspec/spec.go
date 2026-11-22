// Package mcpspec carries connector metadata shared by the MCP stdio server and
// the desktop MCP status panel.
//
// It exists as a separate package because internal/mcp's tests instantiate
// internal/httpapi; if httpapi imported mcp directly, those tests would form an
// import cycle. The MCP tests assert that toolDefinitions() still matches these
// counts, so the two stay in sync.
package mcpspec

// Tool counts exposed by the B6 connector.
const (
	// ReadToolCount is the number of read-only tools.
	ReadToolCount = 10
	// WriteToolCount is the number of write tools (create field, party and
	// contract draft). Deletion and bulk mutation are intentionally absent.
	WriteToolCount = 3
)
