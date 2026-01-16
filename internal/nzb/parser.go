package nzb

import (
	"encoding/xml"
	"io"
	"log"
	"os"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) ParseFile(nzbPath string) (*Model, error) {
	f, err := os.Open(nzbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	return p.Parse(f)

}

func (p *Parser) Parse(r io.Reader) (*Model, error) {
	var nzb Model
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&nzb); err != nil {
		return nil, err
	}

	return &nzb, nil
}
