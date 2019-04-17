//dbstuff.go
package main

import (
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"database/sql"

	"github.com/mattn/go-sqlite3"
)

type Movie struct {
	Id          int64
	Title       string
	CoverUrl    string
	Grabbed     int
	MovieUrl    string
	NzbCount    int
	IgnoreCount int
	Orderfield  int
}

type NZB struct {
	Id         string
	MovieId    int64
	MovieName  string
	Title      string
	Link       string
	Score      float64
	Size       float64
	Grabs      int
	UsenetDate time.Time
	Grabbed    int
	Ignored    int
	GrabURL    string
}

type Downloads struct {
	MovieID  int64
	Nicename string
	Guid     string
	DlId     string
}

type Grabbable struct {
	MovieId    int64
	MovieTitle string
	Id         string
	Link       string
}

func InitDB() (err error) {

	db, err = sql.Open("sqlite3", "./GoGoMovieDL.db?cache=shared&_busy_timeout=500")
	if err != nil {
		log.Panic("Main:InitDB:", err)
	}

	sqlStmt := `

	create table if not exists movies(
		id integer not null primary key, 
		title text,
		coverurl text,
		grabbed integer
	);
	
	create table if not exists nzbs(
		id text not null primary key, 
		movieid integer not null,
		title text,
		link text,
		score real,
		size real,
		grabs integer,
		usenetdate date,
		grabbed integer,
		ignored integer
	);
	
	create table if not exists downloads(
		id int primary key,
		guid text not null,
		dlmethod text not null,
		dlid text not null,
		percentage int,
		status int
	);
	`

	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}

//scroll through the dataset and update the scores
func UpdateNZBScores() {
	var title string
	var id string
	var nzbsize float64
	var usenetdate time.Time
	var score float64

	log.Println("UpdateNZBScores:Begin")

	updatestmt, err := db.Prepare("UPDATE nzbs SET score=? WHERE id=?")
	if err != nil {
		log.Println("UpdateNZBScores:PrepareStmt", err)
		return
	}
	rows, err := db.Query("select id,title,size,usenetdate from nzbs")
	if err != nil {
		log.Println("UpdateNZBScores:Query", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &title, &nzbsize, &usenetdate)
		if err != nil {
			log.Println("UpdateNZBScores:RowScan", err)
		} else {
			score = GetScore(title, usenetdate, nzbsize)
			// update record in db with new score, fail and return if error
			// we can always try again later.
			_, err := updatestmt.Exec(score, id)
			if err != nil {
				log.Println("UpdateNZBScores:UpdateScore", err)
				return
			}
		}
	}
	log.Println("UpdateNZBScores:End")
}

func UpdateCoverURL(id int64, coverurl string) {
	_, err := db.Exec("UPDATE movies SET coverurl=? WHERE id=?", coverurl, id)
	if err != nil {
		log.Println("UpdateCoverURL:", err)
	}
}

// loop the NZBGRSS struct and
// if the movie exists we add the
// file to the releases table using
// the returned id
func NZBGRSStoDB(nz *NZBGRSS) (count int) {
	var id int64
	var grabs int
	var size float64
	var coverurl string
	var guid string
	var score float64
	var usenetdate string
	var usenetdt time.Time

	stmt, err := db.Prepare("INSERT INTO nzbs(id, movieid, title, link, score, size, grabs, grabbed, ignored, usenetdate) VALUES(?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		log.Println("NZBGRSStoDB:PrepareStmt", err)
		return 0
	}

	for _, mv := range nz.Channels.NZBGItems {

		//reset inner loop vars
		id = 0
		usenetdate = ""
		grabs = 0
		size = 0
		coverurl = ""
		guid = ""

		for _, nza := range mv.NZAttribs {

			//log.Printf("%s = %s\n", nza.Name, nza.Value)

			switch {

			case strings.ToLower(nza.Name) == "grabs":
				grabs, _ = strconv.Atoi(nza.Value)

			case strings.ToLower(nza.Name) == "size":
				size, _ = strconv.ParseFloat(nza.Value, 64)
				size = size / (1024 * 1024 * 1024)

			case strings.ToLower(nza.Name) == "coverurl":
				coverurl = nza.Value

			case strings.ToLower(nza.Name) == "guid":
				guid = nza.Value

			case strings.ToLower(nza.Name) == "usenetdate":
				usenetdate = nza.Value

			case strings.ToLower(nza.Name) == "imdb":
				id, _ = strconv.ParseInt(nza.Value, 10, 64)
			}
		}

		//if we can find imdbid in our database then we add the file to the list, and calculate
		//some kind of score based on text in like and hate lists

		if RSSIDExistsInDB(id) {
			usenetdt, _ = time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", usenetdate)
			score = GetScore(mv.Title, usenetdt, size)
			_, err := stmt.Exec(guid, id, mv.Title, mv.Link, score, size, grabs, 0, 0, usenetdt.Format(time.RFC3339))
			if err != nil {
				sqlerr := err.(sqlite3.Error)
				if sqlerr.Code != sqlite3.ErrConstraint {
					log.Printf("NZBGRSStoDB:ExecInsert:%d %s", sqlerr.Code, sqlerr.Error())
				}
			} else {
				UpdateCoverURL(id, coverurl)
				log.Printf("Found NZB id %s for %d %s with score %.0f", guid, id, mv.Title, score)
				count += 1
			}

		}

	}
	return count
}

//returns count of comma separated words in instring
func WordsInString(words string, instring string) (count int) {
	splitwords := strings.Split(words, ",")
	for _, word := range splitwords {
		if strings.Contains(strings.ToLower(instring), strings.ToLower(word)) {
			count += 1
		}
	}
	return count
}

// check if id exists in db and return true
// or return false if id is zero or doesn't exist
func RSSIDExistsInDB(id int64) bool {
	var title string
	if id == 0 {
		return false
	}
	err := db.QueryRow("SELECT title FROM movies where id=?", id).Scan(&title)
	switch {
	case err == sql.ErrNoRows:
		return false
	case err != nil:
		log.Println("RSSIDExistsInDB:", err)
		return false
	default:
		return true
	}
}

func TTtoID(tturl string) (id int64, err error) {
	tt := strings.Split(tturl, "/")
	for _, urlpart := range tt {
		if strings.HasPrefix(strings.ToLower(urlpart), "tt") {
			id, err = strconv.ParseInt(urlpart[2:], 10, 64)
			if err != nil {
				return -1, err
			}
			break //leave now we've found tt
		}
	}
	return id, nil
}

// delete items from db if doesn't exist in rs
func DBRemoveMissingRSS2(rs *RSS2) (count int) {
	//make map from rs
	rsItems := make(map[int64]bool)
	for _, mv := range rs.Items {
		id, err := TTtoID(mv.Link)
		if err != nil {
			//do nothing
		} else {
			rsItems[id] = true
		}
	}

	if len(rsItems) > 0 {
		//get list of movies from db
		movs := MoviesList(0)
		for _, mov := range movs {
			//is movie id in map?
			_, ok := rsItems[mov.Id]
			if !ok {
				if DeleteMovieFromDB(mov.Id) {
					log.Println("Movie not in watchlist, removed %d : %s", mov.Id, mov.Title)
					count = +1
				}
			}
		}
	}
	return count
}

func DeleteMovieFromDB(movieid int64) bool {
	_, err := db.Exec("delete from movies where id=?", movieid)
	if err != nil {
		return false
	} else {
		return true
	}
}

// loop the imdb rss and if the
// movie doesn't already exist we add to
// the Movies table
func RSS2toDB(rs *RSS2) (count int) {
	stmt, err := db.Prepare("INSERT INTO movies(id, title, grabbed) VALUES(?,?,?)")
	if err != nil {
		log.Println("RSS2DB:PrepareStmt", err)
		return 0
	}
	defer stmt.Close()

	for _, mv := range rs.Items {
		//Get ID as INT64
		id, err := TTtoID(mv.Link)
		if err != nil {
			log.Printf("Couldn't find ID - %s %s %+v", mv.Title, mv.Link, err)
		}

		_, err = stmt.Exec(id, mv.Title, 0)
		if err != nil {
			//if err then cast to sqlite3.Error so we can ignore specific errors
			sqlerr := err.(sqlite3.Error)
			if sqlerr.Code != sqlite3.ErrConstraint {
				log.Printf("RSS2DB:ExecInsert:%d %s", sqlerr.Code, sqlerr.Error())
			}
		} else {
			log.Printf("RSS2DB:Added Movie %s with ID:%d", mv.Title, id)
			count += 1
		}

	}
	return count
}

func NzbListByMovie(MovieId int64, GrabbedStatus int, IgnoredStatus int) []NZB {
	var (
		mv  NZB
		mvs []NZB
	)
	rows, err := db.Query(`
		select n.id,n.movieid,m.title as moviename,n.title,link,score,size,grabs
		,usenetdate,n.grabbed,ignored 
		from nzbs n
		inner join movies m on m.id=n.movieid
		where n.movieid=?
		order by ignored,score desc	
	`, MovieId)
	if err != nil {
		log.Println("DB:NzbListByMovie:", err)
		return nil
	}

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&mv.Id, &mv.MovieId, &mv.MovieName, &mv.Title, &mv.Link, &mv.Score, &mv.Size, &mv.Grabs, &mv.UsenetDate, &mv.Grabbed, &mv.Ignored)
		mvs = append(mvs, mv)
	}

	return mvs
}

func MoviesList(GrabbedStatus int) []Movie {
	var (
		mv  Movie
		mvs []Movie
	)

	rows, err := db.Query(`
		PRAGMA read_uncommitted = 1;
		select id,title,grabbed,coalesce(nzbcount,0) as nzbcount,coalesce(ignorecount,0) as ignorecount,coalesce(coverurl,'') as coverurl, case when (1-grabbed)*(nzbcount-ignorecount)>0 THEN 0 ELSE 1 END AS orderfield 
		from movies 
		left outer join (select movieid,count(id) as nzbcount,sum(ignored) as ignorecount from nzbs group by movieid) as c on c.movieid=id
		order by Orderfield,grabbed,title	
	`)
	if err != nil {
		log.Println("DB:MoviesList:", err)
		return nil
	}

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&mv.Id, &mv.Title, &mv.Grabbed, &mv.NzbCount, &mv.IgnoreCount, &mv.CoverUrl, &mv.Orderfield)
		mvs = append(mvs, mv)
	}

	return mvs
}

func DownloadList(dlmethod string) []Downloads {
	var (
		dl  Downloads
		dls []Downloads
	)

	rows, err := db.Query(`
		select m.title as nicename,movieid,guid,dlid from downloads d inner join nzbs n on d.guid=n.id inner join movies m on m.id=n.movieid where dlmethod=?
	`, dlmethod)
	if err != nil {
		log.Println("DB:DownloadList:", err)
		return nil
	}

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&dl.Nicename, &dl.MovieID, &dl.Guid, &dl.DlId)
		dls = append(dls, dl)
	}

	return dls
}

func GrabbableList() []Grabbable {
	var (
		gb  Grabbable
		gbs []Grabbable
	)
	rows, err := db.Query(`
		select n.movieid,m.title as movietitle, n.id, n.link from nzbs n 
		inner join (select movieid,max(score) as maxscore from nzbs 
		where score>0 and grabbed=0 and ignored=0 
		group by movieid) as c on c.movieid=n.movieid and c.maxscore=n.score
		inner join movies m on m.id=n.movieid
		where m.grabbed=0
	`)
	if err != nil {
		log.Println("DB:GrabbableList:", err)
		return nil
	}

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&gb.MovieId, &gb.MovieTitle, &gb.Id, &gb.Link)
		gbs = append(gbs, gb)
	}
	return gbs
}

//Calculate score from nzb title, date and size
func GetScore(title string, usenetdate time.Time, nzbsize float64) (score float64) {
	if nzbsize > 0.7 {
		nzbage := int(time.Since(usenetdate).Hours() / 24)
		// calculate score - gaussian distribution on size and exponential decay for age
		gauss := 100 - math.Abs(math.Pow(nzbsize-4.5, 2)/(2*math.Pow(1.5, 2)))
		decay := math.Pow(math.E, (-1*float64(nzbage))/180) * 100
		// score is a combination of both - this means that slightly older files
		// that are closer to 7.5Gb will have a slightly higher score than newer files
		// that deviate away from this size.

		// Preferred words get a bonus of 500, and banned words a bonus of -10000
		score = gauss*decay + (float64(WordsInString(MYPREFERREDWORDS, title)) * 500) - (float64(WordsInString(MYBANNEDWORDS, title)) * 10000)
	} else {
		score = -10000.7
	}
	return score
}

// Get URL and Title from DB
func URLAndTitleFromDB(guid string, id int64) (nzburl string, nicename string) {
	err := db.QueryRow("select title,n.link from nzbs n where n.id=? and movieid=?", guid, id).Scan(&nicename, &nzburl)
	if err != nil {
		log.Print(err)
		return "", ""
	}
	return nzburl, nicename
}

func SetNZBGrabIgnore(guid string, grabflag int, ignoreflag int) {
	_, err := db.Exec("update nzbs set grabbed=?, ignored=? where id=?", grabflag, ignoreflag, guid)
	if err != nil {
		log.Printf("SetNZBGrabIgnore:Grab=%d,Ignore=%d:%v", grabflag, ignoreflag, err)
	}
}

func SetMovieGrab(id int64, grabflag int) {
	_, err := db.Exec("update movies set grabbed=? where id=?", grabflag, id)
	if err != nil {
		log.Printf("SetMovieGrab:Grab=%d,Id=%d:%v", grabflag, id, err)
	}
}

func MarkNZBDownload(Nzo_id string, guid string, Method string) {
	_, err := db.Exec("INSERT into downloads (guid,dlmethod,dlid) values (?,?,?)", guid, Method, Nzo_id)
	if err != nil {
		log.Printf("MarkNZBDownload:%v", err)
	}
}

func RemoveDownloadFromDB(guid string) {
	_, err := db.Exec("delete from downloads where guid=?", guid)
	if err != nil {
		log.Printf("RemoveDownloadFromDB:%v", err)
	}
}
