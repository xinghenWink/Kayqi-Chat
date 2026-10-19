package conversation

import (
	"context"
	"encoding/json"
	domainchannel "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/channel"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"slices"
	"strings"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	apppoints "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/points"
	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/ports/llm"
)

func (s *Service) SetPointsService(service *apppoints.Service) { s.pointsSvc = service }

// QuoteImagePoints 复用用户模型路由授权，只接受首版已实现规范化的 Images 协议。
func (s *Service) QuoteImagePoints(ctx context.Context, userID uint, model, task string, options map[string]any, fileIDs []string) (*p.Quote, error) {
	if s.pointsSvc == nil {
		return nil, p.ErrDisabled
	}
	if _, err := s.pointsSvc.RequireActive(ctx); err != nil {
		return nil, err
	}
	taskType := channel.TaskTypeImageGeneration
	if task == string(MediaImageTaskEdit) {
		taskType = channel.TaskTypeImageEdit
	} else if task != string(MediaImageTaskGeneration) {
		return nil, p.ErrInvalid
	}
	route, err := s.routeResolver.ResolveRoute(ctx, channel.ResolveRouteInput{PlatformModelName: strings.TrimSpace(model), TaskType: taskType, Scope: channel.RouteScopeUser, UserID: userID})
	if err != nil {
		return nil, mapRouteResolutionError(err)
	}
	return s.quoteImageRoute(ctx, route, task, options, len(fileIDs))
}

func (s *Service) quoteImageRoute(ctx context.Context, route *channel.ResolvedRoute, task string, options map[string]any, references int) (*p.Quote, error) {
	protocol := llm.NormalizeAdapter(route.Protocol)
	if protocol != llm.AdapterOpenAIImageGenerations && protocol != llm.AdapterOpenAIImageEdits {
		return nil, p.ErrInvalid
	}
	parameters, err := domainchannel.ParseImageParameterPolicy(route.ModelCapabilitiesJSON)
	if err != nil || parameters == nil {
		return nil, p.ErrInvalid
	}
	policy, err := s.pointsSvc.RequireActive(ctx)
	if err != nil {
		return nil, err
	}
	if references > min(policy.MaxReferenceImages, parameters.MaxReferenceImages) {
		return nil, ErrMediaImageEditTooManyInputs
	}
	if task == string(MediaImageTaskEdit) && references == 0 {
		return nil, ErrMediaImageEditInputRequired
	}
	if task == string(MediaImageTaskGeneration) && references > 0 {
		return nil, ErrMediaImageGenerationRejectsInputs
	}
	cfg := s.cfg.Snapshot()
	filtered := filterModelOptions(options, route.Protocol, modelOptionPolicyConfig{Mode: cfg.ModelOptionPolicyMode, AllowedPathsJSON: cfg.ModelOptionAllowedPaths, DeniedPathsJSON: cfg.ModelOptionDeniedPaths, ModelCapabilitiesJSON: route.ModelCapabilitiesJSON})
	if filtered == nil {
		filtered = map[string]any{}
	}
	quality, _ := filtered["quality"].(string)
	if quality == "" {
		quality = parameters.Qualities[0]
	}
	size, _ := filtered["size"].(string)
	if size == "" {
		size = parameters.Sizes[0]
	}
	if !slices.Contains(parameters.Qualities, quality) || !slices.Contains(parameters.Sizes, size) {
		return nil, p.ErrInvalid
	}
	// 首版仅开放已经验证的三个参数，不透传额外上游选项。
	count, ok := filtered["n"]
	if !ok {
		count = 1
	}
	filtered = map[string]any{"quality": quality, "size": size, "n": count}
	quote, err := s.pointsSvc.Quote(ctx, route.PlatformModelName, task, filtered, route.ModelCapabilitiesJSON)
	if err != nil {
		return nil, err
	}
	if quote.Count > parameters.MaxCount {
		return nil, p.ErrInvalid
	}
	return quote, nil
}

// PreflightImagePoints 在打开 NDJSON 连接前返回可预检的业务错误，应用用例仍会再次校验。
func (s *Service) PreflightImagePoints(ctx context.Context, userID uint, model, task string, options map[string]any, fileIDs []string, fingerprint string) error {
	if s.pointsSvc == nil {
		return nil
	}
	quote, err := s.QuoteImagePoints(ctx, userID, model, task, options, fileIDs)
	if err != nil {
		return err
	}
	if err = apppoints.ValidateQuote(quote, fingerprint); err != nil {
		return err
	}
	wallet, err := s.pointsSvc.Wallet(ctx, userID, true)
	if err != nil {
		return err
	}
	if wallet.TotalPoints < quote.TotalPoints {
		return p.ErrInsufficient
	}
	return nil
}

// pointSummaries 为任务状态批量附加独立积分信息，仍保留原 Token 与金额字段。
func (s *Service) pointSummaries(ctx context.Context, userID uint, runIDs []string) (map[string]*model.ImagePointSummary, error) {
	result := map[string]*model.ImagePointSummary{}
	if s.pointsSvc == nil {
		return result, nil
	}
	rows, err := s.pointsSvc.ForRuns(ctx, userID, runIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var quote p.Quote
		if err := json.Unmarshal([]byte(row.QuoteJSON), &quote); err != nil {
			return nil, err
		}
		result[row.RunID] = &model.ImagePointSummary{Model: quote.Model, TaskType: quote.TaskType, Options: quote.Options, Spent: row.SpentPoints, Delivered: row.DeliveredCount, Requested: row.RequestedCount, Status: row.Status}
	}
	return result, nil
}
