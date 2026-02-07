package main

import (
	"bufio"
	"os"

	"github.com/toomore/lazyflickrgo/jsonstruct"
)

func getTags(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var result []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) fromSearch(tags string) []jsonstruct.Photo {
	args := map[string]string{
		"tags":      tags,
		"tag_mode":  "all",
		"sort":      "date-posted-desc",
		"user_id":   a.UserID,
	}

	var result []jsonstruct.Photo
	for _, val := range a.Flickr.PhotosSearch(args) {
		result = append(result, val.Photos.Photo...)
	}
	return result
}

func (a *App) allPhotos(result *[]jsonstruct.Photo) {
	args := map[string]string{
		"sort":     "date-posted-desc",
		"user_id":  a.UserID,
	}

	for _, val := range a.Flickr.PhotosSearch(args) {
		*result = append(*result, val.Photos.Photo...)
	}
}
