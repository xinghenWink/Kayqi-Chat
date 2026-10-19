package models

import (
	domainpoints "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"time"
)

// PointWallet 不使用软删除，确保历史账号不会因重新初始化重复领取奖励。
type PointWallet struct {
	UserID             uint   `gorm:"primaryKey;autoIncrement:false"`
	PermanentAvailable int64  `gorm:"not null;default:0;check:point_permanent_nonnegative,permanent_available >= 0"`
	DailyAvailable     int64  `gorm:"not null;default:0;check:point_daily_nonnegative,daily_available >= 0"`
	DailyDate          string `gorm:"size:10;not null;default:''"`
	SignupInitialized  bool   `gorm:"not null;default:false"`
	Version            int64  `gorm:"not null;default:0"`
}

func (PointWallet) TableName() string { return "billing_point_wallets" }

// PointTransaction 的唯一引用号为发放、预留和调整提供幂等边界。
type PointTransaction struct {
	ID             uint   `gorm:"primaryKey"`
	UserID         uint   `gorm:"not null;index:idx_point_transaction_user_created,priority:1"`
	Type           string `gorm:"size:32;not null"`
	DailyDelta     int64  `gorm:"not null"`
	PermanentDelta int64  `gorm:"not null"`
	DailyDate      string `gorm:"size:10;not null"`
	RunID          string `gorm:"size:64;index"`
	RefNo          string `gorm:"size:160;not null;uniqueIndex"`
	ActorID        uint
	Reason         string    `gorm:"size:255"`
	CreatedAt      time.Time `gorm:"index:idx_point_transaction_user_created,priority:2"`
}

func (PointTransaction) TableName() string { return "billing_point_transactions" }

// PointDailyGrant 即使金额为零也记录日期，调价不补发已经领取的日期。
type PointDailyGrant struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    uint   `gorm:"not null;uniqueIndex:idx_point_daily_user_date,priority:1"`
	GrantDate string `gorm:"size:10;not null;uniqueIndex:idx_point_daily_user_date,priority:2"`
	Amount    int64  `gorm:"not null"`
	CreatedAt time.Time
}

func (PointDailyGrant) TableName() string { return "billing_point_daily_grants" }

// PointReservation 显式声明所有列，结算需与媒体完成事务共享此记录。
type PointReservation struct {
	RunID             string    `gorm:"primaryKey;size:64"`
	UserID            uint      `gorm:"not null;index"`
	ConversationID    uint      `gorm:"not null"`
	RequestHash       string    `gorm:"size:64;not null"`
	ReservedDaily     int64     `gorm:"not null"`
	ReservedPermanent int64     `gorm:"not null"`
	DailyDate         string    `gorm:"size:10;not null"`
	QuoteJSON         string    `gorm:"type:text;not null"`
	RequestedCount    int       `gorm:"not null"`
	DeliveredCount    int       `gorm:"not null;default:0"`
	SpentPoints       int64     `gorm:"not null;default:0"`
	Status            string    `gorm:"size:32;not null;index"`
	ExpiresAt         time.Time `gorm:"index"`
	CreatedAt         time.Time
	SettledAt         *time.Time
}

func (PointReservation) TableName() string { return "billing_point_reservations" }

// Domain 返回与持久化解耦的预留记录。
func (r PointReservation) Domain() domainpoints.Reservation {
	return domainpoints.Reservation(r)
}

// ImagePointPrice 使用平台模型 ID 防止上游别名或路由变化引发错价。
type ImagePointPrice struct {
	ID              uint   `gorm:"primaryKey"`
	PlatformModelID uint   `gorm:"not null;uniqueIndex:idx_image_point_price,priority:1"`
	TaskType        string `gorm:"size:32;not null;uniqueIndex:idx_image_point_price,priority:2"`
	Quality         string `gorm:"size:32;not null;uniqueIndex:idx_image_point_price,priority:3"`
	Size            string `gorm:"size:32;not null;default:'';uniqueIndex:idx_image_point_price,priority:4"`
	PointsPerImage  int64  `gorm:"not null;check:image_point_price_nonnegative,points_per_image >= 0"`
	Version         int64  `gorm:"not null;default:1"`
	Enabled         bool   `gorm:"not null;default:false"`
}

func (ImagePointPrice) TableName() string { return "billing_image_point_prices" }
