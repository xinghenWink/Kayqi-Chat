// Package points 提供积分查询、报价及管理员管理接口。
package points

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	appaudit "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/audit"
	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	apppoints "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/points"
	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/pagination"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// Module 只挂载在服务器的计费能力门禁内，桌面本地模式不开放积分管理。
type Module struct {
	service      *apppoints.Service
	conversation *appconversation.Service
	audit        *appaudit.Service
}

func NewModule(service *apppoints.Service, conversation *appconversation.Service, audit *appaudit.Service) *Module {
	return &Module{service: service, conversation: conversation, audit: audit}
}
func (m *Module) RegisterRoutes(group *gin.RouterGroup, gate middleware.FeatureGate) {
	g := group.Group("/points", gate.Require("billingGating"))
	g.GET("/me", m.Wallet)
	g.POST("/daily-grants/ensure", m.GrantDaily)
	g.GET("/transactions", m.Transactions)
	g.POST("/image-quote", m.Quote)
}
func (m *Module) RegisterAdminRoutes(group *gin.RouterGroup, gate middleware.FeatureGate) {
	g := group.Group("/points", gate.Require("billingGating"))
	g.GET("/prices", m.Prices)
	g.PUT("/prices", m.SavePrice)
	g.GET("/users/:id", m.UserWallet)
	g.POST("/users/:id/adjust", m.Adjust)
	g.GET("/transactions", m.AdminTransactions)
	g.GET("/reservations", m.Reservations)
}

// 报价与钱包使用明确 DTO，Swagger 与所有客户端共享同一份契约。
type QuoteRequest struct {
	Model    string         `json:"model" binding:"required,max=128"`
	TaskType string         `json:"taskType" binding:"required,oneof=image_generation image_edit"`
	Options  map[string]any `json:"options,omitempty"`
	FileIDs  []string       `json:"fileIDs,omitempty" binding:"max=16"`
}
type PriceRequest struct {
	ID             uint   `json:"id,omitempty"`
	Model          string `json:"model" binding:"required,max=128"`
	TaskType       string `json:"taskType" binding:"required,oneof=image_generation image_edit"`
	Quality        string `json:"quality" binding:"required,max=32"`
	Size           string `json:"size" binding:"max=32"`
	PointsPerImage *int64 `json:"pointsPerImage" binding:"required,gte=0,lte=1000000000"`
	Version        int64  `json:"version"`
	Enabled        *bool  `json:"enabled" binding:"required"`
}
type AdjustmentRequest struct {
	Delta  int64  `json:"delta" binding:"required"`
	RefNo  string `json:"refNo" binding:"required,min=8,max=100"`
	Reason string `json:"reason" binding:"required,max=255"`
}
type WalletResponseDoc struct {
	ErrorMsg string `json:"errorMsg"`
	Data     Wallet `json:"data"`
}
type QuoteResponseDoc struct {
	ErrorMsg string `json:"errorMsg"`
	Data     Quote  `json:"data"`
}
type PriceResponseDoc struct {
	ErrorMsg string `json:"errorMsg"`
	Data     Price  `json:"data"`
}
type PriceListResponseDoc struct {
	ErrorMsg string  `json:"errorMsg"`
	Data     []Price `json:"data"`
}
type TransactionPage struct {
	Total   int64         `json:"total"`
	Results []Transaction `json:"results"`
}
type TransactionResponseDoc struct {
	ErrorMsg string          `json:"errorMsg"`
	Data     TransactionPage `json:"data"`
}
type ReservationPage struct {
	Total   int64         `json:"total"`
	Results []Reservation `json:"results"`
}
type ReservationResponseDoc struct {
	ErrorMsg string          `json:"errorMsg"`
	Data     ReservationPage `json:"data"`
}

// Status 为普通 HTTP 和既有 NDJSON 错误提供一致状态语义。
func Status(err error) int {
	switch {
	case errors.Is(err, p.ErrDisabled):
		return http.StatusServiceUnavailable
	case errors.Is(err, p.ErrInsufficient):
		return http.StatusPaymentRequired
	case errors.Is(err, p.ErrConcurrent):
		return http.StatusTooManyRequests
	case errors.Is(err, p.ErrConflict), errors.Is(err, p.ErrPriceChanged):
		return http.StatusConflict
	case errors.Is(err, p.ErrPriceMissing), errors.Is(err, p.ErrQuoteRequired), errors.Is(err, p.ErrInvalid), errors.Is(err, p.ErrPolicy):
		return http.StatusBadRequest
	case errors.Is(err, p.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
func writeError(c *gin.Context, err error) {
	if Status(err) == 500 {
		response.InternalError(c, err)
	} else {
		response.ErrorFrom(c, Status(err), err)
	}
}
func page(c *gin.Context) (int, int) {
	p, size := pagination.Parse(c.Query("page"), c.Query("page_size"))
	size = min(size, 100)
	return (p - 1) * size, size
}
func target(c *gin.Context) (uint, bool) {
	n, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || n == 0 {
		response.InvalidQueryParam(c, "id")
		return 0, false
	}
	return uint(n), true
}
func filterUser(c *gin.Context) (uint, bool) {
	v := c.Query("user_id")
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil || n == 0 {
		response.InvalidQueryParam(c, "user_id")
		return 0, false
	}
	return uint(n), true
}

// Wallet godoc
// @Summary 查询当前积分钱包
// @Tags points
// @Produce json
// @Security BearerAuth
// @Success 200 {object} WalletResponseDoc
// @Router /points/me [get]
func (m *Module) Wallet(c *gin.Context) {
	w, err := m.service.Wallet(c.Request.Context(), middleware.MustUserID(c), false)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toWallet(*w))
}

// GrantDaily godoc
// @Summary 幂等领取上海时区当日积分
// @Tags points
// @Produce json
// @Security BearerAuth
// @Success 200 {object} WalletResponseDoc
// @Router /points/daily-grants/ensure [post]
func (m *Module) GrantDaily(c *gin.Context) {
	w, err := m.service.Wallet(c.Request.Context(), middleware.MustUserID(c), true)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toWallet(*w))
}

// Quote godoc
// @Summary 根据后端有效参数报价
// @Tags points
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body QuoteRequest true "图片配置"
// @Success 200 {object} QuoteResponseDoc
// @Router /points/image-quote [post]
func (m *Module) Quote(c *gin.Context) {
	var req QuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	q, err := m.conversation.QuoteImagePoints(c.Request.Context(), middleware.MustUserID(c), req.Model, req.TaskType, req.Options, req.FileIDs)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toQuote(*q))
}

// Transactions godoc
// @Summary 查询当前用户积分流水
// @Tags points
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} TransactionResponseDoc
// @Router /points/transactions [get]
func (m *Module) Transactions(c *gin.Context) { m.transactions(c, middleware.MustUserID(c)) }
func (m *Module) transactions(c *gin.Context, userID uint) {
	offset, limit := page(c)
	rows, total, err := m.service.Transactions(c.Request.Context(), userID, offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	response.SuccessPage(c, total, toTransactions(rows))
}

// Prices godoc
// @Summary 查询图片积分价格
// @Tags admin-points
// @Produce json
// @Security BearerAuth
// @Success 200 {object} PriceListResponseDoc
// @Router /admin/points/prices [get]
func (m *Module) Prices(c *gin.Context) {
	rows, err := m.service.ListPrices(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toPrices(rows))
}

// SavePrice godoc
// @Summary 创建或按版本更新图片价格
// @Tags admin-points
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body PriceRequest true "图片价格"
// @Success 200 {object} PriceResponseDoc
// @Router /admin/points/prices [put]
func (m *Module) SavePrice(c *gin.Context) {
	var req PriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	price, err := m.service.SavePrice(c.Request.Context(), p.Price{ID: req.ID, Model: req.Model, TaskType: req.TaskType, Quality: req.Quality, Size: req.Size, PointsPerImage: *req.PointsPerImage, Version: req.Version, Enabled: *req.Enabled})
	if err != nil {
		writeError(c, err)
		return
	}
	m.recordAudit(c, "points.price.update", req.Model, price)
	response.Success(c, Price(*price))
}

// UserWallet godoc
// @Summary 管理员查询用户钱包
// @Tags admin-points
// @Produce json
// @Security BearerAuth
// @Param id path int true "用户ID"
// @Success 200 {object} WalletResponseDoc
// @Router /admin/points/users/{id} [get]
func (m *Module) UserWallet(c *gin.Context) {
	userID, ok := target(c)
	if !ok {
		return
	}
	w, err := m.service.Wallet(c.Request.Context(), userID, false)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toWallet(*w))
}

// Adjust godoc
// @Summary 幂等调整用户永久积分并记录原因
// @Tags admin-points
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "用户ID"
// @Param body body AdjustmentRequest true "积分增减"
// @Success 200 {object} WalletResponseDoc
// @Router /admin/points/users/{id}/adjust [post]
func (m *Module) Adjust(c *gin.Context) {
	userID, ok := target(c)
	if !ok {
		return
	}
	var req AdjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	input := p.Adjustment{UserID: userID, Delta: req.Delta, RefNo: req.RefNo, Reason: strings.TrimSpace(req.Reason), ActorID: middleware.MustUserID(c)}
	if err := m.service.Adjust(c.Request.Context(), input); err != nil {
		writeError(c, err)
		return
	}
	m.recordAudit(c, "points.wallet.adjust", strconv.FormatUint(uint64(userID), 10), input)
	m.UserWallet(c)
}

// AdminTransactions godoc
// @Summary 管理员查询积分流水
// @Tags admin-points
// @Produce json
// @Security BearerAuth
// @Param user_id query int false "用户ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} TransactionResponseDoc
// @Router /admin/points/transactions [get]
func (m *Module) AdminTransactions(c *gin.Context) {
	userID, ok := filterUser(c)
	if ok {
		m.transactions(c, userID)
	}
}

// Reservations godoc
// @Summary 管理员查询图片任务预留及结算
// @Tags admin-points
// @Produce json
// @Security BearerAuth
// @Param user_id query int false "用户ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} ReservationResponseDoc
// @Router /admin/points/reservations [get]
func (m *Module) Reservations(c *gin.Context) {
	userID, ok := filterUser(c)
	if !ok {
		return
	}
	offset, limit := page(c)
	rows, total, err := m.service.Reservations(c.Request.Context(), userID, offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	response.SuccessPage(c, total, toReservations(rows))
}
func (m *Module) recordAudit(c *gin.Context, action, resourceID string, detail any) {
	if m.audit != nil {
		m.audit.Write(c.Request.Context(), appaudit.WriteInput{ActorUserID: middleware.MustUserID(c), RequestID: middleware.MustRequestID(c), Action: action, Resource: "points", ResourceID: resourceID, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), Detail: detail})
	}
}
