package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTempFile writes content to a fresh temp file and returns its path.
// The file is auto-cleaned at the end of the test.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestReadLastNLines_Empty(t *testing.T) {
	path := writeTempFile(t, "")
	got, err := ReadLastNLines(path, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestReadLastNLines_NZero(t *testing.T) {
	path := writeTempFile(t, "a\nb\nc\n")
	got, err := ReadLastNLines(path, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty for n=0, got %v", got)
	}
}

func TestReadLastNLines_FewerThanN(t *testing.T) {
	path := writeTempFile(t, "a\nb\n")
	got, err := ReadLastNLines(path, 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadLastNLines_ExactlyN(t *testing.T) {
	path := writeTempFile(t, "a\nb\nc\n")
	got, err := ReadLastNLines(path, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadLastNLines_MoreThanN(t *testing.T) {
	path := writeTempFile(t, "a\nb\nc\nd\ne\n")
	got, err := ReadLastNLines(path, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"c", "d", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadLastNLines_NoTrailingNewline(t *testing.T) {
	// File whose last line has no terminating \n — the tail must still return it.
	path := writeTempFile(t, "a\nb\nc")
	got, err := ReadLastNLines(path, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadLastNLines_CRLF(t *testing.T) {
	// Windows line endings. \r must be stripped to match bufio.Scanner behaviour.
	path := writeTempFile(t, "a\r\nb\r\nc\r\n")
	got, err := ReadLastNLines(path, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReadLastNLines_SpansChunkBoundary(t *testing.T) {
	// Build a file much larger than chunkSize (8192) so the reader has to
	// stitch multiple reads together while walking backwards. Each line's
	// content encodes its index so ordering bugs would surface.
	var sb strings.Builder
	const total = 5000
	for i := 0; i < total; i++ {
		sb.WriteString("line-")
		sb.WriteString(itoa(i))
		// Pad to >80 bytes so the file comfortably exceeds 8KB*N.
		sb.WriteString(" ")
		sb.WriteString(strings.Repeat("x", 80))
		sb.WriteByte('\n')
	}
	path := writeTempFile(t, sb.String())

	got, err := ReadLastNLines(path, 50)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(got))
	}
	// First returned line must be index total-50, last must be total-1.
	wantFirst := "line-" + itoa(total-50)
	wantLast := "line-" + itoa(total-1)
	if !strings.HasPrefix(got[0], wantFirst) {
		t.Fatalf("first line = %q, want prefix %q", got[0], wantFirst)
	}
	if !strings.HasPrefix(got[len(got)-1], wantLast) {
		t.Fatalf("last line = %q, want prefix %q", got[len(got)-1], wantLast)
	}
}

// itoa avoids importing strconv for a single use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
