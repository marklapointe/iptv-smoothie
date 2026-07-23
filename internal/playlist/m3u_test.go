package playlist_test

import (
	"strings"
	"testing"

	"github.com/mlapointe/smoothie/internal/playlist"
)

const sampleM3U = `#EXTM3U
#EXTINF:-1 tvg-id="SVT1.se" tvg-name="SWE| SVT 1 HD" tvg-logo="http://logo/svt1.png" group-title="SWEDEN HD & HEVC",SWE| SVT 1 HD
http://line.example:80/user/pass/79662
#EXTINF:-1 tvg-id="" tvg-name="Movie Foo" tvg-logo="" group-title="|EN| MOVIES 2018-2021",Movie Foo (2020)
http://line.example:80/movie/user/pass/12345.mp4
#EXTINF:-1 tvg-id="" tvg-name="Show S01E01" group-title="|EN| ENGLISH SERIES",Show S01E01
http://line.example:80/series/user/pass/99.mkv
#EXTINF:-1 tvg-name="##### FAVORITES #####" group-title="SWEDEN HD & HEVC",##### FAVORITES #####
http://line.example:80/user/pass/1
`

func TestParse_StreamingEntries(t *testing.T) {
	t.Parallel()
	var entries []playlist.Entry
	err := playlist.Parse(strings.NewReader(sampleM3U), func(e playlist.Entry) error {
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	e0 := entries[0]
	if e0.Name != "SWE| SVT 1 HD" {
		t.Errorf("name = %q", e0.Name)
	}
	if e0.TvgID != "SVT1.se" {
		t.Errorf("tvg-id = %q", e0.TvgID)
	}
	if e0.Group != "SWEDEN HD & HEVC" {
		t.Errorf("group = %q", e0.Group)
	}
	if e0.Logo == "" {
		t.Error("expected logo")
	}
	if e0.URL != "http://line.example:80/user/pass/79662" {
		t.Errorf("url = %q", e0.URL)
	}
	if e0.Kind != playlist.KindLive {
		t.Errorf("kind = %s, want live", e0.Kind)
	}
	if entries[1].Kind != playlist.KindVOD {
		t.Errorf("movie kind = %s", entries[1].Kind)
	}
	if entries[2].Kind != playlist.KindVOD {
		t.Errorf("series kind = %s", entries[2].Kind)
	}
	// Header-ish favorites still live stream URL without media ext
	if entries[3].Kind != playlist.KindLive {
		t.Errorf("favorites kind = %s", entries[3].Kind)
	}
}

func TestParse_RemoteKeyStable(t *testing.T) {
	t.Parallel()
	var keys []string
	_ = playlist.Parse(strings.NewReader(sampleM3U), func(e playlist.Entry) error {
		keys = append(keys, e.RemoteKey)
		return nil
	})
	if keys[0] == "" || keys[0] == keys[1] {
		t.Fatalf("remote keys should be non-empty and distinct: %v", keys)
	}
}

func TestClassifyKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		group, url string
		want       playlist.Kind
	}{
		{"News", "http://x/user/pass/1", playlist.KindLive},
		{"Movies", "http://x/movie/u/p/1.mp4", playlist.KindVOD},
		{"X", "http://x/series/u/p/1.mkv", playlist.KindVOD},
		{"|EN| ENGLISH SERIES", "http://x/u/p/1.ts", playlist.KindVOD},
		{"Live Sports", "http://x/live/u/p/9", playlist.KindLive},
	}
	for _, tc := range cases {
		got := playlist.ClassifyKind(tc.group, tc.url)
		if got != tc.want {
			t.Errorf("ClassifyKind(%q,%q)=%s want %s", tc.group, tc.url, got, tc.want)
		}
	}
}

func TestRewrite_LocalPlayURLs(t *testing.T) {
	t.Parallel()
	in := []playlist.Entry{
		{Name: "A", Group: "G", URL: "http://up/1", Kind: playlist.KindLive, Logo: "http://l", TvgID: "a"},
	}
	var b strings.Builder
	if err := playlist.Write(&b, "http://smoothie.lan:8787", []playlist.RewriteItem{
		{ChannelID: "cid-1", Entry: in[0]},
	}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "#EXTM3U") {
		t.Fatal("missing header")
	}
	if !strings.Contains(out, "http://smoothie.lan:8787/play/cid-1") {
		t.Fatalf("missing rewritten URL: %s", out)
	}
	if strings.Contains(out, "http://up/1") {
		t.Fatal("upstream URL should not appear in rewritten playlist")
	}
}
