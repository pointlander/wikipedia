// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wikipedia

import (
	"html/template"
	"net/http"
	"unicode"

	"github.com/julienschmidt/httprouter"
)

// EntryTemplate is a entry page
const EntryTemplate = `<html>
 <head>
  <title>{{.Title}}</title>
 </head>
 <body>
  {{noescape .HTML}}
 </body>
</html>
`

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

func noescape(str string) template.HTML {
	return template.HTML(str)
}

// Server start server mode
func Server(encyclopedia *Encyclopedia, router *httprouter.Router) {
	entryTemplate, err := template.New("entry").Funcs(template.FuncMap{
		"noescape": noescape,
	}).Parse(EntryTemplate)
	if err != nil {
		panic(err)
	}
	encyclopedia.entryTemplate = entryTemplate
	router.GET("/wiki/article/:article", encyclopedia.Article)
}
