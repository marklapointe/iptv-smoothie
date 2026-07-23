package stream

import (
	"io"
	"os"
)

func openFile(path string) (io.ReadCloser, error) {
	return os.Open(path)
}
