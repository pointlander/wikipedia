// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"

	"github.com/pointlander/wikipedia"
)

var (
	// BuildFlag selects build mode
	BuildFlag = flag.Bool("build", false, "build the db")
	// RankFlag ranks the pages
	RankFlag = flag.Bool("rank", false, "build the db")
	// LookupFlag selects looking up an entry
	LookupFlag = flag.String("lookup", "", "look up an entry")
	// SearchFlag searches for the text
	SearchFlag = flag.String("search", "", "searches for the text")
)

func main() {
	flag.Parse()

	if *BuildFlag {
		wikipedia.Build()
		return
	} else if *RankFlag {
		wikipedia.Rank()
		return
	} else if *LookupFlag != "" {
		article := wikipedia.Lookup(*LookupFlag)
		if article != nil {
			fmt.Println(article.Title)
			fmt.Println(article.HTML())
		}
		return
	} else if *SearchFlag != "" {
		results := wikipedia.Search(*SearchFlag)
		fmt.Println("results=", len(results))
		for _, result := range results {
			fmt.Println(result.Rank, result.Count)
			fmt.Println(result.Article.Title)
		}
		return
	}
}
