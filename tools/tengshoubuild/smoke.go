package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// smokeServeTimeout 与旧脚本一致：100 次 × 200ms ≈ 20s 等待服务就绪。
	smokeServeTimeout = 20 * time.Second
	smokePollInterval = 200 * time.Millisecond
	// smokeStepTimeout 防止 MCP 子进程异常挂起；正常 EOF 后进程应立即退出。
	smokeStepTimeout = 2 * time.Minute
)

// MCP 请求与旧冒烟脚本逐字节一致：initialize → notifications/initialized
// → tools/list（或一次写工具调用）。stdin 写原始字节，不引入 BOM。
const smokeInitRequests = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`

const smokeWriteRequests = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tengshou_create_field","arguments":{"entity":"contract","key":"smoke_field","label":"smoke field","type":"text"}}}
`

// runSmokeMCP 以真实 agent 方式（stdin/stdout 管道）拉起发布形态二进制的
// `mcp` 子命令：先在本地服务未启动时断言 initialize / tools/list，再启动同版
// tengshou-cli serve 并调用一次写工具。Linux 没有桌面壳构建，CI 把
// tengshou-cli 同时作为连接器与 serve 实例传入（两个参数相同）。
func runSmokeMCP(args []string) error {
	if len(args) != 2 {
		return errors.New("用法：go run ./tools/tengshoubuild smoke-mcp <被测二进制> <tengshou-cli>")
	}
	desktop, cli := args[0], args[1]
	if err := checkExecutable("被测二进制", desktop); err != nil {
		return err
	}
	if err := checkExecutable("tengshou-cli", cli); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "tengshou-mcp-smoke-")
	if err != nil {
		return fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmp)
	dataDir := filepath.Join(tmp, "data")

	port, err := freePort()
	if err != nil {
		return err
	}

	fmt.Println("1/2 被测二进制 mcp：initialize / tools/list（本地服务未启动）")
	initOut, err := runMCP(desktop, dataDir, "", smokeInitRequests)
	if err != nil {
		return err
	}
	initResponses, err := parseMCPResponses(initOut)
	if err != nil {
		return err
	}
	toolCount, err := checkInitializeResponses(initResponses)
	if err != nil {
		return err
	}
	fmt.Printf("   initialize / tools/list 通过（%d 个工具）\n", toolCount)

	fmt.Println("2/2 启动 tengshou-cli serve 后经被测二进制调用写工具")
	serve, err := startServe(cli, dataDir, port, filepath.Join(tmp, "serve.log"))
	if err != nil {
		return err
	}
	defer serve.stop()
	if err := waitHealthy(serve, port); err != nil {
		serve.stop()
		return fmt.Errorf("%w\nserve 日志：\n%s", err, readLogTail(serve.logPath))
	}

	writeOut, err := runMCP(desktop, dataDir, fmt.Sprintf("http://127.0.0.1:%d", port), smokeWriteRequests)
	if err != nil {
		return err
	}
	writeResponses, err := parseMCPResponses(writeOut)
	if err != nil {
		return err
	}
	if err := checkWriteResponse(writeResponses); err != nil {
		return err
	}
	fmt.Println("   写工具 tengshou_create_field 通过")
	fmt.Println("冒烟通过：被测二进制 mcp（stdio 管道）+ tengshou-cli serve 写工具")
	return nil
}

// checkExecutable 确认路径存在且（非 Windows）带可执行位。
func checkExecutable(label, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("找不到%s %s: %w", label, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s 是目录：%s", label, path)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s 不可执行：%s", label, path)
	}
	return nil
}

// runMCP 用 os/exec 直接写原始字节 stdin（不经过 shell / PowerShell 文本
// 写入器，避免流首出现 UTF-8 BOM），返回 stdout。
func runMCP(exe, dataDir, apiURL, requests string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), smokeStepTimeout)
	defer cancel()

	args := []string{"mcp", "--data-dir", dataDir}
	if apiURL != "" {
		args = append(args, "--api-url", apiURL)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdin = strings.NewReader(requests)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s mcp 退出异常（%w）：%s", exe, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// mcpResponse 只取冒烟断言需要的字段；raw 保留原始行用于错误信息。
type mcpResponse struct {
	Result struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	raw string
}

// parseMCPResponses 把 stdout 的每一非空行解析为一条 JSON-RPC 响应。
func parseMCPResponses(out []byte) ([]mcpResponse, error) {
	var responses []mcpResponse
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var response mcpResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			return nil, fmt.Errorf("JSON-RPC 响应无法解析：%w（原文：%s）", err, line)
		}
		response.raw = line
		responses = append(responses, response)
	}
	return responses, nil
}

// checkInitializeResponses 断言 initialize 与 tools/list 两条响应，
// 返回工具总数。断言强度与旧脚本一致，不降低。
func checkInitializeResponses(responses []mcpResponse) (int, error) {
	if len(responses) != 2 {
		return 0, fmt.Errorf("initialize/tools/list 期望 2 条 JSON-RPC 响应，实际 %d 条", len(responses))
	}
	if got := responses[0].Result.ServerInfo.Name; got != "tengshou" {
		return 0, fmt.Errorf("initialize 返回异常（serverInfo.name=%q）：%s", got, responses[0].raw)
	}
	tools := make(map[string]bool, len(responses[1].Result.Tools))
	for _, tool := range responses[1].Result.Tools {
		tools[tool.Name] = true
	}
	for _, want := range []string{"tengshou_status", "tengshou_create_field"} {
		if !tools[want] {
			return 0, fmt.Errorf("tools/list 缺少 %s：%s", want, responses[1].raw)
		}
	}
	return len(tools), nil
}

// checkWriteResponse 断言写工具调用成功且响应含 smoke_field。
func checkWriteResponse(responses []mcpResponse) error {
	if len(responses) != 2 {
		return fmt.Errorf("写工具调用期望 2 条 JSON-RPC 响应，实际 %d 条", len(responses))
	}
	result := responses[1].Result
	if result.IsError {
		return fmt.Errorf("写工具返回 isError：%s", responses[1].raw)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "smoke_field") {
		return fmt.Errorf("写工具响应缺少字段 smoke_field：%s", responses[1].raw)
	}
	return nil
}

// freePort 监听 :0 取一个空闲端口后立即释放（与旧脚本一致）。
func freePort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("获取空闲端口: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// serveProcess 管理冒烟用的 tengshou-cli serve 子进程。
type serveProcess struct {
	cmd     *exec.Cmd
	done    chan error
	logPath string
	waited  bool
	waitErr error
}

func startServe(cli, dataDir string, port int, logPath string) (*serveProcess, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("创建 serve 日志: %w", err)
	}
	cmd := exec.Command(cli, "serve",
		"--data-dir", dataDir, "--port", strconv.Itoa(port), "--loopback")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("启动 tengshou-cli serve: %w", err)
	}
	serve := &serveProcess{cmd: cmd, done: make(chan error, 1), logPath: logPath}
	go func() {
		err := cmd.Wait()
		logFile.Close()
		serve.done <- err
	}()
	return serve, nil
}

// exited 非阻塞判断 serve 是否已退出；退出后返回其错误（nil 表示正常退出）。
func (s *serveProcess) exited() (bool, error) {
	if s.waited {
		return true, s.waitErr
	}
	select {
	case s.waitErr = <-s.done:
		s.waited = true
		return true, s.waitErr
	default:
		return false, nil
	}
}

func (s *serveProcess) wait() error {
	if !s.waited {
		s.waitErr = <-s.done
		s.waited = true
	}
	return s.waitErr
}

// stop 幂等：杀进程并回收（进程已退出时只是取回退出码）。
func (s *serveProcess) stop() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.wait()
}

// waitHealthy 轮询 /api/v1/health，直到 200 或超时；serve 提前退出立即失败。
func waitHealthy(s *serveProcess, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", port)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(smokeServeTimeout)
	for time.Now().Before(deadline) {
		if exited, err := s.exited(); exited {
			return fmt.Errorf("本地服务未就绪：tengshou-cli serve 提前退出（%v）", err)
		}
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(smokePollInterval)
	}
	return fmt.Errorf("本地服务未就绪（%s 超时）", smokeServeTimeout)
}

func readLogTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "（读取日志失败：" + err.Error() + "）"
	}
	const maxTail = 8 << 10
	if len(data) > maxTail {
		data = data[len(data)-maxTail:]
	}
	return strings.TrimSpace(string(data))
}
