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

	"github.com/blevesearch/bleve"
)

// Container is the container id
var Container = flag.String("id", "", "the container id")

// Page is a wikitext page
type Page struct {
	Title string `xml:"title"`
	Text  string `xml:"revision>text"`
}

func main() {
	flag.Parse()

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
	for err == nil {
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "page" {
				var page Page
				decoder.DecodeElement(&page, &element)
				text, err := Convert(page.Text)
				if err != nil {
					panic(err)
				}
				fmt.Println("---------------------")
				fmt.Println(string(text))
				index.Index(page.Title, page)
			}
		}
		token, err = decoder.Token()
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
