package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/feedbooks/r2-streamer-go/fetcher"
	"github.com/feedbooks/r2-streamer-go/parser"
	"github.com/jinzhu/gorm"
)

// Author store author information
type Author struct {
	gorm.Model
	Name string
}

// Tag store tag information
type Tag struct {
	gorm.Model
	Name      string
	DeletedAt time.Time
}

// BookTag store link beetween book and tag
type BookTag struct {
	gorm.Model
	BookID uint
	TagID  uint
}

// BookAuthor store link beetween book and author
type BookAuthor struct {
	gorm.Model
	BookID   uint
	AuthorID uint
}

// Book store book information
type Book struct {
	gorm.Model
	Isbn               string
	Title              string
	Description        string
	Language           string
	Publisher          string
	Collection         string
	Edited             bool
	OpdsIdentifier     string
	ServiceDownloadURL string
	CoverPath          string
	CoverType          string
	Serie              string
	SerieNumber        float32
	Authors            []Author `gorm:"many2many:book_authors;"`
	Tags               []Tag    `gorm:"many2many:book_tags;"`
}

func (book *Book) getMetada() {
	var authors []Author
	var tags []Tag

	bookIDStr := strconv.Itoa(int(book.ID))
	fmt.Println("get Meta for Book " + bookIDStr)
	filePath := book.FilePath()

	publication, _ := parser.Parse(filePath)

	authors = make([]Author, len(publication.Metadata.Author)+len(publication.Metadata.Contributor), len(publication.Metadata.Author)+len(publication.Metadata.Contributor))
	for i, creator := range publication.Metadata.Author {
		db.Where("name = ? ", creator.Name).Find(&authors[i])
		if authors[i].ID == 0 {
			authors[i].Name = creator.Name.String()
			db.Save(&authors[i])
		}
	}

	book.Title = publication.Metadata.Title.String()
	book.Description = publication.Metadata.Description
	book.Authors = authors
	book.Isbn = publication.Metadata.Identifier
	if publication.Metadata.BelongsTo != nil && len(publication.Metadata.BelongsTo.Series) > 0 {
		book.Serie = publication.Metadata.BelongsTo.Series[0].Name
		book.SerieNumber = publication.Metadata.BelongsTo.Series[0].Position
	}
	for _, sub := range publication.Metadata.Subject {
		tag := Tag{}
		db.Where("name = ?", sub.Name).First(&tag)
		if tag.ID == 0 {
			tag.Name = sub.Name
			db.Save(&tag)
		}
		tags = append(tags, tag)
	}
	book.Tags = tags

	db.Save(&book)

	linkCover, _ := publication.GetCover()
	coverDirPath := "public/books/" + bookIDStr
	coverFilePath := coverDirPath + "/" + bookIDStr + filepath.Ext(linkCover.Href)
	_, err := os.Open(coverFilePath)
	if os.IsNotExist(err) {

		os.MkdirAll(coverDirPath, os.ModePerm)
		coverFile, err := os.Create(coverFilePath)
		if err != nil {
			fmt.Println(err)
		}
		coverReader, _, _ := fetcher.Fetch(publication, linkCover.Href)
		io.Copy(coverFile, coverReader)
		defer coverFile.Close()
	}
	book.CoverType = linkCover.TypeLink
	book.CoverPath = coverFilePath
	db.Save(&book)
}

// DownloadURL get url of the book
func (book *Book) DownloadURL() string {
	bookIDStr := strconv.Itoa(int(book.ID))
	epubDirPath := "/books/" + bookIDStr
	epubFilePath := epubDirPath + "/" + bookIDStr + ".epub"
	return epubFilePath
}

// ReaderURL return the reader base url
func (book *Book) ReaderURL() string {
	bookIDStr := strconv.Itoa(int(book.ID))
	readerPath := "/books/" + bookIDStr + "/reader/"
	return readerPath
}

// FilePath get filepath for the book on os
func (book *Book) FilePath() string {
	bookIDStr := strconv.Itoa(int(book.ID))
	epubDirPath := "public/books/" + bookIDStr
	epubFilePath := epubDirPath + "/" + bookIDStr + ".epub"
	return epubFilePath
}

// CoverDownloadURL get url for book cover
func (book Book) CoverDownloadURL() string {
	var coverFilePath string

	bookIDStr := strconv.Itoa(int(book.ID))
	coverDirPath := "/books/" + bookIDStr
	if book.CoverType == jpgMediaType {
		coverFilePath = coverDirPath + "/" + bookIDStr + ".jpg"
	} else if book.CoverType == pngMediaType {
		coverFilePath = coverDirPath + "/" + bookIDStr + ".png"
	}
	return coverFilePath
}

// TagFormData generate the string for the JS in edit form
func (book Book) TagFormData() string {
	var tags []Tag
	var tagsString []string

	db.Model(&book).Related(&tags, "Tags")

	for _, tag := range tags {
		tagsString = append(tagsString, tag.Name)
	}

	return strings.Join(tagsString, ",")
}

// ToURL return tag URL
func (tag *Tag) ToURL() string {
	return "/index.html?tag=" + strings.Replace(tag.Name, " ", "+", -1)
}

// BeforeDelete callback to clean assoction before deleting tag
func (tag *Tag) BeforeDelete() (err error) {
	if tag.ID != 0 {
		db.Delete(BookTag{}, "tag_id =? ", tag.ID)
	}
	return nil
}
