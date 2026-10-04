package stream

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/audemed44/parlor/internal/stream/retro"
)

func TestPackUnpack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sdmc")
	files := map[string]string{
		"Nintendo 3DS/0/0/title/00040000/001b5000/data/00000001/main":     "save data",
		"Nintendo 3DS/0/0/title/00040000/001b5000/data/00000001.metadata": "meta",
	}
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, "Nintendo 3DS/0/0/extdata/empty"), 0o755)
	a, err := Pack(dir)
	if err != nil || len(a) == 0 {
		t.Fatal(err)
	}
	// The same files give the same bytes, whatever their times.
	later := time.Now().Add(time.Hour)
	for p := range files {
		os.Chtimes(filepath.Join(dir, p), later, later)
	}
	if b, _ := Pack(dir); !bytes.Equal(a, b) {
		t.Fatal("packing isn't deterministic")
	}
	out := filepath.Join(t.TempDir(), "sdmc")
	os.MkdirAll(out, 0o755)
	os.WriteFile(filepath.Join(out, "stale"), []byte("old game's"), 0o644)
	if err := Unpack(out, a); err != nil {
		t.Fatal(err)
	}
	for p, c := range files {
		if got, _ := os.ReadFile(filepath.Join(out, p)); string(got) != c {
			t.Fatalf("%s = %q", p, got)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "stale")); err == nil {
		t.Fatal("unpacking kept files from before")
	}
	if info, err := os.Stat(filepath.Join(out, "Nintendo 3DS/0/0/extdata/empty")); err != nil || !info.IsDir() {
		t.Fatal("empty folder lost")
	}
	if b, _ := Pack(out); !bytes.Equal(a, b) {
		t.Fatal("unpacked save packs differently")
	}
	// A new game: no files, no save.
	if err := Unpack(out, nil); err != nil {
		t.Fatal(err)
	}
	if b, err := Pack(out); err != nil || b != nil {
		t.Fatalf("empty folder packed to %d bytes, %v", len(b), err)
	}
	if b, err := Pack(filepath.Join(out, "missing")); err != nil || b != nil {
		t.Fatalf("missing folder: %v", err)
	}
}

func TestUnpackRefusesEscapes(t *testing.T) {
	for _, name := range []string{"../evil", "/etc/evil", "a/../../evil"} {
		var buf bytes.Buffer
		w := newTar(&buf)
		w(name, "x")
		if err := Unpack(t.TempDir(), buf.Bytes()); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := Unpack(t.TempDir(), []byte("not a tar at all, just some bytes that are long enough to look")); err == nil {
		t.Error("garbage accepted")
	}
}

func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	a := Fingerprint(dir)
	os.WriteFile(filepath.Join(dir, "main"), []byte("1"), 0o644)
	b := Fingerprint(dir)
	if a == b {
		t.Fatal("new file unnoticed")
	}
	os.WriteFile(filepath.Join(dir, "main"), []byte("12"), 0o644)
	if Fingerprint(dir) == b {
		t.Fatal("changed file unnoticed")
	}
}

func TestWrapUnwrap(t *testing.T) {
	f := retro.Frame{Pix: make([]byte, 4*2*2), Width: 4, Height: 2, Stride: 8, Format: retro.RGB565}
	binary.LittleEndian.PutUint16(f.Pix, 0xf800) // red, top left
	img := Picture(f)
	if r, _, _, _ := img.At(0, 0).RGBA(); r>>8 != 0xf8 {
		t.Fatalf("top left = %v", img.At(0, 0))
	}
	state := []byte("CST\x1b the state")
	p, err := Wrap(img, state)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Unwrap(p); err != nil || !bytes.Equal(got, state) {
		t.Fatalf("unwrap: %q %v", got, err)
	}
	if got, _ := Unwrap(state); !bytes.Equal(got, state) {
		t.Fatal("bare state changed")
	}
	if _, err := Unwrap(p[:40]); err == nil {
		t.Fatal("picture without state accepted")
	}
}

func TestPictureNV12(t *testing.T) {
	// 2×2 of mid grey: Y 126, U and V 128 (BT.709, limited range).
	f := retro.Frame{Pix: []byte{126, 126, 126, 126, 128, 128}, Width: 2, Height: 2, Stride: 2, Format: retro.NV12}
	c := color.NRGBAModel.Convert(Picture(f).At(1, 1)).(color.NRGBA)
	if c.R < 125 || c.R > 131 || c.R != c.G || c.G != c.B {
		t.Fatalf("grey came out %v", c)
	}
}

func TestDecodeInput(t *testing.T) {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint16(b[0:], 1<<retro.A|1<<retro.Left)
	binary.LittleEndian.PutUint16(b[4:], uint16(0x8001)) // ly = -32767
	b[10] = 1
	binary.LittleEndian.PutUint16(b[12:], 0)
	binary.LittleEndian.PutUint16(b[14:], 65535)
	in, err := DecodeInput(b)
	if err != nil {
		t.Fatal(err)
	}
	if in.Buttons != 1<<retro.A|1<<retro.Left || in.LY != -32767 || !in.Touching || in.TX != -32767 || in.TY != 32767 {
		t.Fatalf("%+v", in)
	}
	if _, err := DecodeInput(b[:15]); err == nil {
		t.Fatal("short message accepted")
	}
	// The D-pad tilts the stick, unless the stick is in use.
	if s := withStick(in); s.LX != 0 || s.LY != -32767 {
		t.Fatalf("stick in use was changed: %+v", s)
	}
	in.LY = 0
	if s := withStick(in); s.LX != -32767 || s.LY != 0 {
		t.Fatalf("left: %+v", s)
	}
}

func TestSidecarAPI(t *testing.T) {
	s := &Sidecar{Config: Config{Token: "secret"}, Exe: "/nonexistent"}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	req := func(method, auth, body string) int {
		r, _ := http.NewRequest(method, srv.URL+"/session", strings.NewReader(body))
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := req("GET", "", ""); c != 401 {
		t.Fatalf("no token: %d", c)
	}
	if c := req("GET", "wrong", ""); c != 401 {
		t.Fatalf("wrong token: %d", c)
	}
	if c := req("GET", "secret", ""); c != 200 {
		t.Fatalf("status: %d", c)
	}
	for _, body := range []string{
		`{"game_id":1,"platform":"3ds","rom":"../etc/passwd","sdp":"x"}`,
		`{"game_id":1,"platform":"psx","rom":"a.bin","sdp":"x"}`,
		`{"game_id":1,"platform":"3ds","rom":"a.3ds"}`,
		`not json`,
	} {
		if c := req("POST", "secret", body); c != 400 {
			t.Errorf("%s: %d", body, c)
		}
	}
}

func TestBitrate(t *testing.T) {
	if b := bitrate(400, 480); b != 3000 {
		t.Errorf("1×: %d", b)
	}
	if b := bitrate(1200, 1440); b < 6000 || b > 12000 {
		t.Errorf("3×: %d", b)
	}
	if b := bitrate(4000, 4000); b != 20000 {
		t.Errorf("huge: %d", b)
	}
}

// newTar starts a tar in buf; the function returned adds a file.
func newTar(buf *bytes.Buffer) func(name, content string) {
	return func(name, content string) {
		w := tar.NewWriter(buf)
		w.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
		w.Write([]byte(content))
		w.Close()
	}
}
