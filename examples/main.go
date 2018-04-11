package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/discordapp/lilliput"
)

var EncodeOptions = map[string]map[int]int{
	".jpeg": map[int]int{lilliput.JpegQuality: 85},
	".png":  map[int]int{lilliput.PngCompression: 7},
	".webp": map[int]int{lilliput.WebpQuality: 85},
}

var inputFilename string
var outputWidth int
var outputHeight int
var outputFilename string
var stretch bool
var runs int
var remoteInput string
var wg sync.WaitGroup

var client = &http.Client{}

func main() {
	flag.StringVar(&inputFilename, "input", "", "name of input file to resize/transcode")
	flag.StringVar(&remoteInput, "remoteInput", "", "URL of input file to resize/transcode")
	flag.StringVar(&outputFilename, "output", "", "name of output file, also determines output type")
	flag.IntVar(&outputWidth, "width", 0, "width of output file")
	flag.IntVar(&outputHeight, "height", 0, "height of output file")
	flag.IntVar(&runs, "runs", 1, "number of workers should be spawned for the purpose of testing")
	flag.BoolVar(&stretch, "stretch", false, "perform stretching resize instead of cropping")
	flag.Parse()

	for i := 0; i < runs; i++ {
		wg.Add(1)
		go resize(i)
	}
	wg.Wait()
}

func resize(i int) {
	var err error
	defer wg.Done()
	outputFilename := fmt.Sprint(i, "-", outputFilename)

	if inputFilename == "" && remoteInput == "" {
		fmt.Printf("No input filename or remote input URL provided, quitting.\n")
		flag.Usage()
		return
	}

	var inputBuf []byte
	if inputFilename != "" {
		// decoder wants []byte, so read the whole file into a buffer
		inputBuf, err = ioutil.ReadFile(inputFilename)
		if err != nil {
			fmt.Printf("failed to read input file, %s\n", err)
			return
		}
	} else if remoteInput != "" {
		// Fetch remote input URL
		// decoder wants []byte, so read the whole file into a buffer
		inputBuf, err = readRemoteURL(remoteInput)
		if err != nil {
			fmt.Printf("failed to read remote input, %s\n", err)
			return
		}
	}

	decoder, err := lilliput.NewDecoder(inputBuf)
	// this error reflects very basic checks,
	// mostly just for the magic bytes of the file to match known image formats
	if err != nil {
		fmt.Printf("error decoding image, %s\n", err)
		return
	}
	defer decoder.Close()

	header, err := decoder.Header()
	// this error is much more comprehensive and reflects
	// format errors
	if err != nil {
		fmt.Printf("error reading image header, %s\n", err)
		return
	}

	// print some basic info about the image
	fmt.Printf("file type: %s\n", decoder.Description())
	fmt.Printf("%dpx x %dpx\n", header.Width(), header.Height())

	if decoder.Duration() != 0 {
		fmt.Printf("duration: %.2f s\n", float64(decoder.Duration())/float64(time.Second))
	}

	// get ready to resize image,
	// using 8192x8192 maximum resize buffer size
	ops := lilliput.NewImageOps(8192)
	defer ops.Close()

	// create a buffer to store the output image, 50MB in this case
	outputImg := make([]byte, 50*1024*1024)

	// use user supplied filename to guess output type if provided
	// otherwise don't transcode (use existing type)
	outputType := "." + strings.ToLower(decoder.Description())
	if outputFilename != "" {
		outputType = filepath.Ext(outputFilename)
	}

	if outputWidth == 0 {
		outputWidth = header.Width()
	}

	if outputHeight == 0 {
		outputHeight = header.Height()
	}

	resizeMethod := lilliput.ImageOpsFit
	if stretch {
		resizeMethod = lilliput.ImageOpsResize
	}

	opts := &lilliput.ImageOptions{
		FileType:             outputType,
		Width:                outputWidth,
		Height:               outputHeight,
		ResizeMethod:         resizeMethod,
		NormalizeOrientation: true,
		EncodeOptions:        EncodeOptions[outputType],
	}

	// resize and transcode image
	outputImg, err = ops.Transform(decoder, opts, outputImg)
	if err != nil {
		fmt.Printf("error transforming image, %s\n", err)
		return
	}

	// image has been resized, now write file out
	if outputFilename == "" {
		outputFilename = "resized" + filepath.Ext(inputFilename)
	}

	if _, err := os.Stat(outputFilename); !os.IsNotExist(err) {
		fmt.Printf("output filename %s exists, quitting\n", outputFilename)
		return
	}

	err = ioutil.WriteFile(outputFilename, outputImg, 0400)
	if err != nil {
		fmt.Printf("error writing out resized image, %s\n", err)
		return
	}

	fmt.Printf("image written to %s\n", outputFilename)
}

func readRemoteURL(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
