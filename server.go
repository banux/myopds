package main

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v2"

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
	Token             string
	Name              string
	BaseURL           string
	LastSync          time.Time `sql:"DEFAULT:current_timestamp"`
	Port              int       `sql:"DEFAULT:3000"`
	NumberBookPerPage int       `sql:"DEFAULT:50"`
}

var options ServerOption

//create another main() to run the overseer process
//and then convert your old main() into a 'prog(state)'
func main() {
	var serverOption ServerOption
	var err error
	var version = "2.0"

	db, errDb := gorm.Open("sqlite3", "db/myopds.db")
	if errDb != nil {
		panic(err)
	}

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

	// Setup our service export
	//	host := "opds"
	//	info := []string{serverOption.Name}
	//	service, _ := mdns.NewMDNSService(host, "_opds._tcp", "", "", 3000, nil, info)
	//fmt.Println("%v", service)

	// Create the mDNS server, defer shutdown
	//	mdnsServer, _ := mdns.NewServer(&mdns.Config{Zone: service})
	//	defer mdnsServer.Shutdown()

	//	h := &handler.Handler{DB: db}

	graceful.Run(":"+strconv.Itoa(serverOption.Port), 10*time.Second, n)

}

// RootURL return url with absolute path
func RootURL(req *http.Request) string {
	var option ServerOption

	db.First(&option)

	if option.BaseURL != "" {
		return option.BaseURL
	} else {
		return "http://" + req.Host
	}
}

func downloadBookHandler(res http.ResponseWriter, req *http.Request) {
	var book Book
	var serverOption ServerOption

	db.First(&serverOption)
	vars := mux.Vars(req)

	if serverOption.Password != "" && (vars["format"] == "html") {
		if !checkAuth(req) {
			res.Header().Set("Location", "/login.html")
			res.WriteHeader(302)
			return
		}
	}

	bookID, _ := strconv.ParseInt(vars["id"], 10, 64)

	db.Find(&book, bookID)

	f, _ := os.Open(book.FilePath())

	res.Header().Set("Content-Disposition", "attachment; filename=\""+book.Title+".epub\"")
	http.ServeContent(res, req, book.Title+".epub", book.UpdatedAt, f)
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

func escape(s string) string {
	return strings.Replace(url.QueryEscape(s), "+", "%20", -1)
}
