package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/service"
)

// F-TE-4: retained signature images are private assets — file 0600 and the
// containing directory 0700 (POSIX bits; Windows is skipped).
func TestSaveSignatureUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 POSIX 权限位")
	}
	svc, _, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)

	party := mustParty(t, svc, "张三")
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	img.SetNRGBA(2, 2, color.NRGBA{0, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	sig, err := svc.SaveSignature(context.Background(), service.SignatureInput{
		PartyID: party.ID, SignerName: party.Name,
		ImagePNG: buf.Bytes(), SHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatalf("save signature: %v", err)
	}

	full := filepath.Join(dir, filepath.FromSlash(sig.AssetPath))
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat signature file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("signature file perm = %#o, want 0600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(full))
	if err != nil {
		t.Fatalf("stat signature dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("signature dir perm = %#o, want 0700", perm)
	}
}
