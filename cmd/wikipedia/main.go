// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/pointlander/wikipedia"

	"github.com/julienschmidt/httprouter"
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
	// ServerFlag startup in server mode
	ServerFlag = flag.Bool("server", false, "start up in server mode")
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
		db, err := wikipedia.Open(true)
		if err != nil {
			panic(err)
		}
		article := db.Lookup(*LookupFlag)
		if article != nil {
			fmt.Println(article.Title)
			fmt.Println(article.HTML())
		}
		return
	} else if *SearchFlag != "" {
		db, err := wikipedia.Open(true)
		if err != nil {
			panic(err)
		}
		results := db.Search(*SearchFlag)
		fmt.Println("results=", len(results))
		for _, result := range results {
			fmt.Println(result.Rank, result.Count)
			fmt.Println(result.Article.Title)
		}
		return
	} else if *ServerFlag {
		router := httprouter.New()
		db, err := wikipedia.Open(true)
		if err != nil {
			panic(err)
		}
		wikipedia.Server(db, router)
		server := http.Server{
			Addr:    ":8080",
			Handler: router,
		}
		err = server.ListenAndServe()
		if err != nil {
			panic(err)
		}
		return
	}
}
