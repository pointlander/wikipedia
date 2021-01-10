// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"compress/bzip2"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/analysis"
	"github.com/blevesearch/bleve/analysis/char/html"
	"github.com/blevesearch/bleve/registry"
)

// Name is the name of the html filter
const Name = "custom_html"

// AnalyzerConstructor html filter
func AnalyzerConstructor(config map[string]interface{}, cache *registry.Cache) (*analysis.Analyzer, error) {
	htmlCharFilter, err := cache.CharFilterNamed(html.Name)
	if err != nil {
		return nil, err
	}
	rv := analysis.Analyzer{
		CharFilters: []analysis.CharFilter{
			htmlCharFilter,
		},
	}
	return &rv, nil
}

var (
	// Convert to wiki to html
	ConvertFlag = flag.Bool("convert", false, "convert to wiki to html")
	// Container is the container id
	Container = flag.String("id", "", "the container id")
	// WikiRegex is a regex for wiki syntax
	WikiRegex = regexp.MustCompile("[^A-Za-z.!?,;]+")
)

// Page is a wikitext page
type Page struct {
	Title string `xml:"title"`
	Text  string `xml:"revision>text"`
}

// 8526625 63.518098555505276
func main() {
	flag.Parse()

	if *ConvertFlag {
		registry.RegisterAnalyzer(Name, AnalyzerConstructor)
	}

	mapping := bleve.NewIndexMapping()
	index, err := bleve.New("wiki.bleve", mapping)
	if err != nil {
		panic(err)
	}

	input, err := os.Open("enwiki-latest-pages-articles-multistream.xml.bz2")
	if err != nil {
		panic(err)
	}
	defer input.Close()
	reader := bzip2.NewReader(input)
	decoder := xml.NewDecoder(reader)
	token, err := decoder.Token()
	done := make(chan Page, 8)
	convert := func(doc Page) {
		text, err := Convert(doc.Text)
		if err != nil {
			panic(err)
		}
		doc.Text = string(text)
		done <- doc
	}
	wordIndex := make(map[string][]int)
	count := 0
LOOP:
	for err == nil {
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "page" {
				var page Page
				decoder.DecodeElement(&page, &element)
				if *ConvertFlag {
					if count > 64 {
						text := <-done
						fmt.Println("---------------------")
						fmt.Println(text.Text)
						index.Index(text.Title, text.Text)
						go convert(page)
					} else {
						go convert(page)
					}
				}
				text := WikiRegex.ReplaceAllLiteralString(page.Text, " ")
				parts := strings.Split(text, " ")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					indexes := wordIndex[part]
					indexes = append(indexes, count)
					wordIndex[part] = indexes
				}
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				alloc := float64(m.Alloc) / float64(1024*1024*1024)
				fmt.Println(count, alloc)
				count++
				if alloc > 64 {
					break LOOP
				}
			}
		}
		token, err = decoder.Token()
	}

	for i := 0; i < 64; i++ {
		text := <-done
		fmt.Println("---------------------")
		fmt.Println(string(text.Text))
		index.Index(text.Title, text.Text)
	}
}

// Convert converts wikitext to html
func Convert(doc string) ([]byte, error) {
	cmd := exec.Command("docker", "exec", "-i", *Container, "php", "maintenance/parse.php")
	out, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	in, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	_, err = out.Write([]byte(doc))
	if err != nil {
		return nil, err
	}
	err = out.Close()
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, err
	}
	err = cmd.Wait()
	if err != nil {
		return nil, err
	}
	return body, err
}
