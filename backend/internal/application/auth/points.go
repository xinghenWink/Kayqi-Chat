package auth

import "context"

// SetPointSignupProvider 将注册奖励快照交给既有用户创建事务。
func (s *Service) SetPointSignupProvider(provider interface {
	SignupBonus(context.Context) (*int64, error)
}) {
	s.pointSignup = provider
}
func (s *Service) signupPoints(ctx context.Context) (*int64, error) {
	if s.pointSignup == nil {
		return nil, nil
	}
	return s.pointSignup.SignupBonus(ctx)
}
