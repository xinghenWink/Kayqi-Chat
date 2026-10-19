// Package points 实现 SQLite 与 PostgreSQL 共用的积分事务。
package points

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repo 的写操作均在短事务内执行，不持有连接等待上游。
type Repo struct {
	db       *gorm.DB
	isActive func(context.Context, string) bool
}

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// SetActiveChecker 防止慢任务仅因数据库租约过期而被错误退款。
func (r *Repo) SetActiveChecker(check func(context.Context, string) bool) { r.isActive = check }

// lockWallet 用一次实际写锁兼容 SQLite，不能依赖其不支持的行级 FOR UPDATE。
func lockWallet(tx *gorm.DB, userID uint) (*models.PointWallet, error) {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.PointWallet{UserID: userID, SignupInitialized: true}).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&models.PointWallet{}).Where("user_id = ?", userID).UpdateColumn("version", gorm.Expr("version + 1")).Error; err != nil {
		return nil, err
	}
	var user models.User
	if err := tx.Select("id").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, p.ErrNotFound
		}
		return nil, err
	}
	var wallet models.PointWallet
	return &wallet, tx.First(&wallet, "user_id = ?", userID).Error
}

// InitializeSignupTx 与用户创建共用事务；历史账号按需创建钱包时不会领取注册奖励。
func InitializeSignupTx(tx *gorm.DB, userID uint, bonus int64, now time.Time) error {
	if bonus < 0 {
		return p.ErrInvalid
	}
	row := models.PointWallet{UserID: userID, PermanentAvailable: bonus, SignupInitialized: true}
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if created.Error != nil || created.RowsAffected == 0 {
		return created.Error
	}
	return tx.Create(&models.PointTransaction{UserID: userID, Type: "signup", PermanentDelta: bonus, RefNo: fmt.Sprintf("signup:%d", userID), CreatedAt: now}).Error
}

func grantDaily(tx *gorm.DB, wallet *models.PointWallet, policy p.Policy, now time.Time) error {
	date := p.BusinessDate(now)
	if wallet.DailyDate != date {
		if wallet.DailyAvailable > 0 {
			if err := tx.Create(&models.PointTransaction{UserID: wallet.UserID, Type: "expire", DailyDelta: -wallet.DailyAvailable, DailyDate: wallet.DailyDate, RefNo: fmt.Sprintf("expire:%d:%s", wallet.UserID, wallet.DailyDate), CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		wallet.DailyAvailable = 0
		wallet.DailyDate = date
	}
	grant := models.PointDailyGrant{UserID: wallet.UserID, GrantDate: date, Amount: policy.DailyBonus, CreatedAt: now}
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&grant)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected > 0 {
		wallet.DailyAvailable += policy.DailyBonus
		if err := tx.Create(&models.PointTransaction{UserID: wallet.UserID, Type: "daily_grant", DailyDelta: policy.DailyBonus, DailyDate: date, RefNo: fmt.Sprintf("daily:%d:%s", wallet.UserID, date), CreatedAt: now}).Error; err != nil {
			return err
		}
	}
	return tx.Save(wallet).Error
}

// Wallet 的领取标志只由显式领取及图片授权使用，普通查询不发放当日额度。
func (r *Repo) Wallet(ctx context.Context, userID uint, policy p.Policy, now time.Time, grant bool) (*p.Wallet, error) {
	var result p.Wallet
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		wallet, err := lockWallet(tx, userID)
		if err != nil {
			return err
		}
		if grant {
			if err = grantDaily(tx, wallet, policy, now); err != nil {
				return err
			}
		}
		result = p.Wallet{UserID: userID, PermanentPoints: wallet.PermanentAvailable, DailyDate: p.BusinessDate(now), Policy: policy}
		if wallet.DailyDate == result.DailyDate {
			result.DailyPoints = wallet.DailyAvailable
		}
		if err = tx.Model(&models.PointReservation{}).Where("user_id = ? AND status IN ?", userID, []string{"reserved", "reconcile_pending"}).Select("COALESCE(SUM(reserved_daily + reserved_permanent),0)").Scan(&result.FrozenPoints).Error; err != nil {
			return err
		}
		result.TotalPoints = result.PermanentPoints + result.DailyPoints
		return nil
	})
	return &result, err
}

func priceDomain(row models.ImagePointPrice, model string) p.Price {
	return p.Price{ID: row.ID, Model: model, TaskType: row.TaskType, Quality: row.Quality, Size: row.Size, PointsPerImage: row.PointsPerImage, Version: row.Version, Enabled: row.Enabled}
}

func (r *Repo) FindPrice(ctx context.Context, model, task, quality, size string) (*p.Price, error) {
	var platform models.LLMPlatformModel
	if err := r.db.WithContext(ctx).Where("name = ? AND status = ?", model, "active").First(&platform).Error; err != nil {
		return nil, p.ErrPriceMissing
	}
	var rows []models.ImagePointPrice
	if err := r.db.WithContext(ctx).Where("platform_model_id = ? AND task_type = ? AND quality = ? AND size IN ?", platform.ID, task, quality, []string{size, ""}).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Size == size {
			if !row.Enabled {
				return nil, p.ErrPriceMissing
			}
			v := priceDomain(row, model)
			return &v, nil
		}
	}
	if len(rows) > 0 {
		if !rows[0].Enabled {
			return nil, p.ErrPriceMissing
		}
		v := priceDomain(rows[0], model)
		return &v, nil
	}
	return nil, p.ErrPriceMissing
}

func (r *Repo) ListPrices(ctx context.Context) ([]p.Price, error) {
	var rows []models.ImagePointPrice
	if err := r.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	var platforms []models.LLMPlatformModel
	if err := r.db.WithContext(ctx).Find(&platforms).Error; err != nil {
		return nil, err
	}
	names := map[uint]string{}
	for _, row := range platforms {
		names[row.ID] = row.Name
	}
	results := make([]p.Price, 0, len(rows))
	for _, row := range rows {
		results = append(results, priceDomain(row, names[row.PlatformModelID]))
	}
	return results, nil
}

// SavePrice 使用版本条件防止管理员同时编辑时覆盖价格。
func (r *Repo) SavePrice(ctx context.Context, price p.Price) (*p.Price, error) {
	var result p.Price
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model models.LLMPlatformModel
		if err := tx.Where("name = ?", price.Model).First(&model).Error; err != nil {
			return p.ErrInvalid
		}
		if price.ID == 0 {
			row := models.ImagePointPrice{PlatformModelID: model.ID, TaskType: price.TaskType, Quality: price.Quality, Size: price.Size, PointsPerImage: price.PointsPerImage, Version: 1, Enabled: price.Enabled}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			result = priceDomain(row, model.Name)
			return nil
		}
		update := tx.Model(&models.ImagePointPrice{}).Where("id = ? AND version = ? AND platform_model_id = ?", price.ID, price.Version, model.ID).Updates(map[string]any{"points_per_image": price.PointsPerImage, "enabled": price.Enabled, "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return p.ErrPriceChanged
		}
		var row models.ImagePointPrice
		if err := tx.First(&row, price.ID).Error; err != nil {
			return err
		}
		result = priceDomain(row, model.Name)
		return nil
	})
	return &result, err
}

// Reserve 通过配置行的数据库写锁串行化全站并发名额检查，适用于单进程和多连接。
func (r *Repo) Reserve(ctx context.Context, input p.ReserveInput) (*p.Reservation, error) {
	var result p.Reservation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked := tx.Model(&models.SystemSetting{}).Where("namespace = ? AND key = ?", "points", "image_concurrent_global").UpdateColumn("value", gorm.Expr("value"))
		if locked.Error != nil {
			return locked.Error
		}
		if locked.RowsAffected != 1 {
			return p.ErrPolicy
		}
		var existing models.PointReservation
		if err := tx.First(&existing, "run_id = ?", input.RunID).Error; err == nil {
			if existing.UserID != input.UserID || existing.RequestHash != input.RequestHash {
				return p.ErrConflict
			}
			result = existing.Domain()
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var total, userTotal int64
		active := tx.Model(&models.PointReservation{}).Where("status IN ?", []string{"reserved", "reconcile_pending"})
		if err := active.Count(&total).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.PointReservation{}).Where("status IN ? AND user_id = ?", []string{"reserved", "reconcile_pending"}, input.UserID).Count(&userTotal).Error; err != nil {
			return err
		}
		if total >= int64(input.Policy.ConcurrentGlobal) || userTotal >= int64(input.Policy.ConcurrentPerUser) {
			return p.ErrConcurrent
		}
		// 在预留事务内重读价格，防止报价与扣减之间的价格竞争。
		var price models.ImagePointPrice
		if err := tx.First(&price, input.Quote.Price.ID).Error; err != nil {
			return p.ErrPriceChanged
		}
		if !price.Enabled || price.Version != input.Quote.Price.Version {
			return p.ErrPriceChanged
		}
		wallet, err := lockWallet(tx, input.UserID)
		if err != nil {
			return err
		}
		if err = grantDaily(tx, wallet, input.Policy, input.Now); err != nil {
			return err
		}
		amount := input.Quote.TotalPoints
		if amount < 0 || wallet.PermanentAvailable > math.MaxInt64-wallet.DailyAvailable {
			return p.ErrInvalid
		}
		if wallet.PermanentAvailable+wallet.DailyAvailable < amount {
			return p.ErrInsufficient
		}
		daily := min(amount, wallet.DailyAvailable)
		permanent := amount - daily
		wallet.DailyAvailable -= daily
		wallet.PermanentAvailable -= permanent
		if err = tx.Save(wallet).Error; err != nil {
			return err
		}
		raw, err := json.Marshal(input.Quote)
		if err != nil {
			return err
		}
		row := models.PointReservation{RunID: input.RunID, UserID: input.UserID, ConversationID: input.ConversationID, RequestHash: input.RequestHash, ReservedDaily: daily, ReservedPermanent: permanent, DailyDate: p.BusinessDate(input.Now), QuoteJSON: string(raw), RequestedCount: input.Quote.Count, Status: "reserved", ExpiresAt: input.Now.Add(2 * time.Minute), CreatedAt: input.Now}
		if err = tx.Create(&row).Error; err != nil {
			return err
		}
		if err = tx.Create(&models.PointTransaction{UserID: input.UserID, Type: "reserve", DailyDelta: -daily, PermanentDelta: -permanent, DailyDate: row.DailyDate, RunID: input.RunID, RefNo: "reserve:" + input.RunID, CreatedAt: input.Now}).Error; err != nil {
			return err
		}
		result = row.Domain()
		return nil
	})
	return &result, err
}

// SettleTx 可被会话仓储加入媒体完成事务；已结算记录绝不重复退款。
func SettleTx(tx *gorm.DB, runID string, delivered int, now time.Time) (*p.Reservation, error) {
	// 先锁钱包再读取预留：SQLite 的读事务不能在另一连接写入后升级为写事务。
	// 子查询在同一条 UPDATE 内解析用户，PostgreSQL 也保持钱包→预留的一致锁顺序。
	owner := tx.Model(&models.PointReservation{}).Select("user_id").Where("run_id = ?", runID)
	locked := tx.Model(&models.PointWallet{}).Where("user_id = (?)", owner).UpdateColumn("version", gorm.Expr("version + 1"))
	if locked.Error != nil {
		return nil, locked.Error
	}
	if locked.RowsAffected != 1 {
		return nil, p.ErrNotFound
	}
	var row models.PointReservation
	if err := tx.First(&row, "run_id = ?", runID).Error; err != nil {
		return nil, err
	}
	if row.Status == "settled" || row.Status == "released" {
		v := row.Domain()
		return &v, nil
	}
	wallet, err := lockWallet(tx, row.UserID)
	if err != nil {
		return nil, err
	}
	// 钱包写锁后重读状态，避免两个结算连接都使用锁前读取的预留。
	if err = tx.First(&row, "run_id = ?", runID).Error; err != nil {
		return nil, err
	}
	if row.Status == "settled" || row.Status == "released" {
		v := row.Domain()
		return &v, nil
	}
	var quote p.Quote
	if err = json.Unmarshal([]byte(row.QuoteJSON), &quote); err != nil {
		return nil, err
	}
	delivered = max(0, min(delivered, row.RequestedCount))
	spent := int64(delivered) * quote.Price.PointsPerImage
	dailySpent := min(spent, row.ReservedDaily)
	permanentSpent := spent - dailySpent
	refundDaily := row.ReservedDaily - dailySpent
	expiredRefund := int64(0)
	refundPermanent := row.ReservedPermanent - permanentSpent
	if refundPermanent < 0 || wallet.PermanentAvailable > math.MaxInt64-refundPermanent {
		return nil, p.ErrInvalid
	}
	wallet.PermanentAvailable += refundPermanent
	// 旧日预留只能返还到仍属于同一天的钱包，不能转成新日额度。
	if row.DailyDate == p.BusinessDate(now) && wallet.DailyDate == row.DailyDate {
		wallet.DailyAvailable += refundDaily
	} else {
		expiredRefund = refundDaily
		refundDaily = 0
	}
	if expiredRefund > 0 {
		if err = tx.Create(&models.PointTransaction{UserID: row.UserID, Type: "expire", RunID: runID, DailyDate: row.DailyDate, RefNo: "expire-reserve:" + runID, Reason: fmt.Sprintf("expired reserved daily points: %d", expiredRefund), CreatedAt: now}).Error; err != nil {
			return nil, err
		}
	}
	if err = tx.Save(wallet).Error; err != nil {
		return nil, err
	}
	row.DeliveredCount = delivered
	row.SpentPoints = spent
	row.Status = "settled"
	if delivered == 0 {
		row.Status = "released"
	}
	row.SettledAt = &now
	if err = tx.Save(&row).Error; err != nil {
		return nil, err
	}
	if err = tx.Create(&models.PointTransaction{UserID: row.UserID, Type: row.Status, DailyDelta: refundDaily, PermanentDelta: refundPermanent, DailyDate: row.DailyDate, RunID: runID, RefNo: "settle:" + runID, CreatedAt: now}).Error; err != nil {
		return nil, err
	}
	if err = tx.Model(&models.Message{}).Where("run_id = ? AND role = ?", runID, "assistant").Updates(map[string]any{"points_spent": spent, "points_requested": row.RequestedCount, "points_delivered": delivered, "points_status": row.Status, "points_options_json": row.QuoteJSON}).Error; err != nil {
		return nil, err
	}
	v := row.Domain()
	return &v, nil
}

func (r *Repo) Settle(ctx context.Context, runID string, delivered int, now time.Time) (*p.Reservation, error) {
	var result *p.Reservation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { var err error; result, err = SettleTx(tx, runID, delivered, now); return err })
	return result, err
}
func (r *Repo) Renew(ctx context.Context, runID string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&models.PointReservation{}).Where("run_id = ? AND status = ?", runID, "reserved").UpdateColumn("expires_at", now.Add(2*time.Minute)).Error
}

// Reconcile 只收口已失去租约的运行，不自动向上游重新发起付费生成。
func (r *Repo) Reconcile(ctx context.Context, now time.Time) error {
	var rows []models.PointReservation
	if err := r.db.WithContext(ctx).Where("status IN ? AND expires_at < ?", []string{"reserved", "reconcile_pending"}, now).Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if r.isActive != nil && r.isActive(ctx, row.RunID) {
			if err := r.Renew(ctx, row.RunID, now); err != nil {
				return err
			}
			continue
		}
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, err := lockWallet(tx, row.UserID); err != nil {
				return err
			}
			var current models.PointReservation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "run_id = ?", row.RunID).Error; err != nil {
				return err
			}
			if current.ExpiresAt.After(now) || current.Status == "settled" || current.Status == "released" {
				return nil
			}
			var message models.Message
			err := tx.Where("run_id = ? AND role = ?", row.RunID, "assistant").First(&message).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			delivered := int64(0)
			if err == nil && message.Status == "success" {
				if err = tx.Model(&models.Attachment{}).Where("message_id = ? AND kind = ?", message.ID, "image").Count(&delivered).Error; err != nil {
					return err
				}
			} else if err == nil && message.Status == "pending" {
				if err = tx.Model(&models.Message{}).Where("id = ? AND status = ?", message.ID, "pending").Updates(map[string]any{"status": "interrupted", "error_code": "points.interrupted", "error_message": "image generation interrupted"}).Error; err != nil {
					return err
				}
			}
			if delivered == 0 {
				if err := tx.Model(&models.ConversationRun{}).Where("run_id = ? AND status = ?", row.RunID, "running").Updates(map[string]any{"status": "interrupted", "error_code": "points.interrupted", "error_message": "image generation interrupted", "ended_at": now}).Error; err != nil {
					return err
				}
			}
			_, err = SettleTx(tx, row.RunID, int(delivered), now)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) Adjust(ctx context.Context, input p.Adjustment, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		wallet, err := lockWallet(tx, input.UserID)
		if err != nil {
			return err
		}
		ref := "adjust:" + input.RefNo
		var old models.PointTransaction
		if err = tx.First(&old, "ref_no = ?", ref).Error; err == nil {
			if old.UserID != input.UserID || old.PermanentDelta != input.Delta || old.ActorID != input.ActorID || old.Reason != input.Reason {
				return p.ErrConflict
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if input.Delta < 0 && input.Delta < -wallet.PermanentAvailable {
			return p.ErrInsufficient
		}
		if input.Delta > 0 && wallet.PermanentAvailable > math.MaxInt64-input.Delta {
			return p.ErrInvalid
		}
		wallet.PermanentAvailable += input.Delta
		if err = tx.Save(wallet).Error; err != nil {
			return err
		}
		return tx.Create(&models.PointTransaction{UserID: input.UserID, Type: "adjust", PermanentDelta: input.Delta, ActorID: input.ActorID, Reason: input.Reason, RefNo: ref, CreatedAt: now}).Error
	})
}
func (r *Repo) Transactions(ctx context.Context, userID uint, offset, limit int) ([]p.Transaction, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.PointTransaction{})
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.PointTransaction
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]p.Transaction, 0, len(rows))
	for _, row := range rows {
		result = append(result, p.Transaction(row))
	}
	return result, total, nil
}
func (r *Repo) Reservations(ctx context.Context, userID uint, offset, limit int) ([]p.Reservation, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.PointReservation{})
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.PointReservation
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]p.Reservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Domain())
	}
	return result, total, nil
}

// ForRuns 只返回调用者自己的预留，复用任务查询的授权边界。
func (r *Repo) ForRuns(ctx context.Context, userID uint, runIDs []string) ([]p.Reservation, error) {
	var rows []models.PointReservation
	if len(runIDs) == 0 {
		return nil, nil
	}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND run_id IN ?", userID, runIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]p.Reservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Domain())
	}
	return result, nil
}
