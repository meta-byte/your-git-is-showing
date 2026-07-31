package main

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func isHTML(response *http.Response) bool {
	ct := response.Header.Get("Content-Type")
	return strings.Contains(ct, "text/html")
}

func getIndexedFiles(body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}

	var files []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					u, err := url.Parse(attr.Val)
					if err != nil {
						continue
					}
					if u.Path != "" && isSafePath(u.Path) && u.Scheme == "" && u.Host == "" {
						files = append(files, u.Path)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return files
}
