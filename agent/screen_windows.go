//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"syscall"
	"unsafe"
)

var (
	scrUser32 = syscall.NewLazyDLL("user32.dll")
	scrGdi32  = syscall.NewLazyDLL("gdi32.dll")

	pScrGetDC            = scrUser32.NewProc("GetDC")
	pScrReleaseDC        = scrUser32.NewProc("ReleaseDC")
	pScrGetSystemMetrics = scrUser32.NewProc("GetSystemMetrics")

	pCreateCompatibleDC  = scrGdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBmp = scrGdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject        = scrGdi32.NewProc("SelectObject")
	pBitBlt              = scrGdi32.NewProc("BitBlt")
	pGetDIBits           = scrGdi32.NewProc("GetDIBits")
	pDeleteObject        = scrGdi32.NewProc("DeleteObject")
	pDeleteDC            = scrGdi32.NewProc("DeleteDC")
)

const (
	smCXScreen   = 0
	smCYScreen   = 1
	srcCopy      = 0x00CC0020
	biRGB        = 0
	dibRGBColors = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// captureScreenJPEG grabs the primary display via GDI (BitBlt into a memory DC,
// then GetDIBits) and encodes it as JPEG. Pure syscall — no cgo, no dependency.
func captureScreenJPEG(quality int) ([]byte, error) {
	w, _, _ := pScrGetSystemMetrics.Call(smCXScreen)
	h, _, _ := pScrGetSystemMetrics.Call(smCYScreen)
	width, height := int(w), int(h)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("could not read screen size")
	}

	screenDC, _, _ := pScrGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer pScrReleaseDC.Call(0, screenDC)

	memDC, _, _ := pCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer pDeleteDC.Call(memDC)

	bmp, _, _ := pCreateCompatibleBmp.Call(screenDC, uintptr(width), uintptr(height))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer pDeleteObject.Call(bmp)

	old, _, _ := pSelectObject.Call(memDC, bmp)
	defer pSelectObject.Call(memDC, old)

	if ret, _, _ := pBitBlt.Call(memDC, 0, 0, uintptr(width), uintptr(height),
		screenDC, 0, 0, srcCopy); ret == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(width)
	bi.Header.Height = -int32(height) // negative = top-down rows
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	buf := make([]byte, width*height*4)
	ret, _, _ := pGetDIBits.Call(memDC, bmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}

	// GDI returns 32-bit pixels as B,G,R,X; convert to R,G,B,A for image/jpeg.
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.Pix[i*4+0] = buf[i*4+2] // R
		img.Pix[i*4+1] = buf[i*4+1] // G
		img.Pix[i*4+2] = buf[i*4+0] // B
		img.Pix[i*4+3] = 0xff       // A
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
