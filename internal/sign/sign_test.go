package sign

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
	"time"
)

func TestManagerLifecycle(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	m := NewManager(10*time.Minute, clock)

	sess, token, err := m.Create(CreateInput{
		ContractID: "ctr_1", ContractNo: "HT-2026-0001", ContractTitle: "租赁合同",
		ContentSHA256: "abc123", PartyID: "pty_1", SignerName: "张三",
		URLPrefixes: []string{"http://192.168.1.5:8765/sign/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidToken(token) {
		t.Fatalf("invalid token %q", token)
	}
	if len(sess.URLs) != 1 || sess.URLs[0] != "http://192.168.1.5:8765/sign/"+token {
		t.Fatalf("urls = %v", sess.URLs)
	}
	if sess.State != StatePending || !sess.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("session = %+v", sess)
	}
	got, err := m.Get(token)
	if err != nil || got.State != StatePending {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	byID, err := m.GetByID(sess.ID)
	if err != nil || byID.ID != sess.ID {
		t.Fatalf("get by id = %+v err=%v", byID, err)
	}

	if _, err := m.Claim(token); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := m.Claim(token); err != ErrBusy {
		t.Fatalf("second claim err = %v, want ErrBusy", err)
	}
	m.Release(token)
	if got, _ := m.Get(token); got.State != StatePending {
		t.Fatalf("after release state = %s", got.State)
	}
	if _, err := m.Claim(token); err != nil {
		t.Fatalf("re-claim after release: %v", err)
	}
	m.Complete(token, "sig_1", "assets/signatures/sig_1.png", 300, 100)
	done, err := m.Get(token)
	if err != nil || done.State != StateCompleted || done.SignatureID != "sig_1" ||
		done.SignaturePath != "assets/signatures/sig_1.png" {
		t.Fatalf("completed = %+v err=%v", done, err)
	}
	if _, err := m.Claim(token); err != ErrConsumed {
		t.Fatalf("reuse err = %v, want ErrConsumed", err)
	}
	// Advancing past expiry does not erase a completed result.
	now = now.Add(11 * time.Minute)
	if got, _ := m.Get(token); got.State != StateCompleted {
		t.Fatalf("completed session expired: %s", got.State)
	}

	// A pending session expires and cannot be claimed.
	now = now.Add(time.Minute)
	sess2, token2, err := m.Create(CreateInput{ContractID: "ctr_2", SignerName: "李四",
		URLPrefixes: []string{"http://192.168.1.5:8765/sign/"}})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if got, _ := m.Get(token2); got.State != StateExpired {
		t.Fatalf("expired state = %s", got.State)
	}
	if _, err := m.Claim(token2); err != ErrExpired {
		t.Fatalf("claim expired err = %v", err)
	}
	// Cancel removes a pending session entirely.
	sess3, token3, err := m.Create(CreateInput{SignerName: "王五",
		URLPrefixes: []string{"http://192.168.1.5:8765/sign/"}})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cancel(sess3.ID) {
		t.Fatal("cancel returned false")
	}
	if _, err := m.Get(token3); err != ErrNotFound {
		t.Fatalf("get cancelled err = %v", err)
	}
	// Cancelling an expired session just drops it from memory.
	if !m.Cancel(sess2.ID) {
		t.Fatal("cancel of expired session should still remove it")
	}
	if _, err := m.Get(token2); err != ErrNotFound {
		t.Fatalf("get expired-cancelled err = %v", err)
	}
}

func strokePNG(t *testing.T, w, h int, draw func(*image.NRGBA)) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if draw != nil {
		draw(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func drawSignature(img *image.NRGBA) {
	ink := color.NRGBA{0, 0, 0, 255}
	for x := 100; x < 760; x++ {
		y := 240 + int(70*math.Sin(float64(x)/38))
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				if dx*dx+dy*dy > 9 {
					continue
				}
				px := x + dx
				py := y + dy
				if image.Pt(px, py).In(img.Bounds()) {
					img.SetNRGBA(px, py, ink)
				}
			}
		}
	}
	// A long underline to make the signature look real.
	for x := 90; x < 820; x++ {
		for dy := -1; dy <= 1; dy++ {
			img.SetNRGBA(x, 420+dy, ink)
		}
	}
}

func TestNormalizePNGTrimsAndKeepsQuality(t *testing.T) {
	raw := strokePNG(t, 1200, 480, drawSignature)
	got, err := NormalizePNG(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.Width >= 1200 || got.Height >= 480 {
		t.Fatalf("image was not trimmed: %dx%d", got.Width, got.Height)
	}
	if got.Width < 700 || got.Height < 150 {
		t.Fatalf("strokes lost in trim: %dx%d", got.Width, got.Height)
	}
	if len(got.SHA256) != 64 || got.InkRatio <= 0 {
		t.Fatalf("metadata = %+v", got)
	}
	out, err := png.Decode(bytes.NewReader(got.PNG))
	if err != nil {
		t.Fatal(err)
	}
	b := out.Bounds()
	if out.At(b.Min.X, b.Min.Y).(color.NRGBA).A != 0 {
		t.Fatal("trimmed border should be transparent")
	}
	opaqueBlack, transparent := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := out.At(x, y).(color.NRGBA)
			if c.A == 0 {
				transparent++
				continue
			}
			if c.R != 0 || c.G != 0 || c.B != 0 {
				t.Fatalf("ink is not black: %+v", c)
			}
			if c.A == 255 {
				opaqueBlack++
			}
		}
	}
	if opaqueBlack == 0 || transparent == 0 {
		t.Fatalf("opaque=%d transparent=%d", opaqueBlack, transparent)
	}
	// The same drawing must normalize to the same bytes (stable digest).
	again, err := NormalizePNG(raw)
	if err != nil || again.SHA256 != got.SHA256 {
		t.Fatalf("digest not stable: %s vs %s err=%v", got.SHA256, again.SHA256, err)
	}
}

func TestNormalizePNGWhiteBackground(t *testing.T) {
	// A screenshot-like submission: white opaque background with black ink.
	raw := strokePNG(t, 1200, 480, func(img *image.NRGBA) {
		for y := 0; y < 480; y++ {
			for x := 0; x < 1200; x++ {
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
		drawSignature(img)
	})
	got, err := NormalizePNG(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	out, err := png.Decode(bytes.NewReader(got.PNG))
	if err != nil {
		t.Fatal(err)
	}
	b := out.Bounds()
	if c := out.At(b.Min.X, b.Min.Y).(color.NRGBA); c.A != 0 {
		t.Fatalf("white background not removed: %+v", c)
	}
}

func TestNormalizePNGRejectsLightAndSmall(t *testing.T) {
	empty := strokePNG(t, 1200, 480, nil)
	if _, err := NormalizePNG(empty); err == nil {
		t.Fatal("empty canvas should be rejected")
	} else if imgErr, ok := err.(*ImageError); !ok || imgErr.Code != "too_light" {
		t.Fatalf("empty canvas error = %v", err)
	}

	faint := strokePNG(t, 1200, 480, func(img *image.NRGBA) {
		for i := 0; i < 60; i++ {
			img.SetNRGBA(100+i, 200, color.NRGBA{0, 0, 0, 20})
		}
	})
	if _, err := NormalizePNG(faint); err == nil {
		t.Fatal("faint drawing should be rejected")
	} else if imgErr, ok := err.(*ImageError); !ok || imgErr.Code != "too_light" {
		t.Fatalf("faint drawing error = %v", err)
	}

	tiny := strokePNG(t, 1200, 480, func(img *image.NRGBA) {
		for y := 100; y < 130; y++ {
			for x := 100; x < 160; x++ {
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
			}
		}
	})
	if _, err := NormalizePNG(tiny); err == nil {
		t.Fatal("tiny drawing should be rejected")
	} else if imgErr, ok := err.(*ImageError); !ok || imgErr.Code != "too_small" {
		t.Fatalf("tiny drawing error = %v", err)
	}

	if _, err := NormalizePNG([]byte("not a png")); err == nil {
		t.Fatal("non-png should be rejected")
	}
	smallCanvas := strokePNG(t, 100, 50, nil)
	if _, err := NormalizePNG(smallCanvas); err == nil {
		t.Fatal("small canvas should be rejected")
	}
}
