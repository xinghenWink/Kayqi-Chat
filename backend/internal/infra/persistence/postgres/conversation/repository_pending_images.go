package conversation

import (
	"context"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
)

// ListUndeliveredGeneratedFiles 不按固定文件年龄删除，仍有有效预留的慢任务保持暂存。
func (r *Repo) ListUndeliveredGeneratedFiles(ctx context.Context) ([]domainconversation.FileObject, error) {
	var rows []models.FileObject
	err := r.db.WithContext(ctx).Where("purpose = ? AND status = ?", "generated_image_pending", "active").
		Where("generation_run_id IN (?)", r.db.Model(&models.PointReservation{}).Select("run_id").Where("status IN ?", []string{"released", "settled"})).Limit(100).Find(&rows).Error
	return toFileObjectDomains(rows), err
}
