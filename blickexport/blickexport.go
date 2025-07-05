package blickexport

import (
	"net/http"

	"github.com/alesr/tidstrom"
)

type Exporter struct {
	inputCh <-chan tidstrom.Snapshot
	cli     *http.Client
}

func NewExporter(httpCli *http.Client, inputCh <-chan tidstrom.Snapshot) *Exporter {
	return &Exporter{}
}

func (e *Exporter) Export() error {
	// Implement export logic here
	return nil
}
