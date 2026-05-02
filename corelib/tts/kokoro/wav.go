package kokoro

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

func WriteWAV(path string, pcm []float32, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("kokoro: create wav: %w", err)
	}
	defer f.Close()
	return WriteWAVTo(f, pcm, sampleRate)
}

func WriteWAVTo(w io.Writer, pcm []float32, sampleRate int) error {
	dataBytes := uint32(len(pcm) * 2)
	if _, err := w.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(36)+dataBytes); err != nil {
		return err
	}
	if _, err := w.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(sampleRate*2)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(2)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(16)); err != nil {
		return err
	}
	if _, err := w.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, dataBytes); err != nil {
		return err
	}
	for _, v := range pcm {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		s := int16(math.Round(float64(v * 32767)))
		if err := binary.Write(w, binary.LittleEndian, s); err != nil {
			return err
		}
	}
	return nil
}
