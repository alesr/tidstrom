package exporter

import (
	"net/http"

	"github.com/alesr/tidstrom/streambuffer"
)

type Exporter struct {
	inputCh <-chan streambuffer.Snapshot
	cli     *http.Client
}

func NewExporter(httpCli *http.Client, inputCh <-chan streambuffer.Snapshot) *Exporter {
	return &Exporter{}
}

func (e *Exporter) Export() error {
	// Implement export logic here
	return nil
}
