package conversation

import "context"

// CleanupUndeliveredImageFiles 与积分对账一起执行，回收中断任务的文件和存储配额。
func (s *Service) CleanupUndeliveredImageFiles(ctx context.Context) error {
	files, err := s.repo.ListUndeliveredGeneratedFiles(ctx)
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, _, err = s.uploadSvc.DeleteFileIfUnreferenced(ctx, file.UserID, file.FileID); err != nil {
			return err
		}
	}
	return nil
}
