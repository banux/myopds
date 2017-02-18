package main

import (
	"fmt"
	"strconv"

	"github.com/blevesearch/bleve"
)

type indexData struct {
	Title       string
	Authors     string
	Description string
	Serie       string
}

func indexAll() {
	var bleveIndex bleve.Index
	var books []Book
	var err error

	bleveIndex, err = bleve.Open("db/index.bleve")
	if err == bleve.ErrorIndexPathDoesNotExist {
		indexMapping := bleve.NewIndexMapping()
		// indexMapping.AddCustomTokenFilter("clean_token", map[string]interface{}{
		// 	"type": "elision",
		// })
		bleveIndex, _ = bleve.New("db/index.bleve", indexMapping)
	}

	db.Find(&books)
	for _, book := range books {
		is := strconv.FormatInt(int64(book.ID), 10)
		authors := ""
		for _, a := range book.Authors {
			authors += a.Name + " "
		}
		data := indexData{Title: book.Title, Authors: authors, Description: book.Description, Serie: book.Serie}
		bleveIndex.Index(is, data)
	}
	fmt.Println("reindex finish")

}

func indexBook(book Book) {
	var bleveIndex bleve.Index
	var err error

	bleveIndex, err = bleve.Open("db/index.bleve")
	if err == bleve.ErrorIndexPathDoesNotExist {
		indexMapping := bleve.NewIndexMapping()
		// indexMapping.AddCustomTokenFilter("clean_token", map[string]interface{}{
		// 	"type": "elision",
		// })
		bleveIndex, _ = bleve.New("db/index.bleve", indexMapping)
	}

	is := strconv.FormatInt(int64(book.ID), 10)
	authors := ""
	for _, a := range book.Authors {
		authors += a.Name + " "
	}
	data := indexData{Title: book.Title, Authors: authors, Description: book.Description, Serie: book.Serie}
	bleveIndex.Index(is, data)
}

func findBookBySearch(searchTerm string) []Book {
	var books []Book

	bleveIndex, err := bleve.Open("db/index.bleve")
	if err != nil {
		return books
	}

	query := bleve.NewMatchQuery(searchTerm)
	search := bleve.NewSearchRequest(query)
	searchResults, _ := bleveIndex.Search(search)

	for _, r := range searchResults.Hits {
		book := Book{}
		db.First(&book, r.ID)
		if book.ID != 0 {
			books = append(books, book)
		}
	}

	return books
}

// func findBookBySearch(search string) []Book {
// 	var books []Book
//
// 	search = strings.TrimLeft(search, " ")
// 	db.Joins("left join book_authors on books.id = book_authors.book_id left join authors on book_authors.author_id = authors.id").Where("title LIKE ? OR description like ? OR authors.name LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%").Find(&books)
// 	return books
// }
