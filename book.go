package main

import (
	"archive/zip"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beevik/etree"
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
	Name string
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
	Edited             bool
	OpdsIdentifier     string
	ServiceDownloadURL string
	CoverPath          string
	CoverType          string
	Authors            []Author `gorm:"many2many:book_authors;"`
	Tags               []Tag    `gorm:"many2many:book_tags;"`
}

func (book *Book) getMetada() {
	var opfFileName string
	var title string
	var description string
	var identifier string
	var coverFilename string
	var coverID string
	var coverType string
	var buff []byte
	var authors []Author
	var resourcePath = ""
	var tag Tag

	bookIDStr := strconv.Itoa(int(book.ID))
	fmt.Println("get Meta for Book " + bookIDStr)
	filePath := book.FilePath()

	zipReader, err := zip.OpenReader(filePath)
	if err != nil {
		fmt.Println("failed to open zip " + filePath)
		fmt.Println(err)
		return
	}

	for _, f := range zipReader.File {
		if f.Name == "META-INF/container.xml" {
			rc, err := f.Open()
			if err != nil {
				fmt.Println("error openging " + f.Name)
			}
			doc := etree.NewDocument()
			_, err = doc.ReadFrom(rc)
			if err == nil {
				root := doc.SelectElement("container")
				rootFiles := root.SelectElements("rootfiles")
				for _, rootFileTag := range rootFiles {
					rootFile := rootFileTag.SelectElement("rootfile")
					if rootFile != nil {
						opfFileName = rootFile.SelectAttrValue("full-path", "")
					}
				}
			} else {
				fmt.Println(err)
			}
			rc.Close()
		}
	}

	if strings.Contains(opfFileName, "/") {
		resourcePath = strings.Split(opfFileName, "/")[0]
	}

	if opfFileName != "" {
		for _, f := range zipReader.File {
			if f.Name == opfFileName {
				rc, err := f.Open()
				if err != nil {
					fmt.Println("error openging " + f.Name)
				}
				doc := etree.NewDocument()
				_, err = doc.ReadFrom(rc)
				if err == nil {
					root := doc.SelectElement("package")
					meta := root.SelectElement("metadata")
					titleElem := meta.SelectElement("title")
					title = titleElem.Text()
					identifierElem := meta.SelectElement("identifier")
					if identifierElem.SelectAttrValue("scheme", "") == "ISBN" {
						identifier = identifierElem.Text()
					}
					if identifierElem.SelectAttrValue("scheme", "") == "ean" {
						identifier = identifierElem.Text()
					}
					descriptionElem := meta.SelectElement("description")
					if descriptionElem != nil {
						description = descriptionElem.Text()
					} else {
						description = ""
					}
					creators := meta.SelectElements("creator")
					if creators != nil {
						authors = make([]Author, len(creators), len(creators))
						for i, creator := range creators {
							db.Where("name = ? ", creator.Text()).Find(&authors[i])
							if authors[i].ID == 0 {
								authors[i].Name = creator.Text()
								db.Save(&authors[i])
							}
						}
					}

					tagElem := meta.SelectElement("subject")
					if tagElem != nil {
						fmt.Println("find tag " + tagElem.Text())
						db.Where("name = ?", tagElem.Text()).First(&tag)
						if tag.ID == 0 {
							tag.Name = tagElem.Text()
							db.Save(&tag)
						}
					}

					metaElems := meta.SelectElements("meta")
					for _, metaElem := range metaElems {
						fmt.Println(metaElem)
						if metaElem != nil {
							if metaElem.SelectAttrValue("name", "") == "cover" {
								coverID = metaElem.SelectAttrValue("content", "")
							}
						}
					}
					manifestElem := root.SelectElement("manifest")
					items := manifestElem.SelectElements("item")
					for _, i := range items {
						if i.SelectAttrValue("id", "") == coverID {
							coverFilename = i.SelectAttrValue("href", "")
							coverType = i.SelectAttrValue("media-type", "")
						}
					}

					book.Title = title
					book.Description = description
					book.Authors = authors
					book.Isbn = identifier
					if tag.ID != 0 {
						book.Tags = []Tag{tag}
					}
					db.Save(&book)

				} else {
					fmt.Println(err)
				}
			}
		}
	}

	fmt.Println(coverFilename)
	if coverFilename != "" && (filepath.Ext(coverFilename) == ".jpeg" || filepath.Ext(coverFilename) == ".jpg" || filepath.Ext(coverFilename) == ".png") {
		fmt.Println("create cover")
		bookIDStr := strconv.Itoa(int(book.ID))
		coverDirPath := "public/books/" + bookIDStr
		coverFilePath := coverDirPath + "/" + bookIDStr
		if coverType == "image/jpeg" {
			coverFilePath = coverFilePath + ".jpg"
		} else if coverType == "image/png" {
			coverFilePath = coverFilePath + ".png"
		} else {
			fmt.Println("can't find ext for " + coverType)
		}

		_, err := os.Open(coverFilePath)
		if os.IsNotExist(err) {

			for _, f := range zipReader.File {
				//fmt.Println(f.Name)
				checkName := coverFilename
				if resourcePath != "" {
					checkName = resourcePath + "/" + coverFilename
				}
				if f.Name == checkName {
					fmt.Println("open : " + checkName)
					rc, err := f.Open()

					buff, err = ioutil.ReadAll(rc)
					if err != nil {
						fmt.Println(err)
					}
				}
			}

			os.MkdirAll(coverDirPath, os.ModePerm)
			coverFile, err := os.Create(coverFilePath)
			if err != nil {
				fmt.Println(err)
			}

			defer coverFile.Close()
			_, err = coverFile.Write(buff)
			if err != nil {
				fmt.Println(err)
			}
		}
		book.CoverType = coverType
		book.CoverPath = coverFilePath
		db.Save(&book)
	}

}

func (book *Book) DownloadURL() string {
	bookIDStr := strconv.Itoa(int(book.ID))
	epubDirPath := "/books/" + bookIDStr
	epubFilePath := epubDirPath + "/" + bookIDStr + ".epub"
	return epubFilePath
}

func (book *Book) ReaderURL() string {
	bookIDStr := strconv.Itoa(int(book.ID))
	readerPath := "/books/" + bookIDStr + "/reader/"
	return readerPath
}

func (book *Book) FilePath() string {
	bookIDStr := strconv.Itoa(int(book.ID))
	epubDirPath := "public/books/" + bookIDStr
	epubFilePath := epubDirPath + "/" + bookIDStr + ".epub"
	return epubFilePath
}

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
