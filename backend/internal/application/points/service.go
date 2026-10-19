// Package points 编排图片积分，货币余额与此模块始终独立。
package points

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

// Service 的时钟可注入，以确定地测试跨日行为。
type Service struct {
	repo     repository.PointsRepository
	settings repository.SettingsRepository
	clock    func() time.Time
}

func NewService(repo repository.PointsRepository, settings repository.SettingsRepository) *Service {
	return &Service{repo: repo, settings: settings, clock: time.Now}
}

// Policy 读取动态设置；未显式启用时公开图片入口关闭。
func (s *Service) Policy(ctx context.Context) (p.Policy, error) {
	policy := p.Policy{MaxCount: 4, MaxReferenceImages: 4, ConcurrentPerUser: 1, ConcurrentGlobal: 1}
	items, err := s.settings.ListByNamespace(ctx, "points")
	if err != nil {
		return policy, err
	}
	for _, item := range items {
		v := strings.TrimSpace(item.Value)
		if item.Key == "enabled" {
			policy.Enabled = v == "true"
			continue
		}
		n, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil || n < 0 {
			continue
		}
		switch item.Key {
		case "signup_bonus":
			policy.SignupBonus = n
		case "daily_bonus":
			policy.DailyBonus = n
		case "image_max_count":
			policy.MaxCount = int(n)
		case "image_max_reference_images":
			policy.MaxReferenceImages = int(n)
		case "image_concurrent_per_user":
			policy.ConcurrentPerUser = 1 // 首版固定每用户单并发，全站并发独立配置。
		case "image_concurrent_global":
			policy.ConcurrentGlobal = int(n)
		}
	}
	return policy, nil
}

// RequireActive 拒绝双重收费配置，不能通过开启积分同时扣货币余额。
func (s *Service) RequireActive(ctx context.Context) (p.Policy, error) {
	policy, err := s.Policy(ctx)
	if err != nil {
		return policy, err
	}
	if !policy.Enabled {
		return policy, p.ErrDisabled
	}
	items, err := s.settings.ListByNamespace(ctx, "billing")
	if err != nil {
		return policy, err
	}
	for _, item := range items {
		if item.Key == "mode" && item.Value != "self" {
			return policy, p.ErrPolicy
		}
	}
	return policy, nil
}
func (s *Service) Wallet(ctx context.Context, userID uint, grant bool) (*p.Wallet, error) {
	policy, err := s.Policy(ctx)
	if err != nil {
		return nil, err
	}
	if grant && !policy.Enabled {
		return nil, p.ErrDisabled
	}
	return s.repo.Wallet(ctx, userID, policy, s.clock(), grant)
}

// Quote 对规范化参数定价；价格中空尺寸表示明确配置的同质量统一价格。
func (s *Service) Quote(ctx context.Context, model, task string, options map[string]any, capabilities string) (*p.Quote, error) {
	policy, err := s.RequireActive(ctx)
	if err != nil {
		return nil, err
	}
	quality, _ := options["quality"].(string)
	size, _ := options["size"].(string)
	count := 1
	if value, ok := options["n"]; ok {
		switch n := value.(type) {
		case int:
			count = n
		case float64:
			if n != math.Trunc(n) {
				return nil, p.ErrInvalid
			}
			count = int(n)
		default:
			return nil, p.ErrInvalid
		}
	}
	if count < 1 || count > policy.MaxCount {
		return nil, p.ErrInvalid
	}
	price, err := s.repo.FindPrice(ctx, model, task, quality, size)
	if err != nil {
		return nil, err
	}
	if price.PointsPerImage > math.MaxInt64/int64(count) {
		return nil, p.ErrInvalid
	}
	quote := &p.Quote{Model: model, TaskType: task, Options: options, Count: count, Price: *price, TotalPoints: price.PointsPerImage * int64(count)}
	quote.Fingerprint = p.Fingerprint(struct {
		Quote        p.Quote
		Capabilities string
	}{*quote, capabilities})
	return quote, nil
}

func (s *Service) Reserve(ctx context.Context, input p.ReserveInput) (*p.Reservation, error) {
	policy, err := s.RequireActive(ctx)
	if err != nil {
		return nil, err
	}
	input.Policy = policy
	input.Now = s.clock()
	return s.repo.Reserve(ctx, input)
}
func (s *Service) Settle(ctx context.Context, runID string, count int) (*p.Reservation, error) {
	return s.repo.Settle(ctx, runID, count, s.clock())
}

// KeepAlive 维持任务积分租约；调用方必须在任务结束时取消该上下文。
func (s *Service) KeepAlive(ctx context.Context, runID string) error {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.repo.Renew(ctx, runID, s.clock()); err != nil {
				return err
			}
		}
	}
}

// RunReconciliation 随应用生命周期退出，失败向组合根返回供日志记录。
func (s *Service) Reconcile(ctx context.Context) error               { return s.repo.Reconcile(ctx, s.clock()) }
func (s *Service) ListPrices(ctx context.Context) ([]p.Price, error) { return s.repo.ListPrices(ctx) }
func (s *Service) SavePrice(ctx context.Context, price p.Price) (*p.Price, error) {
	price.Model = strings.TrimSpace(price.Model)
	price.Quality = strings.TrimSpace(price.Quality)
	price.Size = strings.TrimSpace(price.Size)
	if price.Model == "" || len(price.Model) > 128 || len(price.Quality) > 32 || len(price.Size) > 32 || price.Quality == "" || price.PointsPerImage < 0 || price.PointsPerImage > 1000000000 || (price.TaskType != "image_generation" && price.TaskType != "image_edit") {
		return nil, p.ErrInvalid
	}
	return s.repo.SavePrice(ctx, price)
}
func (s *Service) Adjust(ctx context.Context, input p.Adjustment) error {
	input.RefNo = strings.TrimSpace(input.RefNo)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.UserID == 0 || input.ActorID == 0 || input.Delta == 0 || len(input.RefNo) < 8 || len(input.RefNo) > 100 || input.Reason == "" || len(input.Reason) > 255 {
		return p.ErrInvalid
	}
	return s.repo.Adjust(ctx, input, s.clock())
}
func (s *Service) Transactions(ctx context.Context, userID uint, offset, limit int) ([]p.Transaction, int64, error) {
	return s.repo.Transactions(ctx, userID, offset, limit)
}
func (s *Service) Reservations(ctx context.Context, userID uint, offset, limit int) ([]p.Reservation, int64, error) {
	return s.repo.Reservations(ctx, userID, offset, limit)
}

// SignupBonus 在创建事务之前固化金额，禁用积分时仍写入零奖励初始化标记。
func (s *Service) SignupBonus(ctx context.Context) (*int64, error) {
	policy, err := s.Policy(ctx)
	if err != nil {
		return nil, err
	}
	bonus := int64(0)
	if policy.Enabled {
		bonus = policy.SignupBonus
	}
	return &bonus, nil
}

// ValidateQuote 将规范化结果与客户端先前看到的报价进行比较。
func ValidateQuote(quote *p.Quote, fingerprint string) error {
	if strings.TrimSpace(fingerprint) == "" {
		return p.ErrQuoteRequired
	}
	if quote == nil || quote.Fingerprint != fingerprint {
		return p.ErrPriceChanged
	}
	return nil
}

// RequestHash 包含会话和分支，禁止复用幂等键执行另一项工作。
func RequestHash(userID, conversationID uint, task, prompt string, fileIDs []string, mask, parent, source, branch string, quote p.Quote) string {
	return p.Fingerprint(struct {
		UserID         uint
		ConversationID uint
		Task           string
		Prompt         string
		Files          []string
		Mask           string
		Parent         string
		Source         string
		Branch         string
		Quote          p.Quote
	}{userID, conversationID, task, prompt, fileIDs, mask, parent, source, branch, quote})
}

// ForRuns 返回持久化结算摘要，不依赖生成进程仍然存活。
func (s *Service) ForRuns(ctx context.Context, userID uint, runIDs []string) ([]p.Reservation, error) {
	return s.repo.ForRuns(ctx, userID, runIDs)
}
