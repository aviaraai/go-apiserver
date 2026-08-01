package animal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"

	animal "go-api-server/internal/database/animal"
	"go-api-server/internal/geo"
	"go-api-server/internal/imaging"
	"go-api-server/internal/inference"

	"golang.org/x/sync/errgroup"
)

// imageFile holds a validated, in-memory image read from the multipart form.
// The bytes are kept so they can be used twice: once for inference and once
// for the storage upload.
type imageFile struct {
	field     string
	header    *multipart.FileHeader
	data      []byte
	validated string
}

func readAndValidateOne(header *multipart.FileHeader, label string) (*imageFile, error) {
	src, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", label, err)
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}

	contentType, err := imaging.ValidateImage(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}

	return &imageFile{
		field:     label,
		header:    header,
		data:      data,
		validated: contentType,
	}, nil
}

func readAndValidate(headers []*multipart.FileHeader, prefix string) ([]*imageFile, error) {
	out := make([]*imageFile, len(headers))
	for i, h := range headers {
		img, err := readAndValidateOne(h, fmt.Sprintf("%s_%d", prefix, i+1))
		if err != nil {
			return nil, err
		}
		out[i] = img
	}
	return out, nil
}

func toInferencePayload(img *imageFile) inference.ImagePayload {
	return inference.ImagePayload{
		Filename:    img.header.Filename,
		ContentType: img.validated,
		Data:        img.data,
	}
}

func toInferencePayloads(imgs []*imageFile) []inference.ImagePayload {
	out := make([]inference.ImagePayload, len(imgs))
	for i, img := range imgs {
		out[i] = inference.ImagePayload{
			Filename:    img.header.Filename,
			ContentType: img.validated,
			Data:        img.data,
		}
	}
	return out
}

func filterByRadius(rows []animal.CandidateRow, lat, lng, radiusKm float64) []animal.CandidateRow {
	filtered := make([]animal.CandidateRow, 0, len(rows))
	for _, r := range rows {
		if geo.HaversineKm(lat, lng, r.Latitude, r.Longitude) <= radiusKm {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func buildUploadList(front, muzzle []*imageFile, left, right, health, valuation *imageFile) []uploadTask {
	var tasks []uploadTask
	for i, f := range front {
		tasks = append(tasks, uploadTask{imageType: "front", sequence: i + 1, img: f})
	}
	for i, m := range muzzle {
		tasks = append(tasks, uploadTask{imageType: "muzzle", sequence: i + 1, img: m})
	}
	tasks = append(tasks,
		uploadTask{imageType: "left", sequence: 1, img: left},
		uploadTask{imageType: "right", sequence: 1, img: right},
	)
	if health != nil {
		tasks = append(tasks, uploadTask{imageType: "health_certificate", sequence: 1, img: health})
	}
	if valuation != nil {
		tasks = append(tasks, uploadTask{imageType: "valuation_certificate", sequence: 1, img: valuation})
	}
	return tasks
}

type uploadTask struct {
	imageType string
	sequence  int
	img       *imageFile
}

type uploadResult struct {
	imageType string
	sequence  int
	imageKey  string
}

func (h *Handler) uploadAll(ctx context.Context, godhaarID string, tasks []uploadTask) ([]uploadResult, error) {
	results := make([]uploadResult, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(7)

	for i := range tasks {
		g.Go(func() error {
			ext, err := imaging.ExtensionForContentType(tasks[i].img.validated)
			if err != nil {
				return fmt.Errorf("%s: %w", tasks[i].imageType, err)
			}
			// Key format: animal/{godhaarID}/{image_type}{image_sequence}{image_ext}
			key := fmt.Sprintf("animal/%s/%s%d%s", godhaarID, tasks[i].imageType, tasks[i].sequence, ext)
			if err := h.Storage.Upload(gctx, bytes.NewReader(tasks[i].img.data), key, tasks[i].img.validated); err != nil {
				return fmt.Errorf("upload %s: %w", tasks[i].imageType, err)
			}
			results[i] = uploadResult{imageType: tasks[i].imageType, sequence: tasks[i].sequence, imageKey: key}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func toCreateImages(results []uploadResult) []animal.CreateImage {
	out := make([]animal.CreateImage, len(results))
	for i, r := range results {
		out[i] = animal.CreateImage{
			ImageType: r.imageType,
			Sequence:  r.sequence,
			ImageKey:  r.imageKey,
		}
	}
	return out
}

func toRegisterResponse(a *animal.Animal) *RegisterResponse {
	return &RegisterResponse{
		GodhaarID: a.GodhaarID,
	}
}
