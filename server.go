package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"code.google.com/p/go-uuid/uuid"
	"github.com/beevik/etree"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/graceful"
)

type ServerOption struct {
	Id                int64
	Uuid              string
	Password          string
	Name              string
	LastSync          time.Time `sql:"DEFAULT:current_timestamp"`
	Port              int       `sql:"DEFAULT:3000"`
	NumberBookPerPage int       `sql:"DEFAULT:20"`
}

type Service struct {
	Id           int64
	Url          string
	Login        string
	Password     string
	RefreshToken string
	Type         string
}

type Author struct {
	Id   int64
	Name string
}

type Category struct {
	Id   int64
	Name string
}

type BookAuthor struct {
	Id       int64
	BookId   int64
	AuthorId int64
}

type Book struct {
	Id                 int64
	Title              string
	Description        string
	Language           string
	Publisher          string
	OpdsIdentifier     string
	ServiceDownloadUrl string
	CoverPath          string
	CoverType          string
	UpdatedAt          time.Time `sql:"DEFAULT:current_timestamp"`
	Authors            []Author  `gorm:"many2many:book_authors;"`
}

var db gorm.DB

func main() {
	var serverOption ServerOption
	var err error

	db, err = gorm.Open("sqlite3", "gopds.db")
	if err != nil {
		panic(err)
	}

	db.AutoMigrate(&Service{}, &Book{}, &Author{}, &Category{}, &ServerOption{}, &BookAuthor{})

	db.First(&serverOption)
	if serverOption.Uuid == "" {
		serverOption.Uuid = uuid.NewRandom().String()
	}
	if serverOption.Name == "" {
		serverOption.Name = "MyOPDS"
	}
	if serverOption.NumberBookPerPage == 0 {
		serverOption.NumberBookPerPage = 20
	}
	db.Save(&serverOption)

	go sync_opds(db)

	routeur := mux.NewRouter()
	routeur.HandleFunc("/index.{format}", rootHandler)
	//routeur.HandleFunc("/books/{id}.{format}", bookHandler)

	n := negroni.Classic()
	n.UseHandler(routeur)
	fmt.Println("launching server")
	graceful.Run(":"+strconv.Itoa(serverOption.Port), 10*time.Second, n)

}

func rootHandler(res http.ResponseWriter, req *http.Request) {
	var books []Book
	var booksCount int
	var serverOption ServerOption
	var page string
	var pageInt int = 1
	var limit int = 0
	var offset int
	var nextLink string
	var prevLink string
	type JsonData struct {
		PrevLink string
		NextLink string
		LastPage int
		Books    []Book
	}

	base_doc := etree.NewDocument()
	base_doc.Indent(2)

	db.First(&serverOption)

	page = req.URL.Query().Get("page")
	if page != "" {
		pageInt, _ = strconv.Atoi(page)
		if pageInt > 1 {
			prevPageStr := strconv.Itoa(pageInt - 1)
			prevReq := req
			values := prevReq.URL.Query()
			values.Set("page", prevPageStr)
			prevReq.URL.RawQuery = values.Encode()
			prevLink = prevReq.URL.String()
		}
		nextPageStr := strconv.Itoa(pageInt + 1)
		nextReq := req
		values := nextReq.URL.Query()
		values.Set("page", nextPageStr)
		nextReq.URL.RawQuery = values.Encode()
		nextLink = nextReq.URL.String()
	} else {
		nextReq := req
		values := nextReq.URL.Query()
		values.Add("page", "2")
		nextReq.URL.RawQuery = values.Encode()
		nextLink = nextReq.URL.String()
	}

	limit = serverOption.NumberBookPerPage
	offset = limit * (pageInt - 1)

	db.Limit(limit).Offset(offset).Find(&books)
	db.Model(Book{}).Count(&booksCount)
	fmt.Println(offset)
	fmt.Print(booksCount)
	if offset+limit > booksCount {
		nextLink = ""
	}
	lastPage := booksCount / limit

	vars := mux.Vars(req)

	if vars["format"] == "atom" {
		res.Header().Set("Content-Type", "application/atom+xml")
		feed := base_opds(base_doc, serverOption.Uuid, serverOption.Name, booksCount, serverOption.NumberBookPerPage, offset+1, prevLink, nextLink)
		for _, book := range books {
			entry_opds(&book, feed)
		}
		xmlString, _ := base_doc.WriteToString()
		fmt.Fprintf(res, xmlString)
	} else {
		data := JsonData{PrevLink: prevLink, NextLink: nextLink}
		data.LastPage = lastPage + 1
		data.Books = make([]Book, len(books), len(books))
		for i, book := range books {
			data.Books[i] = book
		}
		page, _ := json.Marshal(data)
		fmt.Fprintf(res, string(page))
	}

}

func base_opds(doc *etree.Document, uuid string, name string, totalResult int, perPage int, offset int, prevLink string, nextLink string) *etree.Element {
	var totalResultText string
	var perPageText string
	var offsetText string

	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)

	feed := doc.CreateElement("feed")
	feed.CreateAttr("xml:lang", "fr")
	feed.CreateAttr("xmlns:dcterms", "http://purl.org/dc/terms/")
	feed.CreateAttr("xmlns:thr", "http://purl.org/syndication/thread/1.0")
	feed.CreateAttr("xmlns:opds", "http://opds-spec.org/2010/catalog")
	feed.CreateAttr("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance")
	feed.CreateAttr("xmlns:app", "http://www.w3.org/2007/app")
	feed.CreateAttr("xmlns", "http://www.w3.org/2005/Atom")
	feed.CreateAttr("xmlns:opensearch", "http://a9.com/-/spec/opensearch/1.1/")

	id := feed.CreateElement("id")
	id.SetText(uuid)

	title := feed.CreateElement("title")
	title.SetText(name)

	updatedAt := feed.CreateElement("updated_at")
	updatedAt.SetText(time.Now().Format(time.RFC3339))

	author := feed.CreateElement("author")
	author_name := author.CreateElement("name")
	author_name.SetText("MyOPDS")
	author_uri := author.CreateElement("uri")
	author_uri.SetText("http://www.myopds.com")

	if totalResult > 0 {
		totalResultXml := feed.CreateElement("opensearch:totalResults")
		totalResultText = strconv.Itoa(totalResult)
		totalResultXml.SetText(totalResultText)
	}
	if perPage > 0 {
		perPageXml := feed.CreateElement("opensearch:itemsPerPage")
		perPageText = strconv.Itoa(perPage)
		perPageXml.SetText(perPageText)
	}
	if offset > 1 {
		offsetXml := feed.CreateElement("opensearch:startIndex")
		offsetText = strconv.Itoa(offset)
		offsetXml.SetText(offsetText)
	}

	if prevLink != "" {
		prevLinkXml := feed.CreateElement("link")
		prevLinkXml.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		prevLinkXml.CreateAttr("title", "Previous")
		prevLinkXml.CreateAttr("href", prevLink)
		prevLinkXml.CreateAttr("rel", "previous")
	}

	if nextLink != "" {
		nextLinkXml := feed.CreateElement("link")
		nextLinkXml.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		nextLinkXml.CreateAttr("title", "Next")
		nextLinkXml.CreateAttr("href", nextLink)
		nextLinkXml.CreateAttr("rel", "next")
	}

	return feed
}

func entry_opds(book *Book, feed *etree.Element) {
	var authors []Author

	entry := feed.CreateElement("entry")

	id := entry.CreateElement("id")
	id.SetText(strconv.FormatInt(book.Id, 10))

	updatedAt := entry.CreateElement("updated_at")
	updatedAt.SetText(book.UpdatedAt.Format(time.RFC3339))

	title := entry.CreateElement("title")
	title.SetText(book.Title)

	db.Model(book).Related(&authors, "Authors")
	for _, author := range authors {
		authorTag := entry.CreateElement("author")
		name := authorTag.CreateElement("name")
		name.SetText(author.Name)
		uri := authorTag.CreateElement("uri")
		uri.SetText("/authors/" + strconv.FormatInt(author.Id, 10))
	}

	language := entry.CreateElement("dcterms:language")
	language.SetText(book.Language)

	summary := entry.CreateElement("summary")
	summary.CreateAttr("type", "text")
	summary.CreateCharData(book.Description)

	link := entry.CreateElement("link")
	link.CreateAttr("rel", "http://opds-spec.org/acquisition")
	link.CreateAttr("type", "application/epub+zip")
	link.CreateAttr("href", bookDownloadUrl(book))

	if coverDownloadUrl(book) != "" {
		linkCover := entry.CreateElement("link")
		linkCover.CreateAttr("rel", "http://opds-spec.org/image")
		if book.CoverType == "image/jpeg" {
			linkCover.CreateAttr("type", "image/jpeg")
		} else if book.CoverType == "image/png" {
			linkCover.CreateAttr("type", "image/png")
		}
		linkCover.CreateAttr("href", coverDownloadUrl(book))
	}
}

func fullEntryOpds(book *Book, feed *etree.Element) {
	var authors []Author

	entry := feed.CreateElement("entry")

	id := entry.CreateElement("id")
	id.SetText(strconv.FormatInt(book.Id, 10))

	updatedAt := entry.CreateElement("updated_at")
	updatedAt.SetText(book.UpdatedAt.Format(time.RFC3339))

	title := entry.CreateElement("title")
	title.SetText(book.Title)

	db.Model(book).Related(&authors, "Authors")
	for _, author := range authors {
		authorTag := entry.CreateElement("author")
		name := authorTag.CreateElement("name")
		name.SetText(author.Name)
		uri := authorTag.CreateElement("uri")
		uri.SetText("/authors/" + strconv.FormatInt(author.Id, 10))
	}

	language := entry.CreateElement("dcterms:language")
	language.SetText(book.Language)

	summary := entry.CreateElement("summary")
	summary.CreateAttr("type", "text")
	summary.CreateCharData(book.Description)

	link := entry.CreateElement("link")
	link.CreateAttr("rel", "http://opds-spec.org/acquisition")
	link.CreateAttr("type", "application/epub+zip")
	link.CreateAttr("href", bookDownloadUrl(book))

	if coverDownloadUrl(book) != "" {
		linkCover := entry.CreateElement("link")
		linkCover.CreateAttr("rel", "http://opds-spec.org/image")
		if book.CoverType == "image/jpeg" {
			linkCover.CreateAttr("type", "image/jpeg")
		} else if book.CoverType == "image/png" {
			linkCover.CreateAttr("type", "image/png")
		}
		linkCover.CreateAttr("href", coverDownloadUrl(book))
	}
	/*
	  <dc:issued>1917</dc:issued>
	  <category scheme="http://www.bisg.org/standards/bisac_subject/index.html"
	            term="FIC020000"
	            label="FICTION / Men's Adventure"/>
	  <summary type="text">The story of the son of the Bob and the gallant part he played in
	    the lives of a man and a woman.</summary>
	  <link rel="http://opds-spec.org/image"
	        href="/covers/4561.lrg.png"
	        type="image/png"/>
	  <link rel="http://opds-spec.org/image/thumbnail"
	        href="/covers/4561.thmb.gif"
	        type="image/gif"/>

	  <link rel="alternate"
	        href="/opds-catalogs/entries/4571.complete.xml"
	        type="application/atom+xml;type=entry;profile=opds-catalog"
	        title="Complete Catalog Entry for Bob, Son of Bob"/>

	  <link rel="http://opds-spec.org/acquisition"
	        href="/content/free/4561.epub"
	        type="application/epub+zip"/>*/

}

func sync_opds(db gorm.DB) {
	var services []Service
	var nextUrl string
	var req *http.Request
	var reqUrl string

	db.Find(&services)
	// TODO: check last sync
	for _, service := range services {
		nextUrl = "first"
		reqUrl = ""

		for nextUrl != "" {
			client := &http.Client{}

			if nextUrl == "first" {
				reqUrl = service.Url
			} else {
				reqUrl = checkLink(nextUrl, service.Url)
			}

			fmt.Println("parsing " + reqUrl)
			req, _ = http.NewRequest("GET", reqUrl, nil)

			if service.Type == "basic_auth" {
				req.SetBasicAuth(service.Login, service.Password)
			}

			resp, err := client.Do(req)
			if err != nil {
				fmt.Println(err)
				return
			}

			body, _ := ioutil.ReadAll(resp.Body)
			fmt.Println(string(body))
			doc := etree.NewDocument()
			err = doc.ReadFromBytes(body)
			if err == nil {
				root := doc.SelectElement("feed")
				if root != nil {
					nextUrl = parseFeed(root, db, &service)
				}
			}
		}
	}

	fmt.Println("finish sync")
}

func parseFeed(feed *etree.Element, db gorm.DB, service *Service) string {
	var nextUrl string = ""

	links := feed.SelectElements("link")
	for _, link := range links {
		rel := link.SelectAttrValue("rel", "")
		//link_type := link.SelectAttrValue("type", "")
		href := link.SelectAttrValue("href", "")
		if rel == "next" {
			nextUrl = href
		}
	}

	for _, opds_book := range feed.SelectElements("entry") {
		book := Book{}
		Identifier := opds_book.SelectElement("id")

		db.Where(Book{OpdsIdentifier: Identifier.Text()}).FirstOrCreate(&book)
		getBookInfo(db, &book, opds_book, service.Url)
	}

	return nextUrl
}

func getBookInfo(db gorm.DB, book *Book, opds_book *etree.Element, baseUri string) {
	var fullEntry string
	var epubUrl string
	var coverUrl string
	var coverType string
	var authorDb Author
	var bookAuthor BookAuthor

	links := opds_book.SelectElements("link")
	for _, link := range links {
		rel := link.SelectAttrValue("rel", "")
		format_type := link.SelectAttrValue("type", "")
		if rel == "alternate" && format_type == "application/atom+xml;type=entry;profile=opds-catalog" {
			fullEntry = link.SelectAttrValue("href", "")
		}
		if rel == "http://opds-spec.org/acquisition" && format_type == "application/epub+zip" {
			epubUrl = link.SelectAttrValue("href", "")
		}
		if rel == "http://opds-spec.org/image" {
			coverUrl = link.SelectAttrValue("href", "")
			coverType = format_type
		}
	}

	title := opds_book.SelectElement("title")
	book.Title = title.Text()

	lang := opds_book.SelectElement("language")
	if lang != nil {
		book.Language = lang.Text()
	}

	book.ServiceDownloadUrl = epubUrl

	db.Save(book)

	if fullEntry != "" {
		parseFullEntry(db, book, opds_book, fullEntry, baseUri)
		//fmt.Printf("%+v\n", book)
		db.Save(book)
	} else {
		desc := opds_book.SelectElement("summary")
		if desc != nil {
			book.Description = html.UnescapeString(desc.Text())
		}

		if coverUrl != "" {
			book.CoverType = coverType
			book.CoverPath = downloadCover(checkLink(coverUrl, baseUri), coverType, book)
		}
		db.Save(book)
	}

	authors := opds_book.SelectElements("author")
	for _, author := range authors {
		authorDb = Author{}
		bookAuthor = BookAuthor{}
		nameElem := author.SelectElement("name")
		if nameElem != nil {
			db.Where(Author{Name: nameElem.Text()}).FirstOrCreate(&authorDb)
		}
		db.Where(BookAuthor{BookId: book.Id, AuthorId: authorDb.Id}).FirstOrCreate(&bookAuthor)
		bookAuthor.BookId = book.Id
		bookAuthor.AuthorId = authorDb.Id
		db.Save(&bookAuthor)
	}

	if epubUrl != "" {
		go downloadEpub(checkLink(epubUrl, baseUri), book)
	}
}

func downloadEpub(url string, book *Book) {
	fmt.Println("try to download " + url)

	bookIdStr := strconv.FormatInt(book.Id, 10)
	epubDirPath := "public/books/" + bookIdStr
	epubFilePath := epubDirPath + "/" + bookIdStr + ".epub"

	_, err := os.Open(epubFilePath)
	if os.IsNotExist(err) {
		client := &http.Client{}

		req, _ := http.NewRequest("GET", url, nil)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println(err)
			return
		}

		buff, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err)
			return
		}

		os.MkdirAll(epubDirPath, os.ModePerm)
		epubFile, err := os.Create(epubFilePath)
		if err != nil {
			fmt.Println(err)
			return
		}

		defer epubFile.Close()
		_, err = epubFile.Write(buff)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func downloadCover(url string, format string, book *Book) string {
	fmt.Println("try to download " + url)

	bookIdStr := strconv.FormatInt(book.Id, 10)
	coverDirPath := "public/books/" + bookIdStr
	coverFilePath := coverDirPath + "/" + bookIdStr
	if format == "image/jpeg" {
		coverFilePath = coverFilePath + ".jpg"
	} else if format == "image/png" {
		coverFilePath = coverFilePath + ".png"
	} else {
		fmt.Println("can't find ext for " + format)
	}

	_, err := os.Open(coverFilePath)
	if os.IsNotExist(err) {
		client := &http.Client{}

		req, _ := http.NewRequest("GET", url, nil)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println(err)
			return ""
		}

		buff, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err)
			return ""
		}

		os.MkdirAll(coverDirPath, os.ModePerm)
		coverFile, err := os.Create(coverFilePath)
		if err != nil {
			fmt.Println(err)
			return ""
		}

		defer coverFile.Close()
		_, err = coverFile.Write(buff)
		if err != nil {
			fmt.Println(err)
		}
	}
	return coverFilePath
}

func checkLink(uri string, baseUri string) string {

	parsedBaseUri, _ := url.Parse(baseUri)
	parsedUri, _ := url.Parse(uri)
	if parsedUri.IsAbs() {
		return uri
	} else {
		resultUri := parsedBaseUri.Scheme + "://" + parsedBaseUri.Host + uri
		return resultUri
	}
}

func bookDownloadUrl(book *Book) string {
	bookIdStr := strconv.FormatInt(book.Id, 10)
	epubDirPath := "/books/" + bookIdStr
	epubFilePath := epubDirPath + "/" + bookIdStr + ".epub"
	return epubFilePath
}

func coverDownloadUrl(book *Book) string {
	var coverFilePath string

	bookIdStr := strconv.FormatInt(book.Id, 10)
	coverDirPath := "/books/" + bookIdStr
	if book.CoverType == "image/jpeg" {
		coverFilePath = coverDirPath + "/" + bookIdStr + ".jpg"
	} else if book.CoverType == "image/png" {
		coverFilePath = coverDirPath + "/" + bookIdStr + ".png"
	}
	return coverFilePath
}

func parseFullEntry(db gorm.DB, book *Book, opds_book *etree.Element, fullEntry string, baseUri string) {
	var coverUrl string
	var coverType string

	client := &http.Client{}

	finalUrl := checkLink(fullEntry, baseUri)
	fmt.Println("parsing " + finalUrl)
	req, _ := http.NewRequest("GET", finalUrl, nil)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}

	body, _ := ioutil.ReadAll(resp.Body)
	//fmt.Println(string(body))
	doc := etree.NewDocument()
	err = doc.ReadFromBytes(body)
	if err == nil {
		root := doc.SelectElement("entry")
		if root != nil {
			content := root.SelectElement("content")
			if content != nil {
				book.Description = content.Text()
			}
			links := root.SelectElements("link")
			for _, link := range links {
				rel := link.SelectAttrValue("rel", "")
				format_type := link.SelectAttrValue("type", "")
				if rel == "http://opds-spec.org/image" {
					coverUrl = link.SelectAttrValue("href", "")
					coverType = format_type
				}
			}

			if coverUrl != "" {
				fmt.Println(coverUrl)
				book.CoverType = coverType
				book.CoverPath = downloadCover(checkLink(coverUrl, baseUri), coverType, book)
			}
		}
	}
}
