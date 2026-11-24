package main

import "os"

// writeTestFile 写入测试用文件（内容为原始字节，不做任何转码）。
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
