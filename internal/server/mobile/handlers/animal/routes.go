package animal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"

	"go-api-server/internal/database/animal"
	debugdb "go-api-server/internal/database/debug"
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
	GetAnimalByTagID(context.Context, string) (*animal.Animal, error)
}

// DebugRepository is the developer-facing record of what the model did. It is a
// separate interface from Repository because writing to it must never be able
// to affect the user-facing outcome — see debug.go.
type DebugRepository interface {
	RecordRegistrationFailure(context.Context, debugdb.CreateRegistrationFailure) error
	RecordSearch(context.Context, debugdb.CreateSearchRecord) error
}

type Handler struct {
	DB        Repository
	DebugDB   DebugRepository
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

// searchRadiusTiersKm are the candidate radii search tries in order, stopping at
// the first tier that finds at least one candidate. The first tier is the
// tight, fast path (searchRadiusKm); the wider tier exists because a moved
// animal, a different field visit, or a weak GPS fix all leave the animal's
// registration point well outside 3 km — and the old behaviour answered those
// searches with a hard UNKNOWN before the model was ever consulted. Widening is
// safe: the decision engine still gates the verdict on the embedding score, so
// extra candidates can only turn a no-candidate UNKNOWN into "something to
// compare against", never into a confident wrong MATCH.
//
// Register does NOT use these tiers — its duplicate check stays on the tight
// radius, because widening a duplicate check risks false-positive rejections
// that block a registration, which is the worse failure.
var searchRadiusTiersKm = []float64{searchRadiusKm, 50.0}

// bboxDegreesForRadius returns a bounding-box half-width (in degrees) guaranteed
// to contain the given radius circle. 1° latitude ≈ 111 km, plus a small margin;
// the exact Haversine filter runs afterwards, so the box only needs to be a
// superset, not tight.
func bboxDegreesForRadius(radiusKm float64) float64 {
	return radiusKm/111.0 + 0.05
}

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
		TagID:            a.TagID,
		State:            a.State,
		District:         a.District,
		Mandal:           a.Mandal,
		Village:          a.Village,
		HealthRemarks:    a.HealthRemarks,
	}
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	animalGroup := g.Group("/animals")
	animalGroup.GET("", h.listAnimals)
	animalGroup.GET("/:godhaar_id", h.getAnimal)
	animalGroup.POST("/register", h.register, middleware.UserLockMiddleware, middleware.DeviceInfoMiddleware())
	animalGroup.POST("/search", h.search, middleware.DeviceInfoMiddleware())
	animalGroup.GET("/search/:tag_id", h.searchByTagID)
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
		return uploadHTTPError(err)
	}
	muzzleImgs, err := readAndValidate(muzzleHeaders, "muzzle")
	if err != nil {
		return uploadHTTPError(err)
	}
	leftImg, err := readAndValidateOne(leftHeader, "left")
	if err != nil {
		return uploadHTTPError(err)
	}
	rightImg, err := readAndValidateOne(rightHeader, "right")
	if err != nil {
		return uploadHTTPError(err)
	}

	var healthCert, valuationCert *imageFile
	if healthCertHeader != nil {
		healthCert, err = readAndValidateOne(healthCertHeader, "health_certificate")
		if err != nil {
			return uploadHTTPError(err)
		}
	}
	if valuationCertHeader != nil {
		valuationCert, err = readAndValidateOne(valuationCertHeader, "valuation_certificate")
		if err != nil {
			return uploadHTTPError(err)
		}
	}

	// Query nearby FAISS candidates (bounding box + Haversine filter).
	lat, lng := req.Latitude, req.Longitude
	candidateRows, err := h.DB.FindFAISSCandidates(ctx, lat, lng, bboxDegrees)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to find candidates").SetInternal(err)
	}
	nearby := filterByRadius(candidateRows, lat, lng, searchRadiusKm)

	// The inference server only ever knows FAISS ids. Keeping the reverse map
	// is what lets a duplicate rejection name the existing registration —
	// see duplicateDetails.
	faissToGodhaar := make(map[int64]string, len(nearby))
	candidates := make([]inference.Candidate, len(nearby))
	for i, c := range nearby {
		faissToGodhaar[c.FaissID] = c.GodhaarID
		candidates[i] = inference.Candidate{
			FaissID:     c.FaissID,
			BodyColor:   c.BodyColor,
			MuzzleColor: c.MuzzleColor,
			HornShape:   c.HornShape,
			TagID:       c.TagID,
		}
	}

	// Call inference server. This is what actually detects duplicates.
	// The response shape is validated inside the client, so reaching past this
	// point means infResp carries three embedding ids and both colour labels.
	infResp, err := h.Inference.Register(ctx, toInferencePayloads(frontImgs), toInferencePayloads(muzzleImgs), candidates, req.TagID)
	if err != nil {
		infErr := inference.Classify(err)
		matchedGodhaarID, matchErr := duplicateGodhaarID(infErr, faissToGodhaar)
		captureErr := h.captureRegistrationFailure(c, infErr, frontImgs, muzzleImgs, matchedGodhaarID)
		return inferenceHTTPError(infErr,
			registerErrorDetails(infErr, matchedGodhaarID),
			errors.Join(captureErr, matchErr))
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
			TagID:            req.TagID,
			BodyColor:        infResp.ExtractedColors.Body.Label,
			MuzzleColor:      infResp.ExtractedColors.Muzzle.Label,
			HornShape:        infResp.HornShape,
			FaceGeometry:     faceGeometryText(infResp.FaceGeometry),
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

// searchCandidates finds the animals a search should compare against, widening
// the radius when a tighter tier finds nothing (see searchRadiusTiersKm). The
// returned radiusKm is the effective radius of the tier that produced the rows;
// radiusKm == 0 means the unbounded, index-wide fallback was used.
func (h *Handler) searchCandidates(ctx context.Context, lat, lng float64) ([]animal.CandidateRow, float64, error) {
	for _, radiusKm := range searchRadiusTiersKm {
		rows, err := h.DB.FindFAISSCandidates(ctx, lat, lng, bboxDegreesForRadius(radiusKm))
		if err != nil {
			return nil, 0, err
		}
		if nearby := filterByRadius(rows, lat, lng, radiusKm); len(nearby) > 0 {
			return nearby, radiusKm, nil
		}
	}

	// Unbounded fallback: a bbox half-width of 180° covers every valid
	// latitude/longitude, so this fetches every animal with an embedding and a
	// location, and no radius filter is applied. Reached only when neither
	// bounded tier found a candidate — the moved-across-the-state case.
	rows, err := h.DB.FindFAISSCandidates(ctx, lat, lng, 180.0)
	if err != nil {
		return nil, 0, err
	}
	return rows, 0, nil
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
		return uploadHTTPError(err)
	}
	frontImg, err := readAndValidateOne(frontHeader, "front")
	if err != nil {
		return uploadHTTPError(err)
	}

	// ── Bounding-box SQL pre-filter + exact Haversine radius filter ────────
	// Candidate selection widens when a tighter radius finds nothing — see
	// searchCandidates — so a moved animal or a weak GPS fix isn't a guaranteed
	// UNKNOWN before the model is ever consulted.
	nearby, radiusUsed, err := h.searchCandidates(ctx, req.Latitude, req.Longitude)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to find candidates").SetInternal(err)
	}

	// No animal registered near enough to compare against means the model is
	// never consulted, and the verdict below stands as written. It is still
	// recorded and still logged: from the dashboard's point of view this is
	// "searched, found nothing", and leaving it out would make the no-match
	// rate look better than it is.
	v := verdict{Decision: "UNKNOWN", Reason: "no_candidates"}

	if len(nearby) > 0 {
		// Build lookup tables from faiss_id: one row per embedding, 3 per
		// cattle. The attribute table feeds the decision engine's colour/horn
		// comparison, which needs what we have on record for each candidate.
		faissToGodhaar := make(map[int64]string, len(nearby))
		attributes := make(map[string]animalAttributes, len(nearby))
		candidates := make([]inference.Candidate, len(nearby))
		for i, r := range nearby {
			faissToGodhaar[r.FaissID] = r.GodhaarID
			attributes[r.GodhaarID] = animalAttributes{
				BodyColor:   r.BodyColor,
				MuzzleColor: r.MuzzleColor,
				HornShape:   r.HornShape,
			}
			candidates[i] = inference.Candidate{
				FaissID:     r.FaissID,
				BodyColor:   r.BodyColor,
				MuzzleColor: r.MuzzleColor,
				HornShape:   r.HornShape,
				TagID:       r.TagID,
			}
		}

		// ── Call inference server ──────────────────────────────────────────
		infResp, err := h.Inference.Search(
			ctx,
			toInferencePayload(frontImg),
			toInferencePayload(muzzleImg),
			candidates,
			searchTopK,
			req.TagID,
		)
		if err != nil {
			infErr := inference.Classify(err)
			captureErr := h.captureSearchFailure(c, infErr, frontImg, muzzleImg)
			return inferenceHTTPError(infErr, searchErrorDetails(infErr), captureErr)
		}

		// ── Aggregate embedding-level → cattle-level (max score) ───────────
		cattleScores := make(map[string]float64)
		for _, m := range infResp.TopMatches {
			gid, ok := faissToGodhaar[m.FaissID]
			if !ok {
				// faiss_id returned by inference not in our candidate set.
				continue
			}
			if s, seen := cattleScores[gid]; !seen || m.Score > s {
				cattleScores[gid] = m.Score
			}
		}

		// ── Decision engine ────────────────────────────────────────────────
		// The colour and horn labels from inference are compared against what
		// we have on record for each candidate, but only as a bounded
		// adjustment to the embedding score — never as a filter. See
		// attributeWeight for why.
		ranked := rankCandidates(cattleScores, attributes, queryAttributes{
			BodyColor:   infResp.QueryColors.Body.Label,
			MuzzleColor: infResp.QueryColors.Muzzle.Label,
			HornShape:   infResp.HornShape,
		})
		v = decide(ranked)

		// LightGlue is a second, independent check on the SAME top candidate
		// decide() just picked — a keypoint disagreement demotes by one step,
		// see applyLightglueDisagreement's doc comment for the safety proof.
		v = applyLightglueDisagreement(v, infResp.LightglueZone)
	}

	captureErr := h.captureSearch(c, v, frontImg, muzzleImg)

	// The one log line this package emits. A search that succeeds returns 200
	// and so never reaches customHTTPErrorHandler, which is where everything
	// else in this service is logged — but the verdict is the single most
	// useful record of what the model did, and a search that silently returned
	// the wrong animal is only ever diagnosed from it afterwards.
	//
	// A failed capture rides along here rather than being logged where it
	// happened, so this stays the only logging site on the success path.
	attrs := []slog.Attr{
		slog.String("requestID", middleware.GetRequestID(c)),
		slog.String("userID", middleware.UserIDFromContext(c)),
		slog.String("decision", v.Decision),
		slog.String("reason", v.Reason),
		slog.Float64("score", v.Score),
		slog.Float64("adjustedScore", v.AdjustedScore),
		slog.Float64("gap", v.Gap),
		slog.Float64("agreement", v.Agreement),
		slog.Int("candidates", len(nearby)),
		slog.Float64("radiusKm", radiusUsed), // 0 = unbounded (index-wide) fallback
		slog.Float64("latitude", req.Latitude),
		slog.Float64("longitude", req.Longitude),
	}
	if v.GodhaarID != nil {
		attrs = append(attrs, slog.String("topCandidate", *v.GodhaarID))
	}
	if captureErr != nil {
		attrs = append(attrs, slog.Any("captureError", captureErr))
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "search result", attrs...)

	return c.JSON(http.StatusOK, SearchResponse{
		GodhaarID: responseGodhaarID(v),
		Decision:  v.Decision,
		Score:     v.Score,
	})
}

// responseGodhaarID decides whether the client is told which animal came top.
//
// UNKNOWN means "this animal is not registered here", so naming the candidate
// that failed to clear the bar would invite the app to show it as a result. A
// REVIEW is the opposite: it exists precisely so a human can look at that
// candidate, so it has to be named.
func responseGodhaarID(v verdict) *string {
	if v.Decision == "UNKNOWN" {
		return nil
	}
	return v.GodhaarID
}

func (h *Handler) searchByTagID(c echo.Context) error {
	ctx := c.Request().Context()

	var req SearchAnimalByTagIDRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing tag id")
	}

	f, err := h.DB.GetAnimalByTagID(ctx, req.TagID)
	if err != nil {
		if errors.Is(err, animal.ErrRowNotFound) {
			return echo.NewHTTPError(http.StatusBadRequest, "animal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	return c.JSON(http.StatusOK, toAnimalResponse(f))
}
