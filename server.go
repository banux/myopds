package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v2"

	auth "github.com/banux/negroni-auth"
	"github.com/beevik/etree"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pborman/uuid"
	"github.com/stretchr/graceful"
)

const jpgMediaType = "image/jpeg"
const pngMediaType = "image/png"
const atomExt = "atom"

// ServerOption store the option for the server
type ServerOption struct {
	gorm.Model
	UUID              string
	Password          string
	Name              string
	LastSync          time.Time `sql:"DEFAULT:current_timestamp"`
	Port              int       `sql:"DEFAULT:3000"`
	NumberBookPerPage int       `sql:"DEFAULT:50"`
}

// Service store sync information
type Service struct {
	gorm.Model
	URL          string
	Login        string
	Password     string
	RefreshToken string
	Type         string
}

// Page struct for page template displaying books
type Page struct {
	Title       string
	Content     interface{}
	NextPage    string
	PrevPage    string
	FirstPage   string
	LastPage    string
	FilterBlock bool
}

var db *gorm.DB
var layout *template.Template
var options ServerOption

var importDir = kingpin.Flag("import", "Import directory path").Short('i').String()
var serverMode = kingpin.Flag("server", "Server mode").Short('s').Bool()
var metaMode = kingpin.Flag("meta", "Regen all metada").Short('m').Bool()

//create another main() to run the overseer process
//and then convert your old main() into a 'prog(state)'
func main() {
	var serverOption ServerOption
	var books []Book
	var err error
	var version = "0.1"

	db, err = gorm.Open("sqlite3", "db/myopds.db")
	if err != nil {
		panic(err)
	}

	db.AutoMigrate(&ServerOption{}, &Service{}, &Book{}, &Author{}, &Tag{}, &ServerOption{}, &BookAuthor{}, &BookTag{})

	db.First(&serverOption)
	if serverOption.UUID == "" {
		serverOption.UUID = uuid.NewRandom().String()
	}
	if serverOption.Name == "" {
		serverOption.Name = "MyOPDS"
	}
	if serverOption.NumberBookPerPage == 0 {
		serverOption.NumberBookPerPage = 20
	}
	db.Save(&serverOption)
	options = serverOption

	kingpin.Version(version)
	kingpin.Parse()

	//go syncOpds(db)

	// Setup our service export
	//	host := "opds"
	//	info := []string{serverOption.Name}
	//	service, _ := mdns.NewMDNSService(host, "_opds._tcp", "", "", 3000, nil, info)
	//fmt.Println("%v", service)

	// Create the mDNS server, defer shutdown
	//	mdnsServer, _ := mdns.NewServer(&mdns.Config{Zone: service})
	//	defer mdnsServer.Shutdown()

	if *serverMode == true {

		currentPid := os.Getpid()
		pidFile, err := os.Create("/tmp/gopds.pid")
		if err != nil {
			panic("can't write pid file")
		}
		pidFile.WriteString(strconv.Itoa(currentPid))
		pidFile.Close()

		layout = template.Must(template.ParseFiles("template/layout.html"))

		routeur := mux.NewRouter()
		routeur.HandleFunc("/index.{format}", rootHandler)
		routeur.HandleFunc("/settings.html", settingsHandler)
		routeur.HandleFunc("/books/new.html", newBookHandler)
		routeur.HandleFunc("/books/{id}.{format}", bookHandler)
		routeur.HandleFunc("/books/{id}/delete", deleteBookHandler)
		routeur.HandleFunc("/books/{id}/edit", editBookHandler)
		routeur.HandleFunc("/books/{id}/favorite", favoriteBookHandler)
		routeur.HandleFunc("/books/{id}/readed", readedBookHandler)
		routeur.HandleFunc("/books/{id}/download", downloadBookHandler)
		routeur.HandleFunc("/books/{id}/refresh", refreshMetaBookHandler)
		routeur.HandleFunc("/tags_list.html", tagsListHandler)
		routeur.HandleFunc("/tags/{id}/delete", tagDelete)
		routeur.HandleFunc("/opensearch.xml", opensearchHandler)
		routeur.HandleFunc("/reindex", reindexHandler)
		routeur.HandleFunc("/search.{format}", searchHandler)
		routeur.HandleFunc("/books/changeTag", changeTagHandler)
		routeur.HandleFunc("/", redirectRootHandler)

		n := negroni.Classic()
		if options.Password != "" {
			n.Use(auth.Basic("opds", options.Password))
		}
		n.UseHandler(routeur)
		fmt.Println("launching server version " + version + " listening port " + strconv.Itoa(serverOption.Port))
		graceful.Run(":"+strconv.Itoa(serverOption.Port), 10*time.Second, n)
	}

	if *importDir != "" {
		files, _ := ioutil.ReadDir(*importDir)
		for _, f := range files {
			fmt.Println(f.Name())
			importFile(*importDir + "/" + f.Name())
		}
	}

	if *metaMode == true {

		db.Where("edited = 0").Find(&books)
		for _, book := range books {
			book.getMetada()
		}
	}

}

func redirectRootHandler(res http.ResponseWriter, req *http.Request) {
	http.Redirect(res, req, "/index.html", http.StatusMovedPermanently)
}

func rootHandler(res http.ResponseWriter, req *http.Request) {
	var books []Book
	var booksCount int
	var serverOption ServerOption
	var page string
	var pageInt = 1
	var offset int
	var nextLink string
	var prevLink string
	var firstLink string
	var lastLink string
	var bookTemplate *template.Template
	type JSONData struct {
		PrevLink string
		NextLink string
		LastPage int
		Books    []Book
	}

	baseDoc := etree.NewDocument()
	baseDoc.Indent(2)

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

			firstReq := req
			values = firstReq.URL.Query()
			values.Set("page", "1")
			firstReq.URL.RawQuery = values.Encode()
			firstLink = firstReq.URL.String()
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

	limit := serverOption.NumberBookPerPage
	offset = limit * (pageInt - 1)
	tag := req.URL.Query().Get("tag")
	author := req.URL.Query().Get("author")
	authorIDStr := req.URL.Query().Get("author_id")
	authorID, _ := strconv.Atoi(authorIDStr)
	order := req.URL.Query().Get("order")
	serie := req.URL.Query().Get("serie")
	filter := req.URL.Query().Get("filter")

	db.Limit(limit).Offset(offset).Scopes(BookwithCat(tag)).Scopes(BookwithAuthorID(authorID)).Scopes(BookwithAuthor(author)).Scopes(BookOrder(order)).Scopes(BookwithSerie(serie)).Scopes(BookFilter(filter)).Find(&books)

	db.Model(Book{}).Scopes(BookwithCat(tag)).Scopes(BookwithAuthorID(authorID)).Scopes(BookwithAuthor(author)).Scopes(BookwithSerie(serie)).Scopes(BookFilter(filter)).Count(&booksCount)
	if offset+limit > booksCount {
		nextLink = ""
	}
	lastPage := booksCount/limit + 1

	if lastPage != pageInt {
		lastPageStr := strconv.Itoa(lastPage)
		lastReq := req
		values := lastReq.URL.Query()
		values.Set("page", lastPageStr)
		lastReq.URL.RawQuery = values.Encode()
		lastLink = lastReq.URL.String()
	}

	vars := mux.Vars(req)

	if vars["format"] == atomExt {
		res.Header().Set("Content-Type", "application/atom+xml")
		feed := baseOpds(baseDoc, serverOption.UUID, serverOption.Name, booksCount, serverOption.NumberBookPerPage, offset+1, prevLink, nextLink)
		for _, book := range books {
			entryOpds(&book, feed)
		}

		linkFavorite := feed.CreateElement("link")
		linkFavorite.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		linkFavorite.CreateAttr("href", "/index.atom?filter=favorite")
		linkFavorite.CreateAttr("rel", "http://opds-spec.org/sort/popular")
		linkFavorite.CreateAttr("title", "Favori")

		tags := []string{"Roman", "Science-Fiction", "Fantasy", "Thriller", "Romance"}
		for _, tag := range tags {
			link := feed.CreateElement("link")
			link.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
			link.CreateAttr("href", "/index.atom?tag="+strings.Replace(tag, " ", "+", -1))
			link.CreateAttr("rel", "http://opds-spec.org/sort/popular")
			link.CreateAttr("title", tag)
		}
		xmlString, _ := baseDoc.WriteToString()
		fmt.Fprintf(res, xmlString)
	} else if vars["format"] == "json" {
		data := JSONData{PrevLink: prevLink, NextLink: nextLink}
		data.LastPage = lastPage + 1
		data.Books = make([]Book, len(books), len(books))
		for i, book := range books {
			data.Books[i] = book
		}
		page, _ := json.Marshal(data)
		fmt.Fprintf(res, string(page))
	} else {
		bookTemplate = template.Must(layout.Clone())
		bookTemplate = template.Must(bookTemplate.ParseFiles("template/bookcover.html"))
		err := bookTemplate.Execute(res, Page{
			PrevPage:    prevLink,
			NextPage:    nextLink,
			FirstPage:   firstLink,
			LastPage:    lastLink,
			Content:     books,
			FilterBlock: true,
			Title:       serverOption.Name,
		})
		if err != nil {
			panic(err)
		}
	}

}

// BookwithCat scope to get book with specific categories
func BookwithCat(category string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if category == "" {
			return db
		}
		return db.Joins("inner join book_tags on book_tags.book_id = books.id inner join tags on book_tags.tag_id = tags.id").Where("name = ?", category)
	}
}

// BookwithSerie scope to get book with specific serie and order it by serie position
func BookwithSerie(serie string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if serie == "" {
			return db
		}
		return db.Where("serie = ?", serie).Order("serie_number asc")
	}
}

// BookFilter scope to filter book
func BookFilter(filter string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filter == "" {
			return db
		}
		if filter == "favorite" {
			return db.Where("favorite = 1")
		}
		if filter == "notread" {
			return db.Where("read = 0")
		}
		if filter == "read" {
			return db.Where("read = 1")
		}
		return db
	}
}

// BookwithAuthor scope to get book with specific author
func BookwithAuthor(author string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if author == "" {
			return db
		}
		return db.Joins("inner join book_authors on book_authors.book_id = books.id inner join authors on book_authors.author_id = authors.id").Where("name = ?", author)
	}
}

// BookwithAuthorID scope to get book with specific author
func BookwithAuthorID(authorID int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if authorID == 0 {
			return db
		}
		return db.Joins("inner join book_authors on book_authors.book_id = books.id inner join authors on book_authors.author_id = authors.id").Where("authors.id = ?", authorID)
	}
}

// BookOrder scope to order book
func BookOrder(order string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if order == "new" {
			return db.Order("id desc")
		}
		if order == "old" {
			return db.Order("id asc")
		}
		return db.Order("id desc")
	}
}

func bookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book
	var bookTemplate *template.Template
	var serverOption ServerOption

	vars := mux.Vars(req)
	db.First(&serverOption)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)
	db.Preload("Authors").Preload("Tags").Find(&book, bookID)

	if vars["format"] == "html" {
		bookTemplate = template.Must(layout.Clone())
		bookTemplate = template.Must(bookTemplate.ParseFiles("template/book.html"))
		err := bookTemplate.Execute(res, Page{
			Content: book,
			Title:   serverOption.Name,
		})
		if err != nil {

		}
	}

	if vars["format"] == "atom" {
		res.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")

		baseDoc := etree.NewDocument()
		baseDoc.Indent(2)

		baseDoc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)

		feed := baseDoc.CreateElement("entry")

		fullEntryOpds(&book, feed, RootURL(req))
		xmlString, _ := baseDoc.WriteToString()
		fmt.Fprintf(res, xmlString)

	}

}

func baseOpds(doc *etree.Document, uuid string, name string, totalResult int, perPage int, offset int, prevLink string, nextLink string) *etree.Element {
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
	authorName := author.CreateElement("name")
	authorName.SetText("MyOPDS")
	authorURI := author.CreateElement("uri")
	authorURI.SetText("http://www.myopds.com")

	if totalResult > 0 {
		totalResultXML := feed.CreateElement("opensearch:totalResults")
		totalResultText = strconv.Itoa(totalResult)
		totalResultXML.SetText(totalResultText)
	}
	if perPage > 0 {
		perPageXML := feed.CreateElement("opensearch:itemsPerPage")
		perPageText = strconv.Itoa(perPage)
		perPageXML.SetText(perPageText)
	}
	if offset > 1 {
		offsetXML := feed.CreateElement("opensearch:startIndex")
		offsetText = strconv.Itoa(offset)
		offsetXML.SetText(offsetText)
	}

	if prevLink != "" {
		prevLinkXML := feed.CreateElement("link")
		prevLinkXML.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		prevLinkXML.CreateAttr("title", "Previous")
		prevLinkXML.CreateAttr("href", prevLink)
		prevLinkXML.CreateAttr("rel", "previous")
	}

	if nextLink != "" {
		nextLinkXML := feed.CreateElement("link")
		nextLinkXML.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		nextLinkXML.CreateAttr("title", "Next")
		nextLinkXML.CreateAttr("href", nextLink)
		nextLinkXML.CreateAttr("rel", "next")
	}

	linkSearch := feed.CreateElement("link")
	linkSearch.CreateAttr("type", "application/opensearchdescription+xml")
	linkSearch.CreateAttr("href", "/opensearch.xml")
	linkSearch.CreateAttr("rel", "search")

	return feed
}

func entryOpds(book *Book, feed *etree.Element) {
	var authors []Author

	entry := feed.CreateElement("entry")

	id := entry.CreateElement("id")
	id.SetText(strconv.Itoa(int(book.ID)))

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
		uri.SetText("/authors/" + strconv.Itoa(int(author.ID)))
	}

	language := entry.CreateElement("dcterms:language")
	language.SetText(book.Language)

	summary := entry.CreateElement("summary")
	summary.CreateAttr("type", "text")
	summary.CreateCharData(book.Description)

	link := entry.CreateElement("link")
	link.CreateAttr("rel", "http://opds-spec.org/acquisition/open-access")
	link.CreateAttr("type", "application/epub+zip")
	link.CreateAttr("href", book.DownloadURL())

	if book.CoverDownloadURL() != "" {
		linkCover := entry.CreateElement("link")
		linkCover.CreateAttr("rel", "http://opds-spec.org/image")
		if book.CoverType == jpgMediaType {
			linkCover.CreateAttr("type", jpgMediaType)
		} else if book.CoverType == pngMediaType {
			linkCover.CreateAttr("type", pngMediaType)
		}
		linkCover.CreateAttr("href", book.CoverDownloadURL())
	}

	linkFull := entry.CreateElement("link")
	linkFull.CreateAttr("rel", "alternate")
	linkFull.CreateAttr("href", "/books/"+strconv.Itoa(int(book.ID))+".atom")
	linkFull.CreateAttr("type", "application/atom+xml;type=entry;profile=opds-catalog")
	linkFull.CreateAttr("tile", "Full entry")

}

func fullEntryOpds(book *Book, feed *etree.Element, baseURL string) {
	var authors []Author

	entry := feed

	feed.CreateAttr("xml:lang", "fr")
	feed.CreateAttr("xmlns:dcterms", "http://purl.org/dc/terms/")
	feed.CreateAttr("xmlns:thr", "http://purl.org/syndication/thread/1.0")
	feed.CreateAttr("xmlns:opds", "http://opds-spec.org/2010/catalog")
	feed.CreateAttr("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance")
	feed.CreateAttr("xmlns:app", "http://www.w3.org/2007/app")
	feed.CreateAttr("xmlns", "http://www.w3.org/2005/Atom")
	feed.CreateAttr("xmlns:opensearch", "http://a9.com/-/spec/opensearch/1.1/")

	id := entry.CreateElement("id")
	id.SetText(strconv.Itoa(int(book.ID)))

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
		uri.SetText("/authors/" + strconv.Itoa(int(author.ID)))
	}

	if book.Language != "" {
		language := entry.CreateElement("dcterms:language")
		language.SetText(book.Language)
	}

	summary := entry.CreateElement("summary")
	summary.CreateAttr("type", "text")
	summary.CreateCharData(book.Description)

	for _, cat := range book.Tags {
		catElem := entry.CreateElement("category")
		catElem.CreateAttr("scheme", "http://myopds.com/tags")
		catElem.CreateAttr("label", cat.Name)
		catElem.CreateAttr("term", cat.Name)
	}

	link := entry.CreateElement("link")
	link.CreateAttr("rel", "http://opds-spec.org/acquisition/open-access")
	link.CreateAttr("type", "application/epub+zip")
	link.CreateAttr("href", baseURL+book.DownloadURL())

	if book.CoverDownloadURL() != "" {
		linkCover := entry.CreateElement("link")
		linkCover.CreateAttr("rel", "http://opds-spec.org/image")
		if book.CoverType == "image/jpeg" {
			linkCover.CreateAttr("type", "image/jpeg")
		} else if book.CoverType == "image/png" {
			linkCover.CreateAttr("type", "image/png")
		}
		linkCover.CreateAttr("href", baseURL+book.CoverDownloadURL())
	}

	if book.Serie != "" {
		serieElem := entry.CreateElement("link")
		serieElem.CreateAttr("rel", "related")
		serieElem.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		serieElem.CreateAttr("href", baseURL+"/index.atom?serie="+strings.Replace(book.Serie, " ", "+", -1))
		serieElem.CreateAttr("title", book.Serie)
	}

	for _, author := range book.Authors {
		linkAuthor := entry.CreateElement("link")
		linkAuthor.CreateAttr("rel", "http://www.feedbooks.com/opds/same_author")
		linkAuthor.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		linkAuthor.CreateAttr("href", baseURL+"/index.atom?author_id="+strconv.Itoa(int(author.ID)))
		linkAuthor.CreateAttr("title", author.Name)
	}

	for _, tag := range book.Tags {
		linkTag := entry.CreateElement("link")
		linkTag.CreateAttr("rel", "related")
		linkTag.CreateAttr("type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
		linkTag.CreateAttr("href", baseURL+"/index.atom?tag="+strings.Replace(tag.Name, " ", "+", -1))
		linkTag.CreateAttr("title", tag.Name)
	}

}

func opensearchHandler(res http.ResponseWriter, req *http.Request) {
	var xmlString string

	res.Header().Set("Content-Type", "application/xml; charset=utf-8")

	baseDoc := etree.NewDocument()
	baseDoc.Indent(2)

	opensearch := baseDoc.CreateElement("OpenSearchDescription")
	opensearch.CreateAttr("xmlns", "http://a9.com/-/spec/opensearch/1.1/")

	shortName := opensearch.CreateElement("ShortName")
	shortName.SetText("Opds Search")

	descripton := opensearch.CreateElement("Description")
	descripton.SetText("Search")

	inputEncoding := opensearch.CreateElement("InputEncoding")
	inputEncoding.SetText("UTF-8")

	outputEncoding := opensearch.CreateElement("OutputEncoding")
	outputEncoding.SetText("UTF-8")

	htmlURL := opensearch.CreateElement("Url")
	htmlURL.CreateAttr("type", "text/html")
	htmlURL.CreateAttr("template", RootURL(req)+"/search.html?query={searchTerms}")

	atomURL := opensearch.CreateElement("Url")
	atomURL.CreateAttr("type", "application/atom+xml")
	atomURL.CreateAttr("template", RootURL(req)+"/search.atom?query={searchTerms}")

	// <Url type="application/x-suggestions+json" rel="suggestions" template="http://www.feedbooks.com/search.json?query={searchTerms}"/>
	// <Url type="application/x-suggestions+xml" rel="suggestions" template="http://www.feedbooks.com/suggest.xml?query={searchTerms}"/>

	query := opensearch.CreateElement("Query")
	query.CreateAttr("role", "example")
	query.CreateAttr("searchTerms", "robot")

	xmlString, _ = baseDoc.WriteToString()

	fmt.Fprintf(res, xmlString)
}

func searchHandler(res http.ResponseWriter, req *http.Request) {
	var xmlString string
	var bookTemplate *template.Template
	var serverOption ServerOption

	db.First(&serverOption)
	search := req.URL.Query().Get("query")
	books := findBookBySearch(search)

	vars := mux.Vars(req)

	if vars["format"] == "atom" {
		res.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")

		baseDoc := etree.NewDocument()
		baseDoc.Indent(2)

		feed := baseOpds(baseDoc, RootURL(req)+"/search.atom", search, len(books), len(books), 0, "", "")

		for _, book := range books {
			entryOpds(&book, feed)
		}

		xmlString, _ = baseDoc.WriteToString()

		fmt.Fprintf(res, xmlString)
	} else {
		bookTemplate = template.Must(layout.Clone())
		bookTemplate = template.Must(bookTemplate.ParseFiles("template/bookcover.html"))
		err := bookTemplate.Execute(res, Page{
			Content: books,
			Title:   serverOption.Name,
			//			FilterBlock: true,
		})
		if err != nil {
			panic(err)
		}
	}

}

// RootURL return url with absolute path
func RootURL(req *http.Request) string {
	return "http://" + req.Host
}

func changeTagHandler(res http.ResponseWriter, req *http.Request) {

	// action := req.URL.Query().Get("action")
	// tag := req.URL.Query().Get("tag")
	// bookID := req.URL.Query().Get("id")

}

func uploadBookForm(res http.ResponseWriter, req *http.Request) {
}

func deleteBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book

	vars := mux.Vars(req)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Find(&book, bookID)

	if book.ID != 0 {
		db.Delete(&book)
	}
	http.Redirect(res, req, "/index.html", http.StatusTemporaryRedirect)
}

func favoriteBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book

	vars := mux.Vars(req)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Find(&book, bookID)

	if book.ID != 0 {
		if book.Favorite == false {
			book.Favorite = true
		} else {
			book.Favorite = false
		}

		db.Save(&book)
	}
	http.Redirect(res, req, "/books/"+vars["id"]+".html", http.StatusTemporaryRedirect)
}

func refreshMetaBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book

	vars := mux.Vars(req)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Find(&book, bookID)

	if book.ID != 0 {
		book.getMetada()
	}
	http.Redirect(res, req, "/books/"+vars["id"]+".html", http.StatusTemporaryRedirect)
}

func readedBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book

	vars := mux.Vars(req)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Find(&book, bookID)

	if book.ID != 0 {
		if book.Read == false {
			book.Read = true
		} else {
			book.Read = false
		}

		db.Save(&book)
	}
	http.Redirect(res, req, "/books/"+vars["id"]+".html", http.StatusTemporaryRedirect)
}

func downloadBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book

	vars := mux.Vars(req)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Find(&book, bookID)

	f, _ := os.Open(book.FilePath())

	res.Header().Set("Content-Disposition", "attachment; filename=\""+book.Title+".epub\"")
	http.ServeContent(res, req, book.Title+".epub", book.UpdatedAt, f)
}

func editBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book
	var bookTemplate *template.Template
	var tagsObjs []Tag
	var tagObj Tag
	var authorStruct Author
	var authors []Author
	var serverOption ServerOption

	vars := mux.Vars(req)
	db.First(&serverOption)

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Preload("Authors").Find(&book, bookID)

	if req.Method == http.MethodPost {
		book.Title = req.FormValue("title")
		book.Description = req.FormValue("description")
		book.Isbn = req.FormValue("isbn")
		book.Publisher = req.FormValue("publisher")
		book.Collection = req.FormValue("collection")
		book.Serie = req.FormValue("serie")
		num := req.FormValue("serie_number")
		numF, errF := strconv.ParseFloat(num, 32)
		if errF == nil && numF != 0 {
			book.SerieNumber = float32(numF)
		}

		db.Unscoped().Where("book_id = ?", book.ID).Delete(BookTag{})
		tags := strings.Split(req.FormValue("tags"), ",")
		for _, tag := range tags {
			tagObj = Tag{}
			db.FirstOrCreate(&tagObj, Tag{Name: tag})
			tagsObjs = append(tagsObjs, tagObj)
		}
		book.Tags = tagsObjs

		author := req.FormValue("author")
		fmt.Println("author " + author)
		if author != "" {
			db.Where("name = ? ", author).Find(&authorStruct)
			if authorStruct.ID == 0 {
				authorStruct.Name = author
				db.Save(&authorStruct)
			}
			authors = append(authors, authorStruct)
		}
		db.Model(&book).Association("Authors").Clear()
		book.Authors = authors

		db.Save(&book)
		http.Redirect(res, req, "/books/"+vars["id"]+".html", http.StatusTemporaryRedirect)
	} else {
		bookTemplate = template.Must(layout.Clone())
		bookTemplate = template.Must(bookTemplate.ParseFiles("template/book_edit.html"))
		bookTemplate.Execute(res, Page{
			Content: book,
			Title:   serverOption.Name,
		})
	}

}

func newBookHandler(res http.ResponseWriter, req *http.Request) {
	var bookTemplate *template.Template
	var serverOption ServerOption

	db.First(&serverOption)
	if req.Method == http.MethodPost {
		infile, header, err := req.FormFile("book")
		if err != nil {
			http.Error(res, "Error parsing uploaded file: "+err.Error(), http.StatusBadRequest)
			return
		}

		outfile, err := os.Create("/tmp/" + header.Filename)
		if err != nil {
			http.Error(res, "Error saving file: "+err.Error(), http.StatusBadRequest)
			return
		}

		_, err = io.Copy(outfile, infile)
		if err != nil {
			http.Error(res, "Error saving file: "+err.Error(), http.StatusBadRequest)
			return
		}
		outfile.Close()

		book := importFile("/tmp/" + header.Filename)

		idStr := strconv.Itoa(int(book.ID))
		res.Header().Set("Location", "/books/"+idStr+".html")
		res.WriteHeader(302)
	} else {
		bookTemplate = template.Must(layout.Clone())
		bookTemplate = template.Must(bookTemplate.ParseFiles("template/book_new.html"))
		bookTemplate.Execute(res, Page{Title: serverOption.Name})
	}

}

func importFile(filePath string) Book {
	var book Book

	book.Edited = false
	db.Save(&book)

	moveEpub(filePath, &book)
	book.getMetada()
	return book
}

func moveEpub(filepath string, book *Book) {

	bookIDStr := strconv.Itoa(int(book.ID))
	epubDirPath := "public/books/" + bookIDStr
	epubFilePath := epubDirPath + "/" + bookIDStr + ".epub"

	_, err := os.Open(epubFilePath)
	if os.IsNotExist(err) {

		os.MkdirAll(epubDirPath, os.ModePerm)
		infile, err := os.Open(filepath)
		if err != nil {
			return
		}
		outfile, err := os.Create(epubFilePath)
		if err != nil {
			// http.Error(res, "Error saving file: "+err.Error(), http.StatusBadRequest)
			return
		}

		_, err = io.Copy(outfile, infile)
		if err != nil {
			// http.Error(res, "Error saving file: "+err.Error(), http.StatusBadRequest)
			return
		}
		outfile.Close()

	}
}

func tagsListHandler(res http.ResponseWriter, req *http.Request) {
	var tags []Tag
	var serverOption ServerOption

	db.First(&serverOption)
	db.Order("name asc").Find(&tags)

	tagsTemplate := template.Must(layout.Clone())
	tagsTemplate = template.Must(tagsTemplate.ParseFiles("template/tags_list.html"))
	tagsTemplate.Execute(res, Page{Content: tags, Title: serverOption.Name})

}

func settingsHandler(res http.ResponseWriter, req *http.Request) {
	var serverOption ServerOption

	db.First(&serverOption)

	if req.Method == http.MethodPost {

		serverOption.Name = req.FormValue("name")
		perPage, err := strconv.Atoi(req.FormValue("per_page"))
		if err == nil {
			serverOption.NumberBookPerPage = perPage
		}
		port, err := strconv.Atoi(req.FormValue("port"))
		if err == nil {
			serverOption.Port = port
		}
		serverOption.Password = req.FormValue("password")

		db.Save(&serverOption)
		res.Header().Set("Location", "/index.html")
		res.WriteHeader(302)
	} else {

		settingTemplate := template.Must(layout.Clone())
		settingTemplate = template.Must(settingTemplate.ParseFiles("template/settings.html"))
		settingTemplate.Execute(res, Page{Content: serverOption, Title: serverOption.Name})
	}

}

func escape(s string) string {
	return strings.Replace(url.QueryEscape(s), "+", "%20", -1)
}

func tagDelete(res http.ResponseWriter, req *http.Request) {
	var tag Tag

	vars := mux.Vars(req)
	tagID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.First(&tag, tagID)

	if tag.ID != 0 {
		db.Delete(&tag)
	}
	http.Redirect(res, req, "/tags_list.html", http.StatusTemporaryRedirect)
}

func reindexHandler(res http.ResponseWriter, req *http.Request) {

	go indexAll()
	http.Redirect(res, req, "/", http.StatusTemporaryRedirect)

}
