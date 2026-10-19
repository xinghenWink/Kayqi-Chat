// Package points 的 DTO 只在 HTTP 边界保留 JSON 契约，领域类型不绑定传输协议。
package points

import (
	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"time"
)

type Policy struct {
	Enabled            bool  `json:"enabled"`
	SignupBonus        int64 `json:"signupBonus"`
	DailyBonus         int64 `json:"dailyBonus"`
	MaxCount           int   `json:"maxCount"`
	MaxReferenceImages int   `json:"maxReferenceImages"`
	ConcurrentPerUser  int   `json:"concurrentPerUser"`
	ConcurrentGlobal   int   `json:"concurrentGlobal"`
}

type Wallet struct {
	UserID          uint   `json:"userID"`
	PermanentPoints int64  `json:"permanentPoints"`
	DailyPoints     int64  `json:"dailyPoints"`
	FrozenPoints    int64  `json:"frozenPoints"`
	TotalPoints     int64  `json:"totalPoints"`
	DailyDate       string `json:"dailyDate"`
	Policy          Policy `json:"policy"`
}

type Price struct {
	ID             uint   `json:"id"`
	Model          string `json:"model"`
	TaskType       string `json:"taskType"`
	Quality        string `json:"quality"`
	Size           string `json:"size"`
	PointsPerImage int64  `json:"pointsPerImage"`
	Version        int64  `json:"version"`
	Enabled        bool   `json:"enabled"`
}

type Quote struct {
	Model       string         `json:"model"`
	TaskType    string         `json:"taskType"`
	Options     map[string]any `json:"options"`
	Count       int            `json:"count"`
	Price       Price          `json:"price"`
	TotalPoints int64          `json:"totalPoints"`
	Fingerprint string         `json:"fingerprint"`
}

type Transaction struct {
	ID             uint      `json:"id"`
	UserID         uint      `json:"userID"`
	Type           string    `json:"type"`
	DailyDelta     int64     `json:"dailyDelta"`
	PermanentDelta int64     `json:"permanentDelta"`
	DailyDate      string    `json:"dailyDate"`
	RunID          string    `json:"runID"`
	RefNo          string    `json:"refNo"`
	ActorID        uint      `json:"actorID"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Reservation struct {
	RunID             string     `json:"runID"`
	UserID            uint       `json:"userID"`
	ConversationID    uint       `json:"conversationID"`
	RequestHash       string     `json:"requestHash"`
	ReservedDaily     int64      `json:"reservedDaily"`
	ReservedPermanent int64      `json:"reservedPermanent"`
	DailyDate         string     `json:"dailyDate"`
	QuoteJSON         string     `json:"quoteJSON"`
	RequestedCount    int        `json:"requestedCount"`
	DeliveredCount    int        `json:"deliveredCount"`
	SpentPoints       int64      `json:"spentPoints"`
	Status            string     `json:"status"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	CreatedAt         time.Time  `json:"createdAt"`
	SettledAt         *time.Time `json:"settledAt"`
}

func toWallet(w p.Wallet) Wallet {
	return Wallet{UserID: w.UserID, PermanentPoints: w.PermanentPoints, DailyPoints: w.DailyPoints, FrozenPoints: w.FrozenPoints, TotalPoints: w.TotalPoints, DailyDate: w.DailyDate, Policy: Policy(w.Policy)}
}
func toQuote(q p.Quote) Quote {
	return Quote{Model: q.Model, TaskType: q.TaskType, Options: q.Options, Count: q.Count, Price: Price(q.Price), TotalPoints: q.TotalPoints, Fingerprint: q.Fingerprint}
}
func toPrices(rows []p.Price) []Price {
	result := make([]Price, 0, len(rows))
	for _, row := range rows {
		result = append(result, Price(row))
	}
	return result
}
func toTransactions(rows []p.Transaction) []Transaction {
	result := make([]Transaction, 0, len(rows))
	for _, row := range rows {
		result = append(result, Transaction(row))
	}
	return result
}
func toReservations(rows []p.Reservation) []Reservation {
	result := make([]Reservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, Reservation(row))
	}
	return result
}
