//go:build !darwin

// Package mtl is a minimal Metal compute binding (macOS only).
package mtl

import (
	"errors"
	"unsafe"
)

var errNoMetal = errors.New("mtl: Metal is only available on macOS")

type Device struct{ Name string }
type Buffer struct{ Size int }
type Pipeline struct{ MaxThreads int }
type Encoder struct{}

func NewDevice() (*Device, error)                                        { return nil, errNoMetal }
func (d *Device) NewBuffer(n int) (*Buffer, error)                       { return nil, errNoMetal }
func (d *Device) Compile(string, []string) (map[string]*Pipeline, error) { return nil, errNoMetal }
func (d *Device) CompileOpts(string, []string, bool) (map[string]*Pipeline, error) {
	return nil, errNoMetal
}
func (d *Device) Begin() *Encoder                      { return nil }
func (b *Buffer) Bytes() []byte                        { return nil }
func (b *Buffer) Float32s() []float32                  { return nil }
func (b *Buffer) Int32s() []int32                      { return nil }
func (b *Buffer) Release()                             {}
func (e *Encoder) SetPipeline(*Pipeline)               {}
func (e *Encoder) SetBuffer(*Buffer, int, int)         {}
func (e *Encoder) SetBytes(unsafe.Pointer, int, int)   {}
func (e *Encoder) Dispatch(gx, gy, gz, tx, ty, tz int) {}
func (e *Encoder) Wait() error                         { return errNoMetal }
