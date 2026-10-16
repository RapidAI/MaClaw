//go:build darwin

package guiapp

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework ImageIO
#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <ImageIO/ImageIO.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

// Return codes stay stable so logs can name the failing step:
// 1 bad args, 2 source URL, 3 open, 4 page range, 5 empty page,
// 6 bitmap, 7 image, 8 destination, 9 write, 10 encrypted.
//
// Every local is declared before the first goto. The package also compiles
// cgo as Objective-C (-x objective-c on the pet window), and a goto that
// jumps over an initialized local is a hard error in that mode.

static int openCompanionPDF(const char *pdfPath, CGPDFDocumentRef *outDoc) {
	CFURLRef inURL = NULL;
	CGPDFDocumentRef doc = NULL;

	if (outDoc != NULL) {
		*outDoc = NULL;
	}
	if (pdfPath == NULL || pdfPath[0] == '\0' || outDoc == NULL) {
		return 1;
	}
	inURL = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)pdfPath, (CFIndex)strlen(pdfPath), false);
	if (inURL == NULL) {
		return 2;
	}
	doc = CGPDFDocumentCreateWithURL(inURL);
	CFRelease(inURL);
	if (doc == NULL) {
		return 3;
	}
	// Owner-password PDFs are encrypted but already readable. Failing those
	// sent academic papers back to the "install poppler" path.
	if (!CGPDFDocumentIsUnlocked(doc) && !CGPDFDocumentUnlockWithPassword(doc, "")) {
		CGPDFDocumentRelease(doc);
		return 10;
	}
	*outDoc = doc;
	return 0;
}

static int renderUnlockedPage(CGPDFDocumentRef doc, int page, int dpi, const char *outPath) {
	CGPDFPageRef pdfPage = NULL;
	CGColorSpaceRef color = NULL;
	CGContextRef ctx = NULL;
	CGImageRef image = NULL;
	CFURLRef outURL = NULL;
	CGImageDestinationRef dest = NULL;
	CGPDFBox boxKind = kCGPDFCropBox;
	CGRect box = CGRectZero;
	CGRect destRect = CGRectZero;
	CGAffineTransform xform = CGAffineTransformIdentity;
	size_t count = 0;
	size_t width = 0;
	size_t height = 0;
	double scale = 0;
	double dispW = 0;
	double dispH = 0;
	int rotation = 0;
	int rc = 1;

	if (doc == NULL || outPath == NULL || outPath[0] == '\0' || page < 1 || dpi < 1) {
		return 1;
	}
	count = CGPDFDocumentGetNumberOfPages(doc);
	if (count == 0 || (size_t)page > count) {
		return 4;
	}
	pdfPage = CGPDFDocumentGetPage(doc, (size_t)page);
	if (pdfPage == NULL) {
		return 5;
	}

	box = CGPDFPageGetBoxRect(pdfPage, boxKind);
	if (box.size.width < 1.0 || box.size.height < 1.0) {
		boxKind = kCGPDFMediaBox;
		box = CGPDFPageGetBoxRect(pdfPage, boxKind);
	}
	if (box.size.width < 1.0 || box.size.height < 1.0) {
		return 5;
	}

	// Box rects ignore /Rotate. A landscape page stored upright would otherwise
	// be fitted into a portrait bitmap and letterboxed.
	rotation = CGPDFPageGetRotationAngle(pdfPage) % 360;
	if (rotation < 0) {
		rotation += 360;
	}
	dispW = box.size.width;
	dispH = box.size.height;
	if (rotation == 90 || rotation == 270) {
		dispW = box.size.height;
		dispH = box.size.width;
	}

	scale = (double)dpi / 72.0;
	dispW *= scale;
	dispH *= scale;
	if (dispW > 4096.0 || dispH > 4096.0) {
		double fit = 4096.0 / (dispW > dispH ? dispW : dispH);
		dispW *= fit;
		dispH *= fit;
	}
	if (dispW < 1.0) {
		dispW = 1.0;
	}
	if (dispH < 1.0) {
		dispH = 1.0;
	}
	width = (size_t)ceil(dispW);
	height = (size_t)ceil(dispH);
	if (width < 1 || height < 1 || width > 8192 || height > 8192) {
		return 6;
	}

	color = CGColorSpaceCreateDeviceRGB();
	if (color == NULL) {
		rc = 6;
		goto done;
	}
	// bytesPerRow 0 lets Quartz pick a legal stride. A hand-padded stride is
	// rejected on some versions, and a rejected bitmap is what sent the page
	// back to "install poppler". Skip alpha: a premultiplied buffer comes out
	// blank or black. Fall back if this macOS rejects the opaque format.
	ctx = CGBitmapContextCreate(NULL, width, height, 8, 0, color,
		(CGBitmapInfo)(kCGImageAlphaNoneSkipLast | kCGBitmapByteOrder32Big));
	if (ctx == NULL) {
		ctx = CGBitmapContextCreate(NULL, width, height, 8, 0, color,
			(CGBitmapInfo)(kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big));
	}
	if (ctx == NULL) {
		rc = 6;
		goto done;
	}
	CGContextSetInterpolationQuality(ctx, kCGInterpolationHigh);
	CGContextSetRGBFillColor(ctx, 1, 1, 1, 1);
	CGContextFillRect(ctx, CGRectMake(0, 0, (CGFloat)width, (CGFloat)height));

	destRect = CGRectMake(0, 0, (CGFloat)width, (CGFloat)height);
	xform = CGPDFPageGetDrawingTransform(pdfPage, boxKind, destRect, 0, true);
	CGContextConcatCTM(ctx, xform);
	CGContextDrawPDFPage(ctx, pdfPage);

	image = CGBitmapContextCreateImage(ctx);
	if (image == NULL) {
		rc = 7;
		goto done;
	}
	outURL = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)outPath, (CFIndex)strlen(outPath), false);
	if (outURL == NULL) {
		rc = 8;
		goto done;
	}
	dest = CGImageDestinationCreateWithURL(outURL, CFSTR("public.png"), 1, NULL);
	if (dest == NULL) {
		rc = 8;
		goto done;
	}
	CGImageDestinationAddImage(dest, image, NULL);
	if (!CGImageDestinationFinalize(dest)) {
		rc = 9;
		goto done;
	}
	rc = 0;

done:
	if (dest != NULL) {
		CFRelease(dest);
	}
	if (outURL != NULL) {
		CFRelease(outURL);
	}
	if (image != NULL) {
		CGImageRelease(image);
	}
	if (ctx != NULL) {
		CGContextRelease(ctx);
	}
	if (color != NULL) {
		CGColorSpaceRelease(color);
	}
	return rc;
}

int maclawRenderPDFPagePNG(const char *pdfPath, const char *outPath, int page, int dpi) {
	CGPDFDocumentRef doc = NULL;
	int rc = openCompanionPDF(pdfPath, &doc);
	if (rc != 0) {
		return rc;
	}
	rc = renderUnlockedPage(doc, page, dpi, outPath);
	CGPDFDocumentRelease(doc);
	return rc;
}

// maclawRenderPDFPagesPNG draws every page with one open document. outPaths
// and pages are parallel. A later page still draws when an earlier one fails;
// the return is the first error, or 0 when every page landed.
int maclawRenderPDFPagesPNG(const char *pdfPath, char **outPaths, int *pages, int count, int dpi) {
	CGPDFDocumentRef doc = NULL;
	const char *out = NULL;
	int rc = 0;
	int first = 0;
	int i = 0;

	if (outPaths == NULL || pages == NULL || count < 1) {
		return 1;
	}
	rc = openCompanionPDF(pdfPath, &doc);
	if (rc != 0) {
		return rc;
	}
	for (i = 0; i < count; i++) {
		out = outPaths[i];
		rc = renderUnlockedPage(doc, pages[i], dpi, out);
		if (rc != 0 && first == 0) {
			first = rc;
		}
	}
	CGPDFDocumentRelease(doc);
	return first;
}
*/
import "C"
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"unsafe"
)

// fileCompanionQuartzRenderPtr is the code address of the Quartz renderer.
// Go forbids comparing func values with ==, and a test that swaps the hook
// must not take the batched Quartz path, so identity is checked through
// reflect's code pointer.
var fileCompanionQuartzRenderPtr uintptr

func init() {
	fileCompanionNativePDFRender = fileCompanionRenderPageQuartz
	fileCompanionQuartzRenderPtr = reflect.ValueOf(fileCompanionRenderPageQuartz).Pointer()
}

// fileCompanionRenderPageQuartz rasterizes one page with the PDF engine that
// ships with macOS. Companion builds launched from Finder inherit launchd's
// PATH and almost never see a Homebrew poppler or mupdf, which is what made
// pdf_render_page report a missing renderer on an otherwise working Mac.
func fileCompanionRenderPageQuartz(source, outPNG string, page, dpi int) error {
	csrc := C.CString(source)
	cout := C.CString(outPNG)
	defer C.free(unsafe.Pointer(csrc))
	defer C.free(unsafe.Pointer(cout))
	rc := int(C.maclawRenderPDFPagePNG(csrc, cout, C.int(page), C.int(dpi)))
	if rc != 0 {
		_ = os.Remove(outPNG)
		return quartzPDFRenderError(rc)
	}
	info, err := os.Stat(outPNG)
	if err != nil || info.Size() == 0 {
		_ = os.Remove(outPNG)
		return fmt.Errorf("quartz pdf produced no output")
	}
	return nil
}

func quartzPDFRenderError(rc int) error {
	reason := "渲染失败"
	switch rc {
	case 3:
		reason = "无法打开这份 PDF"
	case 4:
		reason = "页码超出范围"
	case 9:
		reason = "无法写入图片"
	case 10:
		reason = "PDF 已加密"
	}
	return fmt.Errorf("quartz pdf render failed: %s (%d)", reason, rc)
}

// fileCompanionQuartzRenderPages draws the set with one PDF open. handled is
// false when the active renderer is not Quartz, so the caller keeps its
// per-page loop for test doubles.
func fileCompanionQuartzRenderPages(ctx context.Context, source, dir string, pages []int, dpi int) (handled bool, err error) {
	if fileCompanionNativePDFRender == nil ||
		reflect.ValueOf(fileCompanionNativePDFRender).Pointer() != fileCompanionQuartzRenderPtr {
		return false, nil
	}
	if len(pages) == 0 {
		return true, nil
	}
	if ctx != nil && ctx.Err() != nil {
		return true, ctx.Err()
	}
	csrc := C.CString(source)
	defer C.free(unsafe.Pointer(csrc))
	outC := make([]*C.char, len(pages))
	pageC := make([]C.int, len(pages))
	outs := make([]string, len(pages))
	for i, page := range pages {
		outs[i] = filepath.Join(dir, fmt.Sprintf("p%d-1.png", page))
		outC[i] = C.CString(outs[i])
		pageC[i] = C.int(page)
	}
	defer func() {
		for _, p := range outC {
			C.free(unsafe.Pointer(p))
		}
	}()
	rc := int(C.maclawRenderPDFPagesPNG(csrc, (**C.char)(unsafe.Pointer(&outC[0])), (*C.int)(unsafe.Pointer(&pageC[0])), C.int(len(pages)), C.int(dpi)))
	var failed error
	for _, out := range outs {
		info, statErr := os.Stat(out)
		if statErr != nil || info.Size() <= paperFigureMinBytes {
			_ = os.Remove(out)
			if failed == nil {
				if rc != 0 {
					failed = quartzPDFRenderError(rc)
				} else {
					failed = fmt.Errorf("quartz pdf produced no output")
				}
			}
		}
	}
	return true, failed
}
