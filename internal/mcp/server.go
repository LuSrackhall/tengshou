package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Protocol constants.
const (
	serverName             = "tengshou"
	defaultProtocolVersion = "2025-06-18"

	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// supportedProtocolVersions are the MCP revisions this minimal tools server
// understands. initialize echoes the client's version when it is listed and
// otherwise answers with defaultProtocolVersion.
var supportedProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
}

const instructions = "腾手（tengshou）是用户本机的合同台账与文书工具。" +
	"只读工具查询合同、相对方档案、动态字段定义、签名资产和模板版本；" +
	"写工具仅能新建字段、相对方档案和合同草稿，不能删除、修改或批量操作既有记录，" +
	"所有写入以 actor=tengshou-mcp 记入变更记录。" +
	"腾手只提供结构化数据：外部部门表格由你自行按用户指定格式填写，腾手不解析、不代填外部表。" +
	"写入是否开启由 config.json 的 mcp.write_enabled 控制；关闭时写工具会返回明确错误。"

// Options wires the connector.
type Options struct {
	API     *API
	Version string
	Logger  *slog.Logger
}

// Server is a minimal MCP stdio server exposing tools over JSON-RPC 2.0.
type Server struct {
	api     *API
	version string
	logger  *slog.Logger
}

// New constructs the MCP server.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	return &Server{api: opts.API, version: version, logger: logger}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads newline-delimited JSON-RPC messages from in until EOF and writes
// responses to out. Diagnostics go to the logger (stderr), never to out.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		response, ok := s.handle(ctx, line)
		if !ok {
			continue
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("编码 MCP 响应失败：%w", err)
		}
		if _, err := writer.Write(encoded); err != nil {
			return fmt.Errorf("写入 MCP 响应失败：%w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("写入 MCP 响应失败：%w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("写入 MCP 响应失败：%w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 MCP 输入失败：%w", err)
	}
	return nil
}

func (s *Server) handle(ctx context.Context, line []byte) (any, bool) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: codeParseError, Message: "JSON 解析失败：" + err.Error()},
		}, true
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: codeInvalidRequest, Message: "无效的 JSON-RPC 2.0 请求"},
		}, true
	}
	notification := len(req.ID) == 0
	response := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		result, rerr := s.initialize(req.Params)
		if rerr != nil {
			response.Error = rerr
		} else {
			response.Result = result
		}
	case "notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed":
		// Notifications never receive a response.
		return nil, false
	case "ping":
		// struct{}{} marshals to {} and is not dropped by omitempty.
		response.Result = struct{}{}
	case "tools/list":
		response.Result = map[string]any{"tools": toolDefinitions()}
	case "tools/call":
		result, rerr := s.callTool(ctx, req.Params)
		if rerr != nil {
			response.Error = rerr
		} else {
			response.Result = result
		}
	default:
		if notification {
			return nil, false
		}
		response.Error = &rpcError{
			Code:    codeMethodNotFound,
			Message: fmt.Sprintf("未知方法 %q", req.Method),
		}
	}
	if response.Result == nil && response.Error == nil {
		response.Result = struct{}{}
	}
	return response, true
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	protocol := defaultProtocolVersion
	if len(bytes.TrimSpace(params)) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "initialize 参数无效"}
		}
		if supportedProtocolVersions[p.ProtocolVersion] {
			protocol = p.ProtocolVersion
		}
		if p.ClientInfo.Name != "" {
			s.logger.Info("mcp client connected",
				"client", p.ClientInfo.Name,
				"client_version", p.ClientInfo.Version,
				"protocol", protocol)
		}
	}
	return map[string]any{
		"protocolVersion": protocol,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"title":   "腾手本地数据连接器",
			"version": s.version,
		},
		"instructions": instructions,
	}, nil
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call 参数无效"}
	}
	name := strings.TrimSpace(p.Name)
	if !knownTool(name) {
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("未知工具 %q", name)}
	}
	s.logger.Debug("mcp tool call", "tool", name)
	return s.executeTool(ctx, name, p.Arguments), nil
}
