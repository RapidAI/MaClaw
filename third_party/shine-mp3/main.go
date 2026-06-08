package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/braheezy/shine-mp3/pkg/mp3"
	"github.com/go-audio/wav"
)

func main() {
	// Handle command line arguments
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <input file> <output file>\n", os.Args[0])
		os.Exit(1)
	}
	inFile := os.Args[1]
	outFile := os.Args[2]

	// Check arguments
	if filepath.Ext(inFile) != ".wav" {
		log.Fatalf("Input file \"%s\" must be a WAV file.\n", inFile)
	}
	if filepath.Ext(outFile) != ".mp3" {
		log.Fatalf("Output file \"%s\" must be a MP3 file.\n", outFile)
	}

	// Read input file
	inputData, err := os.ReadFile(inFile)
	if err != nil {
		fmt.Printf("Error loading audio file: %v\n", err)
		return
	}

	// Decode WAV file
	wavReader := bytes.NewReader(inputData)
	wavDecoder := wav.NewDecoder(wavReader)
	wavBuffer, err := wavDecoder.FullPCMBuffer()
	if err != nil {
		log.Fatalf("Error decoding WAV file: %v", err)
	}

	// Convert audio data to int16
	decodedData := make([]int16, len(wavBuffer.Data))
	for i, val := range wavBuffer.Data {
		decodedData[i] = int16(val)
	}

	// Create output file
	out, err := os.Create(outFile)
	if err != nil {
		fmt.Printf("Could not create \"%s\".\n", outFile)
		os.Exit(1)
	}
	defer out.Close()

	// Create new encoder with audio settings
	mp3Encoder := mp3.NewEncoder(wavBuffer.Format.SampleRate, wavBuffer.Format.NumChannels)

	// Write all the data to the output file
	mp3Encoder.Write(out, decodedData)
}
