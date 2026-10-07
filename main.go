package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: odx-verify [.odx/.pdx file]")
		os.Exit(2)
	}
	path := os.Args[1]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	var errs []string
	if strings.EqualFold(filepath.Ext(path), ".pdx") {
		errs = verifyPDX(data)
	} else {
		errs = validateODX(data)
	}

	if len(errs) == 0 {
		fmt.Println("OK!")
		return
	}
	fmt.Fprintln(os.Stderr, "NOT OK:")
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "  "+e)
	}
	os.Exit(1)
}

// verifyPDX treats the input as a zip archive and validates every .odx/.xml
// entry it contains.
func verifyPDX(data []byte) []string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return []string{"invalid PDX (not a zip archive): " + err.Error()}
	}
	var errs []string
	found := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".odx" && ext != ".xml" {
			continue
		}
		found = true
		rc, err := f.Open()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: cannot open entry: %v", f.Name, err))
			continue
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: cannot read entry: %v", f.Name, err))
			continue
		}
		for _, e := range validateODX(b) {
			errs = append(errs, f.Name+": "+e)
		}
	}
	if !found {
		errs = append(errs, "PDX contains no .odx or .xml entries")
	}
	return errs
}
