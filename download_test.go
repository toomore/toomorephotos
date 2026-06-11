package main

import "testing"

func TestSuffixFromSource(t *testing.T) {
	cases := map[string]string{
		"https://live.staticflickr.com/680/21531509572_59e8bfb5fa_q.jpg":  "q", // Large Square
		"https://live.staticflickr.com/680/21531509572_59e8bfb5fa_m.jpg":  "m", // Small 240
		"https://live.staticflickr.com/680/21531509572_59e8bfb5fa_s.jpg":  "s", // Square 75
		"https://live.staticflickr.com/680/21531509572_59e8bfb5fa_b.jpg":  "b", // Large 1024
		"https://live.staticflickr.com/680/21531509572_ee7e67ed44_3k.jpg": "3k",
		"https://live.staticflickr.com/680/21531509572_ddd8f109da_o.tif":  "o",
		"https://live.staticflickr.com/680/21531509572_59e8bfb5fa.jpg":    "m500", // Medium 500, no suffix
		"https://x/21531509572_59e8bfb5fa_b.jpg?size=1":                   "b",
	}
	for src, want := range cases {
		if got := suffixFromSource(src); got != want {
			t.Errorf("suffixFromSource(%q) = %q; want %q", src, got, want)
		}
	}
}

func TestExtFromURL(t *testing.T) {
	cases := map[string]string{
		"https://live.staticflickr.com/65535/123_abc_b.jpg": ".jpg",
		"https://live.staticflickr.com/65535/123_abc_o.tif": ".tif",
		"https://x/123_abc_o.PNG":                           ".png",
		"https://x/123_abc_b.jpg?size=1":                    ".jpg",
		"https://x/noext":                                   ".jpg",
	}
	for u, want := range cases {
		if got := extFromURL(u); got != want {
			t.Errorf("extFromURL(%q) = %q; want %q", u, got, want)
		}
	}
}

func TestResolveArchiveRoot(t *testing.T) {
	t.Setenv("IMAGE_ARCHIVE_DIR", "/from/env")
	if got := resolveArchiveRoot("/from/flag"); got != "/from/flag" {
		t.Errorf("flag should win, got %q", got)
	}
	if got := resolveArchiveRoot(""); got != "/from/env" {
		t.Errorf("env should be used, got %q", got)
	}
	t.Setenv("IMAGE_ARCHIVE_DIR", "")
	if got := resolveArchiveRoot(""); got != "./archive" {
		t.Errorf("default should be ./archive, got %q", got)
	}
}
