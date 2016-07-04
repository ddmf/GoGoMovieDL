package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/urfave/negroni"

	log "github.com/Sirupsen/logrus"
)

var (
	//	muxrouter *mux.Router
	templates map[string]*template.Template
)

//Show list of all movies
func MoviesHandler(w http.ResponseWriter, r *http.Request) {
	mvs := MoviesList(-1)
	if mvs == nil {
		log.Debug("Webstuff:MoviesHandler:GetMoviesList:NothingReturned")
		http.Error(w, "", 500)
	} else {
		//fixup the urls and cover img tags
		for i, mov := range mvs {
			mvs[i].MovieUrl = fmt.Sprintf("/%d/", mov.Id)
			if mov.CoverUrl != "" {
				mvs[i].CoverUrl = fmt.Sprintf(`<img height=100 src="%s">`, mov.CoverUrl)
			}
		}

		t, ok := templates["MoviesTPL"]
		if !ok {
			log.Debug("Webstuff:MoviesHandler:Parse")
			http.Error(w, "TemplateDoesntExist", 500)
		}
		err := t.Execute(w, mvs)
		if err != nil {
			log.Debug("Webstuff:MoviesHandler:Execute:", err)
			http.Error(w, "Boom", 500)
		}
	}
}

//Show files available for specific movie
func MovieHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	MovieId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
	}

	//moviestruct for passing to template
	type moviestruct struct {
		MovieName string
		NZBList   []NZB
	}

	mv := moviestruct{}
	mv.NZBList = NzbListByMovie(MovieId, -1, -1)
	if len(mv.NZBList) <= 0 {
		return
	}

	mv.MovieName = mv.NZBList[0].MovieName

	//fixup the url
	for i, mov := range mv.NZBList {
		//Remember to modify mv, not mov!
		mv.NZBList[i].GrabURL = fmt.Sprintf("/getnzb/%s/%s/", id, mov.Id)
	}

	t, ok := templates["MovieTPL"]
	if !ok {
		log.Debug("Webstuff:MoviesHandler:Parse")
		http.Error(w, "TemplateDoesntExist", 500)
	}

	//create template data
	//need to add vars to top because can't access elements outside of range

	err = t.Execute(w, mv)
	if err != nil {
		log.Debug("Webstuff:MoviesHandler:Execute:", err)
		http.Error(w, "Boom", 500)
	}

}

//Get all nzbs for a specific movie id
func RefreshNZBHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	movid, _ := strconv.ParseInt(id, 10, 64)
	nz, err := NZBGeekMovieByIMDB(movid, MYAPIKEY)
	if err != nil {
		log.Debug("RefreshNZBHandler:GetByID:", id, err)
	} else {
		NZBGRSStoDB(nz)
	}
	http.Redirect(w, r, "/", 302)
}

//Mark Movie ungrabbed
func MovieUngrabbedHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	movid, _ := strconv.ParseInt(id, 10, 64)
	SetMovieGrab(movid, 0)
	http.Redirect(w, r, "/", 302)
}

//Set NZB ignored
func NZBIgnoredHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	guid := vars["nzbguid"]
	flag := vars["flag"]
	iflag, _ := strconv.Atoi(flag)
	SetNZBGrabIgnore(guid, 0, iflag)
	http.Redirect(w, r, fmt.Sprintf("/%s/", id), 302)
}

//Send NZB and redirect back to movie
func GrabNZBHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	guid := vars["nzbguid"]
	movid, _ := strconv.ParseInt(id, 10, 64)
	SABGrabAndMark(guid, movid)
	http.Redirect(w, r, fmt.Sprintf("/%s/", id), 302)
}

func LoggingMiddleware(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	start := time.Now()

	next(rw, r)

	res := rw.(negroni.ResponseWriter)
	log.Infof("%s %s completed %v %s in %v", r.Method, r.URL.Path, res.Status(), http.StatusText(res.Status()), time.Since(start))
}

func InitWebServer() {
	log.Info("Webstuff:Init:Begin")
	DefineTemplates()

	muxrouter := mux.NewRouter()
	muxrouter.HandleFunc("/", MoviesHandler).Name("allmovies")
	muxrouter.HandleFunc("/{id:[0-9]+}/", MovieHandler).Name("onemovie")
	muxrouter.HandleFunc("/getnzb/{id:[0-9]+}/{nzbguid}/", GrabNZBHandler).Name("getnzb")
	muxrouter.HandleFunc("/refreshnzbs/{id:[0-9]+}/", RefreshNZBHandler).Name("refreshnzbs")
	muxrouter.HandleFunc("/markungrabbed/{id:[0-9]+}/", MovieUngrabbedHandler).Name("markungrabbed")
	muxrouter.HandleFunc("/ignorenzb/{id:[0-9]+}/{nzbguid}/{flag:[0-1]}/", NZBIgnoredHandler).Name("ignorenzb")

	n := negroni.New()
	recovery := negroni.NewRecovery()
	n.Use(recovery)
	n.Use(negroni.HandlerFunc(LoggingMiddleware))
	n.UseHandler(muxrouter)

	err := http.ListenAndServe(":5151", n)
	if err != nil {
		log.Error("InitWebServer", err)
	}
}

func DefineTemplates() {
	MovieTPL := `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>{{.MovieName}}</title>
		<link href="https://cdn.jsdelivr.net/min/1.5/min.min.css" rel="stylesheet" type="text/css">
		<link href="https://cdnjs.cloudflare.com/ajax/libs/foundicons/3.0.0/foundation-icons.min.css" rel="stylesheet" type="text/css">
	</head>
    <body>
		<div class="container">
		<p><a href="/">Home</a></p>
		<h2>{{.MovieName}}</h2>
		<table>
		<thead>
		<tr>
			<th>Date</th>
			<th>Title</th>
			<th>Size</th>
			<th>Score</th>
			<th>Grabbed</th>
			<th>Grab</th>
			<th>Ignored</th>
		</tr>
		</thead>
		<tbody>
        {{range .NZBList}}
		<tr>
			<td>{{.UsenetDate.Format "02/01/2006" }}</td>
			<td>{{.Title}}</td>
			<td>{{ printf "%0.2fGb" .Size}}</td>
			<td>{{ printf "%0.2f" .Score}}</td>
			<td>{{.Grabbed}}</td>
			<td><a href="{{.GrabURL}}"><i class="fi-download"></i></a></td>
			<td>
{{ if eq .Ignored 1 }}<a href="/ignorenzb/{{.MovieId}}/{{.Id}}/0/" title="Ignored - Click to unignore"><i class="fi-dislike"></i></a>{{else}}<a href="/ignorenzb/{{.MovieId}}/{{.Id}}/1/" title="Click to ignore"><i class="fi-like"></i></a>{{end}}
			</td>
		</tr>
		{{end}}
		</div>
	</body>
</html>	
`

	MoviesTPL := `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>GoGoMoviesDL</title>
		<link href="https://cdn.jsdelivr.net/min/1.5/min.min.css" rel="stylesheet" type="text/css">
		<link href="https://cdnjs.cloudflare.com/ajax/libs/foundicons/3.0.0/foundation-icons.min.css" rel="stylesheet" type="text/css">
	</head>
    <body>
		<div class="container">
		<table>
		<thead>
		<tr>
			<th>IMDB</th>
			<th>Cover</th>
			<th>Title</th>
			<th>Grabbed</th>
			<th>Refresh</th>
		</tr>
		</thead>
		<tbody>
{{ range . }}
		<tr>
			<td><a target="_blank" rel="noopener noreferrer" href="http://www.imdb.com/title/tt{{ .Id }}"><i class="fi-projection-screen"></i></a></td>
	{{ if gt .NzbCount 0 }}
			<td><a href="{{.MovieUrl}}">{{ .CoverUrl | safeHTML }}</a></td>
			<td><a href="{{.MovieUrl}}">{{.Title}}</a></td>
	{{ else }}
			<td>{{ .CoverUrl | safeHTML }}</td>
			<td>{{ .Title }}</td>
	{{ end }}
			<td>{{if gt .Grabbed 0 }}<a href="/markungrabbed/{{ .Id }}/"><i class="fi-check"></a></i>{{end}}</td>
			<td><a href="/refreshnzbs/{{.Id}}/"><i class="fi-refresh"></i></a></td>
		</tr>
{{end}}
		</tbody>
		</table>
		</div>
	</body>	
</html>		
`

	if templates == nil {
		templates = make(map[string]*template.Template)
	}

	templates["MoviesTPL"] = template.Must(template.New("MoviesTPL").Funcs(template.FuncMap{
		"safeHTML": safeHTML}).Parse(MoviesTPL))

	templates["MovieTPL"] = template.Must(template.New("MovieTPL").Funcs(template.FuncMap{
		"safeHTML": safeHTML}).Parse(MovieTPL))
}

// safeHTML returns a given string as html/template HTML content.
func safeHTML(a string) template.HTML { return template.HTML(a) }
