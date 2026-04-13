// vim: set autoindent filetype=go noexpandtab tabstop=4 shiftwidth=4:
package main

import (
	"bufio"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed spinebanner.svg
var bannerSVG []byte

//go:embed spines.html
var indexHTML []byte

type Album struct {
	ID       int    `json:"id"`
	Category string `json:"releasetype"`
	Path     string `json:"-"`
	Artist   string `json:"artist"`
	Title    string `json:"album"`
	Year     string `json:"year"`
}

var albums []Album

func readMPDMusicDir() (string, error) {
	conn, err := net.Dial("unix", "/run/mpd/socket")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	fmt.Fprintf(conn, "config\n")

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "OK" || strings.HasPrefix(line, "ACK") {
			break
		}
		if strings.HasPrefix(line, "music_directory: ") {
			return strings.TrimPrefix(line, "music_directory: "), nil
		}
	}

	return "", fmt.Errorf("music_directory not found in MPD config response")
}

var yearRE = regexp.MustCompile(`\((\d{4})\)$`)

func extractYear(s string) (title, year string) {
	m := yearRE.FindStringSubmatch(s)
	if m == nil {
		return s, ""
	}
	title = strings.TrimSpace(s[:len(s)-len(m[0])])
	year = m[1]
	return
}

func parseM3UHeader(path string) (artist, album, year string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			break
		}
		switch {
		case strings.HasPrefix(line, "#EXTART:"):
			artist = strings.TrimSpace(strings.TrimPrefix(line, "#EXTART:"))
		case strings.HasPrefix(line, "#EXTALB:"):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "#EXTALB:"))
			album, year = extractYear(raw)
		}
	}

	err = scanner.Err()
	return
}

func parseCUE(path string) (artist, album, year string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var yeardigRE = regexp.MustCompile(`\d{4}`)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "FILE") {
			break
		}
		switch {
		case strings.HasPrefix(line, "PERFORMER"):
			artist = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "PERFORMER")), `"`)
		case strings.HasPrefix(line, "TITLE"):
			album = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "TITLE")), `"`)
		case strings.HasPrefix(line, "REM DATE"):
			year = yeardigRE.FindString(line)
		}
	}

	err = scanner.Err()
	return
}

func findAlbumFiles(root string) error {
	root = strings.TrimSuffix(root, string(filepath.Separator))
	rootParts := strings.Split(root, string(filepath.Separator))
	var sAlb, sArt, sYear string

	err := filepath.WalkDir(root, func(sCandidate string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip directories we can't read, but don't abort the whole walk
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", sCandidate, err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		name := strings.ToLower(d.Name())
		if name == "album.m3u" || name == "album.cue" {
			classification := strings.Split(sCandidate, string(filepath.Separator))[len(rootParts)]
			if strings.HasSuffix(name, "m3u") {
				sArt, sAlb, sYear, err = parseM3UHeader(sCandidate)
			} else {
				sArt, sAlb, sYear, err = parseCUE(sCandidate)
			}

			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: read error on %s: %v\n", sCandidate, err)
				return nil
			}
			albums = append(albums, Album{ID: len(albums), Path: sCandidate, Category: classification, Artist: sArt, Title: sAlb, Year: sYear})
		}
		return nil
	})

	return err
}

func makeSpine(w, h int, text string) string {
	return fmt.Sprintf(
		`<svg version="1.1" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`+
			`<rect width="100%%" height="100%%" fill="gainsboro"/>`+
			`<text font-size="%d" text-anchor="middle" transform="translate(%d,%d) rotate(90)">%s</text>`+
			`</svg>`,
		w, h, w*10/15, w/2, h/2, text)
}

func makeCover(w, h int) string {
	return fmt.Sprintf(
		`<svg version="1.1" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`+
			`<rect width="100%%" height="100%%" fill="gainsboro" stroke="black" />`+
			`<line x1="0" y1="0" x2="%d" y2="%d" stroke="black" />`+
			`</svg>`,
		w, h, w, h)
}

func serveRotatedSpine(w http.ResponseWriter, path string) {
	//get dimensions
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		http.Error(w, "decode error", http.StatusNotFound)
		return
	}

	// base64 the image file
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "read error", http.StatusNotFound)
		return
	}
	ext := strings.ToLower(filepath.Ext(path)[1:])
	b64 := base64.StdEncoding.EncodeToString(data)
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" version="1.1" width="%d" height="%d">`+
			`<image x="-%d" y="0" width="%d" height="%d" xlink:href="data:image/%s;base64,%s" transform="rotate(-90)"/>`+
			`</svg>`,
		cfg.Height, cfg.Width,
		cfg.Width, cfg.Width, cfg.Height,
		ext, b64,
	)
	w.Header().Set("Content-Type", "image/svg+xml")
	fmt.Fprint(w, svg)
}

func main() {
	sMusicDir, err := readMPDMusicDir()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(sMusicDir)

	if len(os.Args) > 1 {
		sMusicDir = os.Args[1]
	}

	err = findAlbumFiles(sMusicDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(albums) == 0 {
		fmt.Println("No album.m3u or album.cue files found.")
		return
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHTML)
	})

	http.HandleFunc("/spinebanner.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(bannerSVG)
	})

	http.HandleFunc("/getcover", func(w http.ResponseWriter, r *http.Request) {
		sId := r.URL.Query().Get("id")
		sSide := r.URL.Query().Get("side")
		iId, err := strconv.Atoi(sId)
		if err != nil || iId < 0 || iId >= len(albums) {
			http.Error(w, "invalid id" + sId, http.StatusBadRequest)
			return
		}
		if sSide!="cover" && sSide!="back" {
			http.Error(w, "invalid side", http.StatusBadRequest)
			return
		}
		dir := filepath.Dir(albums[iId].Path)
		for _, name := range []string{sSide+".jpg", sSide+".png"} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				http.ServeFile(w, r, candidate)
				return
			}
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, makeCover(500, 500))
	})

	http.HandleFunc("/getspine", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id < 0 || id >= len(albums) {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		dir := filepath.Dir(albums[id].Path)
		for _, name := range []string{"spine.jpg", "spine.png"} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				http.ServeFile(w, r, candidate)
				return
			}
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, makeSpine(1180, 65, albums[id].Title))
	})

	http.HandleFunc("/getspine90", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id < 0 || id > len(albums) {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		dir := filepath.Dir(albums[id].Path)
		//look for png and jpg
		for _, name := range []string{"spine.jpg", "spine.png"} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				// returing spine and exiting
				serveRotatedSpine(w, candidate)
				return
			}
		}

		//no spine
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, bannerSVG)
	})

	http.HandleFunc("/getalbumsjson", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(albums)
	})

	fmt.Println("Listening on :8181")
	if err := http.ListenAndServe(":8181", nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// fmt.Printf("Found %d file(s):\n", len(albums))
	//
	//	for _, f := range albums {
	//		fmt.Printf("%d %s %s %s %s\n", f.ID, f.Category, f.Artist, f.Title, f.Year)
	//	}
}
