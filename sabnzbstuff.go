//sabnzbstuff.go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"time"
)

//Typical response from ADDURL
type SabResponse struct {
	SabErr  string   `json:"error"`
	Status  bool     `json:"status"`
	Nzo_ids []string `json:"nzo_ids"`
}

//History Output
type SabHistory struct {
	History struct {
		Slots []SabSlot `json:"slots"`
	} `json:"history"`
}

type SabSlot struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Nzo_id      string `json:"nzo_id"`
	FailMessage string `json:"fail_message"`
	Category    string `json:"category"`
	Storage     string `json:"storage"`
	Status      string `json:"status"`
}

//sanitises the passed in url from the config
func ReturnNiceSABURL(AURL string) string {
	//url should be in format http://host:port/sabnzbd/api
	newurl, _ := url.Parse(AURL)
	newurl.Path = "sabnzbd/api"
	log.Println("ReturnNiceSABURL - ", newurl.String())
	return newurl.String()
}

func JsonFromURLNoStruct(url string) {
	var f interface{}
	r, err := http.Get(url)
	if err != nil {
		return
	}
	defer r.Body.Close()

	err = json.NewDecoder(r.Body).Decode(&f)
	if err != nil {
		log.Println(err)
	}

	//log.Print(f)

	m := f.(map[string]interface{})

	//log.Print(m)

	for k, v := range m {
		switch vv := v.(type) {
		case string:
			log.Println(k, "is string", vv)
		case int:
			log.Println(k, "is int", vv)
		case []interface{}:
			log.Println(k, "is an array:")
			for i, u := range vv {
				log.Println(i, u)
			}
		default:
			log.Println(k, "is of a type I don't know how to handle")
		}
	}

}

func JsonFromURL(url string, target interface{}) error {
	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func SABParseHistory() {
	//http://localhost:8080/sabnzbd/api?apikey=&mode=history&output=json
	saburl, err := url.Parse(MYSABURL)
	if err != nil {
		log.Println(err)
		return
	}
	params := url.Values{}
	params.Add("output", "json")
	params.Add("apikey", MYSABAPI)
	params.Add("mode", "history")
	saburl.RawQuery = params.Encode()
	sh := new(SabHistory)
	err = JsonFromURL(saburl.String(), sh)
	if err != nil {
		log.Printf("SABParseHistory:%s  %+v", saburl.String(), err)
		return
	}

	//get downloads from the db
	dls := DownloadList("SABNZBD")
	for _, dl := range dls {
		for _, slots := range sh.History.Slots {
			if dl.DlId == slots.Nzo_id {
				//Only care about success or failed status
				switch slots.Status {
				case "Failed", "failed":
					//download failed - mark movie not grabbed,
					SetMovieGrab(dl.MovieID, 0)
					//mark nzb grabbed ignored, leave item in list
					SetNZBGrabIgnore(dl.Guid, 1, 1)
					//delete download record from db
					RemoveDownloadFromDB(dl.Guid)
					log.Printf("SABFailed:Removed %s from downloads table with id %s", dl.Nicename, dl.DlId)
				case "Completed", "completed":
					//download completed ok - delete item from list
					SetMovieGrab(dl.MovieID, 1)
					SetNZBGrabIgnore(dl.Guid, 1, 0)
					RemoveDownloadFromDB(dl.Guid)
					SABRemoveCompleted("history", slots.Nzo_id)
					log.Printf("SABCompleted:Removed %s from downloads table with id %s /n/n %+v", dl.Nicename, dl.DlId, slots)
				default:
					//log.Debugf("SABParseHistory:SlotStatus %s : Msg %s", slots.Status, slots.FailMessage)
				}
			}
		}
	}

}

func SABRemoveCompleted(mode string, nzoid string) bool {
	//http://localhost:8080/sabnzbd/api?apikey=&mode=history&name=delete&output=json&value=SABnzbd_nzo_urhpjt
	saburl, err := url.Parse(MYSABURL)
	if err != nil {
		log.Print(err)
		return false
	}
	params := url.Values{}
	params.Add("output", "json")
	params.Add("apikey", MYSABAPI)
	//restrict mode
	if mode == "queue" {
		params.Add("mode", "queue")
	} else {
		params.Add("mode", "history")
	}
	params.Add("name", "delete")
	params.Add("value", nzoid)
	saburl.RawQuery = params.Encode()
	SabR := new(SabResponse)
	err = JsonFromURL(saburl.String(), SabR)
	if err != nil {
		log.Printf("SABRemoveCompleted:Mode=%s:Error=%v", mode, err)
		return false
	}
	return SabR.Status
}

//Grab NZBD and mark database as grabbed or not.
func SABGrabAndMark(guid string, movid int64) {
	//Get URL and Nicename from DB
	URL, NiceName := URLAndTitleFromDB(guid, movid)
	//Send URL to SAB, returns trackable ID
	if URL != "" {
		Nzo_id := SABSendURL(guid, URL, NiceName, MYSABCAT)
		if Nzo_id != "" {
			//Mark as grabbed for NZB and Movie
			SetNZBGrabIgnore(guid, 1, 0)
			SetMovieGrab(movid, 1)
			//Add to downloads
			MarkNZBDownload(Nzo_id, guid, "SABNZBD")
		} else {
			//Issue with download, mark as ignored
			SetNZBGrabIgnore(guid, 0, 1)
		}
	}
}

//Send the NZBLINK url to SAB with nicename as Name. Returns NZO_ID if valid
func SABSendURL(guid string, nzblink string, nicename string, category string) string {
	log.Printf("SABSendURL:Grabbing:%s:%s:%s", guid, category, nicename)
	nzburl, err := url.Parse(MYSABURL)
	if err != nil {
		log.Print("SABSendURL:Parse:", err)
		return ""
	}
	params := url.Values{}
	params.Add("mode", "addurl")
	params.Add("output", "json")
	params.Add("apikey", MYSABAPI)
	params.Add("name", nzblink)
	params.Add("nzbname", nicename)
	if category != "" {
		params.Add("cat", category)
	}
	nzburl.RawQuery = params.Encode()
	SabR := new(SabResponse)
	err = JsonFromURL(nzburl.String(), SabR)
	if err != nil {
		log.Print("SABSendURL:JSON:", err)
		return ""
	}

	if SabR.Status {
		if len(SabR.Nzo_ids) > 0 {
			//sleep for 250ms after a positive add
			time.Sleep(250 * time.Millisecond)
			return SabR.Nzo_ids[0]
		} else {
			return ""
		}
	} else {
		log.Print("SABSendURL:SendReturnedError:", SabR.SabErr)
		return ""
	}
}
