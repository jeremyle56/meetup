package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type pageData struct {
	Title string
}

type nominatimResult struct {
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
}

type geocodeResponse struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Name string  `json:"name"`
}

func main() {
	http.HandleFunc("/styles.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "styles.css")
	})
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/api/geocode", geocodeHandler)
	http.HandleFunc("/api/search", searchHandler)
	http.HandleFunc("/api/reverse", reverseHandler)

	addr := ":8080"
	log.Printf("Starting meetup locator on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("index.html")
	if err != nil {
		log.Printf("error parsing template: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	data := pageData{Title: "Meetup Locator"}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("error executing template: %v", err)
	}
}

func geocodeHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		http.Error(w, "missing query parameter", http.StatusBadRequest)
		return
	}

	result, err := geocodeAddress(query)
	if err != nil {
		log.Printf("geocode error for %q: %v", query, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("error encoding geocode response: %v", err)
	}
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		http.Error(w, "missing query parameter", http.StatusBadRequest)
		return
	}

	results, err := searchAddress(query, 5)
	if err != nil {
		log.Printf("search error for %q: %v", query, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		log.Printf("error encoding search response: %v", err)
	}
}

func reverseHandler(w http.ResponseWriter, r *http.Request) {
	latStr := strings.TrimSpace(r.URL.Query().Get("lat"))
	lonStr := strings.TrimSpace(r.URL.Query().Get("lon"))
	if latStr == "" || lonStr == "" {
		http.Error(w, "missing lat or lon parameter", http.StatusBadRequest)
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(w, "invalid lat parameter", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(w, "invalid lon parameter", http.StatusBadRequest)
		return
	}

	result, err := reverseGeocode(lat, lon)
	if err != nil {
		log.Printf("reverse error for %f,%f: %v", lat, lon, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("error encoding reverse response: %v", err)
	}
}

func searchAddress(query string, limit int) ([]geocodeResponse, error) {
	endpoint := "https://nominatim.openstreetmap.org/search"
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	params := u.Query()
	params.Set("format", "json")
	params.Set("limit", strconv.Itoa(limit))
	params.Set("q", query)
	params.Set("viewbox", "150.5209,-33.5781,151.3430,-34.1183")
	params.Set("bounded", "0")
	u.RawQuery = params.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "meetup-locator-local/1.0")
	req.Header.Set("Accept-Language", "en")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding service responded with %s", resp.Status)
	}

	var results []nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	suggestions := make([]geocodeResponse, 0, len(results))
	for _, result := range results {
		lat, err := strconv.ParseFloat(result.Lat, 64)
		if err != nil {
			continue
		}
		lon, err := strconv.ParseFloat(result.Lon, 64)
		if err != nil {
			continue
		}
		suggestions = append(suggestions, geocodeResponse{Lat: lat, Lon: lon, Name: result.DisplayName})
	}

	// Prefer Sydney-area results by sorting suggestions by distance to Sydney center
	const sydneyLat = -33.8688
	const sydneyLon = 151.2093
	sort.SliceStable(suggestions, func(i, j int) bool {
		disti := (suggestions[i].Lat-sydneyLat)*(suggestions[i].Lat-sydneyLat) + (suggestions[i].Lon-sydneyLon)*(suggestions[i].Lon-sydneyLon)
		distj := (suggestions[j].Lat-sydneyLat)*(suggestions[j].Lat-sydneyLat) + (suggestions[j].Lon-sydneyLon)*(suggestions[j].Lon-sydneyLon)
		return disti < distj
	})

	return suggestions, nil
}

func geocodeAddress(query string) (*geocodeResponse, error) {
	endpoint := "https://nominatim.openstreetmap.org/search"
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	params := u.Query()
	params.Set("format", "json")
	params.Set("limit", "1")
	params.Set("q", query)
	u.RawQuery = params.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "meetup-locator-local/1.0")
	req.Header.Set("Accept-Language", "en")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding service responded with %s", resp.Status)
	}

	var results []nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no location found for %q", query)
	}

	lat, err := strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return nil, err
	}
	lon, err := strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return nil, err
	}

	return &geocodeResponse{
		Lat:  lat,
		Lon:  lon,
		Name: results[0].DisplayName,
	}, nil
}

func reverseGeocode(lat, lon float64) (*geocodeResponse, error) {
	endpoint := "https://nominatim.openstreetmap.org/reverse"
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	params := u.Query()
	params.Set("format", "json")
	params.Set("lat", fmt.Sprintf("%f", lat))
	params.Set("lon", fmt.Sprintf("%f", lon))
	params.Set("zoom", "18")
	params.Set("addressdetails", "0")
	u.RawQuery = params.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "meetup-locator-local/1.0")
	req.Header.Set("Accept-Language", "en")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding service responded with %s", resp.Status)
	}

	var result struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &geocodeResponse{
		Lat:  lat,
		Lon:  lon,
		Name: result.DisplayName,
	}, nil
}
