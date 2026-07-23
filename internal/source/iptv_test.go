package source_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

const smallM3U = `#EXTM3U
#EXTINF:-1 tvg-id="a" group-title="News",News 1
http://ex/user/pass/1
#EXTINF:-1 group-title="|EN| MOVIES",Film
http://ex/movie/user/pass/2.mp4
`

func TestRefreshFromReader_ClassifiesAndStores(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	src := store.Source{
		Name:       "Lab",
		Type:       store.SourceTypeIPTVM3U,
		Enabled:    true,
		ConfigJSON: `{"urls":["http://example.test/get.php"]}`,
	}
	if err := db.CreateSource(&src); err != nil {
		t.Fatal(err)
	}

	r := source.NewRefresher(db)
	res, err := r.RefreshFromReader(&src, strings.NewReader(smallM3U), "http://example.test/get.php?password=secret")
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Live != 1 || res.VOD != 1 {
		t.Fatalf("result = %+v", res)
	}
	if strings.Contains(res.FetchedURL, "secret") {
		t.Fatalf("password leaked in FetchedURL: %s", res.FetchedURL)
	}

	chs, err := db.ListChannelsBySource(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 2 {
		t.Fatalf("channels = %d", len(chs))
	}

	// Second refresh replaces
	res2, err := r.RefreshFromReader(&src, strings.NewReader(smallM3U), "http://example.test/get.php")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Total != 2 {
		t.Fatal(res2)
	}
	n, err := db.CountChannels(src.ID)
	if err != nil || n != 2 {
		t.Fatalf("count after replace = %d err=%v", n, err)
	}
}
