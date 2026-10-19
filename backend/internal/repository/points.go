package repository

import (
	"context"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"time"
)

// PointsRepository 定义钱包与图片授权的持久化事务边界。
type PointsRepository interface {
	ForRuns(context.Context, uint, []string) ([]points.Reservation, error)
	Wallet(context.Context, uint, points.Policy, time.Time, bool) (*points.Wallet, error)
	FindPrice(context.Context, string, string, string, string) (*points.Price, error)
	ListPrices(context.Context) ([]points.Price, error)
	SavePrice(context.Context, points.Price) (*points.Price, error)
	Reserve(context.Context, points.ReserveInput) (*points.Reservation, error)
	Settle(context.Context, string, int, time.Time) (*points.Reservation, error)
	Renew(context.Context, string, time.Time) error
	Reconcile(context.Context, time.Time) error
	Adjust(context.Context, points.Adjustment, time.Time) error
	Transactions(context.Context, uint, int, int) ([]points.Transaction, int64, error)
	Reservations(context.Context, uint, int, int) ([]points.Reservation, int64, error)
}
