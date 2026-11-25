package doctopdf

import (
	"context"
	"testing"
	"time"
)

func TestFileURL(t *testing.T) {
	cases := []struct{ path, want string }{
		// POSIX 路径本身以 / 开头，保持三段斜杠。
		{"/tmp/tengshou-lo/profile", "file:///tmp/tengshou-lo/profile"},
		// Windows 盘符前必须补斜杠，否则盘符会被解析成 URL host，
		// LibreOffice 收到非法 profile URL 后会挂起。
		{"C:/Users/runner/AppData/Local/Temp/profile", "file:///C:/Users/runner/AppData/Local/Temp/profile"},
		{"c:/tmp/x", "file:///c:/tmp/x"},
	}
	for _, c := range cases {
		if got := fileURL(c.path); got != c.want {
			t.Errorf("fileURL(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestLibreOfficeCommandProfileURLAndWaitDelay(t *testing.T) {
	cmd := libreOfficeCommand(context.Background(), "/opt/soffice",
		fileURL("C:/tmp/profile"), "/tmp/out", "C:/tmp/source.docx")
	if got := cmd.WaitDelay; got != 10*time.Second {
		t.Fatalf("WaitDelay = %v, want 10s", got)
	}
	want := "-env:UserInstallation=file:///C:/tmp/profile"
	found := false
	for _, arg := range cmd.Args {
		if arg == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("args 缺少 %q：%v", want, cmd.Args)
	}
	if cmd.Args[0] != "/opt/soffice" {
		t.Fatalf("binary = %q", cmd.Args[0])
	}
}
