//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>

int copyFilesToPasteboard(const char** paths, int count) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];

    NSMutableArray *urls = [NSMutableArray arrayWithCapacity:count];
    for (int i = 0; i < count; i++) {
        NSString *path = [NSString stringWithUTF8String:paths[i]];
        NSURL *url = [NSURL fileURLWithPath:path];
        if (url) {
            [urls addObject:url];
        }
    }
    BOOL ok = [pb writeObjects:urls];
    return ok ? 0 : 1;
}

// Publishes one pasteboard item holding the original image bytes under their
// native type and, when png is non-NULL, a PNG copy. Either may be absent but
// not both. The bytes are copied into NSData before returning.
int copyImageFlavorsToPasteboard(const void* original, int originalLen, const char* originalType, const void* png, int pngLen) {
    int result = 1;
    @autoreleasepool {
        NSPasteboardItem *item = [[[NSPasteboardItem alloc] init] autorelease];
        BOOL any = NO;
        if (original != NULL && originalLen > 0 && originalType != NULL) {
            NSString *type = [NSString stringWithUTF8String:originalType];
            NSData *originalData = [NSData dataWithBytes:original length:(NSUInteger)originalLen];
            if (type != nil && [item setData:originalData forType:type]) {
                any = YES;
            }
        }
        if (png != NULL && pngLen > 0) {
            NSData *pngData = [NSData dataWithBytes:png length:(NSUInteger)pngLen];
            if ([item setData:pngData forType:NSPasteboardTypePNG]) {
                any = YES;
            }
        }
        if (any) {
            NSPasteboard *pb = [NSPasteboard generalPasteboard];
            [pb clearContents];
            if ([pb writeObjects:[NSArray arrayWithObject:item]]) {
                result = 0;
            }
        }
    }
    return result;
}

// Publishes HTML and its plain-text rendering as one pasteboard item, so
// rich-text editors paste the formatting and plain-text fields the text.
int copyRichTextToPasteboard(const char* html, const char* plain) {
    int result = 1;
    @autoreleasepool {
        NSString *htmlString = html != NULL ? [NSString stringWithUTF8String:html] : nil;
        NSString *plainString = plain != NULL ? [NSString stringWithUTF8String:plain] : nil;
        if (htmlString != nil) {
            NSPasteboardItem *item = [[[NSPasteboardItem alloc] init] autorelease];
            BOOL ok = [item setString:htmlString forType:NSPasteboardTypeHTML];
            if (ok && plainString != nil) {
                ok = [item setString:plainString forType:NSPasteboardTypeString];
            }
            if (ok) {
                NSPasteboard *pb = [NSPasteboard generalPasteboard];
                [pb clearContents];
                if ([pb writeObjects:[NSArray arrayWithObject:item]]) {
                    result = 0;
                }
            }
        }
    }
    return result;
}
*/
import "C"

import (
	"fmt"
	"math"
	"strings"
	"unsafe"

	"go-clipboard/internal/cliputil"

	"golang.design/x/clipboard"
)

func copyFilesToClipboard(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths provided")
	}
	cPaths := make([]*C.char, len(paths))
	for i, p := range paths {
		cPaths[i] = C.CString(p)
	}
	defer func() {
		for _, cp := range cPaths {
			C.free(unsafe.Pointer(cp))
		}
	}()

	result := C.copyFilesToPasteboard(&cPaths[0], C.int(len(paths)))
	if result != 0 {
		return fmt.Errorf("failed to write files to pasteboard")
	}
	return nil
}

// copyImageToClipboard places PNG image data on the macOS clipboard.
func copyImageToClipboard(pngData []byte) error {
	return cliputil.Write(clipboard.FmtImage, pngData)
}

func init() {
	nativeImageFlavorWriter = writeImageFlavorsToPasteboard
	richTextWriter = writeRichTextToPasteboard
}

// bytesPointer returns the address of b's first byte, or nil when b is empty.
// The C side copies the bytes before returning.
func bytesPointer(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Pointer(&b[0])
}

// writeImageFlavorsToPasteboard publishes an image's original bytes under
// nativeType (a UTI such as public.jpeg) and, when pngData is non-nil, a PNG
// copy, as a single pasteboard item.
func writeImageFlavorsToPasteboard(original []byte, nativeType string, pngData []byte) error {
	if len(original) > math.MaxInt32 || len(pngData) > math.MaxInt32 {
		return fmt.Errorf("image is too large for the pasteboard")
	}
	cType := C.CString(nativeType)
	defer C.free(unsafe.Pointer(cType))
	result := C.copyImageFlavorsToPasteboard(
		bytesPointer(original), C.int(len(original)), cType,
		bytesPointer(pngData), C.int(len(pngData)))
	if result != 0 {
		return fmt.Errorf("failed to write image to pasteboard")
	}
	return nil
}

// writeRichTextToPasteboard publishes HTML with a plain-text fallback.
func writeRichTextToPasteboard(htmlDoc, plain string) error {
	cHTML := C.CString(strings.ReplaceAll(htmlDoc, "\x00", ""))
	defer C.free(unsafe.Pointer(cHTML))
	cPlain := C.CString(strings.ToValidUTF8(strings.ReplaceAll(plain, "\x00", ""), "\uFFFD"))
	defer C.free(unsafe.Pointer(cPlain))
	if C.copyRichTextToPasteboard(cHTML, cPlain) != 0 {
		return fmt.Errorf("failed to write rich text to pasteboard")
	}
	return nil
}
