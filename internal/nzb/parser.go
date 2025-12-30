package nzb

import (
	"encoding/xml"
	"gonzb/internal/domain"
	"io"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(r io.Reader) (*domain.NZB, error) {
	var nzb domain.NZB
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&nzb); err != nil {
		return nil, err
	}

	return &nzb, nil
}
