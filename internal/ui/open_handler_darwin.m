// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Native Finder "open document" handling for spice-viewer.
// Double-clicking a .vv delivers kAEOpenDocuments (not a reliable argv path).

#import "open_handler_darwin.h"

#import <Cocoa/Cocoa.h>
#import <Foundation/Foundation.h>

#include <stdlib.h>
#include <string.h>

// Implemented in Go (//export spiceViewerGoOnOpenDocument). Signature must
// match cgo export (non-const char *).
extern void spiceViewerGoOnOpenDocument(char *path);

static char *pendingPath = NULL;
static id openHandler = nil;

static void setPendingPath(NSString *path) {
	if (path == nil || [path length] == 0) {
		return;
	}
	free(pendingPath);
	pendingPath = strdup([path fileSystemRepresentation]);
}

@interface SpiceViewerOpenHandler : NSObject
- (void)handleOpenDocuments:(NSAppleEventDescriptor *)event
	      withReplyEvent:(NSAppleEventDescriptor *)reply;
@end

@implementation SpiceViewerOpenHandler

- (void)handleOpenDocuments:(NSAppleEventDescriptor *)event
	      withReplyEvent:(NSAppleEventDescriptor *)replyEvent {
	NSAppleEventDescriptor *list = [event paramDescriptorForKeyword:keyDirectObject];
	if (list == nil) {
		return;
	}
	NSInteger n = [list numberOfItems];
	for (NSInteger i = 1; i <= n; i++) {
		NSAppleEventDescriptor *item = [list descriptorAtIndex:i];
		if (item == nil) {
			continue;
		}
		NSString *path = nil;

		// Prefer file URL form.
		NSAppleEventDescriptor *urlDesc = [item coerceToDescriptorType:typeFileURL];
		if (urlDesc != nil) {
			NSData *data = [urlDesc data];
			if (data != nil) {
				NSURL *u = [[NSURL alloc] initWithDataRepresentation:data relativeToURL:nil];
				path = [u path];
			}
		}

		// Fallback: POSIX path string.
		if (path == nil) {
			NSString *s = [item stringValue];
			if (s != nil) {
				if ([s hasPrefix:@"file:"]) {
					path = [[NSURL URLWithString:s] path];
				} else if ([s hasPrefix:@"/"]) {
					path = s;
				}
			}
		}

		if (path != nil && [path length] > 0) {
			setPendingPath(path);
			// Cold start: Go ignores until EnableLiveOpens; pending path is taken
			// by ResolveConnectionPath. Live: opens a new session window in-process.
			if (pendingPath != NULL) {
				spiceViewerGoOnOpenDocument(pendingPath);
			}
		}
	}
}

@end

void spiceViewerInstallOpenHandler(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		if (openHandler == nil) {
			openHandler = [[SpiceViewerOpenHandler alloc] init];
		}
		[[NSAppleEventManager sharedAppleEventManager]
			setEventHandler:openHandler
			andSelector:@selector(handleOpenDocuments:withReplyEvent:)
			forEventClass:kCoreEventClass
			andEventID:kAEOpenDocuments];
	}
}

void spiceViewerPumpOpenEvents(double seconds) {
	@autoreleasepool {
		NSApplication *app = [NSApplication sharedApplication];
		NSDate *until = [NSDate dateWithTimeIntervalSinceNow:seconds];
		while ([until timeIntervalSinceNow] > 0) {
			// Deliver Apple Events without requiring a full NSApp run.
			NSDate *slice = [NSDate dateWithTimeIntervalSinceNow:0.05];
			[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode beforeDate:slice];
			NSEvent *ev = [app nextEventMatchingMask:NSEventMaskAny
						   untilDate:[NSDate dateWithTimeIntervalSinceNow:0.0]
						      inMode:NSDefaultRunLoopMode
						     dequeue:YES];
			if (ev != nil) {
				[app sendEvent:ev];
			}
			if (pendingPath != NULL) {
				break;
			}
		}
	}
}

char *spiceViewerTakePendingPath(void) {
	char *p = pendingPath;
	pendingPath = NULL;
	return p;
}
