// Command pdfrender renders every page of a PDF to PNG files.
package main

import (
	"fmt"
	"image/png"
	"os"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	imgs, err := gopdf.RenderAllPagesToImages(data, gopdf.RenderOption{DPI: 110})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	for i, img := range imgs {
		f, _ := os.Create(fmt.Sprintf("%s-page%d.png", os.Args[2], i))
		png.Encode(f, img)
		f.Close()
	}
	fmt.Println("rendered", len(imgs), "pages")
}
