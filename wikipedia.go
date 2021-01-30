// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wikipedia

import (
	"bytes"
	"compress/bzip2"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/pointlander/compress"
	"github.com/pointlander/pagerank"

	"github.com/boltdb/bolt"
	"github.com/golang/protobuf/proto"
)

var (
	// WikiRegex is a regex for wiki syntax
	WikiRegex = regexp.MustCompile("[^A-Za-z]+")
	// NumCPU is the number of CPUs
	NumCPU = runtime.NumCPU()
)

// Page is a wikitext page
type Page struct {
	Title string `xml:"title"`
	ID    uint64 `xml:"id"`
	Text  string `xml:"revision>text"`
}

// Result is a search result
type Result struct {
	Index   uint32
	Count   int
	Rank    float32
	Article *Article
	Matches int
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

// Encyclopedia is an encyclopedia
type Encyclopedia struct {
	DB            *bolt.DB
	entryTemplate *template.Template
}

// Open opens an encyclopedia
func Open(readonly bool) (*Encyclopedia, error) {
	db, err := bolt.Open("wikipedia.db", 0600, &bolt.Options{ReadOnly: readonly})
	if err != nil {
		return nil, err
	}
	return &Encyclopedia{
		DB: db,
	}, nil
}

// Build builds the db
func Build() {
	encyclopedia, err := Open(false)
	if err != nil {
		panic(err)
	}
	db := encyclopedia.DB

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
		article := Article{
			Title: page.Title,
			ID:    page.ID,
			Text:  page.Text,
		}
		encoded, err := proto.Marshal(&article)
		if err != nil {
			panic(err)
		}
		pressed := bytes.Buffer{}
		compress.Mark1Compress16(encoded, &pressed)
		compressed := Compressed{
			Size: uint64(len(encoded)),
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

// Rank ranks the pages
func Rank() {
	graph := pagerank.NewGraph32(1024)
	graph.Verbose = true
	encyclopedia, err := Open(false)
	if err != nil {
		panic(err)
	}
	db := encyclopedia.DB
	err = db.View(func(tx *bolt.Tx) error {
		wiki := tx.Bucket([]byte("wiki"))
		pages := tx.Bucket([]byte("pages"))
		cursor := pages.Cursor()
		key, value := cursor.First()
		i, flight := 0, 0
		type Result struct {
			Source uint32
			Links  []uint32
		}
		done := make(chan Result, 8)
		process := func(key uint32, compressed *Compressed) {
			pressed, output := bytes.NewReader(compressed.Data), make([]byte, compressed.Size)
			compress.Mark1Decompress16(pressed, output)
			article := Article{}
			err := proto.Unmarshal(output, &article)
			if err != nil {
				panic(err)
			}
			parser := &Wikipedia{Buffer: article.Text}
			parser.Init()
			if err := parser.Parse(); err != nil {
				panic(err)
			}
			element := func(node *node32) string {
				node = node.up
				for node != nil {
					switch node.pegRule {
					case rulelink:
						return string(parser.buffer[node.begin:node.end])
					}
					node = node.next
				}
				return ""
			}
			ast, links := parser.AST(), make([]uint32, 0, 8)
			node := ast.up
			for node != nil {
				switch node.pegRule {
				case ruleelement:
					link := element(node)
					if link != "" {
						link = strings.TrimSpace(link)
						value := wiki.Get([]byte(link))
						if len(value) > 0 {
							target := binary.LittleEndian.Uint32(value)
							links = append(links, target)
						}
					}
				}
				node = node.next
			}
			done <- Result{
				Source: key,
				Links:  links,
			}
		}
		for key != nil && value != nil && flight < NumCPU {
			compressed := &Compressed{}
			err = proto.Unmarshal(value, compressed)
			if err != nil {
				return err
			}
			go process(binary.LittleEndian.Uint32(key), compressed)
			flight++

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			alloc := float64(m.Alloc) / float64(1024*1024*1024)
			fmt.Println(i, alloc)
			key, value = cursor.Next()
			i++
		}

		for key != nil && value != nil {
			result := <-done
			flight--
			for _, link := range result.Links {
				graph.Link(uint64(result.Source), uint64(link), 1.0)
			}

			compressed := &Compressed{}
			err = proto.Unmarshal(value, compressed)
			if err != nil {
				return err
			}
			go process(binary.LittleEndian.Uint32(key), compressed)
			flight++

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			alloc := float64(m.Alloc) / float64(1024*1024*1024)
			fmt.Println(i, alloc)
			key, value = cursor.Next()
			i++
		}

		for j := 0; j < flight; j++ {
			result := <-done
			for _, link := range result.Links {
				graph.Link(uint64(result.Source), uint64(link), 1.0)
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	type Rank struct {
		Node uint32
		Rank float32
	}
	ranks := make([]Rank, 0, 8)
	graph.Rank(.85, .00001, func(node uint64, rank float32) {
		ranks = append(ranks, Rank{
			Node: uint32(node),
			Rank: rank,
		})
	})

	err = db.Update(func(tx *bolt.Tx) error {
		tx.DeleteBucket([]byte("ranks"))
		_, err := tx.CreateBucketIfNotExists([]byte("ranks"))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	for i := 0; i < len(ranks); i += 1024 {
		fmt.Println(float64(i) / float64(len(ranks)))
		err := db.Update(func(tx *bolt.Tx) error {
			ranksBucket := tx.Bucket([]byte("ranks"))
			end := i + 1024
			if end > len(ranks) {
				end = len(ranks)
			}
			for _, rank := range ranks[i:end] {
				key, value := make([]byte, 4), make([]byte, 4)
				binary.LittleEndian.PutUint32(key, uint32(rank.Node))
				binary.LittleEndian.PutUint32(value, math.Float32bits(rank.Rank))
				ranksBucket.Put(key, value)
			}
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
}

// HTML returns the HTML version of the article
func (a *Article) HTML() string {
	parser := &Wikipedia{Buffer: a.Text}
	parser.Init()
	if err := parser.Parse(); err != nil {
		panic(err)
	}
	text := ""
	element := func(node *node32) {
		node = node.up
		for node != nil {
			switch node.pegRule {
			case ruleheading6:
				text += fmt.Sprintf("<h6>%s</h6>\n", strings.TrimSpace(string(parser.buffer[node.up.begin:node.up.end])))
			case ruleheading5:
				text += fmt.Sprintf("<h5>%s</h5>\n", strings.TrimSpace(string(parser.buffer[node.up.begin:node.up.end])))
			case ruleheading4:
				text += fmt.Sprintf("<h4>%s</h4>\n", strings.TrimSpace(string(parser.buffer[node.up.begin:node.up.end])))
			case ruleheading3:
				text += fmt.Sprintf("<h3>%s</h3>\n", strings.TrimSpace(string(parser.buffer[node.up.begin:node.up.end])))
			case ruleheading2:
				text += fmt.Sprintf("<h2>%s</h2>\n", strings.TrimSpace(string(parser.buffer[node.up.begin:node.up.end])))
			case ruleheading1:
				text += fmt.Sprintf("<h1>%s</h1>\n", strings.TrimSpace(string(parser.buffer[node.up.begin:node.up.end])))
			case rulehr:
				text += fmt.Sprintf("<hr/>\n")
			case rulebr:
				text += fmt.Sprintf("<br/>\n\n")
			case rulelink:
				link := string(parser.buffer[node.begin:node.end])
				if node.next != nil && node.next.pegRule == ruletext {
					node = node.next
					linkText := string(parser.buffer[node.begin:node.end])
					text += fmt.Sprintf("<a href=\"/wiki/article/%s\">%s</a>", url.PathEscape(link), linkText)
				} else {
					text += fmt.Sprintf("<a href=\"/wiki/article/%s\">%s</a>", url.PathEscape(link), link)
				}
				return
			case rulewild:
				text += string(parser.buffer[node.begin:node.end])
			}
			node = node.next
		}
	}
	ast := parser.AST()
	node := ast.up
	for node != nil {
		switch node.pegRule {
		case ruleelement:
			element(node)
		}
		node = node.next
	}
	return text
}

// Lookup looks up an article
func (e *Encyclopedia) Lookup(title string) (article *Article) {
	db := e.DB
	err := db.View(func(tx *bolt.Tx) error {
		wiki := tx.Bucket([]byte("wiki"))
		pages := tx.Bucket([]byte("pages"))
		value := wiki.Get([]byte(title))
		if value != nil {
			value := pages.Get(value)
			compressed := Compressed{}
			err := proto.Unmarshal(value, &compressed)
			if err != nil {
				return err
			}
			pressed, output := bytes.NewReader(compressed.Data), make([]byte, compressed.Size)
			compress.Mark1Decompress16(pressed, output)
			a := Article{}
			err = proto.Unmarshal(output, &a)
			if err != nil {
				return err
			}
			article = &a
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return article
}

// Search search for a page
func (e *Encyclopedia) Search(query string) []Result {
	db := e.DB
	parts, results := strings.Split(query, " "), make([]Result, 0, 8)
	err := db.View(func(tx *bolt.Tx) error {
		pagesBucket := tx.Bucket([]byte("pages"))
		indexBucket := tx.Bucket([]byte("index"))
		ranksBucket := tx.Bucket([]byte("ranks"))
		indexes := make(map[uint32]int)
		for _, part := range parts {
			part = strings.ToLower(strings.TrimSpace(part))
			value := indexBucket.Get([]byte(part))
			if len(value) > 0 {
				compressed := Compressed{}
				err := proto.Unmarshal(value, &compressed)
				if err != nil {
					return err
				}
				pressed, output := bytes.NewReader(compressed.Data), make([]byte, compressed.Size)
				Decompress(pressed, output)
				values := Index{}
				err = proto.Unmarshal(output, &values)
				if err != nil {
					return err
				}
				index := values.Indexes[len(values.Indexes)-1]
				indexes[index]++
				for i := len(values.Indexes) - 2; i >= 0; i-- {
					index -= values.Indexes[i]
					indexes[index]++
				}
			}
		}

		for index, count := range indexes {
			value := make([]byte, 4)
			binary.LittleEndian.PutUint32(value, uint32(index))
			rank := ranksBucket.Get(value)
			var r float32
			if len(rank) > 0 {
				r = math.Float32frombits(binary.LittleEndian.Uint32(rank))
			}
			results = append(results, Result{
				Index: index,
				Count: count,
				Rank:  r,
			})
		}
		for i, result := range results {
			index := make([]byte, 4)
			binary.LittleEndian.PutUint32(index, uint32(result.Index))
			value := pagesBucket.Get(index)
			compressed := Compressed{}
			err := proto.Unmarshal(value, &compressed)
			if err != nil {
				return err
			}
			pressed, output := bytes.NewReader(compressed.Data), make([]byte, compressed.Size)
			compress.Mark1Decompress16(pressed, output)
			article := &Article{}
			err = proto.Unmarshal(output, article)
			if err != nil {
				return err
			}
			results[i].Article = article
			for _, part := range parts {
				part = strings.ToLower(strings.TrimSpace(part))
				exp := regexp.MustCompile(part)
				matches := exp.FindAllStringIndex(strings.ToLower(article.Text), -1)
				results[i].Matches += len(matches)
			}
		}
		sort.Slice(results, func(i, j int) bool {
			if results[j].Count < results[i].Count {
				return true
			} else if results[j].Count == results[i].Count {
				return results[j].Rank*float32(results[j].Matches) < results[i].Rank*float32(results[i].Matches)
			}
			return false
		})
		return nil
	})
	if err != nil {
		panic(err)
	}
	return results
}
