package source_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

// BenchmarkRefreshFromReader_5k measures bulk ingest throughput (synthetic M3U).
func BenchmarkRefreshFromReader_5k(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	for i := 0; i < 5000; i++ {
		kind := "News"
		url := fmt.Sprintf("http://ex/user/pass/%d", i)
		if i%3 == 0 {
			kind = "|EN| MOVIES"
			url = fmt.Sprintf("http://ex/movie/user/pass/%d.mp4", i)
		}
		fmt.Fprintf(&sb, `#EXTINF:-1 tvg-id="%d" group-title="%s",Chan %d`+"\n%s\n", i, kind, i, url)
	}
	m3u := sb.String()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		db, err := store.Open(filepath.Join(b.TempDir(), "b.db"))
		if err != nil {
			b.Fatal(err)
		}
		src := store.Source{
			Name: "B", Type: store.SourceTypeIPTVM3U, Enabled: true,
			ConfigJSON: `{"urls":["http://x"]}`,
		}
		if err := db.CreateSource(&src); err != nil {
			b.Fatal(err)
		}
		ref := source.NewRefresher(db)
		b.StartTimer()
		_, err = ref.RefreshFromReader(&src, strings.NewReader(m3u), "http://x")
		b.StopTimer()
		_ = db.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}
