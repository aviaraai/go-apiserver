package animal

import (
	"bytes"
	"context"
	"fmt"
	"go-api-server/internal/database/animal"
	"go-api-server/internal/imaging"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

func (h *Handler) uploadRegisterDebugData(ctx context.Context, frontImgs []*imageFile, muzzleImgs []*imageFile, userID, inference_info string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(7)

	uuidV7, err := uuid.NewV7()
	if err != nil {
		return err
	}

	var tasks []uploadTask
	for i, f := range frontImgs {
		tasks = append(tasks, uploadTask{imageType: "front", sequence: i + 1, img: f})
	}
	for i, m := range muzzleImgs {
		tasks = append(tasks, uploadTask{imageType: "muzzle", sequence: i + 1, img: m})
	}

	for i := range tasks {
		g.Go(func() error {
			ext, err := imaging.ExtensionForContentType(tasks[i].img.validated)
			if err != nil {
				return fmt.Errorf("%s: %w", tasks[i].imageType, err)
			}
			// Key format: debug/{uuid}/{image_type}{image_sequence}{image_ext}
			key := fmt.Sprintf("debug/%s/%s%d%s", uuidV7.String(), tasks[i].imageType, tasks[i].sequence, ext)
			if err := h.Storage.Upload(gctx, bytes.NewReader(tasks[i].img.data), key, tasks[i].img.validated); err != nil {
				return fmt.Errorf("upload %s: %w", tasks[i].imageType, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	if err = h.DB.AddDebugAnimal(ctx, animal.DebugCreateParams{ImageFolder: fmt.Sprintf("debug/%s", uuidV7.String()), InferenceInfo: inference_info, CreatedBy: userID}); err != nil {
		return err
	}

	return nil
}
