// Package points 定义与原货币余额隔离的图片积分规则。
package points

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/apperr"
)

var (
	ErrNotFound      = apperr.New("points.not_found", "point account not found")
	ErrDisabled      = apperr.New("points.disabled", "image generation is unavailable")
	ErrInsufficient  = apperr.New("points.insufficient", "insufficient image points")
	ErrPriceMissing  = apperr.New("points.price_missing", "image point price is not configured")
	ErrPriceChanged  = apperr.New("points.price_changed", "image price or parameters changed; request a new quote")
	ErrQuoteRequired = apperr.New("points.quote_required", "an image point quote is required")
	ErrConflict      = apperr.New("points.request_conflict", "image request conflicts with an existing run")
	ErrConcurrent    = apperr.New("points.concurrent_limit", "image generation concurrency limit reached")
	ErrInvalid       = apperr.New("points.invalid_request", "invalid image point request")
	ErrPolicy        = apperr.New("points.policy_invalid", "image points require self billing mode")
)

// Policy 是一次业务操作读取到的积分配置快照。
type Policy struct {
	Enabled            bool
	SignupBonus        int64
	DailyBonus         int64
	MaxCount           int
	MaxReferenceImages int
	ConcurrentPerUser  int
	ConcurrentGlobal   int
}

// Wallet 只暴露当前可用余额，历史每日额度不会算入今天。
type Wallet struct {
	UserID          uint
	PermanentPoints int64
	DailyPoints     int64
	FrozenPoints    int64
	TotalPoints     int64
	DailyDate       string
	Policy          Policy
}

// Price 为显式配置的模型价格，不存在价格时禁止调用。
type Price struct {
	ID             uint
	Model          string
	TaskType       string
	Quality        string
	Size           string
	PointsPerImage int64
	Version        int64
	Enabled        bool
}

// Quote 固化后端规范化后的参数与价格，不能使用浏览器传入的单价。
type Quote struct {
	Model       string
	TaskType    string
	Options     map[string]any
	Count       int
	Price       Price
	TotalPoints int64
	Fingerprint string
}

// Transaction 记录可用余额变动，预留不会在结算时重复扣除。
type Transaction struct {
	ID             uint
	UserID         uint
	Type           string
	DailyDelta     int64
	PermanentDelta int64
	DailyDate      string
	RunID          string
	RefNo          string
	ActorID        uint
	Reason         string
	CreatedAt      time.Time
}

// Reservation 同时保存来源日期、价格和交付快照，支持跨日释放及恢复。
type Reservation struct {
	RunID             string
	UserID            uint
	ConversationID    uint
	RequestHash       string
	ReservedDaily     int64
	ReservedPermanent int64
	DailyDate         string
	QuoteJSON         string
	RequestedCount    int
	DeliveredCount    int
	SpentPoints       int64
	Status            string
	ExpiresAt         time.Time
	CreatedAt         time.Time
	SettledAt         *time.Time
}

// ReserveInput 只由服务端构建，不直接绑定 HTTP 请求。
type ReserveInput struct {
	RunID          string
	UserID         uint
	ConversationID uint
	RequestHash    string
	Quote          Quote
	Policy         Policy
	Now            time.Time
}

// Adjustment 只允许调整永久可用积分，不覆盖冻结额度。
type Adjustment struct {
	UserID  uint
	Delta   int64
	RefNo   string
	Reason  string
	ActorID uint
}

// Fingerprint 利用稳定 JSON 键排序生成报价与请求摘要。
func Fingerprint(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// BusinessDate 固定使用上海时区，不受浏览器或服务器本地时区影响。
func BusinessDate(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}
