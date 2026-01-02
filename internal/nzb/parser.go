package nzb

import (
	"encoding/xml"
	"gonzb/internal/domain"
	"io"
	"log"
	"os"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) ParseFile(nzbPath string) (*domain.NZB, error) {
	f, err := os.Open(nzbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	return p.Parse(f)

}

func (p *Parser) Parse(r io.Reader) (*domain.NZB, error) {
	var nzb domain.NZB
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&nzb); err != nil {
		return nil, err
	}

	return &nzb, nil
}
