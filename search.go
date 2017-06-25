package main

import "strings"

func findBookBySearch(search string) []Book {
	var books []Book

	search = strings.TrimLeft(search, " ")
	search = strings.Replace(search, "''", " ", -1)
	search = "%" + strings.Replace(search, " ", "%", -1) + "%"
	db.Joins("left join book_authors on books.id = book_authors.book_id left join authors on book_authors.author_id = authors.id").Where("title LIKE ? OR description like ? OR authors.name LIKE ?", search, search, search).Find(&books)
	return books
}
