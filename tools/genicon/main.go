// Genicon copies the flat cat artwork to resources/icon.png, the app icon.
//
//	go run ./tools/genicon
package main

import (
	"bytes"
	"image/png"
	"log"
	"os"
)

func main() {
	data, err := os.ReadFile("resources/kopi-flat-cat-icon.png")
	if err != nil {
		log.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	if config.Width != config.Height || config.Width < 1024 {
		log.Fatal("app icon must be a square PNG at least 1024 pixels wide")
	}
	if err := os.WriteFile("resources/icon.png", data, 0o644); err != nil {
		log.Fatal(err)
	}
}
