// Package cctv defines where analysis videos come from.
//
// The camera integration itself is not written yet — how a clip is pulled from
// a goshala's camera depends on hardware and vendor decisions still outstanding.
// Everything downstream of the fetch (upload, inference, storage, history) is
// complete and exercised, so landing the real source is a matter of writing one
// implementation of Source and swapping it in at wiring time.
package cctv

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ErrSourceNotImplemented is returned by NotImplementedSource. Handlers report
// it as a service-unavailable rather than a bad request: nothing the admin did
// caused it and nothing they can do fixes it.
var ErrSourceNotImplemented = errors.New("cctv video source is not implemented yet")

// Video is a clip pulled from a camera.
//
// Body is a stream rather than a []byte because these are multi-minute files;
// the caller spools it to a temp file instead of holding it in memory. The
// caller always closes it, including on its own errors.
type Video struct {
	// Filename is used for the multipart upload to the inference server and
	// for the storage key's extension. A source that has no meaningful name
	// should supply something stable like "camera.mp4".
	Filename string

	// ContentType is stored alongside the object so the browser can play it
	// back directly from a presigned URL. Typically "video/mp4".
	ContentType string

	Body io.ReadCloser
}

// Source fetches a clip to analyse for one goshala.
//
// The signature is deliberately minimal — a goshala and a stream back. Whatever
// resolves that goshala to a camera (a config table, a vendor API, an on-site
// gateway) is the implementation's business, so that decision does not have to
// be made before the rest of this feature can ship.
type Source interface {
	FetchLatest(ctx context.Context, goshalaPublicID string) (*Video, error)
}

// NotImplementedSource is the placeholder wired in today. Every call fails
// cleanly, so the endpoint returns an honest "not available yet" instead of
// half-working.
type NotImplementedSource struct{}

func (NotImplementedSource) FetchLatest(context.Context, string) (*Video, error) {
	return nil, ErrSourceNotImplemented
}

// NaiveImplementationSource serves the same file from disk for every goshala,
// ignoring which one was asked for. It exists to exercise the rest of the
// pipeline before the camera integration lands.
type NaiveImplementationSource struct{}

func (NaiveImplementationSource) FetchLatest(context.Context, string) (*Video, error) {
	f, err := os.Open(naiveVideoPath)
	if err != nil {
		return nil, err
	}
	// Not closed here, deliberately. The open file *is* the Video's Body, and
	// ownership of it passes to the caller — which closes it once the clip has
	// been spooled. Closing it on the way out would hand back a stream that is
	// already shut, and every read of it would fail with ErrClosed.
	return &Video{
		// f.Name() is the path Open was given, not the base name. Only the
		// extension is actually used downstream — for the storage key — but a
		// relative path would put "../" in it.
		Filename:    filepath.Base(f.Name()),
		ContentType: "video/mp4",
		Body:        f,
	}, nil
}

// naiveVideoPath is relative to the process working directory, so it resolves
// only when the server is started from the directory this was written against.
const naiveVideoPath = "../video_analytics.mp4"
