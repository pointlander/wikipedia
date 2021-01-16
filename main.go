// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"compress/bzip2"
	"encoding/binary"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/pointlander/compress"

	"github.com/boltdb/bolt"
	"github.com/golang/protobuf/proto"
)

var (
	// WikiRegex is a regex for wiki syntax
	WikiRegex = regexp.MustCompile("[^A-Za-z]+")
	// NumCPU is the number of CPUs
	NumCPU = runtime.NumCPU()
	// BuildFlag selects build mode
	BuildFlag = flag.Bool("build", false, "build the db")
	// LookupFlag selects looking up an entry
	LookupFlag = flag.String("lookup", "", "look up an entry")
)

// Page is a wikitext page
type Page struct {
	Title string `xml:"title"`
	Text  string `xml:"revision>text"`
}

// Compress compresses some data
func Compress(input []byte, output io.Writer) {
	data, channel := make([]byte, len(input)), make(chan []byte, 1)
	copy(data, input)
	channel <- data
	close(channel)
	compress.BijectiveBurrowsWheelerCoder(channel).MoveToFrontCoder().FilteredAdaptiveBitCoder().Code(output)
}

// Decompress decompresses some data
func Decompress(input io.Reader, output []byte) {
	channel := make(chan []byte, 1)
	channel <- output
	close(channel)
	compress.BijectiveBurrowsWheelerDecoder(channel).MoveToFrontDecoder().FilteredAdaptiveBitDecoder().Decode(input)
}

func main() {
	flag.Parse()

	if *BuildFlag {
		Build()
		return
	} else if *LookupFlag != "" {
		db, err := bolt.Open("wikipedia.db", 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			panic(err)
		}
		err = db.View(func(tx *bolt.Tx) error {
			wiki := tx.Bucket([]byte("wiki"))
			pages := tx.Bucket([]byte("pages"))
			value := wiki.Get([]byte(*LookupFlag))
			if value != nil {
				value := pages.Get(value)
				compressed := Compressed{}
				err = proto.Unmarshal(value, &compressed)
				if err != nil {
					return err
				}
				pressed, output := bytes.NewReader(compressed.Data), make([]byte, compressed.Size)
				compress.Mark1Decompress16(pressed, output)
				fmt.Println(string(output))
			}
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
}

// Build builds the db
func Build() {

	db, err := bolt.Open("wikipedia.db", 0600, nil)
	if err != nil {
		panic(err)
	}

	input, err := os.Open("enwiki-latest-pages-articles.xml.bz2")
	if err != nil {
		panic(err)
	}
	defer input.Close()
	reader := bzip2.NewReader(input)
	decoder := xml.NewDecoder(reader)
	lru := NewLRU(20)
	type Result struct {
		Title string
		Value []byte
		Words map[string]bool
	}
	flush := func(node *Node) error {
		err := db.Update(func(tx *bolt.Tx) error {
			idx, err := tx.CreateBucketIfNotExists([]byte("index"))
			if err != nil {
				return err
			}

			for node != nil {
				indexes := Index{
					Indexes: node.Index,
				}
				value, err := proto.Marshal(&indexes)
				if err != nil {
					panic(err)
				}
				pressed := bytes.Buffer{}
				Compress(value, &pressed)
				compressed := Compressed{
					Size: uint64(len(value)),
					Data: pressed.Bytes(),
				}
				v, err := proto.Marshal(&compressed)
				if err != nil {
					panic(err)
				}
				key := []byte(node.Key)
				if len(key) > bolt.MaxKeySize {
					key = key[:bolt.MaxKeySize]
				}
				err = idx.Put(key, v)
				if err != nil {
					return err
				}
				node = node.B
			}
			return nil
		})
		return err
	}

	results := make(chan Result, 8)
	process := func(page Page) {
		pressed := bytes.Buffer{}
		compress.Mark1Compress16([]byte(page.Text), &pressed)
		compressed := Compressed{
			Size: uint64(len([]byte(page.Text))),
			Data: pressed.Bytes(),
		}
		value, err := proto.Marshal(&compressed)
		if err != nil {
			panic(err)
		}
		text := WikiRegex.ReplaceAllLiteralString(page.Text, " ")
		parts := strings.Split(text, " ")
		words := make(map[string]bool)
		for _, part := range parts {
			part = strings.ToLower(strings.TrimSpace(part))
			if len(part) == 0 {
				continue
			}
			words[part] = true
		}
		results <- Result{
			Title: page.Title,
			Value: value,
			Words: words,
		}
	}

	write := func(wiki, pages, idx *bolt.Bucket, result Result) error {
		index, err := wiki.NextSequence()
		if err != nil {
			return err
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		alloc := float64(m.Alloc) / float64(1024*1024*1024)
		fmt.Println(index, alloc)
		value := make([]byte, 4)
		binary.LittleEndian.PutUint32(value, uint32(index))
		err = wiki.Put([]byte(result.Title), value)
		if err != nil {
			return err
		}
		err = pages.Put(value, result.Value)
		if err != nil {
			return err
		}
		for part := range result.Words {
			node, has := lru.Get(part)
			if !has {
				compressed := Compressed{}
				value := idx.Get([]byte(part))
				if len(value) > 0 {
					err = proto.Unmarshal(value, &compressed)
					if err != nil {
						return err
					}
					pressed, output := bytes.NewReader(compressed.Data), make([]byte, compressed.Size)
					Decompress(pressed, output)
					indexes := Index{}
					err = proto.Unmarshal(output, &indexes)
					if err != nil {
						return err
					}
					node.Index = indexes.Indexes
				}
			}
			tail := len(node.Index) - 1
			if tail >= 0 {
				node.Index[tail] = uint32(index) - node.Index[tail]
			}
			node.Index = append(node.Index, uint32(index))
		}
		return nil
	}

	flight := 0
	err = db.Update(func(tx *bolt.Tx) error {
		token, err := decoder.Token()
		for err == nil && flight < NumCPU {
			switch element := token.(type) {
			case xml.StartElement:
				if element.Name.Local == "page" {
					var page Page
					decoder.DecodeElement(&page, &element)
					if len(page.Text) == 0 {
						break
					}
					go process(page)
					flight++
					var m runtime.MemStats
					runtime.ReadMemStats(&m)
					alloc := float64(m.Alloc) / float64(1024*1024*1024)
					if alloc > 127 {
						return nil
					}
				}
			}
			token, err = decoder.Token()
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	done := false
	for !done {
		var node *Node
		err = db.Update(func(tx *bolt.Tx) error {
			wiki, err := tx.CreateBucketIfNotExists([]byte("wiki"))
			if err != nil {
				return err
			}
			pages, err := tx.CreateBucketIfNotExists([]byte("pages"))
			if err != nil {
				return err
			}
			idx, err := tx.CreateBucketIfNotExists([]byte("index"))
			if err != nil {
				return err
			}

			token, err := decoder.Token()
			for err == nil {
				switch element := token.(type) {
				case xml.StartElement:
					if element.Name.Local == "page" {
						if flight > 0 {
							result := <-results
							err := write(wiki, pages, idx, result)
							if err != nil {
								return err
							}
							flight--
						}

						node = lru.Flush()
						if node != nil {
							return nil
						}

						var page Page
						decoder.DecodeElement(&page, &element)
						if len(page.Text) == 0 {
							break
						}
						go process(page)
						flight++

						var m runtime.MemStats
						runtime.ReadMemStats(&m)
						alloc := float64(m.Alloc) / float64(1024*1024*1024)
						if alloc > 127 {
							return nil
						}
					}
				}
				token, err = decoder.Token()
			}
			done = true
			return nil
		})
		if err != nil {
			panic(err)
		}

		if node != nil {
			err := flush(node)
			if err != nil {
				panic(err)
			}
		}
	}

	err = db.Update(func(tx *bolt.Tx) error {
		wiki, err := tx.CreateBucketIfNotExists([]byte("wiki"))
		if err != nil {
			return err
		}
		pages, err := tx.CreateBucketIfNotExists([]byte("pages"))
		if err != nil {
			return err
		}
		idx, err := tx.CreateBucketIfNotExists([]byte("index"))
		if err != nil {
			return err
		}

		for i := 0; i < flight; i++ {
			result := <-results
			err := write(wiki, pages, idx, result)
			if err != nil {
				return err
			}

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			alloc := float64(m.Alloc) / float64(1024*1024*1024)
			if alloc > 127 {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	err = flush(lru.Head)
	if err != nil {
		panic(err)
	}
}
