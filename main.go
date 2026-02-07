package main

import (
	"flag"
	"log"
	"net/http"
)

var httpPort = flag.String("p", ":8080", "HTTP port")

func main() {
	flag.Parse()
	app, err := NewApp()
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", app.index)
	http.HandleFunc("/p/", app.photo)
	http.HandleFunc("/sitemap/", app.sitemap)
	http.HandleFunc("/rss", app.rss)
	http.HandleFunc("/atom", app.atom)
	http.HandleFunc("/fr", app.notFound)
	http.HandleFunc("/health", app.health)

	app.serveSingle("/favicon.ico", "favicon.ico")
	app.serveSingle("/jquery.unveil.min.js", "jquery.unveil.min.js")
	app.serveSingle("/base_min.css", "base_min.css")
	app.serveSingle("/base_photo_min.css", "base_photo_min.css")
	app.serveSingle("/robots.txt", "robots.txt")

	log.Println("HTTP Port:", *httpPort)
	log.Println(http.ListenAndServe(*httpPort, nil))
}
