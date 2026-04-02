package par2

import "context"

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type Service struct {
	log logger
}

func NewService(log logger) *Service {
	return &Service{log: log}
}

func (s *Service) RunOnce(context.Context) error {
	if s != nil && s.log != nil {
		s.log.Debug("inspect_par2: no inspection candidates available yet")
	}
	return nil
}
