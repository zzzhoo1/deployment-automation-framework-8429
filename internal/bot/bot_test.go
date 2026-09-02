package bot

import (
	"testing"
)

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		text     string
		wantCmd  string
		wantArgs string
	}{
		{"/start", "start", ""},
		{"/Download  https://x", "download", "https://x"},
		{"/LIST\tfoo bar", "list", "foo bar"},
		{"/EmptyTrash", "emptytrash", ""},
		{"no-slash", "", "no-slash"},
		{"/ytdl https://youtu.be/x", "ytdl", "https://youtu.be/x"},
	}
	for _, c := range cases {
		cmd, args := splitCommand(c.text)
		if cmd != c.wantCmd || args != c.wantArgs {
			t.Errorf("splitCommand(%q) = (%q,%q), want (%q,%q)", c.text, cmd, args, c.wantCmd, c.wantArgs)
		}
	}
}

func TestExtractID(t *testing.T) {
	cases := map[string]string{
		"https://drive.google.com/drive/folders/abc123XYZ": "abc123XYZ",
		"https://drive.google.com/file/d/FILEID456/view":   "FILEID456",
		"https://drive.google.com/open?id=zzz999":          "zzz999",
		"plainFolderID123": "plainFolderID123",
	}
	for in, want := range cases {
		if got := extractID(in); got != want {
			t.Errorf("extractID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseID(t *testing.T) {
	cases := map[string]int64{
		"123":      123,
		"456abc":   456,
		"0":        0,
		"abc":      0,
		"99999999": 99999999,
	}
	for in, want := range cases {
		if got := parseID(in); got != want {
			t.Errorf("parseID(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestUiStatusForStage(t *testing.T) {
	cases := map[string]string{
		"下载中":  "downloading",
		"上传中":  "uploading",
		"排队中":  "queued",
		"已暂停":  "paused",
		"其他状态": "processing",
	}
	for in, want := range cases {
		if got := uiStatusForStage(in); got != want {
			t.Errorf("uiStatusForStage(%q) = %q, want %q", in, got, want)
		}
	}
}
