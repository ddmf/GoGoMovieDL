//nzbgeek.go
package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"

	//	log "github.com/Sirupsen/logrus"
)

type NZBGRSS struct {
	Channels struct {
		NZBGItems []NZBGItem `xml:"item"`
	} `xml:"channel"`
}

type NZBGItem struct {
	Title     string     `xml:"title"`
	Link      string     `xml:"link"`
	PubDate   string     `xml:"pubDate"`
	NZAttribs []NZAttrib `xml:"attr"`
}

type NZAttrib struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// helper function to return releases
// available for a specific movie via imdb id
func NZBGeekMovieByIMDB(IMDB int64, APIKey string) (*NZBGRSS, error) {
	// https://api.nzbgeek.info/rss?dl=1&imdb=ttxxxx&r=APIKEY
	return NZBGeekRSS(fmt.Sprintf("dl=1&imdb=tt%d", IMDB), APIKey)
}

// helper function to download latest movies from NZBGEEK
func NZBGeekMovies(APIKey string) (*NZBGRSS, error) {
	// https://api.nzbgeek.info/rss?t=2000&dl=1&num=50&r=APIKEY
	return NZBGeekRSS("t=2000&dl=1", APIKey)
}

// main function to visit URL with APIKEY appended
// and return NZBGRSS structure
func NZBGeekRSS(URL string, APIKey string) (*NZBGRSS, error) {
	NewURL := fmt.Sprintf("https://api.nzbgeek.info/rss?%s&r=%s", URL, APIKey)

	r, err := http.Get(NewURL)
	defer r.Body.Close()
	if err != nil {
		return nil, err
	}

	nz := new(NZBGRSS)
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	err = xml.Unmarshal(body, &nz)
	if err != nil {
		return nil, err
	}

	return nz, nil
}
