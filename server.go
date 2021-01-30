// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wikipedia

import (
	"html/template"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

// EntryTemplate is a entry page
const EntryTemplate = `<html>
 <head>
  <title>{{.Title}}</title>
 </head>
 <body>
  {{.HTML}}
 </body>
</html>
`

// Article is the endpoint for view an article
func (e *Encyclopedia) Article(w http.ResponseWriter, r *http.Request, ps httprouter.Param) {
	title := ps.ByName("article")
	article := e.Lookup(title)
	err := e.entryTemplate.Execute(w, article)
	if err != nil {
		return
	}
}

// Server start server mode
func Server(encyclopedia *Encyclopedia, router *httprouter.Router) {
	entryTemplate, err := template.New("entry").Parse(EntryTemplate)
	if err != nil {
		panic(err)
	}
	encyclopedia.entryTemplate = entryTemplate
	router.GET("/wiki/article/:article", encyclopedia.Article)
}
