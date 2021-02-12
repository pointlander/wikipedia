// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wikipedia

import (
	"html/template"
	"net/http"
	"net/url"
	"unicode"

	"github.com/julienschmidt/httprouter"
)

// IndexPage is the index page
const IndexPage = `<html>
  <head><title>Encyclopedia</title></head>
  <body>
    <h3>Encyclopedia</h3>
    <form action="/wiki/search" method="post">
      <input type="text" id="query" name="query">
      <input type="submit" value="Submit">
    </form>
  </body>
</html>
`

// EntryTemplate is a entry page
const EntryTemplate = `<html>
 <head>
  <title>{{.Title}}</title>
 </head>
 <body>
  <style>
   /* Tooltip container */
   .tooltip {
    position: relative;
    display: inline-block;
    border-bottom: 1px dotted black; /* If you want dots under the hoverable text */
   }
   /* Tooltip text */
   .tooltip .tooltiptext {
    visibility: hidden;
    width: 256px;
    background-color: black;
    color: #fff;
    text-align: center;
    padding: 5px 0;
    border-radius: 6px;

    /* Position the tooltip text - see examples below! */
    position: absolute;
    z-index: 1;
   }

   /* Show the tooltip text when you mouse over the tooltip container */
   .tooltip:hover .tooltiptext {
    visibility: visible;
   }
  </style>
  {{noescape .HTML}}
 </body>
</html>
`

// ResultsTemplate is the template for search results
const ResultsTemplate = `<html>
 <head>
  <title>Search results for {{.Title}}</title>
  </head>
  <body>
		<ul>
{{range .Results}}
			<li><a href="/wiki/article/{{escape .Article.Title}}">{{.Article.Title}}</a></li>
{{end}}
		</ul>
  </body>
 </html>
`

// Interface outputs the search interface
func Interface(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	w.Write([]byte(IndexPage))
}

// Article is the endpoint for view an article
func (e *Encyclopedia) Article(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	title := ps.ByName("article")
	runes := []rune(title)
	runes[0] = unicode.ToUpper(runes[0])
	title = string(runes)
	article := e.Lookup(title)
	err := e.entryTemplate.Execute(w, article)
	if err != nil {
		return
	}
}

// WikiSearch searches for articles
func (e *Encyclopedia) WikiSearch(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	r.ParseForm()
	query := r.Form["query"][0]
	results := e.Search(query)
	type Results struct {
		Title   string
		Results []Result
	}
	data := Results{
		Title:   query,
		Results: results,
	}
	err := e.resultsTemplate.Execute(w, data)
	if err != nil {
		return
	}
}

func noescape(str string) template.HTML {
	return template.HTML(str)
}

func escape(a string) string {
	return url.PathEscape(a)
}

// Server start server mode
func Server(encyclopedia *Encyclopedia, router *httprouter.Router) {
	entryTemplate, err := template.New("entry").Funcs(template.FuncMap{
		"noescape": noescape,
	}).Parse(EntryTemplate)
	if err != nil {
		panic(err)
	}
	resultsTemplate, err := template.New("entry").Funcs(template.FuncMap{
		"escape": escape,
	}).Parse(ResultsTemplate)
	if err != nil {
		panic(err)
	}

	encyclopedia.entryTemplate = entryTemplate
	encyclopedia.resultsTemplate = resultsTemplate
	router.GET("/wiki", Interface)
	router.GET("/wiki/article/:article", encyclopedia.Article)
	router.POST("/wiki/search", encyclopedia.WikiSearch)
}
