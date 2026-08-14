package cctv

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"go-api-server/internal/cctv"
	"go-api-server/internal/httperr"

	"github.com/labstack/echo/v4"
)

// maxUploadBytes caps an uploaded clip.
//
// It is enforced by BodyLimit on the route, which checks Content-Length before
// reading anything and then caps the read itself — so an oversized upload is
// refused up front rather than after being spooled to disk. The number is
// generous on purpose: an hour of 1080p CCTV footage sits well under it, and the
// analysis, not the transfer, is the slow part.
const maxUploadBytes int64 = 512 << 20 // 512 MiB

// videoFormField is the multipart field the dashboard puts the file in.
const videoFormField = "video"

// allowedVideoTypes maps the extensions we accept to the content type stored
// alongside the object.
//
// The extension decides the content type, not the browser's declared one: the
// declared type is client-controlled, and it is what gets served back to a
// player from a presigned URL. Formats outside this set are refused here rather
// than sent on to fail deep inside the pipeline, where the only thing we could
// tell the admin is that "something went wrong".
var allowedVideoTypes = map[string]string{
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".m4v":  "video/x-m4v",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
}

// analyseUpload runs the same analysis as analyse, on a clip the admin uploaded
// instead of one pulled from a camera.
//
// It exists because the camera integration is not written yet and, more
// durably, because footage often reaches an admin as a file — exported from a
// DVR, sent over by a goshala — with no live camera to pull from at all. The
// goshala is still required: it is what the analysis is recorded against, and
// what the history is filtered by.
func (h *Handler) analyseUpload(c echo.Context) error {
	var req AnalyseRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
			Message: "Invalid request body.",
		}).SetInternal(err)
	}

	header, err := c.FormFile(videoFormField)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
			Message: "Choose a video file to analyse.",
			Code:    codeVideoRequired,
		}).SetInternal(err)
	}

	contentType, ext, err := classifyUpload(header)
	if err != nil {
		return err
	}

	// Deliberately not opened yet. The fetcher is called once the request record
	// exists, so a file that turns out to be unreadable fails the same way a
	// camera that cannot be reached does — as a recorded, failed attempt.
	return h.startAnalysis(c, req.GoshalaPublicID, uploadedVideo(header, contentType, ext))
}

// classifyUpload vets the file and returns what to store it as. The returned
// error is already an HTTP error, since every rejection here is the admin's to
// fix and says so plainly.
func classifyUpload(header *multipart.FileHeader) (contentType string, ext string, err error) {
	if header.Size == 0 {
		return "", "", echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
			Message: "That video file is empty.",
			Code:    codeVideoUnreadable,
		})
	}
	// A belt-and-braces check behind BodyLimit: the route's limit covers the
	// whole request, so a file that individually exceeds the cap can only get
	// here if the limits ever drift apart.
	if header.Size > maxUploadBytes {
		return "", "", echo.NewHTTPError(http.StatusRequestEntityTooLarge, httperr.Response{
			Message: fmt.Sprintf("That video is too large. The limit is %d MB.", maxUploadBytes>>20),
			Code:    codeVideoTooLarge,
			Details: map[string]any{"max_bytes": maxUploadBytes},
		})
	}

	// path.Ext on the base name: the filename comes from the client and a
	// browser is not the only thing that can post here, so a name carrying
	// directories must not reach the storage key.
	ext = strings.ToLower(path.Ext(path.Base(toSlash(header.Filename))))
	contentType, known := allowedVideoTypes[ext]
	if !known {
		return "", "", echo.NewHTTPError(http.StatusUnsupportedMediaType, httperr.Response{
			Message: "That file type is not supported. Upload an MP4, MOV, WebM, MKV or AVI video.",
			Code:    codeVideoUnsupportedFormat,
			Details: map[string]any{"allowed_extensions": allowedExtensions()},
		})
	}
	return contentType, ext, nil
}

// toSlash normalises a Windows-style path to a slash-separated one, so a
// browser that sends "C:\Users\...\clip.mp4" as the filename still yields
// "clip.mp4" from path.Base.
func toSlash(name string) string {
	return strings.ReplaceAll(name, `\`, "/")
}

func allowedExtensions() []string {
	out := make([]string, 0, len(allowedVideoTypes))
	for ext := range allowedVideoTypes {
		out = append(out, ext)
	}
	return out
}

// uploadedVideo adapts a multipart file to the same shape a camera clip arrives
// in, so the pipeline downstream cannot tell the two apart.
//
// The name handed on is rebuilt from the request rather than taken from the
// client: it ends up in a storage key and in a multipart header sent to the
// inference server, and neither is a place to put an arbitrary string.
func uploadedVideo(header *multipart.FileHeader, contentType, ext string) videoFetcher {
	return func(_ context.Context, _ string) (*cctv.Video, error) {
		file, err := header.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUploadUnreadable, err)
		}
		return &cctv.Video{
			Filename:    "upload" + ext,
			ContentType: contentType,
			Body:        file,
		}, nil
	}
}
