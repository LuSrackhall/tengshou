package httpapi_test

import (
	"os"
	"path/filepath"
	"testing"
)

// frontendSourceFiles 返回 frontend/src 下所有用户可见的前端源码（Vue/T
// S），供各域的禁用语边界测试统一覆盖。app.js 退役后界面文案全部落在
// 这里，新增组件无需再逐个登记到每个测试。
func frontendSourceFiles(t *testing.T) []string {
	t.Helper()
	root := "../../frontend/src"
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".ts", ".vue", ".js":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("%s has no frontend sources", root)
	}
	return files
}
