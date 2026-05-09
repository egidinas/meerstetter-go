package export

import (
	"context"
	"io"

	"github.com/egidinas/meerstetter-go/tmtclog"
)

// Format names archival output formats implemented by application adapters.
type Format string

const (
	FormatHDF5  Format = "hdf5"
	FormatJSONL Format = "jsonl"
)

type Request struct {
	Format   Format            `json:"format"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Entries  []tmtclog.Entry   `json:"entries,omitempty"`
}

// Writer is the shared export interface. HDF5 implementations can live in an
// app-specific package or CGO-enabled module while callers share this contract.
type Writer interface {
	WriteExport(ctx context.Context, w io.Writer, req Request) error
}
