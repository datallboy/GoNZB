package downloader

import (
	"gonzb/internal/domain"
	"gonzb/internal/nzb"
	"io"
	"log"
)

type Service struct {
	repo   domain.ArticleRespository
	parser *nzb.Parser // may want an interface here too for strict DI
}

func NewService(r domain.ArticleRespository, p *nzb.Parser) *Service {
	return &Service{repo: r, parser: p}
}

func (s *Service) DownloadNZB(nzbReader io.Reader) error {
	nzbData, err := s.parser.Parse(nzbReader)
	if err != nil {
		return err
	}

	for _, file := range nzbData.Files {
		log.Printf("Starting download of file: %s", file.Subject)
		for _, segment := range file.Segments {
			// This is where we'll eventually spawn goroutines
			err := s.downloadSegment(segment.MessageID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) downloadSegment(msgID string) error {
	reader, err := s.repo.FetchBody(msgID)
	if err != nil {
		return err
	}

	// TODO - Pass this reader to the yEnc decoder
	_ = reader
	return nil
}
