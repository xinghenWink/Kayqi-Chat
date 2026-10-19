package conversation

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	appcm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/contentmoderation"
	appstorage "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/objectstorage"
	apppoints "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/points"
	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstorage"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	cmrepo "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/contentmoderation"
	conversationrepo "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/conversation"
	pointsrepo "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/points"
	settingsrepo "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/settings"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/schema"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/secretbox"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/ports/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const verifiedTestImageCaps = `{"image":{"stream":false,"parameters":{"qualities":["low","high"],"sizes":["1024x1024"],"maxCount":2,"maxReferenceImages":2}}}`

// 模拟上游但保留真实应用编排、文件系统与 SQLite 事务，检查交付和积分的一致性。
type pointImageGateway struct {
	conversationRunClaimLLMGateway
	output *llm.GenerateOutput
	fail   error
	before func(context.Context, llm.GenerateInput)
}

func (g *pointImageGateway) Generate(ctx context.Context, _ llm.RouteConfig, input llm.GenerateInput) (*llm.GenerateOutput, error) {
	g.generateCalls++
	if g.before != nil {
		g.before(ctx, input)
	}
	return g.output, g.fail
}

type pointImageRoutes struct{}

func (*pointImageRoutes) ResolveRoute(_ context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error) {
	protocol := llm.AdapterOpenAIImageGenerations
	if input.TaskType == channel.TaskTypeImageEdit {
		protocol = llm.AdapterOpenAIImageEdits
	}
	return &channel.ResolvedRoute{Protocol: protocol, PlatformModelName: "test-image", UpstreamModel: "test-image", ModelCapabilitiesJSON: verifiedTestImageCaps}, nil
}
func (*pointImageRoutes) MarkRouteFailure(context.Context, *channel.ResolvedRoute, error) {}
func (*pointImageRoutes) MarkRouteSuccess(context.Context, *channel.ResolvedRoute)        {}

func pointImageFixture(t *testing.T) (*Service, *gorm.DB, *pointImageGateway) {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "integration.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(schema.Models()...); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	// 与轻量部署的单连接配置一致；积分仓储的并发压力另用多连接文件库测试。
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, row := range []any{
		&models.User{BaseModel: models.BaseModel{ID: 1}, PublicID: "image-test-user", Username: "image-test", Status: "active"},
		&models.LLMPlatformModel{ControlPlaneModel: models.ControlPlaneModel{ID: 1}, Name: "test-image", Status: "active"},
		&models.Conversation{BaseModel: models.BaseModel{ID: 1}, PublicID: "image-test-conversation", UserID: 1, Title: "New conversation", Model: "test-image", Status: "active"},
		&models.SystemSetting{Namespace: "points", Key: "enabled", Value: "true"},
		&models.SystemSetting{Namespace: "points", Key: "image_concurrent_global", Value: "1"},
		&models.SystemSetting{Namespace: "billing", Key: "mode", Value: "self"},
	} {
		if err = db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pr := pointsrepo.NewRepo(db)
	for _, task := range []string{"image_generation", "image_edit"} {
		if _, err = pr.SavePrice(t.Context(), p.Price{Model: "test-image", TaskType: task, Quality: "low", PointsPerImage: 5, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err = pr.Adjust(t.Context(), p.Adjustment{UserID: 1, ActorID: 1, Delta: 20, RefNo: "fixture-balance", Reason: "测试额度"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewRuntime(config.Config{MaxMessageFiles: 4, StorageBackend: "local", StorageRootDir: dir, ModelOptionPolicyMode: "denylist"})
	repo := conversationrepo.NewRepo(db)
	upload := appupload.NewServiceWithRuntime(cfg, repo, zap.NewNop(), appupload.Hooks{}, UploadErrorSet(), "test")
	upload.SetObjectStoreProvider(appstorage.NewRuntimeProvider(cfg, objectstorage.New))
	gateway := &pointImageGateway{}
	s := &Service{cfg: cfg, repo: repo, routeResolver: &pointImageRoutes{}, llmClient: gateway, uploadSvc: upload, logger: zap.NewNop(), generationStreams: newGenerationStreamRegistry(nil, defaultGenerationStreamOptions())}
	s.SetPointsService(apppoints.NewService(pr, settingsrepo.NewRepo(db)))
	pr.SetActiveChecker(s.HasActiveMessageGeneration)
	t.Cleanup(s.Close)
	return s, db, gateway
}

func pointTestPNG(t *testing.T, red uint8) llm.GeneratedImage {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: red, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return llm.GeneratedImage{B64JSON: base64.StdEncoding.EncodeToString(buf.Bytes()), MIMEType: "image/png"}
}

func imagePointInput(t *testing.T, s *Service, run string, count int) MediaImageInput {
	t.Helper()
	options := map[string]any{"quality": "low", "size": "1024x1024", "n": count}
	quote, err := s.QuoteImagePoints(t.Context(), 1, "test-image", "image_generation", options, nil)
	if err != nil {
		t.Fatal(err)
	}
	return MediaImageInput{UserID: 1, ConversationID: 1, ClientRunID: run, TaskType: MediaImageTaskGeneration, PlatformModelName: "test-image", Prompt: "绘制一张测试图片", Options: options, QuoteFingerprint: quote.Fingerprint}
}

func TestImagePointsDeliveryAndRefund(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		images    int
		invalid   bool
		duplicate bool
		text      string
		wantCount int
		wantError bool
	}{
		{name: "full", images: 2, wantCount: 2},
		{name: "partial", images: 1, wantCount: 1},
		{name: "artifact_partial", images: 2, invalid: true, wantCount: 1},
		{name: "duplicate_output", images: 2, duplicate: true, wantCount: 1},
		{name: "text_only", text: "无法生成图片", wantCount: 0},
		{name: "empty", wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, db, gateway := pointImageFixture(t)
			gateway.output = &llm.GenerateOutput{Text: scenario.text}
			for i := range scenario.images {
				gateway.output.GeneratedImages = append(gateway.output.GeneratedImages, pointTestPNG(t, uint8(i+1)))
			}
			if scenario.invalid {
				gateway.output.GeneratedImages[1].B64JSON = "not-base64"
			}
			if scenario.duplicate {
				gateway.output.GeneratedImages[1] = gateway.output.GeneratedImages[0]
			}
			input := imagePointInput(t, s, "run_"+scenario.name, 2)
			result, err := runPointImage(t, s, t.Context(), input)
			if (err != nil) != scenario.wantError {
				t.Fatalf("result error: %v", err)
			}
			if !scenario.wantError && result.AssistantMessage.PointsDelivered != scenario.wantCount {
				t.Fatalf("unexpected delivery: %+v", result.AssistantMessage)
			}
			wallet, err := s.pointsSvc.Wallet(t.Context(), 1, false)
			if err != nil || wallet.TotalPoints != 20-int64(scenario.wantCount)*5 || wallet.FrozenPoints != 0 {
				t.Fatalf("wallet=%+v error=%v", wallet, err)
			}
			var activeFiles int64
			db.Model(&models.FileObject{}).Where("status = ?", "active").Count(&activeFiles)
			if activeFiles != int64(scenario.wantCount) {
				t.Fatalf("unexpected visible/temporary files: %d", activeFiles)
			}
			if _, err = runPointImage(t, s, t.Context(), input); !errors.Is(err, ErrDuplicateMessageGenerationRun) || gateway.generateCalls != 1 {
				t.Fatalf("duplicate charged or called upstream: %v calls=%d", err, gateway.generateCalls)
			}
		})
	}
}

func TestImagePointsRejectsStaleQuoteAndCannotUseFreeChatImages(t *testing.T) {
	s, db, gateway := pointImageFixture(t)
	input := imagePointInput(t, s, "run_stale", 1)
	if err := db.Model(&models.ImagePointPrice{}).Where("task_type = ?", "image_generation").Updates(map[string]any{"points_per_image": 7, "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runPointImage(t, s, t.Context(), input); !errors.Is(err, p.ErrPriceChanged) {
		t.Fatalf("stale quote error: %v", err)
	}
	if gateway.generateCalls != 0 {
		t.Fatal("stale quote reached upstream")
	}
	options := s.textOnlyCallOptions(map[string]any{"tools": []any{map[string]any{"type": "image_generation"}, map[string]any{"type": "web_search"}}, "generationConfig": map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}})
	if len(options["tools"].([]any)) != 1 || options["generationConfig"].(map[string]any)["responseModalities"].([]string)[0] != "TEXT" {
		t.Fatalf("free chat image tool still enabled: %#v", options)
	}
}

// 有效参数而非原始请求参与报价，附加工具和未验证字段不会进入 Images 上游。
func TestImagePointQuoteUsesOnlyVerifiedParameters(t *testing.T) {
	s, _, _ := pointImageFixture(t)
	quote, err := s.QuoteImagePoints(t.Context(), 1, "test-image", "image_generation", map[string]any{"quality": "low", "n": 1, "tools": []any{"image_generation"}, "background": "transparent"}, nil)
	if err != nil || len(quote.Options) != 3 {
		t.Fatalf("unverified quote: %+v, %v", quote, err)
	}
	if _, err = s.QuoteImagePoints(t.Context(), 1, "test-image", "image_generation", map[string]any{"quality": "ultra"}, nil); !errors.Is(err, p.ErrInvalid) {
		t.Fatalf("unverified quality accepted: %v", err)
	}
	if _, err = s.QuoteImagePoints(t.Context(), 1, "test-image", "image_generation", map[string]any{"n": 3}, nil); !errors.Is(err, p.ErrInvalid) {
		t.Fatalf("unverified count accepted: %v", err)
	}
	if _, err = s.repo.GetActiveFileObjectsByIDs(t.Context(), 2, []string{"other-user-file"}); err != nil {
		t.Fatal(err)
	}
}

func TestImagePointsCancellationRefundsAndMarksMessage(t *testing.T) {
	s, db, gateway := pointImageFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	gateway.before = func(context.Context, llm.GenerateInput) { cancel() }
	gateway.fail = context.Canceled
	result, err := runPointImage(t, s, ctx, imagePointInput(t, s, "run_cancel", 1))
	if err != nil || result.AssistantMessage.Status != "canceled" {
		t.Fatalf("cancel: %+v %v", result, err)
	}
	wallet, err := s.pointsSvc.Wallet(t.Context(), 1, false)
	if err != nil || wallet.TotalPoints != 20 || wallet.FrozenPoints != 0 {
		t.Fatalf("cancel wallet: %+v %v", wallet, err)
	}
	var run models.ConversationRun
	if err = db.First(&run, "run_id = ?", "run_cancel").Error; err != nil || run.Status != "canceled" {
		t.Fatalf("cancel run: %+v %v", run, err)
	}
}

func TestImagePointsCommitFailureKeepsOutputPrivateAndRefunds(t *testing.T) {
	s, db, gateway := pointImageFixture(t)
	gateway.output = &llm.GenerateOutput{GeneratedImages: []llm.GeneratedImage{pointTestPNG(t, 7)}}
	failure := errors.New("simulated completion commit failure")
	if err := db.Callback().Update().Before("gorm:update").Register("test:reject-completion", func(tx *gorm.DB) {
		if tx.Statement.Table != "chat_messages" {
			return
		}
		if updates, ok := tx.Statement.Dest.(map[string]any); ok && updates["status"] == "success" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runPointImage(t, s, t.Context(), imagePointInput(t, s, "run_commit_fail", 1)); !errors.Is(err, failure) {
		t.Fatalf("completion should roll back: %v", err)
	}
	wallet, err := s.pointsSvc.Wallet(t.Context(), 1, false)
	if err != nil || wallet.TotalPoints != 20 || wallet.FrozenPoints != 0 {
		t.Fatalf("rollback wallet: %+v %v", wallet, err)
	}
	var active int64
	db.Model(&models.FileObject{}).Where("status = ?", "active").Count(&active)
	if active != 0 {
		t.Fatalf("unpaid output survived: %d", active)
	}
	db.Model(&models.Attachment{}).Where("kind = ?", "image").Count(&active)
	if active != 0 {
		t.Fatal("failed completion left an attachment")
	}
}

func TestImagePointsPendingFilesArePrivateAndRecoveredAfterRestart(t *testing.T) {
	s, db, _ := pointImageFixture(t)
	input := imagePointInput(t, s, "run_crash", 1)
	quote, err := s.QuoteImagePoints(t.Context(), 1, input.PlatformModelName, string(input.TaskType), input.Options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pointsSvc.Reserve(t.Context(), p.ReserveInput{RunID: input.ClientRunID, UserID: 1, ConversationID: 1, RequestHash: "test-crash", Quote: *quote}); err != nil {
		t.Fatal(err)
	}
	image := pointTestPNG(t, 1)
	data, _ := base64.StdEncoding.DecodeString(image.B64JSON)
	uploaded, err := s.uploadSvc.UploadFile(t.Context(), appupload.UploadFileInput{UserID: 1, Purpose: "generated_image_pending", GenerationRunID: input.ClientRunID, FileName: "pending.png", MimeType: "image/png", Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.repo.GetActiveFileObjectByID(t.Context(), 1, uploaded.File.FileID); !errors.Is(err, repository.ErrFileNotFound) {
		t.Fatalf("pending file is public: %v", err)
	}
	files, _, err := s.repo.ListFileObjectsByUserWithFilter(t.Context(), repository.ListFileObjectsInput{UserID: 1, Limit: 10})
	if err != nil || len(files) != 0 {
		t.Fatalf("pending file listed: %+v %v", files, err)
	}
	if err = db.Model(&models.PointReservation{}).Where("run_id = ?", input.ClientRunID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.pointsSvc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.CleanupUndeliveredImageFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	wallet, err := s.pointsSvc.Wallet(t.Context(), 1, false)
	if err != nil || wallet.TotalPoints != 20 || wallet.FrozenPoints != 0 {
		t.Fatalf("restart wallet: %+v %v", wallet, err)
	}
	var file models.FileObject
	if err = db.First(&file, uploaded.File.ID).Error; err != nil || file.Status != "deleted" {
		t.Fatalf("pending file not reclaimed: %+v %v", file, err)
	}
}

// 与 HTTP handler 相同地取得执行所有权，完成后释放恢复流。
func runPointImage(t *testing.T, s *Service, ctx context.Context, input MediaImageInput) (*SendMessageResult, error) {
	t.Helper()
	runCtx, release, ok := s.AcquireMessageGenerationLifecycle(ctx)
	if !ok {
		t.Fatal("generation lifecycle unavailable")
	}
	defer release()
	defer s.FinishMessageGeneration(runCtx, input.ClientRunID)
	return s.StreamMediaImage(runCtx, input)
}

type pointModerationProvider struct{}

func (pointModerationProvider) ValidateBaseURL(string) error { return nil }
func (pointModerationProvider) ModerateText(context.Context, appcm.ProviderConfig, string, []string, string) (*appcm.Response, error) {
	return &appcm.Response{}, nil
}
func (pointModerationProvider) ModerateImages(context.Context, appcm.ProviderConfig, []appcm.ProviderImage, []string, string) (*appcm.Response, error) {
	return &appcm.Response{ID: "mock-block", Results: []appcm.CategoryResult{{Flagged: true, Categories: map[string]bool{"violence": true}, CategoryAppliedInputTypes: map[string][]string{"violence": {"image"}}}}}, nil
}

// 输出审核命中时，图片不能被附件接口或恢复流交付，预留必须全额释放。
func TestImagePointsModerationBlocksBeforeDelivery(t *testing.T) {
	s, db, gateway := pointImageFixture(t)
	gateway.output = &llm.GenerateOutput{GeneratedImages: []llm.GeneratedImage{pointTestPNG(t, 1)}}
	keyring, err := secretbox.NewKeyring("isolated-point-moderation-test-key")
	if err != nil {
		t.Fatal(err)
	}
	cm := appcm.NewService(settingsrepo.NewRepo(db), cmrepo.NewRepo(db), keyring, zap.NewNop())
	cm.SetProvider(pointModerationProvider{})
	enabled, apiKey := true, "mock-key"
	policy := appcm.Policy{OutputImageCategories: []string{"violence"}}
	if _, err = cm.UpdateConfig(t.Context(), "superadmin", appcm.UpdateConfigInput{Enabled: &enabled, APIKey: &apiKey, Policy: &policy}); err != nil {
		t.Fatal(err)
	}
	s.SetModerationService(cm)
	cm.StartBackgroundWorkers(t.Context())
	t.Cleanup(cm.Stop)
	result, err := runPointImage(t, s, t.Context(), imagePointInput(t, s, "run_moderation", 1))
	if err != nil || !result.IsModerationBlocked() {
		t.Fatalf("moderation: %+v %v", result, err)
	}
	wallet, err := s.pointsSvc.Wallet(t.Context(), 1, false)
	if err != nil || wallet.TotalPoints != 20 || wallet.FrozenPoints != 0 {
		t.Fatalf("moderation wallet: %+v %v", wallet, err)
	}
	var attachments int64
	db.Model(&models.Attachment{}).Where("kind = ?", "image").Count(&attachments)
	if attachments != 0 {
		t.Fatal("blocked image was delivered")
	}
}
