package points

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/points"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 使用文件数据库与多个连接，覆盖真实 SQLite 写锁而非纯内存替身。
func pointFixture(t *testing.T) (*Repo, *gorm.DB, p.Policy, time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "points.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.User{}, &models.LLMPlatformModel{}, &models.SystemSetting{}, &models.PointWallet{}, &models.PointTransaction{}, &models.PointDailyGrant{}, &models.PointReservation{}, &models.ImagePointPrice{}, &models.Message{}, &models.Attachment{}, &models.ConversationRun{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for i := uint(1); i <= 2; i++ {
		if err = db.Create(&models.User{BaseModel: models.BaseModel{ID: i}, PublicID: fmt.Sprintf("u%d", i), Username: fmt.Sprintf("user%d", i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Create(&models.SystemSetting{Namespace: "points", Key: "image_concurrent_global", Value: "8", ValueType: "int"}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&models.LLMPlatformModel{ControlPlaneModel: models.ControlPlaneModel{ID: 1}, Name: "test-image", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	policy := p.Policy{Enabled: true, DailyBonus: 8, MaxCount: 4, MaxReferenceImages: 4, ConcurrentPerUser: 8, ConcurrentGlobal: 8}
	return NewRepo(db), db, policy, time.Date(2026, 10, 20, 15, 59, 0, 0, time.UTC)
}
func fixtureQuote(t *testing.T, r *Repo, count int) *p.Quote {
	t.Helper()
	price, err := r.SavePrice(t.Context(), p.Price{Model: "test-image", TaskType: "image_generation", Quality: "high", PointsPerImage: 10, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return &p.Quote{Model: "test-image", TaskType: "image_generation", Options: map[string]any{"quality": "high", "size": "1024x1024", "n": count}, Price: *price, Count: count, TotalPoints: 10 * int64(count)}
}
func addPoints(t *testing.T, r *Repo, user uint, amount int64, now time.Time) {
	t.Helper()
	if err := r.Adjust(t.Context(), p.Adjustment{UserID: user, ActorID: 1, Delta: amount, RefNo: fmt.Sprintf("fixture-%d", user), Reason: "测试初始化"}, now); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitZeroPriceAndDisabledSizeCannotFallBack(t *testing.T) {
	r, _, _, _ := pointFixture(t)
	if _, err := r.FindPrice(t.Context(), "test-image", "image_generation", "low", "1024x1024"); !errors.Is(err, p.ErrPriceMissing) {
		t.Fatalf("unpriced model accepted: %v", err)
	}
	if _, err := r.SavePrice(t.Context(), p.Price{Model: "test-image", TaskType: "image_generation", Quality: "low", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	price, err := r.FindPrice(t.Context(), "test-image", "image_generation", "low", "1024x1024")
	if err != nil || price.PointsPerImage != 0 {
		t.Fatalf("explicit free price: %+v %v", price, err)
	}
	if _, err = r.SavePrice(t.Context(), p.Price{Model: "test-image", TaskType: "image_generation", Quality: "low", Size: "1024x1024", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.FindPrice(t.Context(), "test-image", "image_generation", "low", "1024x1024"); !errors.Is(err, p.ErrPriceMissing) {
		t.Fatalf("disabled size fell back: %v", err)
	}
}

func TestReconciliationPreservesLiveSlowGeneration(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	addPoints(t, r, 1, 20, now)
	quote := fixtureQuote(t, r, 1)
	if _, err := r.Reserve(t.Context(), p.ReserveInput{RunID: "run_live", UserID: 1, ConversationID: 1, RequestHash: "live", Quote: *quote, Policy: policy, Now: now}); err != nil {
		t.Fatal(err)
	}
	r.SetActiveChecker(func(context.Context, string) bool { return true })
	if err := r.Reconcile(t.Context(), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	rows, err := r.ForRuns(t.Context(), 1, []string{"run_live"})
	if err != nil || len(rows) != 1 || rows[0].Status != "reserved" || !rows[0].ExpiresAt.After(now.Add(3*time.Minute)) {
		t.Fatalf("live lease lost: %+v %v", rows, err)
	}
}

func TestConcurrentSettlementRefundsExactlyOnce(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	addPoints(t, r, 1, 20, now)
	quote := fixtureQuote(t, r, 2)
	if _, err := r.Reserve(t.Context(), p.ReserveInput{RunID: "run_settle_parallel", UserID: 1, ConversationID: 1, RequestHash: "settle-parallel", Quote: *quote, Policy: policy, Now: now}); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := r.Settle(t.Context(), "run_settle_parallel", 1, now)
			failures <- err
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	wallet, err := r.Wallet(t.Context(), 1, policy, now, false)
	if err != nil || wallet.TotalPoints != 18 || wallet.FrozenPoints != 0 {
		t.Fatalf("duplicate settlement: %+v %v", wallet, err)
	}
}

func TestSignupZeroIsInitializedAndTransactionRollsBack(t *testing.T) {
	_, db, _, now := pointFixture(t)
	if err := db.Transaction(func(tx *gorm.DB) error { return InitializeSignupTx(tx, 1, 0, now) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return InitializeSignupTx(tx, 1, 10, now) }); err != nil {
		t.Fatal(err)
	}
	var wallet models.PointWallet
	if err := db.First(&wallet, "user_id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	if wallet.PermanentAvailable != 0 || !wallet.SignupInitialized {
		t.Fatalf("zero reward was granted again: %+v", wallet)
	}
	rollback := errors.New("rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := InitializeSignupTx(tx, 2, 10, now); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.PointWallet{}).Where("user_id = ?", 2).Count(&count)
	if count != 0 {
		t.Fatal("wallet survived user transaction rollback")
	}
}

func TestDailyGrantConcurrentAndExpiresWithoutBackfill(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	var group sync.WaitGroup
	failures := make(chan error, 12)
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := r.Wallet(context.Background(), 1, policy, now, true)
			if err != nil {
				failures <- err
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	w, err := r.Wallet(t.Context(), 1, policy, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.DailyPoints != 8 {
		t.Fatalf("duplicate daily grant: %+v", w)
	}
	policy.DailyBonus = 20
	w, err = r.Wallet(t.Context(), 1, policy, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if w.DailyPoints != 8 {
		t.Fatal("policy edit regranted same day")
	}
	w, err = r.Wallet(t.Context(), 1, policy, now.Add(48*time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	if w.DailyPoints != 20 {
		t.Fatalf("missed days were backfilled: %+v", w)
	}
}

func TestCrossMidnightPartialSettlementAndIdempotentRefund(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	addPoints(t, r, 1, 12, now)
	quote := fixtureQuote(t, r, 2)
	_, err := r.Reserve(t.Context(), p.ReserveInput{RunID: "run-cross-day", UserID: 1, RequestHash: "same", Quote: *quote, Policy: policy, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	next := now.Add(2 * time.Minute)
	if _, err = r.Wallet(t.Context(), 1, policy, next, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		settled, e := r.Settle(t.Context(), "run-cross-day", 1, next)
		if e != nil {
			t.Fatal(e)
		}
		if settled.SpentPoints != 10 || settled.DeliveredCount != 1 {
			t.Fatalf("wrong settlement: %+v", settled)
		}
	}
	w, err := r.Wallet(t.Context(), 1, policy, next, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.PermanentPoints != 10 || w.DailyPoints != 8 || w.FrozenPoints != 0 {
		t.Fatalf("old daily refund leaked into today or double refund: %+v", w)
	}
}

func TestConcurrentReservationCannotSpendSameBalance(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	policy.DailyBonus = 0
	addPoints(t, r, 1, 10, now)
	quote := fixtureQuote(t, r, 1)
	var group sync.WaitGroup
	var successes atomic.Int32
	failures := make(chan error, 8)
	for i := range 8 {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			_, err := r.Reserve(context.Background(), p.ReserveInput{RunID: fmt.Sprintf("parallel-%d", i), UserID: 1, RequestHash: fmt.Sprint(i), Quote: *quote, Policy: policy, Now: now})
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, p.ErrInsufficient) {
				failures <- err
			}
		}(i)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if successes.Load() != 1 {
		t.Fatalf("got %d successful spends of the same balance", successes.Load())
	}
}

func TestReservationFingerprintPriceAndConcurrency(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	addPoints(t, r, 1, 100, now)
	quote := fixtureQuote(t, r, 1)
	input := p.ReserveInput{RunID: "same-run", UserID: 1, RequestHash: "hash", Quote: *quote, Policy: policy, Now: now}
	if _, err := r.Reserve(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reserve(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.RequestHash = "changed"
	if _, err := r.Reserve(t.Context(), input); !errors.Is(err, p.ErrConflict) {
		t.Fatalf("changed request reused reservation: %v", err)
	}
	input.RequestHash = "hash"
	input.UserID = 2
	if _, err := r.Reserve(t.Context(), input); !errors.Is(err, p.ErrConflict) {
		t.Fatalf("other user reused reservation: %v", err)
	}
	price := quote.Price
	price.PointsPerImage = 20
	if _, err := r.SavePrice(t.Context(), price); err != nil {
		t.Fatal(err)
	}
	input.UserID = 1
	input.RunID = "changed-price"
	if _, err := r.Reserve(t.Context(), input); !errors.Is(err, p.ErrPriceChanged) {
		t.Fatalf("stale price accepted: %v", err)
	}
	if _, err := r.Settle(t.Context(), "same-run", 0, now); err != nil {
		t.Fatal(err)
	}
	w, err := r.Wallet(t.Context(), 1, policy, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.PermanentPoints != 100 || w.DailyPoints != 8 {
		t.Fatalf("failed reservation was not refunded: %+v", w)
	}
}

func TestGlobalSlotsAndExpiredReservationRecovery(t *testing.T) {
	r, db, policy, now := pointFixture(t)
	policy.ConcurrentGlobal = 1
	addPoints(t, r, 1, 10, now)
	addPoints(t, r, 2, 10, now)
	quote := fixtureQuote(t, r, 1)
	input := p.ReserveInput{RunID: "orphan", UserID: 1, RequestHash: "hash", Quote: *quote, Policy: policy, Now: now}
	if _, err := r.Reserve(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.RunID = "other-user"
	input.UserID = 2
	if _, err := r.Reserve(t.Context(), input); !errors.Is(err, p.ErrConcurrent) {
		t.Fatalf("global limit bypassed: %v", err)
	}
	if err := r.Reconcile(t.Context(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var row models.PointReservation
	db.First(&row, "run_id = ?", "orphan")
	if row.Status != "reserved" {
		t.Fatal("live lease refunded")
	}
	if err := r.Reconcile(t.Context(), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	w, err := r.Wallet(t.Context(), 1, policy, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.FrozenPoints != 0 || w.TotalPoints != 10 {
		t.Fatalf("orphan reservation not recovered: %+v", w)
	}
}

func TestAdjustmentCannotOverwriteFrozenFundsOrDuplicate(t *testing.T) {
	r, _, policy, now := pointFixture(t)
	addPoints(t, r, 1, 10, now)
	input := p.Adjustment{UserID: 1, ActorID: 1, Delta: 5, RefNo: "adjust-retry", Reason: "补偿"}
	for range 2 {
		if err := r.Adjust(t.Context(), input, now); err != nil {
			t.Fatal(err)
		}
	}
	input.Delta = 6
	if err := r.Adjust(t.Context(), input, now); !errors.Is(err, p.ErrConflict) {
		t.Fatalf("changed adjustment accepted: %v", err)
	}
	input.RefNo = "subtract-too-much"
	input.Delta = -16
	if err := r.Adjust(t.Context(), input, now); !errors.Is(err, p.ErrInsufficient) {
		t.Fatal(err)
	}
	w, err := r.Wallet(t.Context(), 1, policy, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.PermanentPoints != 15 {
		t.Fatalf("adjustment repeated: %+v", w)
	}
}
