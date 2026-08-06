package animal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"sort"

	"go-api-server/internal/database/animal"
	"go-api-server/internal/id"
	"go-api-server/internal/inference"
	"go-api-server/internal/middleware"
	"go-api-server/internal/storage"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	AnimalsByFarmerID(context.Context, string) ([]animal.Animal, error)
	UnassignedAnimalsByUser(context.Context, string) ([]animal.Animal, error)
	GetAnimal(context.Context, string) (*animal.Animal, error)
	FarmerIDByPublicID(context.Context, string) (*int64, error)
	FindFAISSCandidates(context.Context, float64, float64, float64) ([]animal.CandidateRow, error)
	CreateAnimalWithEmbeddingsAndImages(context.Context, animal.CreateAnimalTx) (*animal.Animal, error)
	AddDebugAnimal(context.Context, animal.DebugCreateParams) error
}

type Handler struct {
	DB        Repository
	Storage   storage.Storage
	Inference inference.Client
}

// bboxDegrees is the bounding-box half-width (in degrees) used to pre-filter
// FAISS candidates before the exact Haversine radius filter. ~0.05° ≈ 5.5km.
const bboxDegrees = 0.05

// searchRadiusKm is the exact Haversine radius applied after the bounding-box
// pre-filter. Shared by both register and search so candidate selection is
// identical on both paths. Matches the Python SEARCH_RADIUS_KM (3 km).
const searchRadiusKm = 3.0

// searchTopK bounds how many embedding matches the inference server returns per
// search. Kept small to limit FAISS load — the decision only needs the top
// animal and the runner-up, so a handful of embeddings is enough.
const searchTopK = 5

func toAnimalResponse(a *animal.Animal) *AnimalResponse {
	return &AnimalResponse{
		GodhaarID:        a.GodhaarID,
		PublicID:         a.PublicID,
		Type:             a.Type,
		Gender:           a.Gender,
		Breed:            a.Breed,
		Age:              a.Age,
		Cost:             a.Cost,
		InsurancePremium: a.InsurancePremium,
		State:            a.State,
		District:         a.District,
		Mandal:           a.Mandal,
		Village:          a.Village,
	}
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	animalGroup := g.Group("/animals")
	animalGroup.GET("", h.listAnimals)
	animalGroup.GET("/:godhaar_id", h.getAnimal)
	animalGroup.POST("/register", h.register, middleware.UserLockMiddleware, middleware.DeviceInfoMiddleware)
	animalGroup.POST("/search", h.search, middleware.DeviceInfoMiddleware)
}

// Farmer all cattle and check same user
// Unassigned all user cattle
func (h *Handler) listAnimals(c echo.Context) error {
	ctx := c.Request().Context()
	userID := middleware.UserIDFromContext(c)

	var req FarmerIDRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing farmer id")
	}

	var animals []animal.Animal
	var err error

	if req.PublicID == nil {
		animals, err = h.DB.UnassignedAnimalsByUser(ctx, userID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to list animals").SetInternal(err)
		}
	} else {
		animals, err = h.DB.AnimalsByFarmerID(ctx, *req.PublicID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to list animals for farmer id %s", *req.PublicID)).SetInternal(err)
		}
	}

	animalRes := make([]AnimalResponse, len(animals))
	for i := range animals {
		url, err := h.Storage.PresignedURL(ctx, animals[i].ImageKey)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get signed url").SetInternal(err)
		}
		animalRes[i] = *toAnimalResponse(&animals[i])
		animalRes[i].ImageURL = url
	}
	return c.JSON(http.StatusOK, animalRes)
}

func (h *Handler) getAnimal(c echo.Context) error {
	ctx := c.Request().Context()

	var req AnimalRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing godhaar id")
	}

	f, err := h.DB.GetAnimal(ctx, req.GodhaarID)
	if err != nil {
		if errors.Is(err, animal.ErrRowNotFound) {
			return echo.NewHTTPError(http.StatusBadRequest, "animal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	return c.JSON(http.StatusOK, toAnimalResponse(f))
}

func (h *Handler) register(c echo.Context) error {
	ctx := c.Request().Context()
	userID := middleware.UserIDFromContext(c)
	email := middleware.EmailFromContext(c)

	var req RegisterAnimalRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body").SetInternal(err)
	}

	form, err := c.MultipartForm()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid multipart form")
	}

	frontHeaders := form.File["front_images"]
	if len(frontHeaders) != 2 {
		return echo.NewHTTPError(http.StatusBadRequest, "two front images are required")
	}
	muzzleHeaders := form.File["muzzle_images"]
	if len(muzzleHeaders) != 3 {
		return echo.NewHTTPError(http.StatusBadRequest, "three muzzle images are required")
	}

	leftHeader, err := c.FormFile("left_image")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "left image is required")
	}
	rightHeader, err := c.FormFile("right_image")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "right image is required")
	}

	var healthCertHeader *multipart.FileHeader
	if files := form.File["health_certificate"]; len(files) > 0 {
		healthCertHeader = files[0]
	}
	var valuationCertHeader *multipart.FileHeader
	if files := form.File["valuation_certificate"]; len(files) > 0 {
		valuationCertHeader = files[0]
	}

	// Read + validate all images into memory once.
	// We need the bytes twice: once for inference, once for storage upload.
	frontImgs, err := readAndValidate(frontHeaders, "front")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	muzzleImgs, err := readAndValidate(muzzleHeaders, "muzzle")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	leftImg, err := readAndValidateOne(leftHeader, "left")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	rightImg, err := readAndValidateOne(rightHeader, "right")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var healthCert, valuationCert *imageFile
	if healthCertHeader != nil {
		healthCert, err = readAndValidateOne(healthCertHeader, "health_certificate")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}
	if valuationCertHeader != nil {
		valuationCert, err = readAndValidateOne(valuationCertHeader, "valuation_certificate")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	// Query nearby FAISS candidates (bounding box + Haversine filter).
	lat, lng := req.Latitude, req.Longitude
	candidateRows, err := h.DB.FindFAISSCandidates(ctx, lat, lng, bboxDegrees)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to find candidates").SetInternal(err)
	}
	nearby := filterByRadius(candidateRows, lat, lng, searchRadiusKm)
	candidates := make([]inference.Candidate, len(nearby))
	for i, c := range nearby {
		candidates[i] = inference.Candidate{
			FaissID:     c.FaissID,
			BodyColor:   c.BodyColor,
			MuzzleColor: c.MuzzleColor,
			HornShape:   c.HornShape,
		}
	}

	// Call inference server. This is what actually detects duplicates.
	infResp, err := h.Inference.Register(ctx, toInferencePayloads(frontImgs), toInferencePayloads(muzzleImgs), candidates)
	if err != nil {
		switch {
		case errors.Is(err, inference.ErrDuplicateAnimal):
			debugErr := h.uploadRegisterDebugData(ctx, frontImgs, muzzleImgs, userID, err.Error())
			if debugErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(debugErr)
			}
			return echo.NewHTTPError(http.StatusConflict, err.Error()).SetInternal(err)
		case errors.Is(err, inference.ErrPoorImageQuality):
			debugErr := h.uploadRegisterDebugData(ctx, frontImgs, muzzleImgs, userID, err.Error())
			if debugErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(debugErr)
			}
			return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error()).SetInternal(err)
		case errors.Is(err, inference.ErrInferenceInternal):
			return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error()).SetInternal(err)
		default:
			return echo.NewHTTPError(http.StatusBadGateway, err.Error()).SetInternal(err)
		}
	}

	if len(infResp.EmbeddingIDs) != 3 {
		return echo.NewHTTPError(http.StatusBadGateway, "inference server returned wrong embedding count").SetInternal(
			fmt.Errorf("expected 3 embedding ids, got %d", len(infResp.EmbeddingIDs)))
	}

	// Only now — after inference confirmed this is a new registration — generate ID and upload.
	godhaarID, err := id.GenerateGodhaarID(req.State, req.District, req.Breed)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate godhaar id").SetInternal(err)
	}

	uploads := buildUploadList(frontImgs, muzzleImgs, leftImg, rightImg, healthCert, valuationCert)
	results, err := h.uploadAll(ctx, godhaarID, uploads)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to upload images").SetInternal(err)
	}

	// Nil value to handle unassigned animal
	var farmerID *int64

	if req.PublicID != nil {
		farmerID, err = h.DB.FarmerIDByPublicID(ctx, *req.PublicID)
		if err != nil && !errors.Is(err, animal.ErrFarmerNotFound) {
			return echo.NewHTTPError(http.StatusInternalServerError, "internal server error").SetInternal(err)
		}
	}

	// Transactional insert: animal + 3 embeddings + N images, all-or-nothing.
	tx := animal.CreateAnimalTx{
		Animal: animal.CreateAnimal{
			GodhaarID:        godhaarID,
			FarmerID:         farmerID,
			Type:             req.Type,
			Gender:           req.Gender,
			Cost:             req.Cost,
			InsurancePremium: req.InsurancePremium,
			Breed:            req.Breed,
			Age:              req.Age,
			State:            req.State,
			District:         req.District,
			Mandal:           req.Mandal,
			Village:          req.Village,
			BodyColor:        infResp.ExtractedColors.Body.Label,
			MuzzleColor:      infResp.ExtractedColors.Muzzle.Label,
			HornShape:        infResp.HornShape,
			HealthRemarks:    req.HealthRemarks,
			Latitude:         req.Latitude,
			Longitude:        req.Longitude,
			CreatedBy:        userID,
			CreatedByEmail:   email,
			UpdatedBy:        userID,
			UpdatedByEmail:   email,
		},
		Embeddings: []animal.CreateEmbedding{
			{EmbeddingType: "muzzle", Sequence: 1, FaissID: infResp.EmbeddingIDs[0]},
			{EmbeddingType: "muzzle", Sequence: 2, FaissID: infResp.EmbeddingIDs[1]},
			{EmbeddingType: "muzzle", Sequence: 3, FaissID: infResp.EmbeddingIDs[2]},
		},
		Images: toCreateImages(results),
	}

	created, err := h.DB.CreateAnimalWithEmbeddingsAndImages(ctx, tx)
	if err != nil {
		switch {
		case errors.Is(err, animal.ErrInvalidAnimalData):
			return echo.NewHTTPError(http.StatusBadRequest, "invalid animal details").SetInternal(err)
		case errors.Is(err, animal.ErrCreateNoRowReturned):
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to register animal").SetInternal(err)
		default:
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to register animal").SetInternal(err)
		}
	}

	return c.JSON(http.StatusCreated, toRegisterResponse(created))
}

func (h *Handler) search(c echo.Context) error {
	ctx := c.Request().Context()

	var req SearchAnimalRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	muzzleHeader, err := c.FormFile("muzzle_image")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "muzzle image is required")
	}
	frontHeader, err := c.FormFile("front_image")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "front image is required")
	}

	muzzleImg, err := readAndValidateOne(muzzleHeader, "muzzle")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	frontImg, err := readAndValidateOne(frontHeader, "front")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// ── Bounding-box SQL pre-filter + exact Haversine radius filter ────────
	candidateRows, err := h.DB.FindFAISSCandidates(ctx, req.Latitude, req.Longitude, bboxDegrees)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to find candidates").SetInternal(err)
	}
	nearby := filterByRadius(candidateRows, req.Latitude, req.Longitude, searchRadiusKm)

	if len(nearby) == 0 {
		log.Printf("search result: no candidates within %.0f km | lat=%.6f lng=%.6f",
			searchRadiusKm, req.Latitude, req.Longitude)
		return c.JSON(http.StatusOK, SearchResponse{Score: 0})
	}

	// Build lookup tables from faiss_id: one row per embedding, 3 per cattle.
	// animalToGodhaar lets us translate the DB-only animal_id back to the
	// public godhaar_id before responding — animal_id never leaves the server.
	faissToGodhaar := make(map[int64]string, len(nearby))
	candidates := make([]inference.Candidate, len(nearby))
	for i, r := range nearby {
		faissToGodhaar[r.FaissID] = r.GodhaarID
		candidates[i] = inference.Candidate{
			FaissID:     r.FaissID,
			BodyColor:   r.BodyColor,
			MuzzleColor: r.MuzzleColor,
			HornShape:   r.HornShape,
		}
	}

	// ── Call inference server ──────────────────────────────────────────────
	infResp, err := h.Inference.Search(
		ctx,
		toInferencePayload(frontImg),
		toInferencePayload(muzzleImg),
		candidates,
		searchTopK,
	)
	if err != nil {
		switch {
		case errors.Is(err, inference.ErrPoorImageQuality):
			return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error()).SetInternal(err)
		case errors.Is(err, inference.ErrInferenceInternal):
			return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error()).SetInternal(err)
		default:
			return echo.NewHTTPError(http.StatusBadGateway, err.Error()).SetInternal(err)
		}
	}

	// ── Aggregate embedding-level → cattle-level (max score) ───────────────
	cattleScores := make(map[string]float64)
	for _, m := range infResp.TopMatches {
		gid, ok := faissToGodhaar[m.FaissID]
		if !ok {
			// faiss_id returned by inference not in our candidate set — skip.
			continue
		}
		if s, seen := cattleScores[gid]; !seen || m.Score > s {
			cattleScores[gid] = m.Score
		}
	}

	ranked := make([]rankedAnimal, 0, len(cattleScores))
	for gid, score := range cattleScores {
		ranked = append(ranked, rankedAnimal{GodhaarID: gid, Score: score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	// ── Decision engine ────────────────────────────────────────────────────
	// Color labels from inference (infResp.QueryColors) are intentionally NOT
	// used to hard-filter — classifier confidence is unreliable — so, matching
	// the Python flow, all ranked candidates pass to the decision engine.
	v := decide(ranked)

	// Full verdict is logged for debugging; the frontend only receives
	// godhaar_id + score.
	gidLog := "none"
	if v.GodhaarID != nil {
		gidLog = *v.GodhaarID
	}
	log.Printf("search result: godhaar_id=%s decision=%s score=%.6f gap=%.6f reason=%s | lat=%.6f lng=%.6f",
		gidLog, v.Decision, v.Score, v.Gap, v.Reason, req.Latitude, req.Longitude)

	return c.JSON(http.StatusOK, SearchResponse{
		GodhaarID: v.GodhaarID,
		Decision:  v.Decision,
		Score:     v.Score,
	})
}
