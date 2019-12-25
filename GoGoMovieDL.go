package main

import (
	"encoding/csv"
	"encoding/xml"
	"errors"
	"log"
	"net/http"

	"database/sql"

	"github.com/jasonlvhit/gocron"
	"github.com/pelletier/go-toml"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/rogpeppe/go-charset/charset"
	_ "github.com/rogpeppe/go-charset/data"
)

var (
	MYAPIKEY         string  //NZBGeek API
	MYSABURL         string  //SABNZBD URL
	MYSABAPI         string  //SABNZBD API Key
	MYSABCAT         string  //SABNZBD Category
	MYRSS2FEEDURL    string  //RSS2 Watchlist URL
	MYRSSCHECK       int64   //IMDB Watchlist Check Interval in minutes, recommend 120
	MYMOVIECHECK     int64   //Specific Movie Check Interval in minutes, recommend 400
	MYMOVIESCHECK    int64   //Recent Movies Check Interval in minutes, recommend 16 mins
	MYPREFERREDWORDS string  //Preferred words list, comma separated, increase score
	MYBANNEDWORDS    string  //Banned words list, comma separated, kill score
	db               *sql.DB //Global DB Handle
)

type RSS2 struct {
	//	XMLName xml.Name `xml:"rss"`
	Version string `xml:"version,attr"`
	Items   []Item `xml:"channel>item"`
}

type Item struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
}

func main() {
	var err error

	//Log to file
	log.SetOutput(&lumberjack.Logger{
		Filename: "./GoGoMovieDL.log",
		MaxSize:  32, //MB
	})
	log.Println("==============================================")
	log.Println("==============================================")
	log.Println("==============================================")

	//read global settings from file
	ReadConfig()

	//initialise database, create if not already created etc.
	err = InitDB()
	defer db.Close()
	if err != nil {
		log.Panic("Main:InitDB", err)
	}

	//Start webserver in another channel, in case templates fail
	InitWebServer()

	//Update Scores to support possible config preferred/bad changes
	UpdateNZBScores()

	//set up timed jobs
	//will run every x minutes, as defined in file

	gocron.Every(uint64(MYRSSCHECK)).Minutes().Do(RSS2WatchlistUpdate)
	gocron.Every(uint64(MYMOVIESCHECK)).Minutes().Do(MostRecentMovieList)
	gocron.Every(uint64(MYMOVIECHECK)).Minutes().Do(UnGrabbedMovies)

	gocron.Every(2).Minutes().Do(DownloadGrabbableMovies)

	//Also run on load

	RSS2WatchlistUpdate()
	MostRecentMovieList()
	DownloadGrabbableMovies()

	//Start Cronjobs
	<-gocron.Start()
}

func CSV2Feed(URL string) (*RSS2, error) {
	var Newitem Item

	resp, err := http.Get(URL)
	if err != nil {
		log.Println("CSV2Feed:HTTPGET", err)
		return nil, err
	}

	defer resp.Body.Close()
	reader := csv.NewReader(resp.Body)
	reader.Comma = ','
	data, err := reader.ReadAll()
	if err != nil {
		log.Println("CSV2Feed:csvReader", err)
		return nil, err
	}

	Newrss2 := new(RSS2)
	for idx, row := range data {

		//skip header
		if idx == 0 {
			continue
		}

		Newitem.Link = row[6]
		Newitem.PubDate = row[2]
		Newitem.Title = row[5]
		Newrss2.Items = append(Newrss2.Items, Newitem)
	}

	return Newrss2, nil
}

// main function to get IMDB RSS watchlist
// and return NZBGRSS structure
func RSS2Feed(URL string) (*RSS2, error) {

	r, err := http.Get(URL)
	if err != nil {
		log.Println("RSS2Feed:HTTPGET", err)
		return nil, err
	}

	if r.StatusCode != 200 {
		log.Println("RSS2Feed:HTTPRESPONSE=", r.StatusCode)
		return nil, errors.New("Couldn't get rss feed, check url and check not private / no auth required.")
	}

	log.Printf("%+v", r)

	iz := new(RSS2)
	decoder := xml.NewDecoder(r.Body)
	decoder.CharsetReader = charset.NewReader
	err = decoder.Decode(&iz)
	if err != nil {
		log.Println("RSS2Feed:UNMARSHAL", err)
		return nil, err
	}

	return iz, nil
}

// Get the feed from the MYRSS2FEEDURL and
// update the database
func RSS2WatchlistUpdate() {
	log.Println("RSS2WatchlistUpdate")
	iv, err := CSV2Feed(MYRSS2FEEDURL)
	if err != nil {
		log.Println("Main:RSS2WatchlistUpdate:RSS2Feed:", err)
		return
	}
	added := RSS2toDB(iv)
	removed := DBRemoveMissingRSS2(iv)
	log.Printf("RSS2WatchlistUpdate:End:%d added, %d removed", added, removed)
}

// Get the latest movie list and if there are files
// for movies we have then add them
func MostRecentMovieList() {
	//LATEST MOVIES
	log.Println("Main:MostRecentMovieList:Begin")
	nz, err := NZBGeekMovies(MYAPIKEY)
	if err != nil {
		log.Println("Main:MostRecentMovieList:NZBGeekMovies", err)
	} else {
		count := NZBGRSStoDB(nz)
		log.Printf("Main:MostRecentMovieList:End:%d added", count)
	}
}

// Only meant to be run rarely (4 times daily max) - this will scroll all our
// ungrabbed movies and will see if there are any files available
func UnGrabbedMovies() {
	var (
		id  int64
		ids []int64
	)
	log.Println("Main:UnGrabbedMovies:Begin")
	rows, err := db.Query(`
		PRAGMA read_uncommitted = 1;
		select distinct id from movies where grabbed=0
	`)
	if err != nil {
		log.Println("Main:UnGrabbedMovies:Query", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id)
		if err != nil {
			log.Println("Main:UnGrabbedMovies:RowScan", err)
		}
		ids = append(ids, id)
	}

	for _, id := range ids {
		nz, err := NZBGeekMovieByIMDB(id, MYAPIKEY)
		if err != nil {
			log.Println("UngrabbedMovies:GetByID", err)
		} else {
			NZBGRSStoDB(nz)
		}
	}
	log.Println("Main:UnGrabbedMovies:End")
}

// Download teh ungrabbed movies that have files attached
func DownloadGrabbableMovies() {
	//Parse History First to remove complete and allow us to get next if failed
	SABParseHistory()
	//Look for non grabbed nzbs with score>0 and not ignored or grabbed
	gb := GrabbableList()
	for _, gbb := range gb {
		SABGrabAndMark(gbb.Id, gbb.MovieId)
	}
}

func ReadConfig() {
	log.Println("Main:ReadConfig:Begin")
	config, err := toml.LoadFile("GoGoMovieDL.conf")
	if err != nil {
		log.Panic("ReadConfig:", err)
	} else {
		MYAPIKEY = config.Get("MYAPIKEY").(string)
		MYSABURL = ReturnNiceSABURL(config.Get("MYSABURL").(string))
		MYSABAPI = config.Get("MYSABAPI").(string)
		MYSABCAT = config.Get("MYSABCAT").(string)
		MYRSS2FEEDURL = config.Get("MYRSS2FEEDURL").(string)
		MYBANNEDWORDS = config.Get("MYBANNEDWORDS").(string)
		MYPREFERREDWORDS = config.Get("MYPREFERREDWORDS").(string)

		//don't want to check any sooner than every 10 mins
		MYRSSCHECK = config.Get("MYRSSCHECK").(int64)
		if MYRSSCHECK < 10 {
			MYRSSCHECK = 10
		}

		//don't want to check any sooner than every 120 mins
		MYMOVIECHECK = config.Get("MYMOVIECHECK").(int64)
		if MYMOVIECHECK < 120 {
			MYMOVIECHECK = 120
		}

		MYMOVIESCHECK = config.Get("MYMOVIESCHECK").(int64)
		if MYMOVIESCHECK < 15 {
			MYMOVIESCHECK = 15
		}

	}
	log.Println("Main:ReadConfig:End")
}
