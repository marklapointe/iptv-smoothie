// Lab helper: ingest a local M3U file into a SQLite DB (not for production CI).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mlapointe/smoothie/internal/source"
	"github.com/mlapointe/smoothie/internal/store"
)

func main() {
	dbPath := flag.String("db", "", "sqlite path")
	m3uPath := flag.String("m3u", "", "local m3u path")
	name := flag.String("name", "Lab IPTV", "source name")
	flag.Parse()
	if *dbPath == "" || *m3uPath == "" {
		log.Fatal("usage: smoothie-lab-ingest -db path -m3u path")
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	src := store.Source{
		Name:       *name,
		Type:       store.SourceTypeIPTVM3U,
		Enabled:    true,
		ConfigJSON: `{"urls":["file://local-lab"]}`,
		LimitsJSON: `{"max_concurrent_upstreams":2,"max_upstream_bps":1500000}`,
	}
	if err := db.CreateSource(&src); err != nil {
		log.Fatal(err)
	}
	f, err := os.Open(*m3uPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	ref := source.NewRefresher(db)
	res, err := ref.RefreshFromReader(&src, f, "file://"+*m3uPath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("source_id=%s total=%d live=%d vod=%d\n", res.SourceID, res.Total, res.Live, res.VOD)
}
