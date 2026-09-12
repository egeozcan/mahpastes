//go:build darwin && cgo && fileprovider && !bindings

#import <Foundation/Foundation.h>
#import <FileProvider/FileProvider.h>
#import <Security/Security.h>
#import <AppKit/AppKit.h>

static char *encode(NSDictionary *value) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
    return strdup([[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String);
}

// Called on a Go goroutine, never dispatched synchronously to the main queue.
// File Provider completions own their captured state even if the caller times
// out. No Go pointers cross the asynchronous native boundary.
char *mahpastes_fp_call(const char *request) {
    @autoreleasepool {
        if (@available(macOS 13.0, *)) {
            NSDictionary *input = [NSJSONSerialization JSONObjectWithData:[[NSString stringWithUTF8String:request] dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];
            NSBundle *bundle = NSBundle.mainBundle;
            NSString *group = [bundle objectForInfoDictionaryKey:@"MahpastesAppGroup"];
            NSString *keychainGroup = [bundle objectForInfoDictionaryKey:@"MahpastesKeychainGroup"];
            NSString *extensionPath = [bundle.builtInPlugInsPath stringByAppendingPathComponent:@"MahpastesFileProvider.appex"];
            if (!group.length || !keychainGroup.length || ![NSFileManager.defaultManager fileExistsAtPath:extensionPath])
                return encode(@{@"error":@"This app bundle does not contain the Finder extension."});
            NSURL *container = [NSFileManager.defaultManager containerURLForSecurityApplicationGroupIdentifier:group];
            if (!container) return encode(@{@"error":@"The signed app cannot access its App Group container."});
            NSString *operation = input[@"operation"];
            NSString *identifier = input[@"domain"];
            if ([operation isEqualToString:@"info"]) return encode(@{@"supported":@YES,@"groupPath":container.path});
            if (!identifier.length) return encode(@{@"error":@"Missing File Provider domain."});
            if ([operation isEqualToString:@"credential"]) {
                NSMutableDictionary *query = [@{(__bridge id)kSecClass:(__bridge id)kSecClassGenericPassword,
                    (__bridge id)kSecAttrService:@"MahpastesFileProvider",(__bridge id)kSecAttrAccount:identifier,
                    (__bridge id)kSecAttrAccessGroup:keychainGroup,(__bridge id)kSecReturnData:@YES} mutableCopy];
                CFTypeRef result = NULL;
                OSStatus status = SecItemCopyMatching((__bridge CFDictionaryRef)query,&result);
                NSData *data = CFBridgingRelease(result);
                if (status == errSecItemNotFound) {
                    uint8_t bytes[32];
                    status = SecRandomCopyBytes(kSecRandomDefault,sizeof(bytes),bytes);
                    if (status == errSecSuccess) {
                        data = [NSData dataWithBytes:bytes length:sizeof(bytes)];
                        [query removeObjectForKey:(__bridge id)kSecReturnData];
                        query[(__bridge id)kSecValueData] = data;
                        query[(__bridge id)kSecAttrAccessible] = (__bridge id)kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly;
                        status = SecItemAdd((__bridge CFDictionaryRef)query,NULL);
                    }
                }
                if (status != errSecSuccess) return encode(@{@"error":[NSString stringWithFormat:@"File Provider Keychain access failed (%d).",(int)status]});
                return encode(@{@"supported":@YES,@"secret":[data base64EncodedStringWithOptions:0]});
            }
            NSFileProviderDomain *domain = [[NSFileProviderDomain alloc] initWithIdentifier:identifier displayName:@"Mahpastes"];
            dispatch_semaphore_t done = dispatch_semaphore_create(0);
            __block NSError *failure = nil;
            __block NSArray *domains = @[];
            __block NSString *recovery = @"";
            if ([operation isEqualToString:@"list"]) {
                [NSFileProviderManager getDomainsWithCompletionHandler:^(NSArray<NSFileProviderDomain *> *values,NSError *error){
                    failure=error;domains=[values valueForKey:@"identifier"] ?: @[];dispatch_semaphore_signal(done);
                }];
            } else if ([operation isEqualToString:@"add"]) {
                [NSFileProviderManager addDomain:domain completionHandler:^(NSError *error){failure=error;dispatch_semaphore_signal(done);}];
            } else if ([operation isEqualToString:@"remove"]) {
                [NSFileProviderManager removeDomain:domain mode:NSFileProviderDomainRemovalModePreserveDownloadedUserData completionHandler:^(NSURL *url,NSError *error){failure=error;recovery=url.path ?: @"";dispatch_semaphore_signal(done);}];
            } else {
                NSFileProviderManager *manager = [NSFileProviderManager managerForDomain:domain];
                if (!manager) return encode(@{@"error":@"The Finder domain is not registered."});
                if ([operation isEqualToString:@"signal"]) {
                    // Signal all live folder enumerators as well as the working
                    // set; notifications are hints, the journal is authoritative.
                    dispatch_group_t signals = dispatch_group_create();
                    for (NSString *item in @[NSFileProviderWorkingSetContainerItemIdentifier,NSFileProviderRootContainerItemIdentifier,@"active",@"archive"]) {
                        dispatch_group_enter(signals);
                        [manager signalEnumeratorForContainerItemIdentifier:item completionHandler:^(NSError *error){dispatch_group_leave(signals);}];
                    }
                    [manager signalErrorResolved:[NSError errorWithDomain:NSFileProviderErrorDomain code:NSFileProviderErrorServerUnreachable userInfo:nil] completionHandler:^(NSError *error){}];
                    dispatch_group_notify(signals,dispatch_get_global_queue(QOS_CLASS_UTILITY,0),^{dispatch_semaphore_signal(done);});
                } else if ([operation isEqualToString:@"reveal"]) {
                    [manager getUserVisibleURLForItemIdentifier:NSFileProviderRootContainerItemIdentifier completionHandler:^(NSURL *url,NSError *error){
                        failure=error;
                        if (url) dispatch_async(dispatch_get_main_queue(),^{[NSWorkspace.sharedWorkspace activateFileViewerSelectingURLs:@[url]];});
                        dispatch_semaphore_signal(done);
                    }];
                } else return encode(@{@"error":@"Unknown File Provider operation."});
            }
            if (dispatch_semaphore_wait(done,dispatch_time(DISPATCH_TIME_NOW,30*NSEC_PER_SEC))!=0)
                return encode(@{@"error":@"macOS did not finish the File Provider operation within 30 seconds. Check System Settings and retry."});
            if (failure) return encode(@{@"error":failure.localizedDescription});
            return encode(@{@"supported":@YES,@"domains":domains,@"recoveryPath":recovery});
        }
        return encode(@{@"error":@"Finder integration requires macOS 13 or later."});
    }
}
