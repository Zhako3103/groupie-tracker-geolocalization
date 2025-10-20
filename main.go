package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Artist struct {
	ID           int      `json:"id"`
	Image        string   `json:"image"`
	Name         string   `json:"name"`
	Members      []string `json:"members"`
	CreationDate int      `json:"creationDate"`
	FirstAlbum   string   `json:"firstAlbum"`
	Relations    string   `json:"relations"`
}

type Locations struct {
	ID        int      `json:"id"`
	Locations []string `json:"locations"`
}

type Dates struct {
	ID    int      `json:"id"`
	Dates []string `json:"dates"`
}

type Relation struct {
	Index []struct {
		ID             int                 `json:"id"`
		DatesLocations map[string][]string `json:"datesLocations"`
	} `json:"index"`
}

type ArtistFull struct {
	Artist
	Locations    []string
	Dates        []string
	DatesByPlace map[string][]string
}

type PageData struct {
	Artists     []ArtistFull
	SearchQuery string
}

var artistsFull []ArtistFull
var geocodeCache = map[int][]map[string]interface{}{}

func main() {
	log.Println("Загрузка данных c API...")
	loadData()

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/artist", artistHandler)
	http.HandleFunc("/artist_map", artistMapHandler)
	http.HandleFunc("/api/artist_locations", apiArtistLocationsHandler)
	http.HandleFunc("/api/artists", apiArtistsHandler)

	log.Println("Сервер запущен на http://localhost:8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}

func loadData() {
	var artists []Artist
	if err := fetchJSON("https://groupietrackers.herokuapp.com/api/artists", &artists); err != nil {
		log.Fatal("Ошибка запроса artists:", err)
	}

	var locations struct {
		Index []Locations `json:"index"`
	}
	if err := fetchJSON("https://groupietrackers.herokuapp.com/api/locations", &locations); err != nil {
		log.Fatal("Ошибка запроса locations:", err)
	}

	var dates struct {
		Index []Dates `json:"index"`
	}
	if err := fetchJSON("https://groupietrackers.herokuapp.com/api/dates", &dates); err != nil {
		log.Fatal("Ошибка запроса dates:", err)
	}

	var rel Relation
	if err := fetchJSON("https://groupietrackers.herokuapp.com/api/relation", &rel); err != nil {
		log.Fatal("Ошибка запроса relation:", err)
	}

	locByID := make(map[int][]string)
	for _, l := range locations.Index {
		locByID[l.ID] = l.Locations
	}

	datesByID := make(map[int][]string)
	for _, d := range dates.Index {
		datesByID[d.ID] = d.Dates
	}

	for _, a := range artists {
		af := ArtistFull{
			Artist:       a,
			Locations:    locByID[a.ID],
			Dates:        datesByID[a.ID],
			DatesByPlace: map[string][]string{},
		}

		for _, r := range rel.Index {
			if r.ID == a.ID {
				af.DatesByPlace = r.DatesLocations
				break
			}
		}

		artistsFull = append(artistsFull, af)
	}

	log.Printf("Данные загружены. Артистов: %d\n", len(artistsFull))
}

func fetchJSON(url string, target interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query := r.URL.Query().Get("search")
	var artists []ArtistFull

	if query == "" {
		artists = artistsFull
	} else {
		queryLower := strings.ToLower(query)
		for _, artist := range artistsFull {
			if strings.Contains(strings.ToLower(artist.Name), queryLower) {
				artists = append(artists, artist)
			}
		}
	}

	data := PageData{
		Artists:     artists,
		SearchQuery: query,
	}

	tmpl.Execute(w, data)
}

func apiArtistsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artistsFull)
}

func artistHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}

	var artist ArtistFull
	found := false
	for _, a := range artistsFull {
		if idStr == itoa(a.ID) {
			artist = a
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	tmpl, err := template.ParseFiles("templates/artist.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, artist)
}

func artistMapHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}

	var artist ArtistFull
	found := false
	for _, a := range artistsFull {
		if idStr == itoa(a.ID) {
			artist = a
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	tmpl, err := template.ParseFiles("templates/artist_map.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, artist)
}

// geocodeHandler возвращает JSON-массив объектов {location, lat, lon} для артиста с заданным id.
// Использует список локаций артиста (и ключи DatesByPlace) и запрашивает координаты у Nominatim.
func geocodeHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	var artist *ArtistFull
	for _, a := range artistsFull {
		if idStr == itoa(a.ID) {
			artist = &a
			break
		}
	}
	if artist == nil {
		http.NotFound(w, r)
		return
	}

	// Собираем уникальные названия локаций: из Locations и ключей DatesByPlace
	uniq := map[string]struct{}{}
	for _, loc := range artist.Locations {
		if strings.TrimSpace(loc) != "" {
			uniq[loc] = struct{}{}
		}
	}
	for k := range artist.DatesByPlace {
		if strings.TrimSpace(k) != "" {
			uniq[k] = struct{}{}
		}
	}

	locs := make([]string, 0, len(uniq))
	for k := range uniq {
		locs = append(locs, k)
	}

	res, err := geocodeLocations(locs)
	if err != nil {
		log.Println("geocode error:", err)
		http.Error(w, "geocode error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// geocodeLocations делает запросы в Nominatim для каждого названия места.
// Возвращает массив объектов с полями Location, Lat, Lon.
func geocodeLocations(locations []string) ([]map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	results := make([]map[string]interface{}, 0, len(locations))

	for _, loc := range locations {
		q := url.QueryEscape(loc)
		// limit=1 чтобы получить одно совпадение
		api := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1", q)
		req, err := http.NewRequest("GET", api, nil)
		if err != nil {
			return nil, err
		}
		// Указывать User-Agent согласно политике Nominatim
		req.Header.Set("User-Agent", "groupie-tracker/1.0 (your-email@example.com)")

		resp, err := client.Do(req)
		if err != nil {
			// при проблемах с одним запросом — пропускаем
			log.Printf("geocode request failed for %q: %v", loc, err)
			continue
		}
		var body []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			log.Printf("decode nominatim for %q failed: %v", loc, err)
			continue
		}
		resp.Body.Close()

		if len(body) == 0 {
			continue
		}

		// lat/lon may be string or number depending on response; handle both
		var lat, lon float64
		var ok bool

		// latitude
		switch v := body[0]["lat"].(type) {
		case string:
			lat, err = strconv.ParseFloat(v, 64)
			if err != nil {
				log.Printf("parse lat string failed for %q: %v", loc, err)
				continue
			}
		case float64:
			lat = v
		default:
			log.Printf("unknown lat type for %q: %T", loc, body[0]["lat"])
			continue
		}

		// longitude
		switch v := body[0]["lon"].(type) {
		case string:
			lon, err = strconv.ParseFloat(v, 64)
			if err != nil {
				log.Printf("parse lon string failed for %q: %v", loc, err)
				continue
			}
		case float64:
			lon = v
		default:
			log.Printf("unknown lon type for %q: %T", loc, body[0]["lon"])
			continue
		}
		_ = ok

		results = append(results, map[string]interface{}{
			"location": loc,
			"lat":      lat,
			"lon":      lon,
		})

		// Небольшая пауза чтобы не перегружать сервис
		time.Sleep(300 * time.Millisecond)
	}
	return results, nil
}

// getArtistLocationsCached возвращает координаты для артиста, используя кэширование по id.
func getArtistLocationsCached(id int, artist *ArtistFull) ([]map[string]interface{}, error) {
	if v, ok := geocodeCache[id]; ok {
		return v, nil
	}

	// Собираем уникальные места
	uniq := map[string]struct{}{}
	for _, loc := range artist.Locations {
		if strings.TrimSpace(loc) != "" {
			uniq[loc] = struct{}{}
		}
	}
	for k := range artist.DatesByPlace {
		if strings.TrimSpace(k) != "" {
			uniq[k] = struct{}{}
		}
	}
	locs := make([]string, 0, len(uniq))
	for k := range uniq {
		locs = append(locs, k)
	}

	res, err := geocodeLocations(locs)
	if err != nil {
		return nil, err
	}
	geocodeCache[id] = res
	return res, nil
}

// apiArtistLocationsHandler возвращает JSON списка {location,lat,lon} по id артиста,
// использует кэш и при первом запросе делает геокодирование.
func apiArtistLocationsHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var artist *ArtistFull
	for i := range artistsFull {
		if artistsFull[i].ID == id {
			artist = &artistsFull[i]
			break
		}
	}
	if artist == nil {
		http.NotFound(w, r)
		return
	}

	res, err := getArtistLocationsCached(id, artist)
	if err != nil {
		log.Println("apiArtistLocations error:", err)
		http.Error(w, "geocode error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
