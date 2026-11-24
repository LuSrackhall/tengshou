package httpapi

import (
	"log/slog"
	"os"
	"strings"
)

// mcpAccessCommand renders the stdio registration command shown to users.
// exePath is the running binary: the desktop app on Windows and macOS ships
// the connector inside the same executable (`<exe> mcp`), and `tengshou-cli
// serve` runs from the CLI binary, so the command always names the binary that
// actually started the local service instead of a fixed CLI label.
func mcpAccessCommand(exePath, dataDir string) string {
	command := "tengshou mcp"
	if exePath != "" {
		command = quoteCommandArg(exePath) + " mcp"
	}
	if dataDir != "" {
		command += ` --data-dir "` + dataDir + `"`
	}
	return command
}

// currentExecutable resolves the absolute path of the running binary. A
// failure is logged rather than silently swallowed; callers degrade to a
// command without an absolute path.
func currentExecutable(logger *slog.Logger) string {
	exe, err := os.Executable()
	if err != nil {
		if logger != nil {
			logger.Warn("无法确定当前可执行文件路径，MCP 接入命令退化为通用形式", "error", err)
		}
		return ""
	}
	return exe
}

// quoteCommandArg wraps an executable path in double quotes when it contains
// spaces or tabs, so the copied command survives paths such as
// `C:\Program Files\...`. Windows executable paths cannot contain `"`, so no
// inner escaping is needed.
func quoteCommandArg(arg string) string {
	if strings.ContainsAny(arg, " \t") {
		return `"` + arg + `"`
	}
	return arg
}
