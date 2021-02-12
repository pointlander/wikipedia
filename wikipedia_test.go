// Copyright 2021 The Wikipedia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wikipedia

import (
	"testing"
)

func TestWikiTextToHTMLULists(t *testing.T) {
	text := `This is a test
* Test 1
** Test 2
*** Test 3
**** Test 4
*** Test 3 Again
* Test 1 Again
End Test`
	html := WikiTextToHTML(text)
	target := `This is a test
<ul>
 <li>Test 1
 <ul>
  <li>Test 2
  <ul>
   <li>Test 3
   <ul>
    <li>Test 4</li>
   </ul>
   <li>Test 3 Again</li>
  </ul>
 </li>
 </ul>
 <li>Test 1 Again</li>
</ul>
End Test`
	if html != target {
		t.Fatalf("not equal %s", html)
	}
}

func TestWikiTextToHTMLOLists(t *testing.T) {
	text := `This is a test
# Test 1
## Test 2
### Test 3
#### Test 4
### Test 3 Again
# Test 1 Again
End Test`
	html := WikiTextToHTML(text)
	target := `This is a test
<ol>
 <li>Test 1
 <ol>
  <li>Test 2
  <ol>
   <li>Test 3
   <ol>
    <li>Test 4</li>
   </ol>
   <li>Test 3 Again</li>
  </ol>
 </li>
 </ol>
 <li>Test 1 Again</li>
</ol>
End Test`
	if html != target {
		t.Fatalf("not equal %s", html)
	}
}

func TestWikiTextToHTMLCite(t *testing.T) {
	text := `<ref>{{cite act |date=March 3, 1931 |article=14 |article-type=H.R. |legislature=[[71st United States Congress]] |title=An Act To make The Star-Spangled Banner the national anthem of the United States of America |url=https://uscode.house.gov/statviewer.htm?volume=46&page=1508}}</ref>`
	html := WikiTextToHTML(text)
	target := `<sup class="tooltip">0<span class="tooltiptext"><ref>{{cite act |date=March 3, 1931 |article=14 |article-type=H.R. |legislature=[[71st United States Congress]] |title=An Act To make The Star-Spangled Banner the national anthem of the United States of America |url=https://uscode.house.gov/statviewer.htm?volume=46&page=1508}}</ref></span></sup>`
	if html != target {
		t.Fatalf("not equal %s", html)
	}
}
